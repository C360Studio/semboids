# Design: adopt graphview for the read-side graph stream

## Context

`handleGraphStream` today builds per connection: a `bridgeState` (coalescing
map), then two calls to `watchBucket` — one for ENTITY_STATES filtered by
`isBoidKey`, one for COMMUNITY_INDEX — each opening a `kv.WatchAll` scoped to
`r.Context()` and pumping raw `(key, value, deleted)` into `bridgeState`. A
500ms ticker flushes the coalesced map to the wire as a `graphBatch`.

`pkg/graphview` (beta.155) replaces the watcher layer only. Its shape:

```go
func New[T any](source WatcherSource, decode DecodeFunc[T], opts ...Option) (*View[T], error)
func (v *View[T]) Start(ctx context.Context) error
func (v *View[T]) SnapshotAndSubscribe(ctx context.Context) (Snapshot[T], *Subscription[T], error)
func (s *Subscription[T]) Deltas() <-chan []Delta[T]
func (s *Subscription[T]) Err() error   // nil | ctx err | ErrViewStopped | ErrWatcherLost
```

Constraints carried in from the proposal: the wire format, the ~500ms flush
cadence, and every `graph-pane` requirement must survive unchanged; the physics
hot path is not involved; and this is explicitly not a performance change.

One upstream constraint shapes the decode seam: ADR-081 forbids
`UnmarshalEntityStateTrusted` inside a view — that fast path is reserved for
graph-ingest as sole writer. We are an external reader and use the validating
decode, which is what we do today anyway.

## Goals / Non-Goals

**Goals:**

- One `WatchAll` per bucket for the whole process, regardless of client count.
- Decode and contract-validate each write once, not once per connected client.
- Preserve the SSE wire contract and the pane's behavior exactly.
- Preserve graceful degradation when COMMUNITY_INDEX does not yet exist.
- Gain honest degraded-path signals (watcher loss, readiness, poison) and expose
  them as metrics.

**Non-Goals:**

- Any throughput or latency improvement (see proposal — measured below noise).
- Changing the dial, the publisher, or anything write-side.
- Adopting `graphview` at the other two `WatchAll` sites (`boidgraph/probe.go`,
  `sim/lifecycle.go`) — both are single process-lifetime watchers, not per-client
  fan-out.
- Changing the UI.

## Decisions

### D1: Two typed views, not one union view

`View[T]` is generic over one decoded type, and our buckets carry different
shapes: ENTITY_STATES yields boid position/neighbor state, COMMUNITY_INDEX
yields a community assignment. We construct `View[boidEntity]` and
`View[communityAssignment]` with separate `DecodeFunc`s.

*Alternative rejected:* one view over an interface or union type with a
bucket-switching decode. It would erase compile-time typing at the decode seam,
force an interface allocation per entry, and give the projection a heterogeneous
value type for no benefit — the two buckets have independent lifecycles anyway
(D3).

### D2: Views are process-lifetime and owned by the API `Service`

Constructed in the service's start path, stopped in its shutdown path, injected
into the handler. ADR-081 specifies explicit ownership with no process-global
registry, and this is what actually delivers the fan-out win: the view must
outlive any single connection.

*Alternative rejected:* per-request construction — that is precisely the shape
being removed.

### D3: COMMUNITY_INDEX view starts lazily and supervises itself

