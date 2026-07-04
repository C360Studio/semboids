package flock

import (
	"reflect"
	"slices"
	"testing"
)

func TestSnapshotNeighbors(t *testing.T) {
	p := DefaultParams()
	boids := []Boid{
		{ID: 0, Pos: Vec2{100, 100}},
		{ID: 1, Pos: Vec2{120, 100}}, // within 50 of 0 and 2
		{ID: 2, Pos: Vec2{150, 100}}, // within 50 of 1, edge-of-radius to 0
		{ID: 3, Pos: Vec2{800, 800}}, // isolated
	}
	e := engineWith(p, boids)

	got := e.SnapshotNeighbors(50)
	for _, ns := range got {
		slices.Sort(ns)
	}

	want := map[uint32][]uint32{
		0: {1, 2}, // dist 20 and 50 (boundary inclusive)
		1: {0, 2},
		2: {0, 1},
		3: {},
	}
	for id, wantNs := range want {
		if !reflect.DeepEqual(got[id], wantNs) && !(len(got[id]) == 0 && len(wantNs) == 0) {
			t.Fatalf("neighbors[%d] = %v, want %v", id, got[id], wantNs)
		}
	}
}

func TestSnapshotNeighborsDoesNotPerturbSim(t *testing.T) {
	p := DefaultParams()
	a := NewEngine(50, 9, p)
	b := NewEngine(50, 9, p)
	for i := range 60 {
		a.Tick()
		b.Tick()
		if i%10 == 0 {
			_ = a.SnapshotNeighbors(50) // interleaved snapshots must not change physics
		}
	}
	if !reflect.DeepEqual(a.Boids(), b.Boids()) {
		t.Fatal("SnapshotNeighbors perturbed the simulation")
	}
}
