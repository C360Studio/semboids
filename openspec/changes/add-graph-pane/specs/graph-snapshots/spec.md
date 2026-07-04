# graph-snapshots Specification (delta)

## ADDED Requirements

### Requirement: Snapshots derive on the tick loop at a tunable cadence
The sim SHALL derive a boid graph snapshot every Nth physics tick, where N
follows from the snapshot cadence (`graph_hz` config / `--graph-hz` flag,
default 1Hz; runtime-adjustable via the boids API). Derivation SHALL read
the current engine state and neighbor sets (query radius configurable,
default the physics neighbor radius) and copy them into an immutable
snapshot value. The per-boid tick path gains no NATS, KV, rule, or graph
traffic from this change.

#### Scenario: Cadence follows the dial
- **GIVEN** a simulation at 30Hz with graph_hz = 1
- **WHEN** 300 ticks execute
- **THEN** approximately 10 snapshots are derived

#### Scenario: Runtime dial change
- **GIVEN** a running host at graph_hz = 1
- **WHEN** the dial is set to 10 via the boids API
- **THEN** subsequent snapshots derive at ~10Hz without restart

### Requirement: Publication is decoupled and drop-oldest
Snapshots SHALL pass to a dedicated publisher goroutine through a bounded
channel with non-blocking send: when the publisher lags, new snapshots are
dropped and counted, and the tick loop proceeds unimpeded. The publisher
SHALL publish each boid as a BaseMessage-wrapped Graphable
(`boids.boid.v1`, entity ID `<org>.<platform>.sim.flock.boid.<id>`,
position/velocity properties and `flock.neighbor` relationships) to the
ENTITY stream consumed by graph-ingest.

#### Scenario: Physics unaffected by publisher pressure
- **GIVEN** a publisher stalled (or a dial far beyond ingest budget)
- **WHEN** ticks execute
- **THEN** tick timing is unchanged and the drop counter increases

#### Scenario: Boids land in the graph
- **GIVEN** a running flow with graph-ingest
- **WHEN** a snapshot publishes
- **THEN** boid entities appear in ENTITY_STATES with position triples and
  their current neighbor relationships

### Requirement: Neighbor sets replace on each snapshot
Each snapshot SHALL publish the boid's full current neighbor set so
graph-ingest's predicate-level merge replaces the previous set. The
empty-set limitation (merge cannot express "now zero neighbors") SHALL be
handled per the design's D6 outcome — either owned-projection replacement
via the mutation API, or the documented cosmetic-staleness fallback with an
always-present `flock.neighbor.count` property.

#### Scenario: Neighbor churn does not accumulate
- **GIVEN** a boid whose neighbor set changes between snapshots
- **WHEN** the second snapshot lands
- **THEN** ENTITY_STATES holds only the current neighbor relationships (no
  union of past sets)

### Requirement: Snapshot pipeline is observable
The pipeline SHALL expose Prometheus metrics: snapshots published, snapshots
dropped, publish duration, and the current cadence — sufficient for the
load-dial campaign to correlate dial settings with substrate behavior.

#### Scenario: Drops are visible
- **WHEN** the dial exceeds what the publisher sustains
- **THEN** the drop counter on :9090 increases while tick metrics stay flat
