package boidgraph

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semboids/internal/flock"
)

type fakeStream struct {
	mu       sync.Mutex
	subjects []string
	payloads [][]byte
	block    chan struct{} // when non-nil, PublishToStream blocks until closed
}

func (f *fakeStream) PublishToStream(_ context.Context, subject string, data []byte) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subjects = append(f.subjects, subject)
	f.payloads = append(f.payloads, data)
	return nil
}

func (f *fakeStream) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.payloads)
}

type fakeRemover struct {
	mu       sync.Mutex
	requests []map[string]any
}

func (f *fakeRemover) Request(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
	var req map[string]any
	_ = json.Unmarshal(data, &req)
	req["_subject"] = subject
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	return []byte(`{"success": true}`), nil
}

func snapshotWith(tick uint64, neighbors map[uint32][]uint32) Snapshot {
	boids := []flock.Boid{
		{ID: 0, Pos: flock.Vec2{X: 1, Y: 2}, Vel: flock.Vec2{X: 3, Y: 4}},
		{ID: 1, Pos: flock.Vec2{X: 5, Y: 6}, Vel: flock.Vec2{X: 7, Y: 8}},
	}
	return BuildSnapshot(tick, time.UnixMilli(1000), boids, neighbors)
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
	p := NewPublisher(stream, nil, "c360", "semboids", nil)
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
	p := NewPublisher(stream, nil, "c360", "semboids", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	// First snapshot enters the (blocked) publish; buffer cap is 2.
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

func TestPublisherRemovesEmptiedNeighborSets(t *testing.T) {
	stream := &fakeStream{}
	remover := &fakeRemover{}
	p := NewPublisher(stream, remover, "c360", "semboids", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	// Boid 0 has neighbors, then loses them; boid 1 stays empty throughout.
	p.Offer(snapshotWith(1, map[uint32][]uint32{0: {1}}))
	waitFor(t, func() bool { s, _, _ := p.Counts(); return s == 1 })
	p.Offer(snapshotWith(2, nil))
	waitFor(t, func() bool { s, _, _ := p.Counts(); return s == 2 })

	remover.mu.Lock()
	defer remover.mu.Unlock()
	if len(remover.requests) != 1 {
		t.Fatalf("remove requests = %d, want exactly 1 (boid 0's transition)", len(remover.requests))
	}
	req := remover.requests[0]
	if req["_subject"] != "graph.mutation.triple.remove" ||
		req["subject"] != "c360.semboids.sim.flock.boid.0" ||
		req["predicate"] != "flock.neighbor" {
		t.Fatalf("remove request = %v", req)
	}
}
