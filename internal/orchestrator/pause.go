package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/vedantxn/chromebox/internal/api"
)

// Pausing a session stores it in object storage and frees its node; resuming
// brings it back on whichever node has room. While paused a session costs
// nothing: it holds no node memory, does not count as running for its owner's
// limits, and its lifetime clock is stopped. It keeps its network address
// (the VM row stays, with its index), so its identity is the same on resume.
const (
	maxPaused      = 5                  // paused sessions per account
	pauseRetention = 7 * 24 * time.Hour // then it is deleted (the bucket's lifecycle rule is the backstop)
	resumeMinLife  = 5 * time.Minute    // a resumed session gets at least this long, whatever was left
)

type PauseResult struct {
	VM        string  `json:"vm"`
	PausedMs  float64 `json:"paused_ms"` // the browser, while its snapshot was taken
	UploadMs  float64 `json:"upload_ms"`
	TotalMs   float64 `json:"total_ms"`
	StoredMiB float64 `json:"stored_mib"`
	MemoryMiB float64 `json:"memory_mib"`
	DiskMiB   float64 `json:"disk_mib"`
}

type ResumeResult struct {
	VM         string  `json:"vm"`
	Node       string  `json:"node"`
	TotalMs    float64 `json:"total_ms"`
	DownloadMs float64 `json:"download_ms"`
	StoredMiB  float64 `json:"stored_mib"`
}

// Pause snapshots running VM id into storage and stops it. If anything fails
// the VM keeps running as before.
func (o *Orchestrator) Pause(ctx context.Context, id string) (PauseResult, error) {
	res := PauseResult{VM: id}
	vm, err := o.store.GetVM(id)
	if err != nil {
		return res, fmt.Errorf("unknown vm %q", id)
	}
	if vm.State != StateRunning {
		return res, fmt.Errorf("vm %s is %s, not running", id, vm.State)
	}
	ag, err := o.agentFor(vm.Node)
	if err != nil {
		return res, err
	}
	if err := o.store.SetState(id, StatePausing); err != nil {
		return res, err
	}
	start := time.Now()
	prefix := fmt.Sprintf("sessions/%s/%s/", id, randHex(4))
	resp, err := ag.Pause(ctx, id, api.PauseRequest{Prefix: prefix})
	if err != nil {
		_ = o.store.SetState(id, StateRunning)
		o.metrics.pauses.WithLabelValues("failed").Inc()
		o.metrics.errors.WithLabelValues("pause").Inc()
		o.store.addEvent(id, "pause_failed", map[string]any{"error": err.Error()})
		return res, err
	}
	// The browser is gone from its node. Remove its route and tell its clients.
	o.delRoute(ctx, vm.NetIndex)
	o.closeConns(vm.NetIndex)
	remaining := max(time.Until(vm.ExpiresAt()), 0)
	if err := o.store.SetPaused(id, prefix, resp.DiskBase, remaining); err != nil {
		log.Printf("pause %s: stored, but recording it failed: %v", id, err)
		return res, err
	}
	_, _ = o.store.db.Exec(`UPDATE sessions SET paused_at = ? WHERE id = ?`, now(), id)
	res = PauseResult{VM: id, PausedMs: resp.PausedMs, UploadMs: resp.UploadMs, StoredMiB: resp.StoredMiB,
		MemoryMiB: resp.MemoryMiB, DiskMiB: resp.DiskMiB, TotalMs: ms(time.Since(start))}
	o.metrics.pauses.WithLabelValues("ok").Inc()
	o.metrics.pause.Observe(res.TotalMs / 1000)
	o.store.addEvent(id, "paused", map[string]any{"node": vm.Node, "paused_ms": res.PausedMs, "upload_ms": res.UploadMs,
		"total_ms": res.TotalMs, "stored_mib": res.StoredMiB, "memory_mib": res.MemoryMiB, "disk_mib": res.DiskMiB})
	return res, nil
}

// markResumed takes session id out of the paused state and counts the time it
// was paused (which is not browser time). The returned func puts it back, if
// the resume fails.
func (s *Store) markResumed(id string) (undo func()) {
	var pausedAt string
	var total float64
	if s.db.QueryRow(`SELECT paused_at, paused_s FROM sessions WHERE id = ?`, id).Scan(&pausedAt, &total) != nil || pausedAt == "" {
		return func() {}
	}
	if t, err := time.Parse(time.RFC3339Nano, pausedAt); err == nil {
		_, _ = s.db.Exec(`UPDATE sessions SET paused_at = '', paused_s = ? WHERE id = ?`, total+time.Since(t).Seconds(), id)
	}
	return func() {
		_, _ = s.db.Exec(`UPDATE sessions SET paused_at = ?, paused_s = ? WHERE id = ?`, pausedAt, total, id)
	}
}

