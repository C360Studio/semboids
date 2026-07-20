package api

import (
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/graphview"
	"github.com/prometheus/client_golang/prometheus"
)

// Prometheus naming: Namespace_Subsystem_Name → boids_graphview_*. The write
// side has had metrics since the load-dial work; this gives the read side the
// same treatment, which matters more now that one shared view serves every
// client — a stall there is invisible per-connection.
const (
	viewMetricNamespace = "boids"
	viewMetricSubsystem = "graphview"
	viewMetricService   = "boids"
)

// viewMetrics holds the Prometheus handles behind the graphview Hooks seam. A
// nil *viewMetrics is valid and no-ops, so tests construct views without a
// registry.
type viewMetrics struct {
	caughtUp        *prometheus.GaugeVec
	appliedRevision *prometheus.GaugeVec
	subscribers     *prometheus.GaugeVec
	maxPending      *prometheus.GaugeVec
	coalescedDrops  *prometheus.CounterVec
	poison          *prometheus.CounterVec
	watcherLost     *prometheus.CounterVec
}

// newViewMetrics registers the read-side metrics against reg. Returns nil when
// reg is nil, which hooks() treats as a no-op.
func newViewMetrics(reg *metric.MetricsRegistry) *viewMetrics {
	if reg == nil {
		return nil
	}
	labels := []string{"bucket"}
	gauge := func(name, help string) *prometheus.GaugeVec {
		g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: viewMetricNamespace, Subsystem: viewMetricSubsystem,
			Name: name, Help: help,
		}, labels)
		_ = reg.RegisterGaugeVec(viewMetricService, name, g)
		return g
	}
	counter := func(name, help string) *prometheus.CounterVec {
		c := prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: viewMetricNamespace, Subsystem: viewMetricSubsystem,
			Name: name, Help: help,
		}, labels)
		_ = reg.RegisterCounterVec(viewMetricService, name, c)
		return c
	}

	return &viewMetrics{
		caughtUp: gauge("caught_up",
			"1 when the view's projection is current with its bucket, 0 while bootstrapping or after watcher loss."),
		appliedRevision: gauge("applied_revision",
			"Greatest KV revision applied to the view's projection."),
		subscribers: gauge("subscribers",
			"Currently attached local subscribers (one per connected SSE client)."),
		maxPending: gauge("max_pending_deltas",
			"Largest per-subscriber pending-delta buffer after the last fan-out window."),
		coalescedDrops: counter("coalesced_drops_total",
			"Pending deltas overwritten before delivery — the slow-subscriber staleness signal."),
		poison: counter("poison_total",
			"Writes that failed to decode or validate, surfaced as per-key poison."),
		watcherLost: counter("watcher_lost_total",
			"Times the shared watcher was lost and the view failed closed."),
	}
}

// hooks builds the graphview observability callbacks for one bucket.
//
// The labeled children are resolved ONCE here rather than per callback: OnApply
// fires for every delivered entry, and a WithLabelValues map lookup on that path
// would be pure overhead. The callbacks run on the view's watcher and ticker
// goroutines, so they stay to counter/gauge sets — nothing that can block.
func (m *viewMetrics) hooks(bucket string) graphview.Hooks {
	if m == nil {
		return graphview.Hooks{}
	}
	caughtUp := m.caughtUp.WithLabelValues(bucket)
	appliedRevision := m.appliedRevision.WithLabelValues(bucket)
	subscribers := m.subscribers.WithLabelValues(bucket)
	maxPending := m.maxPending.WithLabelValues(bucket)
	coalescedDrops := m.coalescedDrops.WithLabelValues(bucket)
	poison := m.poison.WithLabelValues(bucket)
	watcherLost := m.watcherLost.WithLabelValues(bucket)

	return graphview.Hooks{
		OnApply: func(_ string, revision uint64) {
			appliedRevision.Set(float64(revision))
		},
		OnCaughtUp: func() {
			caughtUp.Set(1)
		},
		OnWatcherLost: func(error) {
			// Pair the counter with clearing caught_up: a dashboard that only
			// watched the counter would miss a view sitting stale.
			watcherLost.Inc()
			caughtUp.Set(0)
		},
		OnPoison: func(string, error) {
			poison.Inc()
		},
		OnSubscribers: func(n int) {
			subscribers.Set(float64(n))
		},
		OnFanOut: func(overwritten, largestPending int) {
			if overwritten > 0 {
				coalescedDrops.Add(float64(overwritten))
			}
			maxPending.Set(float64(largestPending))
		},
	}
}
