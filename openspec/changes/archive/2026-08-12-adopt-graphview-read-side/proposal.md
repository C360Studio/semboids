# Adopt graphview for the read-side graph stream

## Why

`internal/api/graphstream.go` hand-rolls a KV-watch → coalesce → SSE fan-out
bridge, opening **two `WatchAll` consumers per connected SSE client**
(ENTITY_STATES and COMMUNITY_INDEX, both scoped to `r.Context()`). That is an
app-side parallel path to substrate, which this project forbids: *"never
reimplement substrate... if a graph primitive is missing, file a SemStreams
issue; don't carve an app-side parallel path."*

When the bridge was written the primitive did not exist, so hand-rolling was
correct and we filed the gap upstream as **semstreams#579**. SemStreams answered
it in **beta.155**: `pkg/graphview` (ADR-081, PR#585) — a shared read-side view
subscription that owns one `WatchAll` per bucket, decodes and contract-validates
once, coalesces ahead of fan-out, and serves N local subscribers with atomic
snapshot-plus-subscribe and per-subscriber backpressure. ADR-081 names our
`graphstream` as its originating evidence. The gap we reported is closed; keeping
the workaround now *is* the boundary violation.

The second reason is what semboids is for. It is the instrument that exercises
production SemStreams code paths under load — that is how #480, #562, and #579
were found. A load generator that routes around the substrate is not testing the
substrate. `pkg/graphview` is currently unexercised by any load generator and is
headed for products that serve many concurrent operators a live graph view;
semboids adopting it makes us the first real stress of ADR-081.

**This is not a performance change, and the proposal records that deliberately so
no one later reads a win into it.** Measured on beta.155: consumer count scales
exactly `3 + N` on ENTITY_STATES and `N` on COMMUNITY_INDEX for N clients
(structural, exact, no leak) — but the throughput cost at demo scale is *below
the noise floor* (interleaved, settle-waited A/B: deltas +1824 / +1830 / −919
msgs/s, mean +912, stdev 1585). At the single-viewer scale semboids is actually
used at, the expected runtime gain is zero. The justification is boundary
discipline and instrument coverage, nothing else.

## What Changes

- Replace the per-client `watchBucket` helper with **one process-lifetime
  `graphview.View` per bucket** (ENTITY_STATES, COMMUNITY_INDEX), constructed and
  owned by the API service and injected — ADR-081 specifies explicit ownership,
  no process-global registry.
- Each SSE connection becomes a **`SnapshotAndSubscribe` subscriber** instead of a
  NATS consumer: snapshot and registration are atomic at one view sequence, so
  the initial full sync carries no gap, dup, or inversion against the delta
  stream that follows.
- Per-entity decode moves into an injected `DecodeFunc` (the existing
  `isBoidKey` filter maps to `keep=false`; community values decode separately),
  so decode and contract validation happen **once per write** rather than once
  per write per client.
- Backpressure semantics change from *N independent NATS pending buffers, each
  able to trip `nats: slow consumer`* to **per-subscriber last-writer-wins
  coalescing bounded by changed-key cardinality** — a slow browser degrades to
  fewer, fresher batches and is never disconnected, and never blocks the view
  watcher or other clients.
- Adopt graphview's **degraded-path signals**: readiness gating on initial
  replay, applied-revision watermark, and fail-closed termination on watcher
  loss (today a lost watcher silently stops delivering).
- Wire the `graphview.Hooks` seam to our Prometheus registry (caught-up,
  watermark, subscriber count, coalesced/pending drops, poison, watcher-lost) so
  the read side gets the observability the write side already has.
- Keep the ~500ms browser flush cadence and the existing SSE wire format
  unchanged — the UI must not need edits.

No **BREAKING** changes: the HTTP contract (`GET /boids/graph/stream`), the batch
payload shape, and the pane's behavior are all preserved.

## Capabilities

### New Capabilities

None. This is substrate adoption beneath an existing capability, not a new one.

### Modified Capabilities

- `flock-communities`: the *"Communities stream to the browser"* requirement
  currently specifies an SSE endpoint that "watches ENTITY_STATES ... and
  COMMUNITY_INDEX" with an initial full sync — wording that describes the
  per-connection watcher. It changes to a shared view subscription: initial sync
  becomes an atomic snapshot at a view sequence, and the bounded-traffic
  guarantee gains an explicit slow-subscriber clause (coalesce, never
  disconnect, never block peers) plus a fail-closed watcher-loss signal.

`graph-pane` is **not** listed: its requirements govern what the pane renders
(sigma nodes at real positions, community recolor, graceful empty/degraded
states), and all of them must continue to hold unchanged. The
*"Pane degrades gracefully"* requirement needs verification against the new
fail-closed watcher-loss path — `EventSource` auto-reconnect must keep satisfying
"never an error state that requires reload" — but that is a verification
obligation for tasks, not a requirement change.

## Impact

- **Code**: `internal/api/graphstream.go` (the `watchBucket` helper is deleted;
  `handleGraphStream` becomes a subscriber loop), `internal/api/service.go`
  (view construction, ownership, and shutdown), plus the bridge-state coalescing
  that `graphview` now subsumes.
- **Dependencies**: adds `github.com/c360studio/semstreams/pkg/graphview` — already
  available at the pinned `v1.0.0-beta.155`, no version bump required.
- **Tests**: `internal/api/graphstream_integration_test.go` extends to cover
  shared-view fan-out (N clients, one consumer set), snapshot/delta coherence,
  slow-subscriber isolation, and watcher-loss termination.
- **Metrics**: new read-side series on `:9090` via the Hooks seam.
- **Physics hot path**: **not touched.** `graphstream.go` is HTTP-serve-side only;
  the 30Hz loop never enters it, and this change adds nothing to NATS, rules, or
  graph-ingest. The hybrid split (ADR-001) is unaffected.
- **UI**: no changes expected — wire format and cadence are preserved. Any
  observed pane difference is a defect in this change, not an accepted cost.
- **Upstream issues to file**: none anticipated; this change *consumes* the
  answer to #579. Gaps found while adopting go upstream as SemStreams issues, not
  app-side shims. (semstreams#590, the graph-index readiness gate that starves
  clustering, is open and unrelated — it affects community *content*, not this
  transport.)

## Non-goals

- **Not a performance optimization.** No throughput or latency target is claimed
  or accepted as a success criterion; the measured effect at demo scale is below
  noise. A future multi-viewer hosting scenario would be the place to measure.
- **Not a change to the dial, the snapshot publisher, or `graph-snapshots`.**
  The ENTITY_STATES write firehose is untouched — that is the load-generation
  function and it stays exactly as-is.
- **Not adopting `graphview` anywhere else.** `internal/boidgraph/probe.go` and
  `internal/sim/lifecycle.go` also hold raw `WatchAll` handles; both are single
  process-lifetime watchers, not per-client fan-out, so they are correct as-is
  and out of scope.
- **Not fixing semstreams#590.** Clustering starvation is a separate, filed
  upstream question.
- **Not changing the SSE wire format or the UI.**
