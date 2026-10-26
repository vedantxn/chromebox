package agent

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/vedantxn/chromebox/internal/base"
)

// A regular base in RAM holds its whole memory (a GiB) in the same tmpfs that
// snapshots, resumed sessions and profile images need. Once new browsers are
// made from another base (base.Default moved on), the old one would sit there
// for good, and the tmpfs fills: on 2026-10-26 a profile save failed with "no
// space left on device" with two bases loaded. So the agent unloads a base no
// VM on this node was made from and none has been for baseIdleGrace; Load
// brings it back from disk if a VM is made from it again.
const (
	baseIdleGrace  = 10 * time.Minute
	baseSweepEvery = time.Minute
)

// keepsLoaded says whether new VMs are made from base name here, so it stays
// in RAM, and its memory is reserved even before it is loaded.
func (a *Agent) keepsLoaded(name string) bool {
	return name == base.Default || (a.pool != nil && name == a.pool.base)
}

// usedBase notes that a VM is being made from base name. Call with loadMu held.
func (a *Agent) usedBase(name string) { a.baseUsed[name] = time.Now() }

func (a *Agent) unloadIdleBases(ctx context.Context) {
	t := time.NewTicker(baseSweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.sweepBases(time.Now())
		}
	}
}

func (a *Agent) sweepBases(now time.Time) {
	used := map[string]bool{}
	for _, v := range listVMs() {
		used[v.Base] = true
	}
	var loaded []string
	entries, _ := os.ReadDir(base.ShmDir)
	for _, e := range entries {
		if base.Loaded(e.Name()) {
			loaded = append(loaded, e.Name())
		}
	}
	a.loadMu.Lock() // no create loads a base while one is unloaded
	defer a.loadMu.Unlock()
	for _, name := range idleBases(loaded, a.keepsLoaded, used, a.baseUsed, now) {
		if err := base.Unload(name); err != nil {
			log.Printf("base %s: %v", name, err)
			continue
		}
		delete(a.baseUsed, name)
		log.Printf("base %s: no VM made from it for %s, unloaded from RAM", name, baseIdleGrace)
	}
}

// idleBases picks the loaded bases to unload: regular bases that are not kept,
// that no VM uses, and that have not been used for baseIdleGrace. It records
// now as the last use of a base in use, and of one it sees for the first time
// (after the agent starts), so that the grace counts from then.
func idleBases(loaded []string, keep func(string) bool, used map[string]bool, lastUsed map[string]time.Time, now time.Time) []string {
	var out []string
	for _, name := range loaded {
		if base.IsFork(name) || keep(name) {
			continue
		}
		last, seen := lastUsed[name]
		if used[name] || !seen {
			lastUsed[name] = now
			continue
		}
		if now.Sub(last) >= baseIdleGrace {
			out = append(out, name)
		}
	}
	return out
}
