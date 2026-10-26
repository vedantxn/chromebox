package orchestrator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/vedantxn/chromebox/internal/api"
)

// Profiles: "log in once, every later session starts logged in". A profile is
// a named snapshot of one of your running browsers (its pages, cookies,
// storage, memory), kept in object storage. A session started from a profile
// is a fork-like copy of it: it gets its own address and fresh renderer
// processes, so its random state differs, but starts logged in, on the pages
// the profile was saved on. Saving does not stop the browser (it pauses for
// ~200 ms, like a fork). A profile changes only when you save it again.
const (
	maxProfiles    = 3
	profileImageGB = 650 // MiB of RAM a profile image takes on a node, for placement
)

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

const profileSchema = `CREATE TABLE IF NOT EXISTS profiles (
	id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL, prefix TEXT NOT NULL,
	mem_mib INTEGER NOT NULL, disk_base TEXT NOT NULL, stored_mib REAL NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL, UNIQUE (user_id, name))`

type Profile struct {
	Name      string  `json:"name"`
	CreatedAt string  `json:"created_at"`
	StoredMiB float64 `json:"stored_mib"`
	prefix    string
	memMiB    int
	diskBase  string
}

// profileImage is the name the profile's snapshot has on the nodes that hold
// it: fork-p- plus a hash of its storage prefix, so a new version of a profile
// is a new image and never confused with the old one.
func profileImage(prefix string) string {
	h := sha256.Sum256([]byte(prefix))
	return "fork-p-" + hex.EncodeToString(h[:5])
}

func (s *Store) profile(uid, name string) (Profile, error) {
	var p Profile
	err := s.db.QueryRow(`SELECT name, created_at, stored_mib, prefix, mem_mib, disk_base FROM profiles WHERE user_id = ? AND name = ?`, uid, name).
		Scan(&p.Name, &p.CreatedAt, &p.StoredMiB, &p.prefix, &p.memMiB, &p.diskBase)
	return p, err
}

