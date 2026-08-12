# Design: migrate-semstreams-beta160

## Context

See proposal.md for motivation. Facts that shape the approach, all verified against a
two-tag diff (`v1.0.0-beta.158`…`v1.0.0-beta.160`) at the release commit `8403a221`:

- The ENTITY-stream Graphable lane survives: graph-ingest still consumes JetStream
  ports and applies arrivals through `MergeEntity` → `MergeTriples` (predicate-level
  merge, absent predicates preserved). The stream fact-projection step
  (`prepareFactProjection`) is validation + subject-fill only — it does NOT apply
  reconcile semantics. Upstream's own `configs/agentic.json` runs the stream lane and
  the mutation port side by side.
- The admitted mutation op set is closed at `entity.create`, `entity.reconcile`,
  `triple.append`, `entity.delete`. `triple.remove` requests get no responder.
  `pkg/projection.MutationClient` is the only public client seam
  (`internal/graphmutation` is unimportable); reconcile does its authoritative read
  internally, delete requires the caller to supply a nonzero revision, and the client
  never retries for the caller. `revision_mismatch` is a definite non-commit;
  `commit_unknown` must never be blindly retried.
- Flow validation requires exactly one required `graph_mutations` nats-request
  provider input on graph-ingest (`graph.mutation.>`, interface
  `semstreams.graph.mutation/v1`) per flow, and every graph-mutating component must
  declare a compatible requester output.
- Every port moves to the strict `PortDefinition` envelope; `types.ServiceConfig`
  drops `Name` and rejects unknown fields; ordinary streams must declare
  `max_age` + `max_bytes` + `discard`; the config-KV gate treats an equal-or-older
  file `version` as "KV wins".
- Unchanged and load-bearing for us: `pkg/graphview` (byte-identical), the rule
  processor's runtime-reconfig trio, `pkg/lifecycle`, `service.Dependencies`,
  `PublishBatchToStream`.

## Goals / Non-Goals

**Goals:**

- Land on `v1.0.0-beta.160` with every retired surface we touch replaced by its
  canonical successor — no app-side parallel paths.
- Preserve the load-generator mission unchanged: batch stream publishing at the dial,
  physics hot path untouched (ADR-001).
- Convert both silent runtime breaks (dead `triple.remove` responder, fenced
  `entity.delete`) into typed, tested paths before they can fail silently.
- Fresh-NATS product proof plus a perf re-baseline that separates migration effects
  from beta.159's add-lane dedup.

**Non-Goals:**

- See proposal Non-goals. Additionally at design level: no generic mutation-client
  abstraction beyond the two narrow call sites; no retry framework (two bounded loops
  suffice); no exercising of `MaxPayload()`/response-size handling (our mutation
  payloads are tiny).

## Decisions

### D1 — Bulk snapshots stay on the ENTITY stream lane

Boid snapshots keep publishing as batched Graphables to `entity.boid.upsert`.
Alternative — moving snapshots to `entity.reconcile` request/reply — is rejected:
reconcile performs an internal authoritative read per call, so 200 boids × dial Hz
becomes 2 RTTs per entity per snapshot, reintroducing exactly the serial-RTT ceiling
the async batch publisher was built to remove. The stream lane is
supported-but-not-emphasized upstream; if the product proof surfaces a deprecation
signal, that becomes a filed upstream conversation (Product Boundary), not an
app-side redesign.

### D2 — Neighbor-empty clears via `Reconcile` with an empty desired set; the tracker stays

`TripleRemover` (raw `graph.mutation.triple.remove` requests) is replaced by a
`pkg/projection.MutationClient` configured with one app-local projection contract:

- Contract name `semboids-neighbors`, entity pattern matching boid entity IDs, one
  predicate group `neighbors` with `Mode: reconcile`, predicates
  `["flock.neighbor.of"]`.
- On a non-empty→empty transition, the publisher coordinator calls
  `Reconcile{Contract: "semboids-neighbors", Group: "neighbors", EntityID: <boid>,
  Desired: nil}`.

The `prevHadNeighbors` transition tracker **stays**: the stream lane is still
merge-only (verified, Context), so clearing remains caller-initiated, and issuing a
reconcile per boid per snapshot is rejected for the same RTT reasons as D1. What
retires is the raw-wire seam and its copied subject string. Upstream semboids#578 is
closed by this — comment with the verification evidence once `TestNeighborEmptyGate`
passes on the native path.

