package gateway

import (
	"bufio"
	"crypto/tls"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// A real ClientHello, as Go's TLS client sends it, names the server.
func TestServerNameFromARealClientHello(t *testing.T) {
	a, b := net.Pipe()
	go func() {
		c := tls.Client(a, &tls.Config{ServerName: "shop.example.com"})
		_ = c.SetDeadline(time.Now().Add(time.Second))
		_ = c.Handshake() // fails: nobody answers; the hello is what we want
	}()
	br := bufio.NewReaderSize(b, 16<<10)
	if got := sniffHost(br, 443); got != "shop.example.com" {
		t.Fatalf("got %q", got)
	}
	// Nothing was consumed: the tunnel gets the whole hello.
	if first, _ := br.Peek(1); first[0] != 0x16 {
		t.Fatalf("the hello was consumed")
	}
	b.Close()
	a.Close()
}

func TestHostOfAPlainHTTPRequest(t *testing.T) {
	r := bufio.NewReaderSize(strings.NewReader("GET / HTTP/1.1\r\nUser-Agent: x\r\nHost: example.com:8080\r\n\r\n"), 16<<10)
	if got := sniffHost(r, 80); got != "example.com" {
		t.Fatalf("got %q", got)
	}
	rest, _ := io.ReadAll(r)
	if !strings.HasPrefix(string(rest), "GET / HTTP/1.1") {
		t.Fatalf("the request was consumed: %q", rest)
	}
}

func TestAShortHTTPRequestIsNotHeldBack(t *testing.T) {
	a, b := net.Pipe()
	go func() { _, _ = a.Write([]byte("GET / HTTP/1.1\r\nHost: a.example\r\n\r\n")) }()
	start := time.Now()
	if got := sniffHost(bufio.NewReaderSize(b, 16<<10), 80); got != "a.example" || time.Since(start) > time.Second {
		t.Fatalf("got %q after %v", got, time.Since(start))
	}
	a.Close()
	b.Close()
}

func TestVMIndexFromItsAddress(t *testing.T) {
	for addr, want := range map[string]int{"10.200.0.6": 1, "10.200.4.2": 256, "10.200.0.78": 19} {
		if n, ok := vmIndex(netip.MustParseAddr(addr)); !ok || n != want {
			t.Errorf("%s: %d %v, want %d", addr, n, ok, want)
		}
	}
	for _, addr := range []string{"10.200.0.5", "10.201.0.6", "8.8.8.8"} { // a TAP's address, other ranges
		if _, ok := vmIndex(netip.MustParseAddr(addr)); ok {
			t.Errorf("%s taken for a VM", addr)
		}
	}
}

func TestOnlyPublicAddressesCanBeProxies(t *testing.T) {
	for _, a := range []string{"169.254.169.254", "10.0.0.1", "172.31.7.29", "192.168.1.1", "127.0.0.1", "100.64.0.1", "0.0.0.0", "224.0.0.1", "::1", "fd00::1"} {
		if PublicAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s allowed", a)
		}
	}
	for _, a := range []string{"8.8.8.8", "104.20.23.154", "2606:4700::1"} {
		if !PublicAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s refused", a)
		}
	}
}
