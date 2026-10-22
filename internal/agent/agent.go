// Package agent is the long-running process on each node. It owns that node's
// VMs on behalf of the orchestrator: it starts and stops them when told, and
// reports what it is really running once a second. The orchestrator decides;
// the agent does and tells the truth.
package agent

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/vedantxn/chromebox/internal/api"
	"github.com/vedantxn/chromebox/internal/base"
	"github.com/vedantxn/chromebox/internal/firecracker"
	"github.com/vedantxn/chromebox/internal/migrate"
	"github.com/vedantxn/chromebox/internal/pageserver"
	"github.com/vedantxn/chromebox/internal/remote"
	"github.com/vedantxn/chromebox/internal/timing"
	"github.com/vedantxn/chromebox/internal/vm"
)

// hostReserveMiB is memory kept back for the node's own use (the OS, page cache,
// the agent). VMs and the bases held in RAM come out of the rest.
const hostReserveMiB = 1536

// Config is what an agent needs to run.
type Config struct {
	Node         string // this node's name, e.g. "node-a"
	Listen       string // e.g. ":7000"
	Token        string // shared secret for the API
	Orchestrator string // where to send heartbeats, e.g. "http://172.31.7.29:8080"
}

// Agent serves the node API and sends heartbeats.
type Agent struct {
	cfg    Config
	loadMu sync.Mutex // one base load at a time

	outMu    sync.Mutex
	outgoing map[string]*outgoing // post-copy migrations still serving memory
}

// outgoing is a VM that runs on another node but whose memory is still here:
// the page server answers the target until it holds every page.
type outgoing struct {
	ps      *pageserver.Server
	mem     []byte // the snapshot's memory file, mapped
	resumed time.Time
}

func New(cfg Config) *Agent { return &Agent{cfg: cfg, outgoing: map[string]*outgoing{}} }

// Run serves until ctx ends, sending a heartbeat every second.
func (a *Agent) Run(ctx context.Context) error {
	srv := &http.Server{Addr: a.cfg.Listen, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	if a.cfg.Orchestrator != "" {
		go a.heartbeats(ctx)
	}
	select {
	case <-ctx.Done():
		return srv.Shutdown(context.Background())
	case err := <-errc:
		return err
	}
}

// Handler is the node API.
func (a *Agent) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.Status())
	})
	mux.HandleFunc("POST /v1/vms", a.createVM)
	mux.HandleFunc("DELETE /v1/vms/{id}", a.deleteVM)
	mux.HandleFunc("POST /v1/vms/{id}/migrate", a.migrateOut)
	mux.HandleFunc("POST /v1/vms/{id}/commit", a.commit)
	mux.HandleFunc("POST /v1/vms/{id}/abort", a.abort)
	mux.HandleFunc("POST /v1/incoming", a.incoming)
	mux.HandleFunc("POST /v1/vms/{id}/accept", a.accept)
	mux.HandleFunc("POST /v1/vms/{id}/state", a.putState)
	mux.HandleFunc("POST /v1/vms/{id}/delta", a.putDelta)
	return a.auth(mux)
}

func (a *Agent) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if a.cfg.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(a.cfg.Token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Agent) createVM(w http.ResponseWriter, r *http.Request) {
	var req api.CreateVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.Base == "" || req.NetIndex < 1 {
		http.Error(w, "need id, base and net_index", http.StatusBadRequest)
		return
	}
	a.loadMu.Lock()
	err := base.Load(req.Base) // puts the base's memory in RAM the first time; a no-op after
	a.loadMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p := vm.PathsFor(req.ID)
	sw := timing.Start()
	if err := base.Clone(r.Context(), p, req.Base, req.NetIndex, sw); err != nil {
		a.destroy(p) // leave nothing half-made behind
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, api.CreateVMResponse{Laps: sw.Record()})
}

