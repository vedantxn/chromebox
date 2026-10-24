package orchestrator

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/vedantxn/chromebox/internal/api"
)

// The warm pool: a few browsers per node that are already restored, settled
// and warmed up (see base.Warm), handed out whole on create. A browser fresh
// from a snapshot is cold: on these hosts its first connect took ~260 ms and
// its first load of example.com ~455 ms; warmed, ~75 ms and ~240 ms. Claiming
// one is a database update, with no node involved: its route is already set.
//
// Warm VMs belong to nobody until claimed, count toward a node's memory, and
// are the first thing to go when room is needed. They are left out of the
// autoscaler's sense of load, so they never keep a node running.
const (
	StateWarming = "warming" // created, being warmed up
	StateWarm    = "warm"    // ready to be claimed
)

func isWarm(state string) bool { return state == StateWarm || state == StateWarming }

// WarmConfig sizes the warm pool.
type WarmConfig struct {
	PerNode int           // warm browsers to keep on each open node (0: no pool)
	Base    string        // the base they are made from; creates of other bases are not served
	URL     string        // the page they load while warming up (ours, in a context thrown away after)
	MaxAge  time.Duration // a warm browser not claimed by then is replaced
}

// WarmFromEnv reads CHROMEBOX_WARM_PER_NODE (default 1) and CHROMEBOX_WARM_URL.
func WarmFromEnv() WarmConfig {
	c := WarmConfig{PerNode: 1, Base: "chrome-v1", URL: "https://batonvm.vercel.app/docs", MaxAge: 10 * time.Minute}
	if v, err := strconv.Atoi(os.Getenv("CHROMEBOX_WARM_PER_NODE")); err == nil && v >= 0 {
		c.PerNode = v
	}
	if v := os.Getenv("CHROMEBOX_WARM_URL"); v != "" {
		c.URL = v
	}
	return c
}

// claimWarm hands out a warm browser of base for a new session, if there is
// one on an open node: the least loaded such node first. Call with o.place
// held. onReserve runs as it would for a cold create; if it refuses (an
// account limit), the browser goes back to the pool.
func (o *Orchestrator) claimWarm(baseName string, ttl time.Duration, onReserve []func(id string) error) (api.VM, bool, error) {
	vms, err := o.store.ListVMs()
	if err != nil {
		return api.VM{}, false, err
	}
	views, err := o.views()
	if err != nil {
		return api.VM{}, false, err
	}
	open := map[string]int{} // node -> memory in use
	for _, v := range views {
		if v.Up && !v.Cordoned {
			open[v.Name] = v.UsedMemMiB
		}
	}
	var pick *api.VM
	for i, v := range vms {
		if _, ok := open[v.Node]; ok && v.State == StateWarm && v.Base == baseName {
			if pick == nil || open[v.Node] < open[pick.Node] {
				pick = &vms[i]
			}
		}
	}
	if pick == nil {
		return api.VM{}, false, nil
	}
	exp := time.Now().Add(ttl).UTC().Format(time.RFC3339Nano)
	if !o.store.casState(pick.ID, StateWarm, StateRunning, exp) {
		return api.VM{}, false, nil
	}
	if len(onReserve) > 0 {
		if err := onReserve[0](pick.ID); err != nil {
			o.store.casState(pick.ID, StateRunning, StateWarm, pick.Expires)
			return api.VM{}, false, err
		}
	}
	vm := *pick
	vm.State, vm.Expires = StateRunning, exp
	return vm, true, nil
}

// warmQuiet is how long a node must have been left alone before the pool
// starts a browser there. Starting and warming one costs CPU on a node with
// two; done while a session is being forked, paused or resumed there, it made
// those 10-100% slower (fork into 3: 2.6 -> 5.3 s).
const warmQuiet = 3 * time.Second

