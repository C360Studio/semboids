# Design: add-lifecycle-population (minimal slice)

## Context

`pkg/lifecycle` (beta.142) is a schema-and-discipline layer over the single
`ENTITY_STATES` KV bucket — it owns no bucket or stream of its own (ADR-049).
beta.142's keyed-concurrent ingest (ADR-072, gh#480) speeds the *shared write
path* these operations ride (~3.5× — see `docs/perf/melt-campaign-2026-07-05.md`
addendum) but leaves the lifecycle Manager's surface unchanged; the three gaps
below were re-verified still-open in beta.142. What the substrate gives us
(verified in source):

- **Participant** = a plain struct with `lifecycle:"id"` and
  `lifecycle:"phase,predicate=…"` tags implementing `EntityID/Workflow/Phase/
  IsTerminal/ParentEntityID` (`pkg/lifecycle/participant.go:29`,
  `tags.go:215`). State is projected to **triples**, not stored as JSON.
- **Workflow** is a struct literal (no constructor) with `Transitions`
  (`map[string][]string`, terminal = empty slice), `PhasePredicate`, and
  `Schema reflect.Type` (`workflow.go:28`, `transitions.go:28`). Registered on
  `Manager` (`manager.go:139`); `NewManager(client, logger)` takes only those
  two deps (`manager.go:99`).
- **Create/Transition/Complete** each become a **NATS request/reply to
  graph-ingest** (`graph.mutation.entity.{create,update}_with_triples`) with
  CAS + up-to-5 retries (`graph_emit.go:125`, `manager.go:571`). One entity
  per call — no batch API.
- **No despawn primitive.** `Complete`/`Fail` only move to a terminal phase;
  the entity persists in `ENTITY_STATES`. Deleting it needs a separate
  `graph.mutation.entity.delete` (`processor/graph-ingest/mutations.go:67`,
  `graph.DeleteEntityRequest`).
- **Observation** = watch `ENTITY_STATES`. `Manager.Watch` re-projects each
  write into a `Participant` but **skips delete ops** (`manager_query.go:137`).
- **Rule actions**: `lifecycle_transition|complete|fail` on the shared
  `Action` struct (`workflow`/`phase`/`reason`/`set`), resolved through a
  narrowed `LifecycleManager` interface that **excludes raw `Transition`**
  (`processor/rule/actions.go:495`). `$entity.lifecycle.phase` conditions
  exist but cost O(workflows × bucket-size) per fire (`lifecycle_substitution.go:14`).

## Goals / Non-Goals

**Goals:** exercise the full rule→lifecycle→graph→physics causal chain for one
per-boid workflow; make spawn/cull churn a second, orthogonal load axis;
surface the substrate gaps with evidence.

**Non-Goals:** population-target management, rich UI, wave provenance (Non-goals
in the proposal); fixing the substrate gaps app-side.

## Decisions

### D1: Boid is a per-boid lifecycle Participant, phase-only

`BoidLifecycle` struct: `ID string` (`lifecycle:"id"`) = the existing 6-part
`BoidEntityID(org,platform,id)` (`internal/boidgraph`); `Phase string`
(`lifecycle:"phase,predicate=flock.lifecycle.phase"`). Workflow `flock.boid`,
`Transitions{ "active": {"culled","expired"}, "culled": {}, "expired": {} }`,
`EntityIDPattern` matching the boid ID glob. No operator-writable fields in the
slice (cull is a phase move, no payload). Registered once in `cmd/semboids`
beside the existing payload registration.

- The phase triple `flock.lifecycle.phase` lands in the SAME `ENTITY_STATES`
  entity as the boid's position/neighbor triples — the lifecycle dimension is
  additive to the graph node already flowing at the snapshot dial.
