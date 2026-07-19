# graph-snapshots Specification

## Purpose
TBD - created by archiving change add-graph-pane. Update Purpose after archive.
## Requirements
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
position/velocity properties and `flock.neighbor.of` relationships) to the
ENTITY stream consumed by graph-ingest. The entity ID SHALL be the canonical
6-part form and every emitted predicate SHALL be canonical 3-part
(`domain.category.property`), so graph-ingest's canonical-contract validation
accepts the mutation rather than rejecting it.

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

#### Scenario: Conforming snapshots pass the canonical-contract gate
- **GIVEN** graph-ingest's fail-closed canonical-contract validation
- **WHEN** a snapshot of canonical 6-part boid entities carrying only 3-part
  predicates (`flock.position.*`, `flock.velocity.*`, `flock.neighbor.count`,
  `flock.neighbor.of`) is published
- **THEN** the mutation is persisted and the predicate/entity contract-reject
  counters stay flat at zero

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

### Requirement: Neighbor sets replace on each snapshot
Each snapshot SHALL publish the boid's full current neighbor set so
graph-ingest's predicate-level merge (`MergeTriples`) replaces the previous
set for the `flock.neighbor.of` predicate.

The **empty-set case is a verified substrate limitation on the current pinned
SemStreams (beta.152)**: the stream-upsert path (`entity.boid.upsert` →
`MergeEntity`) is add/merge-only and cannot express "now zero neighbors" — an
arrival carrying no `flock.neighbor.of` triple leaves the resident edges in
place (correct merge behavior, since it preserves predicates owned by other
writers such as `flock.lifecycle.phase`). This was confirmed end-to-end:
republishing a boid with an empty neighbor set leaves the stale
`flock.neighbor.of` edges present in **both** ENTITY_STATES (what the graph
pane reads) and the derived INCOMING index; ADR-077 replacement is index-side
and re-projects whatever ENTITY_STATES holds, so it does not clear them.

Therefore, on a boid's non-empty→empty transition the publisher SHALL clear
the edges via the substrate mutation API (`graph.mutation.triple.remove` for
`flock.neighbor.of`), tracked on the off-loop coordinator goroutine
(`prevHadNeighbors`) so the ADR-001 physics hot path is untouched. This
removal is verified-necessary, not a legacy relic. Each snapshot SHALL also
publish an always-present `flock.neighbor.count` property — a genuine
published degree property and the graph pane's neighbor-set reset sentinel.

The substrate-native alternative (retiring the coordinator's transition
tracker) is tracked upstream as **C360Studio/semstreams#578** (opt-in
source-authoritative predicate replacement on stream arrival); if it lands,
a future change retires the tracker. The app SHALL NOT add a second app-side
path in the meantime (Product Boundary).

#### Scenario: Neighbor churn does not accumulate
- **GIVEN** a boid whose neighbor set changes between snapshots
- **WHEN** the second snapshot lands
- **THEN** ENTITY_STATES holds only the current neighbor relationships (no
  union of past sets)

#### Scenario: Emptying a neighbor set clears the edges
- **GIVEN** a boid that had `flock.neighbor.of` edges in the previous snapshot
- **WHEN** its next snapshot has an empty neighbor set
- **THEN** the publisher issues a `graph.mutation.triple.remove` for
  `flock.neighbor.of` on that boid
- **AND** the boid's `flock.neighbor.of` edges are cleared from ENTITY_STATES
  and the INCOMING index (the stream merge alone does not clear them)

### Requirement: Snapshot pipeline is observable
The pipeline SHALL expose Prometheus metrics: snapshots published, boid
entities published, snapshots dropped, per-snapshot publish duration, and
the current cadence — sufficient for a sweep window to be classified as
publisher-bound (drops rising) or not from :9090 alone, and for achieved
snapshot/entity rates to be derived without log scraping.

#### Scenario: Drops are visible
- **WHEN** the dial exceeds what the publisher sustains
- **THEN** the drop counter on :9090 increases while tick metrics stay flat

#### Scenario: Achieved rate is derivable
- **GIVEN** a sweep window at a fixed dial
- **WHEN** :9090 is scraped at the window's start and end
- **THEN** achieved snapshots/s and entities/s over the window follow from
  the published counters

