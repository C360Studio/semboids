# Add Lifecycle Population

## Why

Population is still fixed at start — `pkg/lifecycle` (the last ADR-001
substrate pillar) is unproven here, and the demo can't show births, deaths,
or consequences. Two proven values motivate closing this, both established by
prior work:

1. **A new load axis.** Everything semboids stresses today is the *update*
   path — the same entities rewritten at a dial (that path's ceiling is
   semstreams#480). Lifecycle spawn/cull is *create + delete + audit-triple
   churn*, a load shape the snapshot dial can't produce. Research already
   surfaced three gaps in that path (below) — the fixture pointed at an
   untried pattern.
2. **The rules-decide-existence story.** Zone-steering shows rules that
   *influence* (steering modifiers). This shows rules that *decide existence*
   (cull) — completing the "rules drive an emergent world" narrative.

This is a **minimal vertical slice**: predator cull + spawn wave wired through
the lifecycle Manager and rule actions, with a spawn/churn dial as a second
load axis. The full population-target API, rich UI, and wave-provenance are
explicitly deferred (Non-goals) until the slice proves the churn story.

## What Changes

- **Boid lifecycle workflow** (`boid-lifecycle`): a `lifecycle.Workflow`
  (phases `active → culled | expired`; terminal `culled`/`expired`) with a
  reflected `BoidState` participant (6-part entity IDs, `lifecycle:"id"` +
  `lifecycle:"phase"` tags), registered on one `lifecycle.NewManager(natsClient,
  logger)` in `cmd/semboids` (semdragons wiring) and passed via
  `Dependencies.LifecycleManager`. Spawn calls `Manager.Create`.
- **Predator cull** (`zone-steering`): the sim already tracks zone membership,
  so it emits a `boids.zone.lingered` fact (boid_id, zone_id) when a boid
  dwells in a predator zone past a grace window. A new rule decides the
  consequence — `lifecycle_transition` to `culled` (the rule engine's
  narrowed `LifecycleManager`, not raw `Transition`). Sim provides the spatial
  fact; the rule owns the existence decision.
- **Sim observation + reclaim**: the sim watches `ENTITY_STATES` for boid
  entities reaching `phase=culled` (reusing the probe/SSE watch pattern) and
  stages the boid's physics removal. Because lifecycle has **no despawn
  primitive**, the sim then fires `graph.mutation.entity.delete` itself to
  reclaim the entity (documented upstream candidate — see Impact).
- **Engine population** (`flock-physics`): `AddBoids(n)` / `RemoveBoids(ids)`
  with a monotonic ID allocator, applied between ticks by the same staging
  discipline as steering modifiers (the grid rebuilds from the boids slice
  each tick — no fixed-N assumption in the hot path). Determinism for a fixed
  population is unchanged.
- **Spawn/churn control** (`population-control`, minimal): `POST
  /boids/population/spawn` (a spawn wave of N) and a churn dial `PUT
  /boids/population/churn-hz` (spawn waves/sec — the second load axis). Live
  boid count already rides the frame; the UI gains only a count readout and a
  spawn button.

**Hot-path statement** (required): population changes are event-rate lifecycle
operations. The tick loop reads a *staged* population delta exactly as it
reads staged steering modifiers — no locks, KV, lifecycle, or graph calls per
tick. All CAS'd/audited writes (Create, Transition, delete) happen on
spawn/cull events only, per ADR-001 §3. The physics hot path stays off
NATS/rules/graph-ingest.

## Capabilities

### New Capabilities

- `boid-lifecycle` — workflow declaration, host Manager wiring, spawn via
  `Create`, cull via observed `culled` transition, entity reclaim via graph
  delete.
- `population-control` — spawn-wave endpoint + churn dial (the load axis);
  minimal count/spawn UI.

### Modified Capabilities

- `flock-physics` — `AddBoids`/`RemoveBoids` between ticks (stable IDs,
  determinism restated).
- `zone-steering` — predator cull: sim `lingered` fact + rule
  `lifecycle_transition` action.
- `boid-ui` — live count readout + spawn-wave button (minimal).

## Impact

- `internal/flock` (population APIs + ID allocator); `internal/sim` (staged
  population deltas, `lingered` emission, `ENTITY_STATES` cull watcher,
  spawn/cull orchestration, graph delete); `internal/api` (spawn + churn
  endpoints); `cmd/semboids` (Manager wiring); `configs/rules/` (cull rule);
  `ui/` (count + button).
- **First downstream exercise of `pkg/lifecycle` + rule `lifecycle_*`
  actions.** Research (beta.138) already found file-worthy gaps to raise as
  the slice validates them: (1) **no despawn primitive** — `Complete`/`Fail`
  leave the entity in `ENTITY_STATES`; reclaim needs a separate
  `graph.mutation.entity.delete`, and `Manager.Watch` ignores deletes;
  (2) **no batch spawn/cull** — one graph-ingest round-trip per entity, on the
  #480 write path; (3) **O(workflows × bucket-size) `$entity.lifecycle.*` rule
  lookups** per fire. These are the churn-load findings; each files upstream
  with evidence, never worked around app-side.

## Non-goals

- **Full population management** — no `GET/PUT /boids/population` target-size
  controller, PID-style hold, or despawn-to-target. The slice has a spawn
  trigger + churn dial only.
- **Rich population UI** — no sliders/target fields beyond count + spawn
  button.
- **Wave-provenance entities** — no graph entity linking a wave's members
  (deferred; nice for the graph pane, not needed for the slice).
- **Predator *boids*** or autonomous hunters (culling stays zone+rule-driven).
- **Population dynamics** beyond waves and culls (no breeding, energy, aging).
- **Lifecycle-managed zones** (still static config).
- **App-side fixes for the substrate gaps** — they file upstream (Impact),
  not patched here beyond the minimal delete the slice needs to not leak.
