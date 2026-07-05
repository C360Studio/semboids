package boidgraph

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/metric"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
)

// DefaultProbeSampleN is the 1-in-N sampling rate for the e2e latency probe
// (D4): record every 10th boid update to bound probe cost at melt rates.
const DefaultProbeSampleN = 10

// entityStatesBucket is the KV bucket graph-ingest writes entity states to.
const entityStatesBucket = "ENTITY_STATES"

// kvBucketSource is the slice of the NATS client the probe needs — an
// interface so the watch loop is exercised only by the integration test
// (3.3) while observe() stays pure and unit-testable. WaitForBucket (not a
// bare Get) tolerates the startup race: graph-ingest creates ENTITY_STATES at
// its own Start, which can land a few ms after the sim's.
type kvBucketSource interface {
	WaitForBucket(ctx context.Context, name string, timeout time.Duration) (jetstream.KeyValue, error)
}

// bucketWaitTimeout bounds how long the probe waits for ENTITY_STATES to
// appear before giving up (generous: graph-ingest creates it at startup).
const bucketWaitTimeout = 60 * time.Second

// LatencyProbe watches ENTITY_STATES for boid entities and records the
// end-to-end ingest latency (observation time − observed_at) into a
// Prometheus histogram, sampling 1-in-N to bound cost at saturation rates
// (D4). It runs with the sim's publisher service, independently of any UI or
// SSE client — the substrate-side counterpart to the publisher's ceiling
// metrics, so a sweep window's saturation source is attributable from :9090.
type LatencyProbe struct {
	kv      kvBucketSource
	hist    prometheus.Observer
	sampleN uint64
	logger  *slog.Logger

	// now is injectable so unit tests pin the observation clock.
	now func() time.Time
	// counter advances per sampled boid update (post non-boid filter).
	counter atomic.Uint64
}

// NewLatencyProbe builds a probe against the given KV source and metrics
// registry. sampleN ≤ 0 defaults to DefaultProbeSampleN; reg may be nil
// (tests / metrics disabled), which makes every observation a no-op.
func NewLatencyProbe(kv kvBucketSource, reg *metric.MetricsRegistry, sampleN int, logger *slog.Logger) *LatencyProbe {
	if logger == nil {
		logger = slog.Default()
	}
	if sampleN <= 0 {
		sampleN = DefaultProbeSampleN
	}
	return &LatencyProbe{
		kv:      kv,
		hist:    newE2ELatencyHistogram(reg),
		sampleN: uint64(sampleN),
		logger:  logger,
		now:     time.Now,
	}
}

// Run watches ENTITY_STATES until ctx is cancelled, feeding each update to
// observe. Mirrors the SSE bridge's watch pattern but is a separate,
// always-on watcher (D4): the probe must record regardless of browsers
// attached. It waits for the bucket to exist (startup race) before watching.
// Returns the setup error if the bucket never appears or the watcher can't be
// created; a cancelled ctx returns nil.
func (p *LatencyProbe) Run(ctx context.Context) error {
	kv, err := p.kv.WaitForBucket(ctx, entityStatesBucket, bucketWaitTimeout)
	if err != nil {
		return err
	}
	watcher, err := kv.WatchAll(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Stop() }()

	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-watcher.Updates():
			if !ok {
				return nil
			}
			if entry == nil {
				continue // initial-sync completion marker
			}
			deleted := entry.Operation() == jetstream.KeyValueDelete ||
				entry.Operation() == jetstream.KeyValuePurge
			p.observe(entry.Key(), entry.Value(), deleted)
		}
	}
}

// observe records one KV event's latency when it is a sampled boid update.
// Non-boid keys, deletes, and malformed payloads are skipped without error.
// Sampling happens before the JSON unmarshal so probe cost stays bounded at
// melt rates (the unmarshal is the expensive step).
func (p *LatencyProbe) observe(key string, value []byte, deleted bool) {
	if deleted || !isBoidEntityKey(key) {
		return
	}
	if p.counter.Add(1)%p.sampleN != 0 {
		return
	}
	observedAt, ok := parseObservedAt(value)
	if !ok {
		return
	}
	latency := p.now().Sub(observedAt)
	if latency < 0 {
		latency = 0 // guard clock coarseness; never record negative
	}
	if p.hist != nil {
		p.hist.Observe(latency.Seconds())
	}
}

// isBoidEntityKey filters ENTITY_STATES keys to boid entities (the 6-part ID
// BoidEntityID renders: <org>.<platform>.sim.flock.boid.<id>).
func isBoidEntityKey(key string) bool {
	return strings.Contains(key, ".flock.boid.")
}

// parseObservedAt extracts the snapshot derivation time from a stored
// EntityState: every boid triple carries observed_at as its timestamp
// (payload.go), and predicate-level merge keeps the newest arrival's value,
// so the maximum triple timestamp is the latest observed_at. Returns false
// for malformed or timestamp-less payloads.
func parseObservedAt(value []byte) (time.Time, bool) {
	var es struct {
		Triples []struct {
			Timestamp time.Time `json:"timestamp"`
		} `json:"triples"`
	}
	if err := json.Unmarshal(value, &es); err != nil || len(es.Triples) == 0 {
		return time.Time{}, false
	}
	var newest time.Time
	for _, tr := range es.Triples {
		if tr.Timestamp.After(newest) {
			newest = tr.Timestamp
		}
	}
	if newest.IsZero() {
		return time.Time{}, false
	}
	return newest, true
}
