# flock-physics Delta — add-lifecycle-population

## ADDED Requirements

### Requirement: Population is mutable between ticks
The engine SHALL support `AddBoids`/`RemoveBoids` with monotonic stable IDs,
applied by the tick loop between ticks via the same staging discipline as
steering modifiers — no locks, KV, or lifecycle calls in the per-tick path.
Adding or removing boids SHALL NOT disturb the identity, position, or motion
of surviving boids, and the spatial hash SHALL rebuild from the current
population each tick. A run whose population never changes SHALL remain
bit-for-bit deterministic (the staging path is never entered).

#### Scenario: Spawns and culls apply between ticks
- **GIVEN** staged add and remove deltas
- **WHEN** the next tick begins
- **THEN** the population reflects them, surviving boids are unchanged, and the
  tick rate holds

#### Scenario: Fixed population stays deterministic
- **GIVEN** no population changes over a run
- **WHEN** the sim runs from a fixed seed
- **THEN** output is identical to the pre-change engine
