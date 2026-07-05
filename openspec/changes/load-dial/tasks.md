# Tasks — load-dial

## 1. Publisher fan-out (`internal/boidgraph`) — TDD

- [ ] 1.1 Failing tests: ordering invariant — with `graph_publish_workers >
      1` and consecutive snapshots both containing boid B, B's snapshot-N
      publish completes before its snapshot-N+1 publish is issued (mock
      `StreamPublisher` recording per-boid order + a concurrency high-water
      mark); pool bound respected; `workers: 1` reproduces exact serial
      call order; neighbor-empty removals still fire under fan-out and
      `prevHadNeighbors` mutation stays on the coordinator
- [ ] 1.2 Implement: `errgroup.SetLimit`-bounded fan-out inside
      `publishSnapshot` (workers marshal + publish only; coordinator owns
      bookkeeping and joins the group before returning); config
      `graph_publish_workers` (default 16, validated ≥1); invariant comment
      on `Run` (one snapshot at a time — D1)
- [ ] 1.3 Metrics: per-snapshot publish duration histogram, entities
      published counter, configured-workers gauge on the existing pipeline
      metric set (achieved rates derivable from :9090 alone — spec)
- [ ] 1.4 `BenchmarkPublishSnapshot` with a fixed-latency fake publisher:
      workers=1 vs 16 shows ~Nx wall-clock improvement (documents the
      instrument ceiling math)

## 2. JetStream metrics wiring (`cmd/semboids`)

- [ ] 2.1 Reorder `main.go`: create `MetricsRegistry` before
      `connectToNATS`; construct the client with
      `natsclient.WithMetrics(registry)` (D3 — wire, don't build)
- [ ] 2.2 Live verify on isolated NATS (:24222): scrape :9090 and confirm
      `consumer_pending_messages` / `consumer_delivered_total` appear for
      the graph-ingest consumer and `stream_messages` for the ENTITY
      stream (30s poller — allow one interval)

## 3. E2E latency probe (`internal/boidgraph`) — TDD

- [ ] 3.1 Failing tests: latency = observation time − `observed_at` from
      the entity payload; 1-in-N sampling honored (default 10,
      configurable); malformed/non-boid entries skipped without error;
      probe lifecycle tied to the publisher service, not UI clients
- [ ] 3.2 Implement: ENTITY_STATES KV watcher on boid keys (reuse the SSE
      bridge's watch pattern, separate watcher — D4), Prometheus histogram
      `boids_graph_e2e_latency_seconds`, sampling config
- [ ] 3.3 Integration test (testcontainer, `-tags=integration`): sim +
      graph-ingest running, zero SSE clients — histogram populates and
      upper quantiles track an induced backlog

## 4. Sweep tooling

- [ ] 4.1 Sweep script + Taskfile target (`task sweep HZ=<n> [WINDOW=90]`):
      set dial via `PUT /boids/graph/hz`, 30s warm-up, hold window, scrape
      :9090 at boundaries, emit per-window summary — achieved
      snapshots/entities per second, drops, `consumer_pending_messages`
      trend, e2e p50/p99, physics fps (fps < 30 marks the window invalid)
- [ ] 4.2 Classification in the summary per the D5 attribution matrix
      (publisher-bound / ingest-bound / downstream-lag / rejection-loss),
      with the raw signals printed so the doc can quote them

## 5. Instrument validation (200-boid row)

- [ ] 5.1 Control run `graph_publish_workers: 1`: reproduce the ~21.6/s
      ceiling from `graph-dial-first-look.md` (validates the sweep tooling
      against known data)
- [ ] 5.2 Default workers: walk dial {1, 5, 10, 30, 60, 90} — expect 30Hz
      to achieve 30/30 with zero drops (spec scenario "Fan-out raises the
      instrument ceiling"); record where the 200-boid row now saturates
      and classify it; addendum note in `graph-dial-first-look.md`
- [ ] 5.3 `task check:push` green (lint, race, integration, cross-compile)

## 6. Melt campaign

- [ ] 6.1 Boid-count rows {500, 1000, 2000} via config restart per row;
      walk each row's dial until a sustained ingest-bound window (melt) or
      the row tops out publisher-bound (record which); pprof 30s CPU at
      each melt candidate and one dial step below
- [ ] 6.2 Results doc `docs/perf/melt-campaign-<date>.md`: per-row tables
      (dial → achieved, drops, pending trend, e2e quantiles,
      classification), pprof highlights, melt points stated with evidence
- [ ] 6.3 File upstream from the evidence: graph-ingest
      processing-duration histogram enhancement (gap verified in beta.137);
      any melt bottleneck found (component + profile + repro, house
      style); `WithMetricsInterval` option only if 30s polling actually
      hurt attribution
- [ ] 6.4 `openspec validate load-dial --strict`; README status/roadmap
      update; archive the change

## 7. Follow-up (blocked on semstreams#470)

- [ ] 7.1 When #470 ships: swap the worker pool for the async publish API
      behind the same `publishSnapshot` seam (D2), keep ordering tests
      green, re-run the 200-boid row for parity, drop or repurpose
      `graph_publish_workers` accordingly
