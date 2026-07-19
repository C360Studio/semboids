## Context

semboids republishes each boid as a graph entity every snapshot through the
batch stream (`entity.boid.upsert` → graph-ingest `MergeEntity`). A boid's
`flock.neighbor.of` edge set churns every tick and legitimately drops to **zero**.
graph-ingest merges per `(subject, predicate)` (`graph.MergeTriples`): an arrival
carrying *no* triple for a predicate leaves the resident triples untouched — the
correct default, because it preserves predicates owned by *other* writers (e.g.
`flock.lifecycle.phase`, written by the lifecycle Manager, which the snapshot must
not clobber). The consequence: a stream arrival **cannot express now-zero** — an
emptied neighbor set leaves stale `flock.neighbor.of` edges in ENTITY_STATES,
which the graph pane reads directly (`internal/api/graphstream.go`) and which the
INCOMING index derives from.

The app carries a workaround (`internal/boidgraph/publisher.go`): the publisher
coordinator keeps a `prevHadNeighbors map[uint32]bool`, detects each
non-empty→empty transition, and issues a `graph.mutation.triple.remove` for
`flock.neighbor.of` on that boid; `payload.go` always emits `flock.neighbor.count`
(even 0). This proposal asked whether the just-adopted **beta.152 wave (ADR-077
replacement semantics)** now expresses now-zero on the stream path, letting the
workaround retire. Per the Product Boundary, the outcome is either substrate-native
or an upstream issue — never a second app-side path.

## Goals / Non-Goals

**Goals:**
- Answer the gate with live evidence on beta.152: does an empty-set stream publish
  clear a boid's `flock.neighbor.of` edges end-to-end (ENTITY_STATES + INCOMING)?
- Record the verified decision in the `graph-snapshots` spec so the workaround is
  documented as *verified-necessary on the current substrate*, not a stale relic.
- Leave a permanent regression guard that re-opens the gate if the substrate ever
  changes to clear empties.
- File the upstream enhancement that would let the workaround retire natively.

**Non-Goals:**
- Not changing snapshot cadence/dial, the async batch publisher, or the
  one-snapshot-at-a-time ordering invariant.
- Not touching physics, spatial hash, or steering.
- Not reimplementing any graph-ingest primitive or adding a second app-side path.

## Decisions

### D1 — Gate methodology: raw stream publish, both stores

Verify with an integration test (`internal/boidgraph/neighbor_empty_verify_test.go`,
real graph-ingest + graph-index over testcontainer NATS) that publishes one boid
through the **raw** stream path (`PublishToStreamWithAck`, *bypassing* the
publisher's `removeNeighborTriples`) so it measures the substrate's merge alone,
and checks **both** stores the handoff flagged: ENTITY_STATES `flock.neighbor.of`
triple count (the pane's source, parsed exactly) and INCOMING index rows keyed by
the boid as source (`target6.source6.hex(predicate)`, ADR-077).

*Alternative considered:* drive the full sim (as `snapshot_integration_test.go`
does) and wait for a boid to naturally empty. Rejected: non-deterministic and
slow; the direct publish isolates the exact merge semantics under test.

### D2 — Verified result: the stream path cannot clear an emptied set

| Step | ENTITY_STATES `flock.neighbor.of` | INCOMING rows (source) |
|---|---|---|
| 1. publish neighbors {1,2,3} | 3 | 3 |
| 2. **republish empty set (stream `MergeEntity`)** | **3 (unchanged)** | **3 (unchanged)** |
| 3. `graph.mutation.triple.remove` | **0** | **0** |

Zero contract rejections throughout — this is merge-vs-replace semantics, not
validation. ADR-077's replacement is **index-side** (NAME/PREDICATE/INCOMING owner
discovery): the index faithfully re-projects whatever ENTITY_STATES holds, so if
`MergeTriples` preserves the stale edges in ENTITY_STATES, the index preserves them
too. Source-confirmed: `MergeTriples` (`graph/helpers.go`) keeps non-conflicting
resident triples; `extractEntityFromMessage` builds the arrival's EntityState
purely from `graphable.Triples()` — the stream arrival has **no** remove channel.

### D3 — Keep the workaround (the gate's "NOT verified" branch)

The stream path can't express now-zero, and this is *inherent* to merge semantics
(multi-writer safety for `flock.lifecycle.phase`), not a beta.146 relic. The
current removal is already substrate-native (`triple.remove`), lives entirely on
the off-loop coordinator (ADR-001 hot path untouched), and fires only on the rare
transition. **Keep it**, and re-anchor its code comments + the spec to the beta.152
verification.

*Alternative considered — `graph.mutation.entity.update_with_triples` replace-mode
(branch b):* its `RemoveTriples` still requires *explicitly naming*
`flock.neighbor.of` (no better than today's `triple.remove`), is **request/reply
per boid** (a throughput regression that defeats the load-generator purpose), is
must-exist (complicates fresh boids), and whole-entity replace would clobber
`flock.lifecycle.phase`. Not an improvement.

### D4 — Keep `flock.neighbor.count`

It is a genuine published property (neighbor degree, useful to the pane and to
graph queries) *and* the pane's reset sentinel (`graphstream.go` clears the
accumulating neighbor set when it sees the count). It is not merely a
"keep-the-merge-firing" hack, so it stays. (Note: the sentinel does not by itself
clear stale edges — under merge order they sort *after* the count — which is
exactly why `triple.remove` remains load-bearing.)

### D5 — File the upstream enhancement

Filed **C360Studio/semstreams#578**: an opt-in, ADR-077-aligned mechanism for a
producer to declare a **source-owned predicate authoritative**, so an emptied set
on a stream arrival replaces-to-empty (only for the named predicate; everything
else stays merge-preserved). That would let semboids drop the transition tracker
natively. Framed as an *enhancement*, not a defect — the merge default is correct.

### D6 — Keep the gate test as a regression guard

Retain `TestNeighborEmptyGate` (reframed from a one-shot spike to a durable
substrate-contract check): its final assertion fails loudly if a future substrate
bump ever clears empties on the stream path, re-opening the retire decision.

## Risks / Trade-offs

- **[The change delivers no code simplification]** → Accepted and intended: the
  gate's honest answer is "keep." The value is the verified decision, the
  regression guard, and the filed upstream path — not deleted lines.
- **[`prevHadNeighbors` grows one entry per ever-spawned boid ID]** (monotonic
  IDs, never pruned on despawn) → Pre-existing, out of scope here; noted as a
  minor follow-up (a tiny leak on indefinite churn runs), not addressed by this
  change since the publisher has no despawn signal.
- **[Someone re-reads the code as a stale relic]** → Mitigated by re-anchoring the
  comments and the spec to the beta.152 verification + #578.

## Migration Plan

No behavior change; nothing to roll back. Steps: keep the verify test; re-anchor
the code comments in `publisher.go`/`payload.go` to beta.152 + #578; update the
`graph-snapshots` spec requirement; record the outcome. `task check` /
`check:push` stay green (the gate test runs under `-tags=integration`).

## Open Questions

- Whether semstreams accepts #578's shape (arrival-level authoritative-predicate
  marker) or prefers a different mechanism — tracked upstream; a positive
  resolution becomes a *future* retire-the-tracker change, not this one.
