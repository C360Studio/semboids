# Design: load-dial

## Context

The first dial characterization (`docs/perf/graph-dial-first-look.md`) hit
~21.6 snapshots/s at 200 boids and post-hoc analysis attributed the ceiling
to the instrument: `internal/boidgraph/publisher.go` issues 200 serial
synchronous `PublishToStream` calls per snapshot, and each blocks on the
JetStream PubAck (~231µs RTT locally) → 46ms/snapshot. Substrate saturation
(graph-ingest → ENTITY_STATES → graph-index/clustering) was never reached.

What the substrate already gives us (verified against beta.137 source):

- **natsclient JetStream metrics** (`natsclient/jetstream_metrics.go`):
  `consumer_pending_messages`, `consumer_delivered_total`,
  `consumer_acked_total`, `consumer_redelivered_total`, `stream_messages`,
  `stream_bytes` — polled every 30s (hardcoded) for streams/consumers
  created through the client. **Not currently exported by semboids**: our
  host builds the client without `natsclient.WithMetrics` (the registry is
  created after the client in `cmd/semboids/main.go`).
- **graph-ingest metrics** (`processor/graph-ingest/component.go`):
  throughput counters (`entities_updated_total`, `mutation_rejections_total`,
  …) but **no per-message processing-duration histogram**.
- **Our own pipeline metrics** (graph-snapshots spec): snapshots published /
  dropped, publish duration, current cadence — already on :9090.
- Our boid `Entity` payload carries `observed_at` (snapshot derivation
  time), so end-to-end latency is computable at any downstream observation
  point without payload changes.

Upstream: semstreams#470 (async/pipelined publish) is filed and may land
during this change; #471 (ws-output re-encode) affects profile
interpretation only.

## Goals / Non-Goals

**Goals:**

- Raise the instrument ceiling far above any plausible substrate melt point
  (target: ≥10× the highest sweep dial × boid count).
- Make saturation attributable from :9090 alone: publisher-bound vs
  stream-bound vs ingest-bound must be distinguishable per sweep window.
- A reproducible campaign: one Taskfile invocation per sweep point, results
  as `docs/perf/` documents, melt findings filed upstream with evidence.

**Non-Goals:**

- Reimplementing async publish inside the app (that is #470; we compose the
  public sync API until it ships).
- Fixing whatever melts (files upstream per ADR-001).
- New UI, new ADR, dashboard tooling (raw Prometheus + pprof suffice).

## Decisions

### D1: Intra-snapshot async batch publish

`publishSnapshot` marshals every boid entity, then dispatches the whole
snapshot as one async batch via semstreams `PublishBatchToStream` (gh#470),
which pipelines the publishes past the per-ack RTT ceiling and joins on
every ack before returning. The coordinator goroutine keeps everything
stateful: it reads/writes `prevHadNeighbors` (single-goroutine, no locking)
and issues neighbor-empty removals after the batch joins.

- Ceiling: the batch dispatches all N boids without paying N× the ack RTT,
  so the instrument achieves the dial (measured: 30Hz × 200 boids = 30/30
  snapshots/s, 0 drops — the old serial path capped at 21.6/s). The
  bottleneck moves entirely to the substrate.
- Ordering: snapshots remain strictly sequential (the `Run` loop consumes
  one at a time and `publishSnapshot` joins before the next); each boid
  appears once per snapshot and all publish to one subject (in-order per
  connection) — so no same-entity reordering is possible. This invariant
  gets a test.
- Alternatives rejected: goroutine-per-entity (unbounded connection
  contention); N parallel snapshot consumers (breaks same-entity ordering);
  an app-side errgroup worker pool (an interim carried only while #470 was
  unlanded — see D2).

### D2: #470 landed in beta.138 — adopted, not deferred

The interim design shipped an app-side `errgroup` worker pool behind the
`StreamPublisher` interface as headroom while #470 was open. #470 landed in
semstreams beta.138 mid-change, so the pool was collapsed to
`PublishBatchToStream` at the same seam: `Offer`, ordering, and drop/latency
metrics contracts are byte-identical; only the fan-out mechanism changed.
`graph_publish_workers` was dropped (no configurable async in-flight window
to repurpose it to, and the field never shipped).

### D3: Consumer lag comes from natsclient's existing metrics — wire, don't build

Reorder `cmd/semboids/main.go` to create the `MetricsRegistry` before
`connectToNATS` and pass `natsclient.WithMetrics(registry)`. That turns on
the whole JetStream family — `consumer_pending_messages` for the
graph-ingest consumer is the lag signal — with zero new code. No app-side
jsz poller (that would reimplement substrate observability).

- The 30s poll interval is hardcoded upstream; campaign windows are 60–120s
  (D5) so each window spans ≥2 samples. If finer resolution proves
  necessary, the ask is a `WithMetricsInterval` option upstream — a
  one-line enhancement, filed with evidence.

### D4: End-to-end latency via a sampled ENTITY_STATES watch probe

A small always-on watcher (lives in `internal/boidgraph`, started with the
publisher when the dial is active) watches ENTITY_STATES for boid entities,
parses `observed_at`, and records `now − observed_at` into a Prometheus
histogram (`boids_graph_e2e_latency_seconds`), sampling 1-in-N (default
N=10) to bound cost at melt rates.

- Same-process, same-host wall clock — no skew concern at this stage.
- Deliberately separate from the SSE bridge's watch (that one serves UI
  clients; the probe must run regardless of browsers attached).
