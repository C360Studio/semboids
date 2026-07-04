package flock

import (
	"slices"
	"testing"
)

// queryNeighbors is a test helper: rebuild the grid for boids and return the
// sorted neighbor indices of boids[i] within radius.
func queryNeighbors(t *testing.T, boids []Boid, i int, w, h, cell, radius float64) []int32 {
	t.Helper()
	g := newGrid(w, h, cell)
	g.rebuild(boids)
	got := g.neighbors(boids, i, radius, nil)
	slices.Sort(got)
	return got
}

func TestGridNeighborsBasic(t *testing.T) {
	boids := []Boid{
		{ID: 0, Pos: Vec2{100, 100}},
		{ID: 1, Pos: Vec2{110, 100}}, // dist 10 from 0
		{ID: 2, Pos: Vec2{100, 140}}, // dist 40 from 0
		{ID: 3, Pos: Vec2{300, 300}}, // far away
	}
	got := queryNeighbors(t, boids, 0, 1600, 900, 50, 50)
	want := []int32{1, 2}
	if !slices.Equal(got, want) {
		t.Fatalf("neighbors = %v, want %v", got, want)
	}
}

func TestGridNeighborsExcludesSelf(t *testing.T) {
	boids := []Boid{
		{ID: 0, Pos: Vec2{100, 100}},
		{ID: 1, Pos: Vec2{100, 100}}, // co-located
	}
	got := queryNeighbors(t, boids, 0, 1600, 900, 50, 50)
	if slices.Contains(got, int32(0)) {
		t.Fatalf("neighbors of 0 contains self: %v", got)
	}
	if !slices.Contains(got, int32(1)) {
		t.Fatalf("co-located boid missing from neighbors: %v", got)
	}
}

func TestGridNeighborsRadiusFilter(t *testing.T) {
	boids := []Boid{
		{ID: 0, Pos: Vec2{100, 100}},
		{ID: 1, Pos: Vec2{149, 100}}, // dist 49 — inside radius 50
		{ID: 2, Pos: Vec2{151, 100}}, // dist 51 — outside radius 50, same 3x3 scan
	}
	got := queryNeighbors(t, boids, 0, 1600, 900, 50, 50)
	want := []int32{1}
	if !slices.Equal(got, want) {
		t.Fatalf("neighbors = %v, want %v", got, want)
	}
}

func TestGridNeighborsWrapAround(t *testing.T) {
	const w, h = 1600.0, 900.0
	boids := []Boid{
		{ID: 0, Pos: Vec2{5, 450}},       // near left edge
		{ID: 1, Pos: Vec2{w - 5, 450}},   // near right edge — torus dist 10
		{ID: 2, Pos: Vec2{800, 5}},       // near top edge
		{ID: 3, Pos: Vec2{800, h - 5}},   // near bottom edge — torus dist 10
		{ID: 4, Pos: Vec2{5, 5}},         // corner
		{ID: 5, Pos: Vec2{w - 5, h - 5}}, // opposite corner — torus dist ~14.1
	}
	tests := []struct {
		name string
		i    int
		want []int32
	}{
		{"left sees right", 0, []int32{1}},
		{"right sees left", 1, []int32{0}},
		{"top sees bottom", 2, []int32{3}},
		{"corner sees opposite corner", 4, []int32{5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := queryNeighbors(t, boids, tt.i, w, h, 50, 50)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("neighbors of %d = %v, want %v", tt.i, got, tt.want)
			}
		})
	}
}

func TestGridReuseAcrossRebuilds(t *testing.T) {
	g := newGrid(1600, 900, 50)
	a := []Boid{{ID: 0, Pos: Vec2{100, 100}}, {ID: 1, Pos: Vec2{110, 100}}}
	g.rebuild(a)
	if got := g.neighbors(a, 0, 50, nil); len(got) != 1 {
		t.Fatalf("first rebuild: neighbors = %v, want one", got)
	}
	// Move everything; stale bucket contents must not leak into results.
	b := []Boid{{ID: 0, Pos: Vec2{100, 100}}, {ID: 1, Pos: Vec2{800, 800}}}
	g.rebuild(b)
	if got := g.neighbors(b, 0, 50, nil); len(got) != 0 {
		t.Fatalf("second rebuild: neighbors = %v, want none", got)
	}
}
