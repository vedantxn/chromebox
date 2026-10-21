// Package gateway is the part of chromebox that sits at the network edge: the
// CDP proxy agents connect to, and the helpers that point a VM's address at
// whichever node holds it.
package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vedantxn/chromebox/internal/netsetup"
)

// cdpPort is where the guest's bridge exposes Chrome's DevTools port.
const cdpPort = 9223

var vmPath = regexp.MustCompile(`^/v/(\d+)(/.*)?$`)

type ctxKey int

const (
	keyVM ctxKey = iota
	keyClientHost
)

// Handler returns the CDP proxy: an agent connects to
// http://<gateway>:9222/v/<n>/... and is forwarded to Chrome in VM n, using
// the VM's fixed address. The proxy knows nothing about which node holds the
// VM: it dials the VM's IP and the network routes the packets. That is why a
// migration is invisible to it, and why the agent's connection, which ends here,
// never has to be reopened.
func Handler() http.Handler {
	transport := &http.Transport{
		// Short keepalive probes keep the long-lived upstream connection from
		// being mistaken for dead, and detect it if it really is.
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 5 * time.Second}).DialContext,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     30 * time.Second,
	}
	rp := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			n := pr.In.Context().Value(keyVM).(int)
			nn, _ := netsetup.ForIndex(n) // validated by the caller
			target := netip.AddrPortFrom(nn.GuestIP, cdpPort).String()
			rest := "/"
			if m := vmPath.FindStringSubmatch(pr.In.URL.Path); m != nil && m[2] != "" {
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

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		ctx := context.WithValue(r.Context(), keyVM, n)
		ctx = context.WithValue(ctx, keyClientHost, r.Host)
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			log.Printf("vm %d: websocket open %s", n, r.URL.Path)
		}
		rp.ServeHTTP(w, r.WithContext(ctx))
	})
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
