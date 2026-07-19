## Why

semboids carries an app-side workaround for a beta.146 limitation: graph-ingest's
predicate-level merge could not express "a boid now has *zero* neighbors" (an
empty `flock.neighbor.of` set is a no-op merge, leaving stale edges), so the
publisher tracks a `prevHadNeighbors` map on its coordinator goroutine and, on
each non-empty→empty transition, issues an extra `graph.mutation.triple.remove`
request to clear the edges. The just-adopted beta.152 wave (ADR-077) added
**complete replacement semantics** for graph projections — our `graph-snapshots`
spec already names "owned-projection replacement via the mutation API" as the
alternative to this "cosmetic-staleness fallback." Per the Product Boundary
(prefer the substrate over an app-side parallel path), we should verify beta.152
now expresses now-zero and retire the workaround if so.

## What Changes

- **Verify first (gating).** Measure, on beta.152, whether publishing a boid
  snapshot with an empty neighbor set clears its `flock.neighbor.of` edges
  **end-to-end** — the boid's triples in `ENTITY_STATES` (what the graph pane
  reads via `graphstream.go`) *and* the derived INCOMING index — through the
  existing stream-upsert path (`entity.boid.upsert` → `MergeEntity`). If the
  stream merge still can't clear an absent predicate, test whether the
  `graph.mutation.entity.update_with_triples` replace-mode does (a request/reply
  vs batch-stream throughput tradeoff to weigh).
- **If verified — retire the workaround** (the payoff): remove `prevHadNeighbors`,
  `removeNeighborTriples`, the coordinator transition loop, and the `TripleRemover`
  dependency from `internal/boidgraph/publisher.go`; the snapshot path just
  publishes the current set and lets replacement clear staleness. Re-evaluate the
  always-present `flock.neighbor.count` property (keep as a useful property, or
  drop if it existed only to keep the merge firing).
- **If NOT verified — keep the workaround and file upstream** rather than carry a
  silent app-side path: annotate/file a SemStreams issue for empty-predicate-set
  replacement on entity upsert, and leave the current (documented) removal in
  place. Boundary discipline: the outcome is either substrate-native or an
  upstream issue, not a new app-side hack.

## Capabilities

### New Capabilities

<!-- none: this simplifies an existing behavior against new substrate semantics -->

### Modified Capabilities

- `graph-snapshots`: the **"Neighbor sets replace on each snapshot"** requirement
  currently pins the D6 cosmetic-staleness fallback (explicit removal + the
  always-present `flock.neighbor.count`). It changes to require substrate-native
  replacement of the neighbor set — including the empty case — with the app-side
  removal retired. (If verification fails, the requirement is instead clarified
  to name the upstream gap; the delta is written to match the measured outcome.)

## Impact

- **Code**: `internal/boidgraph/publisher.go` (remove `prevHadNeighbors`, the
  coordinator empty-transition loop, `removeNeighborTriples`, the `TripleRemover`
  dep + its wiring in `component.go`); possibly `internal/boidgraph/payload.go`
  (the always-present `flock.neighbor.count`). Tests: `publisher_test.go`,
  `snapshot_integration_test.go` (the empty-neighbor removal + non-accumulation
  assertions) migrate to assert substrate-native clearing.
- **Physics hot path**: not touched. The neighbor-empty handling lives entirely
  on the publisher's off-loop coordinator goroutine (after the async batch
  joins), never on the 30Hz physics tick — the ADR-001 hybrid split is preserved.
- **Substrate**: this makes semboids exercise and validate ADR-077's replacement
  semantics for a real edge-heavy, high-churn workload — the load-generator role.
  A negative result is a filed SemStreams issue, not an app-side carry.

## Non-goals

- Not changing the snapshot cadence/dial, the async batch publisher, or the
  one-snapshot-at-a-time ordering invariant.
- Not touching the physics loop, spatial hash, or steering — only the graph
  projection of an emptying neighbor set.
- Not reimplementing any graph-ingest primitive. If the substrate can't express
  now-zero on the stream path, we keep the documented workaround and file
  upstream — we do not build a second app-side path.
- Not re-modeling the neighbor relationship (`flock.neighbor.of` stays the edge
  predicate; `flock.neighbor.count` semantics unchanged if kept).