func (s *Store) profiles(uid string) []Profile {
	rows, err := s.db.Query(`SELECT name, created_at, stored_mib, prefix, mem_mib, disk_base FROM profiles WHERE user_id = ? ORDER BY name`, uid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Profile{}
	for rows.Next() {
		var p Profile
		if rows.Scan(&p.Name, &p.CreatedAt, &p.StoredMiB, &p.prefix, &p.memMiB, &p.diskBase) == nil {
			out = append(out, p)
		}
	}
	return out
}

type SaveProfileResult struct {
	Name      string  `json:"name"`
	PausedMs  float64 `json:"paused_ms"`
	UploadMs  float64 `json:"upload_ms"`
	StoredMiB float64 `json:"stored_mib"`
}

// SaveProfile stores running VM id's state as user uid's profile name,
// replacing any earlier version.
func (o *Orchestrator) SaveProfile(ctx context.Context, uid, id, name string) (SaveProfileResult, error) {
	res := SaveProfileResult{Name: name}
	if !profileName.MatchString(name) {
		return res, errors.New("profile names are 1-31 characters: lowercase letters, digits and hyphens")
	}
	vm, err := o.store.GetVM(id)
	if err != nil || vm.State != StateRunning {
		return res, fmt.Errorf("session %s is not running", id)
	}
	old, oldErr := o.store.profile(uid, name)
	if oldErr != nil && len(o.store.profiles(uid)) >= maxProfiles {
		return res, limitError{fmt.Sprintf("limit: at most %d profiles; delete one first", maxProfiles)}
	}
	ag, err := o.agentFor(vm.Node)
	if err != nil {
		return res, err
	}
	prefix := fmt.Sprintf("profiles/%s/%s/%s/", uid, name, randHex(4))
	resp, err := ag.SaveProfile(ctx, id, api.SaveProfileRequest{Prefix: prefix})
	if err != nil {
		o.metrics.errors.WithLabelValues("save_profile").Inc()
		o.store.addEvent(id, "profile_save_failed", map[string]any{"name": name, "error": err.Error()})
		return res, err
	}
	_, err = o.store.db.Exec(`INSERT INTO profiles (id, user_id, name, prefix, mem_mib, disk_base, stored_mib, created_at) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id, name) DO UPDATE SET prefix = excluded.prefix, mem_mib = excluded.mem_mib, disk_base = excluded.disk_base,
		stored_mib = excluded.stored_mib, created_at = excluded.created_at`,
		"p_"+randHex(6), uid, name, prefix, resp.MemMiB, resp.DiskBase, resp.StoredMiB, now())
	if err != nil {
		go o.deleteSnapshots(prefix)
		return res, err
	}
	if oldErr == nil {
		go o.deleteSnapshots(old.prefix) // the earlier version
	}
	res.PausedMs, res.UploadMs, res.StoredMiB = resp.PausedMs, resp.UploadMs, resp.StoredMiB
	o.store.addEvent(id, "profile_saved", map[string]any{"name": name, "paused_ms": res.PausedMs, "upload_ms": res.UploadMs, "stored_mib": res.StoredMiB})
	return res, nil
}

// CreateFromProfile starts a session from user uid's profile name. The node
// that will run it must hold the profile's snapshot as a local base: one that
// already does is used if it has room; otherwise a node is chosen and fetches
// it first (once; later sessions from the profile on that node share its memory).
func (o *Orchestrator) CreateFromProfile(ctx context.Context, uid, name string, ttl time.Duration) (api.VM, map[string]any, error) {
	prof, err := o.store.profile(uid, name)
	if err != nil {
		return api.VM{}, nil, fmt.Errorf("no profile %q", name)
	}
	image := profileImage(prof.prefix)
	// Refuse an over-limit account before downloading anything for it.
	o.place.Lock()
	err = o.checkLimits(uid)
	o.place.Unlock()
	if err != nil {
		return api.VM{}, nil, err
	}
	detail := map[string]any{"profile": name, "image": image}
	views, err := o.views()
	if err != nil {
		return api.VM{}, nil, err
	}
	if _, err := Place(views, Request{MemMiB: prof.memMiB, Base: image}, o.cfg.Strategy); err != nil {
		node, err := Place(views, Request{MemMiB: prof.memMiB + profileImageGB, Base: prof.diskBase}, o.cfg.Strategy)
		if err != nil {
			return api.VM{}, nil, err
		}
		ag, err := o.agentFor(node)
		if err != nil {
			return api.VM{}, nil, err
		}
		resp, err := ag.EnsureProfileImage(ctx, api.ProfileImageRequest{Name: image, Prefix: prof.prefix})
		if err != nil {
			return api.VM{}, nil, fmt.Errorf("fetch profile %s to %s: %w", name, node, err)
		}
		detail["image_node"], detail["image_download_ms"], detail["fetched"] = node, resp.DownloadMs, resp.Fetched
	}
	vm, err := o.CreateFor(ctx, uid, image, ttl)
	if err != nil {
		return api.VM{}, nil, err
	}
	o.store.addEvent(vm.ID, "started_from_profile", detail)
	return vm, detail, nil
}

func (o *Orchestrator) profileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/users/{uid}/profiles", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, o.store.profiles(r.PathValue("uid")))
	})
	mux.HandleFunc("POST /v1/users/{uid}/profiles", o.saveProfile)
	mux.HandleFunc("DELETE /v1/users/{uid}/profiles/{name}", o.deleteProfile)
}

func (o *Orchestrator) saveProfile(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	var in struct{ Session, Name string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Session == "" || in.Name == "" {
		http.Error(w, "need session and name", http.StatusBadRequest)
		return
	}
	if _, err := o.owned(uid, in.Session); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	res, err := o.SaveProfile(r.Context(), uid, in.Session, in.Name)
	if err != nil {
		code := http.StatusConflict
		var le limitError
		if errors.As(err, &le) {
			code = http.StatusTooManyRequests
		} else if strings.Contains(err.Error(), "profile names are") {
			code = http.StatusBadRequest
		}
		http.Error(w, err.Error(), code)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (o *Orchestrator) deleteProfile(w http.ResponseWriter, r *http.Request) {
	uid, name := r.PathValue("uid"), r.PathValue("name")
	p, err := o.store.profile(uid, name)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		http.Error(w, "no such profile", http.StatusNotFound)
		return
	}
	_, _ = o.store.db.Exec(`DELETE FROM profiles WHERE user_id = ? AND name = ?`, uid, name)
	go o.deleteSnapshots(p.prefix)
	w.WriteHeader(http.StatusNoContent)
}
