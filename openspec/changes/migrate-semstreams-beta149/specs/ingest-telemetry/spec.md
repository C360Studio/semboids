# ingest-telemetry Delta — migrate-semstreams-beta149

## ADDED Requirements

### Requirement: Canonical-contract rejects are visible
The sweep/telemetry surface SHALL expose graph-ingest's canonical-contract reject
counters — `semstreams_graph_ingest_predicate_contract_rejections_total{lane,reason}`
and `semstreams_graph_ingest_entity_state_contract_rejections_total{lane,field,reason}`
(fail-closed at graph-ingest since beta.147) — alongside the mutation-rejection
counter, so a boid or zone emitting a non-3-part predicate or a non-6-part entity
ID surfaces as a counted, classified reject rather than silent graph loss. A sweep
window over a conforming corpus SHALL show these counters flat at zero; a non-zero
delta SHALL classify the window as a contract-reject loss. The mutation-rejection
counter SHALL be read under its actual subsystem `graph_ingest`
(`semstreams_graph_ingest_mutation_rejections_total`), not a non-existent
`datamanager` series.

#### Scenario: A conforming flock keeps the counters flat
- **GIVEN** semboids emitting only canonical 6-part entity IDs and 3-part
  predicates
- **WHEN** a sweep window elapses at any dial
- **THEN** the predicate- and entity-state-contract reject deltas are zero and
  the window is not classified as a contract-reject loss

#### Scenario: A non-conforming token surfaces as a counted reject
- **GIVEN** a regression that reintroduces a non-3-part predicate or a
  non-6-part entity ID
- **WHEN** a snapshot carrying it reaches graph-ingest under the fail-closed
  contract
- **THEN** the matching contract-reject counter increments with its `reason`
  (e.g. `arity`) and the sweep reports the loss rather than the entity silently
  vanishing from the graph