// Resume brings paused VM id back on a node chosen by placement. check, if
// not nil, runs under the placement lock before anything is changed (the
// account limits).
func (o *Orchestrator) Resume(ctx context.Context, id string, check func() error) (ResumeResult, error) {
	res := ResumeResult{VM: id}
	vm, err := o.store.GetVM(id)
	if err != nil {
		return res, fmt.Errorf("unknown vm %q", id)
	}
	if vm.State != StatePaused {
		return res, fmt.Errorf("vm %s is %s, not paused", id, vm.State)
	}
	prefix, remaining, err := o.store.SnapInfo(id)
	if err != nil || prefix == "" {
		return res, fmt.Errorf("vm %s has no stored snapshot", id)
	}

	o.place.Lock()
	views, err := o.views()
	if err != nil {
		o.place.Unlock()
		return res, err
	}
	node, err := Place(views, Request{MemMiB: vm.MemMiB, Base: vm.Base}, o.cfg.Strategy)
	if err != nil {
		o.place.Unlock()
		return res, err
	}
	if check != nil {
		if err := check(); err != nil {
			o.place.Unlock()
			return res, err
		}
	}
	undo := o.store.markResumed(id)
	err = o.store.SetResuming(id, node)
	o.place.Unlock()
	if err != nil {
		undo()
		return res, err
	}
	res.Node = node

	fail := func(err error) (ResumeResult, error) {
		undo()
		_ = o.store.SetState(id, StatePaused)
		_, _ = o.store.db.Exec(`UPDATE vms SET node = '' WHERE id = ?`, id)
		o.delRoute(context.Background(), vm.NetIndex)
		o.metrics.resumes.WithLabelValues("failed").Inc()
		o.metrics.errors.WithLabelValues("resume").Inc()
		o.store.addEvent(id, "resume_failed", map[string]any{"node": node, "error": err.Error()})
		return res, err
	}
	ag, err := o.agentFor(node)
	if err != nil {
		return fail(err)
	}
	start := time.Now()
	routeErr := make(chan error, 1)
	go func() { routeErr <- o.setRoute(ctx, vm.NetIndex, node) }()
	resp, err := ag.Resume(ctx, id, api.ResumeRequest{Prefix: prefix, NetIndex: vm.NetIndex})
	if rerr := <-routeErr; err == nil {
		err = rerr
	}
	if err != nil {
		return fail(err)
	}
	if err := o.store.SetResumed(id, time.Now().Add(max(remaining, resumeMinLife))); err != nil {
		return fail(err)
	}
	res.TotalMs, res.DownloadMs, res.StoredMiB = ms(time.Since(start)), resp.DownloadMs, resp.StoredMiB
	o.metrics.resumes.WithLabelValues("ok").Inc()
	o.metrics.resume.Observe(res.TotalMs / 1000)
	o.store.addEvent(id, "resumed", map[string]any{"node": node, "resume_ms": res.TotalMs, "download_ms": res.DownloadMs, "stored_mib": res.StoredMiB})
	go o.deleteSnapshots(prefix) // the stored copy is stale the moment the browser runs again
	return res, nil
}

// deleteSnapshots removes the stored objects under prefix, through any node
// that is up (the nodes hold the storage role; this host does not).
func (o *Orchestrator) deleteSnapshots(prefix string) {
	if prefix == "" {
		return
	}
	views, err := o.views()
	if err != nil {
		return
	}
	for _, v := range views {
		if !v.Up {
			continue
		}
		ag, err := o.agentFor(v.Name)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = ag.DeleteSnapshots(ctx, prefix)
		cancel()
		if err == nil {
			return
		}
	}
	log.Printf("could not delete stored snapshot %s (the bucket's lifecycle rule will)", prefix)
}

// expirePaused deletes sessions that have been paused too long.
func (o *Orchestrator) expirePaused(ctx context.Context) {
	rows, err := o.store.db.Query(`SELECT id, paused_at FROM sessions WHERE paused_at != '' AND ended_at = ''`)
	if err != nil {
		return
	}
	var stale []string
	for rows.Next() {
		var id, at string
		if rows.Scan(&id, &at) == nil {
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) > pauseRetention {
				stale = append(stale, id)
			}
		}
	}
	rows.Close()
	for _, id := range stale {
		log.Printf("%s was paused for over %s, deleting", id, pauseRetention)
		o.store.endSessionRecord(id, "paused for too long")
		if err := o.Delete(ctx, id); err != nil {
			log.Printf("expire paused %s: %v", id, err)
		}
	}
}

func (o *Orchestrator) pauseSession(w http.ResponseWriter, r *http.Request) {
	uid, sid := r.PathValue("uid"), r.PathValue("sid")
	if _, err := o.owned(uid, sid); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var paused int
	_ = o.store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND ended_at = '' AND paused_at != ''`, uid).Scan(&paused)
	if paused >= maxPaused {
		http.Error(w, fmt.Sprintf("limit: at most %d paused sessions; end one first", maxPaused), http.StatusTooManyRequests)
		return
	}
	res, err := o.Pause(r.Context(), sid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": o.sessionView(sid, uid), "pause": res})
}

func (o *Orchestrator) resumeSession(w http.ResponseWriter, r *http.Request) {
	uid, sid := r.PathValue("uid"), r.PathValue("sid")
	if _, err := o.owned(uid, sid); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	res, err := o.Resume(r.Context(), sid, func() error { return o.checkRunning(uid) })
	if err != nil {
		code := http.StatusConflict
		var le limitError
		switch {
		case errors.As(err, &le):
			code = http.StatusTooManyRequests
		case len(err.Error()) > 7 && err.Error()[:7] == "no node":
			code = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), code)
		return
	}
	vm, _ := o.store.GetVM(sid)
	s := o.sessionView(sid, uid)
	s.ConnectURL = o.connectURL(vm, "cdp", time.Until(vm.ExpiresAt()))
	writeJSON(w, http.StatusOK, map[string]any{"session": s, "resume": res})
}