// touch records that an operation is working on these nodes (all nodes, if
// none are named).
func (o *Orchestrator) touch(nodes ...string) {
	o.opMu.Lock()
	defer o.opMu.Unlock()
	now := time.Now()
	if len(nodes) == 0 {
		o.mu.RLock()
		for n := range o.seen {
			o.lastOp[n] = now
		}
		o.mu.RUnlock()
		return
	}
	for _, n := range nodes {
		o.lastOp[n] = now
	}
}

func (o *Orchestrator) quietFor(node string) time.Duration {
	o.opMu.Lock()
	defer o.opMu.Unlock()
	return time.Since(o.lastOp[node])
}

// kickWarm asks the warm loop to top the pool up now.
func (o *Orchestrator) kickWarm() {
	select {
	case o.warmKick <- struct{}{}:
	default:
	}
}

// warmLoop keeps the pool at its size: it replaces warm browsers that are too
// old or on a node that is down or cordoned, and makes new ones, one at a time.
func (o *Orchestrator) warmLoop(ctx context.Context) {
	c := o.cfg.Warm
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	backoff := map[string]time.Time{} // node -> no new warm browser there before this
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-o.warmKick:
		}
		views, err := o.views()
		if err != nil {
			continue
		}
		open := map[string]NodeView{}
		for _, v := range views {
			if v.Up && !v.Cordoned {
				open[v.Name] = v
			}
		}
		vms, err := o.store.ListVMs()
		if err != nil {
			continue
		}
		have := map[string]int{}
		for _, v := range vms {
			if v.State == StateCreating || v.State == StateMigrating || v.State == StatePausing {
				o.touch(v.Node) // something is under way there: leave it alone
			}
			if !isWarm(v.State) {
				continue
			}
			_, ok := open[v.Node]
			stale := v.State == StateWarm && time.Now().After(v.ExpiresAt())
			stuck := v.State == StateWarming && time.Since(v.ExpiresAt().Add(-c.MaxAge)) > 2*time.Minute
			if !ok || stale || stuck {
				if err := o.Delete(ctx, v.ID); err != nil {
					log.Printf("warm pool: removing %s: %v", v.ID, err)
				}
				continue
			}
			have[v.Node]++
		}
		mem, ok := o.baseMem(c.Base)
		if !ok {
			continue
		}
		for name, v := range open {
			// Leave room for one more browser beside it: a warm one must never
			// be what stops a create of another base, or a fork, from fitting.
			if have[name] >= c.PerNode || v.CapMemMiB-v.UsedMemMiB < 2*mem || v.Bases[c.Base] == "" || time.Now().Before(backoff[name]) ||
				o.quietFor(name) < warmQuiet {
				continue
			}
			if !o.makeWarm(ctx, name) {
				backoff[name] = time.Now().Add(time.Minute) // don't churn on a node that can't
			}
			break // one at a time; the next tick does the next
		}
	}
}

// makeWarm starts a browser on node and warms it up. It reports whether a
// warm browser is ready.
func (o *Orchestrator) makeWarm(ctx context.Context, node string) bool {
	c := o.cfg.Warm
	started := time.Now()
	vm, _, err := o.createCold(ctx, c.Base, c.MaxAge, node, StateWarming)
	if err != nil {
		log.Printf("warm pool: starting a browser on %s: %v", node, err)
		return false
	}
	ag, err := o.agentFor(node)
	if err == nil {
		err = ag.Warm(ctx, vm.ID, c.URL)
	}
	if err != nil || !o.store.casState(vm.ID, StateWarming, StateWarm, vm.Expires) {
		log.Printf("warm pool: warming %s on %s: %v", vm.ID, node, err)
		_ = o.Delete(context.Background(), vm.ID)
		return false
	}
	o.metrics.warmReady.Observe(secs(time.Since(started)))
	return true
}

// evictWarm deletes one warm browser to make room, if there is one. Call with
// o.place held.
func (o *Orchestrator) evictWarm(ctx context.Context) bool {
	vms, err := o.store.ListVMs()
	if err != nil {
		return false
	}
	for _, v := range vms {
		if v.State == StateWarm {
			return o.Delete(ctx, v.ID) == nil
		}
	}
	return false
}
