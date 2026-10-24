package orchestrator

import (
	"context"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/vedantxn/chromebox/internal/api"
)

// The autoscaler starts a node when capacity runs low and, when load drops,
// drains a node with live migration (no client disconnects) and stops it.
// Nodes are EC2 instances; the gateway's instance role lets the orchestrator
// start and stop the ones tagged Project=chromebox, and nothing else.
//
// Rules, in order of priority (decide, below, is pure so it can be tested):
//  1. Scale up: the nodes that are up and open have room for fewer than
//     Headroom more VMs (or a create was just refused for capacity): start a
//     stopped node, if fewer than MaxNodes are up.
//  2. Consolidate: a managed node whose VMs would all fit on the other open
//     nodes, with Headroom VMs of room to spare, is cordoned and drained.
//  3. Stop: a cordoned managed node that has had no VMs for IdleStop is stopped.
//
// One action at a time, with Cooldown between them, so a burst of creates
// cannot start every node and a lull cannot flap one up and down. The
// always-on node is never drained or stopped.
type AutoscaleConfig struct {
	Enabled   bool
	AlwaysOn  string        // never drained or stopped (default node-a)
	MaxNodes  int           // nodes up at once, including AlwaysOn (default 3)
	Headroom  int           // VMs of spare room to keep (default 1)
	Cooldown  time.Duration // between actions (default 2m)
	IdleStop  time.Duration // an empty drained node is stopped after this (default 5m)
	StartWait time.Duration // a started node must heartbeat within this (default 4m)
}

// AutoscaleFromEnv reads CHROMEBOX_AUTOSCALE (1 to enable) and the optional
// CHROMEBOX_ALWAYS_ON, _MAX_NODES, _HEADROOM, _AUTOSCALE_COOLDOWN, _IDLE_STOP.
func AutoscaleFromEnv() AutoscaleConfig {
	c := AutoscaleConfig{Enabled: os.Getenv("CHROMEBOX_AUTOSCALE") == "1", AlwaysOn: "node-a", MaxNodes: 3, Headroom: 1,
		Cooldown: 2 * time.Minute, IdleStop: 5 * time.Minute, StartWait: 4 * time.Minute}
	if v := os.Getenv("CHROMEBOX_ALWAYS_ON"); v != "" {
		c.AlwaysOn = v
	}
	if n, err := strconv.Atoi(os.Getenv("CHROMEBOX_MAX_NODES")); err == nil && n > 0 {
		c.MaxNodes = n
	}
	if n, err := strconv.Atoi(os.Getenv("CHROMEBOX_HEADROOM")); err == nil && n >= 0 {
		c.Headroom = n
	}
	if d, err := time.ParseDuration(os.Getenv("CHROMEBOX_AUTOSCALE_COOLDOWN")); err == nil {
		c.Cooldown = d
	}
	if d, err := time.ParseDuration(os.Getenv("CHROMEBOX_IDLE_STOP")); err == nil {
		c.IdleStop = d
	}
	return c
}

// Instances starts and stops the machines behind nodes.
type Instances interface {
	Start(ctx context.Context, node string) error
	Stop(ctx context.Context, node string) error
}

// asNode is what decide knows about a node.
type asNode struct {
	Name     string
	Up       bool
	Cordoned bool
	FreeMiB  int
	UsedMiB  int
	VMs      int           // VMs the database places on it (any state but lost)
	State    string        // "" or starting, draining, stopping: a transition in flight
	EmptyFor time.Duration // how long it has had no VMs
}

// Action is something the autoscaler did, kept for /v1/stats and the status page.
type Action struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"` // start, drain, stop, node_up, start_timeout, drain_failed
	Node   string    `json:"node"`
	Reason string    `json:"reason"`
}

type decision struct{ Kind, Node, Reason string }

