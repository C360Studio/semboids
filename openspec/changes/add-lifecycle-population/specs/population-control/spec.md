# population-control Delta — add-lifecycle-population

## ADDED Requirements

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
