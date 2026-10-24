package agent

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/vedantxn/chromebox/internal/api"
	"github.com/vedantxn/chromebox/internal/base"
	"github.com/vedantxn/chromebox/internal/blockdiff"
	"github.com/vedantxn/chromebox/internal/firecracker"
	"github.com/vedantxn/chromebox/internal/guestctl"
	"github.com/vedantxn/chromebox/internal/netsetup"
	"github.com/vedantxn/chromebox/internal/snapstore"
	"github.com/vedantxn/chromebox/internal/timing"
	"github.com/vedantxn/chromebox/internal/vm"
)

// Pausing a session to object storage, and resuming it, on any node.
//
// Pause: the VM is snapshotted (a pause of ~200 ms, as in a migration), the
// snapshot is uploaded (device state, the disk as a delta from the root base,
// the non-zero memory in parallel 32 MiB objects), and only then is the VM
// stopped. If the upload fails the VM is resumed and the session carries on.
//
// Resume: the node clones its own copy of the root base's disk, applies the
// stored delta, and writes the stored memory runs into a sparse file in RAM,
// downloading the segments in parallel while the TAP and a jailed Firecracker
// are made; then it loads the snapshot, tells the guest the time (it may have
// been asleep for days) and waits for Chrome to answer.

func (a *Agent) snapRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/vms/{id}/pause", a.pauseVM)
	mux.HandleFunc("POST /v1/vms/{id}/resume", a.resumeVM)
	mux.HandleFunc("DELETE /v1/snapshots", a.deleteSnapshots)
	mux.HandleFunc("POST /v1/vms/{id}/save-profile", a.saveProfile)
	mux.HandleFunc("POST /v1/profile-images", a.ensureProfileImage)
}

// openSnaps connects to the snapshot bucket named by CHROMEBOX_SNAP_BUCKET.
func openSnaps() *snapstore.Store {
	bucket := os.Getenv("CHROMEBOX_SNAP_BUCKET")
	if bucket == "" {
		return nil
	}
	b, err := snapstore.NewS3(context.Background(), bucket, "us-east-1")
	if err != nil {
		return nil
	}
	st := snapstore.New(b)
	if n, err := strconv.Atoi(os.Getenv("CHROMEBOX_SNAP_PARALLEL")); err == nil && n > 0 {
		st.Parallel = n
	}
	return st
}

func (a *Agent) noSnaps(w http.ResponseWriter) bool {
	if a.snaps == nil {
		http.Error(w, "this node has no snapshot storage (CHROMEBOX_SNAP_BUCKET)", http.StatusNotImplemented)
		return true
	}
	return false
}

