// Package orchestrator is the control plane: it knows the nodes (from their
// heartbeats), decides where each VM goes, and keeps the gateway's routes and
// the database in step with what the agents report.
package orchestrator

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vedantxn/chromebox/internal/agent"
	"github.com/vedantxn/chromebox/internal/api"
	"github.com/vedantxn/chromebox/internal/gateway"
	"github.com/vedantxn/chromebox/internal/netsetup"
	"github.com/vedantxn/chromebox/internal/timing"
)

// Config is what the orchestrator needs to run.
type Config struct {
	Token            string
	DBPath           string
	APIListen        string        // e.g. ":8080"
	ProxyListen      string        // the CDP proxy agents connect to, e.g. ":9222"
	Strategy         Strategy      // placement
	HeartbeatTimeout time.Duration // a node silent for longer is down
}

type nodeState struct {
	Status   api.Status
	Seen     time.Time
	AgentURL string
}

// Orchestrator is the control plane.
type Orchestrator struct {
	cfg   Config
	store *Store

	place sync.Mutex // placement decisions happen one at a time

	mu   sync.RWMutex
	seen map[string]nodeState

	pending map[string]int // memory held on a node for migrations in flight (guarded by place)
}

func New(cfg Config) (*Orchestrator, error) {
	if cfg.HeartbeatTimeout == 0 {
		cfg.HeartbeatTimeout = 4 * time.Second
	}
	if cfg.Strategy == "" {
		cfg.Strategy = Spread
	}
	st, err := Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	return &Orchestrator{cfg: cfg, store: st, seen: map[string]nodeState{}, pending: map[string]int{}}, nil
}

// Run serves the API and the CDP proxy until ctx ends.
func (o *Orchestrator) Run(ctx context.Context) error {
	api := &http.Server{Addr: o.cfg.APIListen, Handler: o.Handler(), ReadHeaderTimeout: 10 * time.Second}
	proxy := &http.Server{Addr: o.cfg.ProxyListen, Handler: gateway.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 2)
	go func() { errc <- api.ListenAndServe() }()
	go func() { errc <- proxy.ListenAndServe() }()
	log.Printf("orchestrator: API on %s, CDP proxy on %s, strategy %s", o.cfg.APIListen, o.cfg.ProxyListen, o.cfg.Strategy)
	select {
	case <-ctx.Done():
		return nil
	case err := <-errc:
		return err
	}
}

// Handler is the REST API.
func (o *Orchestrator) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("POST /v1/heartbeat", o.heartbeat)
	mux.HandleFunc("POST /v1/vms", o.createVM)
	mux.HandleFunc("GET /v1/vms", o.listVMs)
	mux.HandleFunc("DELETE /v1/vms/{id}", o.deleteVM)
	mux.HandleFunc("GET /v1/nodes", o.listNodes)
	mux.HandleFunc("POST /v1/vms/{id}/migrate", o.migrateVM)
	mux.HandleFunc("POST /v1/nodes/{name}/drain", o.drainNode)
	mux.HandleFunc("POST /v1/nodes/{name}/cordon", func(w http.ResponseWriter, r *http.Request) { o.cordon(w, r, true) })
	mux.HandleFunc("POST /v1/nodes/{name}/uncordon", func(w http.ResponseWriter, r *http.Request) { o.cordon(w, r, false) })
	return o.auth(mux)
}

