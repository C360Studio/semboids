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

### Confirmed on a quiet box (sibling stacks stopped)

Re-ran the same isolated A/B with the `semconnect-conformance` stack gone
(rest-load ~2.5, though the semboids stack itself drives it to ~7–9 during a run):

| Run | beta.146 | beta.149 | Δ |
|---|---:|---:|---|
| Round 1 (b149 first) | 2492 | 2158 | −13.4% |
| Round 2 (b146 first) | 2529 | 2326 | −8.0% |
| **Aggregate mean** | **~2511** | **~2242** | **−10.7%** |
| Best window | ~2640 | ~2405 | −8.9% |

The gap barely moved from the loaded box (−13 %) to the quiet box (−11 %), so
contention was *not* the driver — it added a couple of points of variance, no
more. Across 8 measurements and both run orders the effect is stable:
**beta.149 ingests ~9–11 % slower** for semboids' high-fanout snapshot firehose.

### Attribution (b146↔b149 profile diff): validation on the serialized RMW path

- **beta.146 ingest path has no validation.** `processIngest → ingestEntity →
  MergeEntity`, with a plain `json.Unmarshal` of the stored entity — no
  `UnmarshalEntityState`, no `Validate*`, no `ParsePredicate` (they do not exist
  at beta.146).
- **beta.149 swaps the plain decode for the validating `UnmarshalEntityState`**
  (8.35 % cum): `json.Unmarshal` 2.77 s (pre-wave) **+ `ValidateDecodedEntityState`
  550 ms (new)**, plus `validateEntityStateGuardEntry` 1.94 %, `ValidateEntityStateContract`
  1.89 %, `ParsePredicate` per triple.
- **Both are I/O-bound** — `syscall.Write` (NATS/KV) ~20 % of CPU on each — and
  ingest is **per-key serialized** (ADR-072 keyed lanes). So each mutation's
  cycle is `read → decode+validate → merge → CAS-write`, serialized per entity
  ID. The new validation is only ~4–5 % of aggregate CPU, but it sits *on that
  serialized critical path*, extending every cycle's latency, which is why a
  small CPU cost yields a ~10 % per-key throughput drop.

**Sharp point for upstream:** `UnmarshalEntityState` re-validates ENTITY_STATES
that **graph-ingest itself wrote and already validated on the way in** (via the
candidate-side `ValidateEntityPredicates` + guard). For the sole writer's own
read-modify-write, that read-path re-validation is redundant — the bucket can't
have gone non-canonical since graph-ingest is its only writer. A non-validating
fast-path decoder for the RMW read (keeping the validating decoder for external
graph-view consumers) would remove the 550 ms from the hot cycle. That is the
verified-regression the sister-repo checklist asks us to file.

### Net trade-off

