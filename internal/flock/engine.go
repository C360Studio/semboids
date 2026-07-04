// Package flock implements the in-process Reynolds boids engine: separation,
// cohesion, and alignment over a toroidal 2D world with spatial-hash neighbor
// queries. Per ADR-001 this package is pure simulation — it has no SemStreams
// imports and performs no I/O; the tick loop is driven and published by the
// sim component that owns it.
package flock

import (
	"math"
	"math/rand/v2"
)

// Boid is one simulated agent.
type Boid struct {
	ID  uint32
	Pos Vec2
	Vel Vec2
}

// Engine advances a boid population one fixed timestep at a time. Steering
// for a tick is computed from the previous tick's state (double-buffered) so
// update order cannot bias the flock. Engine is not safe for concurrent use;
// the owning component serializes Tick and reads.
type Engine struct {
	p       Params
	buf     [2][]Boid
	cur     int
	ticks   uint64
	grid    *grid
	scratch []int32
	// external holds per-boid steering staged by the owner before Tick
	// (zone modifiers per ADR-001's rules→physics contract). Read-only
	// during Tick; nil or empty means no external influence.
	external map[uint32]Vec2
}

// NewEngine seeds n boids deterministically: uniform random positions,
// random headings at 50–100% of MaxSpeed. Same n, seed, and params produce
// identical trajectories.
func NewEngine(n int, seed uint64, p Params) *Engine {
	rng := rand.New(rand.NewPCG(seed, seed))
	e := &Engine{
		p:    p,
		grid: newGrid(p.Width, p.Height, p.NeighborRadius),
	}
	e.buf[0] = make([]Boid, n)
	e.buf[1] = make([]Boid, n)
	for i := range e.buf[0] {
		angle := rng.Float64() * 2 * math.Pi
		speed := p.MaxSpeed * (0.5 + 0.5*rng.Float64())
		e.buf[0][i] = Boid{
			ID:  uint32(i),
			Pos: Vec2{rng.Float64() * p.Width, rng.Float64() * p.Height},
			Vel: Vec2{math.Cos(angle), math.Sin(angle)}.Scale(speed),
		}
	}
	return e
}

// SetBoids replaces the population with an exact state (primarily for tests
// and future spawn/despawn wiring).
func (e *Engine) SetBoids(boids []Boid) {
	n := len(boids)
	e.buf[0] = make([]Boid, n)
	e.buf[1] = make([]Boid, n)
	copy(e.buf[e.cur], boids)
}

// Boids returns the current state. The slice is the engine's internal buffer:
// read-only, valid until the next Tick.
func (e *Engine) Boids() []Boid { return e.buf[e.cur] }

// TickCount returns the number of completed ticks.
func (e *Engine) TickCount() uint64 { return e.ticks }

// SetExternalSteering stages per-boid external steering (units/second²)
// applied on subsequent ticks, summed with the Reynolds terms before the
// MaxForce clamp — external influence can never exceed the force budget.
// The engine keeps the reference; the owner must not mutate the map while
// Tick runs. Pass nil to clear.
func (e *Engine) SetExternalSteering(ext map[uint32]Vec2) { e.external = ext }

// Params returns the engine's simulation parameters.
func (e *Engine) Params() Params { return e.p }

// Tick advances the simulation one fixed timestep.
func (e *Engine) Tick() {
	cur := e.buf[e.cur]
	next := e.buf[1-e.cur]
	e.grid.rebuild(cur)
	for i := range cur {
		b := cur[i]
		acc := e.steer(cur, i)
		vel := b.Vel.Add(acc.Scale(e.p.DT)).Limit(e.p.MaxSpeed)
		pos := wrap(b.Pos.Add(vel.Scale(e.p.DT)), e.p.Width, e.p.Height)
		next[i] = Boid{ID: b.ID, Pos: pos, Vel: vel}
	}
	e.cur = 1 - e.cur
	e.ticks++
}

// steer computes the combined Reynolds steering force for boids[i], clamped
// to MaxForce.
func (e *Engine) steer(boids []Boid, i int) Vec2 {
	self := boids[i]
	var ext Vec2
	if e.external != nil {
		ext = e.external[self.ID]
	}
	e.scratch = e.grid.neighbors(boids, i, e.p.NeighborRadius, e.scratch[:0])
	if len(e.scratch) == 0 {
		return ext.Limit(e.p.MaxForce)
	}

	var sep, cohSum, alnSum Vec2
	nSep := 0
	for _, j := range e.scratch {
		other := boids[j]
		d := torusDelta(self.Pos, other.Pos, e.p.Width, e.p.Height)
		dist := d.Len()
		if dist < e.p.SeparationRadius && dist > 0 {
			// Inverse-square repulsion away from the neighbor.
			sep = sep.Sub(d.Scale(1 / (dist * dist)))
			nSep++
		}
		cohSum = cohSum.Add(d)
		alnSum = alnSum.Add(other.Vel)
	}

	n := float64(len(e.scratch))
	var acc Vec2
	if nSep > 0 && e.p.SeparationWeight != 0 {
		acc = acc.Add(e.steerToward(sep, self.Vel).Scale(e.p.SeparationWeight))
	}
	if e.p.CohesionWeight != 0 {
		acc = acc.Add(e.steerToward(cohSum.Scale(1/n), self.Vel).Scale(e.p.CohesionWeight))
	}
	if e.p.AlignmentWeight != 0 {
		acc = acc.Add(e.steerToward(alnSum.Scale(1/n), self.Vel).Scale(e.p.AlignmentWeight))
	}
	return acc.Add(ext).Limit(e.p.MaxForce)
}

// steerToward converts a desired direction into a Reynolds steering force:
// scale desired to MaxSpeed, subtract current velocity, clamp to MaxForce.
func (e *Engine) steerToward(desired, vel Vec2) Vec2 {
	l := desired.Len()
	if l == 0 {
		return Vec2{}
	}
	return desired.Scale(e.p.MaxSpeed / l).Sub(vel).Limit(e.p.MaxForce)
}
