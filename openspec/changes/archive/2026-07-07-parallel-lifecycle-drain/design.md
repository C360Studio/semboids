# Design: parallel-lifecycle-drain

## Context

The sim's off-loop lifecycle IO is serial today:

- `createPending` (`internal/sim/lifecycle.go`) drains the `pendingCreate`
  queue and calls `Manager.Create` **one boid at a time**, each a blocking
  `graph.mutation.entity.create_with_triples` request/reply.
- `runCullWatcher` observes `phase=culled` on the `ENTITY_STATES` watch and
  calls `deleteEntity` **inline in the watch goroutine** — the next culled
  entity can't be observed until the current delete's request/reply returns.

Measured ceiling (`docs/perf/churn-lifecycle-2026-07-06.md`): create ~150–340/s
declining with N; cull lower. graph-ingest is keyed-concurrent (beta.142,
ADR-072, `ingest_lanes` default 8: same entity ID → one lane, different IDs →
parallel), and the box is ~92% idle. The serial drain is the app-side wall.
Constraint (ADR-001): the physics tick loop must stay off NATS/rules/lifecycle
— only the *already-off-loop* Create/delete dispatch changes here.

## Goals / Non-Goals

**Goals:** run distinct-boid Create/delete calls concurrently across the lanes;
a config knob that includes a serial (`=1`) baseline for A/B; no change to
physics, determinism, the spawn/churn API, or the metric surface.

**Non-Goals:** batching (no upstream batch API — #498); atomic despawn (#497);
a churn cap / population target; reading the substrate's actual lane count.

## Decisions

### D1: One shared bounded worker pool, not two mechanisms

A single `drainPool` — a counting semaphore (`chan struct{}` of size N) plus a
`sync.WaitGroup` — with a `submit(ctx, func())` that acquires a slot (blocking
when full), runs the closure in a goroutine, and releases. Both paths submit to
it: `createPending` submits each `Manager.Create`; the cull watcher submits each
`deleteEntity`+`observeCull`.

- **Why shared, size N = lane count:** creates and deletes both hash by entity
  ID onto graph-ingest's N lanes. A shared budget of N matches the substrate's
  real parallelism; two independent N-budgets could offer 2N and over-subscribe
  the lanes during simultaneous spawn+cull churn.
- **Why a semaphore pool over `errgroup.SetLimit`:** `errgroup`'s `Wait()` fits
  a finite batch (the create drain) but not the *continuous* cull-watch stream;
  one semaphore pool serves both uniformly, and `submit` blocking on a full
  semaphore is exactly the backpressure we want. (`x/sync` is still promoted to
  a direct dep — the pool is ~20 lines, no errgroup needed.)

### D2: `lifecycle_drain_concurrency`, default 8, clamp ≥ 1

New optional sim-config int. Default **8** = graph-ingest's `ingest_lanes`
default (ADR-072). `1` reproduces today's serial path (the A/B baseline for the
perf note); values < 1 clamp to 1. The sim cannot read the substrate's actual
lane config, so this is a documented default operators override to match a
tuned `ingest_lanes`.

### D3: Ordering is safe without extra coordination

- **Distinct boids are independent** (distinct entity IDs → distinct lanes).
- **Same-boid create-then-delete cannot race:** a boid must be `Create`d
  (`active`, landed in `ENTITY_STATES`) before the *rule* can transition it to
  `culled`; the sim's watcher only observes `culled` — and thus only submits a
  delete — *after* that create has landed. So a delete is always causally after
  its create completes; there's no in-flight create for the same ID when its
  delete is submitted. graph-ingest additionally serializes same-key ops.
- The cull watcher's `seen` dedup map stays owned by the single watch goroutine
  (only the delete closure is offloaded), so no lock is added there.
- Prometheus counters/histograms are goroutine-safe; `observeSpawn`/
  `observeCull` move into the worker closures unchanged.

### D4: Backpressure keeps goroutines bounded

`submit` blocks its caller when N are in flight, so a 5,000-boid spawn burst
runs N-at-a-time (never 5,000 goroutines); the untouched `pendingCreate` slice
holds the backlog, and the poll goroutine simply blocks in `submit`. The cull
watcher blocks in `submit` when reclaims saturate the pool — the ENTITY_STATES
watch buffers upstream, same drop-nothing behavior as today, just deeper.
Physics removal (`stageRemoval`) stays **synchronous** in the watch loop before
the delete is offloaded, so a slow reclaim never delays a boid leaving physics.

### D5: Shutdown joins in-flight work

The pool binds to the component context. On `Stop`, ctx cancels: `submit`
stops accepting, in-flight `Request`s either finish or fail on the cancelled
ctx (logged, not fatal), and `pool.wait()` joins them within the component's
Stop timeout. Same lifecycle as the existing spawn-creator/cull-watch
goroutines.

## Risks / Trade-offs

- **Shared budget contention (create vs cull compete for N slots)** → default N
  = lane count is the principled cap; operators raise `lifecycle_drain_
  concurrency` if they tune `ingest_lanes` up. Contention only means churn
  self-limits to the substrate's parallelism — the intended behavior.
- **Concurrency exposes a latent CAS/ordering bug in the substrate** → the
  same-key serialization (D3) makes this unlikely; if a `revision_mismatch`
  storm appears under load it's a finding for #480's lane path, surfaced via
  the existing spawn/cull duration histograms, not hidden.
- **More goroutines under churn** → bounded to N by the semaphore (D4); the
  race detector in `check:push` guards the new sharing.

## Migration Plan

Additive. New config defaults to 8 → faster out of the box; `=1` restores the
serial path with no code change (rollback lever + A/B baseline). No data
migration; `git revert` is a clean rollback. Re-run the churn campaign at
`concurrency ∈ {1, 8}` and update `docs/perf/`.

## Open Questions

- Final default: 8 (match lanes) vs a small multiple. Bring-up call from the
  A/B numbers — if 8 leaves the box idle and RTT-bound, try 16 and note whether
  the shared NATS connection (the #480 sublinear wall) caps it first.
