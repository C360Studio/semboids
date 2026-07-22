# Pre-change baseline (task 1.1 / 1.2)

Captured on `v1.0.0-beta.155`, clean NATS, `configs/flock.json` (200 boids,
dial 1Hz), before any graphview adoption.

## 1.1 JetStream consumers vs concurrent SSE clients

| SSE clients | `KV_ENTITY_STATES` | `KV_COMMUNITY_INDEX` |
|---:|---:|---:|
| 0 | 3 | 0 |
| 1 | 4 | 1 |
| 2 | 5 | 2 |
| 4 | 7 | 4 |
| 8 | 11 | 8 |
| 0 (teardown) | 3 | 0 |

Exactly `3 + N` and `N`: two `WatchAll` consumers per connected client. Clean
teardown, so the cost is purely proportional to concurrent viewers.

**Post-change requirement:** both columns MUST be flat in N, equal to the
zero-client baseline (3 and 0). This is the falsifiable claim of the change.

## 1.2 SSE wire format

Golden captured at `evidence/sse-wire-golden.json` (trimmed to two entities;
shape is what matters). Top-level shape is `{"entities": [...]}`, one array of
per-entity objects, flushed ~500ms.

**Post-change requirement:** byte-compatible shape. The UI must need no edits.

## 1.3 Graph pane visual reference

`evidence/graph-pane-baseline-pre-change.png` — nodes and edges rendering in
the neutral unassigned color (`#6f6f6f`), COMMUNITY_INDEX empty.

Captured only after fixing a pre-existing UI defect found while taking this
baseline: the pane rendered completely blank whenever clustering had never
run (commit `8d907bb`). `evidence/graph-pane-BLANK-beta155.png` is the
before state. The pane had only ever worked because clustering happened to
be alive; semstreams#590 starvation exposed it.

**Post-change requirement:** visually equivalent to this reference at dial 1Hz.
