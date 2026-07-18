package api

import (
	"encoding/json"
	"testing"
)

func entityValue(t *testing.T, x, y float64, neighbors ...string) []byte {
	t.Helper()
	triples := []map[string]any{
		{"predicate": "flock.position.x", "object": x},
		{"predicate": "flock.position.y", "object": y},
		{"predicate": "flock.neighbor.count", "object": float64(len(neighbors))},
	}
	for _, n := range neighbors {
		triples = append(triples, map[string]any{"predicate": "flock.neighbor.of", "object": n})
	}
	data, err := json.Marshal(map[string]any{"triples": triples})
	if err != nil {
		t.Fatalf("marshal entity: %v", err)
	}
	return data
}

func TestBridgeCoalescesLatestWins(t *testing.T) {
	b := newBridgeState()
	b.applyEntity("boid.1", entityValue(t, 1, 1), false)
	b.applyEntity("boid.1", entityValue(t, 2, 2, "boid.2"), false)
	b.applyEntity("boid.2", entityValue(t, 9, 9), false)

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

func TestBridgeDuplicateTriplesLastWriteWins(t *testing.T) {
	// Simulates upstream #466 append behavior: duplicated predicates with
	// newest at the tail; the count marker resets the neighbor list.
	value := []byte(`{"triples":[
		{"predicate":"flock.position.x","object":1},
		{"predicate":"flock.neighbor.count","object":1},
		{"predicate":"flock.neighbor.of","object":"boid.9"},
		{"predicate":"flock.position.x","object":5},
		{"predicate":"flock.neighbor.count","object":1},
		{"predicate":"flock.neighbor.of","object":"boid.2"}
	]}`)
	b := newBridgeState()
	b.applyEntity("boid.1", value, false)
	batch := b.flush()
	e := batch.Entities[0]
	if e.X != 5 {
		t.Fatalf("X = %v, want 5 (last write wins)", e.X)
	}
	if len(e.Neighbors) != 1 || e.Neighbors[0] != "boid.2" {
		t.Fatalf("neighbors = %v, want latest set only", e.Neighbors)
	}
}

func TestBridgeCommunityInversion(t *testing.T) {
	b := newBridgeState()
	comm := func(members ...string) []byte {
		data, _ := json.Marshal(map[string]any{"level": 0, "members": members})
		return data
	}
	b.applyCommunity("0.boid.1", comm("boid.1", "boid.2"), false)
	b.applyCommunity("0.boid.5", comm("boid.5"), false)
	b.applyCommunity("1.boid.1", comm("boid.1", "boid.2", "boid.5"), false) // higher level: ignored
	b.applyCommunity("entity.0.boid.1", []byte(`"junk"`), false)            // reverse-lookup keys: ignored

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
	b.applyCommunity("0.boid.5", nil, true)
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
	b.applyEntity("boid.3", entityValue(t, 1, 2), false)
	_ = b.flush()
	b.applyEntity("boid.3", nil, true)
	batch := b.flush()
	if len(batch.Removed) != 1 || batch.Removed[0] != "boid.3" {
		t.Fatalf("removed = %v, want [boid.3]", batch.Removed)
	}
}
