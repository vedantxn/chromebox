package gateway

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Egress sends a session's traffic out through the customer's own proxy.
//
// Chrome's proxy is a launch setting, and the browser is already running, so
// this happens in the network instead: the gateway (which every VM's traffic
// crosses anyway) redirects new TCP connections from a proxied VM to this
// listener (an nftables DNAT the control plane manages, egress set
// "proxied"). Here each connection's original destination is read back, the
// host name taken from the TLS ClientHello (SNI) or the HTTP Host header, and
// the connection is tunnelled through the customer's proxy with CONNECT. The
// site therefore sees the proxy's address, and resolves the name where the
// proxy is. UDP from a proxied VM, other than DNS, is dropped by the same
// rules, so QUIC and WebRTC cannot go around the proxy.
const EgressPort = 3129

// ServeEgress accepts redirected connections on addr until it fails.
func (p *Proxy) ServeEgress(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		go p.egress(c.(*net.TCPConn))
	}
}

func (p *Proxy) egress(c *net.TCPConn) {
	defer c.Close()
	dst, err := originalDst(c)
	if err != nil {
		return
	}
	src, _ := netip.ParseAddrPort(c.RemoteAddr().String())
	n, ok := vmIndex(src.Addr())
	if !ok {
		return
	}
	s, ok := p.settingsFor(n)
	if !ok || s.Proxy == nil {
		return // not a proxied VM (or no longer): nothing goes out
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReaderSize(c, 16<<10)
	host := sniffHost(br, dst.Port())
	_ = c.SetReadDeadline(time.Time{})
	if host == "" {
		host = dst.Addr().String()
	}
	up, err := dialUpstream(s.Proxy, net.JoinHostPort(host, strconv.Itoa(int(dst.Port()))))
	if err != nil {
		log.Printf("vm %d: proxy %s: %v", n, s.Proxy.Server, err)
		return
	}
	defer up.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, br); closeWrite(up); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, up); closeWrite(c); done <- struct{}{} }()
	<-done
	<-done
}

func closeWrite(c net.Conn) {
	if t, ok := c.(*net.TCPConn); ok {
		_ = t.CloseWrite()
	}
}

// vmIndex is the VM whose address a is (netsetup: VM n is 10.200.0.0 + 4n + 2).
func vmIndex(a netip.Addr) (int, bool) {
	b := a.Unmap().As4()
	if !a.Unmap().Is4() || b[0] != 10 || b[1] != 200 {
		return 0, false
	}
	off := int(b[2])<<8 | int(b[3])
	if off%4 != 2 {
		return 0, false
	}
	return off / 4, true
}

// sniffHost reads the host a connection is for without consuming anything:
// the server name of a TLS ClientHello, or the Host header of an HTTP request.
func sniffHost(br *bufio.Reader, port uint16) string {
	first, err := br.Peek(1)
	if err != nil {
		return ""
	}
	if first[0] == 0x16 { // TLS handshake
		hdr, err := br.Peek(5)
		if err != nil {
			return ""
		}
		n := 5 + (int(hdr[3])<<8 | int(hdr[4]))
		b, err := br.Peek(min(n, br.Size()))
		if err != nil && len(b) < n {
			return ""
		}
		return serverName(b[5:])
	}
	if port == 443 {
		return ""
	}
	// Plain HTTP: the request line and headers, as far as they have arrived;
	// wait for more only while they are incomplete.
	for {
		b, _ := br.Peek(br.Buffered())
		if i := strings.Index(string(b), "\r\n\r\n"); i >= 0 {
			for _, line := range strings.Split(string(b[:i]), "\r\n")[1:] {
				if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "host") {
					h := strings.TrimSpace(v)
					if hh, _, err := net.SplitHostPort(h); err == nil {
						h = hh
					}
					return h
				}
			}
			return ""
		}
		if br.Buffered() >= br.Size() {
			return ""
		}
		if _, err := br.Peek(br.Buffered() + 1); err != nil {
			return ""
		}
	}
}

// serverName finds the SNI host name in a TLS handshake message (a ClientHello).
func serverName(b []byte) string {
	// handshake type (1) + length (3) + version (2) + random (32)
	if len(b) < 38 || b[0] != 0x01 {
		return ""
	}
	i := 38
	skip := func(lenBytes int) bool {
		if i+lenBytes > len(b) {
			return false
		}
		l := 0
		for k := 0; k < lenBytes; k++ {
			l = l<<8 | int(b[i+k])
		}
		i += lenBytes + l
		return i <= len(b)
	}
	if !skip(1) || !skip(2) || !skip(1) { // session id, cipher suites, compression
		return ""
	}
	if i+2 > len(b) {
		return ""
	}
	end := i + 2 + (int(b[i])<<8 | int(b[i+1]))
	i += 2
	for i+4 <= end && end <= len(b) {
		typ, l := int(b[i])<<8|int(b[i+1]), int(b[i+2])<<8|int(b[i+3])
		i += 4
		if typ == 0 && i+5 <= len(b) { // server_name: list length (2), type (1), name length (2), name
			nl := int(b[i+3])<<8 | int(b[i+4])
			if b[i+2] == 0 && i+5+nl <= len(b) {
				return string(b[i+5 : i+5+nl])
			}
			return ""
		}
		i += l
	}
	return ""
}

// dialUpstream opens a CONNECT tunnel to target through the customer's proxy.
func dialUpstream(u *Upstream, target string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(u.Server)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	var addr netip.Addr
	for _, ip := range ips {
		if PublicAddr(ip) {
			addr = ip
			break
		}
	}
	if !addr.IsValid() {
		return nil, errors.New("the proxy's address is not a public one")
	}
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(addr.String(), port))
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
	if u.Username != "" || u.Password != "" {
		req += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(u.Username+":"+u.Password)) + "\r\n"
	}
	if _, err := io.WriteString(c, req+"\r\n"); err != nil {
		c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		c.Close()
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.Close()
		return nil, fmt.Errorf("CONNECT %s: %s", target, resp.Status)
	}
	_ = c.SetDeadline(time.Time{})
	if br.Buffered() > 0 { // the proxy already sent some of the tunnel
		return &prefixConn{Conn: c, r: br}, nil
	}
	return c, nil
}

type prefixConn struct {
	net.Conn
	r *bufio.Reader
}

func (p *prefixConn) Read(b []byte) (int, error) { return p.r.Read(b) }

// PublicAddr reports whether ip is on the public internet: not loopback,
// private (RFC 1918, our VPC and VMs), link-local (the cloud metadata service),
// shared (100.64/10) or multicast. A customer must not be able to point the
// gateway at anything inside.
func PublicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if ip.Is4() {
		b := ip.As4()
		if b[0] == 0 || b[0] >= 240 || (b[0] == 100 && b[1]&0xc0 == 64) || (b[0] == 192 && b[1] == 0 && b[2] == 0) {
			return false
		}
	}
	return true
}
