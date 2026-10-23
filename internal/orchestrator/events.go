package orchestrator

import (
	"encoding/json"
	"net/http"
	"time"
)

// A session's event log: one row per thing that happened to a browser, so that
// "what did my session go through" survives a page reload and a restart. The
// dashboard's timeline and GET /v1/users/{uid}/sessions/{sid}/events read it.
//
//	created       placed on a node (node, base, how long the create took)
//	forked_from   made as a copy of another session
//	forked        copied into n new sessions (n, parent pause)
//	migrated      moved between nodes (from, to, paused_ms, unreachable_ms, stream_ms)
//	migration_failed
//	paused / resumed (S1)
//	ended         why it ended
type Event struct {
	At     string         `json:"at"`
	Kind   string         `json:"kind"`
	Detail map[string]any `json:"detail,omitempty"`
}

const eventSchema = `CREATE TABLE IF NOT EXISTS events (
	id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, at TEXT NOT NULL,
	kind TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '')`

const eventIndex = `CREATE INDEX IF NOT EXISTS events_session ON events (session_id, id)`

// addEvent records an event for session id. Failures to record never fail the
// operation they describe.
func (s *Store) addEvent(id, kind string, detail map[string]any) {
	var d string
	if len(detail) > 0 {
		b, _ := json.Marshal(detail)
		d = string(b)
	}
	_, _ = s.db.Exec(`INSERT INTO events (session_id, at, kind, detail) VALUES (?,?,?,?)`, id, now(), kind, d)
}

// events returns session id's events, oldest first.
func (s *Store) events(id string) []Event {
	rows, err := s.db.Query(`SELECT at, kind, detail FROM events WHERE session_id = ? ORDER BY id LIMIT 500`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var d string
		if rows.Scan(&e.At, &e.Kind, &d) == nil {
			if d != "" {
				_ = json.Unmarshal([]byte(d), &e.Detail)
			}
			out = append(out, e)
		}
	}
	return out
}

func (o *Orchestrator) sessionEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := o.owned(r.PathValue("uid"), r.PathValue("sid")); err != nil {
		// A session that has ended is no longer a VM; check the session row.
		var n int
		_ = o.store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ? AND user_id = ?`, r.PathValue("sid"), r.PathValue("uid")).Scan(&n)
		if n == 0 {
			http.Error(w, "no such session", http.StatusNotFound)
			return
		}
	}
	writeJSON(w, http.StatusOK, o.store.events(r.PathValue("sid")))
}

// ms is a duration as milliseconds with microsecond precision, for event details.
func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