Today a failed `watchBucket` on COMMUNITY_INDEX is non-fatal: we log and stream
without communities, and the pane shows neutral colors until assignments arrive.
That behavior is a `graph-pane` requirement ("Pane degrades gracefully without
graph data", "Late-arriving substrate"), so it must survive.

The bucket may genuinely not exist for a long time — semstreams#590 means
clustering can be starved indefinitely, so "wait for the bucket at boot" would
be waiting on something that may never come.

Therefore: attempt the COMMUNITY_INDEX view at start; if the bucket is absent,
leave it unset and retry on a background interval, wiring subscribers in once it
succeeds. Connections opened before it exists serve entity data with no
community values and pick up colors when the view comes up — no reload.

**Amended during build (2026-07-20).** This decision originally made the
ENTITY_STATES view a hard start error. That was wrong: ENTITY_STATES is created
by graph-ingest shortly *after* the API service starts — which is exactly why
`internal/boidgraph/probe.go` already has to `WaitForBucket` — so a hard error
would make boot race-dependent on component start order. It also misdescribed
the pre-change behavior, which is a per-request `503`, not a boot failure.
**Both** views are therefore supervised on the same retry loop, differing only
in what their absence means to a client: no entity view yields `503`
(pre-change behavior preserved exactly), no community view yields
neutral-colored nodes.

*Alternative rejected:* `WaitForBucket` at boot (blocks startup on something
that may never arrive); *and* per-connection construction of just the community
view (reintroduces the per-client watcher we are removing).

### D4: One subscriber goroutine per connection, selecting over both delta lanes

Per connection: `SnapshotAndSubscribe` on each available view, seed
`bridgeState` from the two snapshots, then a single loop `select`ing over both
`Deltas()` channels plus the flush ticker and request context. One goroutine per
connection, two subscriptions — down from two NATS consumers.

Seeding from the snapshot rather than replaying gives us the G1 guarantee
directly: every key at ≤ S in the snapshot, every delta > S in the stream, no
gap or duplicate. That is strictly stronger than today's reliance on "initial
values arrive before live updates (WatchAll semantics)".

### D5: `bridgeState` survives as the wire-batch assembler, minus decoding

It stops receiving raw bytes and stops decoding; it consumes typed `Delta[T]`
and continues to coalesce latest-wins per entity into the 500ms flush. Deleting
it would be scope creep — it owns the wire shape (`graphEntity`, `graphBatch`)
that the proposal commits to preserving.

This leaves two coalescing stages (view tick, then flush). That is not
redundant: the view tick coalesces across *all* subscribers once, the flush
produces the per-client wire batch. To keep the spec's "bounded by the flush
interval" guarantee binding, the view tick is set **below** the flush interval
(see Open Questions).

### D6: `isBoidKey` becomes `keep=false` in the decode

`DecodeFunc` returns `keep=false` for non-boid keys, which maps them to absence
rather than filtering downstream. Non-boid entities never enter the projection,
so the view holds only what the pane can render.

### D7: Watcher loss ends the connection fail-closed

When `Deltas()` closes, `Err()` distinguishes clean unsubscribe from
`ErrViewStopped` and `ErrWatcherLost`. On a staleness signal we end the SSE
response rather than silently serving a frozen projection — today a lost watcher
just stops delivering with no signal at all.

`EventSource` reconnects automatically, and the next attach triggers the view's
`Restart()` with ghost-key reconciliation. **This is the one place the change
could violate a `graph-pane` requirement** ("never an error state that requires
reload"), so it carries an explicit verification obligation in tasks.

### D8: Metrics through the `Hooks` seam

`graphview.Hooks` callbacks feed our existing Prometheus registry: caught-up,
applied-revision watermark, subscriber count, coalesced/pending drops, poison,
watcher-lost. Hooks run on the watcher and ticker goroutines and must be fast
and non-blocking — counter/gauge sets only, no allocation-heavy work.

## Risks / Trade-offs

- **Snapshot capture cost at high boid counts** → `SnapshotAndSubscribe` copies
  the projection under the view lock. At 14k+ boids that is a large map copy on
  every client connect, briefly blocking the watcher. ADR-081 already requires
  capture-under-lock / deliver-outside; mitigation is to verify connect latency
  at a high boid count rather than assume, since semboids reaches populations
  most consumers will not.
- **A projection now exists with zero clients** → the view runs and holds full
  boid state even when nobody is watching, which the per-client design did not.
  Net memory falls for N ≥ 1 and rises for N = 0. Accepted as the cost of the
  shared seam; lazy-start on first subscriber is the escape hatch if it bites.
- **Two coalescing stages add latency** → worst case view tick + flush. Mitigated
  by keeping the view tick below the flush interval; bounded by design, and the
  pane already tolerates staleness by spec.
- **Slow clients no longer surface as `nats: slow consumer`** → they now coalesce
  silently instead. This is the intended improvement, but it removes a signal we
  previously saw in logs; the Hooks drop counters replace it, which is why D8 is
  in scope rather than deferred.
- **COMMUNITY_INDEX may never appear** (semstreams#590) → the retry loop must be
  cheap and must never escalate to an error state; the pane's neutral-color path
  is the steady state, not an exception.

## Migration Plan

Serve-side only — no data migration, no bucket format change, no coordination
with the write path. Deploy is a normal restart. Rollback is a revert of the
change; the SSE wire format is unchanged in both directions, so a rolled-back
backend serves the same UI without edits.

## Open Questions

1. **View tick interval** — 250ms (clearly below the 500ms flush, more view-side
   work) versus matching 500ms (risking beat interference between the two
   timers). Decide by measuring end-to-end pane latency at dial 1Hz and 10Hz;
   default to 250ms absent evidence.
2. **Lazy versus eager view start** — eager is simpler and is assumed above;
   revisit only if the zero-client projection cost shows up at high populations.
3. **Delta-only subscribe for COMMUNITY_INDEX** — assignments are small and the
   pane can tolerate coloring in on the first delta, so `Subscribe` may suffice
   instead of `SnapshotAndSubscribe`. Resolve during build; snapshot is the safe
   default and preserves "Late-arriving substrate" most obviously.
