# Melt Campaign — graph-ingest saturation (beta.138)

Captured 2026-07-05 for `load-dial` (tasks 6.1–6.2). The instrument ceiling
(serial synchronous publish, ~21.6 snapshots/s — see
`graph-dial-first-look.md`) was removed by the async batch publisher
(`PublishBatchToStream`, semstreams#470), so this is the first characterization
of the *substrate's* melt rather than the instrument's.

## Setup

Isolated single-node NATS (:24222, JetStream), full flow (sim + graph-ingest +
graph-index + graph-clustering @2s + rule-processor + ws-output), 12-core
darwin/arm64, semstreams v1.0.0-beta.138. Backend run with `--pprof` (info
logging — a `--debug` profile spends ~27% of samples in slog and throttles
ingest ~20%, so it is not representative).

Each row restarts the sim at a fixed boid count and holds one saturating dial
(offered ≫ ingest capacity), measured with `task sweep`: 15s warm-up, **75s
window** (≥ 2× the 30s `consumer_pending_messages` poll — D5), physics fps off
the real frame stream, and the D5 attribution matrix. Offered rate held near
6–7.5k entity/s so every row saturates graph-ingest.

## Results

| Boids | Eff. dial | Offered/s | Achieved ingest/s | Drops | `consumer_pending` Δ (75s) | Backlog drain | e2e p99 | Physics | Class |
|---|---|---|---|---|---|---|---|---|---|
| 200 | 30.0/s | 6,000 | **559** | 0 | +481k | ~860s (14m) | saturated | 30.0 fps | ingest-bound |
| 500 | 15.0/s | 7,500 | **592** | 0 | +612k | ~1034s (17m) | saturated | 30.0 fps | ingest-bound |
| 1000 | 6.0/s | 6,000 | **517** | 0 | +487k | ~942s (16m) | saturated | 30.0 fps | ingest-bound |
| 2000 | 3.0/s | 6,000 | **357** | 0 | +500k | ~1401s (23m) | saturated | 30.0 fps | ingest-bound |

Eff. dial is achieved snapshots/s (the dial quantizes to an integer tick
divisor: a 12Hz request runs every-2-ticks = 15Hz). Backlog drain =
`pending / ingest-rate`, a proxy for how far the graph trails the sim; e2e
latency exceeds the probe histogram's finite range (now widened to ~524s) —
"saturated" means minutes-scale, consistent with the drain estimate.

## Findings

1. **The ingest ceiling is ~350–590 entity/s and roughly flat across a 10×
   boid range.** It does not scale with entity/key count — the mark of a
   *per-message serial* ceiling, not per-key CAS contention (contention would
   worsen monotonically as keys multiply). This corroborates the profile in
   **semstreams#480**: graph-ingest dispatches `consumer.Consume` serially and
   each message pays two sequential NATS KV round-trips (a `Get` for the
   revision + a revision-checked CAS `Put`). The box sits ~92% idle; the melt
   is round-trip-latency bound, not CPU/merge/contention bound (`MergeEntity`
   = 1.1% CPU).

2. **The instrument is not the bottleneck at any tested scale.** The async
   publisher achieves the dial with **zero drops** from 200 to 2000 boids
   (6–7.5k entity/s published). Every row is cleanly ingest-bound (drops flat,
   `consumer_pending` climbing ~0.5M over 75s).

3. **Physics holds 30.0 fps through 2000 boids** — ADR-001 isolation confirmed
   at 10× the default population; the melting substrate never touches the tick
   loop.

4. **Mild degradation at 2000 boids (357/s vs ~560/s).** Denser flocks mean
   larger neighbor sets → bigger entity payloads → slower per-message
   marshal + KV round-trips. Secondary to the serial-dispatch ceiling, but it
   means the per-message cost is payload-sensitive.

5. **Practical melt line:** graph-ingest goes ingest-bound whenever
   `boids × dial > ~500 entity/s`. At the 200-boid default that is ~2.5Hz;
   the graph pane stays live and correct below it, and above it the
   drop-oldest publisher sheds snapshots while physics and egress are
   unaffected.

## Upstream

Filed **semstreams#480** (graph-ingest ingest ceiling: serial dispatch +
2-RTT CAS write; box 92% idle; fix directions ranked concurrent-dispatch >
batch-writes > server-side-merge; folds in the missing per-message
processing-duration histogram). The clean 30s CPU profile backing it is the
evidence attached there.
