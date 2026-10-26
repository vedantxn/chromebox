package agent

import (
	"reflect"
	"testing"
	"time"

	"github.com/vedantxn/chromebox/internal/base"
)

func TestIdleBasesUnloadsOnlyOldUnusedRegularBases(t *testing.T) {
	keep := func(n string) bool { return n == "chrome-v2" }
	t0 := time.Date(2026, 10, 26, 13, 0, 0, 0, time.UTC)
	last := map[string]time.Time{}
	loaded := []string{"chrome-v1", "chrome-v2", "chrome-v0", "fork-p-abc"}
	used := map[string]bool{"chrome-v0": true}

	// First sight: nothing goes, the grace starts now.
	if got := idleBases(loaded, keep, used, last, t0); got != nil {
		t.Fatalf("first sweep unloaded %v", got)
	}
	// Within the grace: still nothing.
	if got := idleBases(loaded, keep, used, last, t0.Add(baseIdleGrace-time.Second)); got != nil {
		t.Fatalf("unloaded %v within the grace", got)
	}
	// After it: only chrome-v1. chrome-v2 is kept, chrome-v0 has a VM, fork images are the orchestrator's.
	if got := idleBases(loaded, keep, used, last, t0.Add(baseIdleGrace)); !reflect.DeepEqual(got, []string{"chrome-v1"}) {
		t.Fatalf("got %v, want [chrome-v1]", got)
	}
	// A base in use keeps resetting its clock (a sweep runs every minute): once
	// its VM is gone, the grace counts from the last sweep that saw it.
	t1 := t0.Add(3 * baseIdleGrace)
	idleBases(loaded, keep, used, last, t1)
	if got := idleBases(loaded, keep, map[string]bool{}, last, t1.Add(baseSweepEvery)); !reflect.DeepEqual(got, []string{"chrome-v1"}) {
		t.Fatalf("got %v right after chrome-v0's VM ended", got)
	}
	if got := idleBases(loaded, keep, map[string]bool{}, last, t1.Add(baseIdleGrace)); !reflect.DeepEqual(got, []string{"chrome-v1", "chrome-v0"}) {
		t.Fatalf("got %v, want chrome-v0 too once idle for the grace", got)
	}
}

func TestKeepsLoadedTheDefaultAndThePoolBase(t *testing.T) {
	a := &Agent{pool: &pool{base: "chrome-x"}}
	for name, want := range map[string]bool{base.Default: true, "chrome-x": true, "chrome-v1": false} {
		if a.keepsLoaded(name) != want {
			t.Errorf("keepsLoaded(%s) = %v", name, !want)
		}
	}
	if (&Agent{}).keepsLoaded("chrome-x") {
		t.Error("no pool: only the default is kept")
	}
}

func TestUnloadRefusesForkImagesAndBasesWithNoDiskCopy(t *testing.T) {
	if err := base.Unload("fork-p-abc"); err == nil {
		t.Error("unloaded a fork image")
	}
	if err := base.Unload("no-such-base-7f3a"); err == nil {
		t.Error("unloaded a base with no copy on disk")
	}
}
