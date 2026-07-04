package boidgraph

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/payloadregistry"
)

func testEntity() *Entity {
	return &Entity{
		Boid: BoidState{
			ID: 7, X: 812.5, Y: 301.25, VX: 21.75, VY: -37,
			Neighbors: []uint32{12, 31},
		},
		OrgID:      "c360",
		Platform:   "semboids",
		Tick:       1234,
		ObservedAt: time.UnixMilli(1719936000123),
	}
}

func TestBoidEntityID6Part(t *testing.T) {
	got := testEntity().EntityID()
	want := "c360.semboids.sim.flock.boid.7"
	if got != want {
		t.Fatalf("EntityID = %q, want %q", got, want)
	}
}

func TestBoidTriples(t *testing.T) {
	e := testEntity()
	props := map[string]any{}
	var neighborObjects []string
	for _, tr := range e.Triples() {
		if tr.Subject != e.EntityID() {
			t.Fatalf("triple %q subject = %q, want %q", tr.Predicate, tr.Subject, e.EntityID())
		}
		if tr.Predicate == "flock.neighbor" {
			neighborObjects = append(neighborObjects, tr.Object.(string))
			continue
		}
		props[tr.Predicate] = tr.Object
	}

	wantProps := map[string]any{
		"flock.position.x":     812.5,
		"flock.position.y":     301.25,
		"flock.velocity.x":     21.75,
		"flock.velocity.y":     -37.0,
		"flock.neighbor.count": 2.0,
	}
	for pred, want := range wantProps {
		if props[pred] != want {
			t.Fatalf("predicate %q = %v, want %v", pred, props[pred], want)
		}
	}
	if len(neighborObjects) != 2 ||
		neighborObjects[0] != "c360.semboids.sim.flock.boid.12" ||
		neighborObjects[1] != "c360.semboids.sim.flock.boid.31" {
		t.Fatalf("neighbor relationships = %v", neighborObjects)
	}
}

func TestNeighborCountAlwaysPresent(t *testing.T) {
	// The count property must exist even with zero neighbors so
	// predicate-level merge fires on every snapshot (spike 1.1).
	e := testEntity()
	e.Boid.Neighbors = nil
	found := false
	for _, tr := range e.Triples() {
		if tr.Predicate == "flock.neighbor.count" {
			found = true
			if tr.Object != 0.0 {
				t.Fatalf("neighbor.count = %v, want 0", tr.Object)
			}
		}
		if tr.Predicate == "flock.neighbor" {
			t.Fatal("unexpected flock.neighbor triple with no neighbors")
		}
	}
	if !found {
		t.Fatal("flock.neighbor.count missing")
	}
}

func TestBoidPayloadSchemaAndRegistry(t *testing.T) {
	e := testEntity()
	if got := e.Schema().String(); got != "boids.boid.v1" {
		t.Fatalf("Schema = %q, want boids.boid.v1", got)
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("valid entity rejected: %v", err)
	}
	reg := payloadregistry.New()
	if err := RegisterPayloads(reg); err != nil {
		t.Fatalf("RegisterPayloads: %v", err)
	}
	if err := RegisterPayloads(reg); err == nil {
		t.Fatal("double registration accepted")
	}
}
