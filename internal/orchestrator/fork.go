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
	Spills   []Spill  `json:"spills,omitempty"` // other nodes the image was pulled to
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

	// Copies go to the parent's node while it has room: there they share the
	// image's memory. The rest spill: the image is pulled to another node (its
	// disk as a delta from the base every node holds, its memory without zero
	// pages), and the copies that did not fit are placed again.
	left := n
	have := []string{parent.Node} // nodes holding the image
	for {
		made, full := o.createCopies(ctx, res.Image, left, ttl, &res, onReserve...)
		res.Copies = append(res.Copies, made...)
		if full == 0 {
			break
		}
		left = full
		mem, _ := o.baseMem(res.Image)
		views, err := o.views()
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			break
		}
		node, err := Place(views, Request{MemMiB: mem, Base: fr.DiskBase}, o.cfg.Strategy, have...)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%d copies did not fit: %v", left, err))
			break
		}
		dst, err := o.agentFor(node)
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			break
		}
		t := time.Now()
		pr, err := dst.PullImage(ctx, res.Image, ag.URL)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("spill to %s: %v", node, err))
			break
		}
		res.Spills = append(res.Spills, Spill{Node: node, Ms: float64(time.Since(t).Microseconds()) / 1000,
			DiskMiB: pr.DiskMiB, MemMiB: pr.MemMiB})
		have = append(have, node)
	}
	res.TotalMs = float64(time.Since(start).Microseconds()) / 1000
	if len(res.Copies) == 0 {
		return res, fmt.Errorf("no copy started: %s", strings.Join(res.Errors, "; "))
	}
	return res, nil
}

// Spill is one node a fork image was pulled to, and what that cost.
type Spill struct {
	Node    string  `json:"node"`
	Ms      float64 `json:"ms"`
	DiskMiB float64 `json:"disk_mib"`
	MemMiB  float64 `json:"mem_mib"`
}

// createCopies starts n copies from image in parallel. It returns the ones
// that started and how many failed only because no node holding the image had
// room (those can spill); other failures, such as an account limit, are
// recorded in res.Errors and end there.
func (o *Orchestrator) createCopies(ctx context.Context, image string, n int, ttl time.Duration, res *ForkResult, onReserve ...func(id string) error) ([]api.VM, int) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var made []api.VM
	full := 0
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vm, _, err := o.Create(ctx, image, ttl, onReserve...)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				made = append(made, vm)
			case strings.HasPrefix(err.Error(), "no node can take"):
				full++
			default:
				res.Errors = append(res.Errors, err.Error())
			}
		}()
	}
	wg.Wait()
	return made, full
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
