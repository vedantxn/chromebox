package orchestrator

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vedantxn/chromebox/internal/api"
)

// Per-account limits. They are checked under the placement lock, in the same
// step that reserves the VM, so two parallel requests cannot both slip under a
// limit.
const (
	maxConcurrent    = 2
	maxCreatesPerMin = 10
	maxMinutesPerDay = 60
	sessionLifetime  = 15 * time.Minute
)

// accountSchema adds the product tables to the orchestrator's database: users
// (from GitHub), their API keys (only a hash is kept), and one row per browser
// session for limits and history.
func (s *Store) accountSchema() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY, github_id INTEGER NOT NULL UNIQUE, login TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL, prefix TEXT NOT NULL,
			hash TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, revoked_at TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL, created_at TEXT NOT NULL,
			ended_at TEXT NOT NULL DEFAULT '', end_reason TEXT NOT NULL DEFAULT '', migrations INTEGER NOT NULL DEFAULT 0)`,
		eventSchema, eventIndex,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashKey(k string) string { h := sha256.Sum256([]byte(k)); return hex.EncodeToString(h[:]) }

// Session is a browser session as the account owner sees it.
type Session struct {
	ID         string `json:"id"`
	Status     string `json:"status"` // running, migrating, ended
	Node       string `json:"node,omitempty"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	EndedAt    string `json:"ended_at,omitempty"`
	EndReason  string `json:"end_reason,omitempty"`
	Migrations int    `json:"migrations"`
	ConnectURL string `json:"connect_url,omitempty"` // only when the session is created
}

func (o *Orchestrator) accountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/users", o.upsertUser)
	mux.HandleFunc("POST /v1/keys/verify", o.verifyKey)
	mux.HandleFunc("GET /v1/users/{uid}/keys", o.listKeys)
	mux.HandleFunc("POST /v1/users/{uid}/keys", o.createKey)
	mux.HandleFunc("DELETE /v1/users/{uid}/keys/{kid}", o.revokeKey)
	mux.HandleFunc("GET /v1/users/{uid}/sessions", o.listSessions)
	mux.HandleFunc("POST /v1/users/{uid}/sessions", o.createSession)
	mux.HandleFunc("GET /v1/users/{uid}/sessions/{sid}", o.getSession)
	mux.HandleFunc("GET /v1/users/{uid}/sessions/{sid}/events", o.sessionEvents)
	mux.HandleFunc("DELETE /v1/users/{uid}/sessions/{sid}", o.endSession)
	mux.HandleFunc("POST /v1/users/{uid}/sessions/{sid}/migrate", o.migrateSession)
	mux.HandleFunc("POST /v1/users/{uid}/sessions/{sid}/fork", o.forkSession)
	mux.HandleFunc("POST /v1/users/{uid}/sessions/{sid}/token", o.sessionToken)
	mux.HandleFunc("GET /v1/users/{uid}/usage", o.usage)
}

