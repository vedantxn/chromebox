package orchestrator

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestIndexesAreHandedOutRoundRobinAndNeverShared(t *testing.T) {
	s := openTest(t)
	a, _ := s.InsertVM("a", "node-a", "chrome", 1024, time.Time{})
	b, _ := s.InsertVM("b", "node-a", "chrome", 1024, time.Time{})
	c, _ := s.InsertVM("c", "node-b", "chrome", 1024, time.Time{})
	if a.NetIndex != 1 || b.NetIndex != 2 || c.NetIndex != 3 {
		t.Fatalf("indexes %d %d %d", a.NetIndex, b.NetIndex, c.NetIndex)
	}
	s.DeleteVM("b") // frees 2
	d, _ := s.InsertVM("d", "node-b", "chrome", 1024, time.Time{})
	if d.NetIndex != 4 {
		t.Fatalf("a freed index must not come straight back (its process may still be exiting): got %d, want 4", d.NetIndex)
	}
}

func TestIndexesWrapAroundAndFindTheFreeOnes(t *testing.T) {
	s := openTest(t)
	s.lastIdx = maxUserIndex - 1
	a, _ := s.InsertVM("a", "n", "chrome", 1, time.Time{}) // the last index
	b, _ := s.InsertVM("b", "n", "chrome", 1, time.Time{}) // wraps to the first
	c, _ := s.InsertVM("c", "n", "chrome", 1, time.Time{})
	if a.NetIndex != maxUserIndex || b.NetIndex != 1 || c.NetIndex != 2 {
		t.Fatalf("indexes %d %d %d", a.NetIndex, b.NetIndex, c.NetIndex)
	}
	s.DeleteVM("b") // frees 1; 2 and maxUserIndex stay taken
	s.lastIdx = maxUserIndex
	d, _ := s.InsertVM("d", "n", "chrome", 1, time.Time{})
	if d.NetIndex != 1 {
		t.Fatalf("wrapping round should find the free index 1, got %d", d.NetIndex)
	}
}

func TestConcurrentInsertsGetDistinctIndexes(t *testing.T) {
	s := openTest(t)
	var wg sync.WaitGroup
	seen := sync.Map{}
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := s.InsertVM(fmt.Sprintf("vm-%d", i), "n", "chrome", 1, time.Time{})
			if err != nil {
				t.Error(err)
				return
			}
			if _, dup := seen.LoadOrStore(v.NetIndex, v.ID); dup {
				t.Errorf("index %d handed out twice", v.NetIndex)
			}
		}(i)
	}
	wg.Wait()
}

func TestUsedMemCountsEverythingButLostVMs(t *testing.T) {
	s := openTest(t)
	s.InsertVM("a", "node-a", "chrome", 1024, time.Time{})
	s.InsertVM("b", "node-a", "chrome", 1024, time.Time{}) // still "creating": it already takes its space
	s.InsertVM("c", "node-b", "chrome", 1024, time.Time{})
	s.SetState("c", StateLost)
	used, _ := s.UsedMem()
	if used["node-a"] != 2048 || used["node-b"] != 0 {
		t.Fatalf("used = %v", used)
	}
}
