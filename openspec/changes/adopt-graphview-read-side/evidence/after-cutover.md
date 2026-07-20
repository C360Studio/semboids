# Post-cutover verification (tasks 7.1 / 7.2 / 7.3)

Same protocol as `baseline-pre-change.md`: clean NATS, `configs/flock.json`
(200 boids, dial 1Hz), `v1.0.0-beta.155`.

## 7.1 JetStream consumers vs concurrent SSE clients — THE claim

| SSE clients | `KV_ENTITY_STATES` | `KV_COMMUNITY_INDEX` |
|---:|---:|---:|
| 0 | 4 | 1 |
| 1 | 4 | 1 |
| 2 | 4 | 1 |
| 4 | 4 | 1 |
| 8 | 4 | 1 |
| 0 (teardown) | 4 | 1 |

**Flat in N.** Before, the same measurement gave `3 + N` and `N`
(3/4/5/7/11 and 0/1/2/4/8). The `+1` per bucket versus the zero-client
baseline is the shared view's own single `WatchAll` — one per bucket for the
whole process, which is the intended shape.

At N=8 that is **11 → 4** on ENTITY_STATES and **8 → 1** on COMMUNITY_INDEX.
Connecting and disconnecting clients no longer creates or destroys any
JetStream consumer.

## 7.2 SSE wire format

Compared live output against `sse-wire-golden.json`:

- top-level keys: `['entities']` both
- entity keys: `['id', 'neighbors', 'x', 'y']` both

Shape identical. The UI needed no changes.

## 7.3 Graph pane

`evidence/graph-pane-after-graphview.png` versus
`evidence/graph-pane-baseline-pre-change.png`: nodes at real positions with
edges, neutral unassigned color, topology mirroring the canvas flocks. No
visible difference.

## Note on throughput

No throughput claim is made — see the proposal's Non-goals. The measured
effect at demo scale was below the noise floor before the change and nothing
here contradicts that. The win recorded above is structural (consumer count),
not performance.