beta.149 buys a **~14× cut in index write amplification** for a **~10 %
ingest-ceiling reduction** (contract validation on the serialized write path).
Favorable overall — far less redundant index maintenance — but the ingest cost
is real, attributed, and worth an upstream optimization (drop the redundant
read-path re-validation in graph-ingest's own RMW).

## Update: beta.151 (gh#562 read-side fix) — no macro recovery, true cause found

semstreams shipped the gh#562 fix in **beta.151** (`UnmarshalEntityStateTrusted`
on the five owner RMW reads; validating decode kept for external readers;
micro-bench 33.0→29.7µs/RMW decode, 163→114 allocs). We re-measured. semboids
now pins beta.151 (builds/boots clean, zero contract rejections — passes
beta.150's new triple-lane structural gate too).

### The fix did not recover the ingest ceiling

Clean interleaved three-way (fresh NATS per run, settle-waited, alternating
order — within-session; absolute numbers drift ~10% across sessions, so only
within-session comparisons are trusted):

| | drain | vs b146 |
|---|---:|---|
| beta.146 | 2771/s | — |
| beta.149 | 2520/s | −9.1% |
| beta.151 | 2519/s | −9.1% |

**b151 is identical to b149** (<0.1%; per-mutation processing 2593 vs 2592µs).
The fix landed structurally (profile: `MergeEntity` → `UnmarshalEntityStateTrusted`,
read-side `ValidateDecodedEntityState` 550ms→~0 on the hot path) but produced no
macro recovery — the read-side re-validation, though genuinely redundant, was not
the bottleneck.

### Index-contention hypothesis: refuted

Four-cell isolation `{b146, b151} × {index-on, index-off}` (does the index side's
ADR-077 owner-discovery LIST traffic on the shared NATS connection cause it?):

| | index-on | index-off | index cost |
|---|---:|---:|---:|
| beta.146 | 2533/s | 2781/s | +248/s |
| beta.151 | 2352/s | 2610/s | +258/s |

The b146→b151 gap **persists with the index off** (−6.2% vs −7.2%), and the index
cost is **identical** for both versions (+248 vs +258/s). So it is *not* a
second-degree index effect — the regression lives in graph-ingest's own
per-mutation write path.

### The cause: write-side contract validation, ~3× per mutation

`MergeEntity` diff, beta.146 → beta.151 (code-definitive):

- beta.146: plain `json.Marshal` / `json.Unmarshal` — **zero validation**.
- beta.149+: `graph.ValidateEntityStateContract(entity)` at the top **and**
  `graph.MarshalEntityState` (which itself calls `ValidateEntityStateContract`
  before marshaling) at **two** sites (the candidate and the merged result). So
  every mutation runs the canonical-contract validation ~3× — each pass
  `ParsePredicate`-ing every triple. Our boids carry ~15–30 triples, so the cost
  scales with fan-out.

On the per-key-serialized ingest lane (ADR-072), this per-mutation CPU extends
each lane's cycle, dropping the ceiling ~6–9% for high-fan-out producers. The
gh#562 fix removed one of the ~3–4 passes (the read-side), which is why it did not
move the macro number; the merged-marshal re-validation (the safety invariant that
*lets* the read be trusted) and the top-level contract check remain — inherent to
the fail-closed contract. A `MergeEntity` micro-bench with/without the contract
validation would give the clean per-op number; reducing redundant re-validation of
already-validated resident triples on the merged-marshal path is the analogous
follow-up to gh#562, at semstreams' discretion given the safety invariant.

**Bottom line:** the wave's net for semboids is unchanged — ~14× lower index
write amplification, ~6–9% lower ingest ceiling from per-mutation contract
validation (not the read-side redundancy, not index contention). Favorable
overall.

## Update 2: the −9% cause localized — ~1 extra read per mutation (semstreams#562)

semstreams proposed a sharper hypothesis: the wave routes per-mutation work
through *existing* foreign-edge helpers (partition triples by subject → foreign
lane: claims/birth-stubs/restamps = an extra CAS write to a different entity per
edge), which an edge-heavy boid workload would multiply — I/O round trips, not
CPU (explaining the profile-invisible, fan-out-scaled shape). Measured it
directly (index-off, at overload):

| | ops/entity (NATS `varz` msgs ÷ entities) |
|---|---:|
| beta.146 | ~41 |
| beta.151 | ~44 |

**+5–6% more round trips per entity** — robust (a per-entity ratio, load-independent)
and roughly accounts for the −6–9% throughput drop on the serialized lane. The
foreign-edge helpers are present in both tags (327 vs 331 grep hits), consistent
with "existing helpers."

**But the extra ops are reads, not writes.** Per-bucket writes/entity (jsz
`last_seq` deltas) are identical:

| bucket | beta.146 | beta.151 |
|---|---:|---:|
| KV_ENTITY_STATES | 1.0 | 1.0 |
| KV_ENTITY_SUFFIX_INDEX | 2.0 | 2.0 |
| KV_GRAPH_INGEST_APPLIED_SEQ | 1.0 | 1.0 |
| **graph-ingest writes/entity** | **4.0** | **4.0** |

`ENTITY_STATES` grows ~1:1 with boid snapshots — so the "extra CAS *write* per
foreign edge" doesn't multiply for semboids, because our neighbor references are
**resident** boids (fixed 200-flock; births only at spawn), so the birth-stub /
restamp path finds every target present and writes nothing. The +5–6% is
therefore ~**1 extra read per mutation** (writes flat, total ops up).

**Complete cause:** the −6–9% regression = ~1 extra KV read per mutation (I/O,
~+5–6%) + the per-mutation write-side contract-validation CPU (Update 1, a few %),
both on the per-key-serialized ADR-072 lane. semstreams' round-trips *direction*
is confirmed; their write-multiplication *mechanism* would bite a workload that
births/restamps foreign targets, but semboids (resident targets) sees it as a
fixed extra read instead. The exact read isn't localizable from metrics (jsz seq
counts only writes) — handed to semstreams with the `OWNER_CLAIMS`-reader-absent
observe-only-seam lead (semstreams#562). Reported: comment 5012453853.

## Update 3: the fix (semstreams main@7485c785) — read tax → 0, ingest recovers

semstreams localized the +3 msgs/entity precisely: the wave (`cba784ea`, beta.147)
added **three live `WatchAll` contract validators** on ENTITY_STATES —
`startEntityStateGuard` (graph-ingest self-watch), `startGraphStateGuard` (rule
processor, unconditional even with zero patterns), `startEntityContractWatch`
(graph-clustering). Each fans every write back full-payload over the shared
connection with a validating decode, on **watcher goroutines** (why the CPU
profile stayed quiet; the bytes land in `syscall.Write`). My `OWNER_CLAIMS` guess
was the wrong seam, but the read-shaped round-trips shape was right. Fixed by
**#570** (write-path validation collapse) + **#572 / `7485c785`** (retire all
three watchers; per-entity poison response, ADR-079). semboids builds + passes
`task check:push` on `7485c785`.

