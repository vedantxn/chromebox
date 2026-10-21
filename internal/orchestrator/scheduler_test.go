package orchestrator

import (
	"strings"
	"testing"
)

func nodes() []NodeView {
	b := map[string]string{"chrome": "aaa"}
	return []NodeView{
		{Name: "node-a", Up: true, CapMemMiB: 5000, UsedMemMiB: 3000, Bases: b},
		{Name: "node-b", Up: true, CapMemMiB: 5000, UsedMemMiB: 1000, Bases: b},
		{Name: "node-c", Up: true, CapMemMiB: 5000, UsedMemMiB: 2000, Bases: b},
	}
}

func TestSpreadPicksTheEmptiestNode(t *testing.T) {
	got, err := Place(nodes(), Request{MemMiB: 1024, Base: "chrome"}, Spread)
	if err != nil || got != "node-b" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestPackPicksTheFullestNodeThatFits(t *testing.T) {
	got, _ := Place(nodes(), Request{MemMiB: 1024, Base: "chrome"}, Pack)
	if got != "node-a" { // 2000 MiB free is enough for 1024
		t.Fatalf("got %q", got)
	}
	// With 2500 MiB needed, node-a (2000 free) no longer fits.
	got, _ = Place(nodes(), Request{MemMiB: 2500, Base: "chrome"}, Pack)
	if got != "node-c" {
		t.Fatalf("got %q", got)
	}
}

func TestSkipsNodesThatCannotTakeIt(t *testing.T) {
	ns := nodes()
	ns[1].Up = false      // node-b is down
	ns[2].Cordoned = true // node-c is being drained
	got, err := Place(ns, Request{MemMiB: 1024, Base: "chrome"}, Spread)
	if err != nil || got != "node-a" {
		t.Fatalf("got %q, %v", got, err)
	}
	ns[0].UsedMemMiB = 4500 // and node-a is now too full
	_, err = Place(ns, Request{MemMiB: 1024, Base: "chrome"}, Spread)
	if err == nil || !strings.Contains(err.Error(), "node-b down") || !strings.Contains(err.Error(), "node-c cordoned") {
		t.Fatalf("error should say why: %v", err)
	}
}

func TestExcludeAndBaseVersion(t *testing.T) {
	got, _ := Place(nodes(), Request{MemMiB: 1024, Base: "chrome"}, Spread, "node-b")
	if got != "node-c" {
		t.Fatalf("exclusion ignored: %q", got)
	}
	ns := nodes()
	ns[1].Bases = map[string]string{"chrome": "stale"} // node-b holds an old version
	got, _ = Place(ns, Request{MemMiB: 1024, Base: "chrome", BaseSHA: "aaa"}, Spread)
	if got != "node-c" {
		t.Fatalf("stale base accepted: %q", got)
	}
	if _, err := Place(ns, Request{MemMiB: 1024, Base: "firefox"}, Spread); err == nil {
		t.Fatal("a base nobody holds should not be placeable")
	}
}

func TestTiesAreBrokenByName(t *testing.T) {
	ns := nodes()
	ns[0].UsedMemMiB, ns[1].UsedMemMiB, ns[2].UsedMemMiB = 1000, 1000, 1000
	for i := 0; i < 20; i++ {
		if got, _ := Place(ns, Request{MemMiB: 100, Base: "chrome"}, Spread); got != "node-a" {
			t.Fatalf("not deterministic: %q", got)
		}
	}
}
