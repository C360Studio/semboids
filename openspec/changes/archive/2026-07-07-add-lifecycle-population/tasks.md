# Tasks — add-lifecycle-population (minimal slice)

> Minimal vertical slice: predator cull + spawn wave + churn dial. Full
> population-target API, rich UI, and wave provenance are Non-goals (proposal).
> Substrate: semstreams beta.142 (keyed-concurrent ingest fixed #480 ~3.5×; the
> lifecycle despawn/batch/watch-delete/O(N)-lookup gaps stand — 7.4).

## 1. Boid lifecycle workflow + host wiring

- [x] 1.1 `BoidLifecycle` participant (co-located with `BoidEntityID` in
      `internal/boidgraph`): `ID string` (`lifecycle:"id"`, the 6-part boid
      ID), `Phase string` (`lifecycle:"phase,predicate=flock.lifecycle.phase"`);
      a `flock.boid` `lifecycle.Workflow` (`Transitions{active:{culled,expired},
      culled:{}, expired:{}}`, `EntityIDPattern`, `Schema`). Unit test:
      `Transitions.Validate()` passes; participant satisfies the interface.
- [x] 1.2 Wire one `lifecycle.NewManager(natsClient, logger)` in
      `cmd/semboids`, `Register` the boid workflow, assign
      `svcDeps.LifecycleManager` (semdragons pattern) — reaches the rule
      processor and sim via `Dependencies`.

## 2. Engine population (`internal/flock`) — TDD

- [x] 2.1 Failing tests: monotonic `nextID` never reused; `AddBoids(n)` returns
      `n` fresh IDs and grows `Boids()` next tick; `RemoveBoids(ids)` shrinks
      without disturbing survivors' identity/position; grid rebuilds; a run
      with no population change stays bit-identical (determinism preserved).
- [x] 2.2 Implement `AddBoids`/`RemoveBoids` + the ID allocator; document the
      between-ticks application contract.

## 3. Sim staging, cull observation, reclaim (`internal/sim`) — TDD

- [x] 3.1 Failing tests: a `populationState` (mirroring `steeringState`) stages
      add/remove off-loop under a mutex and the tick loop drains it once per
      tick *before* `Tick()`; staged deltas apply between ticks; the staging
      path is untouched when idle (determinism).
- [x] 3.2 Cull watcher: `ENTITY_STATES` watch (reuse `watchBucket`) filtered to
      boid keys → on `flock.lifecycle.phase == culled`, stage removal and fire
      `graph.mutation.entity.delete`. Unit-test the pure observe/decode split;
      lifecycle observed via KV watch (not `Manager.Watch` — it skips deletes).
- [x] 3.3 Lingered emission: the zone tracker counts per-boid dwell ticks and
      emits `boids.zone.lingered` (`entity_id`, `zone_id`) past
      `cull_grace_ticks`. Test: fires only past grace; never for a boid that
      exits first.
- [x] 3.4 Spawn orchestration: a spawn wave stages `n` adds AND calls
      `Manager.Create` (active) per new boid; a churn ticker fires waves at the
      dial rate. Test spawn stages adds + `Create` is invoked per boid.

## 4. Predator cull rule (`configs/rules/zone-steering`)

- [x] 4.1 `predator-cull` rule: conditions `event == lingered` &&
      `zone_type == predator`, action `lifecycle_transition → culled` on the
      trigger `entity_id`; ships **enabled** (the engine skips disabled rules
      at load — `rule_loader.go:124` — so it must load to be toggleable),
      gated by flee + `cull_grace_ticks`; toggle via a new `cull` kind in
      `internal/api`.
- [x] 4.2 Integration (`-tags=integration`, testcontainer): boid lingers in a
      predator zone → rule fires → `phase=culled` lands in `ENTITY_STATES` →
      the sim removes it from physics and deletes the entity.
      (`internal/sim/cull_integration_test.go`: full-chain + disabled control.)

## 5. Population API + metrics (`internal/api`)

- [x] 5.1 `POST /boids/population/spawn {n}` → sim spawn hook;
      `PUT /boids/population/churn-hz {hz}` → sim churn ticker (clamped ≥ 0; 0
      disables). `OpenAPISpec` entries so the handlers mount.
- [x] 5.2 Metrics on :9090: `boids_lifecycle_spawns_total`,
      `boids_lifecycle_culls_total`, `boids_lifecycle_active` gauge, spawn/cull
      publish-duration histogram — a churn sweep attributable from :9090.

## 6. UI (`ui/`)

- [x] 6.1 Live boid-count readout (from the frame already streamed) + a
      spawn-wave button (`POST /boids/population/spawn`). eslint / svelte-check
      / vitest green.

## 7. Verify + churn characterization + upstream

- [x] 7.1 `task check:push` green (lint, race unit + integration,
      cross-compile) + the UI workflow.
- [x] 7.2 Live-verified 2026-07-06: predator cull is visible (a lingering boid dies *through the
      graph*); a spawn wave repopulates; the churn dial holds a rate; physics
      holds 30fps under churn (staged deltas never block the tick).
- [x] 7.3 Churn load run: crank `churn-hz`, characterize the create/delete
      ceiling on beta.142's keyed-concurrent ingest and contrast with the
      snapshot dial's update ceiling; short note in `docs/perf/`.
      (`docs/perf/churn-lifecycle-2026-07-06.md`: create ~150–340/s, cull
      ~40–135/s, both ~7–15× below the 2,331/s batched snapshot ceiling.)
- [x] 7.4 File upstream the lifecycle gaps re-verified in beta.142, each with
      this slice as the repro: (1) no `Manager` despawn primitive +
      `Manager.Watch` skips `KeyValueDelete`/`Purge` → **semstreams#497**;
      (2) no batch `Create`/`Transition` (one graph-ingest round-trip per
      entity), with churn evidence → **semstreams#498**; (3) the
      `$entity.lifecycle.*` O(N)-scan gap was already CLOSED in beta.142
      (`LookupByEntityID` is O(workflows)+direct-key Get) — the finding is a
      stale doc comment → **semstreams#499**. Bonus crash found in the churn
      run: websocket concurrent-write panic → **semstreams#500**.
- [x] 7.5 `openspec validate add-lifecycle-population --strict`; README
      status/roadmap update; archive the change.
