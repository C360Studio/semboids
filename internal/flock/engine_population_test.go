package flock

import "testing"

func TestAddBoidsAllocatesFreshMonotonicIDs(t *testing.T) {
	e := NewEngine(5, 1, DefaultParams()) // seeds IDs 0..4
	ids := e.AddBoids(3)
	if len(ids) != 3 {
		t.Fatalf("AddBoids returned %d ids, want 3", len(ids))
	}
	for i, want := range []uint32{5, 6, 7} {
		if ids[i] != want {
			t.Fatalf("id[%d] = %d, want %d (must not collide with seeded 0..4)", i, ids[i], want)
		}
	}
	if len(e.Boids()) != 8 {
		t.Fatalf("population = %d, want 8", len(e.Boids()))
	}
}

func TestNextIDNeverReused(t *testing.T) {
	e := NewEngine(3, 1, DefaultParams())
	first := e.AddBoids(2) // 3, 4
	e.RemoveBoids(first)   // drop them
	next := e.AddBoids(1)  // must be 5, not a reused 3/4
	if next[0] != 5 {
		t.Fatalf("reused id: got %d, want fresh 5", next[0])
	}
}

func TestRemoveBoidsPreservesSurvivors(t *testing.T) {
	e := NewEngine(5, 1, DefaultParams())
	before := append([]Boid(nil), e.Boids()...) // deep copy (Boid is a value)

	e.RemoveBoids([]uint32{1, 3})
	got := e.Boids()
	if len(got) != 3 {
		t.Fatalf("population = %d, want 3 after removing 2", len(got))
	}
	want := []uint32{0, 2, 4}
	for i, b := range got {
		if b.ID != want[i] {
			t.Fatalf("survivor[%d] id = %d, want %d (order preserved)", i, b.ID, want[i])
		}
		if b.Pos != before[b.ID].Pos || b.Vel != before[b.ID].Vel {
			t.Fatalf("survivor %d state changed by removal", b.ID)
		}
	}
}

func TestPopulationChangeAppliesAcrossTick(t *testing.T) {
	e := NewEngine(4, 1, DefaultParams())
	e.AddBoids(2) // 6 staged on the front buffer
	e.Tick()      // must not panic — Tick reconciles the back buffer
	if len(e.Boids()) != 6 || e.TickCount() != 1 {
		t.Fatalf("after add+tick: pop=%d ticks=%d, want 6/1", len(e.Boids()), e.TickCount())
	}
	e.RemoveBoids([]uint32{0})
	e.Tick()
	if len(e.Boids()) != 5 {
		t.Fatalf("after remove+tick: pop=%d, want 5", len(e.Boids()))
	}
}

func TestSpawnPlacementDeterministic(t *testing.T) {
	a := NewEngine(5, 42, DefaultParams())
	b := NewEngine(5, 42, DefaultParams())
	a.AddBoids(3)
	b.AddBoids(3)
	for i, ba := range a.Boids() {
		if ba != b.Boids()[i] {
			t.Fatalf("boid[%d] differs across identical seed+spawn sequences", i)
		}
	}
}
