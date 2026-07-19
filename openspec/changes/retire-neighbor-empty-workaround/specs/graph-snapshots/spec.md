# graph-snapshots Delta — retire-neighbor-empty-workaround

## MODIFIED Requirements

### Requirement: Neighbor sets replace on each snapshot
Each snapshot SHALL publish the boid's full current neighbor set so
graph-ingest's predicate-level merge (`MergeTriples`) replaces the previous
set for the `flock.neighbor.of` predicate.

The **empty-set case is a verified substrate limitation on the current pinned
SemStreams (beta.152)**: the stream-upsert path (`entity.boid.upsert` →
`MergeEntity`) is add/merge-only and cannot express "now zero neighbors" — an
arrival carrying no `flock.neighbor.of` triple leaves the resident edges in
place (correct merge behavior, since it preserves predicates owned by other
writers such as `flock.lifecycle.phase`). This was confirmed end-to-end:
republishing a boid with an empty neighbor set leaves the stale
`flock.neighbor.of` edges present in **both** ENTITY_STATES (what the graph
pane reads) and the derived INCOMING index; ADR-077 replacement is index-side
and re-projects whatever ENTITY_STATES holds, so it does not clear them.

Therefore, on a boid's non-empty→empty transition the publisher SHALL clear
the edges via the substrate mutation API (`graph.mutation.triple.remove` for
`flock.neighbor.of`), tracked on the off-loop coordinator goroutine
(`prevHadNeighbors`) so the ADR-001 physics hot path is untouched. This
removal is verified-necessary, not a legacy relic. Each snapshot SHALL also
publish an always-present `flock.neighbor.count` property — a genuine
published degree property and the graph pane's neighbor-set reset sentinel.

The substrate-native alternative (retiring the coordinator's transition
tracker) is tracked upstream as **C360Studio/semstreams#578** (opt-in
source-authoritative predicate replacement on stream arrival); if it lands,
a future change retires the tracker. The app SHALL NOT add a second app-side
path in the meantime (Product Boundary).

#### Scenario: Neighbor churn does not accumulate
- **GIVEN** a boid whose neighbor set changes between snapshots
- **WHEN** the second snapshot lands
- **THEN** ENTITY_STATES holds only the current neighbor relationships (no
  union of past sets)

#### Scenario: Emptying a neighbor set clears the edges
- **GIVEN** a boid that had `flock.neighbor.of` edges in the previous snapshot
- **WHEN** its next snapshot has an empty neighbor set
- **THEN** the publisher issues a `graph.mutation.triple.remove` for
  `flock.neighbor.of` on that boid
- **AND** the boid's `flock.neighbor.of` edges are cleared from ENTITY_STATES
  and the INCOMING index (the stream merge alone does not clear them)
