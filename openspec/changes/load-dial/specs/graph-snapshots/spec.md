# graph-snapshots Delta — load-dial

## MODIFIED Requirements

### Requirement: Publication is decoupled and drop-oldest
Snapshots SHALL pass to a dedicated publisher goroutine through a bounded
channel with non-blocking send: when the publisher lags, new snapshots are
dropped and counted, and the tick loop proceeds unimpeded. The publisher
SHALL publish each boid as a BaseMessage-wrapped Graphable
(`boids.boid.v1`, entity ID `<org>.<platform>.sim.flock.boid.<id>`,
position/velocity properties and `flock.neighbor` relationships) to the
ENTITY stream consumed by graph-ingest.

Within a snapshot, per-boid publishes SHALL fan out across a bounded worker
pool (`graph_publish_workers`, default 16; 1 restores exact serial
behavior). Snapshots SHALL be consumed strictly one at a time: every
publish for snapshot N completes before snapshot N+1 begins, so no entity's
updates can reorder across snapshots (each boid appears at most once per
snapshot). Stateful bookkeeping (neighbor-empty tracking and removals)
SHALL remain on the coordinator goroutine, outside the worker pool.

#### Scenario: Physics unaffected by publisher pressure
- **GIVEN** a publisher stalled (or a dial far beyond ingest budget)
- **WHEN** ticks execute
- **THEN** tick timing is unchanged and the drop counter increases

#### Scenario: Boids land in the graph
- **GIVEN** a running flow with graph-ingest
- **WHEN** a snapshot publishes
- **THEN** boid entities appear in ENTITY_STATES with position triples and
  their current neighbor relationships

#### Scenario: No cross-snapshot reordering per entity
- **GIVEN** a publisher with `graph_publish_workers` > 1 and consecutive
  snapshots N and N+1 both containing boid B
- **WHEN** both snapshots publish
- **THEN** B's snapshot-N publish is acknowledged before B's snapshot-N+1
  publish is issued

#### Scenario: Fan-out raises the instrument ceiling
- **GIVEN** a dial that saturates the serial publisher (drops counted at
  `graph_publish_workers: 1`)
- **WHEN** the same dial runs with the default worker count
- **THEN** the achieved snapshot rate matches the dial and the drop counter
  stays flat

### Requirement: Snapshot pipeline is observable
The pipeline SHALL expose Prometheus metrics: snapshots published, boid
entities published, snapshots dropped, per-snapshot publish duration, the
current cadence, and the configured worker count — sufficient for a sweep
window to be classified as publisher-bound (drops rising) or not from :9090
alone, and for achieved snapshot/entity rates to be derived without log
scraping.

#### Scenario: Drops are visible
- **WHEN** the dial exceeds what the publisher sustains
- **THEN** the drop counter on :9090 increases while tick metrics stay flat

#### Scenario: Achieved rate is derivable
- **GIVEN** a sweep window at a fixed dial
- **WHEN** :9090 is scraped at the window's start and end
- **THEN** achieved snapshots/s and entities/s over the window follow from
  the published counters
