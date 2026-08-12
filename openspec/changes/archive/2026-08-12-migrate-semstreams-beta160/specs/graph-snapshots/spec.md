# graph-snapshots delta: typed reconcile replaces triple.remove

## MODIFIED Requirements

### Requirement: Neighbor sets replace on each snapshot
Each snapshot SHALL publish the boid's full current neighbor set so
graph-ingest's predicate-level merge (`MergeTriples`) replaces the previous
set for the `flock.neighbor.of` predicate.

The **empty-set case is a verified substrate limitation on the current pinned
SemStreams (beta.160)**: the stream-upsert path (`entity.boid.upsert` →
`MergeEntity`) is add/merge-only and cannot express "now zero neighbors" — an
arrival carrying no `flock.neighbor.of` triple leaves the resident edges in
place (correct merge behavior, since it preserves predicates owned by other
writers such as `flock.lifecycle.phase`). Verified against beta.160 source:
the stream fact-projection lane validates and subject-fills arrivals but still
applies them through the same predicate-level merge.

Therefore, on a boid's non-empty→empty transition the publisher SHALL clear
the edges through the substrate's typed mutation contract: an
`entity.reconcile` mutation whose desired set for the `flock.neighbor.of`
predicate group is empty (the canonical replacement for the retired
`graph.mutation.triple.remove` operation, which no longer has a responder).
The transition SHALL remain tracked on the off-loop coordinator goroutine so
the ADR-001 physics hot path is untouched. Each snapshot SHALL also publish an
always-present `flock.neighbor.count` property — a genuine published degree
property and the graph pane's neighbor-set reset sentinel.

The reconcile is revision-fenced by the substrate. A revision conflict from a
concurrent snapshot write is a definite non-commit: the publisher SHALL retry
the clear (bounded), and SHALL NOT blindly retry an ambiguous
(`commit_unknown`) outcome. Bulk snapshot publishing SHALL stay on the batch
stream lane; the reconcile fires only on emptying transitions
(Product Boundary: no per-snapshot request/reply for ordinary updates).

The upstream ask that tracked a substrate-native alternative,
**C360Studio/semstreams#578**, is RESOLVED by this typed reconcile operation;
the transition tracker itself remains because clearing stays caller-initiated
on the stream lane.

#### Scenario: Neighbor churn does not accumulate
- **GIVEN** a boid whose neighbor set changes between snapshots
- **WHEN** the second snapshot lands
- **THEN** ENTITY_STATES holds only the current neighbor relationships (no
  union of past sets)

#### Scenario: Emptying a neighbor set clears the edges
- **GIVEN** a boid that had `flock.neighbor.of` edges in the previous snapshot
- **WHEN** its next snapshot has an empty neighbor set
- **THEN** the publisher issues an `entity.reconcile` mutation with an empty
  desired set for `flock.neighbor.of` on that boid
- **AND** the boid's `flock.neighbor.of` edges are cleared from ENTITY_STATES
  and the INCOMING index (the stream merge alone does not clear them)

#### Scenario: Clearing an already-empty set costs nothing
- **GIVEN** a boid whose `flock.neighbor.of` edges are already cleared
- **WHEN** an `entity.reconcile` with an empty desired set is issued for it
- **THEN** the mutation reports unchanged and writes no new entity revision

#### Scenario: A concurrent write does not permanently defeat the clear
- **GIVEN** an emptying transition racing other writes to the same boid
- **WHEN** the reconcile returns a revision conflict
- **THEN** the publisher retries with a fresh authoritative read and the edges
  are cleared
