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
	// rng continues the seed sequence for spawn placement (AddBoids), so a
	// given spawn sequence is reproducible. Untouched after seeding when the
	// population never changes → fixed-population runs stay deterministic.
	rng *rand.Rand
	// nextID is the monotonic ID allocator; starts past the seeded IDs and
	// never reuses, so a culled-then-respawned slot cannot collide.
	nextID uint32
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
		p:      p,
		grid:   newGrid(p.Width, p.Height, p.NeighborRadius),
		rng:    rng,
		nextID: uint32(n),
	}
	e.buf[0] = make([]Boid, n)
	e.buf[1] = make([]Boid, n)
	for i := range e.buf[0] {
		e.buf[0][i] = e.spawnBoid(uint32(i))
	}
	return e
}

// spawnBoid draws one boid with the given ID from the seed sequence — uniform
// position, random heading at 50–100% of MaxSpeed (the NewEngine placement,
// factored so spawns match seeding).
func (e *Engine) spawnBoid(id uint32) Boid {
	angle := e.rng.Float64() * 2 * math.Pi
	speed := e.p.MaxSpeed * (0.5 + 0.5*e.rng.Float64())
	return Boid{
		ID:  id,
		Pos: Vec2{e.rng.Float64() * e.p.Width, e.rng.Float64() * e.p.Height},
		Vel: Vec2{math.Cos(angle), math.Sin(angle)}.Scale(speed),
	}
}

// AddBoids appends n freshly-placed boids with new monotonic IDs and returns
// those IDs. Applied to the current buffer; the next Tick reconciles the
// double buffer (population changes happen between ticks — the sim stages them
// like steering modifiers). n <= 0 is a no-op.
func (e *Engine) AddBoids(n int) []uint32 {
	if n <= 0 {
		return nil
	}
	ids := make([]uint32, n)
	cur := e.buf[e.cur]
	for i := range n {
		id := e.nextID
		e.nextID++
		cur = append(cur, e.spawnBoid(id))
		ids[i] = id
	}
	e.buf[e.cur] = cur
	return ids
}

// RemoveBoids drops the given IDs from the current buffer, preserving the
// order, identity, and state of every survivor. Unknown IDs are ignored. The
// next Tick reconciles the double buffer.
func (e *Engine) RemoveBoids(ids []uint32) {
	if len(ids) == 0 {
		return
	}
	drop := make(map[uint32]struct{}, len(ids))
	for _, id := range ids {
		drop[id] = struct{}{}
	}
	cur := e.buf[e.cur]
	kept := cur[:0]
	for _, b := range cur {
		if _, gone := drop[b.ID]; !gone {
			kept = append(kept, b)
		}
	}
	e.buf[e.cur] = kept
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

// SnapshotNeighbors returns each boid's neighbor IDs within radius (torus
// distance, self excluded) from the current state. It rebuilds the reusable
// grid — safe because the owner serializes it with Tick — and allocates the
// result; intended for snapshot cadence (graph publishing), not per tick.
func (e *Engine) SnapshotNeighbors(radius float64) map[uint32][]uint32 {
	boids := e.Boids()
	e.grid.rebuild(boids)
	out := make(map[uint32][]uint32, len(boids))
	for i := range boids {
		e.scratch = e.grid.neighbors(boids, i, radius, e.scratch[:0])
		ns := make([]uint32, 0, len(e.scratch))
		for _, j := range e.scratch {
			ns = append(ns, boids[j].ID)
		}
		out[boids[i].ID] = ns
	}
	return out
}

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
	// Reconcile the back buffer to the current population — AddBoids/
	// RemoveBoids resize only the front buffer between ticks. A no-op when the
	// size is unchanged, so fixed-population trajectories are untouched.
	if len(e.buf[1-e.cur]) != len(cur) {
		e.buf[1-e.cur] = make([]Boid, len(cur))
	}
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
