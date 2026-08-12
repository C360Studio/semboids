# boid-lifecycle Specification

## Purpose
TBD - created by archiving change add-lifecycle-population. Update Purpose after archive.
## Requirements
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
physics removal, and reclaim the entity through the substrate's typed,
revision-fenced delete: read the entity's authoritative revision, then issue
the `entity.delete` mutation at that revision (raw un-fenced delete requests
are no longer accepted by the substrate). Observation SHALL run independently
of any UI client.

A revision conflict (a concurrent write bumped the entity between read and
delete) is a definite non-commit: the sim SHALL retry the read-then-delete
pairing (bounded). Retries converge because a culled boid leaves physics
immediately and therefore stops receiving snapshot writes. An ambiguous
(`commit_unknown`) outcome SHALL NOT be blindly retried; it SHALL be surfaced
via log and metric rather than silently reinterpreted as success or failure.

#### Scenario: A culled boid leaves physics and the graph
- **GIVEN** the sim's cull watcher running
- **WHEN** a boid's phase becomes `culled`
- **THEN** the boid is removed from the next tick and its ENTITY_STATES entry
  is deleted

#### Scenario: Reclaim needs no browser
- **GIVEN** an active dial and zero UI clients
- **WHEN** a boid is culled
- **THEN** the entity is still deleted

#### Scenario: Reclaim survives a racing snapshot write
- **GIVEN** a boid culled while its final snapshot writes are still applying
- **WHEN** the fenced delete returns a revision conflict
- **THEN** the sim re-reads the authoritative revision, retries, and the
  entity is deleted once the in-flight writes drain
