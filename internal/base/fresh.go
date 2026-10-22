package base

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// freshTabs gives each web page open in a fork's copy a new renderer process,
// by opening its URL in a new tab and closing the old one.
//
// Why: a fork copies the browser's memory, and with it each renderer's random
// generator. JavaScript's Math.random in a tab that was open at the fork
// returned the same numbers in every copy (and a reload doesn't help: the new
// page's generator is seeded from the same copied process). A new tab gets a
// new process, seeded from the guest kernel, which reseeded itself when told it
// was cloned (VMGenID). Cookies and storage stay: they belong to the profile,
// not the process. What is lost is what a reload would lose (in-page state).
// crypto.getRandomValues was already different in every copy.
//
// It uses Chrome's plain HTTP DevTools endpoints, from the node to the guest.
func freshTabs(ctx context.Context, cdp string) (int, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	get := func(method, path string, out any) error {
		req, _ := http.NewRequestWithContext(ctx, method, "http://"+cdp+path, nil)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s %s: %s", method, path, resp.Status)
		}
		if out != nil {
			return json.NewDecoder(resp.Body).Decode(out)
		}
		return nil
	}
	var targets []struct{ ID, Type, URL string }
	if err := get(http.MethodGet, "/json/list", &targets); err != nil {
		return 0, err
	}
	n := 0
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}
		u, err := url.Parse(t.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue // about:blank and friends have no state worth keeping
		}
		if err := get(http.MethodPut, "/json/new?"+url.QueryEscape(t.URL), nil); err != nil {
			return n, err
		}
		if err := get(http.MethodGet, "/json/close/"+t.ID, nil); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
