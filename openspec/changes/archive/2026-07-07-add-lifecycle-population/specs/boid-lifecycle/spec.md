# boid-lifecycle Delta — add-lifecycle-population

## ADDED Requirements

### Requirement: Boids are lifecycle participants
Each boid SHALL be a `pkg/lifecycle` participant under a single `flock.boid`
workflow (phases `active → culled | expired`; `culled`/`expired` terminal),
registered on one `lifecycle.Manager` in the host and reached by the rule
processor and the sim through `Dependencies.LifecycleManager`. The
participant's entity ID SHALL be the boid's existing 6-part graph ID, so the
lifecycle phase triple lands in the same `ENTITY_STATES` entity as the boid's
position/neighbor triples. The initial seed population AND each spawned boid
SHALL be created in `active` (the seed flock too, else it has no phase triple
and cannot be culled).

#### Scenario: Spawned boid enters the graph as active
- **GIVEN** the boid workflow registered on the host Manager
- **WHEN** a boid spawns
- **THEN** `Manager.Create` records it with phase `active` in ENTITY_STATES

#### Scenario: Cull is a phase transition, not a delete
- **GIVEN** an active boid
- **WHEN** the predator-cull rule fires `lifecycle_transition`
- **THEN** the boid's `flock.lifecycle.phase` becomes `culled` (terminal) and
  the entity persists until the sim reclaims it

### Requirement: The sim observes culls and reclaims entities
Because lifecycle exposes no despawn primitive, the sim SHALL watch
ENTITY_STATES for boid entities reaching `phase = culled`, stage the boid's
physics removal, and issue `graph.mutation.entity.delete` to reclaim the
entity. Observation SHALL run independently of any UI client.

#### Scenario: A culled boid leaves physics and the graph
- **GIVEN** the sim's cull watcher running
- **WHEN** a boid's phase becomes `culled`
- **THEN** the boid is removed from the next tick and its ENTITY_STATES entry
  is deleted

#### Scenario: Reclaim needs no browser
- **GIVEN** an active dial and zero UI clients
- **WHEN** a boid is culled
- **THEN** the entity is still deleted
