package orchestrator

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/vedantxn/chromebox/internal/api"
)

// reconcileLoop compares what the database says should run with what the
// agents report, once a second.
func (o *Orchestrator) reconcileLoop(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	rerouted := false
	lastPush := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		// Right after a restart nobody has reported yet: give every node one
		// heartbeat timeout before calling it down.
		if time.Since(o.started) < o.cfg.HeartbeatTimeout+time.Second {
			continue
		}
		if !rerouted { // routes live in the kernel and survive a restart, but make sure
			o.reassertRoutes(ctx)
			rerouted = true
		}
		o.markLost(ctx)
		o.expire(ctx)
		o.expirePaused(ctx)
		o.collectForks(ctx)
		if time.Since(lastPush) >= 30*time.Second { // a restarted CDP proxy gets the settings back
			lastPush = time.Now()
			o.reapplyOptions(ctx)
		}
	}
}

// expire deletes VMs past their lifetime. Nobody may keep a stranger's browser
// running forever by forgetting to delete it.
func (o *Orchestrator) expire(ctx context.Context) {
	vms, err := o.store.ListVMs()
	if err != nil {
		return
	}
	for _, v := range vms {
		if e := v.ExpiresAt(); v.State == StateRunning && !e.IsZero() && time.Now().After(e) {
			log.Printf("%s reached its lifetime, deleting", v.ID)
			o.store.endSessionRecord(v.ID, "lifetime reached")
			if err := o.Delete(ctx, v.ID); err != nil {
				log.Printf("expire %s: %v", v.ID, err)
			}
		}
	}
}

// markLost handles nodes that have gone quiet. Their VMs cannot be saved (their
// memory was only on that node), so they are marked lost, their route is
// removed and the proxy closes their clients' connections: an agent learns at
// once instead of waiting on a connection that will never answer.
func (o *Orchestrator) markLost(ctx context.Context) {
	vms, err := o.store.ListVMs()
	if err != nil {
		return
	}
	for _, v := range vms {
		if v.State != StateRunning || o.up(v.Node) {
			continue
		}
		_ = o.store.SetState(v.ID, StateLost)
		o.metrics.errors.WithLabelValues("vm_lost_node").Inc()
		o.store.endSessionRecord(v.ID, "node lost")
		o.dropOptions(v)
		o.delRoute(ctx, v.NetIndex)
		n := o.closeConns(v.NetIndex)
		log.Printf("node %s is down: %s lost, %d client connection(s) closed", v.Node, v.ID, n)
	}
}

func (o *Orchestrator) reassertRoutes(ctx context.Context) {
	vms, err := o.store.ListVMs()
	if err != nil {
		return
	}
	for _, v := range vms {
		if v.State == StateRunning || isWarm(v.State) {
			_ = o.setRoute(ctx, v.NetIndex, v.Node)
		}
	}
}

// fence runs on every heartbeat: any VM a node is running that the database
// does not place on that node is killed. That covers a node coming back after
// being declared down (its VMs were marked lost and may have been replaced),
// and leftovers of failed operations. Without it the same VM, with the same
// address, could run twice.
func (o *Orchestrator) fence(st api.Status) {
	for _, info := range st.VMs {
		if strings.HasPrefix(info.ID, "base-") { // a base being built
			continue
		}
		v, err := o.store.GetVM(info.ID)
		switch {
		case err != nil: // unknown to the database
		case v.State == StateMigrating || v.State == StateCreating || v.State == StatePausing:
			continue // in flight: the operation decides
		case v.State == StateLost || v.Node != st.Node:
		default:
			continue
		}
		go func(id string) {
			ag, err := o.agentFor(st.Node)
			if err != nil {
				return
			}
			if err := ag.DeleteVM(context.Background(), id); err == nil {
				log.Printf("fenced %s on %s: the database does not place it there", id, st.Node)
			}
		}(info.ID)
	}
}

// (allowed is no longer used by the proxy, which now trusts signed tokens; kept
// for the operator-only /v/<n> listener if it is ever wired back.)
// allowed tells the proxy whether VM index n is running, so that a client asking
// for a lost or unknown VM gets an immediate 404 instead of a dial that hangs.
func (o *Orchestrator) allowed(n int) bool {
	vms, err := o.store.ListVMs()
	if err != nil {
		return true // don't turn a database hiccup into an outage
	}
	for _, v := range vms {
		if v.NetIndex == n {
			return v.State == StateRunning || v.State == StateMigrating
		}
	}
	return false
}

// vanishGrace is how long a running VM may be missing from its node's
// heartbeats before it is declared lost (a heartbeat taken just before a
// create finished may not list it yet).
const vanishGrace = 3 * time.Second

// vanished handles VMs that the database places on a node which is up but no
// longer runs them. Since M5 this happens by design: after a post-copy
// migration, if the target can never get the rest of its memory (the source
// died mid-stream), its page-fault handler kills the VM instead of leaving it
// frozen on a page nobody will serve. A crashed Firecracker looks the same.
// Either way it is a loss like a node's: route removed, clients told at once.
func (o *Orchestrator) vanished(st api.Status) {
	reported := make(map[string]bool, len(st.VMs))
	for _, v := range st.VMs {
		reported[v.ID] = true
	}
	vms, err := o.store.ListVMs()
	if err != nil {
		return
	}
	o.goneMu.Lock()
	defer o.goneMu.Unlock()
	for _, v := range vms {
		if v.Node != st.Node {
			continue
		}
		if (v.State != StateRunning && v.State != StateWarm) || reported[v.ID] {
			delete(o.gone, v.ID)
			continue
		}
		first, ok := o.gone[v.ID]
		if !ok {
			o.gone[v.ID] = time.Now()
			continue
		}
		if time.Since(first) < vanishGrace {
			continue
		}
		delete(o.gone, v.ID)
		if v.State == StateWarm { // nobody's yet: just make sure it is never handed out
			log.Printf("warm browser %s no longer runs on %s: removing it", v.ID, v.Node)
			go o.Delete(context.Background(), v.ID)
			continue
		}
		_ = o.store.SetState(v.ID, StateLost)
		o.metrics.errors.WithLabelValues("vm_vanished").Inc()
		o.store.endSessionRecord(v.ID, "browser stopped")
		o.dropOptions(v)
		o.delRoute(context.Background(), v.NetIndex)
		n := o.closeConns(v.NetIndex)
		log.Printf("%s no longer runs on %s: lost, %d client connection(s) closed", v.ID, v.Node, n)
	}
}
