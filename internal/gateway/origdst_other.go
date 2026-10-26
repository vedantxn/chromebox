//go:build !linux

package gateway

import (
	"errors"
	"net"
	"net/netip"
)

// originalDst needs Linux's SO_ORIGINAL_DST; the gateway only runs on Linux.
func originalDst(*net.TCPConn) (netip.AddrPort, error) {
	return netip.AddrPort{}, errors.New("original destination: Linux only")
}
