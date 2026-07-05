# Proposal: load-dial

## Why

The first dial characterization (`docs/perf/graph-dial-first-look.md`) showed
the ~21.6 snapshots/s ceiling at 200 boids is the **instrument's own limit**,
not the substrate's: 200 serial synchronous `PublishToStream` calls per
snapshot at ~231µs ack RTT is 46ms/snapshot = 21.6/s exactly. Graph-ingest
melt is uncharacterized beyond ~4.3k entity publishes/s, which defeats the
project's second purpose — a calibrated load generator. This change makes the
instrument outrun the thing it measures and runs the formal melt campaign.

## What Changes

- **Pipelined snapshot publisher**: the publisher fans out publishes *within*
  one snapshot across a bounded in-flight window (configurable, default
  sized so the instrument ceiling sits well above expected substrate melt).
  Snapshot-at-a-time consumption is retained, so per-boid ordering across
  snapshots is preserved (each boid appears once per snapshot; only
  intra-snapshot parallelism is added). The non-blocking drop-oldest `Offer`
  contract to the tick loop is unchanged.
- **Staged upstream adoption**: when semstreams#470 (async/pipelined
  JetStream publish in `natsclient`) lands, the worker fan-out swaps to the
  substrate API. Tracked as a follow-up task, not a blocker.
- **Ingest-lag telemetry**: substrate-side measurement so instrument ceiling
  and substrate saturation are distinguishable — JetStream consumer lag for
  the graph-ingest consumer (num_pending / ack floor, polled at event rate)
  and end-to-end publish→`ENTITY_STATES`-visible latency (sampled probe
  entities), both exported as Prometheus metrics alongside the existing
  snapshot pipeline metrics.
- **Formal melt campaign**: reproducible dial sweeps (long windows, pprof
  capture, boid-count sweep 200→1000+) driven by Taskfile targets; results
  land in `docs/perf/`; melt points and bottlenecks file upstream as
  SemStreams issues per ADR-001.

## Capabilities

### New Capabilities

- `ingest-telemetry`: substrate-side lag observability for the boid graph
  pipeline — consumer lag and end-to-end ingest latency metrics that let a
  dial sweep attribute saturation to publisher, stream, or ingest/index.

### Modified Capabilities

- `graph-snapshots`: the "Publication is decoupled and drop-oldest"
  requirement changes — publication becomes concurrent within a snapshot
  (bounded in-flight window, ordering semantics stated); the "Snapshot
  pipeline is observable" requirement extends with achieved-rate,
  in-flight/concurrency, and publish-latency metrics needed to correlate
  dial settings with substrate behavior.

## Impact

- **Physics hot path: untouched.** All changes sit on the consumer side of
  the existing bounded snapshot channel or in new telemetry that consumes
  NATS/KV at event rates. No new NATS, KV, rule, or graph traffic on the
  tick path; `Offer` semantics are byte-identical.
- **Code**: `internal/boidgraph` (publisher fan-out + metrics), a new
  telemetry probe (consumer-lag poller + latency sampler; likely
  `internal/boidgraph` or a sibling package), config surface for publish
  concurrency, Taskfile campaign targets. Possible small `internal/api`
  addition (achieved-rate readback next to the existing `PUT
  /boids/graph/hz` dial) — design decides.
- **Upstream**: semstreams#470 filed (async publish — adoption staged);
  semstreams#471 filed (ws-output re-encode — affects profile
  interpretation during sweeps, not this change's code). If graph-ingest
  turns out not to expose per-message processing latency, that gap files
  upstream too — the app-side probe measures end-to-end only.
- **Docs**: campaign results in `docs/perf/`; `graph-dial-first-look.md`
  already carries the instrument-ceiling attribution.

## Non-goals

- **No substrate reimplementation**: the worker fan-out composes the public
  sync `PublishToStream` API app-side; it does not re-create an async
  publish client — that is semstreams#470's job, and we swap to it when it
  ships.
- **No app-side fixes for substrate melt**: whatever the campaign finds in
  graph-ingest/graph-index/clustering gets filed as SemStreams issues, not
  patched or worked around here.
- **No new ADR**: the dial stays a dial; per-tick state stays off the
  substrate. ADR-001's supersede trigger (moving per-tick physics into the
  substrate) is not touched by a faster publisher.
- **No UI work**: the graph pane and dial API already exist; the campaign is
  backend + metrics + docs.
- **Not blocked on #470**: the app-side fan-out delivers the needed
  instrument headroom now; upstream adoption is a cleanup swap.
