package netsetup

import "testing"

func TestForIndex(t *testing.T) {
	n, err := ForIndex(1)
	if err != nil {
		t.Fatal(err)
	}
	// VM 1 owns 10.200.0.4/30: host end .5, guest .6.
	if got := n.TapIP.String(); got != "10.200.0.5" {
		t.Errorf("tap ip = %s", got)
	}
	if got := n.GuestIP.String(); got != "10.200.0.6" {
		t.Errorf("guest ip = %s", got)
	}
	if n.GuestMAC != "06:00:0a:c8:00:06" || n.TapMAC != "06:00:0a:c8:00:05" {
		t.Errorf("macs = %s %s", n.GuestMAC, n.TapMAC)
	}
	if got := n.Block().String(); got != "10.200.0.4/30" {
		t.Errorf("block = %s", got)
	}
	if got := n.BootArgs(); got != "ip=10.200.0.6::10.200.0.5:255.255.255.252::eth0:off" {
		t.Errorf("boot args = %s", got)
	}
}

func TestForIndexCrossesAByteBoundary(t *testing.T) {
	n, err := ForIndex(64) // 4*64 = 256 -> third octet rolls over
	if err != nil {
		t.Fatal(err)
	}
	if got := n.GuestIP.String(); got != "10.200.1.2" {
		t.Errorf("guest ip = %s", got)
	}
}

func TestForIndexRange(t *testing.T) {
	for _, bad := range []int{-1, 0, MaxIndex + 1} {
		if _, err := ForIndex(bad); err == nil {
			t.Errorf("index %d should be rejected", bad)
		}
	}
	if _, err := ForIndex(MaxIndex); err != nil {
		t.Errorf("max index rejected: %v", err)
	}
}

func TestAddressesAreUniquePerVM(t *testing.T) {
	seen := map[string]int{}
	for i := 1; i <= MaxIndex; i++ {
		n, _ := ForIndex(i)
		for _, a := range []string{n.TapIP.String(), n.GuestIP.String()} {
			if prev, dup := seen[a]; dup {
				t.Fatalf("VMs %d and %d share %s", prev, i, a)
			}
			seen[a] = i
		}
	}
}
