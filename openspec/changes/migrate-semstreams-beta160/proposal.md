# Proposal: migrate-semstreams-beta160

## Why

SemStreams `v1.0.0-beta.160` (2026-08-12) is the intentionally-breaking graph/framework
foundation checkpoint — no shims, no aliases, and a hard requirement that every adopting
downstream starts on freshly provisioned NATS storage. SemBoids sits on beta.158; staying
behind forfeits the substrate's canonical mutation contract and blocks every future tag.
The upstream cutover census already sized us: 4 files of legacy mutation wire, zero other
debt. Adopting now, while the flock's state is 100% physics-derived and a wipe costs
nothing, is the cheapest this migration will ever be.

## What Changes

- **BREAKING (runtime, compiles clean)**: `graph.mutation.triple.remove` no longer has a
  responder — the admitted op set closed at `entity.create/reconcile/delete` +
  `triple.append`. The publisher's neighbor-set-empty path moves to
  `pkg/projection.MutationClient.Reconcile` with an empty desired set over a declared
  neighbor predicate group — the canonical replacement for the retired op, and the typed
  resolution of semstreams#578. The raw-wire `TripleRemover` seam retires; the
  `prevHadNeighbors` transition tracker stays (verified at beta.160 source: the stream
  ingest lane is still merge-only, so clearing remains caller-initiated on transitions).
- **BREAKING (runtime, compiles clean)**: `graph.mutation.entity.delete` now requires a
  nonzero `expected_revision` (CAS-fenced). The cull-reclaim path moves from a raw NATS
  request to the `MutationClient` `ReadAuthoritative → Delete` pairing; the caller owns
  `revision_mismatch` retries and must never auto-retry `commit_unknown`.
- **BREAKING (compile)**: `types.ServiceConfig.Name` removed and unknown JSON fields
  rejected — `configs/flock.json` services entries and `ensureServiceManagerConfig` in
  main.go both change. `component.BuildPortFromDefinition` and flat
  `Ports.Outputs[0].Subject` retired — sim port construction moves to
  `PortDefinition.Resolve`/`Port.Facts()`.
- **BREAKING (config validation)**: every port in `configs/flock.json` rewrites to the
  strict `PortDefinition` envelope (`{name, required, config: {kind, ...}}`); graph-ingest
  gains the mandatory `graph_mutations` nats-request provider input and the sim declares
  the matching requester output; `streams.ENTITY` declares `max_bytes` and
  `discard: "old"`; graph-clustering's explicit ports mirror its new `DefaultConfig`;
  top-level config `version` bumps to bare `1.2.0`. Integration-test fixtures embedding
  flat port configs update to the same grammar.
- **Unchanged by verification, not assumption** (two-tag diff at the release commit):
  `pkg/graphview` (byte-identical — the whole read side is untouched), the rule
  processor's runtime-reconfig trio (live rule toggles keep working), `pkg/lifecycle`,
  the service plane, `PublishBatchToStream`, and the ENTITY-stream Graphable batch lane
  itself — the load-dial firehose survives as-is.
- **Operational**: fresh NATS storage per the upstream adoption contract (local
  containers are ephemeral; `task dev:nats:clean` covers the dev instance), then a full
  product proof (demo end-to-end, `task check:push`) and a dial-sweep perf re-baseline
  against beta.158 recorded in `docs/perf/` (beta.159's add-lane dedup may legitimately
  shift ingest numbers).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `graph-snapshots`: the "emptying a neighbor set clears the edges" requirement currently
  pins `graph.mutation.triple.remove` as the clearing mechanism; it changes to pin the
  typed reconcile-with-empty-desired-set mutation (transition tracking stays, now with
  revision-fence retry semantics).
- `boid-lifecycle`: the reclaim requirement pins a raw `graph.mutation.entity.delete`
  request; it changes to pin the revision-fenced typed delete (read authoritative
  revision, then delete at that revision).
- `population-control`: the churn requirement names `graph.mutation.entity.delete` for
  culled boids and its scenarios assume single-round-trip deletes; it changes to the
  revision-fenced pairing (two round trips per reclaim) with the drain-pool contract
  otherwise intact.

## Non-goals

- No adoption of the request/reply mutation lane for bulk snapshot publishing. Boid
  snapshots stay on the batch ENTITY-stream Graphable lane — it survives in beta.160,
  and per-entity CAS request/reply at 200 boids × dial Hz would gut the load-generator
  mission (ADR-001). If upstream ever deprecates the stream lane, that is a filed
  conversation, not an app-side workaround.
- No new UI work. The browser talks only to semboids' own REST/SSE/WebSocket surfaces.
- No rules rework. Zone-steering rules use `expression`/`publish`/`lifecycle_transition`
  only — none of the retired rule surfaces.
- No re-litigation of retired perf items (the ~4% index-side residual, the #590 soak) —
  tracked separately.
- No graph-clustering semantic/LLM features newly exposed in beta.160
  (`semantic_edges`, `enhancement_workers`).

## Physics hot path

Untouched. Every changed write path (neighbor-empty reconcile, cull-reclaim delete) runs
on the off-loop publisher coordinator or the bounded drain pool; the 30Hz tick loop stays
off NATS, rules, and graph-ingest per ADR-001. The batch snapshot publisher — the only
high-rate producer — is structurally unchanged.

## Substrate gaps

None to file. This migration consumes upstream fixes rather than surfacing gaps:
semstreams#578 is resolved by `Reconcile` with an empty desired set (close it with a
verification note once the native path is proven here). Watch items that could become
issues if verification fails: whether graph-clustering functions without its three new
default kv-read inputs when ports are declared explicitly, and whether the stream-ingest
lane remains supported at our rates — both get settled by the product-proof run first.

## Impact

- **Code**: `internal/boidgraph/publisher.go` + `payload.go` (neighbor-empty path,
  tracker retirement), `internal/sim/lifecycle.go` + `component.go` + `drainpool.go`
  (reclaim path, port construction, mutation-client wiring), `cmd/semboids/main.go`
  (ServiceConfig), `componentregistry/register.go` (unchanged registration API,
  verified).
- **Config**: `configs/flock.json` (wholesale port rewrite, services block, streams
  bounds, version), zone-steering rule files unchanged.
- **Tests**: `TestNeighborEmptyGate` rewrites from workaround-pin to native-path proof;
  ~12 integration tests' inline component configs move to the new port grammar.
- **Dependencies**: `go.mod` pins `v1.0.0-beta.160`.
- **Operations**: fresh NATS storage (dev + demo containers), config version bump
  (KV config gate), perf re-baseline in `docs/perf/`.
