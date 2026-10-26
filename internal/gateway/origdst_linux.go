package gateway

import (
	"net"
	"net/netip"
	"syscall"
)

// originalDst is where a redirected connection was going (SO_ORIGINAL_DST,
// set by the nftables redirect).
func originalDst(c *net.TCPConn) (netip.AddrPort, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return netip.AddrPort{}, err
	}
	var (
		mreq *syscall.IPv6Mreq
		gerr error
	)
	err = raw.Control(func(fd uintptr) {
		// sockaddr_in comes back in the first 16 bytes; IPv6Mreq is a 20-byte
		// buffer the standard library already knows how to fill.
		mreq, gerr = syscall.GetsockoptIPv6Mreq(int(fd), syscall.IPPROTO_IP, 80 /* SO_ORIGINAL_DST */)
	})
	if err != nil {
		return netip.AddrPort{}, err
	}
	if gerr != nil {
		return netip.AddrPort{}, gerr
	}
	b := mreq.Multiaddr
	port := uint16(b[2])<<8 | uint16(b[3])
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte{b[4], b[5], b[6], b[7]}), port), nil
}
