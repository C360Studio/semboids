# boid-lifecycle delta: revision-fenced reclaim

## MODIFIED Requirements

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
