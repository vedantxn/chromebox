package orchestrator

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// Email and password accounts, next to the GitHub ones. A password is kept
// only as an argon2id hash (OWASP's parameters: 19 MiB, 2 passes, 1 thread);
// the site never sees one, it passes what the user typed straight through.
// There is no email verification yet: the address is whatever was typed.

const (
	minPassword = 8
	maxPassword = 128

	maxFailedLogins = 10 // per address, in loginWindow
	loginWindow     = 15 * time.Minute
	maxSignups      = 5 // per client address, in signupWindow
	signupWindow    = time.Hour
)

// passwordSchema lets a user exist without a GitHub account, and adds the
// password table. Users made before this had github_id NOT NULL; that table is
// rebuilt once, keeping every row.
func (s *Store) passwordSchema() error {
	var notNull int
	_ = s.db.QueryRow(`SELECT "notnull" FROM pragma_table_info('users') WHERE name = 'github_id'`).Scan(&notNull)
	if notNull == 1 {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range []string{
			`CREATE TABLE users_new (id TEXT PRIMARY KEY, github_id INTEGER UNIQUE, login TEXT NOT NULL, created_at TEXT NOT NULL)`,
			`INSERT INTO users_new (id, github_id, login, created_at) SELECT id, github_id, login, created_at FROM users`,
			`DROP TABLE users`,
			`ALTER TABLE users_new RENAME TO users`,
		} {
			if _, err := tx.Exec(stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("users without github_id: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS passwords (
		user_id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE, hash TEXT NOT NULL, created_at TEXT NOT NULL)`)
	return err
}

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	sum := argon2.IDKey([]byte(pw), salt, 2, 19*1024, 1, 32)
	b := base64.RawStdEncoding
	return fmt.Sprintf("argon2id$v=19$m=19456,t=2,p=1$%s$%s", b.EncodeToString(salt), b.EncodeToString(sum))
}

func checkPassword(pw, stored string) bool {
	var m uint32
	var t uint32
	var p uint8
	parts := strings.Split(stored, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false
	}
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	b := base64.RawStdEncoding
	salt, err1 := b.DecodeString(parts[3])
	want, err2 := b.DecodeString(parts[4])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// A hash to check against when the address has no account, so a wrong
// address takes as long to refuse as a wrong password. Made on first use, not
// at package init: argon2id takes ~28 ms on purpose, and every start of the
// chromebox binary would pay it, the page-fault handler a migration starts
// inside its pause among them (the pause went from 212 to 255 ms).
var dummyHash = sync.OnceValue(func() string { return hashPassword("not a real password") })

// attempts counts recent events per key (failed logins per address, signups
// per client) in memory. A restart forgets them, which is fine for a limit.
type attempts struct {
	mu sync.Mutex
	at map[string][]time.Time
}

// over reports whether key already has n events within window.
func (a *attempts) over(key string, n int, window time.Duration) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.recent(key, window)) >= n
}

func (a *attempts) add(key string, window time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.at[key] = append(a.recent(key, window), time.Now())
}

func (a *attempts) clear(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.at, key)
}

// recent prunes key's events older than window; call with a.mu held.
func (a *attempts) recent(key string, window time.Duration) []time.Time {
	if a.at == nil {
		a.at = map[string][]time.Time{}
	}
	keep := a.at[key][:0]
	for _, t := range a.at[key] {
		if time.Since(t) < window {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(a.at, key)
		return nil
	}
	a.at[key] = keep
	return keep
}

type passwordIn struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	IP       string `json:"ip"` // the visitor's address, as the site saw it; for the signup limit
}

func readPasswordIn(w http.ResponseWriter, r *http.Request) (passwordIn, string) {
	var in passwordIn
	// A form's worth at most: a huge body would otherwise be read, and hashed.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		return in, "bad request"
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if a, err := mail.ParseAddress(in.Email); err != nil || a.Address != in.Email || len(in.Email) > 254 || !strings.Contains(in.Email[strings.LastIndex(in.Email, "@"):], ".") {
		return in, "enter a valid email address"
	}
	return in, ""
}

// signup makes an account from an email address and a password.
func (o *Orchestrator) signup(w http.ResponseWriter, r *http.Request) {
	in, problem := readPasswordIn(w, r)
	if problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}
	if n := len([]rune(in.Password)); n < minPassword || n > maxPassword {
		http.Error(w, fmt.Sprintf("use a password of %d to %d characters", minPassword, maxPassword), http.StatusBadRequest)
		return
	}
	if in.IP != "" && o.tries.over("signup:"+in.IP, maxSignups, signupWindow) {
		http.Error(w, "too many new accounts from this address; try again later", http.StatusTooManyRequests)
		return
	}
	var taken int
	_ = o.store.db.QueryRow(`SELECT COUNT(*) FROM passwords WHERE email = ?`, in.Email).Scan(&taken)
	if taken > 0 {
		http.Error(w, "an account with this email already exists; sign in instead", http.StatusConflict)
		return
	}
	id, hash := "u_"+randHex(6), hashPassword(in.Password)
	tx, err := o.store.db.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = tx.Exec(`INSERT INTO users (id, login, created_at) VALUES (?,?,?)`, id, in.Email, now())
	if err == nil {
		_, err = tx.Exec(`INSERT INTO passwords (user_id, email, hash, created_at) VALUES (?,?,?,?)`, id, in.Email, hash, now())
	}
	if err != nil {
		_ = tx.Rollback()
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, "an account with this email already exists; sign in instead", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if in.IP != "" {
		o.tries.add("signup:"+in.IP, signupWindow)
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "login": in.Email})
}

// login checks an email address and password. Wrong address or wrong
// password get the same answer; after maxFailedLogins failures in a row an
// address is refused for a while, whatever the password.
func (o *Orchestrator) login(w http.ResponseWriter, r *http.Request) {
	in, problem := readPasswordIn(w, r)
	if problem != "" || len([]rune(in.Password)) > maxPassword {
		http.Error(w, "wrong email or password", http.StatusUnauthorized)
		return
	}
	key := "login:" + in.Email
	if o.tries.over(key, maxFailedLogins, loginWindow) {
		http.Error(w, "too many failed sign-ins for this email; try again in 15 minutes", http.StatusTooManyRequests)
		return
	}
	var id, hash string
	err := o.store.db.QueryRow(`SELECT user_id, hash FROM passwords WHERE email = ?`, in.Email).Scan(&id, &hash)
	if err != nil {
		hash = dummyHash()
	}
	if !checkPassword(in.Password, hash) || err != nil {
		o.tries.add(key, loginWindow)
		http.Error(w, "wrong email or password", http.StatusUnauthorized)
		return
	}
	o.tries.clear(key)
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "login": in.Email})
}
