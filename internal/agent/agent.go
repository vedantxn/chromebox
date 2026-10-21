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

	"github.com/vedantxn/chromebox/internal/api"
	"github.com/vedantxn/chromebox/internal/base"
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
}

func New(cfg Config) *Agent { return &Agent{cfg: cfg} }

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
	st.VMs = listVMs()
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
