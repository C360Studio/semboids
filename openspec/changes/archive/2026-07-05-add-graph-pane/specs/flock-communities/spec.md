# flock-communities Specification (delta)

## ADDED Requirements

### Requirement: Community detection runs on the live boid graph
The flow SHALL include `graph-index` (relationship indexes over
ENTITY_STATES) and `graph-clustering` (LPA over the indexed graph, LLM
enhancement disabled) so that community assignments for boid entities land
in the COMMUNITY_INDEX KV bucket at the configured detection interval
(default 2s).

#### Scenario: Flocks become communities
- **GIVEN** a running simulation whose boids have formed spatially distinct
  flocks with published neighbor relationships
- **WHEN** a detection interval elapses
- **THEN** COMMUNITY_INDEX contains communities whose members correspond to
  the distinct flocks

#### Scenario: Clustering lags, never blocks
- **GIVEN** a dial setting that saturates ingest
- **WHEN** detection runs
- **THEN** community assignments may be stale but physics, frames, and zone
  steering are unaffected

### Requirement: Communities stream to the browser
The boids API service SHALL expose an SSE endpoint
(`GET /boids/graph/stream`) that watches ENTITY_STATES (boid entities) and
COMMUNITY_INDEX, sends an initial full sync, and then batched updates
(coalesced per entity, flush interval ~500ms) carrying boid positions,
neighbor lists, and per-entity community assignments.

#### Scenario: Initial sync then increments
- **WHEN** a client connects to the stream
- **THEN** it first receives the current graph state, then periodic batches
  reflecting subsequent KV changes

#### Scenario: Bounded browser traffic
- **GIVEN** the dial at 30Hz
- **WHEN** updates flood the KV buckets
- **THEN** SSE messages remain bounded by the flush interval (latest state
  per entity per batch), not by the dial rate
