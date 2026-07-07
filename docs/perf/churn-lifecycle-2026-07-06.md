# Churn Campaign — lifecycle spawn/cull ceiling (beta.142)

Captured 2026-07-06 for `add-lifecycle-population` (task 7.3). This is the
*second* load axis: where the snapshot dial rewrites the same entities
(update churn — ceiling in `melt-campaign-2026-07-05.md`), lifecycle spawn/cull
is **create + transition + delete churn**, a load shape the dial can't produce.
The question: how fast can boids be born and reclaimed *through the graph*, and
how does that ceiling compare to the snapshot update ceiling on the same
graph-ingest write path?

## Setup

Clean single-node NATS (:4222, JetStream, `nats:2.12-alpine`), 12-core
darwin/arm64, semstreams v1.0.0-beta.142. Backend `--pprof` (info logging —
representative, not the DEBUG-throttled path). Lifecycle-focused config
(`scratchpad/churn.json`): 200 seed boids @ 30Hz, one **world-covering
predator zone** (r=5000, every boid inside from tick 1) and
`cull_grace_ticks=30`, so each boid lingers ~1s then is culled — a steady
spawn→(1s)→cull pipeline. `graph_hz=1` (snapshot load kept minimal to isolate
the lifecycle axis). **websocket output disabled** (see Crash below). Rates
read off `:9090` (`boids_lifecycle_*`); population = the `_active` gauge.

Two probes: (1) a single **spawn burst** (`POST /population/spawn {n:5000}`)
to isolate the create-drain rate at bounded population; (2) a **sustained
churn dial** (`PUT /population/churn-hz`) for steady-state spawn+cull.

## Results

**Create ceiling (burst of 5000, serial `Manager.Create` drain):**

| Flock size | Achieved create/s |
|---|---|
| ~700 | 339 |
| ~1,500 | 240 |
| ~2,200 | 211 |
| ~4,000 | ~185 |

**Sustained churn (`churn-hz=60` = 300 requested spawns/s):**

| t | spawns/s | culls/s | live pop |
|---|---|---|---|
| 3s | 297 | 108 | 590 |
| 6s | 305 | 135 | 1,110 |
| 9s | 213 | 67 | 1,573 |
| 12s | 151 | 42 | 1,916 |
| 19s | 149 | 38 | 2,609 |
| 25s | 142 | 38 | 3,266 |

Per-create round-trip: **~1–2 ms unloaded** (seed 200), **5.25 ms mean** across
7,820 creates under churn. graph-ingest `ingest_lag_seconds`: ~56% ≤5ms but a
long tail — ~19% >1s once the backlog forms.

## Findings

1. **Create churn tops out at ~150–340 entity/s and *declines* as the flock
   grows** — the opposite of the snapshot ceiling's rough flatness. Cause is
   the serial single-entity path: `internal/sim/lifecycle.go` `createPending`
   drains the pending-create queue one `Manager.Create` at a time, each a
   synchronous `graph.mutation.entity.create_with_triples` request/reply
   (CAS + retries; `pkg/lifecycle/graph_emit.go`). As the flock grows, the
   per-create RTT rises (1–2ms → ~7ms marginal at 3k boids) under contention
   from physics + the rule processor firing flee/cull for thousands of boids,
   so the serial ceiling `1/RTT` sinks with N.

2. **The cull/delete side is slower still — ~38–135/s — and can't match
   spawns.** A cull is a longer chain than a spawn: sim `lingered` →
   rule `lifecycle_transition` (`update_with_triples`, one RTT) → cull watcher
   observes `phase=culled` → `graph.mutation.entity.delete` (a second RTT).
   Two serial substrate round-trips per reclaim vs one per spawn, plus the
   rule + KV-watch stages between them. Under `churn-hz=60` the flock grew
   unbounded (590 → 3,266 in 25s) because deletes lagged births.
   Side-note: the `runChurn` "bounded by churnCap" comment is aspirational —
   no cap is actually implemented, so churn is a pure growth generator; the
   only brake is predator culls, which here can't keep up.

