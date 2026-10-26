package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/vedantxn/chromebox/internal/agent"
	"github.com/vedantxn/chromebox/internal/api"
	"github.com/vedantxn/chromebox/internal/base"
	"github.com/vedantxn/chromebox/internal/gateway"
	"github.com/vedantxn/chromebox/internal/node"
	"github.com/vedantxn/chromebox/internal/orchestrator"
	"github.com/vedantxn/chromebox/internal/remote"
	"github.com/vedantxn/chromebox/internal/timing"
)

// runAgent: `chromebox agent <listen>`; settings come from node.json and the
// environment (CHROMEBOX_TOKEN, CHROMEBOX_ORCHESTRATOR).
func runAgent(listen string) error {
	cfg, err := node.Load()
	if err != nil || cfg == nil {
		return fmt.Errorf("no node config at %s: %v", node.Path, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return agent.New(agent.Config{
		Node: cfg.Name, Listen: listen,
		Token: os.Getenv("CHROMEBOX_TOKEN"), Orchestrator: os.Getenv("CHROMEBOX_ORCHESTRATOR"),
	}).Run(ctx)
}

// runOrchestrator: `chromebox orchestrator <api-listen>`; the CDP proxy listens
// on :9222. Strategy from CHROMEBOX_STRATEGY (spread or pack).
func runOrchestrator(listen string) error {
	maxVMs, _ := strconv.Atoi(os.Getenv("CHROMEBOX_MAX_VMS"))
	db := os.Getenv("CHROMEBOX_DB") // a local copy, for development
	if db == "" {
		db = "/var/lib/chromebox/orchestrator.db"
	}
	auto := orchestrator.AutoscaleFromEnv()
	var instances orchestrator.Instances
	if auto.Enabled {
		e, err := orchestrator.NewEC2(context.Background(), "us-east-1")
		if err != nil {
			return err
		}
		instances = e
	}
	o, err := orchestrator.New(orchestrator.Config{
		Autoscale: auto, Instances: instances, Warm: orchestrator.WarmFromEnv(),
		Token: os.Getenv("CHROMEBOX_TOKEN"), DBPath: db,
		APIListen: listen, ProxyAdmin: "http://127.0.0.1:9301",
		SessionSecret: []byte(os.Getenv("CHROMEBOX_SESSION_SECRET")), MaxVMs: maxVMs,
		PublicURL: os.Getenv("CHROMEBOX_PUBLIC_URL"),
		Strategy:  orchestrator.Strategy(os.Getenv("CHROMEBOX_STRATEGY")),
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return o.Run(ctx)
}

// runProxy: `chromebox proxy <public-listen>` runs the CDP proxy as its own
// process, so restarting the orchestrator does not cut anyone's browser.
// Three listeners:
//   - public (behind CloudFront): only /cdp/<signed token>
//   - :9222, operator only (security group): /v/<n> without a token, for tests
//   - 127.0.0.1:9301: admin; the orchestrator asks it to close a VM's
//     connections, and sets or drops a session's browser settings
//   - :3129 (gateway.EgressPort): proxied VMs' connections, redirected here and
//     sent on through the customer's proxy
func runProxy(public string) error {
	secret := os.Getenv("CHROMEBOX_SESSION_SECRET")
	if len(secret) < 32 {
		return fmt.Errorf("CHROMEBOX_SESSION_SECRET must be at least 32 characters")
	}
	p := gateway.NewProxy(nil)
	p.SetSecret([]byte(secret))
	admin := http.NewServeMux()
	admin.HandleFunc("POST /close/{n}", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		json.NewEncoder(w).Encode(map[string]int{"Closed": p.CloseVM(n)})
	})
	// PUT /settings/{n}?wait=<duration>: set VM n's browser settings, and with
	// wait, answer once its open tabs have them.
	admin.HandleFunc("PUT /settings/{n}", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		var s gateway.Settings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		wait, _ := time.ParseDuration(r.URL.Query().Get("wait"))
		if err := p.SetSettings(n, s, wait); err != nil {
			http.Error(w, err.Error(), http.StatusGatewayTimeout)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	admin.HandleFunc("DELETE /settings/{n}", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		p.DropSettings(n)
		w.WriteHeader(http.StatusNoContent)
	})
	// Around a snapshot of VM n: hold detaches the proxy's session from the
	// browser (so no copy inherits it), release brings it back.
	admin.HandleFunc("POST /settings/{n}/hold", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		p.HoldSettings(n)
		w.WriteHeader(http.StatusNoContent)
	})
	admin.HandleFunc("POST /settings/{n}/release", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		p.ReleaseSettings(n)
		w.WriteHeader(http.StatusNoContent)
	})
	errc := make(chan error, 4)
	go func() { errc <- p.ServeEgress(fmt.Sprintf(":%d", gateway.EgressPort)) }()
	for addr, h := range map[string]http.Handler{public: p.Public(), ":9222": p, "127.0.0.1:9301": admin} {
		srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
		go func() { errc <- srv.ListenAndServe() }()
	}
	log.Printf("cdp proxy: public %s, operator :9222, admin 127.0.0.1:9301, egress :%d", public, gateway.EgressPort)
	return <-errc
}

// ctlCmd is the command-line client for the orchestrator's API.
// CHROMEBOX_API (default http://127.0.0.1:8080) and CHROMEBOX_TOKEN select it.
func ctlCmd(args []string) error {
	url := os.Getenv("CHROMEBOX_API")
	if url == "" {
		url = "http://127.0.0.1:8080"
	}
	call := func(method, path string, in, out any) error {
		var body io.Reader
		if in != nil {
			b, _ := json.Marshal(in)
			body = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, url+path, body)
		req.Header.Set("Authorization", "Bearer "+os.Getenv("CHROMEBOX_TOKEN"))
		resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			msg, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(msg))
		}
		if out != nil {
			return json.NewDecoder(resp.Body).Decode(out)
		}
		return nil
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: chromebox ctl create [base] | ls | nodes | rm <id> | cordon|uncordon <node>")
	}
	switch args[0] {
	case "create":
		b := base.Default
		if len(args) > 1 {
			b = args[1]
		}
		var out struct {
			VM      api.VM        `json:"vm"`
			TotalMs float64       `json:"total_ms"`
			Laps    timing.Record `json:"laps"`
		}
		if err := call("POST", "/v1/vms", map[string]string{"base": b}, &out); err != nil {
			return err
		}
		fmt.Printf("%s  node %s  index %d  cdp %s   created in %.0f ms\n", out.VM.ID, out.VM.Node, out.VM.NetIndex, out.VM.CDPPath, out.TotalMs)
		if os.Getenv("CHROMEBOX_VERBOSE") != "" {
			for _, l := range out.Laps.Laps {
				fmt.Printf("    node: %-26s %7.1f ms\n", l.Name, l.StepMs)
			}
		}
	case "ls":
		var vms []api.VM
		if err := call("GET", "/v1/vms", nil, &vms); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNODE\tSTATE\tINDEX\tCDP\tBASE\tPSS (MiB)\tPRIVATE (MiB)")
		for _, v := range vms {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%.0f\t%.0f\n", v.ID, v.Node, v.State, v.NetIndex, v.CDPPath, v.Base, v.PssMiB, v.PrivateMiB)
		}
		tw.Flush()
	case "nodes":
		var ns []api.Node
		if err := call("GET", "/v1/nodes", nil, &ns); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NODE\tUP\tCORDONED\tVMS\tMEMORY USED/CAP (MiB)\tSTATE")
		for _, n := range ns {
			fmt.Fprintf(tw, "%s\t%v\t%v\t%d\t%d / %d\t%s\n", n.Name, n.Up, n.Cordoned, n.VMs, n.UsedMiB, n.CapMemMiB, n.State)
		}
		tw.Flush()
	case "bench":
		// bench <n> [parallel]: create n VMs (parallel at a time) and delete them,
		// summarizing how long each create took.
		n, par := 20, 1
		if len(args) > 1 {
			fmt.Sscanf(args[1], "%d", &n)
		}
		if len(args) > 2 {
			fmt.Sscanf(args[2], "%d", &par)
		}
		var mu sync.Mutex
		var totals []time.Duration
		perNode := map[string]int{}
		sem := make(chan struct{}, par)
		var wg sync.WaitGroup
		var firstErr error
		started := time.Now()
		for i := 0; i < n; i++ {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				var out struct {
					VM      api.VM  `json:"vm"`
					TotalMs float64 `json:"total_ms"`
				}
				if err := call("POST", "/v1/vms", map[string]string{"base": base.Default}, &out); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
				mu.Lock()
				totals = append(totals, time.Duration(out.TotalMs*float64(time.Millisecond)))
				perNode[out.VM.Node]++
				mu.Unlock()
				_ = call("DELETE", "/v1/vms/"+out.VM.ID, nil, nil)
			}()
		}
		wg.Wait()
		if len(totals) == 0 {
			return firstErr
		}
		sm := timing.Summarize(totals)
		fmt.Printf("%d creates, %d at a time, %.1f s wall clock, per node %v\n", len(totals), par, time.Since(started).Seconds(), perNode)
		fmt.Printf("create (request -> browser answering, routed): p50 %.0f ms   p95 %.0f ms   max %.0f ms\n",
			float64(sm.P50.Microseconds())/1000, float64(sm.P95.Microseconds())/1000, float64(sm.Max.Microseconds())/1000)
		return firstErr
	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: chromebox ctl rm <id>")
		}
		return call("DELETE", "/v1/vms/"+args[1], nil, nil)
	case "migrate", "drain":
		if len(args) < 2 {
			return fmt.Errorf("usage: chromebox ctl migrate <id> [node] | drain <node>")
		}
		var out any
		path := "/v1/vms/" + args[1] + "/migrate"
		if len(args) > 2 {
			path += "?to=" + args[2]
		}
		if args[0] == "drain" {
			path = "/v1/nodes/" + args[1] + "/drain"
		}
		if err := call("POST", path, nil, &out); err != nil {
			return err
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	case "pause", "resume":
		if len(args) < 2 {
			return fmt.Errorf("usage: chromebox ctl %s <id>", args[0])
		}
		var out any
		if err := call("POST", "/v1/vms/"+args[1]+"/"+args[0], nil, &out); err != nil {
			return err
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	case "fork":
		if len(args) < 3 {
			return fmt.Errorf("usage: chromebox ctl fork <id> <count>")
		}
		n, err := strconv.Atoi(args[2])
		if err != nil {
			return err
		}
		var out any
		if err := call("POST", "/v1/vms/"+args[1]+"/fork", map[string]int{"count": n}, &out); err != nil {
			return err
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	case "cordon", "uncordon":
		if len(args) < 2 {
			return fmt.Errorf("usage: chromebox ctl %s <node>", args[0])
		}
		return call("POST", "/v1/nodes/"+args[1]+"/"+args[0], nil, nil)
	default:
		return fmt.Errorf("unknown ctl command %q", args[0])
	}
	return nil
}

// basePush copies a stored base snapshot to another node: `base push <name> <ssh-target>`.
// tar streams it through the shared ssh connection; --sparse keeps the disk
// image's holes and the memory file's zero pages cheap.
func basePush(ctx context.Context, name, host string) error {
	if _, err := base.Read(name); err != nil {
		return fmt.Errorf("base %q: %w", name, err)
	}
	r := remote.New(host)
	dir := strings.TrimPrefix(base.Dir+"/"+name, "/")
	if _, err := r.Run(ctx, "mkdir", "-p", base.Dir); err != nil {
		return err
	}
	send := exec.CommandContext(ctx, "tar", "-C", "/", "--sparse", "-cf", "-", dir)
	recv := r.Command(ctx, "tar", "-C", "/", "-xf", "-")
	pipe, err := send.StdoutPipe()
	if err != nil {
		return err
	}
	recv.Stdin = pipe
	var e1, e2 bytes.Buffer
	send.Stderr, recv.Stderr = &e1, &e2
	if err := recv.Start(); err != nil {
		return err
	}
	if err := send.Start(); err != nil {
		return err
	}
	se, re := send.Wait(), recv.Wait()
	if se != nil || re != nil {
		return fmt.Errorf("push: send: %v %s; receive: %v %s", se, e1.String(), re, e2.String())
	}
	return nil
}
