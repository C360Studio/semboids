# Parallelize Lifecycle Drain

## Why

The churn campaign (`docs/perf/churn-lifecycle-2026-07-06.md`) found the
lifecycle create ceiling at ~150–340/s and *declining* as the flock grows,
with cull reclaim lower still — while the box sits ~92% idle and graph-ingest
has 8 keyed-concurrent lanes waiting (beta.142, ADR-072). The cause is app-side
and fixable now: the sim drains its off-loop lifecycle IO **serially** — one
blocking `Manager.Create` at a time in `createPending`, and one blocking
`graph.mutation.entity.delete` at a time inside the cull-watch goroutine — so
throughput is `1/RTT` and sinks as RTT rises under load. Distinct boids have
distinct entity IDs → distinct lanes, so distinct-boid ops are independent;
dispatching them concurrently lets a churn run actually use the lanes it is
currently leaving idle. This is the app-side complement to the structural
upstream fixes (batch API #498, atomic despawn #497) and does **not** wait on
them.

## What Changes

- The spawn-create drain (`createPending`) dispatches its drained batch through
  a **bounded worker pool** instead of a serial loop — up to `N` in-flight
  `Manager.Create` calls, each still one boid (no batch API upstream yet).
- The cull watcher offloads each `graph.mutation.entity.delete` (and its
  `observeCull`) to the same bounded pool, so the watch goroutine keeps
  draining `ENTITY_STATES` while reclaims are in flight — deletes no longer
  serialize behind the observation loop. Physics removal still stages
  immediately (unchanged), so a slow reclaim never delays the boid leaving the
  flock.
- A config knob `lifecycle_drain_concurrency` (sim config; default **8** to
  match graph-ingest's `ingest_lanes`; clamped ≥ 1, so `1` reproduces today's
  serial path for A/B measurement).
- The churn campaign is re-run at the new ceiling and `docs/perf/` updated;
  metrics are unchanged (`boids_lifecycle_*`, thread-safe Prometheus).

## Capabilities

### New Capabilities

<!-- none: this is a dispatch/throughput change to an existing capability -->

### Modified Capabilities

- `population-control`: add a requirement that the lifecycle create/cull IO
  drains with **bounded, configurable concurrency** across graph-ingest's
  lanes (default = lane count), keeping the churn axis's ceiling
  lane-scaled rather than serial-RTT-bound. The spawn/churn API contract and
  the churn metrics are unchanged; `boid-lifecycle`'s cull *semantics* (a boid
  is reclaimed through the graph) are unchanged — only the dispatch parallelism
  changes.

## Impact

- **Code**: `internal/sim/lifecycle.go` (`createPending`, `runCullWatcher` /
  `deleteEntity` → bounded pool), `internal/sim/component.go` (wire the knob;
  hold the pool/semaphore), `internal/sim/*_test.go` (concurrency + ordering-
  safety unit tests). `golang.org/x/sync/errgroup` promoted from indirect to
  direct dependency.
- **Config**: new optional `lifecycle_drain_concurrency` on the sim component
  (default 8).
- **Perf**: `docs/perf/churn-lifecycle-2026-07-06.md` (or a follow-up note)
  updated with the parallel-drain ceiling and the serial (`=1`) contrast.
- **Substrate**: no new upstream issue — this consumes beta.142's existing
  keyed-concurrent ingest (ADR-072); #497/#498 remain the structural asks.

## Hot-path statement

**This change does NOT touch the physics hot path.** `AddBoids`/`RemoveBoids`
stay staged on the tick loop exactly as today; only the *off-loop* Create/delete
IO (already outside the tick loop) gains bounded concurrency. The tick loop
still never calls NATS, rules, lifecycle, or graph-ingest (ADR-001). The
worker pool and its backpressure live entirely on the spawn-creator and
cull-watch goroutines.

## Non-goals

- **Batching lifecycle ops** — one `Create`/`delete` per boid still, since the
  substrate has no batch API (upstream #498). This change parallelizes N single
  calls; it does not coalesce them.
- **An atomic despawn** — reclaim stays transition-then-delete (upstream #497);
  concurrency does not change the two-round-trip shape of a cull.
- **A population-target controller / churn cap** — unbounded churn growth is a
  separate roadmap item (deferred Non-goal of `add-lifecycle-population`); this
  change only speeds the drain, it does not bound the population.
- **Tuning graph-ingest** (`ingest_lanes`, `max_ack_pending`) — substrate-side,
  out of scope; the app default merely *matches* the lane count.
- **Changing the spawn/churn API or metrics** — the HTTP contract and
  `boids_lifecycle_*` surface are unchanged.