Retry policy: on `revision-conflict` (classified via `errs`), retry up to 3 times
(the client re-reads internally each call). On `commit_unknown`, stop, log, and
count — never blind-retry. A clear that exhausts retries is logged and counted; the
next emptying transition re-arms it, and `flock.neighbor.count = 0` remains the
pane's reset sentinel meanwhile.

Consistency window (pre-existing, not a regression): the reconcile applies via
request/reply while earlier snapshot messages may still sit in the consumer backlog;
under deep melt backlog a pre-empty snapshot can re-merge edges after the clear. The
old `triple.remove` had the same window. The revision fence narrows it slightly, and
the count sentinel plus the next transition re-clear bound the visible effect.

### D3 — Cull reclaim becomes `ReadAuthoritative → Delete`, bounded retry

`internal/sim/lifecycle.go` swaps its raw `graph.mutation.entity.delete` request for
the client pairing: read the authoritative revision, delete at that revision. On
`revision-conflict`, re-read and retry up to 5 times — conflicts are transient
because a culled boid leaves physics immediately, so its snapshot writes stop and the
retry converges once in-flight publishes drain. On `commit_unknown` or retry
exhaustion: log + increment a reclaim-failure metric and move on (the entity lingers
as a `phase=culled` zombie; it is observable, and a fresh cull cycle does not depend
on it). Both round trips run inside the existing drain-pool operation so reclaim
concurrency stays bounded (population-control spec).

Alternative — unbounded retry until success — rejected: a permanently ambiguous
outcome would wedge a pool slot forever.

### D4 — One `MutationClient`, narrow interfaces at the seams

The sim component constructs a single `MutationClient` (NATS client from
`Dependencies`, the `semboids-neighbors` contract, default timeout) and hands
narrow slices to the two consumers, preserving the existing test-seam pattern:

- publisher: a `PredicateReconciler`-shaped interface (replaces `TripleRemover`);
- cull reclaim: `AuthoritativeReader` + `EntityDeleter`-shaped interfaces.

Tests fake the narrow interfaces exactly as they fake `StreamPublisher` today.

### D5 — Config rewrite: strict envelopes, provider port, stream bounds, version

`configs/flock.json` moves wholesale to the new grammar:

| Port (component) | Old | New `config` |
|---|---|---|
| `frames` out (sim) | flat `subject`, `type: nats` | `{kind: nats, subject: boids.frames}` |
| `entity_stream` in (graph-ingest) | flat `subject` + `stream_name` | `{kind: jetstream, stream_name: ENTITY, subjects: ["entity.>"]}` |
| `graph_mutations` in (graph-ingest) | — (new, mandatory) | `{kind: nats-request, subject: "graph.mutation.>", interface: {type: semstreams.graph.mutation, version: v1}}`, `required: true` |
| `entity_states` out (graph-ingest) | `type: kv-write`, `subject` | `{kind: kv-write, bucket: ENTITY_STATES}` |
| `zone_events` in / `steering` out (rule) | flat nats | `{kind: nats, subject: …}` |
| `nats_input` in (websocket) | flat nats | `{kind: nats, subject: boids.frames}` (exactly one subject — already true) |
| `websocket_server` out (websocket) | URL-in-subject, `type: network` | `{kind: network, protocol: http, host: 0.0.0.0, port: 8081}` |
| `entity_watch` in (graph-index, clustering) | `type: kv-watch`, `subject` | `{kind: kv-watch, bucket: ENTITY_STATES}` |
| 4 index outs (graph-index) | `type: kv-write`, `subject` | `{kind: kv-write, bucket: <INDEX>}` |
| clustering ports | watch in + communities out | mirror upstream `DefaultConfig`: `entity_watch` kv-watch + `entity_states`/`outgoing_index`/`incoming_index` kv-read inputs, `graph_mutations` nats-request required output + `communities` kv-write out |

