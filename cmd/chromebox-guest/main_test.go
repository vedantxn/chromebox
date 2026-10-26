package main

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

func TestParseStat(t *testing.T) {
	pid, comm, ppid, ok := parseStat([]byte("812 (socat) S 377 812 377 0 -1 4194560 120 0 0 0\n"))
	if !ok || pid != 812 || comm != "socat" || ppid != 377 {
		t.Fatalf("got %d %q %d %v", pid, comm, ppid, ok)
	}
	// A name may hold spaces and parentheses.
	pid, comm, ppid, ok = parseStat([]byte("9 (a b) c)) R 1 9 9 0\n"))
	if !ok || pid != 9 || comm != "a b) c)" || ppid != 1 {
		t.Fatalf("got %d %q %d %v", pid, comm, ppid, ok)
	}
	if _, _, _, ok := parseStat([]byte("garbage")); ok {
		t.Fatal("parsed garbage")
	}
}

// Only socat's children are connections: not socat itself (the listener) or
// anything else.
func TestBridged(t *testing.T) {
	proc := t.TempDir()
	for pid, stat := range map[string]string{
		"1":   "1 (systemd) S 0 1 1 0",
		"377": "377 (socat) S 1 377 377 0", // the listener
		"812": "812 (socat) S 377 812 377 0",
		"813": "813 (socat) S 377 813 377 0",
		"500": "500 (chrome) S 1 500 500 0",
		"501": "501 (socat) S 500 501 500 0", // a socat, but not the bridge's
	} {
		_ = os.MkdirAll(filepath.Join(proc, pid), 0o755)
		_ = os.WriteFile(filepath.Join(proc, pid, "stat"), []byte(stat), 0o644)
	}
	got := bridged(proc)
	sort.Ints(got)
	if !slices.Equal(got, []int{812, 813}) {
		t.Fatalf("got %v, want [812 813]", got)
	}
}
