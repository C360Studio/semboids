# Measurement contract

The source audit covers SemStreams `v1.0.0-beta.160` and the frozen target
`v1.0.0-beta.162.0.20260930150212-8b99efe9c66a` (full SHA
`8b99efe9c66a4faa4fa509f9f62cc6bad8392128`). SemStreams beta.160 source is
`8403a2218000e45a31c5132fbfe01af42ed04f14`; SemBoids baseline source is
`8c03cc53836ced93a5df7064473c63ff144e64f1`. These update points were read before interpreting runs.

## Signals and limits

- `boids_graph_entities_published_total`
  - Producer: `internal/boidgraph/publisher.go:168`, after successful batch ACK join.
  - Meaning: Accepted boid publications, not graph commits. Partly ACKed failed batches undercount.

- `boids_graph_snapshots_published_total`
  - Producer: `publisher.go:184`, unconditional after batch attempt and neighbor work.
  - Meaning: Completed snapshot attempts, including batch errors despite its HELP text.

- `boids_graph_snapshots_dropped_total`
  - Producer: `Publisher.Offer`, busy channel rejection.
  - Meaning: Whole snapshot offers dropped before publish; not individual entity, NATS, or WebSocket drops.

- `semstreams_datamanager_entities_updated_total`
  - Producer: `processor/graph-ingest/component.go`, successful merge/create;
    `canonical_mutations.go:recordCanonicalEntityCommit`.
  - Meaning: Committed entity write operations. Includes lifecycle, cluster, and explicit mutations; not exclusively
    snapshot messages.

- `semstreams_jetstream_consumer_pending_messages`
  - Producer: `natsclient/jetstream_metrics.go:poll`, `Set(info.NumPending)` at both pins.
  - Meaning: Periodic unread-message gauge; excludes delivered, unacknowledged work.

- NATS `/jsz` `num_pending + num_ack_pending`
  - Producer: Server ConsumerInfo, sampled directly.
  - Meaning: Authoritative outstanding work for `ENTITY` / `graph-ingest-entity-wildcard`. Both terms retained
    separately.

- `boids_graph_e2e_latency_seconds`
  - Producer: `internal/boidgraph/probe.go:observe`.
  - Meaning: One in 10 boid KV-watch events: wallclock minus snapshot `observed_at`. Uses newest timestamp across all
    stored triples, so concurrent lifecycle mutations can bias it toward newer state. Includes queue/commit/watch
    delay. Deletes/non-boids skipped; not HTTP or WebSocket latency.

- `boids_lifecycle_spawns_total`
  - Producer: `internal/sim/lifecycle.go:observeSpawn` after `Manager.Create`.
  - Meaning: Successful lifecycle registrations, including startup seed flock.

- `boids_lifecycle_culls_total`
  - Producer: `runCullWatcher`, before asynchronously executing delete.
  - Meaning: Observed culled phases and staged physics removals; HELP misleadingly says reclaimed. Reclaim success
    requires separate evidence.

- `boids_lifecycle_reclaim_failures_total`
  - Producer: `reclaimFailed`.
  - Meaning: Exhausted or ambiguous fenced-delete attempts, not every retry.

- Frame FPS / gaps / tick discontinuities
  - Producer: Direct subscription to `boids.frames`.
  - Meaning: Real physics output plus core NATS delivery, without inferring from configured 30Hz. Does not measure a
    browser or slow WebSocket client.

All metric names are checked against emitted raw exposition. Absent series are `null`, never inferred zero.
Histogram windows subtract cumulative buckets; empty windows are `null`; overflow quantiles are marked lower bounds.

## Known instrument traps

Both SemStreams pins' `jetstream_metrics.go` increment their consumer delivered, ACKed, and redelivered counters by
**cumulative** `info.Delivered.Stream`, `info.AckFloor.Stream`, and `info.NumRedelivered` every poll. Their counter
deltas
therefore cannot measure actual per-window delivery or ACK rates. These series are preserved raw but excluded from
interpretation. The same limitation exists at both pins; it is not a migration regression.

NATS `/jsz` captures include stream sequence/retention state and consumer policy. The analyzer permits a conservation
estimate only with dense retained history, no deleted messages, unchanged stream/consumer identity and config,
consumer coverage of all offered subjects, explicit ACK/all-delivery policy, unlimited delivery attempts, and no
redelivery evidence. Otherwise it emits `null` and reasons. Finite retry limits in these runs disable that estimate.
Even an eligible estimate represents disposed consumer work (ACK or TERM), not proof of a successful graph commit.
Committed entity writes remain a distinct signal because mutations can write outside the snapshot consumer.

The existing `cmd/sweep` does not distinguish absent series from zero and omits ACK-pending work. Qualification uses
the separate harness with regression tests for both failure shapes; it does not silently change the product sweep.
No rate calculation divides by the configured window when actual elapsed sampling time is available.

## Reproduction

```bash
python3 -m unittest discover -s scripts/qualification -p 'test_*.py' -v
python3 scripts/qualification/load.py \
  --binary /tmp/semboids-shared-pin/baseline-semboids \
  --source /tmp/semboids-shared-pin/baseline \
  --label baseline-pre-upgrade \
  --out /tmp/semboids-shared-pin/performance/baseline-pre-upgrade \
  --profile stable --hz 30 --warmup 20 --window 45
```

The binary and source locations are configurable. Each output directory must be new. Node 22 runs one normal WebSocket
observer for the paired runs. Each run records the binary
SHA-256, `go version -m`, exact generated config, wallclock, load averages, raw metrics every five seconds, full NATS
server snapshots, every observed frame's tick/population/timestamp, process logs, and bounded shutdown result.

Each run starts and removes its own fresh NATS Docker container using immutable image
`nats@sha256:b270f5e2428354c0335612694d7dd2fb588148e567a5757fdff325ef9c9332e6` (the local `nats:2.12-alpine` image).
This is deliberately separate from SemStreams' integration-test helper using `nats:2.14-alpine`. No host or container
volumes are reused. Default isolated ports: NATS 44222, monitor 48222, API 38080, WebSocket 38081, metrics 39090.

Stable profile: initial 200 boids, seed 1, physics 30Hz, graph 30Hz, no zones, cull disabled, churn 0Hz. Intended
snapshot load is 6,000 entities/s. Churn profile: same initial population/seed/tick rate, normal configured zones,
cull enabled, graph 1Hz, churn 1Hz (five boids per wave). Population changes are a measured outcome, so resulting
accepted entity volume must be reported alongside write rate. Changing population makes a raw throughput ratio
insufficient evidence of a speedup.

## Transport qualification

Both pins increment `semstreams_websocket_messages_sent_total` after successful socket writes and
`semstreams_websocket_errors_total{error_type="client_send"|"client_timeout"}` on send failure or five-second send
timeout.
`pending_buffer_full` belongs to at-least-once pending-ACK bookkeeping and is not the
configured at-most-once frame-drop count. There is no universal per-frame at-most-once loss metric. Missing error
series remain unknown, not zero. Each normal paired load run therefore observes actual WebSocket frame tick gaps
in addition to NATS frame tick gaps. Separate `--slow-client` runs connect one deliberately non-reading socket and
report physics output and normal-client output independently; these runs never enter performance ratios.

The first pre-upgrade diagnostic had no WebSocket observer and a brief overlapping cached unit-test run (reported by
the baseline agent). It establishes observed pre-upgrade behavior, but is excluded from clean paired comparisons.