func (o *Orchestrator) upsertUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GitHubID int64  `json:"github_id"`
		Login    string `json:"login"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.GitHubID == 0 {
		http.Error(w, "need github_id", http.StatusBadRequest)
		return
	}
	id := "u_" + randHex(6)
	_, err := o.store.db.Exec(`INSERT INTO users (id, github_id, login, created_at) VALUES (?,?,?,?)
		ON CONFLICT(github_id) DO UPDATE SET login = excluded.login`, id, in.GitHubID, in.Login, now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = o.store.db.QueryRow(`SELECT id FROM users WHERE github_id = ?`, in.GitHubID).Scan(&id)
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "login": in.Login})
}

// createKey makes an API key. The key itself is returned once and never stored:
// only its SHA-256 and a short prefix (to tell keys apart in the dashboard).
func (o *Orchestrator) createKey(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.Name == "" {
		in.Name = "default"
	}
	key := "bk_" + randHex(24)
	id := "k_" + randHex(6)
	if _, err := o.store.db.Exec(`INSERT INTO api_keys (id, user_id, name, prefix, hash, created_at) VALUES (?,?,?,?,?,?)`,
		id, r.PathValue("uid"), in.Name, key[:11], hashKey(key), now()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "key": key, "prefix": key[:11], "name": in.Name})
}

func (o *Orchestrator) listKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := o.store.db.Query(`SELECT id, name, prefix, created_at, revoked_at FROM api_keys WHERE user_id = ? ORDER BY created_at DESC`, r.PathValue("uid"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, prefix, created, revoked string
		_ = rows.Scan(&id, &name, &prefix, &created, &revoked)
		out = append(out, map[string]any{"id": id, "name": name, "prefix": prefix, "created_at": created, "revoked": revoked != ""})
	}
	writeJSON(w, http.StatusOK, out)
}

func (o *Orchestrator) revokeKey(w http.ResponseWriter, r *http.Request) {
	_, _ = o.store.db.Exec(`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND user_id = ?`, now(), r.PathValue("kid"), r.PathValue("uid"))
	w.WriteHeader(http.StatusNoContent)
}

func (o *Orchestrator) verifyKey(w http.ResponseWriter, r *http.Request) {
	var in struct{ Key string }
	_ = json.NewDecoder(r.Body).Decode(&in)
	var uid, login string
	err := o.store.db.QueryRow(`SELECT k.user_id, u.login FROM api_keys k JOIN users u ON u.id = k.user_id
		WHERE k.hash = ? AND k.revoked_at = ''`, hashKey(in.Key)).Scan(&uid, &login)
	if err != nil {
		http.Error(w, "invalid API key", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"user_id": uid, "login": login})
}

// limitError is returned when an account is over one of its limits.
type limitError struct{ msg string }

func (e limitError) Error() string { return e.msg }

// checkLimits must be called with o.place held.
func (o *Orchestrator) checkLimits(uid string) error {
	var running, recent int
	_ = o.store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND ended_at = ''`, uid).Scan(&running)
	if running >= maxConcurrent {
		return limitError{fmt.Sprintf("limit: at most %d sessions at a time; end one first", maxConcurrent)}
	}
	_ = o.store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND created_at > ?`, uid,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)).Scan(&recent)
	if recent >= maxCreatesPerMin {
		return limitError{fmt.Sprintf("limit: at most %d sessions per minute", maxCreatesPerMin)}
	}
	if used := o.minutesToday(uid); used >= maxMinutesPerDay {
		return limitError{fmt.Sprintf("limit: %d browser minutes per day used up", maxMinutesPerDay)}
	}
	return nil
}

func (o *Orchestrator) minutesToday(uid string) float64 {
	day := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339Nano)
	rows, err := o.store.db.Query(`SELECT created_at, ended_at FROM sessions WHERE user_id = ? AND created_at >= ?`, uid, day)
	if err != nil {
		return 0
	}
	defer rows.Close()
	var total float64
	for rows.Next() {
		var c, e string
		_ = rows.Scan(&c, &e)
		start, _ := time.Parse(time.RFC3339Nano, c)
		end := time.Now()
		if e != "" {
			end, _ = time.Parse(time.RFC3339Nano, e)
		}
		total += end.Sub(start).Minutes()
	}
	return total
}

func (o *Orchestrator) createSession(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	vm, err := o.CreateFor(r.Context(), uid, "chrome-v1", sessionLifetime)
	if err != nil {
		code := http.StatusInternalServerError
		var le limitError
		switch {
		case errors.As(err, &le):
			code = http.StatusTooManyRequests
		case strings.HasPrefix(err.Error(), "no node"):
			code = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), code)
		return
	}
	s := o.sessionView(vm.ID, uid)
	s.ConnectURL = o.connectURL(vm, "cdp", sessionLifetime)
	writeJSON(w, http.StatusCreated, s)
}

// CreateFor is Create for an account: the limits are checked, and the session
// row written, under the same lock that places the VM.
func (o *Orchestrator) CreateFor(ctx context.Context, uid, base string, ttl time.Duration) (api.VM, error) {
	vm, _, err := o.Create(ctx, base, ttl, o.reserveFor(uid))
	return vm, err
}

// reserveFor runs while Create holds the placement lock: check the account's
// limits and record the session in one step.
func (o *Orchestrator) reserveFor(uid string) func(id string) error {
	return func(id string) error {
		if err := o.checkLimits(uid); err != nil {
			return err
		}
		_, err := o.store.db.Exec(`INSERT INTO sessions (id, user_id, created_at) VALUES (?,?,?)`, id, uid, now())
		return err
	}
}

// forkSession copies one of the account's running sessions into count new
// ones (M6). Each copy counts against the limits like any session. The limits
// are checked once before the parent is paused, so a fork that would be
// refused never pauses anyone, and again for each copy as it is placed.
func (o *Orchestrator) forkSession(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	if _, err := o.owned(uid, r.PathValue("sid")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var req struct{ Count int }
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Count < 1 || req.Count > 16 {
		http.Error(w, "count must be 1-16", http.StatusBadRequest)
		return
	}
	o.place.Lock()
	err := o.checkLimits(uid)
	o.place.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	res, err := o.Fork(r.Context(), r.PathValue("sid"), req.Count, sessionLifetime, o.reserveFor(uid))
	if err != nil {
		code := http.StatusConflict
		if strings.Contains(err.Error(), "limit:") {
			code = http.StatusTooManyRequests
		}
		http.Error(w, err.Error(), code)
		return
	}
	out := map[string]any{"parent": res.Parent, "paused_ms": res.PausedMs, "total_ms": res.TotalMs, "errors": res.Errors}
	var sessions []Session
	for _, vm := range res.Copies {
		s := o.sessionView(vm.ID, uid)
		s.ConnectURL = o.connectURL(vm, "cdp", sessionLifetime)
		sessions = append(sessions, s)
	}
	out["sessions"] = sessions
	writeJSON(w, http.StatusCreated, out)
}

func (o *Orchestrator) connectURL(vm api.VM, kind string, ttl time.Duration) string {
	pub := strings.Replace(strings.Replace(o.cfg.PublicURL, "https://", "wss://", 1), "http://", "ws://", 1)
	return pub + "/cdp/" + o.sign(vm, kind, ttl)
}

// owned returns the session's VM if it belongs to uid.
func (o *Orchestrator) owned(uid, sid string) (api.VM, error) {
	var owner string
	if err := o.store.db.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, sid).Scan(&owner); err != nil || owner != uid {
		return api.VM{}, errors.New("no such session")
	}
	return o.store.GetVM(sid)
}

func (o *Orchestrator) sessionView(sid, uid string) Session {
	s := Session{ID: sid}
	var ended, reason string
	_ = o.store.db.QueryRow(`SELECT created_at, ended_at, end_reason, migrations FROM sessions WHERE id = ? AND user_id = ?`, sid, uid).
		Scan(&s.CreatedAt, &ended, &reason, &s.Migrations)
	s.EndedAt, s.EndReason = ended, reason
	if ended != "" {
		s.Status = "ended"
		return s
	}
	if vm, err := o.store.GetVM(sid); err == nil {
		s.Status, s.Node, s.ExpiresAt = vm.State, vm.Node, vm.Expires
	}
	return s
}

func (o *Orchestrator) listSessions(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	rows, err := o.store.db.Query(`SELECT id FROM sessions WHERE user_id = ? ORDER BY created_at DESC LIMIT 50`, uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	out := []Session{}
	for _, id := range ids {
		out = append(out, o.sessionView(id, uid))
	}
	writeJSON(w, http.StatusOK, out)
}

func (o *Orchestrator) getSession(w http.ResponseWriter, r *http.Request) {
	if _, err := o.owned(r.PathValue("uid"), r.PathValue("sid")); err != nil {
		var exists int
		_ = o.store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ? AND user_id = ?`, r.PathValue("sid"), r.PathValue("uid")).Scan(&exists)
		if exists == 0 {
			http.Error(w, "no such session", http.StatusNotFound)
			return
		}
	}
	writeJSON(w, http.StatusOK, o.sessionView(r.PathValue("sid"), r.PathValue("uid")))
}

