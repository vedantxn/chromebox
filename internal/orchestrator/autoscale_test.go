package orchestrator

import (
	"testing"
	"time"
)

const vm = 1024

var cfg = AutoscaleConfig{Enabled: true, AlwaysOn: []string{"node-a"}, MaxNodes: 3, Headroom: 1,
	Cooldown: 2 * time.Minute, IdleStop: 5 * time.Minute}

func node(name string, up bool, used, free, vms int) asNode {
	return asNode{Name: name, Up: up, UsedMiB: used * vm, FreeMiB: free * vm, VMs: vms}
}

func TestScaleUpWhenNoRoomIsLeft(t *testing.T) {
	nodes := []asNode{node("node-a", true, 4, 0, 4), node("node-b", false, 0, 0, 0), node("node-c", false, 0, 0, 0)}
	d := decide(nodes, vm, cfg, false, time.Hour)
	if d.Kind != "start" || d.Node != "node-b" {
		t.Fatalf("got %+v, want to start node-b", d)
	}
}

func TestNoScaleUpWithRoomToSpare(t *testing.T) {
	nodes := []asNode{node("node-a", true, 3, 1, 3), node("node-b", false, 0, 0, 0)}
	if d := decide(nodes, vm, cfg, false, time.Hour); d.Kind == "start" {
		t.Fatalf("started a node with a slot free: %+v", d)
	}
}

func TestPressureStartsANodeEvenWithRoom(t *testing.T) {
	nodes := []asNode{node("node-a", true, 2, 2, 2), node("node-b", false, 0, 0, 0)}
	if d := decide(nodes, vm, cfg, true, time.Hour); d.Kind != "start" {
		t.Fatalf("a refused create did not start a node: %+v", d)
	}
}

func TestCooldownHoldsAllButStopping(t *testing.T) {
	full := []asNode{node("node-a", true, 4, 0, 4), node("node-b", false, 0, 0, 0)}
	if d := decide(full, vm, cfg, false, 30*time.Second); d.Kind != "" {
		t.Fatalf("acted inside the cooldown: %+v", d)
	}
}

func TestNeverMoreThanMaxNodes(t *testing.T) {
	c := cfg
	c.MaxNodes = 2
	nodes := []asNode{node("node-a", true, 4, 0, 4), node("node-b", true, 4, 0, 4), node("node-c", false, 0, 0, 0)}
	if d := decide(nodes, vm, c, false, time.Hour); d.Kind == "start" {
		t.Fatalf("started a third node with MaxNodes 2: %+v", d)
	}
}

func TestOneTransitionAtATime(t *testing.T) {
	nodes := []asNode{node("node-a", true, 4, 0, 4), {Name: "node-b", State: "starting"}, node("node-c", false, 0, 0, 0)}
	if d := decide(nodes, vm, cfg, true, time.Hour); d.Kind != "" {
		t.Fatalf("started another node while one was booting: %+v", d)
	}
}

func TestConsolidatesWhenVMsFitElsewhere(t *testing.T) {
	// node-a has 3 free slots; node-b's 2 VMs fit, and one slot of headroom remains.
	nodes := []asNode{node("node-a", true, 1, 3, 1), node("node-b", true, 2, 2, 2)}
	d := decide(nodes, vm, cfg, false, time.Hour)
	if d.Kind != "drain" || d.Node != "node-b" {
		t.Fatalf("got %+v, want to drain node-b", d)
	}
}

func TestDoesNotConsolidateWithoutHeadroom(t *testing.T) {
	// node-b's 1 VM would fit on node-a's single free slot, but that leaves no spare room.
	nodes := []asNode{node("node-a", true, 3, 1, 3), node("node-b", true, 1, 3, 1)}
	if d := decide(nodes, vm, cfg, false, time.Hour); d.Kind == "drain" {
		t.Fatalf("drained a node, leaving no headroom: %+v", d)
	}
}

func TestNeverDrainsTheAlwaysOnNode(t *testing.T) {
	nodes := []asNode{node("node-a", true, 1, 3, 1), node("node-b", true, 0, 4, 0)}
	if d := decide(nodes, vm, cfg, false, time.Hour); d.Node == "node-a" {
		t.Fatalf("touched the always-on node: %+v", d)
	}
}

func TestStopsAnEmptyDrainedNodeAfterTheIdleTime(t *testing.T) {
	b := node("node-b", true, 0, 4, 0)
	b.Cordoned, b.EmptyFor = true, 4*time.Minute
	nodes := []asNode{node("node-a", true, 1, 3, 1), b}
	if d := decide(nodes, vm, cfg, false, time.Hour); d.Kind == "stop" {
		t.Fatalf("stopped after 4 min of a 5 min idle time: %+v", d)
	}
	nodes[1].EmptyFor = 6 * time.Minute
	if d := decide(nodes, vm, cfg, false, 0); d.Kind != "stop" || d.Node != "node-b" {
		t.Fatalf("got %+v, want to stop node-b", d)
	}
}

func TestReopensANodeWhenLoadComesBack(t *testing.T) {
	// node-b was cordoned for draining, but node-a filled up again and its 3 VMs have nowhere to go.
	b := node("node-b", true, 3, 1, 3)
	b.Cordoned = true
	nodes := []asNode{node("node-a", true, 4, 0, 4), b}
	if d := decide(nodes, vm, cfg, false, time.Hour); d.Kind != "reopen" || d.Node != "node-b" {
		t.Fatalf("got %+v, want to reopen node-b", d)
	}
}

func TestKeepsEveryAlwaysOnNodeRunning(t *testing.T) {
	two := cfg
	two.AlwaysOn = []string{"node-a", "node-b"}
	// node-b is stopped: it is started even with no load at all.
	nodes := []asNode{node("node-a", true, 0, 4, 0), node("node-b", false, 0, 0, 0)}
	if d := decide(nodes, vm, two, false, time.Hour); d.Kind != "start" || d.Node != "node-b" {
		t.Fatalf("got %+v, want to start node-b", d)
	}
	// Running and empty, its VMs fitting elsewhere: never drained or stopped.
	nodes = []asNode{node("node-a", true, 0, 4, 0), node("node-b", true, 0, 4, 0)}
	if d := decide(nodes, vm, two, false, time.Hour); d.Node == "node-b" || d.Node == "node-a" {
		t.Fatalf("touched an always-on node: %+v", d)
	}
}
