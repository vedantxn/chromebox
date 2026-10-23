package agent

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vedantxn/chromebox/internal/base"
	"github.com/vedantxn/chromebox/internal/vm"
)

// The pool keeps a few jailed Firecracker processes started ahead of time for
// the base most creates use. Starting one costs ~70 ms (the jailer: a chroot,
// a cgroup, a PID namespace, a copy of the binary) and needs nothing about
// the VM to come: not its id, not its address. So a create takes a ready slot
// and renames it to the VM's id, and the slot is replaced in the background.
//
//	CHROMEBOX_POOL       slots to keep ready (default 2; 0 turns it off)
//	CHROMEBOX_POOL_BASE  the base they are for (default chrome-v1)
//
// Slots are not VMs: they hold no guest memory, are never reported in
// heartbeats (or the orchestrator would fence them as unknown) and are
// cleared when the agent starts, since a slot outlives the agent that made it.
type pool struct {
	base string
	size int

	mu    sync.Mutex
	ready []vm.Paths
	kick  chan struct{}
}

const slotPrefix = "pool-"

func newPool() *pool {
	size := 2
	if v, err := strconv.Atoi(os.Getenv("CHROMEBOX_POOL")); err == nil {
		size = v
	}
	b := os.Getenv("CHROMEBOX_POOL_BASE")
	if b == "" {
		b = "chrome-v1"
	}
	return &pool{base: b, size: size, kick: make(chan struct{}, 1)}
}

// take returns a live slot for base, or false (then the create starts its
// own process, as without a pool).
func (pl *pool) take(baseName string) (vm.Paths, bool) {
	if pl == nil || baseName != pl.base {
		return vm.Paths{}, false
	}
	pl.mu.Lock()
	defer pl.mu.Unlock()
	for len(pl.ready) > 0 {
		s := pl.ready[0]
		pl.ready = pl.ready[1:]
		if alive(s) {
			pl.refill()
			return s, true
		}
		go destroySlot(s) // its process died: drop it
	}
	pl.refill()
	return vm.Paths{}, false
}

func (pl *pool) refill() {
	select {
	case pl.kick <- struct{}{}:
	default:
	}
}

// run keeps size slots ready until ctx ends.
func (pl *pool) run(ctx context.Context, load func(string) error) {
	clearSlots()
	pl.refill()
	for {
		select {
		case <-ctx.Done():
			return
		case <-pl.kick:
		}
		for {
			pl.mu.Lock()
			n := len(pl.ready)
			pl.mu.Unlock()
			if n >= pl.size {
				break
			}
			if err := load(pl.base); err != nil {
				log.Printf("pool: base %s: %v", pl.base, err)
				break
			}
			s := vm.PathsFor(slotPrefix + randomToken()[:8])
			if err := base.PrepareSlot(ctx, s, pl.base); err != nil {
				log.Printf("pool: slot %s: %v", filepath.Base(s.Dir), err)
				destroySlot(s)
				time.Sleep(5 * time.Second)
				break
			}
			pl.mu.Lock()
			pl.ready = append(pl.ready, s)
			pl.mu.Unlock()
		}
	}
}

func isSlot(id string) bool { return strings.HasPrefix(id, slotPrefix) }

func destroySlot(s vm.Paths) {
	_ = vm.Kill(s)
	_ = os.RemoveAll(s.Dir)
	_ = os.RemoveAll(s.MemDir())
}

// clearSlots removes slots left by a previous agent.
func clearSlots() {
	entries, _ := os.ReadDir(filepath.Join(vm.StateDir, "vms"))
	for _, e := range entries {
		if isSlot(e.Name()) {
			destroySlot(vm.PathsFor(e.Name()))
		}
	}
}