3. **Lifecycle churn is ~7–15× below the snapshot update ceiling on the *same*
   graph-ingest write path.** The snapshot dial sustains **2,331 entity/s**
   (beta.142, `melt-campaign` addendum) because it is **async + batched**
   (`PublishBatchToStream`, #470) and rides keyed-concurrent ingest (#480 /
   ADR-072). Lifecycle Create/Transition are **synchronous, single-entity
   request/reply** — no batch API — so they never amortize the round-trip the
   way the snapshot publisher does. Same substrate, opposite batching
   discipline: the update axis melts at thousands/s, the create/delete axis at
   hundreds/s. This is the churn-load evidence behind upstream gap #2 (no batch
   `Create`/`Transition`).

4. **Physics isolation holds (ADR-001).** The tick loop stays decoupled: spawn
   deltas stage between ticks and never block on the create backlog — physics
   grew to thousands of boids while creates queued, exactly as designed (the
   backlog lives in `pendingCreate`, not the tick loop). Re-confirms the
   melt-campaign 30fps-through-2000-boids result at the create axis.

## Crash (separate upstream finding)

The first burst run (default config, websocket output enabled) **panicked**:
`panic: concurrent write to websocket connection` in the semstreams websocket
output's `pingClients` goroutine racing the frame-write path
(`output/websocket/websocket.go:1606`, no per-conn write mutex), preceded by
`nats: slow consumer, messages dropped`. Triggered under the ~4k-boid burst.
Distinct from the already-filed #490 (duplicate-metrics panic on restart).
Filed as **semstreams#500**; worked around here by disabling the websocket
output for the measurement. **Fixed in beta.143** (per-conn write mutex
serializing the ping and frame paths) and verified live — the default
websocket-enabled stack now survives the same slow-client + burst load that
crashed it, so the workaround is no longer needed.

## Upstream

- **semstreams#498 (no batch spawn/cull)** — this campaign is the evidence: a
  create/delete axis stuck in the hundreds/s while the batched update axis does
  thousands/s, purely from single-entity request/reply.
- **semstreams#500 (websocket concurrent-write panic)** — new crash bug found
  during this run, filed separately.

## Addendum — parallel drain A/B (2026-07-07, `parallel-lifecycle-drain`)

The serial drain was the app-side half of the ceiling. `createPending` now
dispatches through a bounded pool (`lifecycle_drain_concurrency`, default 8 =
`ingest_lanes`; `1` = the old serial path), and the cull watcher offloads its
deletes the same way. A/B on the **isolated create drain** — one 5,000-boid
spawn burst, `graph_hz=0` and culling off so nothing competes with or offsets
it (this isolation is why the serial number here, ~930/s, is well above the
§Results sustained figures, which ran under `graph_hz=1` + a growing flock):

| `lifecycle_drain_concurrency` | create/s (5,000-boid burst) |
|---|---|
| 1 (serial) | 928 |
| 8 (default) | 1,424 |
| 16 | 1,426 |

**Findings:**

1. **~1.5× from parallelizing, then flat.** 8 lanes lift the drain 928 → 1,424
   create/s; 16 adds nothing (1,426). The wall moved off the app's serial
   dispatch onto the **shared NATS connection + single KV write path** — the
   exact sublinear ceiling beta.142's `melt-campaign` addendum named for #480's
   8-lane ingest. The sim's one client connection contends the same way.
2. **Default 8 is validated:** it reaches the plateau and matches `ingest_lanes`;
   raising it further is wasted. `=1` cleanly reproduces the pre-change path.
3. **Batching (#498), not more concurrency, is the next lever.** Parallelism
   overlaps round-trips but can't reduce their *count*; only a batch
   Create/Transition API cuts N round-trips to 1 and beats the shared-connection
   wall. This A/B is the quantified case for #498.
4. **Physics unaffected:** 30.0 fps at rest and mid-burst (4,000 concurrent
   creates in flight) — the tick loop never touches the pool (ADR-001).