func (o *Orchestrator) endSession(w http.ResponseWriter, r *http.Request) {
	if _, err := o.owned(r.PathValue("uid"), r.PathValue("sid")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := o.Delete(r.Context(), r.PathValue("sid")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (o *Orchestrator) migrateSession(w http.ResponseWriter, r *http.Request) {
	if _, err := o.owned(r.PathValue("uid"), r.PathValue("sid")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	res, err := o.Migrate(r.Context(), r.PathValue("sid"), "")
	if err != nil {
		res.Error = err.Error()
		writeJSON(w, http.StatusConflict, res)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (o *Orchestrator) sessionToken(w http.ResponseWriter, r *http.Request) {
	vm, err := o.owned(r.PathValue("uid"), r.PathValue("sid"))
	if err != nil || (vm.State != StateRunning && vm.State != StateMigrating) {
		http.Error(w, "no running session", http.StatusNotFound)
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "view"
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": o.connectURL(vm, kind, 10*time.Minute)})
}

func (o *Orchestrator) usage(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	var running int
	_ = o.store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND ended_at = ''`, uid).Scan(&running)
	writeJSON(w, http.StatusOK, map[string]any{
		"running": running, "max_running": maxConcurrent,
		"minutes_today": o.minutesToday(uid), "max_minutes_per_day": maxMinutesPerDay,
		"session_lifetime_minutes": sessionLifetime.Minutes(),
	})
}

// endSessionRecord closes the session row of VM id (no-op if it has none).
func (s *Store) endSessionRecord(id, reason string) {
	res, err := s.db.Exec(`UPDATE sessions SET ended_at = ?, end_reason = ? WHERE id = ? AND ended_at = ''`, now(), reason, id)
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			s.addEvent(id, "ended", map[string]any{"reason": reason})
		}
	}
}

func (s *Store) countMigration(id string) {
	_, _ = s.db.Exec(`UPDATE sessions SET migrations = migrations + 1 WHERE id = ?`, id)
}

var _ = sql.ErrNoRows
