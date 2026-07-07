# zone-steering Specification

## Purpose
TBD - created by archiving change add-zone-steering. Update Purpose after archive.
## Requirements
### Requirement: Edge-triggered zone transition events
The sim SHALL detect boid zone containment in-process each tick against
static zone geometry and publish a transition event ONLY when a boid's
containment state changes (enter or exit edge). Events SHALL be published
fire-and-forget to core NATS (`boids.zone.events`) as BaseMessage-wrapped
payloads carrying `boid_id`, `zone_id`, `zone_type`, `event`
(`entered`|`exited`), and `tick`.

#### Scenario: Enter and exit each fire once
- **GIVEN** a boid crossing into and later out of a zone
- **WHEN** the crossing happens
- **THEN** exactly one `entered` event and one `exited` event are published
  for that visit, regardless of how many ticks the boid spends inside

#### Scenario: No steady-state traffic
- **GIVEN** all boids stationary relative to zone membership
- **WHEN** ticks execute
- **THEN** no transition events are published

### Requirement: Rules translate transitions into steering modifiers
Zone behavior SHALL be expressed as SemStreams JSON rules (loaded via the
rule processor's `rules_files`) whose conditions match transition-event
fields and whose `publish` actions emit steering modifiers to
`boids.steering`, forwarding the triggering `boid_id` and `zone_id` via
`$message.*` substitution. A modifier carries `boid_id`, `zone_id`, `kind`
(`flee`|`attract`|`wind`|`cancel`), and `ttl_ticks`. Exit transitions SHALL
produce `cancel` modifiers for the boid/zone pair.

#### Scenario: Predator entry produces flee modifier
- **GIVEN** the predator rule enabled
- **WHEN** a boid's `entered` event for a predator zone is processed
- **THEN** a `flee` modifier for that boid and zone arrives on
  `boids.steering`

#### Scenario: Disabled rule produces nothing
- **GIVEN** the predator rule disabled
- **WHEN** a boid enters a predator zone
- **THEN** no modifier is emitted and the boid's motion is unaffected

### Requirement: Modifier lifecycle is TTL-bounded
The sim SHALL hold received modifiers in an in-memory table keyed by boid
and zone, decrement TTLs each tick, remove entries on `cancel`, and expire
entries whose TTL reaches zero — a missed exit event therefore self-heals.
Modifier state SHALL never persist across restarts.

#### Scenario: Cancel removes influence
- **GIVEN** a boid under a wind modifier
- **WHEN** its `cancel` modifier arrives
- **THEN** the wind term no longer contributes from the next tick

#### Scenario: TTL expiry self-heals
- **GIVEN** a modifier whose exit event was lost
- **WHEN** its TTL reaches zero
- **THEN** the influence ends without any external input

### Requirement: Rule toggling changes behavior live
Toggling a zone rule SHALL take effect on a running simulation without
restarting the host or the sim component, and the behavioral change SHALL be
observable in the flock (e.g. boids stop scattering at predator zones)
within a few seconds.

#### Scenario: Toggle off mid-run
- **GIVEN** a running demo with boids scattering at a predator zone
- **WHEN** the predator rule is toggled off
- **THEN** subsequent zone entries produce no reaction, with no process
  restart

#### Scenario: Toggle back on
- **WHEN** the rule is re-enabled
- **THEN** the next zone entry produces the flee reaction again

### Requirement: Predator zones cull lingering boids
The sim SHALL emit a lingered zone event (`event="lingered"`) on the
zone-transition stream (`boids.zone.events`) for a boid that dwells in a
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

