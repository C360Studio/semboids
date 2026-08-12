package boidgraph

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/projection"

	"github.com/c360studio/semboids/internal/flock"
)

// fakeStream records batched publishes in order. When block is non-nil,
// PublishBatchToStream blocks until it is closed — modelling a stalled
// publisher so Offer's drop path can be exercised.
type fakeStream struct {
	mu       sync.Mutex
	subjects []string
	payloads [][]byte
	batches  int
	block    chan struct{}
}

func (f *fakeStream) PublishBatchToStream(_ context.Context, subject string, msgs [][]byte) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches++
	for _, m := range msgs {
		f.subjects = append(f.subjects, subject)
		f.payloads = append(f.payloads, m)
	}
	return nil
}

func (f *fakeStream) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.payloads)
}

// recordedOrder decodes each published payload's (boid ID, tick) in publish
// order, so tests can assert per-boid ordering across snapshots.
func (f *fakeStream) recordedOrder(t *testing.T) []boidTick {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]boidTick, 0, len(f.payloads))
	for _, data := range f.payloads {
		var wire struct {
			Payload struct {
				Boid struct {
					ID uint32 `json:"id"`
				} `json:"boid"`
				Tick uint64 `json:"tick"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		out = append(out, boidTick{id: wire.Payload.Boid.ID, tick: wire.Payload.Tick})
	}
	return out
}

// boidTick identifies one publish by (boid ID, snapshot tick).
type boidTick struct {
	id   uint32
	tick uint64
}

// fakeReconciler captures neighbor-group reconciles and returns scripted
// errors per call (nil beyond the script), standing in for the projection
// mutation client.
type fakeReconciler struct {
	mu       sync.Mutex
	requests []projection.ReconcileMutation
	script   []error
}

func (f *fakeReconciler) Reconcile(_ context.Context, req projection.ReconcileMutation) (projection.MutationReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := len(f.requests)
	f.requests = append(f.requests, req)
	if call < len(f.script) && f.script[call] != nil {
		return projection.MutationReceipt{}, f.script[call]
	}
	return projection.MutationReceipt{}, nil
}

func (f *fakeReconciler) calls() []projection.ReconcileMutation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]projection.ReconcileMutation(nil), f.requests...)
}

// mutationErr builds the classified error shape the projection client returns.
func mutationErr(kind projection.MutationErrorKind) error {
	return &projection.MutationError{Kind: kind, Err: errors.New(string(kind))}
}

func snapshotWith(tick uint64, neighbors map[uint32][]uint32) Snapshot {
	boids := []flock.Boid{
		{ID: 0, Pos: flock.Vec2{X: 1, Y: 2}, Vel: flock.Vec2{X: 3, Y: 4}},
		{ID: 1, Pos: flock.Vec2{X: 5, Y: 6}, Vel: flock.Vec2{X: 7, Y: 8}},
	}
	return BuildSnapshot(tick, time.UnixMilli(1000), boids, neighbors)
}

// snapshotN builds a snapshot of n boids (IDs 0..n-1), no neighbors.
func snapshotN(tick uint64, n int) Snapshot {
	boids := make([]flock.Boid, n)
	for i := range boids {
		boids[i] = flock.Boid{ID: uint32(i), Pos: flock.Vec2{X: float64(i)}}
	}
	return BuildSnapshot(tick, time.UnixMilli(1000), boids, nil)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatal("condition never met")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestPublisherPublishesEachBoid(t *testing.T) {
	stream := &fakeStream{}
	p := NewPublisher(stream, nil, "c360", "semboids", nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	if !p.Offer(snapshotWith(1, map[uint32][]uint32{0: {1}, 1: {0}})) {
		t.Fatal("Offer rejected with empty buffer")
	}
	waitFor(t, func() bool { return stream.count() == 2 })

	stream.mu.Lock()
	defer stream.mu.Unlock()
	for _, s := range stream.subjects {
		if s != IngestSubject {
			t.Fatalf("published to %q, want %q", s, IngestSubject)
		}
	}
	var envelope struct {
		Type map[string]any `json:"type"`
	}
	if err := json.Unmarshal(stream.payloads[0], &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Type["category"] != "boid" {
		t.Fatalf("envelope type = %v, want boid category", envelope.Type)
	}
	snaps, entities, dropped := p.Counts()
	if snaps != 1 || entities != 2 || dropped != 0 {
		t.Fatalf("counts = %d/%d/%d, want 1/2/0", snaps, entities, dropped)
	}
}

func TestPublisherDropsWhenStalled(t *testing.T) {
	stream := &fakeStream{block: make(chan struct{})}
	p := NewPublisher(stream, nil, "c360", "semboids", nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	// First snapshot enters the (blocked) batch; buffer cap is 2.
	accepted := 0
	for i := range 10 {
		if p.Offer(snapshotWith(uint64(i), nil)) {
			accepted++
		}
	}
	if accepted >= 10 {
		t.Fatal("stalled publisher accepted every snapshot — Offer must drop")
	}
	_, _, dropped := p.Counts()
	if dropped == 0 {
		t.Fatal("drops not counted")
	}
	close(stream.block) // unblock for clean shutdown
}

func TestPublisherClearsEmptiedNeighborSets(t *testing.T) {
	stream := &fakeStream{}
	reconciler := &fakeReconciler{}
	p := NewPublisher(stream, reconciler, "c360", "semboids", nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	// Boid 0 has neighbors, then loses them; boid 1 stays empty throughout.
	p.Offer(snapshotWith(1, map[uint32][]uint32{0: {1}}))
	waitFor(t, func() bool { s, _, _ := p.Counts(); return s == 1 })
	p.Offer(snapshotWith(2, nil))
	waitFor(t, func() bool { s, _, _ := p.Counts(); return s == 2 })

	calls := reconciler.calls()
	if len(calls) != 1 {
		t.Fatalf("reconcile calls = %d, want exactly 1 (boid 0's transition)", len(calls))
	}
	req := calls[0]
	if req.Contract != NeighborContractName || req.Group != NeighborGroup ||
		req.EntityID != "c360.semboids.sim.flock.boid.0" || len(req.Desired) != 0 {
		t.Fatalf("reconcile request = %+v", req)
	}
}

// TestClearNeighborsRetryClassification pins design D2's retry policy on the
// clear path: definite non-commits (revision conflict, no responder) retry up
// to the bound, a missing entity is already-clear, and an ambiguous outcome
// is never blind-retried.
func TestClearNeighborsRetryClassification(t *testing.T) {
	cases := []struct {
		name      string
		script    []error
		wantCalls int
	}{
		{"conflict then success", []error{mutationErr(projection.MutationRevisionConflict), nil}, 2},
		{"unavailable then success", []error{mutationErr(projection.MutationUnavailable), nil}, 2},
		{"conflict exhausts bound", []error{
			mutationErr(projection.MutationRevisionConflict),
			mutationErr(projection.MutationRevisionConflict),
			mutationErr(projection.MutationRevisionConflict),
			mutationErr(projection.MutationRevisionConflict), // must never be reached
		}, neighborClearRetries},
		{"not found is already clear", []error{mutationErr(projection.MutationNotFound)}, 1},
		{"ambiguous is not retried", []error{mutationErr(projection.MutationCommitUnknown)}, 1},
		{"invalid is not retried", []error{mutationErr(projection.MutationInvalid)}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reconciler := &fakeReconciler{script: tc.script}
			p := NewPublisher(&fakeStream{}, reconciler, "c360", "semboids", nil, nil)
			p.clearNeighbors(context.Background(), 7)
			if got := len(reconciler.calls()); got != tc.wantCalls {
				t.Fatalf("reconcile calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

// TestPublisherNoCrossSnapshotReorder pins the D1/D2 invariant: consecutive
// snapshots are consumed one at a time and each is one async batch joined on
// all acks, so every boid's snapshot-N publish lands before its snapshot-N+1
// publish. Both snapshots are offered up front (buffer cap 2) so the
// publisher — not the test — decides their ordering. A publisher that
// consumed snapshots concurrently would interleave the ticks and fail.
func TestPublisherNoCrossSnapshotReorder(t *testing.T) {
	const boids = 4
	stream := &fakeStream{}
	p := NewPublisher(stream, nil, "c360", "semboids", nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	p.Offer(snapshotN(1, boids))
	p.Offer(snapshotN(2, boids))
	waitFor(t, func() bool { s, _, _ := p.Counts(); return s == 2 })

	last := map[uint32]uint64{}
	for _, bt := range stream.recordedOrder(t) {
		if prev, ok := last[bt.id]; ok && bt.tick < prev {
			t.Fatalf("boid %d published tick %d after tick %d — cross-snapshot reorder", bt.id, bt.tick, prev)
		}
		last[bt.id] = bt.tick
	}
	// All boids from both snapshots must have landed.
	if got := stream.count(); got != 2*boids {
		t.Fatalf("published %d entities, want %d", got, 2*boids)
	}
}
