# graph-snapshots Delta — migrate-semstreams-beta149

## MODIFIED Requirements

### Requirement: Publication is decoupled and drop-oldest
Snapshots SHALL pass to a dedicated publisher goroutine through a bounded
channel with non-blocking send: when the publisher lags, new snapshots are
dropped and counted, and the tick loop proceeds unimpeded. The publisher
SHALL publish each boid as a BaseMessage-wrapped Graphable
(`boids.boid.v1`, entity ID `<org>.<platform>.sim.flock.boid.<id>`,
position/velocity properties and `flock.neighbor.of` relationships) to the
ENTITY stream consumed by graph-ingest. The entity ID SHALL be the canonical
6-part form and every emitted predicate SHALL be canonical 3-part
(`domain.category.property`), so graph-ingest's structural gate accepts the
mutation rather than rejecting it.

Within a snapshot, per-boid publishes SHALL be dispatched as one async
batch (semstreams `PublishBatchToStream`, gh#470) — pipelined past the
per-ack RTT ceiling and joined on every ack before the snapshot completes.
Snapshots SHALL be consumed strictly one at a time: every publish for
snapshot N is acknowledged before snapshot N+1 begins, so no entity's
updates can reorder across snapshots (each boid appears at most once per
snapshot, and all publish to one subject, in-order per connection).
Stateful bookkeeping (neighbor-empty tracking and removals) SHALL remain on
the coordinator goroutine, after the batch joins.

#### Scenario: Physics unaffected by publisher pressure
- **GIVEN** a publisher stalled (or a dial far beyond ingest budget)
- **WHEN** ticks execute
- **THEN** tick timing is unchanged and the drop counter increases

#### Scenario: Boids land in the graph
- **GIVEN** a running flow with graph-ingest
- **WHEN** a snapshot publishes
- **THEN** boid entities appear in ENTITY_STATES with position triples and
  their current neighbor relationships (`flock.neighbor.of`)

#### Scenario: No cross-snapshot reordering per entity
- **GIVEN** async batch publishing and consecutive snapshots N and N+1 both
  containing boid B
- **WHEN** both snapshots publish
- **THEN** B's snapshot-N publish is acknowledged before B's snapshot-N+1
  publish is issued

#### Scenario: Async publish raises the instrument ceiling
- **GIVEN** a dial that saturated the old serial publisher (the ~21.6/s
  ceiling at 200 boids)
- **WHEN** the same dial runs with async batch publishing
- **THEN** the achieved snapshot rate matches the dial and the drop counter
  stays flat (the instrument is no longer the bottleneck)

#### Scenario: The structural gate accepts conforming snapshots
- **GIVEN** graph-ingest's beta.149 structural-validation gate
- **WHEN** a snapshot of canonical 6-part boid entities carrying only 3-part
  predicates (`flock.position.*`, `flock.velocity.*`, `flock.neighbor.count`,
  `flock.neighbor.of`) is published
- **THEN** the mutation passes the gate and is persisted, and the
  structural-reject counter stays flat at zero
