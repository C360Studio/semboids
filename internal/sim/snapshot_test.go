package sim

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semboids/internal/boidgraph"
)

type fakeSink struct {
	mu    sync.Mutex
	snaps []boidgraph.Snapshot
}

func (f *fakeSink) Offer(s boidgraph.Snapshot) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snaps = append(f.snaps, s)
	return true
}

func (f *fakeSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.snaps)
}

func (f *fakeSink) last() boidgraph.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snaps[len(f.snaps)-1]
}

func TestSnapshotCadenceFollowsDial(t *testing.T) {
	comp, frames := newTestComponent(t, 10, 200) // 200Hz physics
	sink := &fakeSink{}
	comp.snapshots = sink
	if err := comp.SetGraphHz(50); err != nil { // every 4th tick
		t.Fatalf("SetGraphHz: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := comp.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = comp.Stop(time.Second) }()

	// Collect ~40 frames (ticks); expect snapshots ≈ ticks/4.
	for range 40 {
		select {
		case <-frames:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for frames")
		}
	}
	snaps := sink.count()
	if snaps < 5 || snaps > 15 {
		t.Fatalf("snapshots after ~40 ticks at every-4th = %d, want ~10", snaps)
	}

	s := sink.last()
	if len(s.Boids) != 10 {
		t.Fatalf("snapshot population = %d, want 10", len(s.Boids))
	}
	if s.Boids[0].Neighbors == nil {
		t.Fatal("snapshot missing neighbor data (nil map lookup?)")
	}

	// Dial to 0: snapshots stop.
	if err := comp.SetGraphHz(0); err != nil {
		t.Fatalf("SetGraphHz(0): %v", err)
	}
	base := sink.count()
	for range 20 {
		select {
		case <-frames:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for frames")
		}
	}
	if sink.count() != base {
		t.Fatalf("snapshots continued after dial 0: %d -> %d", base, sink.count())
	}
}

func TestSetGraphHzClampsAndRejects(t *testing.T) {
	comp, _ := newTestComponent(t, 5, 30)
	if err := comp.SetGraphHz(-1); err == nil {
		t.Fatal("negative hz accepted")
	}
	if err := comp.SetGraphHz(500); err != nil {
		t.Fatalf("SetGraphHz(500): %v", err)
	}
	if got := comp.GraphHz(); got != 30 {
		t.Fatalf("GraphHz = %v, want clamped to tick rate 30", got)
	}
}
