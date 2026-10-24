package base

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Warm gets a freshly restored browser ready for its first user, over CDP at
// cdp (host:port, from the node).
//
// Why: a browser restored from a snapshot is cold. On our hosts every page of
// guest memory costs 12-17 µs the first time it is touched after a restore,
// so the first CDP replies and the first page load are slow (a first load of
// example.com ~490 ms, then ~130 ms). Warming before the snapshot does not
// help: a restore makes all of it cold again. Warming the running browser
// does: its processes have touched what a first connection and a first load
// need, and the pages they freed come back already mapped.
//
// Everything happens in a separate browser context that is thrown away at the
// end, so the default profile, the one the user gets, has no history, cookies
// or cache from it: open a page (pageURL, a page of ours), run some script in
// it, and dispose of the context.
func Warm(ctx context.Context, cdp, pageURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var version struct{ WebSocketDebuggerURL string }
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+cdp+"/json/version", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	err = json.NewDecoder(resp.Body).Decode(&version)
	resp.Body.Close()
	if err != nil {
		return err
	}
	u, err := url.Parse(version.WebSocketDebuggerURL)
	if err != nil {
		return err
	}
	u.Host = cdp // Chrome names its own loopback address; reach it the way we came

	c, _, err := websocket.Dial(ctx, u.String(), nil)
	if err != nil {
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(16 << 20)
	s := &cdpConn{c: c, seen: map[string]bool{}}

	// What a client's connect does first (Playwright's connectOverCDP sends these).
	if _, err := s.call(ctx, "", "Browser.getVersion", nil); err != nil {
		return err
	}
	if _, err := s.call(ctx, "", "Target.getTargets", nil); err != nil {
		return err
	}

	var bc struct{ BrowserContextID string }
	if err := s.callInto(ctx, "", "Target.createBrowserContext", map[string]any{"disposeOnDetach": true}, &bc); err != nil {
		return err
	}
	defer s.call(context.Background(), "", "Target.disposeBrowserContext", map[string]any{"browserContextId": bc.BrowserContextID})

	var t struct{ TargetID string }
	if err := s.callInto(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank", "browserContextId": bc.BrowserContextID}, &t); err != nil {
		return err
	}
	var at struct{ SessionID string }
	if err := s.callInto(ctx, "", "Target.attachToTarget", map[string]any{"targetId": t.TargetID, "flatten": true}, &at); err != nil {
		return err
	}
	if _, err := s.call(ctx, at.SessionID, "Page.enable", nil); err != nil {
		return err
	}
	if _, err := s.call(ctx, at.SessionID, "Page.navigate", map[string]any{"url": pageURL}); err != nil {
		return err
	}
	if err := s.waitEvent(ctx, at.SessionID, "Page.loadEventFired"); err != nil {
		return fmt.Errorf("warm page did not load: %w", err)
	}
	// Some script and layout of its own, beyond what the page runs.
	_, err = s.call(ctx, at.SessionID, "Runtime.evaluate", map[string]any{"expression": `(() => {
		const d = document.createElement('div'); document.body.appendChild(d);
		const o = []; for (let i = 0; i < 20000; i++) o.push({a: i, b: 'x' + i});
		for (let i = 0; i < 2000; i++) { const p = document.createElement('p'); p.textContent = JSON.stringify(o[i]); d.appendChild(p); }
		return d.getBoundingClientRect().height;
	})()`, "returnByValue": true})
	return err
}

// cdpConn is a minimal CDP client: one request at a time, events skipped
// unless waited for.
type cdpConn struct {
	c    *websocket.Conn
	id   atomic.Int64
	seen map[string]bool // events read while waiting for something else
}

type cdpMsg struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Params    any             `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (s *cdpConn) call(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	id := s.id.Add(1)
	b, _ := json.Marshal(cdpMsg{ID: id, Method: method, SessionID: session, Params: params})
	if err := s.c.Write(ctx, websocket.MessageText, b); err != nil {
		return nil, err
	}
	for {
		m, err := s.read(ctx)
		if err != nil {
			return nil, err
		}
		if m.ID == id {
			if m.Error != nil {
				return nil, fmt.Errorf("%s: %s", method, m.Error.Message)
			}
			return m.Result, nil
		}
	}
}

func (s *cdpConn) callInto(ctx context.Context, session, method string, params, out any) error {
	r, err := s.call(ctx, session, method, params)
	if err != nil {
		return err
	}
	return json.Unmarshal(r, out)
}

func (s *cdpConn) waitEvent(ctx context.Context, session, method string) error {
	if s.seen[session+" "+method] {
		return nil
	}
	for {
		m, err := s.read(ctx)
		if err != nil {
			return err
		}
		if m.Method == method && m.SessionID == session {
			return nil
		}
	}
}

func (s *cdpConn) read(ctx context.Context) (cdpMsg, error) {
	var m cdpMsg
	_, b, err := s.c.Read(ctx)
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(b, &m)
	if m.Method != "" {
		s.seen[m.SessionID+" "+m.Method] = true
	}
	return m, err
}
