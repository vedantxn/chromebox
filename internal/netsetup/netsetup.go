// Package netsetup gives each VM a network identity that depends only on its
// index, so the same VM has the same IP and MAC addresses on every node. That
// is what lets a migrated guest keep its TCP connections: its own end of each
// connection is unchanged, and only the route towards it moves.
package netsetup

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
)

// guestRange is where all guest addresses live. Each VM gets a /30 inside it.
var guestRange = netip.MustParseAddr("10.200.0.0")

// MaxIndex is the highest VM index: 65536 addresses in a /16, four per VM,
// minus the first block, which is not used.
const MaxIndex = 16383

// Net is one VM's network identity.
type Net struct {
	Index    int
	TapName  string     // the host-side device Firecracker attaches to
	TapIP    netip.Addr // the host end, and the guest's default gateway
	GuestIP  netip.Addr
	TapMAC   string
	GuestMAC string
}

// ForIndex derives a VM's addresses from its index n (1..MaxIndex). VM n owns
// the /30 block starting at 10.200.0.0 + 4n: the host end is block+1, the guest
// is block+2. A /30 per VM keeps each VM's addresses unique on a node that runs
// many VMs, while staying the same on every node for the same VM.
func ForIndex(n int) (Net, error) {
	if n < 1 || n > MaxIndex {
		return Net{}, fmt.Errorf("net index %d out of range 1..%d", n, MaxIndex)
	}
	base := addrPlus(guestRange, 4*n)
	tap, guest := addrPlus(base, 1), addrPlus(base, 2)
	return Net{
		Index:    n,
		TapName:  fmt.Sprintf("fc%d", n),
		TapIP:    tap,
		GuestIP:  guest,
		TapMAC:   macFor(tap),
		GuestMAC: macFor(guest),
	}, nil
}

// Block is the VM's /30, the unit the gateway routes.
func (n Net) Block() netip.Prefix {
	return netip.PrefixFrom(addrPlus(n.TapIP, -1), 30)
}

// BootArgs is the kernel command line fragment that configures the guest's
// eth0 before init starts (the kernel's built-in IP autoconfiguration):
// ip=<client>:<server>:<gateway>:<netmask>:<hostname>:<device>:<autoconf>.
// Per-VM data goes here, not in the golden image.
func (n Net) BootArgs() string {
	return fmt.Sprintf("ip=%s::%s:255.255.255.252::eth0:off", n.GuestIP, n.TapIP)
}

// macFor encodes an IPv4 address in a MAC: 06:00 plus the four address bytes
// (Firecracker's own convention). Locally administered, unicast, and easy to
// read in a packet capture.
func macFor(a netip.Addr) string {
	b := a.As4()
	return fmt.Sprintf("06:00:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3])
}

func addrPlus(a netip.Addr, delta int) netip.Addr {
	b := a.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	v = uint32(int64(v) + int64(delta))
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

// EnsureTap creates the host-side TAP device if it doesn't exist and gives it
// the VM's gateway address and its fixed MAC. The device is owned by uid/gid so
// that Firecracker can open it without extra privileges. Safe to repeat.
//
// The MAC is fixed on purpose: a restored guest still has the TAP's MAC in its
// neighbor (ARP) cache from before the move. If the new node's TAP had a
// different MAC, the guest would keep sending to an address nobody answers to.
func (n Net) EnsureTap(uid, gid int) error {
	var cmds []string
	if _, err := net.InterfaceByName(n.TapName); err != nil {
		cmds = append(cmds, fmt.Sprintf("tuntap add dev %s mode tap user %d group %d", n.TapName, uid, gid))
	}
	mac := n.TapMAC
	if os.Getenv("CHROMEBOX_FAULT") == "tap-mac" { // fault injection for break-it tests
		mac = "06:00:de:ad:be:ef"
	}
	cmds = append(cmds,
		fmt.Sprintf("link set dev %s address %s", n.TapName, mac),
		fmt.Sprintf("addr replace %s/30 dev %s", n.TapIP, n.TapName),
		fmt.Sprintf("link set dev %s up", n.TapName),
	)
	return ipBatch(cmds)
}

// DeleteTap removes the TAP device. A missing device is not an error.
func (n Net) DeleteTap() error {
	if _, err := net.InterfaceByName(n.TapName); err != nil {
		return nil
	}
	return ipBatch([]string{fmt.Sprintf("link del %s", n.TapName)})
}

// ipBatch runs several `ip` commands in one process (one sudo, one exec).
func ipBatch(cmds []string) error {
	c := exec.Command("sudo", "ip", "-batch", "-")
	c.Stdin = strings.NewReader(strings.Join(cmds, "\n") + "\n")
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	if err := c.Run(); err != nil {
		return fmt.Errorf("ip %v: %w: %s", cmds, err, bytes.TrimSpace(out.Bytes()))
	}
	return nil
}
