package sim

import (
	"testing"

	"github.com/c360studio/semboids/internal/flock"
	"github.com/c360studio/semboids/internal/zone"
)

func TestTrackerEdgeDetection(t *testing.T) {
	zones := []zone.Zone{
		{ID: "pred-1", Type: zone.TypePredator, X: 100, Y: 100, R: 50, Strength: 1},
	}
	tr := newZoneTracker(zones)

	outside := []flock.Boid{{ID: 0, Pos: flock.Vec2{X: 300, Y: 300}}}
	inside := []flock.Boid{{ID: 0, Pos: flock.Vec2{X: 100, Y: 100}}}

	// Outside: nothing.
	if evs := tr.transitions(outside); len(evs) != 0 {
		t.Fatalf("outside produced events: %v", evs)
	}
	// Cross in: exactly one entered.
	evs := tr.transitions(inside)
	if len(evs) != 1 || !evs[0].entered || evs[0].zone.ID != "pred-1" || evs[0].boidID != 0 {
		t.Fatalf("enter events = %+v, want one entered for pred-1", evs)
	}
	// Stay inside: steady state, nothing.
	for range 10 {
		if evs := tr.transitions(inside); len(evs) != 0 {
			t.Fatalf("steady-state inside produced events: %v", evs)
		}
	}
	// Cross out: exactly one exited.
	evs = tr.transitions(outside)
	if len(evs) != 1 || evs[0].entered || evs[0].zone.ID != "pred-1" {
		t.Fatalf("exit events = %+v, want one exited for pred-1", evs)
	}
	// Stay outside: nothing.
	if evs := tr.transitions(outside); len(evs) != 0 {
		t.Fatalf("steady-state outside produced events: %v", evs)
	}
}

func TestTrackerMultipleZonesAndBoids(t *testing.T) {
	zones := []zone.Zone{
		{ID: "a", Type: zone.TypeFood, X: 0, Y: 0, R: 10, Strength: 1},
		{ID: "b", Type: zone.TypeFood, X: 100, Y: 0, R: 10, Strength: 1},
	}
	tr := newZoneTracker(zones)
	boids := []flock.Boid{
		{ID: 0, Pos: flock.Vec2{X: 0, Y: 0}},   // in a
		{ID: 1, Pos: flock.Vec2{X: 100, Y: 0}}, // in b
	}
	evs := tr.transitions(boids)
	if len(evs) != 2 {
		t.Fatalf("events = %+v, want 2 enters", evs)
	}
	// Swap zones: both exit and both enter — 4 events.
	swapped := []flock.Boid{
		{ID: 0, Pos: flock.Vec2{X: 100, Y: 0}},
		{ID: 1, Pos: flock.Vec2{X: 0, Y: 0}},
	}
	evs = tr.transitions(swapped)
	if len(evs) != 4 {
		t.Fatalf("events after swap = %+v, want 4", evs)
	}
}
