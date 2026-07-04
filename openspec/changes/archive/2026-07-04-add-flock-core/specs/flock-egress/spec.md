# flock-egress Specification (delta)

## ADDED Requirements

### Requirement: One aggregated frame per tick
The sim component SHALL publish exactly one frame per tick to the core-NATS
subject `boids.frames` as a fire-and-forget publish (no JetStream
persistence). The frame SHALL carry tick number, timestamp, world dimensions,
and per-boid `[id, x, y, vx, vy]` compact arrays.

#### Scenario: Frame cadence matches tick rate
- **GIVEN** a simulation ticking at 30Hz
- **WHEN** a subscriber listens on `boids.frames` for one second
- **THEN** it receives approximately 30 frames, each with the full population

### Requirement: Sim runs as a SemStreams Input component
The physics engine SHALL be owned by a SemStreams Input component registered
in the component registry and wired via flow config, so ServiceManager governs
its lifecycle; context cancellation SHALL stop the tick loop cleanly.

#### Scenario: Clean shutdown
- **GIVEN** a running sim component
- **WHEN** its context is cancelled
- **THEN** the tick goroutine exits without publishing further frames

### Requirement: At-most-once browser broadcast
Frames SHALL reach browsers via the SemStreams `output/websocket` component in
at-most-once mode with per-client write timeouts. A slow or stalled client
SHALL only lose its own frames; it SHALL NOT delay other clients or the
physics loop.

#### Scenario: Slow client sheds frames
- **GIVEN** one healthy client and one stalled client
- **WHEN** frames broadcast at 30Hz
- **THEN** the healthy client keeps receiving ~30 frames/s and physics tick
  timing is unaffected

#### Scenario: Mid-run join
- **WHEN** a client connects while the simulation is running
- **THEN** it begins receiving complete frames from the next broadcast with no
  replay required