func (a *Agent) deleteVM(w http.ResponseWriter, r *http.Request) {
	a.destroy(vm.PathsFor(r.PathValue("id")))
	w.WriteHeader(http.StatusNoContent)
}

// migrateOut is the source half of a migration. It ends with the VM running on
// the target and paused here; the orchestrator then moves the route and says
// commit or abort. No disk pre-copy: the target clones the base disk it already
// holds, and only the blocks this VM wrote since it was cloned travel.
//
// Post-copy (the default since M5): the pause sends only the device state and
// the disk delta, the target resumes at once, and the memory follows from a
// page server here, on demand and in a background stream. Stop-and-copy sends
// all the memory during the pause, as M2–M4 did.
func (a *Agent) migrateOut(w http.ResponseWriter, r *http.Request) {
	var req api.MigrateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetAgent == "" || req.TargetSSH == "" {
		http.Error(w, "need target_agent and target_ssh", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	p := vm.PathsFor(id)
	baseName := vm.BaseOf(p)
	nn, _ := vm.NetOf(p)
	if !alive(p) || baseName == "" || nn == nil {
		http.Error(w, "no running VM made from a base: "+id, http.StatusNotFound)
		return
	}
	postCopy := !req.StopAndCopy
	ctx := r.Context()
	target := NewClient(req.TargetAgent, a.cfg.Token)
	rem := remote.New(req.TargetSSH)
	sw := timing.Start()

	if err := target.Incoming(ctx, api.IncomingRequest{ID: id, Base: baseName, NetIndex: nn.Index}); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if !postCopy { // stop-and-copy sends the memory through ssh
		if _, err := rem.Run(ctx, "true"); err != nil { // open the shared ssh connection before the pause
			_ = target.DeleteVM(context.Background(), id)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	var ps *pageserver.Server
	var mem []byte
	accept := api.AcceptRequest{}
	if postCopy {
		var err error
		if ps, err = a.preparePostCopy(p, req.TargetAgent); err != nil {
			_ = target.DeleteVM(context.Background(), id)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Send the disk's changes so far while the VM still runs; the pause
		// then only sends what changed since this reference.
		if _, err := pushDelta(ctx, target, id, p, base.Disk(baseName), p.DiskRef(), true); err != nil {
			_ = ps.Close()
			_ = target.DeleteVM(context.Background(), id)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		accept = api.AcceptRequest{Source: ps.Addr(), Token: ps.Token(), FaultKiB: req.FaultKiB}
		if accept.FaultKiB == 0 { // the node's default, for experiments
			accept.FaultKiB, _ = strconv.Atoi(os.Getenv("CHROMEBOX_FAULT_KIB"))
		}
	}
	sw.Lap("target prepared")

	c := firecracker.New(p.Socket())
	paused := time.Now()
	fail := func(code int, err error) {
		_ = c.ResumeVM(context.Background())
		_ = target.DeleteVM(context.Background(), id)
		if ps != nil {
			_ = ps.Close()
		}
		if mem != nil {
			_ = unix.Munmap(mem)
		}
		http.Error(w, "migration rolled back, source resumed: "+err.Error(), code)
	}
	if err := vm.Snapshot(ctx, c, p, vm.SnapshotOptions{Sync: false}, sw); err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	if postCopy {
		var err error
		if mem, err = mapFile(p.Mem()); err != nil {
			fail(http.StatusInternalServerError, err)
			return
		}
		ps.Start(mem)
	}
	var delta int64
	var err error
	if postCopy {
		delta, err = pushState(ctx, target, id, p, p.DiskRef())
	} else {
		delta, err = migrate.CopyState(ctx, rem, p, base.Disk(baseName), true)
	}
	if err != nil {
		fail(http.StatusBadGateway, err)
		return
	}
	if postCopy {
		sw.Lap("device state + disk delta sent")
	} else {
		sw.Lap("memory + disk delta sent")
	}
	tr, err := target.Accept(ctx, id, accept)
	if err != nil {
		fail(http.StatusBadGateway, err)
		return
	}
	sw.Lap("running on target")
	var laps bytes.Buffer
	sw.Print(&laps)
	log.Printf("migrate %s (post-copy %v):\n%starget:\n%s", id, postCopy, laps.String(), fmtLaps(tr.Laps))
	if postCopy {
		a.outMu.Lock()
		a.outgoing[id] = &outgoing{ps: ps, mem: mem, resumed: time.Now()}
		a.outMu.Unlock()
		if os.Getenv("CHROMEBOX_FAULT") == "source-dies" { // fault injection for break-it tests:
			go func() { // the source vanishes mid-stream, page server and all
				time.Sleep(200 * time.Millisecond)
				log.Printf("fault injection: source dies during the stream of %s", id)
				os.Exit(3)
			}()
		}
	} else {
		_ = os.RemoveAll(p.MemDir())
	}
	writeJSON(w, http.StatusOK, api.MigrateResponse{Laps: sw.Record(), TargetLaps: tr.Laps,
		PausedMs: float64(time.Since(paused).Microseconds()) / 1000, DeltaBytes: delta, PostCopy: postCopy})
}

// preparePostCopy does, before the pause, what need not happen during it:
//   - reserve the page server's port on the address the target will reach;
//   - allocate the memory file in RAM, so that during the pause Firecracker
//     only copies into it (allocating and zeroing those pages took about half
//     of the 334 ms snapshot write in C1).
func (a *Agent) preparePostCopy(p vm.Paths, targetAgent string) (*pageserver.Server, error) {
	local, targetIP, err := routeTo(targetAgent)
	if err != nil {
		return nil, err
	}
	ps, err := pageserver.Listen(local, randomToken(), targetIP)
	if err != nil {
		return nil, err
	}
	_, img, err := vm.ImageOf(p)
	if err == nil {
		err = os.MkdirAll(p.MemDir(), 0o755)
	}
	if err == nil {
		err = preallocate(p.Mem(), int64(img.MemMiB)<<20)
	}
	if err != nil {
		_ = ps.Close()
		return nil, err
	}
	return ps, nil
}

// commit ends a migration on the source: the VM now lives elsewhere. After
// post-copy the memory here is still the only copy of the pages the target has
// not received, so it waits until the target says it holds them all.
func (a *Agent) commit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var res api.CommitResponse
	a.outMu.Lock()
	out := a.outgoing[id]
	a.outMu.Unlock()
	var err error
	if out != nil {
		err = out.ps.Wait(60 * time.Second)
		res = api.CommitResponse{StreamMs: float64(time.Since(out.resumed).Microseconds()) / 1000,
			StreamMiB: float64(out.ps.Sent()) / (1 << 20)}
		_ = out.ps.Close()
		_ = unix.Munmap(out.mem)
	}
	a.destroy(vm.PathsFor(id))
	// Only now stop hiding it from heartbeats: until destroy, the paused copy
	// still runs here, and reporting it would get it fenced mid-stream.
	a.outMu.Lock()
	delete(a.outgoing, id)
	a.outMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// abort ends a failed migration on the source: resume the paused VM.
func (a *Agent) abort(w http.ResponseWriter, r *http.Request) {
	a.outMu.Lock()
	if out := a.outgoing[r.PathValue("id")]; out != nil {
		_ = out.ps.Close()
		_ = unix.Munmap(out.mem)
		delete(a.outgoing, r.PathValue("id"))
	}
	a.outMu.Unlock()
	if err := firecracker.New(vm.PathsFor(r.PathValue("id")).Socket()).ResumeVM(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// incoming prepares this node to receive a VM: its directories, and its disk as
// a clone of the base this node already holds. The source then sends only the
// blocks that differ from the base.
func (a *Agent) incoming(w http.ResponseWriter, r *http.Request) {
	var req api.IncomingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.Base == "" {
		http.Error(w, "need id and base", http.StatusBadRequest)
		return
	}
	if _, err := base.Read(req.Base); err != nil {
		http.Error(w, "this node does not hold base "+req.Base, http.StatusConflict)
		return
	}
	p := vm.PathsFor(req.ID)
	a.destroy(p)
	for _, d := range []string{p.SnapDir(), p.MemDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := vm.CloneFile(base.Disk(req.Base), p.SnapRootfs()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The TAP and the Firecracker process don't depend on the snapshot: make
	// them now, so the source's pause doesn't wait for them (~190 ms in C2).
	if err := vm.PrepareRestore(r.Context(), p, req.NetIndex); err != nil {
		a.destroy(p)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// accept is the target half: restore the received snapshot and wait until the
// browser answers.
func (a *Agent) accept(w http.ResponseWriter, r *http.Request) {
	p := vm.PathsFor(r.PathValue("id"))
	var req api.AcceptRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // empty: the memory came with the snapshot
	var uffdArgs []string
	if req.Source != "" {
		uffdArgs = []string{"source=" + req.Source, "token=" + req.Token}
		if req.FaultKiB > 0 {
			uffdArgs = append(uffdArgs, "fault_kib="+strconv.Itoa(req.FaultKiB))
		}
	}
	id, began := r.PathValue("id"), time.Now()
	sw := timing.Start()
	if _, err := vm.LoadPrepared(r.Context(), p, sw, uffdArgs...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The VM runs. Answer now, so the route moves while the browser warms up
	// (with post-copy, its first moments are page faults); its readiness is
	// only logged. Waiting here added ~400 ms to the pause in C2.
	go func() {
		if _, err := vm.WaitRestored(context.Background(), p, 0, 15*time.Second); err != nil {
			log.Printf("accept %s: browser did not answer after restore: %v", id, err)
			return
		}
		log.Printf("accept %s: browser answering %.0f ms after the restore began", id, float64(time.Since(began).Microseconds())/1000)
	}()
	for _, f := range [][2]string{{p.SnapBase(), p.BaseFile()}, {p.SnapNet(), p.NetFile()}, {p.SnapImage(), p.ImageFile()}} {
		if b, err := os.ReadFile(f[0]); err == nil {
			_ = os.WriteFile(f[1], b, 0o644)
		}
	}
	_ = os.RemoveAll(p.MemDir()) // the VM has the memory mapped; unlinking frees it when the VM exits
	writeJSON(w, http.StatusOK, api.CreateVMResponse{Laps: sw.Record()})
}

// destroy stops a VM and removes everything it left on this node. It is safe
// to call on a VM that is already gone.
func (a *Agent) destroy(p vm.Paths) {
	_ = vm.Kill(p)
	_ = os.RemoveAll(p.Dir)
	_ = os.RemoveAll(p.MemDir())
}

// Status describes the node: capacity, the VMs actually running, the bases held.
func (a *Agent) Status() api.Status {
	st := api.Status{Node: a.cfg.Node, AgentPort: portOf(a.cfg.Listen), VCPUs: numCPU()}
	st.Bases = listBases()
	// A base this node holds will sit in RAM as soon as a VM is made from it, so
	// reserve its memory whether or not it is loaded yet. Counting only loaded
	// bases would make a node look emptier right until its first create.
	reserve := hostReserveMiB
	for _, b := range st.Bases {
		reserve += b.MemMiB
	}
	st.CapMemMiB = memTotalMiB() - reserve
	a.outMu.Lock()
	moving := make(map[string]bool, len(a.outgoing))
	for id := range a.outgoing {
		moving[id] = true
	}
	a.outMu.Unlock()
	for _, v := range listVMs() {
		if p := vm.PathsFor(v.ID); vm.MemoryOrphaned(p) {
			// Its page-fault handler died before the memory was complete: the
			// guest is frozen on a page nobody will serve. Kill it and stop
			// reporting it; the orchestrator then declares it lost.
			log.Printf("%s: memory handler died before the memory was complete, killing the VM", v.ID)
			_ = vm.Kill(p)
			continue
		}
		// A VM that moved away but whose memory is still being served is not
		// this node's: reporting it would get it fenced mid-stream.
		if !moving[v.ID] {
			st.VMs = append(st.VMs, v)
		}
	}
	return st
}

func listVMs() []api.VMInfo {
	var out []api.VMInfo
	entries, _ := os.ReadDir(filepath.Join(vm.StateDir, "vms"))
	for _, e := range entries {
		p := vm.PathsFor(e.Name())
		if !alive(p) {
			continue
		}
		info := api.VMInfo{ID: e.Name(), MemMiB: 0}
		if name, img, err := vm.ImageOf(p); err == nil {
			info.Image, info.MemMiB = name, img.MemMiB
		}
		if b, err := os.ReadFile(p.BaseFile()); err == nil {
			info.Base = strings.TrimSpace(string(b))
		}
		if nn, _ := vm.NetOf(p); nn != nil {
			info.NetIndex = nn.Index
		}
		info.PssMiB, info.PrivateMiB = memCost(p)
		out = append(out, info)
	}
	return out
}

// alive reports whether the VM's Firecracker process is running.
func alive(p vm.Paths) bool {
	b, err := os.ReadFile(p.PIDFile())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return err == nil && syscall.Kill(pid, 0) == nil
}

func listBases() []api.BaseInfo {
	var out []api.BaseInfo
	entries, _ := os.ReadDir(base.Dir)
	for _, e := range entries {
		if m, err := base.Read(e.Name()); err == nil {
			out = append(out, api.BaseInfo{Name: m.Name, DiskSHA256: m.DiskSHA256, MemMiB: m.MemMiB})
		}
	}
	return out
}

func (a *Agent) heartbeats(ctx context.Context) {
	client := &http.Client{Timeout: 2 * time.Second}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		body, _ := json.Marshal(a.Status())
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.Orchestrator+"/v1/heartbeat", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
		if resp, err := client.Do(req); err != nil {
			log.Printf("heartbeat: %v", err)
		} else {
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func portOf(listen string) int {
	i := strings.LastIndex(listen, ":")
	n, _ := strconv.Atoi(listen[i+1:])
	return n
}

func memTotalMiB() int {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		var kb int
		if _, err := fmt.Sscanf(line, "MemTotal: %d kB", &kb); err == nil {
			return kb >> 10
		}
	}
	return 0
}

func numCPU() int {
	n := 0
	b, _ := os.ReadFile("/proc/cpuinfo")
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "processor") {
			n++
		}
	}
	return n
}

// memCost reads, from the kernel, what the VM's Firecracker process really
// costs: PSS (private pages plus its share of pages it has in common with other
// VMs) and the private part alone (pages only this VM has: whatever it wrote
// since it was cloned).
func memCost(p vm.Paths) (pss, private float64) {
	b, err := os.ReadFile(p.PIDFile())
	if err != nil {
		return 0, 0
	}
	roll, err := os.ReadFile("/proc/" + strings.TrimSpace(string(b)) + "/smaps_rollup")
	if err != nil {
		return 0, 0
	}
	var priv int
	for _, line := range strings.Split(string(roll), "\n") {
		var kb int
		switch {
		case strings.HasPrefix(line, "Pss:"):
			fmt.Sscanf(line, "Pss: %d kB", &kb)
			pss = float64(kb) / 1024
		case strings.HasPrefix(line, "Private_Clean:"):
			fmt.Sscanf(line, "Private_Clean: %d kB", &kb)
			priv += kb
		case strings.HasPrefix(line, "Private_Dirty:"):
			fmt.Sscanf(line, "Private_Dirty: %d kB", &kb)
			priv += kb
		}
	}
	return pss, float64(priv) / 1024
}

func fmtLaps(r timing.Record) string {
	b, _ := json.Marshal(r)
	return string(b)
}
