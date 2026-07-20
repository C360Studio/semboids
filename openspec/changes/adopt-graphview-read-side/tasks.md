## 1. Baseline — capture what must not regress

- [x] 1.1 Record the pre-change consumer baseline: with the stack running and
      zero SSE clients, capture `consumer_count` for `KV_ENTITY_STATES` and
      `KV_COMMUNITY_INDEX` from `:8222/jsz`, then for N = 1, 2, 4, 8 held-open
      clients. Expect `3+N` and `N` — this is the number the change must flatten.
- [x] 1.2 Capture the current SSE wire output (one batch, pretty-printed) as a
      golden reference for the "wire format unchanged" commitment.
- [x] 1.3 Screenshot the graph pane at dial 1Hz as the visual reference for
      "no UI change".

## 2. Decode seam

- [x] 2.1 Define the boid entity decode: `graphview.DecodeFunc[boidEntity]`
      returning `keep=false` for non-boid keys (replacing `isBoidKey`
      filtering), using the validating decode — `UnmarshalEntityStateTrusted`
      is forbidden here by ADR-081.
- [x] 2.2 Define the community decode: `graphview.DecodeFunc[communityAssignment]`.
- [x] 2.3 Unit-test both decodes: boid key kept, non-boid key mapped to absence,
      malformed value surfaced as poison rather than silently skipped.

## 3. View construction and ownership

- [x] 3.1 Add the ENTITY_STATES `View[boidEntity]` to the API service:
      constructed and `Start`ed in the service start path, `Stop`ped in
      shutdown, injected into the handler (no package-global).
- [x] 3.2 Supervise the ENTITY_STATES view on the same retry loop rather than
      failing start (amended — see design D3): graph-ingest creates the bucket
      *after* this service starts, so a hard error would make boot
      race-dependent. The stream endpoint answers `503` while the view is
      absent, which is the pre-change behavior.
- [x] 3.3 Add the COMMUNITY_INDEX `View[communityAssignment]` with the D3 lazy
      supervisor: attempt at start, on absence retry on a background interval,
      wire in when it succeeds. Absence MUST NOT fail the service or any
      connection.
- [x] 3.4 Set the view tick interval below the 500ms flush (default 250ms per
      design Open Question 1).

## 4. Handler rewrite

- [x] 4.1 Replace `watchBucket` usage in `handleGraphStream` with
      `SnapshotAndSubscribe` on each available view; seed `bridgeState` from the
      snapshots before entering the flush loop.
- [x] 4.2 Rewrite the connection loop as a single goroutine selecting over both
      `Deltas()` channels, the 500ms flush ticker, and `r.Context()`.
- [x] 4.3 Convert `bridgeState.applyEntity` / `applyCommunity` to consume typed
      `Delta[T]` (upsert / delete / poison) instead of raw `(key, value,
      deleted)`; keep the coalescing and wire-shape logic intact.
- [x] 4.4 Handle `Deltas()` closure: inspect `Subscription.Err()` and end the
      response on `ErrWatcherLost` / `ErrViewStopped`; clean unsubscribe on
      client disconnect.
- [x] 4.5 Delete the `watchBucket` helper once no callers remain.

## 5. Observability

- [x] 5.1 Wire `graphview.Hooks` to the existing Prometheus registry: caught-up,
      applied-revision watermark, subscriber count, coalesced/pending drops,
      poison, watcher-lost. Callbacks must be non-blocking counter/gauge sets.
- [x] 5.2 Confirm the new series appear on `:9090` and carry sane values under a
      live run.

## 6. Tests — one per spec scenario

- [x] 6.1 `Consumer count is flat in client count`: N clients connect, assert
      JetStream consumer counts on both buckets equal the zero-client baseline
      for any N.
- [x] 6.2 `Disconnect leaves the shared view running`: all clients disconnect,
      a later client is served an initial sync with no new watcher opened.
- [x] 6.3 `Snapshot and stream do not overlap or gap`: write continuously while
      a client attaches; assert no missed entity change and no redelivery of a
      change already in the snapshot.
- [x] 6.4 `Initial sync then increments` and `Bounded browser traffic`: preserve
      the existing assertions in `graphstream_integration_test.go`.
- [x] 6.5 `Slow client does not stall its peers`: write-gated slow reader beside
      a fast reader; fast client keeps receiving current batches, slow client
      receives coalesced latest-state rather than a backlog.
- [x] 6.6 `Stale projection is not served silently`: induce watcher loss, assert
      the response ends rather than continuing to emit last-known state.
- [x] 6.7 `Stream starts before clustering has ever run`: no COMMUNITY_INDEX
      bucket, connection healthy, entity data flowing, no community values.
- [x] 6.8 `Assignments appear mid-connection`: create the bucket after a client
      has connected; the same connection begins carrying assignments.
- [x] 6.9 Run the suite under `-race`; no sleeps for synchronization — gate on
      Hooks callbacks or channel signals.

## 7. Live verification

- [x] 7.1 Re-run the 1.1 measurement post-change and record the result in the
      change: consumer counts MUST be flat in N. This is the falsifiable claim.
- [x] 7.2 Diff live SSE output against the 1.2 golden reference — wire format
      byte-compatible.
- [x] 7.3 Compare the pane against the 1.3 screenshot at dial 1Hz — no visible
      change.
- [x] 7.4 `Recovery needs no reload`: with the pane open in a browser, induce
      watcher loss and confirm `EventSource` reconnects and the pane resumes
      without user action. This is the D7 risk to `graph-pane`'s "never an error
      state that requires reload" requirement — verify, do not assume.
- [x] 7.5 Measure snapshot-capture cost at a high boid count (spawn to ~14k):
      confirm client connect latency stays acceptable and the watcher is not
      blocked meaningfully. Named as a risk in design; measure rather than assume.
- [x] 7.6 Resolve design Open Question 1 with the 7.5 data: keep 250ms tick or
      adjust, and record the reason.

## 8. Close out

- [ ] 8.1 `task check:push` green (build, vet, gofmt, revive, `-race` unit +
      integration).
- [ ] 8.2 Record in the change that the measured throughput effect remains at or
      below the noise floor — the proposal's non-goal must survive contact with
      the implementation, and a perf win MUST NOT be claimed.
- [ ] 8.3 File any substrate gap found while adopting as a SemStreams issue —
      no app-side shims.
- [ ] 8.4 Commit in conventional splits; `openspec validate --strict` green.
