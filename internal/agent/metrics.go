package agent

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prometheus metrics of one node's agent, at /metrics (bearer token, like the
// rest of the agent's API). What the node does, in seconds; and what it
// holds right now, read at scrape time.
var agentBuckets = []float64{0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5, 0.75, 1, 1.5, 2, 3, 5, 10, 30}

type agentMetrics struct {
	reg *prometheus.Registry

	create       prometheus.Histogram
	migrateOut   prometheus.Histogram // the guest's pause
	forkImage    prometheus.Histogram // the parent's pause
	poolClaims   *prometheus.CounterVec
	vmFailures   *prometheus.CounterVec
	pullSeconds  prometheus.Histogram
	handlerKills prometheus.Counter
}

func newAgentMetrics(a *Agent) *agentMetrics {
	h := func(name, help string) prometheus.Histogram {
		return prometheus.NewHistogram(prometheus.HistogramOpts{Name: name, Help: help, Buckets: agentBuckets})
	}
	m := &agentMetrics{
		reg:         prometheus.NewRegistry(),
		create:      h("baton_agent_create_seconds", "Starting a VM on this node: base clone to browser answering."),
		migrateOut:  h("baton_agent_migrate_pause_seconds", "Guest pause of migrations out of this node."),
		forkImage:   h("baton_agent_fork_pause_seconds", "Parent pause when taking a fork image on this node."),
		pullSeconds: h("baton_agent_image_pull_seconds", "Bringing a fork image or snapshot to this node."),
		poolClaims: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "baton_agent_pool_claims_total",
			Help: "Creates by whether a pre-started jailed VMM was ready."}, []string{"result"}),
		vmFailures: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "baton_agent_failures_total",
			Help: "Failed operations on this node, by kind."}, []string{"kind"}),
		handlerKills: prometheus.NewCounter(prometheus.CounterOpts{Name: "baton_agent_orphaned_vms_killed_total",
			Help: "VMs killed because their page-fault handler died before the memory was complete."}),
	}
	gauge := func(name, help string, f func() float64) prometheus.GaugeFunc {
		return prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help}, f)
	}
	m.reg.MustRegister(m.create, m.migrateOut, m.forkImage, m.pullSeconds, m.poolClaims, m.vmFailures, m.handlerKills,
		gauge("baton_agent_vms", "VMs running on this node.", func() float64 { return float64(len(listVMs())) }),
		gauge("baton_agent_pool_ready", "Pre-started jailed VMMs ready.", func() float64 { return float64(a.pool.readyCount()) }),
		gauge("baton_agent_bases_held_mib", "RAM held by bases and fork images.", func() float64 {
			var n int
			for _, b := range listBases() {
				n += b.HeldMiB
			}
			return float64(n)
		}),
		gauge("baton_agent_outgoing", "Migrations whose memory this node is still serving.", func() float64 {
			a.outMu.Lock()
			defer a.outMu.Unlock()
			return float64(len(a.outgoing))
		}),
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return m
}

func (m *agentMetrics) handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

func since(t time.Time) float64 { return time.Since(t).Seconds() }
