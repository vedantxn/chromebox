// Package gateway is the part of chromebox that sits at the network edge: the
// CDP proxy agents connect to, and the helpers that point a VM's address at
// whichever node holds it.
package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vedantxn/chromebox/internal/netsetup"
	"github.com/vedantxn/chromebox/internal/token"
)

// cdpPort is where the guest's bridge exposes Chrome's DevTools port.
const cdpPort = 9223

var vmPath = regexp.MustCompile(`^/v/(\d+)(/.*)?$`)

type ctxKey int

const (
	keyVM ctxKey = iota
	keyClientHost
	keyRest // the path to request from Chrome, when the handler already worked it out
)

// Handler returns the CDP proxy: an agent connects to
// http://<gateway>:9222/v/<n>/... and is forwarded to Chrome in VM n, using
// the VM's fixed address. The proxy knows nothing about which node holds the
// VM: it dials the VM's IP and the network routes the packets. That is why a
// migration is invisible to it, and why the agent's connection, which ends here,
// never has to be reopened.
func Handler() http.Handler { return NewProxy(nil) }

// Proxy is the CDP proxy plus a register of the WebSocket connections it holds
// for each VM, so that the control plane can close them when a VM is lost.
// Without that, an agent whose VM died waits on a connection that will never
// answer (M2's break-it test: no error, no close, just silence).
type Proxy struct {
	rp      *httputil.ReverseProxy
	allowed func(n int) bool // nil: every VM index is allowed

	mu    sync.Mutex
	conns map[int]map[*trackedConn]struct{}

	secret []byte // signs session tokens; nil disables /cdp/<token>
}

// SetSecret enables token-authenticated access at /cdp/<token>[/path].
func (p *Proxy) SetSecret(secret []byte) { p.secret = secret }

// Public returns the handler for the internet-facing listener: only
// /cdp/<token> paths are served there. The /v/<n> paths, which need no token,
// stay on a listener only the operator can reach.
func (p *Proxy) Public() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/cdp/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		p.ServeHTTP(w, r)
	})
}

// serveToken handles /cdp/<token>[/rest]. A bare /cdp/<token> means "the
// browser": the proxy asks Chrome where its browser endpoint is, so a client
// can pass the URL straight to connectOverCDP.
func (p *Proxy) serveToken(w http.ResponseWriter, r *http.Request) {
	tok, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/cdp/"), "/")
	if p.secret == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	c, err := token.Verify(p.secret, tok, time.Now())
	if err != nil {
		http.Error(w, "invalid session token: "+err.Error(), http.StatusUnauthorized)
		return
	}
	nn, err := netsetup.ForIndex(c.Index)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	path := "/" + rest
	if rest == "" {
		if path, err = browserPath(r.Context(), netip.AddrPortFrom(nn.GuestIP, cdpPort).String()); err != nil {
			http.Error(w, "browser not reachable: "+err.Error(), http.StatusBadGateway)
			return
		}
	}
	ctx := context.WithValue(r.Context(), keyVM, c.Index)
	ctx = context.WithValue(ctx, keyClientHost, r.Host)
	ctx = context.WithValue(ctx, keyRest, path)
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		log.Printf("vm %d (%s): websocket open", c.Index, c.Session)
		w = &hijackTracker{ResponseWriter: w, track: func(tc *trackedConn) { p.add(c.Index, tc) }}
	}
	p.rp.ServeHTTP(w, r.WithContext(ctx))
}

// browserPath asks Chrome for its browser-level WebSocket path.
func browserPath(ctx context.Context, target string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+target+"/json/version", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var v struct {
		URL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	u, err := url.Parse(v.URL)
	if err != nil {
		return "", err
	}
	return u.Path, nil
}

// NewProxy returns a proxy. allowed, if set, refuses VMs the control plane does
// not know as running, at once, instead of dialing an address nobody answers.
func NewProxy(allowed func(n int) bool) *Proxy {
	p := &Proxy{rp: newReverseProxy(), allowed: allowed, conns: map[int]map[*trackedConn]struct{}{}}
	return p
}

// CloseVM closes every client connection to VM n and returns how many.
func (p *Proxy) CloseVM(n int) int {
	p.mu.Lock()
	cs := p.conns[n]
	delete(p.conns, n)
	p.mu.Unlock()
	for c := range cs {
		_ = c.Conn.Close()
	}
	return len(cs)
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/cdp/") {
		p.serveToken(w, r)
		return
	}
	m := vmPath.FindStringSubmatch(r.URL.Path)
	if m == nil {
		http.Error(w, "use /v/<vm index>/...", http.StatusNotFound)
		return
	}
	n, _ := strconv.Atoi(m[1])
	if _, err := netsetup.ForIndex(n); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if p.allowed != nil && !p.allowed(n) {
		http.Error(w, "no running VM with index "+m[1], http.StatusNotFound)
		return
	}
	ctx := context.WithValue(r.Context(), keyVM, n)
	ctx = context.WithValue(ctx, keyClientHost, r.Host)
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		log.Printf("vm %d: websocket open %s", n, r.URL.Path)
		// The reverse proxy takes the client connection over (hijacks it) for
		// a WebSocket; catch it there and register it under the VM.
		w = &hijackTracker{ResponseWriter: w, track: func(c *trackedConn) { p.add(n, c) }}
	}
	p.rp.ServeHTTP(w, r.WithContext(ctx))
}

