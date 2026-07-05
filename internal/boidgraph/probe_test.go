package boidgraph

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// recordObserver captures Observe calls (satisfies prometheus.Observer).
type recordObserver struct {
	mu   sync.Mutex
	vals []float64
}

func (r *recordObserver) Observe(v float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vals = append(r.vals, v)
}

func (r *recordObserver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.vals)
}

const boidKey = "c360.semboids.sim.flock.boid.7"

// boidStateValue renders a stored EntityState carrying observed_at as its
// triple timestamps (the shape graph-ingest persists).
func boidStateValue(t *testing.T, observedAt time.Time) []byte {
	t.Helper()
	es := map[string]any{
		"triples": []map[string]any{
			{"predicate": "flock.position.x", "timestamp": observedAt},
			{"predicate": "flock.neighbor.count", "timestamp": observedAt},
		},
	}
	data, err := json.Marshal(es)
	if err != nil {
		t.Fatalf("marshal entity state: %v", err)
	}
	return data
}

func probeWith(rec *recordObserver, sampleN int, now time.Time) *LatencyProbe {
	return &LatencyProbe{
		hist:    rec,
		sampleN: uint64(sampleN),
		now:     func() time.Time { return now },
	}
}

// TestProbeRecordsLatency confirms latency = observation time − observed_at.
func TestProbeRecordsLatency(t *testing.T) {
	rec := &recordObserver{}
	now := time.UnixMilli(10_000)
	observedAt := now.Add(-500 * time.Millisecond)
	p := probeWith(rec, 1, now)

	p.observe(boidKey, boidStateValue(t, observedAt), false)

	if rec.count() != 1 {
		t.Fatalf("recorded %d, want 1", rec.count())
	}
	if got := rec.vals[0]; got < 0.49 || got > 0.51 {
		t.Fatalf("latency = %v, want ~0.5s", got)
	}
}

// TestProbeSampling confirms 1-in-N sampling: with N=10, 20 boid updates
// yield exactly 2 records.
func TestProbeSampling(t *testing.T) {
	rec := &recordObserver{}
	now := time.UnixMilli(10_000)
	p := probeWith(rec, 10, now)

	val := boidStateValue(t, now.Add(-time.Second))
	for range 20 {
		p.observe(boidKey, val, false)
	}
	if rec.count() != 2 {
		t.Fatalf("recorded %d, want 2 (1-in-10 of 20)", rec.count())
	}
}

// TestProbeSkipsNonBoidAndMalformed confirms non-boid keys, deletes, and
// malformed payloads are skipped without error and don't consume samples.
func TestProbeSkipsNonBoidAndMalformed(t *testing.T) {
	rec := &recordObserver{}
	now := time.UnixMilli(10_000)
	p := probeWith(rec, 1, now)

	// Non-boid key (a zone entity) — skipped, no sample consumed.
	p.observe("c360.semboids.sim.zones.zone.predator", boidStateValue(t, now), false)
	// Delete event on a boid key — skipped.
	p.observe(boidKey, nil, true)
	// Malformed value — sampled but not recorded.
	p.observe(boidKey, []byte("{not json"), false)
	// Boid state with no triples — no timestamp, not recorded.
	p.observe(boidKey, []byte(`{"triples":[]}`), false)

	if rec.count() != 0 {
		t.Fatalf("recorded %d, want 0 (all inputs skippable)", rec.count())
	}

	// A well-formed boid update afterward still records — the probe kept
	// working through the bad inputs.
	p.observe(boidKey, boidStateValue(t, now.Add(-100*time.Millisecond)), false)
	if rec.count() != 1 {
		t.Fatalf("recorded %d after a valid update, want 1", rec.count())
	}
}

// TestParseObservedAtNewest confirms the newest triple timestamp wins (merge
// keeps the latest arrival's observed_at across predicates).
func TestParseObservedAtNewest(t *testing.T) {
	older := time.UnixMilli(1000)
	newer := time.UnixMilli(5000)
	es := map[string]any{
		"triples": []map[string]any{
			{"predicate": "flock.position.x", "timestamp": newer},
			{"predicate": "flock.velocity.x", "timestamp": older},
		},
	}
	data, _ := json.Marshal(es)
	got, ok := parseObservedAt(data)
	if !ok {
		t.Fatal("parseObservedAt returned !ok for valid state")
	}
	if !got.Equal(newer) {
		t.Fatalf("observed_at = %v, want newest %v", got, newer)
	}
}
