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

## 5.2 Read-side metrics on :9090

All seven series exposed, labeled by bucket:

```
boids_graphview_applied_revision{bucket="ENTITY_STATES"} 2803
boids_graphview_caught_up{bucket="ENTITY_STATES"} 1
boids_graphview_caught_up{bucket="COMMUNITY_INDEX"} 1
boids_graphview_subscribers{bucket="ENTITY_STATES"} 0   -> 3 with three clients
boids_graphview_coalesced_drops_total{...} 0
boids_graphview_max_pending_deltas{...} 0
boids_graphview_poison_total{...} 0
boids_graphview_watcher_lost_total{...} 0
```

`subscribers` tracks connected clients (0 → 3), and `caught_up` is 1 for both
views. `applied_revision` advances on ENTITY_STATES and stays 0 on
COMMUNITY_INDEX because nothing has ever been written there (semstreams#590).

**`poison_total` = 0 against live production data** is the meaningful one: the
cutover swapped a bespoke `json.Unmarshal` for the validating
`graph.UnmarshalEntityState`, and this confirms real boid states satisfy the
entity-state contract rather than silently poisoning.

## 7.5 Snapshot-capture cost at scale

Design named this as the main risk: `SnapshotAndSubscribe` copies the
projection under the view lock, and semboids reaches populations most
graphview consumers will not. Measured rather than assumed.

Connect-to-first-batch, 5 samples each, same process, spawned up mid-run:

| population | latency (ms) |
|---:|---|
| 200 | 1038, 1040, 1039, 1047, 1541 |
| 9,673 | 1062, 1071, 1068, 1072, 1067 |

**~27ms added for a ~48x larger projection.** Latency is dominated by the
500ms flush tick, not by snapshot capture.

The watcher is not blocked by connects: `applied_revision` advanced
37,781 → 40,652 *during* the connect burst, and `caught_up` stayed 1
throughout. The bounded copy behaves as ADR-081 describes (capture under
lock, deliver outside).

Risk resolved — no mitigation needed, and nothing to report upstream.

## 7.6 View tick interval (design Open Question 1)

**Resolved: keep 250ms.** The 7.5 data shows connect and delivery latency is
flush-dominated (500ms) and essentially flat across a 48x population change,
so the view tick is nowhere near binding. Keeping it below the flush interval
preserves the flock-communities guarantee that browser traffic is bounded by
the flush interval rather than by the view. No evidence for changing it, and
raising it to 500ms would risk beat interference between the two timers.

## 7.4 Recovery needs no reload

The design (D7) assumed `EventSource` auto-reconnect covers the fail-closed
watcher-loss path. Tested by killing the backend with a browser attached and
never touching the page.

**A/B, clean low-population stack, ports confirmed free between runs:**

| | unattended reconnect |
|---|---|
| with the store fix | yes — 1 connect, subscribers 1/1 |
| with the fix reverted | **yes — 1 connect, subscribers 1/1** |

So D7's assumption holds for the common case and 7.4 passes on the original
code. Three earlier runs that appeared to show "the pane never recovers" were
confounded: the backend was unreachable, first because ~9.7k boids starve the
HTTP endpoints (a known trap in this repo), then because the restarted process
could not bind ports the dying one still held.

**But a narrower failure is real and measured.** Polling the stream endpoint
every 100ms across a start:

```
responses observed during startup: {503: 4, 200: 1}
```

`/boids/graph/stream` serves **503 for ~400ms on every start**, while the
shared view attaches. Per the HTML spec a non-200 makes the browser close an
EventSource *permanently* — `readyState` CLOSED, no retry. Against a ~3s
browser retry cadence that is roughly a 1-in-7 chance per restart of the pane
being stranded until a manual reload, which the graph-pane requirement
forbids. The passing A/B above recovered because the retry happened to miss
the window; that is luck, not correctness.

The store therefore re-arms itself when it observes `readyState === CLOSED`,
which also covers proxy 502s during a backend outage. Three unit tests pin it:
re-arm on CLOSED, leave a still-retrying stream alone, and never reconnect
after an explicit disconnect.
