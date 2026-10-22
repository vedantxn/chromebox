package orchestrator

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/vedantxn/chromebox/internal/api"
)

// MigrationResult reports one migration.
type MigrationResult struct {
	VM       string  `json:"vm"`
	From     string  `json:"from"`
	To       string  `json:"to"`
	PausedMs float64 `json:"paused_ms"` // source paused -> running on target
	// UnreachableMs adds the route flip: paused -> the gateway sends packets to the target.
	UnreachableMs float64 `json:"unreachable_ms"`
	DeltaMiB      float64 `json:"delta_mib"`
	Error         string  `json:"error,omitempty"`
}

// Migrate moves VM id to target ("" lets the scheduler choose). Two phases:
// the source agent moves the VM and leaves its own copy paused; then the
// orchestrator points the gateway at the target and tells the source to
// commit (destroy its copy). If anything fails before the route moves, the
// source is told to abort (resume) and the target's copy is deleted.
func (o *Orchestrator) Migrate(ctx context.Context, id, target string) (MigrationResult, error) {
	vm, err := o.store.GetVM(id)
	if err != nil {
		return MigrationResult{}, fmt.Errorf("unknown vm %q", id)
	}
	res := MigrationResult{VM: id, From: vm.Node}
	if vm.State != StateRunning {
		return res, fmt.Errorf("vm %s is %s, not running", id, vm.State)
	}

	// Choose the target and hold its memory until the move is over, so that
	// concurrent migrations and creates don't all pick the same last slot.
	o.place.Lock()
	if target == "" {
		views, err := o.views()
		if err != nil {
			o.place.Unlock()
			return res, err
		}
		if target, err = Place(views, Request{MemMiB: vm.MemMiB, Base: vm.Base}, o.cfg.Strategy, vm.Node); err != nil {
			o.place.Unlock()
			return res, err
		}
	}
	o.pending[target] += vm.MemMiB
	o.place.Unlock()
	defer func() {
		o.place.Lock()
		o.pending[target] -= vm.MemMiB
		o.place.Unlock()
	}()
	res.To = target

	src, err := o.agentFor(vm.Node)
	if err != nil {
		return res, err
	}
	dst, err := o.agentFor(target)
	if err != nil {
		return res, err
	}
	if err := o.store.SetState(id, StateMigrating); err != nil {
		return res, err
	}
	restore := func() { _ = o.store.SetState(id, StateRunning) }

	start := time.Now()
	mr, err := src.Migrate(ctx, id, api.MigrateRequest{TargetAgent: dst.URL, TargetSSH: "ec2-user@" + hostOf(dst.URL)})
	if err != nil {
		restore() // the source agent has already resumed the VM and cleaned the target
		return res, err
	}
	// The VM runs on the target. Move the network, then retire the source.
	if err := o.setRoute(ctx, vm.NetIndex, target); err != nil {
		_ = dst.DeleteVM(context.Background(), id)
		_ = src.Abort(context.Background(), id)
		restore()
		return res, fmt.Errorf("route flip failed, migration aborted: %w", err)
	}
	unreachable := mr.PausedMs + float64(time.Since(start).Microseconds())/1000 - lapTotal(mr)
	if err := o.store.SetNode(id, target); err != nil {
		return res, err
	}
	restore()
	if err := src.Commit(ctx, id); err != nil {
		log.Printf("migrate %s: commit on %s failed (the source copy is paused and orphaned): %v", id, vm.Node, err)
	}
	res.PausedMs, res.UnreachableMs = mr.PausedMs, unreachable
	o.store.countMigration(id)
	res.DeltaMiB = float64(mr.DeltaBytes) / (1 << 20)
	return res, nil
}

// lapTotal is the total time the source agent spent, so that the time after it
// (the route flip) can be added to the pause.
func lapTotal(mr api.MigrateResponse) float64 { return mr.Laps.TotalMs }

// Drain empties a node: cordon it, then migrate each of its VMs to a node the
// scheduler chooses, two at a time. A VM whose migration fails is retried once
// on another target.
func (o *Orchestrator) Drain(ctx context.Context, node string) ([]MigrationResult, error) {
	if err := o.store.SetCordoned(node, true); err != nil {
		return nil, err
	}
	vms, err := o.store.ListVMs()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, v := range vms {
		if v.Node == node && v.State == StateRunning {
			ids = append(ids, v.ID)
		}
	}
	results := make([]MigrationResult, len(ids))
	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, id string) {
			defer wg.Done()
			defer func() { <-sem }()
			r, err := o.Migrate(ctx, id, "")
			if err != nil { // once more, somewhere else
				log.Printf("drain %s: %s to %s failed, retrying: %v", node, id, r.To, err)
				r2, err2 := o.Migrate(ctx, id, "")
				r, err = r2, err2
			}
			if err != nil {
				r.Error = err.Error()
			}
			results[i] = r
		}(i, id)
	}
	wg.Wait()
	return results, nil
}

func (o *Orchestrator) migrateVM(w http.ResponseWriter, r *http.Request) {
	res, err := o.Migrate(r.Context(), r.PathValue("id"), r.URL.Query().Get("to"))
	if err != nil {
		res.Error = err.Error()
		writeJSON(w, http.StatusInternalServerError, res)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (o *Orchestrator) drainNode(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	res, err := o.Drain(r.Context(), r.PathValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"migrations": res, "wall_ms": float64(time.Since(started).Microseconds()) / 1000})
}

func hostOf(agentURL string) string {
	u, err := url.Parse(agentURL)
	if err != nil {
		return strings.TrimPrefix(agentURL, "http://")
	}
	return u.Hostname()
}
