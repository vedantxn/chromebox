package netsetup

import (
	"net"
	"testing"
)

// A delete that was put off must not remove a TAP that a newer VM with the
// same index has claimed in the meantime.
func TestDeleteTapUnlessReclaimed(t *testing.T) {
	ifs, err := net.Interfaces()
	if err != nil || len(ifs) == 0 {
		t.Skip("no network interface to stand in for the TAP")
	}
	var deletes int
	orig := ipRunLow
	ipRunLow = func([]string) error { deletes++; return nil }
	defer func() { ipRunLow = orig }()

	n := Net{Index: 12345, TapName: ifs[0].Name}
	claim := func() { e := n.entry(); e.mu.Lock(); e.claims++; e.mu.Unlock() }

	claim() // a VM has the index
	stopped := n.Claims()
	if err := n.DeleteTapUnlessReclaimed(stopped); err != nil || deletes != 1 {
		t.Fatalf("nobody reclaimed it: want one delete, got %d (err %v)", deletes, err)
	}
	claim() // a new VM takes the index before a second, late delete runs
	if err := n.DeleteTapUnlessReclaimed(stopped); err != nil || deletes != 1 {
		t.Fatalf("reclaimed TAP must stay: deletes = %d (err %v)", deletes, err)
	}
	if n.Claims() != stopped+1 {
		t.Fatalf("claims = %d, want %d", n.Claims(), stopped+1)
	}
}

// The next claim of an index waits for the VM that still holds it, once.
func TestBusyIsWaitedForOnceByTheNextClaim(t *testing.T) {
	orig := ipRun
	ipRun = func([]string) error { return nil }
	defer func() { ipRun = orig }()

	n := Net{Index: 12346, TapName: "fc-test-none"}
	waited := 0
	n.SetBusy(func() { waited++ })
	if err := n.EnsureTap(1, 1); err != nil || waited != 1 {
		t.Fatalf("first claim: waited %d times (err %v), want 1", waited, err)
	}
	if err := n.EnsureTap(1, 1); err != nil || waited != 1 {
		t.Fatalf("second claim: waited %d times (err %v), want still 1", waited, err)
	}
}