func (p *Proxy) add(n int, c *trackedConn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conns[n] == nil {
		p.conns[n] = map[*trackedConn]struct{}{}
	}
	p.conns[n][c] = struct{}{}
	c.onClose = func() {
		p.mu.Lock()
		delete(p.conns[n], c)
		p.mu.Unlock()
	}
}

type hijackTracker struct {
	http.ResponseWriter
	track func(*trackedConn)
}

func (h *hijackTracker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := h.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("connection cannot be hijacked")
	}
	c, rw, err := hj.Hijack()
	if err != nil {
		return nil, nil, err
	}
	tc := &trackedConn{Conn: c, done: make(chan struct{}), lastWrite: time.Now()}
	h.track(tc)
	go tc.keepalive()
	return tc, rw, nil
}

// trackedConn is the client side of a proxied WebSocket. The reverse proxy
// writes Chrome's frames to it; it watches their boundaries so that keepalive
// can slip a ping in between two of them.
type trackedConn struct {
	net.Conn
	once    sync.Once
	onClose func()
	done    chan struct{}

	mu        sync.Mutex // one writer at a time: the proxy's copy or a ping
	frames    frameTracker
	lastWrite time.Time
}

func (c *trackedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err := c.Conn.Write(p)
	c.frames.feed(p[:n])
	c.lastWrite = time.Now()
	return n, err
}

func (c *trackedConn) keepalive() {
	t := time.NewTicker(keepaliveAfter / 2)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
		}
		c.mu.Lock()
		if c.frames.atBoundary() && time.Since(c.lastWrite) >= keepaliveAfter {
			_, _ = c.Conn.Write(pingFrame)
			c.lastWrite = time.Now()
		}
		c.mu.Unlock()
	}
}

func (c *trackedConn) Close() error {
	c.once.Do(func() {
		close(c.done)
		if c.onClose != nil {
			c.onClose()
		}
	})
	return c.Conn.Close()
}

func newReverseProxy() *httputil.ReverseProxy {
	transport := &http.Transport{
		// Short keepalive probes keep the long-lived upstream connection from
		// being mistaken for dead, and detect it if it really is.
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 5 * time.Second}).DialContext,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     30 * time.Second,
	}
	return &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			n := pr.In.Context().Value(keyVM).(int)
			nn, _ := netsetup.ForIndex(n) // validated by the caller
			target := netip.AddrPortFrom(nn.GuestIP, cdpPort).String()
			rest := "/"
			if r, ok := pr.In.Context().Value(keyRest).(string); ok {
				rest = r
			} else if m := vmPath.FindStringSubmatch(pr.In.URL.Path); m != nil && m[2] != "" {
				rest = m[2]
			}
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = target
			pr.Out.URL.Path, pr.Out.URL.RawPath = rest, ""
			// Chrome only answers requests whose Host header is an IP address
			// or localhost, and rejects WebSocket handshakes that carry a
			// browser Origin header.
			pr.Out.Host = target
			pr.Out.Header.Del("Origin")
		},
		ModifyResponse: rewriteDiscovery,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("vm %v: upstream error: %v", r.Context().Value(keyVM), err)
			http.Error(w, "bad gateway: "+err.Error(), http.StatusBadGateway)
		},
	}
}

// rewriteDiscovery fixes up Chrome's discovery documents (/json/version,
// /json/list, ...), which tell the client where to open its WebSocket. Chrome
// fills in the address it was reached at (the VM's private IP); the agent
// needs the gateway's address and the /v/<n> prefix instead.
func rewriteDiscovery(resp *http.Response) error {
	path := resp.Request.URL.Path
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(path, "/json") {
		return nil
	}
	n := resp.Request.Context().Value(keyVM).(int)
	client, _ := resp.Request.Context().Value(keyClientHost).(string)
	nn, _ := netsetup.ForIndex(n)
	target := netip.AddrPortFrom(nn.GuestIP, cdpPort).String()

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf("%s/v/%d/", client, n)
	body = bytes.ReplaceAll(body, []byte("ws://"+target+"/"), []byte("ws://"+prefix))
	body = bytes.ReplaceAll(body, []byte("ws="+target+"/"), []byte("ws="+prefix))
	body = bytes.ReplaceAll(body, []byte("http://"+target+"/"), []byte("http://"+prefix))
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return nil
}
