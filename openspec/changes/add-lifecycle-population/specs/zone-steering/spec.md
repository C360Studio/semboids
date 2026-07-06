# zone-steering Delta — add-lifecycle-population

## ADDED Requirements

### Requirement: Predator zones cull lingering boids
The sim SHALL emit a `boids.zone.lingered` event for a boid that dwells in a
predator zone beyond a configurable grace window (a sim-side tick count, so
the rule stays stateless). A `predator-cull` rule SHALL fire
`lifecycle_transition → culled` on that boid via the rule engine's lifecycle
action. The rule SHALL key off the `lingered` event fields, not
`$entity.lifecycle.*` conditions, to avoid the per-fire full-bucket scan. The
cull rule SHALL be independently toggleable like the other zone rules.

#### Scenario: A lingering boid is culled through a rule
- **GIVEN** the predator-cull rule enabled and a boid past the grace window in
  a predator zone
- **WHEN** the sim emits `lingered`
- **THEN** the rule transitions the boid to `culled` and the sim removes it

#### Scenario: A fleeing boid survives
- **GIVEN** the flee modifier pushing a boid out before the grace window
- **WHEN** ticks advance
- **THEN** no `lingered` event fires and the boid is not culled

#### Scenario: Cull rule toggles off cleanly
- **GIVEN** the predator-cull rule disabled via the rule toggle
- **WHEN** boids linger in a predator zone
- **THEN** no culls occur (flee steering still applies)
