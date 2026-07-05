# ingest-telemetry Delta — load-dial

## ADDED Requirements

### Requirement: JetStream consumer lag is exported
The host SHALL construct its NATS client with the substrate's JetStream
metrics enabled (`natsclient.WithMetrics` against the host metrics
registry), so stream and consumer gauges — notably
`consumer_pending_messages` for the consumer graph-ingest reads — are
exported on :9090 for every stream and consumer created through the client.
Lag observability SHALL come from this substrate facility, not an app-side
poller.

#### Scenario: Ingest-bound saturation is visible
- **GIVEN** a dial beyond ingest capacity but within the publisher's
  ceiling
- **WHEN** a sweep window elapses
- **THEN** `consumer_pending_messages` for the graph-ingest consumer grows
  monotonically while the snapshot drop counter stays flat

#### Scenario: Healthy load shows a flat backlog
- **GIVEN** a dial well within ingest capacity
- **WHEN** a sweep window elapses
- **THEN** `consumer_pending_messages` stays near zero across polls

### Requirement: End-to-end ingest latency is measured
While the graph dial is active, a probe SHALL watch ENTITY_STATES for boid
entities and record `observation time − observed_at` (the entity's embedded
snapshot timestamp) into a Prometheus histogram
(`boids_graph_e2e_latency_seconds`), sampling 1-in-N updates (configurable,
default 10) to bound probe cost at saturation rates. The probe SHALL run
independently of any UI or SSE clients.

#### Scenario: Latency reflects backlog
- **GIVEN** a dial that grows the ingest backlog
- **WHEN** the window elapses
- **THEN** the latency histogram's upper quantiles rise alongside
  `consumer_pending_messages`

#### Scenario: Probe needs no browser
- **GIVEN** a running flow with an active dial and zero connected UI
  clients
- **WHEN** snapshots publish
- **THEN** the latency histogram still populates

### Requirement: Saturation source is attributable from exported metrics
For any sweep window, the metric set on :9090 SHALL suffice to classify the
window as publisher-bound (instrument ceiling), ingest-bound (substrate
melt), downstream-lag, or rejection-loss — without log scraping: snapshot
drops distinguish the publisher, `consumer_pending_messages` distinguishes
ingest, the e2e latency histogram distinguishes downstream consumers, and
graph-ingest's `entities_updated_total` / `mutation_rejections_total`
rates distinguish loss.

#### Scenario: Instrument ceiling is not misread as melt
- **GIVEN** a window where the snapshot drop counter rises while
  `consumer_pending_messages` stays flat
- **WHEN** the window is classified
- **THEN** it is publisher-bound — invalid as a melt observation — and the
  sweep raises workers rather than recording a melt point

#### Scenario: Melt is evidenced, not inferred
- **GIVEN** a window where drops stay flat and `consumer_pending_messages`
  grows for the full window
- **WHEN** the window is classified
- **THEN** it is ingest-bound and the melt point is recorded with the
  window's metrics and a pprof capture

### Requirement: Campaign sweeps are reproducible
A Taskfile target SHALL run one sweep point — given a target boid count and
dial Hz, it applies the dial to a running stack, waits a warm-up period,
holds a measurement window (60–120s), scrapes :9090 at the window
boundaries, and emits a per-window summary suitable for the campaign
results document in `docs/perf/`.

#### Scenario: One command per sweep point
- **GIVEN** a running stack configured at the target boid count
- **WHEN** the sweep task runs with a dial value
- **THEN** it sets the dial, warms up, measures a full window, and emits
  the window summary with its classification inputs
