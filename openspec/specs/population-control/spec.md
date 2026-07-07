# population-control Specification

## Purpose
TBD - created by archiving change add-lifecycle-population. Update Purpose after archive.
## Requirements
### Requirement: Spawn waves add boids on demand
The boids API SHALL expose `POST /boids/population/spawn` with body `{"n": N}`
that adds `N` new boids (fresh stable IDs) to the flock — created as `active`
lifecycle participants and staged into physics between ticks.

#### Scenario: A spawn wave grows the flock
- **GIVEN** a running sim of N boids
- **WHEN** `POST /boids/population/spawn {"n": 50}`
- **THEN** the flock is N+50 within one tick and 50 new active entities appear
  in the graph

### Requirement: A churn dial drives steady spawn load
The API SHALL expose `PUT /boids/population/churn-hz` setting a spawn-wave rate
(clamped ≥ 0; 0 disables) so a sweep can hold a steady entity-churn load — the
create/delete counterpart to the graph snapshot dial.

#### Scenario: Churn holds a spawn rate
- **GIVEN** `PUT /boids/population/churn-hz {"hz": 5}`
- **WHEN** the sim runs
- **THEN** spawn waves fire at ~5 Hz until the dial changes

### Requirement: Population churn is observable
The pipeline SHALL expose Prometheus metrics — spawns total, culls total, an
active-boid gauge, and spawn/cull publish duration — so a churn sweep is
attributable from :9090 (alongside the substrate's `ingest_lag_seconds`).

#### Scenario: Spawn and cull counters advance
- **WHEN** spawn waves and culls occur
- **THEN** `boids_lifecycle_spawns_total` / `boids_lifecycle_culls_total`
  increase and `boids_lifecycle_active` tracks the live count

### Requirement: Lifecycle churn IO drains with bounded concurrency
The sim SHALL dispatch its off-loop lifecycle IO — `Manager.Create` for spawned
boids and `graph.mutation.entity.delete` for reclaimed (culled) boids — through
a single bounded worker pool whose size is set by a `lifecycle_drain_concurrency`
sim-config value (default 8, matching graph-ingest's `ingest_lanes`; clamped
≥ 1, where 1 is the serial path). Distinct-boid operations SHALL run
concurrently across graph-ingest's keyed-concurrent lanes; the pool SHALL bound
the number of in-flight operations so a burst never spawns unbounded goroutines.
This SHALL NOT touch the physics tick loop: `AddBoids`/`RemoveBoids` stay staged
between ticks, and a boid's removal from physics (`stageRemoval`) SHALL remain
synchronous with observing its cull, independent of when the reclaim delete
completes.

#### Scenario: Concurrent creates lift the spawn ceiling
- **GIVEN** `lifecycle_drain_concurrency` at its default (8)
- **WHEN** a large spawn burst stages many boids
- **THEN** up to 8 `Manager.Create` calls are in flight at once and the achieved
  create rate exceeds the serial (`=1`) baseline for the same burst

#### Scenario: Serial mode reproduces the old path
- **GIVEN** `lifecycle_drain_concurrency` set to 1
- **WHEN** spawns and culls occur
- **THEN** at most one lifecycle Create/delete is in flight at a time (the
  pre-change behavior, for A/B measurement)

#### Scenario: Reclaim deletes no longer block cull observation
- **GIVEN** a stream of boids reaching `phase=culled`
- **WHEN** their entity-delete round-trips are slow
- **THEN** the cull watcher keeps observing further culls (deletes run in the
  pool) while each culled boid still leaves physics immediately

#### Scenario: A burst does not exhaust goroutines
- **GIVEN** a spawn burst far larger than the concurrency limit
- **WHEN** the drain runs
- **THEN** in-flight operations stay bounded by `lifecycle_drain_concurrency`
  and the backlog waits in the pending queue, not in goroutines

