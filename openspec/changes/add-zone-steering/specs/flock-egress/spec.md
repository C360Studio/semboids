# flock-egress Specification (delta)

## MODIFIED Requirements

### Requirement: One aggregated frame per tick
The sim component SHALL publish exactly one frame per tick to the core-NATS
subject `boids.frames` as a fire-and-forget publish (no JetStream
persistence). The frame SHALL carry tick number, timestamp, world
dimensions, zone geometry (`[type, x, y, r]` per zone), and per-boid
`[id, x, y, vx, vy, m]` compact arrays where `m` encodes the boid's active
modifier kind (0 none, 1 flee, 2 attract, 3 wind).

#### Scenario: Frame cadence matches tick rate
- **GIVEN** a simulation ticking at 30Hz
- **WHEN** a subscriber listens on `boids.frames` for one second
- **THEN** it receives approximately 30 frames, each with the full
  population and all configured zones

#### Scenario: Modifier flag reflects live influence
- **GIVEN** a boid currently under a flee modifier
- **WHEN** the next frame publishes
- **THEN** that boid's tuple carries `m = 1` and reverts to `m = 0` after
  the modifier ends

## ADDED Requirements

### Requirement: Zone transition events publish at event rate
The sim component SHALL publish zone transition events to `boids.zone.events`
(core NATS) as BaseMessage-wrapped registered payloads, only on containment
edges, alongside — never replacing — the frame stream.

#### Scenario: Events decodable by the rule processor
- **GIVEN** the rule processor subscribed to `boids.zone.events` via a nats
  input port
- **WHEN** a transition event publishes
- **THEN** the rule processor decodes it and its conditions can address
  `boid_id`, `zone_id`, `zone_type`, and `event`

### Requirement: Steering modifiers are consumed from the substrate
The sim component SHALL subscribe to `boids.steering` (core NATS), decode
rule-engine publish-action payloads, and stage valid modifiers
(`boid_id`, `zone_id`, `kind`, `ttl_ticks` in `properties`) for the next
tick; malformed or unknown-kind modifiers SHALL be dropped with a warning,
never crashing the loop.

#### Scenario: Round trip from rule to physics
- **GIVEN** an enabled predator rule and a boid entering a predator zone
- **WHEN** the rule fires
- **THEN** the resulting modifier is applied to that boid within a few ticks
  of the transition event

#### Scenario: Malformed modifier ignored
- **WHEN** a message with an unknown `kind` arrives on `boids.steering`
- **THEN** it is logged and dropped; ticking and other modifiers are
  unaffected