- Alternatives rejected: a separate lifecycle payload/bucket (ADR-049 says
  don't); wave-level workflow (defer — Non-goal).

### D2: One Manager in the host, wired the semdragons way

`cmd/semboids`: `mgr := lifecycle.NewManager(natsClient, logger);
mgr.Register(boidgraph.BoidWorkflow()); svcDeps.LifecycleManager = mgr` — the
framework fans it to the rule processor (which narrows it for
`lifecycle_transition`) and to the sim component via `Dependencies`. No
`AttachOwnership` (ownership stays no-op, as in semdragon).

### D3: Cull loop — sim states the fact, the rule decides, the sim enacts

The value is *rules decide existence*, so the rule must be the decider, but the
sim owns the spatial truth. Split:

1. **Sim → fact.** The zone tracker already knows membership; extend it to
   count dwell ticks and, when a boid exceeds `cull_grace_ticks` in a predator
   zone, publish a lingered event (`event="lingered"`, `entity_id`, `zone_id`)
   on the existing `boids.zone.events` stream — the same edge-triggered shape
   as transitions (one per boid per crossing), so the rule processor consumes
   it with zero new wiring.
2. **Rule → decision.** A `predator-cull` rule (conditions
   `event == lingered`, `zone_type == predator`) fires
   `lifecycle_transition → culled` on the trigger `entity_id`.
3. **Sim → enact.** The sim runs an always-on `ENTITY_STATES` watcher (the
   probe/SSE `watchBucket` pattern) filtered to boid keys; on seeing
   `flock.lifecycle.phase == culled` it **stages the boid's removal** (D4) and
   fires `graph.mutation.entity.delete` for that entity (D6).

- **Why the pure watch path** (not a direct `publish` cull directive to the
  sim): one mechanism, and it makes the demo honest — the boid dies *through
  the graph*, so under a churn-melt the cull visibly lags (a feature: you see
  the substrate's backpressure in the flock). At demo churn the round-trip is
  tens of ms. Documented trade-off, not a bug.
- Cull grace via a sim-side tick counter (not a stateful rule) keeps the rule
  stateless and avoids the JetStream-backed stateful-eval path.

### D4: Engine population between ticks, staged like modifiers

`flock.Engine` gains a monotonic `nextID uint32` allocator and
`AddBoids(n int) []uint32` / `RemoveBoids(ids []uint32)`. `internal/sim` grows
a `populationState` mirroring `steeringState`: off-loop goroutines (spawn API,
cull watcher) `stage()` add/remove deltas under a mutex; `run()` drains them
once per tick *before* `engine.Tick()`, exactly where staged modifiers apply.
The grid rebuilds from the boids slice every tick, so a changed N is free in
the hot path. Fixed-population runs never touch the staging path →
determinism unchanged.

### D5: Spawn/churn is the second load axis

- `POST /boids/population/spawn {n}` → sim stages `n` adds AND calls
  `Manager.Create` for each new boid (active) — the spawn-churn generator.
- `PUT /boids/population/churn-hz {hz}` → a sim ticker firing spawn waves at
  `hz` (paired with cull despawn) to hold a steady churn rate for load runs —
  the dial `task sweep`'s successor can crank. Mirrors the graph dial's
  runtime-adjustable, clamped design (D6 of load-dial: control via API, read
  via :9090).
- New metrics: `boids_lifecycle_spawns_total`, `culls_total`,
  `active_boids` gauge, spawn/cull publish-duration — so a churn sweep is
  attributable from :9090 like the snapshot pipeline.

### D6: Reclaim is app-side and honest about the gap

Lifecycle won't delete, so the cull watcher fires
`graph.mutation.entity.delete` after staging removal. This is the *minimal*
app-side code the slice needs to not leak entities — not a workaround for the
gap but the gap's only current remedy. Filed upstream (Impact §): the substrate
lacks an atomic transition-then-delete / despawn on `Manager`, and
`Manager.Watch` can't observe the delete we issue.

## Risks / Trade-offs

- **Cull latency rides the shared ingest write path.** Under a churn-melt,
  `culled` triples (and the deletes) queue behind graph-ingest's ceiling — now
  ~2.3k/s after beta.142's keyed-concurrent fix (was ~500/s), so the melt is
  ~3.5× harder to induce but not gone → past it the flock keeps flying dead
  boids until the backlog drains. Illustrative for the demo; measured via the
  new substrate `ingest_lag_seconds` metric alongside the app-side e2e probe,
  not hidden.
- **O(N) rule lifecycle lookups.** The `predator-cull` rule keys off the
  `lingered` event fields, NOT `$entity.lifecycle.*`, specifically to dodge the
  per-fire full-bucket scan. If a future rule needs the phase condition, that
  scan is the finding.
- **Entity accumulation if a delete fails.** Deletes are best-effort
  request/reply; a dropped delete leaks a `culled` entity. The `active_boids`
  gauge vs `ENTITY_STATES` count surfaces drift; retry is out of slice scope.
- **Re-spawn ID reuse collides.** `Create` on an existing entity returns
  `ErrAlreadyExists`; the monotonic allocator never reuses an ID within a run,
  so a reused ID can only occur across restarts (fresh bucket) — safe.

## Migration / Rollout

Additive: the workflow, Manager, endpoints, and metrics are new; the cull rule
is disabled-by-default in config (toggle like the other zone rules). With churn
at 0 and the cull rule off, behavior is byte-identical to today. Rollback = the
git revert; no data migration.

## Open Questions

- `cull_grace_ticks` default — long enough that the flee modifier usually saves
  the boid (cull is the exception, not the rule), tuned during bring-up.
- Whether spawn waves should reuse the seeded RNG for placement (determinism)
  or spawn at zone edges for visual drama — bring-up call.
