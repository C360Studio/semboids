# flock-physics Specification

## Purpose
TBD - created by archiving change add-flock-core. Update Purpose after archive.
## Requirements
### Requirement: In-process simulation loop stays off the substrate
The physics engine SHALL run as an in-process Go loop with a fixed timestep
derived from a configurable tick rate (default 30Hz) over a configurable boid
population (default 200). The tick path SHALL NOT perform per-boid NATS
publishes, KV writes, rule evaluations, lifecycle transitions, or graph
mutations. Per-tick substrate interaction is limited to: one aggregated
frame handed to the egress capability, plus zone transition events published
only on containment edges (never steady-state). Zone containment checks and
steering-modifier application SHALL be in-process operations on in-memory
state. `internal/flock` SHALL NOT import SemStreams packages.

#### Scenario: Tick has no per-boid substrate traffic
- **GIVEN** a running simulation of N boids with zones configured
- **WHEN** one tick executes with no boid crossing a zone boundary
- **THEN** at most one message is published (the aggregated frame) and zero
  KV/graph/rule/lifecycle operations occur

#### Scenario: Edge ticks add only transition events
- **WHEN** K boids cross zone boundaries during a tick
- **THEN** exactly K transition events are published in addition to the
  frame, and no other substrate traffic occurs

#### Scenario: Tick budget holds at target scale
- **WHEN** `BenchmarkTick` runs at 200 and 500 boids with zones configured
- **THEN** a single tick completes in far less than the 33ms tick period on
  a developer machine

### Requirement: Reynolds steering produces emergent flocking
Each boid SHALL compute separation, cohesion, and alignment steering from
neighbors within per-rule configurable radii, weighted by per-rule
configurable weights, with the summed steering clamped by a maximum force and
resulting velocity clamped by a maximum speed. Steering for a tick SHALL be
computed from the previous tick's state (double-buffered) so update order
cannot bias results.

#### Scenario: Separation repels
- **GIVEN** two boids within separation radius
- **WHEN** a tick executes
- **THEN** the distance between them does not decrease

#### Scenario: Alignment converges headings
- **GIVEN** a neighborhood of boids with divergent headings and alignment
  enabled
- **WHEN** multiple ticks execute
- **THEN** heading variance within the neighborhood decreases

#### Scenario: Clamps always hold
- **WHEN** any tick executes with any parameter set
- **THEN** no boid's speed exceeds max speed and no applied steering exceeds
  max force

### Requirement: Spatial-hash neighbor queries
Neighbor lookups SHALL use a uniform-grid spatial hash with cell size equal to
the largest neighbor radius, rebuilt each tick into reused buckets, so that
per-boid neighbor cost depends on local density rather than total population.

#### Scenario: Wrap-around neighbors are found
- **GIVEN** two boids within neighbor radius across the world's toroidal edge
- **WHEN** neighbors are queried
- **THEN** each appears in the other's neighbor set

### Requirement: Deterministic seeding
Given identical seed, parameters, and tick count, the engine SHALL produce
identical world state.

#### Scenario: Reproducible trajectory
- **GIVEN** two engines constructed with the same seed and params
- **WHEN** both advance N ticks
- **THEN** their boid states are identical

### Requirement: Toroidal 2D world
The world SHALL be a 2D rectangle (default 1600×900) with toroidal wrapping
for position, distance, and neighbor queries.

#### Scenario: Positions wrap
- **WHEN** a boid crosses a world edge
- **THEN** its position re-enters from the opposite edge with velocity
  unchanged

### Requirement: External steering modifiers apply within existing clamps
The engine SHALL accept an external per-boid steering vector each tick,
summed with the Reynolds terms BEFORE the total-steering MaxForce clamp, so
no modifier can exceed the force budget or violate the max-speed clamp. The
external term SHALL be read from state prepared before the tick (no channel
reads, lock acquisition, or I/O inside the per-boid loop).

#### Scenario: Modifier steers the boid
- **GIVEN** a boid with a flee modifier away from a zone center
- **WHEN** ticks execute
- **THEN** the boid's trajectory bends away from the zone relative to an
  unmodified run with the same seed

#### Scenario: Clamps still hold under modifiers
- **GIVEN** extreme modifier magnitudes
- **WHEN** any tick executes
- **THEN** no boid's speed exceeds MaxSpeed and no per-tick velocity change
  exceeds MaxForce×DT

#### Scenario: Determinism preserved without modifiers
- **GIVEN** the same seed and params and no modifiers
- **WHEN** two engines advance N ticks
- **THEN** their states are identical (the modifier path adds no
  nondeterminism when unused)

