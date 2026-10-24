package orchestrator

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prometheus metrics for the control plane, served at /metrics (behind the
// same bearer token as the rest of the API). Timings are histograms in
// seconds with buckets that resolve the 100 ms to 10 s range the operations
// live in; gauges about nodes and VMs are read at scrape time, from the
// database and the last heartbeats, so they cannot drift from them.
var timeBuckets = []float64{0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5, 0.75, 1, 1.5, 2, 3, 5, 10, 30}

type metrics struct {
	reg *prometheus.Registry

	create     prometheus.Histogram
	migPause   prometheus.Histogram
	migStream  prometheus.Histogram
	forkPause  prometheus.Histogram
	forkTotal  prometheus.Histogram
	pause      prometheus.Histogram
	resume     prometheus.Histogram
	pauses     *prometheus.CounterVec // result
	resumes    *prometheus.CounterVec // result
	migrations *prometheus.CounterVec // result
	forks      *prometheus.CounterVec // result
	forkCopies prometheus.Counter
	errors     *prometheus.CounterVec // kind
	autoscale  *prometheus.CounterVec // action
	warmClaims *prometheus.CounterVec // hit, miss
	warmReady  prometheus.Histogram
}

func newMetrics(o *Orchestrator) *metrics {
	h := func(name, help string) prometheus.Histogram {
		return prometheus.NewHistogram(prometheus.HistogramOpts{Name: name, Help: help, Buckets: timeBuckets})
	}
	m := &metrics{
		reg:        prometheus.NewRegistry(),
		create:     h("baton_create_seconds", "Orchestrator create: request to browser answering, route set."),
		migPause:   h("baton_migration_pause_seconds", "How long the guest was paused during a migration."),
		migStream:  h("baton_migration_stream_seconds", "How long a migration's memory kept arriving after the VM resumed."),
		forkPause:  h("baton_fork_parent_pause_seconds", "How long the parent was paused to take a fork image."),
		forkTotal:  h("baton_fork_seconds", "Fork: request to every copy answering."),
		pause:      h("baton_pause_seconds", "Pause: request to the session stored and its browser stopped."),
		resume:     h("baton_resume_seconds", "Resume: request to the browser answering again."),
		pauses:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "baton_pauses_total", Help: "Pauses by result."}, []string{"result"}),
		resumes:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "baton_resumes_total", Help: "Resumes by result."}, []string{"result"}),
		migrations: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "baton_migrations_total", Help: "Migrations by result."}, []string{"result"}),
		forks:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "baton_forks_total", Help: "Forks by result."}, []string{"result"}),
		forkCopies: prometheus.NewCounter(prometheus.CounterOpts{Name: "baton_fork_copies_total", Help: "Copies started by forks."}),
		errors:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "baton_errors_total", Help: "Failures by kind."}, []string{"kind"}),
		autoscale:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "baton_autoscaler_actions_total", Help: "Autoscaler actions by kind."}, []string{"action"}),
		warmClaims: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "baton_warm_claims_total", Help: "Creates served from the warm pool (hit) or by a restore (miss)."}, []string{"result"}),
		warmReady:  h("baton_warm_ready_seconds", "Warm pool: starting a browser and warming it up."),
	}
	m.reg.MustRegister(m.pause, m.resume, m.pauses, m.resumes, m.create, m.autoscale, m.migPause, m.migStream, m.forkPause, m.forkTotal, m.migrations, m.forks, m.forkCopies, m.errors, m.warmClaims, m.warmReady,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}), &stateCollector{o})
	return m
}

func (m *metrics) handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// stateCollector reports what the orchestrator believes right now.
type stateCollector struct{ o *Orchestrator }

var (
	descVMs      = prometheus.NewDesc("baton_vms", "VMs by state.", []string{"state"}, nil)
	descNodeUp   = prometheus.NewDesc("baton_node_up", "1 if the node has sent a heartbeat recently.", []string{"node"}, nil)
	descNodeCord = prometheus.NewDesc("baton_node_cordoned", "1 if the node takes no new VMs.", []string{"node"}, nil)
	descNodeVMs  = prometheus.NewDesc("baton_node_vms", "VMs the database places on the node.", []string{"node"}, nil)
	descNodeMem  = prometheus.NewDesc("baton_node_memory_mib", "Node memory for VMs: capacity, and used by assigned VMs.", []string{"node", "kind"}, nil)
)

func (c *stateCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{descVMs, descNodeUp, descNodeCord, descNodeVMs, descNodeMem} {
		ch <- d
	}
}

func (c *stateCollector) Collect(ch chan<- prometheus.Metric) {
	o := c.o
	states := map[string]int{StateCreating: 0, StateRunning: 0, StateMigrating: 0, StateLost: 0, StateDeleting: 0}
	perNode := map[string]int{}
	if vms, err := o.store.ListVMs(); err == nil {
		for _, v := range vms {
			states[v.State]++
			perNode[v.Node]++
		}
	}
	for s, n := range states {
		ch <- prometheus.MustNewConstMetric(descVMs, prometheus.GaugeValue, float64(n), s)
	}
	views, err := o.views()
	if err != nil {
		return
	}
	b2f := func(b bool) float64 {
		if b {
			return 1
		}
		return 0
	}
	for _, v := range views {
		ch <- prometheus.MustNewConstMetric(descNodeUp, prometheus.GaugeValue, b2f(v.Up), v.Name)
		ch <- prometheus.MustNewConstMetric(descNodeCord, prometheus.GaugeValue, b2f(v.Cordoned), v.Name)
		ch <- prometheus.MustNewConstMetric(descNodeVMs, prometheus.GaugeValue, float64(perNode[v.Name]), v.Name)
		ch <- prometheus.MustNewConstMetric(descNodeMem, prometheus.GaugeValue, float64(v.CapMemMiB), v.Name, "capacity")
		ch <- prometheus.MustNewConstMetric(descNodeMem, prometheus.GaugeValue, float64(v.UsedMemMiB), v.Name, "used")
	}
}

func secs(d time.Duration) float64 { return d.Seconds() }
