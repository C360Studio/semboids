# zone-model Specification

## Purpose
TBD - created by archiving change add-zone-steering. Update Purpose after archive.
## Requirements
### Requirement: Zones are config-defined static circles
Zones SHALL be defined in the flow configuration under the sim component as
circles with `id`, `type` (one of `predator`, `food`, `wind`), center
(`x`, `y`), radius `r`, and `strength`; wind zones additionally carry a
direction vector. Configuration SHALL be validated at component creation:
unknown types, non-positive radii, and duplicate ids are rejected.

#### Scenario: Valid zones load
- **GIVEN** a config with one predator, one food, and one wind zone
- **WHEN** the sim component is created
- **THEN** all three zones are active in the simulation

#### Scenario: Invalid zone rejected
- **WHEN** the sim component is created with a zone of unknown type or
  radius <= 0
- **THEN** component creation fails with an error naming the offending zone

### Requirement: Zones are ingested as graph entities at startup
Each configured zone SHALL be published at host startup as a
BaseMessage-wrapped Graphable payload with deterministic 6-part entity ID
(`c360.semboids.sim.flock.zone.<id>`) carrying type, geometry, and strength
as triples, routed through `graph-ingest` (the sole `ENTITY_STATES` writer)
via its JetStream input.

#### Scenario: Zones land in the graph
- **GIVEN** a running host with three configured zones
- **WHEN** startup completes
- **THEN** `ENTITY_STATES` contains three zone entities with their type and
  geometry triples

#### Scenario: No direct KV writes
- **WHEN** zones are ingested
- **THEN** all writes flow through graph-ingest; the host never writes the
  `ENTITY_STATES` bucket directly

