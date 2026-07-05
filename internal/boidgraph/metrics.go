package boidgraph

import (
	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// Prometheus naming: Namespace_Subsystem_Name → boids_graph_*. The
// MetricsRegistry key (metricService) only dedupes registrations; the
// scrape name comes from the opts below.
const (
	metricNamespace = "boids"
	metricSubsystem = "graph"
	metricService   = "boids"
)

// publisherMetrics holds the Prometheus handles for the snapshot pipeline
// (spec: snapshots/entities published, drops, per-snapshot publish duration —
// enough to classify a sweep window as publisher-bound from :9090 alone). A
// nil *publisherMetrics is valid and no-ops, so unit tests construct a
// Publisher without a registry.
//
// Registration is idempotent per (metricService, name); the sim component
// that owns the Publisher is created once per process, so the
// already-registered branch never fires in practice.
type publisherMetrics struct {
	publishDuration prometheus.Observer
	entities        prometheus.Counter
	snapshots       prometheus.Counter
	dropped         prometheus.Counter
}

// newPublisherMetrics registers the pipeline metrics against reg. Returns nil
// when reg is nil (tests), which every accessor below treats as a no-op.
func newPublisherMetrics(reg *metric.MetricsRegistry) *publisherMetrics {
	if reg == nil {
		return nil
	}
	duration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "publish_duration_seconds",
		Help:      "Wall-clock time to publish one snapshot's boid entities (fan-out joined).",
		Buckets:   prometheus.ExponentialBuckets(0.0005, 2, 12),
	})
	_ = reg.RegisterHistogram(metricService, "publish_duration_seconds", duration)

	entities := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricNamespace, Subsystem: metricSubsystem,
		Name: "entities_published_total",
		Help: "Boid entities published to graph-ingest.",
	})
	_ = reg.RegisterCounter(metricService, "entities_published_total", entities)

	snapshots := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricNamespace, Subsystem: metricSubsystem,
		Name: "snapshots_published_total",
		Help: "Snapshots fully published (all boids joined).",
	})
	_ = reg.RegisterCounter(metricService, "snapshots_published_total", snapshots)

	dropped := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricNamespace, Subsystem: metricSubsystem,
		Name: "snapshots_dropped_total",
		Help: "Snapshots dropped at Offer because the publisher was busy (publisher-bound signal).",
	})
	_ = reg.RegisterCounter(metricService, "snapshots_dropped_total", dropped)

	return &publisherMetrics{
		publishDuration: duration,
		entities:        entities,
		snapshots:       snapshots,
		dropped:         dropped,
	}
}

func (m *publisherMetrics) observeDuration(seconds float64) {
	if m != nil {
		m.publishDuration.Observe(seconds)
	}
}

func (m *publisherMetrics) addEntities(n int) {
	if m != nil {
		m.entities.Add(float64(n))
	}
}

func (m *publisherMetrics) incSnapshot() {
	if m != nil {
		m.snapshots.Inc()
	}
}

func (m *publisherMetrics) incDropped() {
	if m != nil {
		m.dropped.Inc()
	}
}

// NewDialHzSetter registers the current-cadence gauge (boids_graph_dial_hz —
// the spec's "current cadence" observable) and returns a setter for it. The
// returned func is always safe to call and no-ops when reg is nil (tests).
// Exposed for the sim component, which owns the dial, so it needn't import
// prometheus itself.
func NewDialHzSetter(reg *metric.MetricsRegistry) func(hz float64) {
	if reg == nil {
		return func(float64) {}
	}
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "dial_hz",
		Help:      "Current graph snapshot cadence setpoint (the load dial).",
	})
	_ = reg.RegisterGauge(metricService, "dial_hz", g)
	return g.Set
}

// newE2ELatencyHistogram registers the probe's end-to-end ingest-latency
// histogram (boids_graph_e2e_latency_seconds). Returns nil when reg is nil.
func newE2ELatencyHistogram(reg *metric.MetricsRegistry) prometheus.Observer {
	if reg == nil {
		return nil
	}
	h := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "e2e_latency_seconds",
		Help:      "End-to-end ingest latency (observation time − observed_at) for boid entities landing in ENTITY_STATES.",
		// 1ms … ~524s: wide enough that a melt backlog (drain time can reach
		// minutes) lands in a finite bucket rather than saturating +Inf.
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 20),
	})
	_ = reg.RegisterHistogram(metricService, "e2e_latency_seconds", h)
	return h
}
