# beta.146 → beta.149 migration: index write-amp collapses ~14×

**Date:** 2026-07-18
**Change:** `openspec/changes/migrate-semstreams-beta149`
**Question:** does the pre-v1 canonical-contract wave (147 breaking + 148 + 149)
shift semboids' throughput or write amplification? (semstreams sister-repo
checklist item: "re-run load instrumentation against the new watcher ordering.")

## Method

Matched A/B on one box, **fresh isolated NATS per run** (`:24222`, JetStream),
identical sweep binary and parameters:

- 200 boids @ 30 Hz, 20 s warmup + 60 s window.
- **beta.146** = `main` (pre-migration code + pin: bare `flock.neighbor`, bare
  rule condition fields, no `pack_id`), built from a worktree.
- **beta.149** = the migration branch (`flock.neighbor.of`, `$message.*`
  conditions, `pack_id`, registered lifecycle predicate).

Both runs are ingest-bound melts (dial > ingest budget) — the regime where the
index maintenance path is most exercised.

## Result

| Metric (per-window) | beta.146 | beta.149 | Δ |
|---|---:|---:|---|
| physics fps | 30.0 | 30.0 | flat |
| contract rejections (predicate / entity-id) | 0 / 0 | 0 / 0 | flat |
| **index write-amp** (puts ÷ idx-events) | **21.3** | **1.47** | **−93% (~14×)** |
| index puts/s (all buckets) | 978.8 | 152.6 | −84% |
| incoming-index puts/s | 564.7 | 47.8 | −92% |
| index events/s (idx throughput) | 46.0 | 104.2 | +126% |
| entities updated/s (ingest drain) | 1668 | 1331 | −20% (confounded) |
| e2e latency p99 | 65.1 s | 64.9 s | flat |
| snapshot drops | 0 | 0 | flat |

## Findings

1. **Index write amplification collapsed 21.3 → 1.47 puts/idx-entity (~14×).**
   This is the headline, and it is unconfounded (a ratio; the ~14× gap dwarfs
   any run-to-run variance). Drivers, from the raw
   `graph_index_kv_operations_total`:
   - **Membership change-detection.** The `predicate` bucket did **1662 puts
     against 37224 list-checks** — one list per event, but a write only when a
     boid's predicate *membership set* changes. A boid always carries the same
     predicates (`flock.position.*`, `flock.velocity.*`, `flock.neighbor.count`,
     `flock.neighbor.of`), so steady-state predicate puts are ~nil. beta.146
     re-wrote membership every snapshot.
   - **Raw predicate keys (ADR-078).** `predicate3.entity6` with
     `PREDICATE_CATALOG` retired — verified absent (`KV_PREDICATE_CATALOG` not
     created) — removes the catalog double-write/join.
   - **Replacement semantics (ADR-077).** INCOMING moved from append to
     owner-discovery + complete replacement; incoming puts/s fell 565 → 48.
2. **Index throughput up 2.3× (46 → 104 events/s).** Cheaper per-event work lets
   the graph-index KV-watch consumer drain faster.
3. **Ingest wall (#480) unchanged, as expected.** Both runs stay ingest-bound at
   30 Hz — ADR-072 keyed-concurrent ingest is in both, and neither 147–149 nor
   this change touches the ENTITY_STATES single-writer path. e2e p99 is flat
   (~65 s, the saturation ceiling).
4. **Ingest drain −20% (1668 → 1331/s) is NOT a clean regression signal.** The
   two windows differed in publish rate (6000 vs 5493 entities/s) and starting
   backlog (44 k vs 0 pending), both of which move the melt drain rate; beta.149
   also grew *less* backlog over the window (+216 k vs +257 k). Before claiming a
   watcher-ordering throughput regression, run an isolated ingest-ceiling A/B
   (single burst, `graph_hz=0`) the way `churn-lifecycle` / `parallel-lifecycle-drain`
   did. Open follow-up.
5. **Correctness/parity clean.** 0 predicate/entity contract rejections on
   beta.149 (all our predicates 3-part canonical, all entity IDs 6-part), physics
   pinned at 30 fps, INCOMING edges + clustering communities render.

## Conclusion

beta.149 is a **clear index-efficiency win for semboids — ~14× less index write
amplification and 2.3× more index throughput — with no correctness or physics
cost.** The ingest wall (#480) is unchanged, as expected. This is semboids doing
its load-generator job: measuring and validating a substrate improvement live.
The ingest-throughput question stays open pending an isolated ceiling run.

Raw JSON for both windows is in the migration change's evidence envelope.

## Follow-up: the ingest ceiling (isolated, but on a loaded box)

The −20% ingest-drain delta above was confounded, so we re-measured with a
**stable population** (`zones: []` → no culling → identical publish rate on both
versions), **deep-backlog steady-state** (overload to a large backlog, then
measure the drain), **multiple windows**, and **both run orders** to break the
order/contention confound. Load average was recorded per window.

| Run | beta.146 drain/s | beta.149 drain/s | Δ |
|---|---:|---:|---|
| Round 1 (b149 first) | 2558 | 2193 | −14% |
| Round 2 (b146 first) | 2478 | 2097 | −15% |
| Best (least-contended) window | 2670 | 2355 | −12% |

**The ~13% gap holds across both orders** — so it is a *real relative effect*, not
the run-order/contention artifact that produced the first −20%. These absolute
numbers (~2.1–2.7 k/s) are also much higher than the confounded melt (1.3–1.7 k/s)
and near the historical ~2331/s, confirming the isolation worked.

**Profile (beta.149, `--pprof`, 30 s under overload) confirms new validation cost
in the ingest write path.** graph-ingest re-decodes the stored entity each
read-modify-write via `graph.UnmarshalEntityState` (8.35 % cum); within it,
`json.Unmarshal` is 2.77 s (present pre-wave) but the **new `ValidateDecodedEntityState`
adds 550 ms**. Across the process the canonical-contract validation appears as
`validateEntityStateGuardEntry` (1.94 %), `ValidateEntityStateContract` (1.89 %),
`ValidateEntityPredicates` + `vocabulary.ParsePredicate` (~0.9 % each), plus
graph-clustering re-validating on its own contract watch (~1.85 %).

**Caveat — this is not yet a clean, filable regression.** Two gaps:
1. **The box was heavily loaded** (`load ~10` on 12 cores throughout — a
   `semconnect-conformance` semstreams+NATS stack and a second AI agent were
   running), so absolute numbers are depressed and the −13 % is likely
   *amplified* by CPU/IO contention on the shared Docker/NATS path the ingest
   ceiling is bottlenecked on (#480, I/O-RTT-bound).
2. **The measured new-validation CPU (~4–5 %) is smaller than the −13 %
   throughput gap**, so validation alone does not fully explain it on this box —
   contention amplification and/or other write-path serialization is implicated.

**To close it:** re-run on a quiet box (pause the sibling stacks) with a
b146↔b149 CPU-profile diff, to get clean absolute numbers and a full attribution
before filing the verified upstream issue the checklist asks for. The **direction
is established** (beta.149 ingests measurably slower for a high-fanout snapshot
firehose); the **magnitude and full cause are pending a quiet-box run**.

Net trade-off for semboids: beta.149 buys a ~14× cut in index write
amplification at the cost of a ~10–15 %-ish ingest-ceiling reduction (validation
on the write path). Both are the substrate doing more correctness work per
mutation while doing far less redundant index maintenance.