The sim declares a `graph_mutations` requester output (same envelope as clustering's)
— it is a graph-mutating component (D2/D3) and the flow contract requires the
declaration. Clustering mirrors `DefaultConfig` fully because upstream's defaulting
logic does not merge defaults into a non-empty lane and whether the kv-read inputs
are load-bearing is undocumented — mirroring is the safe side, and the product proof
verifies detections still fire.

Services block: drop every `"name"` field (unknown fields now rejected; the map key
is the identity). `streams.ENTITY` declares `max_age: "24h"`, `max_bytes: 2 GiB`,
`discard: "old"` — the stream is a transport buffer, not an archive; 168h retention
was never load-bearing and an explicit byte bound with discard-old degrades exactly
the way a load generator wants (oldest backlog evicts; `discard: "new"` would 503
the firehose). Top-level `version` bumps `1.1.0` → `1.2.0` (bare semver — the
comparator rejects prerelease suffixes; fresh NATS makes first boot clean regardless,
the bump is belt-and-braces for later restarts).

`cmd/semboids/main.go`: `ensureServiceManagerConfig` drops the `Name` field.
`internal/sim/component.go`: flat `Ports.Outputs[0].Subject` +
`BuildPortFromDefinition` replaced with `PortDefinition.Resolve(DirectionOutput)` and
`Port.Facts()` for the subject read.

### D6 — Test fixture migration and the rewritten gate test

All integration tests that build components in-proc move their inline port configs to
the new grammar (mechanical, same mapping table). `TestNeighborEmptyGate` is
rewritten to pin both halves of the new contract: (a) a raw stream publish of an
empty neighbor set still leaves resident edges (the merge-only limitation persists —
if this half ever fails, the tracker can retire); (b) the `Reconcile`-empty path
clears ENTITY_STATES and INCOMING (the native path works). Scenario names track the
graph-snapshots delta spec.

## Risks / Trade-offs

- [Stream-lane deprecation later] → Named ambiguity, not a blocker: product proof
  exercises it end-to-end on beta.160; if upstream signals retirement, file the
  conversation with our load-dial evidence attached.
- [Clustering kv-read inputs undocumented] → Mirror `DefaultConfig` (D5); product
  proof checks live detections + COMMUNITY_INDEX population; file upstream only if
  mirrored config still starves.
- [Reconcile/delete racing deep backlog resurrects edges briefly] → Pre-existing
  window (D2); count sentinel + re-clear bound it; not a regression, documented.
- [`commit_unknown` leaves a culled zombie entity] → Bounded by metric + log (D3);
  zombies are observable in ENTITY_STATES and harmless to physics.
- [Perf numbers shift for substrate reasons (beta.159 add-lane dedup)] → Re-baseline
  is a deliverable, not a regression gate; compare shapes (ingest ceiling, index amp,
  e2e latency) and attribute before judging, per the beta.149 arc lesson.
- [2 GiB / 24h ENTITY bound evicts under extreme melt campaigns] → Intentional:
  discard-old eviction is the correct failure mode for a load instrument; bounds are
  config, raised per campaign if a specific run needs deeper retention.

## Migration Plan

Guide-ordered (upstream migration doc), one branch `migrate-semstreams-beta160`:

1. Pin `v1.0.0-beta.160`; full compile harvest (`go build ./... && go vet ./...`)
   to enumerate every deleted-API hit; fix mechanical compile breaks (main.go
   ServiceConfig, sim port construction).
2. Rewrite `configs/flock.json` (D5); `semboids --validate` until strict config +
   flow validation pass.
3. Write-path migration (D2/D3/D4) with unit tests on the narrow seams.
4. Test-fixture grammar migration + `TestNeighborEmptyGate` rewrite (D6).
5. Fresh NATS (`task dev:nats:clean` / demo container recreate); `task check` then
   `task check:push` (race + integration).
6. Product proof on the running demo: ingest at the dial, rule toggles, spawn/cull
   chain, community colours, SSE stream, restart, then a dial sweep vs the beta.158
   baseline recorded in `docs/perf/beta160-migration-2026-08-12.md`. Close
   semstreams#578 with evidence; comment on the census row if useful.

Rollback: revert the branch; state is disposable by design (fresh NATS both
directions), so rollback is `git revert` + container recreate — no data migration
either way.

## Open Questions

None blocking. Two watch items intentionally deferred to the product proof (they
cannot change specs, approach, or task breakdown — only trigger upstream filings):
whether clustering functions identically with the mirrored kv-read inputs, and
whether the stream lane shows any deprecation friction at our rates.
