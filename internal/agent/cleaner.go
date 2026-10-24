package agent

import (
	"sync"
	"time"
)

// cleaner runs deferred work one item at a time, so that clearing the leftovers
// of deleted VMs never competes with itself, or for long with a create.
type cleaner struct {
	q  chan func()
	wg sync.WaitGroup
}

func newCleaner() *cleaner {
	c := &cleaner{q: make(chan func(), 256)}
	go func() {
		for f := range c.q {
			f()
			c.wg.Done()
		}
	}()
	return c
}

// later queues f. If the queue is full it runs f now rather than let it grow.
func (c *cleaner) later(f func()) {
	c.wg.Add(1)
	select {
	case c.q <- f:
	default:
		f()
		c.wg.Done()
	}
}

// wait returns when everything queued so far is done, or after d.
func (c *cleaner) wait(d time.Duration) {
	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
	}
}
