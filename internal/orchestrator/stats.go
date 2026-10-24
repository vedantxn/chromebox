package orchestrator

import (
	"net/http"
	"sort"
	"time"
)

// GET /v1/stats: what the public status page shows. Counts and percentiles of
// the last hour come from the event log (not from process memory), so they
// survive an orchestrator restart. Nothing here names a user or a session.
type Pct struct {
	N   int     `json:"n"`
	P50 float64 `json:"p50_ms"`
	P95 float64 `json:"p95_ms"`
}

type StatsNode struct {
	Name     string `json:"name"`
	Up       bool   `json:"up"`
	Cordoned bool   `json:"cordoned"`
	State    string `json:"state,omitempty"`
	VMs      int    `json:"vms"`
	CapMiB   int    `json:"cap_mib"`
	UsedMiB  int    `json:"used_mib"`
}

type Stats struct {
	At          time.Time      `json:"at"`
	Nodes       []StatsNode    `json:"nodes"`
	Sessions    map[string]int `json:"sessions"` // running, paused
	LastHour    map[string]Pct `json:"last_hour"`
	Failures    map[string]int `json:"failures_last_hour"`
	Autoscaler  bool           `json:"autoscaler"`
	AutoActions []Action       `json:"autoscaler_actions"`
}

func pct(xs []float64) Pct {
	if len(xs) == 0 {
		return Pct{}
	}
	sort.Float64s(xs)
	q := func(p float64) float64 { return xs[min(len(xs)-1, int(float64(len(xs))*p))] }
	return Pct{N: len(xs), P50: q(0.5), P95: q(0.95)}
}

// values returns the numeric field of the given kind's events in the last hour.
func (s *Store) values(kind, field string, since time.Time) []float64 {
	rows, err := s.db.Query(`SELECT json_extract(detail, '$.`+field+`') FROM events WHERE kind = ? AND at > ? AND json_extract(detail, '$.`+field+`') IS NOT NULL`,
		kind, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var v float64
		if rows.Scan(&v) == nil {
			out = append(out, v)
		}
	}
	return out
}

func (s *Store) countEvents(kind string, since time.Time) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE kind = ? AND at > ?`, kind, since.UTC().Format(time.RFC3339Nano)).Scan(&n)
	return n
}

func (o *Orchestrator) stats(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-time.Hour)
	st := Stats{At: time.Now().UTC(), Sessions: map[string]int{}, LastHour: map[string]Pct{}, Failures: map[string]int{}, Autoscaler: o.auto != nil}
	views, _ := o.views()
	vms, _ := o.store.ListVMs()
	count := map[string]int{}
	for _, v := range vms {
		switch v.State {
		case StatePaused:
			st.Sessions["paused"]++
		case StateRunning, StateMigrating, StatePausing:
			st.Sessions["running"]++
		}
		if v.State != StateLost && v.State != StatePaused {
			count[v.Node]++
		}
	}
	for _, v := range views {
		st.Nodes = append(st.Nodes, StatsNode{Name: v.Name, Up: v.Up, Cordoned: v.Cordoned, State: o.auto.NodeState(v.Name),
			VMs: count[v.Name], CapMiB: v.CapMemMiB, UsedMiB: v.UsedMemMiB})
	}
	sort.Slice(st.Nodes, func(i, j int) bool { return st.Nodes[i].Name < st.Nodes[j].Name })
	for name, src := range map[string][2]string{
		"create": {"created", "create_ms"}, "migration_pause": {"migrated", "paused_ms"}, "fork_parent_pause": {"forked", "paused_ms"},
		"pause": {"paused", "total_ms"}, "resume": {"resumed", "resume_ms"},
	} {
		st.LastHour[name] = pct(o.store.values(src[0], src[1], since))
	}
	for _, k := range []string{"migration_failed", "pause_failed", "resume_failed", "profile_save_failed"} {
		if n := o.store.countEvents(k, since); n > 0 {
			st.Failures[k] = n
		}
	}
	acts := o.auto.Actions()
	if len(acts) > 12 {
		acts = acts[len(acts)-12:]
	}
	st.AutoActions = acts
	writeJSON(w, http.StatusOK, st)
}
