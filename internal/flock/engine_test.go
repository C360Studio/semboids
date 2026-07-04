package flock

import (
	"math"
	"reflect"
	"testing"
)

// isolate returns params with only the given rule weights enabled, generous
// clamps, and a small world so hand-placed boids are always in range.
func isolate(sep, coh, aln float64) Params {
	p := DefaultParams()
	p.SeparationWeight = sep
	p.CohesionWeight = coh
	p.AlignmentWeight = aln
	return p
}

// engineWith builds an engine whose state is exactly the given boids,
// bypassing random placement, so rule tests are deterministic by hand.
func engineWith(p Params, boids []Boid) *Engine {
	e := NewEngine(len(boids), 1, p)
	e.SetBoids(boids)
	return e
}

func torusDist(a, b Vec2, p Params) float64 {
	return torusDelta(a, b, p.Width, p.Height).Len()
}

func TestSeparationRepels(t *testing.T) {
	p := isolate(1.5, 0, 0)
	boids := []Boid{
		{ID: 0, Pos: Vec2{800, 450}},
		{ID: 1, Pos: Vec2{810, 450}}, // dist 10 < SeparationRadius
	}
	e := engineWith(p, boids)
	before := torusDist(boids[0].Pos, boids[1].Pos, p)
	for range 10 {
		e.Tick()
	}
	got := e.Boids()
	after := torusDist(got[0].Pos, got[1].Pos, p)
	if after <= before {
		t.Fatalf("separation did not repel: dist %v -> %v", before, after)
	}
}

func TestCohesionAttracts(t *testing.T) {
	p := isolate(0, 1.0, 0)
	boids := []Boid{
		{ID: 0, Pos: Vec2{800, 450}},
		{ID: 1, Pos: Vec2{840, 450}}, // dist 40: inside NeighborRadius, outside SeparationRadius
	}
	e := engineWith(p, boids)
	before := torusDist(boids[0].Pos, boids[1].Pos, p)
	for range 10 {
		e.Tick()
	}
	got := e.Boids()
	after := torusDist(got[0].Pos, got[1].Pos, p)
	if after >= before {
		t.Fatalf("cohesion did not attract: dist %v -> %v", before, after)
	}
}

// headingVariance is circular variance: 1 - |mean unit heading|.
// 0 = perfectly aligned, 1 = uniformly scattered.
func headingVariance(boids []Boid) float64 {
	var sum Vec2
	n := 0
	for _, b := range boids {
		l := b.Vel.Len()
		if l == 0 {
			continue
		}
		sum = sum.Add(b.Vel.Scale(1 / l))
		n++
	}
	if n == 0 {
		return 0
	}
	return 1 - sum.Scale(1/float64(n)).Len()
}

func TestAlignmentConvergesHeadings(t *testing.T) {
	p := isolate(0, 0, 1.0)
	// A tight clump with divergent headings, all within NeighborRadius.
	boids := []Boid{
		{ID: 0, Pos: Vec2{800, 450}, Vel: Vec2{60, 0}},
		{ID: 1, Pos: Vec2{815, 450}, Vel: Vec2{0, 60}},
		{ID: 2, Pos: Vec2{800, 465}, Vel: Vec2{-42, 42}},
		{ID: 3, Pos: Vec2{815, 465}, Vel: Vec2{42, -42}},
	}
	e := engineWith(p, boids)
	before := headingVariance(e.Boids())
	for range 30 {
		e.Tick()
	}
	after := headingVariance(e.Boids())
	if after >= before {
		t.Fatalf("alignment did not converge headings: variance %v -> %v", before, after)
	}
}

func TestClampsAlwaysHold(t *testing.T) {
	p := DefaultParams()
	// Extreme weights to try to break the clamps.
	p.SeparationWeight = 100
	p.CohesionWeight = 100
	p.AlignmentWeight = 100
	e := NewEngine(50, 42, p)
	prev := make([]Boid, len(e.Boids()))
	copy(prev, e.Boids())
	maxDV := p.MaxForce * p.DT
	for tick := range 100 {
		e.Tick()
		cur := e.Boids()
		for i, b := range cur {
			if speed := b.Vel.Len(); speed > p.MaxSpeed+eps {
				t.Fatalf("tick %d boid %d: speed %v exceeds MaxSpeed %v", tick, i, speed, p.MaxSpeed)
			}
			if dv := b.Vel.Sub(prev[i].Vel).Len(); dv > maxDV+eps {
				t.Fatalf("tick %d boid %d: |Δv| %v exceeds MaxForce*DT %v", tick, i, dv, maxDV)
			}
		}
		copy(prev, cur)
	}
}

func TestDeterministicSeeding(t *testing.T) {
	p := DefaultParams()
	a := NewEngine(100, 7, p)
	b := NewEngine(100, 7, p)
	for range 100 {
		a.Tick()
		b.Tick()
	}
	if !reflect.DeepEqual(a.Boids(), b.Boids()) {
		t.Fatal("same seed+params produced divergent state after 100 ticks")
	}
	if a.TickCount() != 100 {
		t.Fatalf("TickCount = %d, want 100", a.TickCount())
	}
}

func TestDifferentSeedsDiverge(t *testing.T) {
	p := DefaultParams()
	a := NewEngine(100, 7, p)
	b := NewEngine(100, 8, p)
	if reflect.DeepEqual(a.Boids(), b.Boids()) {
		t.Fatal("different seeds produced identical initial state")
	}
}

func TestPositionsStayInWorld(t *testing.T) {
	p := DefaultParams()
	e := NewEngine(100, 3, p)
	for range 200 {
		e.Tick()
	}
	for i, b := range e.Boids() {
		if b.Pos.X < 0 || b.Pos.X >= p.Width || b.Pos.Y < 0 || b.Pos.Y >= p.Height ||
			math.IsNaN(b.Pos.X) || math.IsNaN(b.Pos.Y) {
			t.Fatalf("boid %d out of world: %+v", i, b.Pos)
		}
	}
}

func TestInitialSpeedsWithinClamp(t *testing.T) {
	p := DefaultParams()
	e := NewEngine(100, 9, p)
	for i, b := range e.Boids() {
		s := b.Vel.Len()
		if s == 0 || s > p.MaxSpeed+eps {
			t.Fatalf("boid %d initial speed %v, want (0, %v]", i, s, p.MaxSpeed)
		}
	}
}
