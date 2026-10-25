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
	"github.com/vedantxn/chromebox/internal/token"
)

// Config is what the orchestrator needs to run.
type Config struct {
	Token            string
	DBPath           string
	APIListen        string        // e.g. ":8080"
	ProxyAdmin       string        // the CDP proxy's local admin address, e.g. "http://127.0.0.1:9301"
	SessionSecret    []byte        // signs session tokens the proxy checks
	MaxVMs           int           // refuse creates beyond this many VMs (0: no cap)
	PublicURL        string        // how clients reach the proxy, e.g. "https://xxxx.cloudfront.net"
	Strategy         Strategy      // placement
	HeartbeatTimeout time.Duration // a node silent for longer is down

	Autoscale AutoscaleConfig // start and stop nodes by load (off unless enabled)
	Warm      WarmConfig      // browsers kept running and warmed up, handed out on create
	Instances Instances       // the machines behind nodes; needed when Autoscale is enabled
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

	goneMu sync.Mutex
	gone   map[string]time.Time // running VMs missing from their node's heartbeats, since when

	started time.Time

	metrics  *metrics
	auto     *autoscaler   // nil unless autoscaling is enabled
	warmKick chan struct{} // asks the warm loop to top the pool up

	opMu   sync.Mutex
	lastOp map[string]time.Time // node -> when an operation last touched it (see touch)

	tries attempts // failed sign-ins and new accounts, for their limits
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
	if err := st.accountSchema(); err != nil {
		return nil, err
	}
	o := &Orchestrator{cfg: cfg, store: st, seen: map[string]nodeState{}, pending: map[string]int{}, gone: map[string]time.Time{}, started: time.Now(),
		warmKick: make(chan struct{}, 1), lastOp: map[string]time.Time{}}
	o.metrics = newMetrics(o)
	if cfg.Autoscale.Enabled && cfg.Instances != nil {
		o.auto = newAutoscaler(o, cfg.Autoscale, cfg.Instances)
	}
	return o, nil
}