**Consumer-report confirmation:** `KV_ENTITY_STATES` consumers **6 → 3**
(beta.151 → `7485c785`) — the three guard watchers retired.

**Read-shaped delta → zero.** Ops/entity (`varz` msgs ÷ entities, index-off):

| | ops/entity |
|---|---:|
| beta.146 | 39.77 |
| main@`7485c785` | 39.78 |

Identical — the +5–6% fan-out tax is gone.

**Macro recovery (interleaved, settle-waited, same-session):**

| | drain (index-on) | drain (index-off) |
|---|---:|---:|
| beta.146 | 2828/s | 3187/s |
| beta.151 | 2571/s (−9.1%) | — |
| main@`7485c785` | 2712/s (−4.1%) | 3171/s (−0.5%) |

**Index-off, graph-ingest fully recovers** to baseline (−0.5%, noise) — so the
write/merge path carries no residual. Index-on, ~55% of the −9.1% is back and a
**~4% residual** remains — and it appears *only* with the index-side consumers
running (index-off it is zero), so it is **index-side, not the merge path**.
Likely graph-clustering's validation, now folded into its polled read seam (#572),
doing per-read work beta.146's clustering did not — a hypothesis, not measured;
pinnable with a clustering on/off isolation, filed separately if worth it.

**Decomposition of the original −9.1%:** dominant share = three-watcher fan-out
reads (fixed by #572, read delta → 0); graph-ingest write path fully recovers; a
small ~4% index-side residual remains (index-on only). semboids stays pinned to
beta.151 in the PR — `7485c785` is untagged main; adopt the tag once it carries
#570 + #572.

**Method note (two attribution near-misses, both caught by measuring):** the
read-side redundancy and the "residual = write-path CPU" call were both plausible
from the profile and both wrong at the macro level — resolved only by the
firehose rig (op-count parity, index on/off isolation), not by reading code.

## Update 4: adopted — beta.152

The fixes tagged as **beta.152** (= beta.151 + #570 + #572 + one openspec doc-tick,
`4568b8de`; code byte-identical to `7485c785`). semboids pins beta.152: `task
check:push` green, live boot clean (entities landing, zero contract rejections,
`KV_ENTITY_STATES` consumers = 3 — watchers retired). Update 3's recovery
measurement stands as the beta.152 result. Migration PR moves 146 → beta.152.