// decide picks the next action, if any. vmMiB is one VM's memory.
func decide(nodes []asNode, vmMiB int, c AutoscaleConfig, pressure bool, sinceLast time.Duration) decision {
	for _, n := range nodes {
		if n.State != "" {
			return decision{} // a transition is in flight: wait for it
		}
	}
	slots, up := 0, 0
	for _, n := range nodes {
		if n.Up {
			up++
			if !n.Cordoned {
				slots += max(n.FreeMiB, 0) / vmMiB
			}
		}
	}
	// Stopping an empty drained node needs no cooldown after the drain: it has
	// already waited IdleStop.
	for _, n := range nodes {
		if n.Name != c.AlwaysOn && n.Up && n.Cordoned && n.VMs == 0 && n.EmptyFor >= c.IdleStop && !pressure {
			return decision{"stop", n.Name, "no VMs for " + n.EmptyFor.Round(time.Second).String()}
		}
	}
	if sinceLast < c.Cooldown {
		return decision{}
	}
	if (pressure || slots < c.Headroom) && up < c.MaxNodes {
		var stopped []string
		for _, n := range nodes {
			if !n.Up {
				stopped = append(stopped, n.Name)
			}
		}
		sort.Strings(stopped)
		if len(stopped) > 0 {
			why := "room for " + strconv.Itoa(slots) + " more VMs"
			if pressure {
				why = "a create was refused for capacity"
			}
			return decision{"start", stopped[0], why}
		}
	}
	if pressure {
		return decision{}
	}
	// Consolidate: the managed node with the fewest VMs whose VMs fit elsewhere.
	var best *asNode
	for i := range nodes {
		n := &nodes[i]
		if n.Name == c.AlwaysOn || !n.Up || n.Cordoned {
			continue
		}
		others := 0
		for _, o := range nodes {
			if o.Name != n.Name && o.Up && !o.Cordoned {
				others += max(o.FreeMiB, 0)
			}
		}
		if n.UsedMiB+c.Headroom*vmMiB <= others && (best == nil || n.VMs < best.VMs) {
			best = n
		}
	}
	if best != nil {
		return decision{"drain", best.Name, strconv.Itoa(best.VMs) + " VMs fit on the other nodes"}
	}
	// A node we cordoned that no longer fits elsewhere (load came back): reopen it.
	for _, n := range nodes {
		if n.Name != c.AlwaysOn && n.Up && n.Cordoned && n.VMs > 0 {
			others := 0
			for _, o := range nodes {
				if o.Name != n.Name && o.Up && !o.Cordoned {
					others += max(o.FreeMiB, 0)
				}
			}
			if n.UsedMiB > others {
				return decision{"reopen", n.Name, "its VMs no longer fit elsewhere"}
			}
		}
	}
	return decision{}
}

type autoscaler struct {
	o   *Orchestrator
	cfg AutoscaleConfig
	ec2 Instances

	mu       sync.Mutex
	state    map[string]string    // node -> starting, draining, stopping
	since    map[string]time.Time // when that began
	emptyAt  map[string]time.Time // node -> since when it has had no VMs
	last     time.Time            // last action
	pressure bool
	actions  []Action
}

func newAutoscaler(o *Orchestrator, cfg AutoscaleConfig, inst Instances) *autoscaler {
	return &autoscaler{o: o, cfg: cfg, ec2: inst, state: map[string]string{}, since: map[string]time.Time{},
		emptyAt: map[string]time.Time{}, last: time.Now().Add(-cfg.Cooldown)}
}

// Pressure says a create was refused for lack of capacity: the next tick starts
// a node if it can.
func (a *autoscaler) Pressure() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.pressure = true
	a.mu.Unlock()
}

// Booting reports whether a node is starting, and how long it has been.
func (a *autoscaler) Booting() (time.Duration, bool) {
	if a == nil {
		return 0, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for n, s := range a.state {
		if s == "starting" {
			return time.Since(a.since[n]), true
		}
	}
	return 0, false
}

// NodeState is the transition a node is in ("" if none).
func (a *autoscaler) NodeState(node string) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state[node]
}

// Actions returns the most recent actions, newest last.
func (a *autoscaler) Actions() []Action {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Action(nil), a.actions...)
}

func (a *autoscaler) record(kind, node, reason string) {
	a.actions = append(a.actions, Action{At: time.Now().UTC(), Kind: kind, Node: node, Reason: reason})
	if len(a.actions) > 50 {
		a.actions = a.actions[len(a.actions)-50:]
	}
	a.o.metrics.autoscale.WithLabelValues(kind).Inc()
	log.Printf("autoscale: %s %s (%s)", kind, node, reason)
}

func (a *autoscaler) run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if time.Since(a.o.started) < a.o.cfg.HeartbeatTimeout+3*time.Second {
			continue // nobody has reported yet
		}
		a.tick(ctx)
	}
}