// Run serves the API and the CDP proxy until ctx ends.
func (o *Orchestrator) Run(ctx context.Context) error {
	api := &http.Server{Addr: o.cfg.APIListen, Handler: o.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go o.reconcileLoop(ctx)
	if o.cfg.Warm.PerNode > 0 {
		go o.warmLoop(ctx)
	}
	if o.auto != nil {
		go o.auto.run(ctx)
		log.Printf("orchestrator: autoscaling on (always on: %s, at most %d nodes, cooldown %s, idle stop %s)",
			o.cfg.Autoscale.AlwaysOn, o.cfg.Autoscale.MaxNodes, o.cfg.Autoscale.Cooldown, o.cfg.Autoscale.IdleStop)
	}
	errc := make(chan error, 1)
	go func() { errc <- api.ListenAndServe() }()
	log.Printf("orchestrator: API on %s, strategy %s, max VMs %d", o.cfg.APIListen, o.cfg.Strategy, o.cfg.MaxVMs)
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
	mux.Handle("GET /metrics", o.metrics.handler())
	mux.HandleFunc("GET /v1/stats", o.stats)
	mux.HandleFunc("POST /v1/vms", o.createVM)
	mux.HandleFunc("GET /v1/vms", o.listVMs)
	mux.HandleFunc("DELETE /v1/vms/{id}", o.deleteVM)
	mux.HandleFunc("GET /v1/nodes", o.listNodes)
	mux.HandleFunc("POST /v1/vms/{id}/migrate", o.migrateVM)
	mux.HandleFunc("POST /v1/vms/{id}/fork", o.forkVM)
	mux.HandleFunc("POST /v1/vms/{id}/pause", func(w http.ResponseWriter, r *http.Request) {
		res, err := o.Pause(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /v1/vms/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		res, err := o.Resume(r.Context(), r.PathValue("id"), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /v1/vms/{id}/token", o.issueToken)
	o.accountRoutes(mux)
	mux.HandleFunc("POST /v1/nodes/{name}/drain", o.drainNode)
	mux.HandleFunc("POST /v1/nodes/{name}/cordon", func(w http.ResponseWriter, r *http.Request) { o.cordon(w, r, true) })
	mux.HandleFunc("POST /v1/nodes/{name}/uncordon", func(w http.ResponseWriter, r *http.Request) { o.cordon(w, r, false) })
	return o.auth(o.resolveMe(mux))
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
	o.fence(st)
	o.vanished(st)
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
// Create's onReserve, if set, runs right after the VM's row is written and
// before the VM starts (an account's session record is written there).
func (o *Orchestrator) Create(ctx context.Context, baseName string, ttl time.Duration, onReserve ...func(id string) error) (api.VM, api.CreateVMResponse, error) {
	start := time.Now()
	vm, resp, err := o.create(ctx, baseName, ttl, onReserve...)
	if err != nil {
		o.metrics.errors.WithLabelValues("create").Inc()
		if strings.HasPrefix(err.Error(), "no node") {
			o.auto.Pressure() // nil-safe: starts a node, if autoscaling is on
		}
		return vm, resp, err
	}
	o.touch(vm.Node)
	d := time.Since(start)
	o.metrics.create.Observe(secs(d))
	ev := map[string]any{"node": vm.Node, "base": vm.Base, "create_ms": ms(d)}
	if resp.Warm {
		ev["warm"] = true // handed a browser that was already running and warmed up
	}
	o.store.addEvent(vm.ID, "created", ev)
	return vm, resp, nil
}

func (o *Orchestrator) create(ctx context.Context, baseName string, ttl time.Duration, onReserve ...func(id string) error) (api.VM, api.CreateVMResponse, error) {
	if o.cfg.Warm.PerNode > 0 && baseName == o.cfg.Warm.Base {
		sw := timing.Start()
		o.place.Lock()
		vm, ok, err := o.claimWarm(baseName, ttl, onReserve)
		o.place.Unlock()
		if err != nil {
			return api.VM{}, api.CreateVMResponse{}, err
		}
		if ok {
			o.metrics.warmClaims.WithLabelValues("hit").Inc()
			o.kickWarm()
			sw.Lap("claimed a warm browser")
			vm.CDPPath = fmt.Sprintf("/v/%d", vm.NetIndex)
			return vm, api.CreateVMResponse{OrchLaps: sw.Record(), Warm: true}, nil
		}
		o.metrics.warmClaims.WithLabelValues("miss").Inc()
		defer o.kickWarm()
	}
	return o.createCold(ctx, baseName, ttl, "", StateRunning, onReserve...)
}

// createCold places a VM (on node, if given), starts it from the base snapshot
// and routes the gateway to it, leaving it in state final.
func (o *Orchestrator) createCold(ctx context.Context, baseName string, ttl time.Duration, onNode, final string, onReserve ...func(id string) error) (api.VM, api.CreateVMResponse, error) {
	sw := timing.Start()
	mem, ok := o.baseMem(baseName)
	if !ok {
		return api.VM{}, api.CreateVMResponse{}, fmt.Errorf("no node holds base %q", baseName)
	}
	o.place.Lock()
	if o.cfg.MaxVMs > 0 {
		if vms, err := o.store.ListVMs(); err == nil && countLive(vms) >= o.cfg.MaxVMs {
			o.place.Unlock()
			return api.VM{}, api.CreateVMResponse{}, fmt.Errorf("no node: at capacity (%d VMs)", o.cfg.MaxVMs)
		}
	}
	views, err := o.views()
	if err != nil {
		o.place.Unlock()
		return api.VM{}, api.CreateVMResponse{}, err
	}
	if onNode != "" {
		var only []NodeView
		for _, v := range views {
			if v.Name == onNode {
				only = append(only, v)
			}
		}
		views = only
	}
	node, err := Place(views, Request{MemMiB: mem, Base: baseName}, o.cfg.Strategy)
	if err != nil && onNode == "" && o.evictWarm(ctx) { // a warm browser was in the way: make room
		if views, err = o.views(); err == nil {
			node, err = Place(views, Request{MemMiB: mem, Base: baseName}, o.cfg.Strategy)
		}
	}
	if err != nil {
		o.place.Unlock()
		return api.VM{}, api.CreateVMResponse{}, err
	}
	vm, err := o.store.InsertVM(newID(), node, baseName, mem, time.Now().Add(ttl))
	if err == nil && len(onReserve) > 0 {
		if err = onReserve[0](vm.ID); err != nil {
			_ = o.store.DeleteVM(vm.ID)
		}
	}
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
		o.store.endSessionRecord(vm.ID, "failed to start")
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
	if err := o.store.SetState(vm.ID, final); err != nil {
		return fail(err)
	}
	vm.State, vm.CDPPath = final, fmt.Sprintf("/v/%d", vm.NetIndex)
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
	if !isWarm(vm.State) {
		o.touch(vm.Node)
	}
	_ = o.store.SetState(id, StateDeleting)
	if vm.State == StatePaused { // its snapshot is in storage, not on a node
		if prefix, _, err := o.store.SnapInfo(id); err == nil {
			go o.deleteSnapshots(prefix)
		}
	}
	o.delRoute(ctx, vm.NetIndex)
	o.closeConns(vm.NetIndex)
	if ag, err := o.agentFor(vm.Node); err == nil {
		if err := ag.DeleteVM(ctx, id); err != nil && o.up(vm.Node) {
			return err // the node is up and refused: keep the record so it can be retried
		}
	}
	o.store.endSessionRecord(id, "closed")
	return o.store.DeleteVM(id)
}

func (o *Orchestrator) createVM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Base       string `json:"base"`
		TTLSeconds int    `json:"ttl_seconds"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Base == "" {
		req.Base = "chrome-v1"
	}
	if req.TTLSeconds <= 0 || req.TTLSeconds > 3600 {
		req.TTLSeconds = 900 // a VM nobody deletes is deleted after 15 minutes
	}
	started := time.Now()
	vm, resp, err := o.Create(r.Context(), req.Base, time.Duration(req.TTLSeconds)*time.Second)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.HasPrefix(err.Error(), "no node") {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"vm": vm, "connect_token": o.sign(vm, "cdp", time.Until(vm.ExpiresAt())), "laps": resp.Laps, "orch_laps": resp.OrchLaps, "total_ms": float64(time.Since(started).Microseconds()) / 1000,
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
	warm := map[string]int{}
	for _, v := range vms {
		switch {
		case isWarm(v.State):
			warm[v.Node]++
		case v.State != StateLost:
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
			Cordoned: n.Cordoned, VMs: count[n.Name], Warm: warm[n.Name], CapMemMiB: s.Status.CapMemMiB, UsedMiB: used[n.Name], AgentURL: n.AgentURL,
			State: o.auto.NodeState(n.Name)})
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

// sign issues a token for VM v, valid for ttl (never beyond the VM's own expiry).
func (o *Orchestrator) sign(v api.VM, kind string, ttl time.Duration) string {
	exp := time.Now().Add(ttl)
	if e := v.ExpiresAt(); !e.IsZero() && e.Before(exp) {
		exp = e
	}
	return token.Sign(o.cfg.SessionSecret, token.Claims{Session: v.ID, Index: v.NetIndex, Expires: exp.Unix(), Kind: kind})
}

// issueToken: POST /v1/vms/{id}/token?kind=view&ttl=300 gives a fresh token,
// e.g. a short-lived one for the dashboard's live view.
func (o *Orchestrator) issueToken(w http.ResponseWriter, r *http.Request) {
	v, err := o.store.GetVM(r.PathValue("id"))
	if err != nil || (v.State != StateRunning && v.State != StateMigrating) {
		http.Error(w, "no running vm "+r.PathValue("id"), http.StatusNotFound)
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "cdp"
	}
	ttl, _ := time.ParseDuration(r.URL.Query().Get("ttl") + "s")
	if ttl <= 0 || ttl > time.Hour {
		ttl = 5 * time.Minute
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": o.sign(v, kind, ttl)})
}

// closeConns asks the CDP proxy to close every client connection to VM index n.
func (o *Orchestrator) closeConns(n int) int {
	if o.cfg.ProxyAdmin == "" {
		return 0
	}
	resp, err := http.Post(fmt.Sprintf("%s/close/%d", o.cfg.ProxyAdmin, n), "text/plain", nil)
	if err != nil {
		log.Printf("proxy admin: %v", err)
		return 0
	}
	defer resp.Body.Close()
	var out struct{ Closed int }
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.Closed
}

// countLive counts the VMs that occupy a node: paused ones live in storage.
func countLive(vms []api.VM) int {
	n := 0
	for _, v := range vms {
		if v.State != StatePaused && !isWarm(v.State) { // warm browsers make room when needed
			n++
		}
	}
	return n
}
