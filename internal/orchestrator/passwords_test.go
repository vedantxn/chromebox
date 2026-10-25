package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func passwordMux(t *testing.T) (*Orchestrator, func(path, body string) *httptest.ResponseRecorder) {
	t.Helper()
	o := warmTest(t)
	if err := o.store.accountSchema(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/users", o.upsertUser)
	mux.HandleFunc("POST /v1/users/signup", o.signup)
	mux.HandleFunc("POST /v1/users/login", o.login)
	return o, func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return w
	}
}

func TestSignupThenLogin(t *testing.T) {
	o, post := passwordMux(t)
	w := post("/v1/users/signup", `{"email":" Ada@Example.com ","password":"correct horse"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("signup: %d %s", w.Code, w.Body)
	}
	var u struct{ ID, Login string }
	_ = json.Unmarshal(w.Body.Bytes(), &u)
	if u.Login != "ada@example.com" {
		t.Fatalf("login name %q, want the address in lower case", u.Login)
	}
	var stored string
	_ = o.store.db.QueryRow(`SELECT hash FROM passwords WHERE user_id = ?`, u.ID).Scan(&stored)
	if !strings.HasPrefix(stored, "argon2id$") || strings.Contains(stored, "correct horse") {
		t.Fatalf("stored %q, want an argon2id hash", stored)
	}

	if w := post("/v1/users/signup", `{"email":"ada@example.com","password":"another one"}`); w.Code != http.StatusConflict {
		t.Fatalf("second signup with the same address: %d, want 409", w.Code)
	}
	w = post("/v1/users/login", `{"email":"ADA@example.com","password":"correct horse"}`)
	var got struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != http.StatusOK || got.ID != u.ID {
		t.Fatalf("login: %d %s, want 200 and %s", w.Code, w.Body, u.ID)
	}
	wrong := post("/v1/users/login", `{"email":"ada@example.com","password":"wrong horse"}`)
	nobody := post("/v1/users/login", `{"email":"nobody@example.com","password":"correct horse"}`)
	if wrong.Code != http.StatusUnauthorized || nobody.Code != http.StatusUnauthorized || wrong.Body.String() != nobody.Body.String() {
		t.Fatalf("wrong password %d %q, unknown address %d %q: want the same 401", wrong.Code, wrong.Body, nobody.Code, nobody.Body)
	}
}

func TestSignupChecksItsInput(t *testing.T) {
	_, post := passwordMux(t)
	for _, body := range []string{
		`{"email":"not-an-address","password":"long enough"}`,
		`{"email":"a@b","password":"long enough"}`,
		`{"email":"Ada <ada@example.com>","password":"long enough"}`,
		`{"email":"ada@example.com","password":"short"}`,
	} {
		if w := post("/v1/users/signup", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", body, w.Code)
		}
	}
}

func TestLoginLocksAfterTooManyFailures(t *testing.T) {
	_, post := passwordMux(t)
	post("/v1/users/signup", `{"email":"ada@example.com","password":"correct horse"}`)
	for i := 0; i < maxFailedLogins; i++ {
		post("/v1/users/login", `{"email":"ada@example.com","password":"guess"}`)
	}
	if w := post("/v1/users/login", `{"email":"ada@example.com","password":"correct horse"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("right password after %d failures: %d, want 429", maxFailedLogins, w.Code)
	}
}

func TestSignupsPerAddressAreLimited(t *testing.T) {
	_, post := passwordMux(t)
	for i := 0; i < maxSignups; i++ {
		if w := post("/v1/users/signup", `{"email":"u`+string(rune('a'+i))+`@example.com","password":"long enough","ip":"203.0.113.9"}`); w.Code != http.StatusCreated {
			t.Fatalf("signup %d: %d", i, w.Code)
		}
	}
	if w := post("/v1/users/signup", `{"email":"late@example.com","password":"long enough","ip":"203.0.113.9"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("signup past the limit: %d, want 429", w.Code)
	}
}

// A database from before email accounts: github_id NOT NULL. Its users must
// survive, and GitHub sign-in must still find them.
func TestOldUsersTableIsRebuilt(t *testing.T) {
	s := openTest(t)
	if _, err := s.db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, github_id INTEGER NOT NULL UNIQUE, login TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	_, _ = s.db.Exec(`INSERT INTO users VALUES ('u_old', 42, 'octo', '2026-10-01T00:00:00Z')`)
	if err := s.accountSchema(); err != nil {
		t.Fatal(err)
	}
	if err := s.accountSchema(); err != nil { // and again, as on every start
		t.Fatal(err)
	}
	var login string
	if err := s.db.QueryRow(`SELECT login FROM users WHERE github_id = 42`).Scan(&login); err != nil || login != "octo" {
		t.Fatalf("old user after the rebuild: %q %v", login, err)
	}
	if _, err := s.db.Exec(`INSERT INTO users (id, login, created_at) VALUES ('u_new', 'a@b.co', 'now')`); err != nil {
		t.Fatalf("a user without github_id: %v", err)
	}
}