func (o *Orchestrator) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if o.cfg.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(o.cfg.Token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (o *Orchestrator) heartbeat(w http.ResponseWriter, r *http.Request) {
	var st api.Status
	if err := json.NewDecoder(r.Body).Decode(&st); err != nil || st.Node == "" {
		http.Error(w, "bad status", http.StatusBadRequest)
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	url := fmt.Sprintf("http://%s:%d", host, st.AgentPort)
	o.mu.Lock()
	o.seen[st.Node] = nodeState{Status: st, Seen: time.Now(), AgentURL: url}
	o.mu.Unlock()
	if err := o.store.UpsertNode(st.Node, url); err != nil {
		log.Printf("upsert node %s: %v", st.Node, err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// up reports whether a node has sent a heartbeat recently.
func (o *Orchestrator) up(name string) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	s, ok := o.seen[name]
	return ok && time.Since(s.Seen) < o.cfg.HeartbeatTimeout
}

func (o *Orchestrator) agentFor(node string) (*agent.Client, error) {
	o.mu.RLock()
	s, ok := o.seen[node]
	o.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("node %q has never reported", node)
	}
	return agent.NewClient(s.AgentURL, o.cfg.Token), nil
}

// views builds the scheduler's picture of the nodes.
func (o *Orchestrator) views() ([]NodeView, error) {
	rows, err := o.store.ListNodes()
	if err != nil {
		return nil, err
	}
	used, err := o.store.UsedMem()
	if err != nil {
		return nil, err
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	var out []NodeView
	for _, n := range rows {
		s := o.seen[n.Name]
		v := NodeView{Name: n.Name, Cordoned: n.Cordoned, CapMemMiB: s.Status.CapMemMiB,
			UsedMemMiB: used[n.Name] + o.pending[n.Name], Bases: map[string]string{}}
		v.Up = !s.Seen.IsZero() && time.Since(s.Seen) < o.cfg.HeartbeatTimeout
		for _, b := range s.Status.Bases {
			v.Bases[b.Name] = b.DiskSHA256
		}
		out = append(out, v)
	}
	return out, nil
}

// baseMem is how much memory a VM of the base needs, as the nodes report it.
func (o *Orchestrator) baseMem(name string) (int, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	for _, s := range o.seen {
		for _, b := range s.Status.Bases {
			if b.Name == name {
				return b.MemMiB, true
			}
		}
	}
	return 0, false
}

// Create places a new VM, starts it from the base snapshot, and routes the
// gateway to it. The VM takes its place (a database row with its memory) before
// the slow part begins, so concurrent creates see each other.
func (o *Orchestrator) Create(ctx context.Context, baseName string) (api.VM, api.CreateVMResponse, error) {
	sw := timing.Start()
	mem, ok := o.baseMem(baseName)
	if !ok {
		return api.VM{}, api.CreateVMResponse{}, fmt.Errorf("no node holds base %q", baseName)
	}
	o.place.Lock()
	views, err := o.views()
	if err != nil {
		o.place.Unlock()
		return api.VM{}, api.CreateVMResponse{}, err
	}
	node, err := Place(views, Request{MemMiB: mem, Base: baseName}, o.cfg.Strategy)
	if err != nil {
		o.place.Unlock()
		return api.VM{}, api.CreateVMResponse{}, err
	}
	vm, err := o.store.InsertVM(newID(), node, baseName, mem)
	o.place.Unlock()
	if err != nil {
		return api.VM{}, api.CreateVMResponse{}, err
	}
	sw.Lap("placed")

	fail := func(err error) (api.VM, api.CreateVMResponse, error) {
		if ag, aerr := o.agentFor(node); aerr == nil {
			_ = ag.DeleteVM(context.Background(), vm.ID)
		}
		_ = o.store.DeleteVM(vm.ID)
		return api.VM{}, api.CreateVMResponse{}, err
	}
	ag, err := o.agentFor(node)
	if err != nil {
		return fail(err)
	}
	// The route does not depend on the VM being up, so set it while the node
	// starts the VM; its cost (a sudo and an ip process) then hides inside the
	// node's.
	routeErr := make(chan error, 1)
	go func() { routeErr <- o.setRoute(ctx, vm.NetIndex, node) }()
	resp, err := ag.CreateVM(ctx, api.CreateVMRequest{ID: vm.ID, Base: baseName, NetIndex: vm.NetIndex})
	rerr := <-routeErr
	if err != nil {
		return fail(err)
	}
	if rerr != nil {
		return fail(rerr)
	}
	sw.Lap("node started the VM, route set")
	resp.OrchLaps = sw.Record()
	if err := o.store.SetState(vm.ID, StateRunning); err != nil {
		return fail(err)
	}
	vm.State, vm.CDPPath = StateRunning, fmt.Sprintf("/v/%d", vm.NetIndex)
	return vm, resp, nil
}

// setRoute points the gateway at the node that holds VM index idx.
func (o *Orchestrator) setRoute(ctx context.Context, idx int, node string) error {
	nn, err := netsetup.ForIndex(idx)
	if err != nil {
		return err
	}
	return gateway.SetRouteLocal(ctx, nn.Block(), "gre-"+node)
}

func (o *Orchestrator) delRoute(ctx context.Context, idx int) {
	if nn, err := netsetup.ForIndex(idx); err == nil {
		_ = gateway.DelRouteLocal(ctx, netip.Prefix(nn.Block()))
	}
}

// Delete stops a VM and forgets it.
func (o *Orchestrator) Delete(ctx context.Context, id string) error {
	vm, err := o.store.GetVM(id)
	if err != nil {
		return fmt.Errorf("unknown vm %q", id)
	}
	_ = o.store.SetState(id, StateDeleting)
	o.delRoute(ctx, vm.NetIndex)
	if ag, err := o.agentFor(vm.Node); err == nil {
		if err := ag.DeleteVM(ctx, id); err != nil && o.up(vm.Node) {
			return err // the node is up and refused: keep the record so it can be retried
		}
	}
	return o.store.DeleteVM(id)
}

func (o *Orchestrator) createVM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Base string `json:"base"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Base == "" {
		req.Base = "chrome-v1"
	}
	started := time.Now()
	vm, resp, err := o.Create(r.Context(), req.Base)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.HasPrefix(err.Error(), "no node") {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"vm": vm, "laps": resp.Laps, "orch_laps": resp.OrchLaps, "total_ms": float64(time.Since(started).Microseconds()) / 1000,
	})
}

func (o *Orchestrator) listVMs(w http.ResponseWriter, r *http.Request) {
	vms, err := o.store.ListVMs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Add what each node last reported about the VM's real memory cost.
	o.mu.RLock()
	for i, v := range vms {
		for _, info := range o.seen[v.Node].Status.VMs {
			if info.ID == v.ID {
				vms[i].PssMiB, vms[i].PrivateMiB = info.PssMiB, info.PrivateMiB
			}
		}
	}
	o.mu.RUnlock()
	writeJSON(w, http.StatusOK, vms)
}

func (o *Orchestrator) deleteVM(w http.ResponseWriter, r *http.Request) {
	if err := o.Delete(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (o *Orchestrator) listNodes(w http.ResponseWriter, r *http.Request) {
	rows, err := o.store.ListNodes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	vms, _ := o.store.ListVMs()
	count := map[string]int{}
	for _, v := range vms {
		if v.State != StateLost {
			count[v.Node]++
		}
	}
	used, _ := o.store.UsedMem()
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := []api.Node{}
	for _, n := range rows {
		s := o.seen[n.Name]
		out = append(out, api.Node{Name: n.Name, Up: !s.Seen.IsZero() && time.Since(s.Seen) < o.cfg.HeartbeatTimeout,
			Cordoned: n.Cordoned, VMs: count[n.Name], CapMemMiB: s.Status.CapMemMiB, UsedMiB: used[n.Name], AgentURL: n.AgentURL})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (o *Orchestrator) cordon(w http.ResponseWriter, r *http.Request, v bool) {
	if err := o.store.SetCordoned(r.PathValue("name"), v); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func newID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		panic(errors.New("no randomness"))
	}
	return "vm-" + hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
