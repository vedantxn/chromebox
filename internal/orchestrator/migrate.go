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
	"github.com/vedantxn/chromebox/internal/base"
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
	// Post-copy: the VM ran on the target while its memory arrived, for StreamMs.
	PostCopy  bool    `json:"post_copy"`
	StreamMs  float64 `json:"stream_ms,omitempty"`
	StreamMiB float64 `json:"stream_mib,omitempty"`
	// A fork's copy moving to a node without its image: the time to pull it
	// there first, while the VM still ran.
	ImagePullMs float64 `json:"image_pull_ms,omitempty"`
	Error       string  `json:"error,omitempty"`
}

// Migrate moves VM id to target ("" lets the scheduler choose). Two phases:
// the source agent moves the VM and leaves its own copy paused; then the
// orchestrator points the gateway at the target and tells the source to
// commit (destroy its copy). If anything fails before the route moves, the
// source is told to abort (resume) and the target's copy is deleted.
func (o *Orchestrator) Migrate(ctx context.Context, id, target string) (MigrationResult, error) {
	res, err := o.migrate(ctx, id, target)
	if err != nil {
		o.metrics.migrations.WithLabelValues("failed").Inc()
		o.metrics.errors.WithLabelValues("migrate").Inc()
		o.store.addEvent(id, "migration_failed", map[string]any{"from": res.From, "to": res.To, "error": err.Error()})
		return res, err
	}
	o.metrics.migrations.WithLabelValues("ok").Inc()
	o.metrics.migPause.Observe(res.PausedMs / 1000)
	if res.PostCopy {
		o.metrics.migStream.Observe(res.StreamMs / 1000)
	}
	o.store.addEvent(id, "migrated", map[string]any{"from": res.From, "to": res.To, "paused_ms": res.PausedMs,
		"unreachable_ms": res.UnreachableMs, "stream_ms": res.StreamMs, "stream_mib": res.StreamMiB, "post_copy": res.PostCopy})
	return res, nil
}

func (o *Orchestrator) migrate(ctx context.Context, id, target string) (MigrationResult, error) {
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
		// A fork's copy can go to any node holding the regular base its disk
		// descends from; the fork image itself is pulled there below.
		if target, err = Place(views, Request{MemMiB: vm.MemMiB, Base: o.diskBase(vm.Base)}, o.cfg.Strategy, vm.Node); err != nil {
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
	// The target needs the VM's base (for the disk it rebuilds and the
	// device state it expects). Regular bases are on every node; a fork image
	// is pulled from the node the copy runs on, while the VM still runs: the
	// pull is not part of the pause.
	if base.IsFork(vm.Base) && !o.holds(target, vm.Base) {
		t := time.Now()
		pr, err := dst.PullImage(ctx, vm.Base, src.URL)
		if err != nil {
			return res, fmt.Errorf("bring fork image %s to %s: %w", vm.Base, target, err)
		}
		res.ImagePullMs = float64(time.Since(t).Microseconds()) / 1000
		log.Printf("migrate %s: pulled %s to %s in %.0f ms (%.1f MiB disk, %.0f MiB memory)", id, vm.Base, target, res.ImagePullMs, pr.DiskMiB, pr.MemMiB)
	}
	if err := o.store.SetState(id, StateMigrating); err != nil {
		return res, err
	}
	restore := func() { _ = o.store.SetState(id, StateRunning) }

	mr, err := src.Migrate(ctx, id, api.MigrateRequest{TargetAgent: dst.URL, TargetSSH: "ec2-user@" + hostOf(dst.URL)})
	if err != nil {
		restore() // the source agent has already resumed the VM and cleaned the target
		return res, err
	}
	// The VM runs on the target. Move the network, then retire the source.
	answered := time.Now()
	if err := o.setRoute(ctx, vm.NetIndex, target); err != nil {
		_ = dst.DeleteVM(context.Background(), id)
		_ = src.Abort(context.Background(), id)
		restore()
		return res, fmt.Errorf("route flip failed, migration aborted: %w", err)
	}
	// From the source's pause to the route pointing at the target: the time no
	// packet for the VM could be delivered. The pause as the source timed it,
	// plus the route flip as timed here; never one machine's clock minus
	// another's (a node that just started can be off by hundreds of ms).
	unreachable := mr.PausedMs + float64(time.Since(answered).Microseconds())/1000
	if err := o.store.SetNode(id, target); err != nil {
		return res, err
	}
	restore()
	cr, err := src.Commit(ctx, id)
	if err != nil {
		log.Printf("migrate %s: commit on %s failed: %v", id, vm.Node, err)
	}
	res.PausedMs, res.UnreachableMs = mr.PausedMs, unreachable
	res.PostCopy, res.StreamMs, res.StreamMiB = mr.PostCopy, cr.StreamMs, cr.StreamMiB
	o.store.countMigration(id)
	res.DeltaMiB = float64(mr.DeltaBytes) / (1 << 20)
	return res, nil
}

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
