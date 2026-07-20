package api

import (
	"testing"

	"github.com/c360studio/semstreams/pkg/graphview"
)

// Triple decoding is covered by graphstream_decode_test.go. These tests cover
// what bridgeState still owns after the graphview cutover: coalescing typed
// deltas into the per-client wire batch.

func entityUpsert(key string, x, y float64, neighbors ...string) graphview.Delta[graphEntity] {
	return graphview.Delta[graphEntity]{
		Op:    graphview.DeltaUpsert,
		Key:   key,
		Value: graphEntity{ID: key, X: x, Y: y, Neighbors: neighbors},
	}
}

func communityUpsert(key string, members ...string) graphview.Delta[[]string] {
	return graphview.Delta[[]string]{Op: graphview.DeltaUpsert, Key: key, Value: members}
}

func TestBridgeCoalescesLatestWins(t *testing.T) {
	b := newBridgeState()
	b.applyEntityDeltas([]graphview.Delta[graphEntity]{
		entityUpsert("boid.1", 1, 1),
		entityUpsert("boid.1", 2, 2, "boid.2"),
		entityUpsert("boid.2", 9, 9),
	})

	batch := b.flush()
	if batch == nil {
		t.Fatal("flush returned nil with dirty state")
	}
	if len(batch.Entities) != 2 {
		t.Fatalf("entities = %d, want 2 (coalesced)", len(batch.Entities))
	}
	byID := map[string]graphEntity{}
	for _, e := range batch.Entities {
		byID[e.ID] = e
	}
	if byID["boid.1"].X != 2 || len(byID["boid.1"].Neighbors) != 1 {
		t.Fatalf("boid.1 = %+v, want latest state", byID["boid.1"])
	}

	// Nothing new: flush is nil.
	if b.flush() != nil {
		t.Fatal("second flush not nil")
	}
}

func TestBridgeSeedFromSnapshotIsTheInitialSync(t *testing.T) {
	b := newBridgeState()
	b.seedEntities(graphview.Snapshot[graphEntity]{
		Entries: map[string]graphview.Entry[graphEntity]{
			"boid.1": {Value: graphEntity{ID: "boid.1", X: 1, Y: 2}},
			"boid.2": {Value: graphEntity{ID: "boid.2", X: 3, Y: 4}},
		},
	})

	batch := b.flush()
	if batch == nil || len(batch.Entities) != 2 {
		t.Fatalf("initial sync = %+v, want both snapshot entities", batch)
	}

	// A delta after the seed coalesces on top of it rather than duplicating.
	b.applyEntityDeltas([]graphview.Delta[graphEntity]{entityUpsert("boid.1", 9, 9)})
	batch = b.flush()
	if len(batch.Entities) != 1 || batch.Entities[0].X != 9 {
		t.Fatalf("post-seed delta = %+v, want only the changed entity", batch.Entities)
	}
}

func TestBridgeCommunityInversion(t *testing.T) {
	b := newBridgeState()
	b.applyCommunityDeltas([]graphview.Delta[[]string]{
		communityUpsert("0.boid.1", "boid.1", "boid.2"),
		communityUpsert("0.boid.5", "boid.5"),
	})

	batch := b.flush()
	if batch == nil || batch.Communities == nil {
		t.Fatal("no community map flushed")
	}
	want := map[string]string{"boid.1": "0.boid.1", "boid.2": "0.boid.1", "boid.5": "0.boid.5"}
	for m, c := range want {
		if batch.Communities[m] != c {
			t.Fatalf("communities[%s] = %s, want %s (full: %v)", m, batch.Communities[m], c, batch.Communities)
		}
	}

	// Deletion (clustering Clear before re-detection) marks dirty and drops.
	b.applyCommunityDeltas([]graphview.Delta[[]string]{
		{Op: graphview.DeltaDelete, Key: "0.boid.5"},
	})
	batch = b.flush()
	if batch == nil {
		t.Fatal("delete did not mark dirty")
	}
	if _, ok := batch.Communities["boid.5"]; ok {
		t.Fatal("deleted community still present")
	}
}

func TestBridgeEntityRemoval(t *testing.T) {
	b := newBridgeState()
	b.applyEntityDeltas([]graphview.Delta[graphEntity]{entityUpsert("boid.3", 1, 2)})
	_ = b.flush()
	b.applyEntityDeltas([]graphview.Delta[graphEntity]{
		{Op: graphview.DeltaDelete, Key: "boid.3"},
	})
	batch := b.flush()
	if len(batch.Removed) != 1 || batch.Removed[0] != "boid.3" {
		t.Fatalf("removed = %v, want [boid.3]", batch.Removed)
	}
}

func TestBridgePoisonKeepsLastKnownGood(t *testing.T) {
	// graphview heals a poisoned key on the next valid write, so dropping the
	// boid out of the pane because one write failed to decode would be worse
	// than briefly showing its previous position.
	b := newBridgeState()
	b.applyEntityDeltas([]graphview.Delta[graphEntity]{entityUpsert("boid.4", 7, 8)})
	_ = b.flush()

	b.applyEntityDeltas([]graphview.Delta[graphEntity]{
		{Op: graphview.DeltaPoison, Key: "boid.4"},
	})
	if batch := b.flush(); batch != nil {
		t.Fatalf("poison produced a batch %+v, want no wire change", batch)
	}

	b.applyEntityDeltas([]graphview.Delta[graphEntity]{entityUpsert("boid.4", 1, 1)})
	batch := b.flush()
	if len(batch.Entities) != 1 || batch.Entities[0].X != 1 {
		t.Fatalf("healed entity = %+v, want the newer valid write", batch.Entities)
	}
}
