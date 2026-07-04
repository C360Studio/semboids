package flock

import (
	"reflect"
	"testing"
)

func TestExternalSteeringBendsTrajectory(t *testing.T) {
	p := isolate(0, 0, 0) // Reynolds off: isolate the external term
	boids := []Boid{{ID: 0, Pos: Vec2{800, 450}, Vel: Vec2{60, 0}}}

	plain := engineWith(p, boids)
	steered := engineWith(p, boids)
	ext := map[uint32]Vec2{0: {0, p.MaxForce}} // push +Y
	steered.SetExternalSteering(ext)

	for range 30 {
		plain.Tick()
		steered.Tick()
	}

	pb := plain.Boids()[0]
	sb := steered.Boids()[0]
	if reflect.DeepEqual(pb, sb) {
		t.Fatal("external steering had no effect")
	}
	if sb.Vel.Y <= pb.Vel.Y {
		t.Fatalf("expected +Y velocity gain: steered %v vs plain %v", sb.Vel, pb.Vel)
	}
}

func TestExternalSteeringClampsHold(t *testing.T) {
	p := DefaultParams()
	e := NewEngine(50, 42, p)
	// Absurd external magnitudes on every boid.
	ext := make(map[uint32]Vec2, 50)
	for _, b := range e.Boids() {
		ext[b.ID] = Vec2{1e9, -1e9}
	}
	e.SetExternalSteering(ext)

	prev := make([]Boid, len(e.Boids()))
	copy(prev, e.Boids())
	maxDV := p.MaxForce * p.DT
	for tick := range 50 {
		e.Tick()
		for i, b := range e.Boids() {
			if s := b.Vel.Len(); s > p.MaxSpeed+eps {
				t.Fatalf("tick %d boid %d: speed %v exceeds MaxSpeed", tick, i, s)
			}
			if dv := b.Vel.Sub(prev[i].Vel).Len(); dv > maxDV+eps {
				t.Fatalf("tick %d boid %d: |Δv| %v exceeds MaxForce*DT %v", tick, i, dv, maxDV)
			}
		}
		copy(prev, e.Boids())
	}
}

func TestDeterminismUnaffectedByEmptyExternal(t *testing.T) {
	p := DefaultParams()
	a := NewEngine(100, 7, p)
	b := NewEngine(100, 7, p)
	b.SetExternalSteering(map[uint32]Vec2{}) // empty map, not nil
	for range 100 {
		a.Tick()
		b.Tick()
	}
	if !reflect.DeepEqual(a.Boids(), b.Boids()) {
		t.Fatal("empty external steering changed trajectories")
	}
}

func TestExternalSteeringClearable(t *testing.T) {
	p := isolate(0, 0, 0)
	boids := []Boid{{ID: 0, Pos: Vec2{800, 450}, Vel: Vec2{60, 0}}}
	e := engineWith(p, boids)
	e.SetExternalSteering(map[uint32]Vec2{0: {0, p.MaxForce}})
	e.Tick()
	yAfterPush := e.Boids()[0].Vel.Y

	e.SetExternalSteering(nil)
	for range 5 {
		e.Tick()
	}
	if got := e.Boids()[0].Vel.Y; got != yAfterPush {
		t.Fatalf("velocity still changing after external cleared: %v -> %v", yAfterPush, got)
	}
}