func (a *Agent) pauseVM(w http.ResponseWriter, r *http.Request) {
	if a.noSnaps(w) {
		return
	}
	var req api.PauseRequest
	if err := decodeJSON(r, &req); err != nil || req.Prefix == "" {
		http.Error(w, "need prefix", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	p := vm.PathsFor(id)
	baseName := vm.BaseOf(p)
	imgName, img, ierr := vm.ImageOf(p)
	if !alive(p) || baseName == "" || ierr != nil {
		http.Error(w, "no running VM made from a base: "+id, http.StatusNotFound)
		return
	}
	ctx := r.Context()
	c := firecracker.New(p.Socket())
	sw := timing.Start()
	began := time.Now()
	// Anything that goes wrong leaves the VM running as it was.
	undo := func(code int, err error) {
		_ = c.ResumeVM(context.Background())
		vm.ClearMemDir(p)
		_ = os.RemoveAll(p.SnapDir())
		a.metrics.vmFailures.WithLabelValues("pause").Inc()
		http.Error(w, "pause failed, the session keeps running: "+err.Error(), code)
	}

	if err := vm.Snapshot(ctx, c, p, vm.SnapshotOptions{}, sw); err != nil {
		undo(http.StatusInternalServerError, err)
		return
	}
	paused := time.Since(began)

	root := base.RootBase(baseName)
	up := time.Now()
	st, err := a.upload(ctx, req.Prefix, snapstore.Manifest{Kind: "session", Image: imgName, DiskBase: root, MemMiB: img.MemMiB},
		p.Vmstate(), p.SnapRootfs(), p.Mem())
	if err != nil {
		undo(http.StatusBadGateway, err)
		return
	}
	sw.Lap("uploaded")
	a.destroy(p)
	mib := func(n int64) float64 { return float64(n) / (1 << 20) }
	writeJSON(w, http.StatusOK, api.PauseResponse{Laps: sw.Record(), PausedMs: ms(paused), UploadMs: ms(time.Since(up)),
		TotalMs: ms(time.Since(began)), StoredMiB: mib(st.Bytes), MemoryMiB: mib(st.MemBytes), DiskMiB: mib(st.DiskBytes), DiskBase: root})
}

// upload stores a snapshot made of three local files: the device state, the
// disk (as a delta from the manifest's DiskBase, which this node holds), and
// the guest memory.
func (a *Agent) upload(ctx context.Context, prefix string, m snapstore.Manifest, vmstatePath, diskPath, memPath string) (snapstore.Stats, error) {
	ranges, err := diskDelta(diskPath, base.Disk(m.DiskBase))
	if err != nil {
		return snapstore.Stats{}, err
	}
	var disk bytes.Buffer
	df, err := os.Open(diskPath)
	if err != nil {
		return snapstore.Stats{}, err
	}
	err = blockdiff.Send(&disk, df, ranges)
	df.Close()
	if err != nil {
		return snapstore.Stats{}, err
	}
	vmstate, err := os.ReadFile(vmstatePath)
	if err != nil {
		return snapstore.Stats{}, err
	}
	mem, err := os.Open(memPath)
	if err != nil {
		return snapstore.Stats{}, err
	}
	defer mem.Close()
	return a.snaps.Save(ctx, prefix, snapstore.Snapshot{Manifest: m, VMState: vmstate, Disk: disk.Bytes(),
		Memory: mem, MemorySize: int64(m.MemMiB) << 20})
}

func (a *Agent) resumeVM(w http.ResponseWriter, r *http.Request) {
	if a.noSnaps(w) {
		return
	}
	var req api.ResumeRequest
	if err := decodeJSON(r, &req); err != nil || req.Prefix == "" || req.NetIndex < 1 {
		http.Error(w, "need prefix and net_index", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	p := vm.PathsFor(id)
	ctx := r.Context()
	sw := timing.Start()
	began := time.Now()
	fail := func(code int, err error) {
		a.destroy(p)
		a.metrics.vmFailures.WithLabelValues("resume").Inc()
		http.Error(w, "resume failed: "+err.Error(), code)
	}
	a.destroy(p) // nothing of an earlier attempt may remain

	m, err := a.snaps.Manifest(ctx, req.Prefix)
	if err != nil {
		fail(http.StatusBadGateway, err)
		return
	}
	if _, err := base.Read(m.DiskBase); err != nil {
		fail(http.StatusConflict, fmt.Errorf("this node does not hold base %s", m.DiskBase))
		return
	}
	for _, d := range []string{p.Dir, p.SnapDir(), p.MemDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			fail(http.StatusInternalServerError, err)
			return
		}
	}
	// The image sets the jail's memory limit; the disk starts as this node's
	// own copy of the root base.
	if err := os.WriteFile(p.ImageFile(), []byte(m.Image+"\n"), 0o644); err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	if err := vm.CloneFile(base.Disk(m.DiskBase), p.SnapRootfs()); err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	// Memory goes into a sparse file: zero pages are never stored, so they stay holes.
	mf, err := os.OpenFile(p.Mem(), os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	defer mf.Close()
	if err := mf.Truncate(int64(m.MemMiB) << 20); err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	sw.Lap("prepared")

	// The TAP and the jailed Firecracker don't need the snapshot: make them
	// while it downloads.
	var wg sync.WaitGroup
	var prepErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		prepErr = vm.PrepareRestore(ctx, p, req.NetIndex)
	}()
	l, err := a.snaps.Load(ctx, req.Prefix, func(off uint64, data []byte) error {
		_, err := mf.WriteAt(data, int64(off))
		return err
	})
	wg.Wait()
	if err != nil {
		fail(http.StatusBadGateway, err)
		return
	}
	if prepErr != nil {
		fail(http.StatusInternalServerError, prepErr)
		return
	}
	sw.Lap("downloaded")

	if _, err := blockdiff.Apply(bytes.NewReader(l.Disk), p.SnapRootfs()); err != nil {
		fail(http.StatusInternalServerError, fmt.Errorf("apply disk delta: %w", err))
		return
	}
	// A resumed VM's disk descends from the root base, whatever the VM was
	// made from, so that is its base now.
	for path, v := range map[string]string{p.Vmstate(): string(l.VMState), p.SnapNet(): strconv.Itoa(req.NetIndex) + "\n",
		p.SnapImage(): m.Image + "\n", p.SnapBase(): m.DiskBase + "\n"} {
		if err := os.WriteFile(path, []byte(v), 0o644); err != nil {
			fail(http.StatusInternalServerError, err)
			return
		}
	}
	sw.Lap("disk rebuilt")

	if _, err := vm.LoadPrepared(ctx, p, sw); err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	// The guest may have been asleep for days: tell it the time.
	nn, err := netsetup.ForIndex(req.NetIndex)
	if err == nil {
		err = guestctl.SetIdentity(ctx, p.VsockSock(), nn.GuestIP.String()+"/30", nn.TapIP.String(), time.Now())
	}
	if err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	sw.Lap("clock set")
	if _, err := vm.WaitRestored(ctx, p, 0, 15*time.Second); err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	for _, f := range [][2]string{{p.SnapBase(), p.BaseFile()}, {p.SnapNet(), p.NetFile()}, {p.SnapImage(), p.ImageFile()}} {
		if b, err := os.ReadFile(f[0]); err == nil {
			_ = os.WriteFile(f[1], b, 0o644)
		}
	}
	_ = os.RemoveAll(p.SnapDir())
	vm.ClearMemDir(p) // the VM has the memory mapped
	capLater(p)
	sw.Lap("browser answering")
	a.metrics.create.Observe(since(began))
	mib := func(n int64) float64 { return float64(n) / (1 << 20) }
	writeJSON(w, http.StatusOK, api.ResumeResponse{Laps: sw.Record(), DownloadMs: l.Stats.Ms,
		TotalMs: ms(time.Since(began)), StoredMiB: mib(l.Stats.Bytes)})
}

// deleteSnapshots removes the objects under ?prefix=. Only the two prefixes
// the orchestrator uses are accepted.
func (a *Agent) deleteSnapshots(w http.ResponseWriter, r *http.Request) {
	if a.noSnaps(w) {
		return
	}
	prefix := r.URL.Query().Get("prefix")
	if len(prefix) < 12 || !(hasPrefix(prefix, "sessions/") || hasPrefix(prefix, "profiles/")) {
		http.Error(w, "bad prefix", http.StatusBadRequest)
		return
	}
	if _, err := a.snaps.Delete(r.Context(), prefix); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

// saveProfile stores a running VM's state as a profile. The VM is paused only
// for as long as taking a fork image takes (~200 ms: the same snapshot a fork
// makes, written into the VM's own memory directory and renamed); the upload
// then runs with the VM running again, and the local image is removed.
func (a *Agent) saveProfile(w http.ResponseWriter, r *http.Request) {
	if a.noSnaps(w) {
		return
	}
	var req api.SaveProfileRequest
	if err := decodeJSON(r, &req); err != nil || req.Prefix == "" {
		http.Error(w, "need prefix", http.StatusBadRequest)
		return
	}
	p := vm.PathsFor(r.PathValue("id"))
	baseName := vm.BaseOf(p)
	imgName, img, ierr := vm.ImageOf(p)
	if !alive(p) || baseName == "" || ierr != nil {
		http.Error(w, "no running VM made from a base: "+r.PathValue("id"), http.StatusNotFound)
		return
	}
	ctx := r.Context()
	sw := timing.Start()
	local := "fork-save-" + randomToken()[:8]
	if _, err := base.Fork(ctx, firecracker.New(p.Socket()), p, local, sw); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = base.Delete(local) }()
	steps, _ := sw.Steps()
	var paused time.Duration
	for _, k := range []string{"paused", "snapshot written", "disk cloned", "resumed"} {
		paused += steps[k]
	}
	root := base.RootBase(baseName)
	vmstate, mem, disk := base.ImageFiles(local)
	up := time.Now()
	st, err := a.upload(ctx, req.Prefix, snapstore.Manifest{Kind: "profile", Image: imgName, DiskBase: root, MemMiB: img.MemMiB}, vmstate, disk, mem)
	if err != nil {
		a.metrics.vmFailures.WithLabelValues("save-profile").Inc()
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, api.SaveProfileResponse{PausedMs: ms(paused), UploadMs: ms(time.Since(up)),
		StoredMiB: float64(st.Bytes) / (1 << 20), MemMiB: img.MemMiB, DiskBase: root})
}

var imageMu sync.Mutex // one profile image is fetched at a time (they are ~600 MiB of RAM each)

// ensureProfileImage makes the profile snapshot at req.Prefix available here as
// a local base named req.Name, shaped like a fork image: new sessions then
// start from it with Clone, mapping its memory copy-on-write, so the second
// session from a profile on a node costs no download.
func (a *Agent) ensureProfileImage(w http.ResponseWriter, r *http.Request) {
	if a.noSnaps(w) {
		return
	}
	var req api.ProfileImageRequest
	if err := decodeJSON(r, &req); err != nil || req.Prefix == "" || !base.IsFork(req.Name) {
		http.Error(w, "need prefix and a fork- name", http.StatusBadRequest)
		return
	}
	imageMu.Lock()
	defer imageMu.Unlock()
	if m, err := base.Read(req.Name); err == nil {
		writeJSON(w, http.StatusOK, api.ProfileImageResponse{MemMiB: m.MemMiB, DiskBase: m.DiskBase})
		return
	}
	ctx := r.Context()
	m, err := a.snaps.Manifest(ctx, req.Prefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Memory goes into a sparse temporary file in the same tmpfs, which Install renames into place.
	tmp := filepath.Join(vm.ShmDir, "tmp-"+req.Name)
	_ = os.MkdirAll(vm.ShmDir, 0o755)
	mf, err := os.OpenFile(tmp, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp)
	defer mf.Close()
	if err := mf.Truncate(int64(m.MemMiB) << 20); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	l, err := a.snaps.Load(ctx, req.Prefix, func(off uint64, data []byte) error {
		_, err := mf.WriteAt(data, int64(off))
		return err
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	bm := base.Manifest{Name: req.Name, Image: m.Image, MemMiB: m.MemMiB, DiskBase: m.DiskBase, ForkOf: "profile"}
	if img, err := vm.LookupImage(m.Image); err == nil {
		bm.VCPUs = img.VCPUs
	}
	bm.BuiltAt = time.Now().UTC()
	err = base.Install(bm, l.VMState,
		func(path string) error { _, err := blockdiff.Apply(bytes.NewReader(l.Disk), path); return err },
		func(path string) error { return os.Rename(tmp, path) })
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.heartbeat(ctx); err != nil {
		log.Printf("profile image %s: heartbeat: %v", req.Name, err)
	}
	writeJSON(w, http.StatusOK, api.ProfileImageResponse{Fetched: true, DownloadMs: l.Stats.Ms, StoredMiB: float64(l.Stats.Bytes) / (1 << 20),
		MemMiB: m.MemMiB, DiskBase: m.DiskBase})
}
