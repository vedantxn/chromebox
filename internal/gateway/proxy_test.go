package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The discovery document must point the client at the gateway, not at the
// VM's private address, which the agent cannot reach.
func TestRewriteDiscovery(t *testing.T) {
	const in = `{"webSocketDebuggerUrl": "ws://10.200.0.6:9223/devtools/browser/abc",` +
		`"devtoolsFrontendUrl": "/devtools/inspector.html?ws=10.200.0.6:9223/devtools/page/1"}`
	req := httptest.NewRequest("GET", "http://x/json/version", nil)
	ctx := context.WithValue(req.Context(), keyVM, 1)
	ctx = context.WithValue(ctx, keyClientHost, "203.0.113.7:9222")
	resp := &http.Response{
		StatusCode: 200,
		Request:    req.WithContext(ctx),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(in)),
	}
	if err := rewriteDiscovery(resp); err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(resp.Body)
	got := string(out)
	for _, want := range []string{
		"ws://203.0.113.7:9222/v/1/devtools/browser/abc",
		"ws=203.0.113.7:9222/v/1/devtools/page/1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "10.200.0.6") {
		t.Errorf("private address leaked: %s", got)
	}
}

func TestHandlerRejectsOtherPaths(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/json/version", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/v/0/json/version", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("index 0: status = %d", rec.Code)
	}
}
