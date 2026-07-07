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
	tr := newZoneTracker(zones, 0)

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
	tr := newZoneTracker(zones, 0)
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

func TestTrackerLingeredFiresPastGracePredatorOnly(t *testing.T) {
	zones := []zone.Zone{
		{ID: "pred", Type: zone.TypePredator, X: 100, Y: 100, R: 50, Strength: 1},
		{ID: "food", Type: zone.TypeFood, X: 500, Y: 500, R: 50, Strength: 1},
	}
	tr := newZoneTracker(zones, 3) // grace = 3 ticks
	both := []flock.Boid{
		{ID: 0, Pos: flock.Vec2{X: 100, Y: 100}}, // in predator
		{ID: 1, Pos: flock.Vec2{X: 500, Y: 500}}, // in food (must never linger)
	}

	// Ticks 1,2: under grace → no lingered.
	for range 2 {
		tr.transitions(both)
		if len(tr.lingered()) != 0 {
			t.Fatalf("lingered before grace: %v", tr.lingered())
		}
	}
	// Tick 3: predator dwell hits grace → exactly one lingered, boid 0 only.
	tr.transitions(both)
	ling := tr.lingered()
	if len(ling) != 1 || ling[0].boidID != 0 || ling[0].zone.Type != zone.TypePredator {
		t.Fatalf("lingered = %+v, want one for boid 0 in predator (food never lingers)", ling)
	}
	// Tick 4: already fired this crossing → no repeat.
	tr.transitions(both)
	if len(tr.lingered()) != 0 {
		t.Fatalf("lingered repeated: %v", tr.lingered())
	}
}

func TestTrackerLingeredNotForFleeingBoid(t *testing.T) {
	zones := []zone.Zone{{ID: "pred", Type: zone.TypePredator, X: 100, Y: 100, R: 50, Strength: 1}}
	tr := newZoneTracker(zones, 3)
	in := []flock.Boid{{ID: 0, Pos: flock.Vec2{X: 100, Y: 100}}}
	out := []flock.Boid{{ID: 0, Pos: flock.Vec2{X: 400, Y: 400}}}

	tr.transitions(in)  // dwell 1
	tr.transitions(in)  // dwell 2 (under grace)
	tr.transitions(out) // fled before grace → dwell resets
	if len(tr.lingered()) != 0 {
		t.Fatal("fleeing boid should not linger")
	}
	// Re-enter: needs a full fresh grace window.
	tr.transitions(in)
	tr.transitions(in)
	if len(tr.lingered()) != 0 {
		t.Fatal("re-entered boid lingered too early")
	}
	tr.transitions(in) // dwell 3 → fires
	if len(tr.lingered()) != 1 {
		t.Fatal("re-entered boid should linger after a full grace window")
	}
}

func TestTrackerForgetPrunes(t *testing.T) {
	zones := []zone.Zone{{ID: "pred", Type: zone.TypePredator, X: 0, Y: 0, R: 10, Strength: 1}}
	tr := newZoneTracker(zones, 2)
	tr.transitions([]flock.Boid{{ID: 5, Pos: flock.Vec2{X: 0, Y: 0}}})
	if _, ok := tr.inside[5]; !ok {
		t.Fatal("boid 5 not tracked")
	}
	tr.forget([]uint32{5})
	if _, ok := tr.inside[5]; ok {
		t.Fatal("forget did not prune boid 5's tracker state")
	}
}
