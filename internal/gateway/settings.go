package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/vedantxn/chromebox/internal/netsetup"
)

// Settings are what a session asked for beyond a default Chrome: how the
// browser presents itself (user agent, locale, timezone, window size) and
// the proxy its traffic should leave through.
//
// The browser is already running when a session gets it (a warm one, or a
// restore of a snapshot), so none of this can be a Chrome launch flag. The
// presentation settings are DevTools overrides instead, which last only as
// long as the DevTools session that set them. So the proxy keeps a session of
// its own to every such browser, for as long as the setting holds: it attaches
// to every tab of the default browser context (the one a client's
// connectOverCDP uses), new ones included, and sets the overrides before the
// tab runs any script. The connection goes to the VM's fixed address like a
// client's, so it survives a migration; when the browser goes away (a pause),
// it reconnects as soon as the browser is back.
type Settings struct {
	UserAgent string    `json:"user_agent,omitempty"`
	Locale    string    `json:"locale,omitempty"`   // BCP 47, e.g. fr-FR
	Timezone  string    `json:"timezone,omitempty"` // IANA, e.g. Europe/Paris
	Viewport  *Viewport `json:"viewport,omitempty"`
	Proxy     *Upstream `json:"proxy,omitempty"`
}

type Viewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Upstream is a customer's HTTP proxy (CONNECT), with optional basic auth.
type Upstream struct {
	Server   string `json:"server"` // host:port
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// emulates reports whether s needs a DevTools session at all (a proxy alone
// does not: that is done in the network, see egress.go).
func (s Settings) emulates() bool {
	return s.UserAgent != "" || s.Locale != "" || s.Timezone != "" || s.Viewport != nil
}

// settingsManager keeps one browser's overrides in place.
type settingsManager struct {
	n      int
	s      Settings
	cancel context.CancelFunc

	mu      sync.Mutex
	applied chan struct{} // closed once the tabs that exist have their overrides
}

// SetSettings starts applying s to VM n's browser (replacing what it had) and,
// if wait is set, returns once every open tab has it, or with an error after
// timeout.
func (p *Proxy) SetSettings(n int, s Settings, wait time.Duration) error {
	p.mu.Lock()
	if p.settings == nil {
		p.settings = map[int]*settingsManager{}
	}
	old := p.settings[n]
	if old != nil && settingsEqual(old.s, s) {
		p.mu.Unlock()
		return old.wait(wait)
	}
	if old != nil {
		old.cancel()
		delete(p.settings, n)
	}
	m := &settingsManager{n: n, s: s, applied: make(chan struct{})}
	p.settings[n] = m
	p.mu.Unlock()
	if !s.emulates() {
		close(m.applied)
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.run(ctx)
	return m.wait(wait)
}

// DropSettings forgets VM n's settings and closes the proxy's own session.
func (p *Proxy) DropSettings(n int) {
	p.mu.Lock()
	m := p.settings[n]
	delete(p.settings, n)
	p.mu.Unlock()
	if m != nil && m.cancel != nil {
		m.cancel()
	}
}

// settingsFor returns VM n's settings, if any.
func (p *Proxy) settingsFor(n int) (Settings, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	m, ok := p.settings[n]
	if !ok {
		return Settings{}, false
	}
	return m.s, true
}

func settingsEqual(a, b Settings) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func (m *settingsManager) wait(d time.Duration) error {
	if d <= 0 {
		return nil
	}
	m.mu.Lock()
	ch := m.applied
	m.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-time.After(d):
		return fmt.Errorf("browser settings not applied within %s", d)
	}
}

// run holds a DevTools session to the browser until ctx ends, starting a new
// one whenever the old one ends (the browser was paused, or restarted).
func (m *settingsManager) run(ctx context.Context) {
	for ctx.Err() == nil {
		err := m.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("vm %d: browser settings: %v (retrying)", m.n, err)
		}
		// Applied again on the next connection: until then, anyone waiting waits.
		m.mu.Lock()
		select {
		case <-m.applied:
			m.applied = make(chan struct{})
		default:
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// cdpSession is a DevTools connection on which several flattened sessions
// send commands and wait for their answers at once.
type cdpSession struct {
	c       *websocket.Conn
	ctx     context.Context
	id      atomic.Int64
	wmu     sync.Mutex
	pmu     sync.Mutex
	pending map[int64]chan cdpReply
}

type cdpReply struct {
	Result json.RawMessage
	Err    string
}

func (s *cdpSession) call(session, method string, params any) (json.RawMessage, error) {
	id := s.id.Add(1)
	ch := make(chan cdpReply, 1)
	s.pmu.Lock()
	s.pending[id] = ch
	s.pmu.Unlock()
	msg := map[string]any{"id": id, "method": method}
	if session != "" {
		msg["sessionId"] = session
	}
	if params != nil {
		msg["params"] = params
	}
	b, _ := json.Marshal(msg)
	s.wmu.Lock()
	err := s.c.Write(s.ctx, websocket.MessageText, b)
	s.wmu.Unlock()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	select {
	case r := <-ch:
		if r.Err != "" {
			return nil, fmt.Errorf("%s: %s", method, r.Err)
		}
		return r.Result, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: no answer", method)
	}
}

func (m *settingsManager) session(ctx context.Context) error {
	nn, err := netsetup.ForIndex(m.n)
	if err != nil {
		return err
	}
	addr := netip.AddrPortFrom(nn.GuestIP, cdpPort).String()
	path, err := browserPath(ctx, addr)
	if err != nil {
		return err
	}
	c, _, err := websocket.Dial(ctx, "ws://"+addr+path, nil)
	if err != nil {
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(32 << 20)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := &cdpSession{c: c, ctx: ctx, pending: map[int64]chan cdpReply{}}

	var ua string // Chrome's own user agent, when only the locale changes (set below)

	var (
		applying   sync.WaitGroup
		defaultCtx atomic.Value // the browser context of the first page: the default one
	)
	apply := func(sid string, info targetInfo, waiting bool) {
		defer applying.Done()
		if info.Type == "page" {
			if d, ok := defaultCtx.Load().(string); !ok {
				defaultCtx.Store(info.BrowserContextID)
				m.override(s, sid, ua)
			} else if d == info.BrowserContextID {
				m.override(s, sid, ua)
			}
		}
		if waiting {
			_, _ = s.call(sid, "Runtime.runIfWaitingForDebugger", nil)
		}
	}

	readErr := make(chan error, 1)
	go func() {
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				readErr <- err
				return
			}
			var msg struct {
				ID     int64           `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(b, &msg) != nil {
				continue
			}
			if msg.ID != 0 {
				s.pmu.Lock()
				ch := s.pending[msg.ID]
				delete(s.pending, msg.ID)
				s.pmu.Unlock()
				if ch != nil {
					r := cdpReply{Result: msg.Result}
					if msg.Error != nil {
						r.Err = msg.Error.Message
					}
					ch <- r
				}
				continue
			}
			if msg.Method == "Target.attachedToTarget" {
				var ev struct {
					SessionID          string     `json:"sessionId"`
					TargetInfo         targetInfo `json:"targetInfo"`
					WaitingForDebugger bool       `json:"waitingForDebugger"`
				}
				if json.Unmarshal(msg.Params, &ev) == nil {
					applying.Add(1)
					go apply(ev.SessionID, ev.TargetInfo, ev.WaitingForDebugger)
				}
			}
		}
	}()

	if m.s.UserAgent == "" && m.s.Locale != "" {
		// An Accept-Language override goes with a user agent: keep Chrome's own.
		if r, err := s.call("", "Browser.getVersion", nil); err == nil {
			var v struct {
				UserAgent string `json:"userAgent"`
			}
			_ = json.Unmarshal(r, &v)
			ua = v.UserAgent
		}
	}
	// Existing tabs are announced before the answer to setAutoAttach; new ones
	// wait for the debugger until their overrides are in.
	if _, err := s.call("", "Target.setAutoAttach", map[string]any{
		"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true,
	}); err != nil {
		return err
	}
	applying.Wait()
	m.mu.Lock()
	select {
	case <-m.applied:
	default:
		close(m.applied)
	}
	m.mu.Unlock()
	log.Printf("vm %d: browser settings applied", m.n)
	return <-readErr
}

type targetInfo struct {
	Type             string `json:"type"`
	BrowserContextID string `json:"browserContextId"`
}

// override sets the overrides on one tab. A failure (the tab closed meanwhile)
// is logged and the tab skipped.
func (m *settingsManager) override(s *cdpSession, sid, defaultUA string) {
	set := func(method string, params map[string]any) {
		if _, err := s.call(sid, method, params); err != nil {
			log.Printf("vm %d: %v", m.n, err)
		}
	}
	if m.s.UserAgent != "" || m.s.Locale != "" {
		p := map[string]any{"userAgent": m.s.UserAgent}
		if m.s.UserAgent == "" {
			p["userAgent"] = defaultUA
		}
		if m.s.Locale != "" {
			p["acceptLanguage"] = m.s.Locale
		}
		set("Emulation.setUserAgentOverride", p)
	}
	if m.s.Locale != "" {
		set("Emulation.setLocaleOverride", map[string]any{"locale": m.s.Locale})
	}
	if m.s.Timezone != "" {
		set("Emulation.setTimezoneOverride", map[string]any{"timezoneId": m.s.Timezone})
	}
	if v := m.s.Viewport; v != nil {
		set("Emulation.setDeviceMetricsOverride", map[string]any{"width": v.Width, "height": v.Height, "deviceScaleFactor": 0, "mobile": false})
	}
}
