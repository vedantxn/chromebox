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
		o.delRoute(ctx, v.NetIndex)
		n := o.proxy.CloseVM(v.NetIndex)
		log.Printf("node %s is down: %s lost, %d client connection(s) closed", v.Node, v.ID, n)
	}
}

func (o *Orchestrator) reassertRoutes(ctx context.Context) {
	vms, err := o.store.ListVMs()
	if err != nil {
		return
	}
	for _, v := range vms {
		if v.State == StateRunning {
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
		case v.State == StateMigrating || v.State == StateCreating:
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
