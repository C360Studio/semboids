# Tasks — load-dial

> **beta.138 note:** semstreams#470 (async/pipelined publish) landed in
> beta.138 mid-change. The interim app-side worker pool (tasks 1.x) was
> collapsed to `PublishBatchToStream` per task 7.1 (D2); `graph_publish_workers`
> was dropped. Task rows below reflect the realized async design.

## 1. Publisher async batch (`internal/boidgraph`) — TDD

- [x] 1.1 Ordering invariant tests: consecutive snapshots both containing
      boid B publish B's snapshot-N before its snapshot-N+1 (one snapshot at
      a time + batch join); neighbor-empty removals still fire and
      `prevHadNeighbors` stays on the coordinator. (Worker-pool bound/serial
      tests superseded by the async swap — 7.1.)
- [x] 1.2 Implement: async batch publish inside `publishSnapshot`
      (`PublishBatchToStream`, gh#470); coordinator owns bookkeeping and
      joins on all acks before returning; invariant comment on `Run` (D1/D2)
- [x] 1.3 Metrics: per-snapshot publish duration histogram, entities/snapshots
      published counters, drops counter, dial-hz gauge on :9090 (achieved
      rates derivable from :9090 alone). (Worker-count gauge dropped with the
      pool.)
- [x] 1.4 `BenchmarkPublishSnapshot` with a fixed-latency fake batch
      publisher — one snapshot ≈ one drain latency, ~constant in boid count
      (documents the pipelining win vs the old 200×ack-RTT serial floor)

## 2. JetStream metrics wiring (`cmd/semboids`)

- [x] 2.1 Reorder `main.go`: create `MetricsRegistry` before
      `connectToNATS`; construct the client with
      `natsclient.WithMetrics(registry)` (D3 — wire, don't build)
- [x] 2.2 Live verify on isolated NATS (:24222): scraped :9090 and confirmed
      `consumer_pending_messages` / `consumer_delivered_total` for the
      `graph-ingest-entity-wildcard` consumer and `stream_messages` for the
      ENTITY stream

## 3. E2E latency probe (`internal/boidgraph`) — TDD

- [x] 3.1 Tests: latency = observation time − `observed_at` (newest triple
      timestamp); 1-in-N sampling honored (default 10, configurable);
      malformed/non-boid entries skipped without error
- [x] 3.2 Implement: ENTITY_STATES KV watcher on boid keys (separate,
      always-on watcher — D4), Prometheus histogram
      `boids_graph_e2e_latency_seconds`, sampling config
      `graph_probe_sample_n`. **Bug fixed during bring-up**: the probe gave
      up when ENTITY_STATES didn't exist yet (startup race vs graph-ingest) —
      now waits for the bucket
- [x] 3.3 Integration test (testcontainer, `-tags=integration`): sim +
      graph-ingest, zero SSE clients — histogram populates and the flood
      window's mean latency rises above the baseline window's (backlog
      tracked). Passes; whole suite green

## 4. Sweep tooling

- [x] 4.1 Sweep tool (`cmd/sweep`) + `task sweep HZ=<n> [WINDOW] [WARMUP]
      [BOIDS]`: sets the dial, subscribes to `boids.frames` for measured
      physics fps, warm-up, holds the window, scrapes :9090 at boundaries,
      emits achieved snapshots/entities/s, drops, `consumer_pending` trend,
      e2e p50/p99, physics fps (with a 5% jitter tolerance — bug fixed during
      bring-up). Live-verified
- [x] 4.2 Classification per the D5 attribution matrix
      (publisher-bound / ingest-bound / downstream-lag / rejection-loss) with
      raw signals printed + a `SWEEP_JSON` line. Live-verified: 200 boids ×
      30Hz correctly classified **ingest-bound**

## 5. Instrument validation (200-boid row)

- [x] 5.1 ~~Control run `graph_publish_workers: 1`~~ — moot after the async
      swap (no serial worker path). The 21.6/s serial baseline is already
      banked in `graph-dial-first-look.md`; the sweep tool was validated
      against the ingest-bound signature instead
- [x] 5.2 200-boid dial: **30Hz achieves 30/30 snapshots/s with 0 drops** via
      async (spec scenario "Async publish raises the instrument ceiling"); the
      row saturates **ingest-bound** (characterized fully in the melt campaign,
      §6). `graph-dial-first-look.md` addendum added. (Dials 60/90 clamp to
      tick_hz=30; the healthy↔melt transition is the melt-line in §6.2.)
- [x] 5.3 `task check:push` green (build, lint, `go vet -tags=integration`,
      race unit + integration) + linux/amd64 cross-compile of `./cmd/semboids`

## 6. Melt campaign

- [x] 6.1 Boid rows {200, 500, 1000, 2000} at saturating dials (75s windows,
      ≥2× the 30s pending poll — a first 15s pass misclassified on stale
      pending and was discarded). All four cleanly ingest-bound; 30s CPU
      profile captured at the 200-boid melt (backs #480)
- [x] 6.2 Results doc `docs/perf/melt-campaign-2026-07-05.md`: per-row table
      (dial → achieved, drops, pending trend, backlog drain, classification),
      profile attribution, melt line stated. Key finding: ingest ceiling
      ~350–590/s **flat across a 10× boid range** → serial per-message, not
      key contention. `graph-dial-first-look.md` addendum added
- [x] 6.3 Filed **semstreams#480** — graph-ingest ingest caps ~670 msg/s
      (serial Consume dispatch × 2-RTT CAS write, box 92% idle). Grounded by
      a clean `--pprof` profile: the "CAS contention" guess was refuted
      (MergeEntity 1.14% CPU, 1 in-flight write) — it's I/O-RTT-bound. Folds
      in the processing-duration-histogram gap. Config levers ruled out
      (MaxAckPending not port-exposed, entityCache query-side only)
- [ ] 6.4 `openspec validate load-dial --strict`; README status/roadmap
      update; archive the change

## 7. Async publish adoption (semstreams#470 — landed beta.138)

- [x] 7.1 Swapped the app-side worker pool for `PublishBatchToStream` behind
      the same `publishSnapshot` seam (D2): ordering tests green, metrics/
      `Offer` contracts unchanged, `graph_publish_workers` dropped, 200-boid
      row re-run for parity (30/30, 0 drops)
