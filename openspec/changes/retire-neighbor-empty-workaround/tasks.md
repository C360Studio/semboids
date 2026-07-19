## 1. Verify the gate (beta.152)

- [x] 1.1 Write `internal/boidgraph/neighbor_empty_verify_test.go` (integration):
  real graph-ingest + graph-index over testcontainer NATS; publish one boid
  through the raw stream path (`PublishToStreamWithAck`, bypassing the app-side
  removal), then via `graph.mutation.triple.remove`.
- [x] 1.2 Assert the result in BOTH stores: ENTITY_STATES `flock.neighbor.of`
  count (pane source) and INCOMING rows keyed by the boid as source.
- [x] 1.3 Confirm the outcome: stream `MergeEntity` does NOT clear an emptied set
  (edges linger in ENTITY_STATES + INCOMING); `triple.remove` clears both.
  Result recorded in design.md D2.

## 2. Record the decision (keep the workaround)

- [x] 2.1 Reframe the gate test from a one-shot spike to a durable
  substrate-contract regression guard (header comment + a final assertion that
  fails loudly if a future substrate ever clears empties on the stream path).
- [x] 2.2 Re-anchor the code comments to the verified reality: `publisher.go`
  (the `TripleRemover` doc, `removeNeighborTriples`) and `payload.go`
  (the `flock.neighbor.count` rationale) cite the beta.152 verification +
  `semstreams#578` instead of the beta.146 "spike 1.1" framing.
- [ ] 2.3 Apply the `graph-snapshots` spec delta (this change's
  `specs/graph-snapshots/spec.md`) into `openspec/specs/graph-snapshots/spec.md`
  at archive time (the modified "Neighbor sets replace on each snapshot"
  requirement + the empty-set scenario).

## 3. Upstream

- [x] 3.1 File the ADR-077-aligned enhancement (opt-in source-authoritative
  predicate replacement on stream arrival) → **C360Studio/semstreams#578**.

## 4. Validate

- [x] 4.1 `openspec validate retire-neighbor-empty-workaround --strict` green.
- [x] 4.2 `task check` green (vet/gofmt/revive/-race) and the gate test green
  under `-tags=integration`.
- [x] 4.3 Update project memory with the verified outcome (workaround kept,
  #578 filed, regression guard added).
