# flock-physics Specification (delta)

## MODIFIED Requirements

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

## ADDED Requirements

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