func (a *autoscaler) tick(ctx context.Context) {
	views, err := a.o.views()
	if err != nil {
		return
	}
	vms, _ := a.o.store.ListVMs()
	count := map[string]int{}
	warmMiB := map[string]int{} // warm browsers hold memory but are not load: they make room when needed
	for _, v := range vms {
		switch {
		case isWarm(v.State):
			warmMiB[v.Node] += v.MemMiB
		case v.State != StateLost && v.State != StatePaused:
			count[v.Node]++
		}
	}

	a.mu.Lock()
	var nodes []asNode
	for _, v := range views {
		// Idle time counts only while the node is up, from when it last became
		// empty or came up, whichever is later: a node that was stopped, or
		// only just started, has not been idle yet.
		if v.Up && count[v.Name] == 0 {
			if _, ok := a.emptyAt[v.Name]; !ok {
				a.emptyAt[v.Name] = time.Now()
			}
		} else {
			delete(a.emptyAt, v.Name)
		}
		used := v.UsedMemMiB - warmMiB[v.Name]
		n := asNode{Name: v.Name, Up: v.Up, Cordoned: v.Cordoned, FreeMiB: v.CapMemMiB - used, UsedMiB: used,
			VMs: count[v.Name], State: a.state[v.Name]}
		if t, ok := a.emptyAt[v.Name]; ok {
			n.EmptyFor = time.Since(t)
		}
		// Settle transitions that have finished.
		switch n.State {
		case "starting":
			if v.Up {
				a.record("node_up", v.Name, "took "+time.Since(a.since[v.Name]).Round(time.Second).String()+" to heartbeat")
				delete(a.state, v.Name)
				n.State = ""
			} else if time.Since(a.since[v.Name]) > a.cfg.StartWait {
				a.record("start_timeout", v.Name, "no heartbeat after "+a.cfg.StartWait.String())
				delete(a.state, v.Name)
				n.State = ""
			}
		case "stopping":
			if !v.Up {
				delete(a.state, v.Name)
				n.State = ""
			}
		}
		nodes = append(nodes, n)
	}
	pressure := a.pressure
	a.pressure = false
	d := decide(nodes, vmMemMiB(vms), a.cfg, pressure, time.Since(a.last))
	if d.Kind != "" {
		a.last = time.Now()
		a.record(d.Kind, d.Node, d.Reason)
		switch d.Kind {
		case "start":
			a.state[d.Node], a.since[d.Node] = "starting", time.Now()
		case "drain":
			a.state[d.Node], a.since[d.Node] = "draining", time.Now()
		case "stop":
			a.state[d.Node], a.since[d.Node] = "stopping", time.Now()
		}
	}
	a.mu.Unlock()

	switch d.Kind {
	case "start":
		_ = a.o.store.SetCordoned(d.Node, false)
		if err := a.ec2.Start(ctx, d.Node); err != nil {
			a.fail(d.Node, "start failed: "+err.Error())
		}
	case "drain":
		go func() {
			if _, err := a.o.Drain(context.Background(), d.Node); err != nil {
				a.mu.Lock()
				a.record("drain_failed", d.Node, err.Error())
				a.mu.Unlock()
			}
			a.mu.Lock()
			delete(a.state, d.Node)
			a.mu.Unlock()
		}()
	case "stop":
		if err := a.ec2.Stop(ctx, d.Node); err != nil {
			a.fail(d.Node, "stop failed: "+err.Error())
		}
	case "reopen":
		_ = a.o.store.SetCordoned(d.Node, false)
	}
}

func (a *autoscaler) fail(node, why string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.state, node)
	a.record("error", node, why)
	// Try again soon, not after a whole cooldown: the usual cause is a node
	// that was still stopping when load came back.
	a.last = time.Now().Add(-a.cfg.Cooldown + 10*time.Second)
}

// vmMemMiB is the memory of one VM: what the VMs there are say, or the
// browser image's 1 GiB.
func vmMemMiB(vms []api.VM) int {
	for _, v := range vms {
		if v.MemMiB > 0 {
			return v.MemMiB
		}
	}
	return 1024
}

// retryAfter tells a client that was refused for capacity when to try again:
// when capacity is being added, or can be (a node is booting, or one is stopped
// and the node limit is not reached), in a few seconds; the SDK waits and asks
// again, and the new machine is up in about a minute. Otherwise the client is
// simply refused: nothing is coming.
func (o *Orchestrator) retryAfter(w http.ResponseWriter) {
	if o.auto == nil {
		return
	}
	if age, ok := o.auto.Booting(); ok {
		wait := max(60*time.Second-age, 10*time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())))
		return
	}
	views, err := o.views()
	if err != nil {
		return
	}
	up, stopped := 0, 0
	for _, v := range views {
		if v.Up {
			up++
		} else {
			stopped++
		}
	}
	if stopped > 0 && up < o.cfg.Autoscale.MaxNodes {
		w.Header().Set("Retry-After", "20")
	}
}
