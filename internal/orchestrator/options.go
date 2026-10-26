package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // timezone names are checked here, whatever the host has installed

	"github.com/vedantxn/chromebox/internal/api"
	"github.com/vedantxn/chromebox/internal/gateway"
	"github.com/vedantxn/chromebox/internal/netsetup"
)

// SessionOptions are what a session can ask for when it is created: how the
// browser presents itself, how long it may run, and a proxy of the customer's
// own for its traffic. They are kept with the session, carried over to fork
// copies, and applied again on resume. The CDP proxy applies the browser
// settings (gateway/settings.go); the proxy is done in the network
// (gateway/egress.go, and the nftables set managed below).
type SessionOptions struct {
	Viewport       *gateway.Viewport `json:"viewport,omitempty"`
	UserAgent      string            `json:"user_agent,omitempty"`
	Locale         string            `json:"locale,omitempty"`
	Timezone       string            `json:"timezone,omitempty"`
	TimeoutMinutes int               `json:"timeout_minutes,omitempty"`
	Proxy          *gateway.Upstream `json:"proxy,omitempty"`
}

const maxSessionMinutes = 60 // the longest a session may ask for (still within the daily minutes)

var localeRe = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`)

// optionError is a request the API refuses as invalid (HTTP 400).
type optionError struct{ msg string }

func (e optionError) Error() string { return e.msg }

func (s *SessionOptions) validate() error {
	if v := s.Viewport; v != nil && (v.Width < 320 || v.Width > 3840 || v.Height < 240 || v.Height > 2160) {
		return optionError{"viewport must be 320-3840 wide and 240-2160 high"}
	}
	if len(s.UserAgent) > 512 || strings.ContainsAny(s.UserAgent, "\r\n") {
		return optionError{"user_agent must be one line of at most 512 characters"}
	}
	if s.Locale != "" && !localeRe.MatchString(s.Locale) {
		return optionError{"locale must be a language tag like en-US or fr-FR"}
	}
	if s.Timezone != "" {
		if _, err := time.LoadLocation(s.Timezone); err != nil || !strings.Contains(s.Timezone, "/") && s.Timezone != "UTC" {
			return optionError{"timezone must be an IANA name like America/New_York"}
		}
	}
	if s.TimeoutMinutes < 0 || s.TimeoutMinutes > maxSessionMinutes {
		return optionError{fmt.Sprintf("timeout_minutes must be 1-%d", maxSessionMinutes)}
	}
	if p := s.Proxy; p != nil {
		server := strings.TrimPrefix(strings.TrimPrefix(p.Server, "http://"), "https://")
		host, port, err := net.SplitHostPort(server)
		if n, perr := strconv.Atoi(port); err != nil || host == "" || perr != nil || n < 1 || n > 65535 {
			return optionError{"proxy.server must be host:port (an HTTP proxy that supports CONNECT)"}
		}
		if ip, err := netip.ParseAddr(host); err == nil && !gateway.PublicAddr(ip) {
			return optionError{"proxy.server must be a public address"}
		}
		if len(p.Username) > 256 || len(p.Password) > 256 {
			return optionError{"proxy username and password must be at most 256 characters"}
		}
		p.Server = server
	}
	return nil
}

func (s SessionOptions) ttl() time.Duration {
	if s.TimeoutMinutes > 0 {
		return time.Duration(s.TimeoutMinutes) * time.Minute
	}
	return sessionLifetime
}

func (s SessionOptions) settings() gateway.Settings {
	return gateway.Settings{UserAgent: s.UserAgent, Locale: s.Locale, Timezone: s.Timezone, Viewport: s.Viewport, Proxy: s.Proxy}
}

func (s SessionOptions) empty() bool {
	return s.Viewport == nil && s.UserAgent == "" && s.Locale == "" && s.Timezone == "" && s.Proxy == nil
}

// public is s as the API shows it: the proxy's password never leaves.
func (s SessionOptions) public() *SessionOptions {
	if s.empty() && s.TimeoutMinutes == 0 {
		return nil
	}
	if s.Proxy != nil {
		p := *s.Proxy
		if p.Password != "" {
			p.Password = "********"
		}
		s.Proxy = &p
	}
	return &s
}

func (st *Store) saveOptions(sid string, s SessionOptions) {
	b, _ := json.Marshal(s)
	_, _ = st.db.Exec(`UPDATE sessions SET options = ? WHERE id = ?`, string(b), sid)
}

func (st *Store) optionsOf(sid string) SessionOptions {
	var raw string
	var s SessionOptions
	if st.db.QueryRow(`SELECT options FROM sessions WHERE id = ?`, sid).Scan(&raw) == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &s)
	}
	return s
}

// applyOptions puts a session's settings in place for its VM: the proxy
// starts holding them (and, with wait, answers once the open tabs have them),
// and a proxied VM's address joins the egress set.
func (o *Orchestrator) applyOptions(ctx context.Context, vm api.VM, s SessionOptions, wait time.Duration) error {
	if s.empty() {
		return nil
	}
	if s.Proxy != nil {
		if err := o.egress("add", vm.NetIndex); err != nil {
			return err
		}
	}
	if o.cfg.ProxyAdmin == "" {
		return nil
	}
	b, _ := json.Marshal(s.settings())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("%s/settings/%d?wait=%s", o.cfg.ProxyAdmin, vm.NetIndex, wait), bytes.NewReader(b))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("browser settings: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		var msg bytes.Buffer
		_, _ = msg.ReadFrom(resp.Body)
		return fmt.Errorf("browser settings: %s", strings.TrimSpace(msg.String()))
	}
	return nil
}

// dropOptions undoes applyOptions when a session ends.
func (o *Orchestrator) dropOptions(vm api.VM) {
	if o.store.optionsOf(vm.ID).empty() {
		return
	}
	_ = o.egress("delete", vm.NetIndex)
	if o.cfg.ProxyAdmin != "" {
		req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/settings/%d", o.cfg.ProxyAdmin, vm.NetIndex), nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}
}

// holdOptions detaches the CDP proxy's own DevTools session from vm's browser
// before a snapshot of it (fork, pause, profile), so that no copy wakes up
// with a client that is no longer there (gateway/settings.go). The returned
// func brings the session back.
func (o *Orchestrator) holdOptions(vm api.VM) (release func()) {
	if o.cfg.ProxyAdmin == "" || o.store.optionsOf(vm.ID).empty() {
		return func() {}
	}
	post := func(what string) {
		resp, err := http.Post(fmt.Sprintf("%s/settings/%d/%s", o.cfg.ProxyAdmin, vm.NetIndex, what), "", nil)
		if err != nil {
			log.Printf("%s: settings %s: %v", vm.ID, what, err)
			return
		}
		resp.Body.Close()
	}
	post("hold")
	return func() { post("release") }
}

// startWith creates a session's browser with its options in place: the
// session row records them, and the API answers only once the browser has
// them, so a client never sees the defaults first. If they cannot be applied,
// the session is ended rather than handed out without them.
func (o *Orchestrator) startWith(ctx context.Context, vm api.VM, s SessionOptions) error {
	o.store.saveOptions(vm.ID, s)
	if err := o.applyOptions(ctx, vm, s, 5*time.Second); err != nil {
		o.store.endSessionRecord(vm.ID, "settings could not be applied")
		_ = o.Delete(context.Background(), vm.ID)
		return err
	}
	return nil
}

// egressTable is the nftables state behind the customer proxies: a proxied
// VM's new TCP connections go to the CDP proxy's egress listener, and its UDP
// other than DNS goes nowhere (QUIC and WebRTC would bypass the proxy).
var egressTable = fmt.Sprintf(`add table ip baton-egress
add set ip baton-egress proxied { type ipv4_addr; }
add chain ip baton-egress prerouting { type nat hook prerouting priority dstnat; policy accept; }
add chain ip baton-egress forward { type filter hook forward priority filter; policy accept; }
flush chain ip baton-egress prerouting
flush chain ip baton-egress forward
add rule ip baton-egress prerouting ip saddr @proxied meta l4proto tcp redirect to :%d
add rule ip baton-egress forward ip saddr @proxied meta l4proto udp udp dport != 53 drop
`, gateway.EgressPort)

// ensureEgress creates the egress table (idempotent) and puts back the
// addresses of every live proxied session: nftables state does not survive a
// reboot, the session database does.
func (o *Orchestrator) ensureEgress() {
	if o.cfg.ProxyAdmin == "" {
		return
	}
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(egressTable)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("egress table: %v: %s", err, bytes.TrimSpace(out))
		return
	}
	o.reapplyOptions(context.Background())
}

// egress adds VM n's address to the proxied set, or deletes it.
func (o *Orchestrator) egress(op string, n int) error {
	if o.cfg.ProxyAdmin == "" {
		return nil
	}
	nn, err := netsetup.ForIndex(n)
	if err != nil {
		return err
	}
	out, err := exec.Command("nft", op, "element", "ip", "baton-egress", "proxied", "{", nn.GuestIP.String(), "}").CombinedOutput()
	if err != nil && op == "add" {
		return fmt.Errorf("egress: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// reapplyOptions pushes the settings of every running session that has any, so
// that a restarted CDP proxy (which keeps them only in memory) gets them back.
// Pushing settings the proxy already holds changes nothing.
func (o *Orchestrator) reapplyOptions(ctx context.Context) {
	rows, err := o.store.db.Query(`SELECT id, options FROM sessions WHERE ended_at = '' AND options != ''`)
	if err != nil {
		return
	}
	type row struct {
		id string
		s  SessionOptions
	}
	var live []row
	for rows.Next() {
		var r row
		var raw string
		if rows.Scan(&r.id, &raw) == nil && json.Unmarshal([]byte(raw), &r.s) == nil && !r.s.empty() {
			live = append(live, r)
		}
	}
	rows.Close()
	for _, r := range live {
		vm, err := o.store.GetVM(r.id)
		if err != nil || (vm.State != StateRunning && vm.State != StateMigrating) { // a paused one has none until it resumes
			continue
		}
		if err := o.applyOptions(ctx, vm, r.s, 0); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("%s: %v", r.id, err)
		}
	}
}
