package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vedantxn/chromebox/internal/api"
	"github.com/vedantxn/chromebox/internal/base"
)

// ForkResult is what a fork did: the parent's pause, the image, and the copies.
type ForkResult struct {
	Parent   string   `json:"parent"`
	Image    string   `json:"image"`
	PausedMs float64  `json:"paused_ms"` // the parent, while its snapshot was taken
	ImageMiB int      `json:"image_mib"` // RAM the image holds (zero pages punched out)
	ZeroMiB  int      `json:"zero_mib"`
	ImageMs  float64  `json:"image_ms"` // request -> image ready
	TotalMs  float64  `json:"total_ms"` // request -> every copy answering
	Copies   []api.VM `json:"copies"`
	Errors   []string `json:"errors,omitempty"`
}

// Fork copies running VM id n times (M6). The parent's node turns it into a
// fork image: a base snapshot made on the fly from the running browser, with
// its tabs, cookies and storage. Then n ordinary creates run from that image,
// in parallel. Placement does the rest: a VM of a base can only go to a node
// that holds the base, which for a new image is the parent's node. Each copy
// gets its own address and session token, exactly like a fresh browser.
// onReserve runs for each copy under the placement lock (account limits).
func (o *Orchestrator) Fork(ctx context.Context, id string, n int, ttl time.Duration, onReserve ...func(id string) error) (ForkResult, error) {
	start := time.Now()
	res := ForkResult{Parent: id, Image: "fork-" + strings.TrimPrefix(id, "vm-") + "-" + randHex(2)}
	parent, err := o.store.GetVM(id)
	if err != nil {
		return res, fmt.Errorf("unknown vm %q", id)
	}
	if parent.State != StateRunning {
		return res, fmt.Errorf("vm %s is %s, not running", id, parent.State)
	}
	ag, err := o.agentFor(parent.Node)
	if err != nil {
		return res, err
	}
	fr, err := ag.ForkImage(ctx, id, res.Image)
	if err != nil {
		return res, err
	}
	res.PausedMs, res.ImageMiB, res.ZeroMiB = fr.PausedMs, fr.HeldMiB, fr.ZeroMiB
	res.ImageMs = float64(time.Since(start).Microseconds()) / 1000

	var mu sync.Mutex
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vm, _, err := o.Create(ctx, res.Image, ttl, onReserve...)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				res.Errors = append(res.Errors, err.Error())
				return
			}
			res.Copies = append(res.Copies, vm)
		}()
	}
	wg.Wait()
	res.TotalMs = float64(time.Since(start).Microseconds()) / 1000
	if len(res.Copies) == 0 {
		return res, fmt.Errorf("no copy started: %s", strings.Join(res.Errors, "; "))
	}
	return res, nil
}

func (o *Orchestrator) forkVM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Count      int `json:"count"`
		TTLSeconds int `json:"ttl_seconds"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Count < 1 || req.Count > 16 {
		http.Error(w, "count must be 1-16", http.StatusBadRequest)
		return
	}
	if req.TTLSeconds <= 0 || req.TTLSeconds > 3600 {
		req.TTLSeconds = 900
	}
	res, err := o.Fork(r.Context(), r.PathValue("id"), req.Count, time.Duration(req.TTLSeconds)*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// collectForks deletes fork images no VM uses any more. An image is given a
// minute first, so that its copies have time to be placed.
func (o *Orchestrator) collectForks(ctx context.Context) {
	vms, err := o.store.ListVMs()
	if err != nil {
		return
	}
	used := map[string]bool{}
	for _, v := range vms {
		used[v.Base] = true
	}
	o.mu.RLock()
	type img struct{ node, name string }
	var stale []img
	for node, s := range o.seen {
		for _, b := range s.Status.Bases {
			if base.IsFork(b.Name) && !used[b.Name] && time.Since(b.BuiltAt) > time.Minute {
				stale = append(stale, img{node, b.Name})
			}
		}
	}
	o.mu.RUnlock()
	for _, s := range stale {
		if ag, err := o.agentFor(s.node); err == nil {
			if err := ag.DeleteBase(ctx, s.name); err == nil {
				log.Printf("fork image %s on %s: no VM uses it, deleted", s.name, s.node)
			}
		}
	}
}
