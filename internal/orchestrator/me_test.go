package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMeIsTheAccountOfTheKey(t *testing.T) {
	o := warmTest(t)
	if err := o.store.accountSchema(); err != nil {
		t.Fatal(err)
	}
	users := http.NewServeMux()
	users.HandleFunc("POST /v1/users", o.upsertUser)
	users.HandleFunc("POST /v1/users/{uid}/keys", o.createKey)
	do := func(h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	var u struct{ ID string }
	_ = json.Unmarshal(do(users, "POST", "/v1/users", `{"github_id": 7, "login": "x"}`, nil).Body.Bytes(), &u)
	var k struct{ Key string }
	_ = json.Unmarshal(do(users, "POST", "/v1/users/"+u.ID+"/keys", `{}`, nil).Body.Bytes(), &k)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/users/{uid}/sessions/{sid}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.PathValue("uid") + " " + r.PathValue("sid")))
	})
	h := o.resolveMe(mux)
	if w := do(h, "GET", "/v1/users/me/sessions/vm-1", "", map[string]string{"X-Baton-Key": k.Key}); w.Body.String() != u.ID+" vm-1" {
		t.Fatalf("me with a valid key: %d %q, want %q", w.Code, w.Body.String(), u.ID+" vm-1")
	}
	if w := do(h, "GET", "/v1/users/me/sessions/vm-1", "", map[string]string{"X-Baton-Key": "bk_wrong"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("me with a wrong key: %d, want 401", w.Code)
	}
	if w := do(h, "GET", "/v1/users/"+u.ID+"/sessions/vm-2", "", nil); w.Body.String() != u.ID+" vm-2" {
		t.Fatalf("explicit uid is passed through unchanged: %q", w.Body.String())
	}
}
