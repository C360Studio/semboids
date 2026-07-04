# Rule Engine Under Zone Steering — 200 boids @ 30Hz

Captured 2026-07-04 during `add-zone-steering` verification (task 6.2), after
~8 minutes of live running with three zones and six rules. Companion to
[baseline-200boids-30hz.md](baseline-200boids-30hz.md).

## Setup

- `SEMBOIDS_NATS_URLS=nats://localhost:24222 ./semboids --config
  configs/flock.json --debug` (isolated NATS — see finding 3)
- Zones: predator (r90), food (r100), wind (r140); 6 expression rules
- Metrics scraped from :9090

## Numbers

| Metric | Value |
|---|---|
| Transition events received (`boids.zone.events`) | 2,288 over ~8 min (~4.8/s average, bursty) |
| Rule evaluations | 13,728 (6 rules × every event — O(all rules)/message confirmed live) |
| Mean evaluation duration | **~3.9µs** per rule per event |
| Triggered | food 452, predator 392, wind 300 (enter/exit pairs match exactly) |
| Round-trip visible effect | boids tint/deflect within a few frames of zone entry |

## Findings

1. **The rule engine is nowhere near stressed.** 6 rules at event rates cost
   ~54µs of evaluation per event burst-second. The documented O(all rules)
   per message scaling is real but irrelevant at this rule count — the load
   dial (graph-ingest cadence) remains the interesting stress axis, not rule
   evaluation.
2. **Enter/exit pairing is exact** (452/452, 392/392, 300/300) — the
   edge-triggered tracker and the per-entity stateful rule evaluation agree
   perfectly over thousands of transitions.
3. **Shared-NATS config collision (upstream)**: on first demo start the
   backend connected to a sibling project's NATS (semsource on :4222) and
   the config manager silently adopted *its* `semstreams_config` KV bucket
   (15 foreign components), panicking in `websocket.CreateOutput`. The
   bucket name is global, not namespaced by platform/app — two sem*
   products cannot share a NATS server safely. Filed as
   [semstreams#459](https://github.com/C360Studio/semstreams/issues/459).
   Demo isolation workaround: dedicated NATS on :24222.
