# beta.160 migration — ingest re-baseline (2026-08-12)

Re-baseline of the graph-ingest ceiling across the beta.158 → beta.160
migration (`migrate-semstreams-beta160`), per the change's task 6.1. The
migration also absorbed beta.159, so deltas here bundle both tags' effects.

## Method

Interleaved within-session A/B, beta.149-arc methodology:

- Binaries: `main` @ beta.158 vs the migration branch @ beta.160, each with its
  own grammar-correct config; **stable population** (zones `[]`, culling off,
  200 boids) at **dial 30 Hz** (deliberate overload — offered ≈ 6000
  entities/s), fresh isolated NATS (`nats:2.12-alpine -js`, :34222) per run.
- 20–25 s settle, then a 45–60 s window on
  `semstreams_datamanager_entities_updated_total`; run order alternated
  (158, 160, 158, 160) plus one instrumented verification run per binary.
- **Box caveat**: contended host (load ~7 on 12 cores, a sibling stack
  running). Only the within-experiment interleaved ratios are trustworthy;
  absolute numbers are directional and swung ~40 % between the two experiments
  for the same binary. Do not quote absolutes as ceilings.

Instrument checks (both binaries, verification runs):

- Offered load identical: `boids_graph_entities_published_total` = 6000–6004/s
  both — the async batch publisher is not the variable.
- Both variants in deep backlog throughout:
  `semstreams_jetstream_consumer_pending_messages{consumer="graph-ingest-entity-wildcard"}`
  ended at 278 k (b158) vs 217 k (b160) — so ingested/s measures the **ingest
  ceiling**, not keep-up. Backlog arithmetic cross-checks: 65 s × (6000 −
  ceiling) ≈ observed pending, both variants.
- Metric-name trap logged: the first harness summed
  `semstreams_nats_consumer_pending_messages` (nonexistent → silent 0). The
  real family is `semstreams_jetstream_consumer_pending_messages`. Same
  lesson as the sweep's `datamanager` vs `graph_ingest` subsystem bug.

## Result

| Run | beta.158 ingested/s | beta.160 ingested/s | ratio |
|---|---|---|---|
| 2×2 round 1 | 1926 | 3592 | 1.87× |
| 2×2 round 2 | 2142 | 3560 | 1.66× |
| verification | 1223 | 2303 | 1.88× |

**beta.160 ingests ≈1.7–1.9× beta.158 at identical offered load**, consistent
across three interleaved comparisons. The pre-v1 contract wave's ~9 % ingest
tax (beta.146→149, semstreams#562 arc) is not just recovered — the ceiling
roughly doubled relative to beta.158.

## Attribution (hypothesis, not verified)

Candidate: **beta.159's add-lane six-field-tuple dedup** (#697/#713). At 30 Hz
snapshots, consecutive arrivals re-assert largely identical neighbor-edge
triples (~12/boid; positions change every tick, edges mostly don't), so a
tuple-level dedup skips a large fraction of per-entity merge work. Not
isolated here (would need a beta.159-pinned midpoint run); a changed counter
semantics for `entities_updated_total` across the wave can't be fully
excluded, though the family and labels are unchanged. If the ceiling matters
for a future campaign, pin beta.159 and run the midpoint on a quiet box.

Migration-specific write paths are not in this loop: the neighbor-empty
reconcile fires only on transitions (rare) and culling was off. Their live
behavior is in the change's `evidence/product-proof.md` (7/7 fenced reclaims,
one observed `revision_mismatch` retried to success, 0 clear failures).

## Standing conclusions carried forward

- semboids remains **ingest-bound**, not index-bound; the firehose instrument
  is unchanged (publisher at 6000/s with dial headroom).
- `coalesce_ms: 500` kept on graph-index (backlog fix, gate-independent).
- The ~4 % index-side residual from the beta.152 arc was not re-measured here
  (index consumers ran but were not isolated); still open.
- Owed when the box is quiet: a clean-box ceiling number for beta.160 and the
  beta.159 midpoint if attribution is ever needed.
