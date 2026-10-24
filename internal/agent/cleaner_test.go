package agent

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestCleanerRunsOneAtATimeInOrder(t *testing.T) {
	c := newCleaner()
	var running, peak int32
	var order []int
	for i := 0; i < 5; i++ {
		i := i
		c.later(func() {
			n := atomic.AddInt32(&running, 1)
			if n > atomic.LoadInt32(&peak) {
				atomic.StoreInt32(&peak, n)
			}
			time.Sleep(5 * time.Millisecond)
			order = append(order, i)
			atomic.AddInt32(&running, -1)
		})
	}
	c.wait(2 * time.Second)
	if peak != 1 || len(order) != 5 {
		t.Fatalf("peak concurrency %d, ran %d of 5", peak, len(order))
	}
	for i, v := range order {
		if v != i {
			t.Fatalf("order = %v", order)
		}
	}
}

func TestCleanerRunsInlineWhenFull(t *testing.T) {
	c := &cleaner{q: make(chan func(), 1)} // no worker: the queue fills at once
	ran := 0
	c.later(func() { ran++ }) // queued
	c.later(func() { ran++ }) // full: runs inline
	if ran != 1 {
		t.Fatalf("inline runs = %d, want 1", ran)
	}
}