- Alternative rejected: an ingest-side processing histogram would separate
  queueing from processing, but graph-ingest doesn't export one (verified).
  The e2e number plus D3's pending gauge is sufficient for attribution
  (D5); the upstream histogram is a candidate enhancement filed after the
  first campaign provides evidence.

### D5: Melt definition and attribution matrix

A sweep point = (boid count, dial Hz) held for a 60–120s window after a 30s
warm-up. Per window, scrape :9090 and classify:

| Signal | Meaning |
|---|---|
| `snapshots_dropped` rising, pending flat | **Publisher-bound** (instrument ceiling — invalid window, raise workers) |
| Drops flat, `consumer_pending_messages` growing monotonically | **Ingest-bound** (substrate melt — capture pprof, file upstream) |
| Pending flat, e2e p99 rising | **Downstream lag** (index/clustering — check their consumers) |
| `entities_updated_total` rate < publish rate, pending flat | **Loss/rejection** (check `mutation_rejections_total`) |

Melt point = the first dial step in a boid-count row where the ingest-bound
signature is sustained for a full window. Grid: boids {200, 500, 1000,
2000} × dial {1, 5, 10, 30, 60, 90} Hz, walked until melt per row. pprof
(30s CPU) captured at each row's melt candidate and one step below.
Campaign driven by a Taskfile target per sweep point; results in
`docs/perf/melt-campaign-<date>.md`.

### D6: No API readback endpoint

The campaign scrapes :9090; adding achieved-rate readback to the boids API
would duplicate that surface. `PUT /boids/graph/hz` stays the only dial
control. (Decided against, not deferred.)

## Risks / Trade-offs

- [Fan-out invariant depends on one-snapshot-at-a-time consumption] → the
  ordering test in D1 pins it; a comment on `Run` marks the invariant so a
  future "pipeline snapshots" change can't drift in silently.
- [Watch-probe lag under melt: at saturation the KV watcher itself may fall
  behind, inflating measured e2e latency] → acceptable and honest — watcher
  lag *is* downstream-consumer experience; report p50 alongside p99 and
  note the effect in the campaign doc.
- [30s metric poll granularity] → 60–120s windows; upstream interval option
  only if data shows we need it.
- [Publish-side contention: async in-flight window] → `PublishBatchToStream`
  uses jetstream-go's default max-pending; at these sizes the stream ack
  path is nowhere near limiting (the melt is on the ingest consumer, not the
  publish path — see the campaign). If the async window ever bounds the
  instrument, a `WithPublishAsyncMaxPending` option is the upstream ask.
- [Boid counts ≥1000 change physics cost too (spatial hash density)] → the
  baseline profile showed physics at ~0.1% of a core at 200; capture tick
  duration metrics per window so physics cost is visible, and treat physics
  fps < 30 as an invalid window, not a melt signal.

## Migration Plan

Additive rollout: the async batch publisher, metrics wiring, and the probe
require no config or data migration. The interim `graph_publish_workers`
field never shipped (introduced and removed within this change), so its
removal breaks nothing. Rollback is the git revert of the publisher swap.

## Open Questions

- Probe sampling default (1-in-10) is a guess — validate during bring-up
  that histogram cost is negligible at the melt point, adjust N if not.
- Whether to file the graph-ingest processing-histogram enhancement before
  or after the first campaign — leaning after, so the issue carries sweep
  evidence (house style).
