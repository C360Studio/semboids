# Tasks: migrate-semstreams-beta160

## 1. Pin and compile harvest

- [x] 1.1 Branch `migrate-semstreams-beta160` off main (with the archive PR merged);
      `go get github.com/c360studio/semstreams@v1.0.0-beta.160` (GOPROXY=direct if the
      proxy lags), tidy
- [x] 1.2 Run `go build ./... && go vet ./...` and record the complete error harvest in
      the change dir (`evidence/compile-harvest.md`) — this is the authoritative
      work list per the upstream guide; diff it against the audit's predictions and
      flag any surprise surface
- [x] 1.3 Fix `cmd/semboids/main.go` `ensureServiceManagerConfig`: drop the removed
      `ServiceConfig.Name` field
- [x] 1.4 Fix `internal/sim/component.go` port construction: replace flat
      `Ports.Outputs[0].Subject` + `BuildPortFromDefinition` with
      `PortDefinition.Resolve(DirectionOutput)` / `Port.Facts()` (design D5)
- [x] 1.5 Compile clean: `go build ./... && go vet ./...` green (tests may still fail)

## 2. Configuration cutover

- [x] 2.1 Rewrite every port in `configs/flock.json` to the strict PortDefinition
      envelope per the design D5 mapping table (sim, graph-ingest, rule-processor,
      frames-websocket, graph-index, graph-clustering)
- [x] 2.2 Add graph-ingest's mandatory `graph_mutations` nats-request provider input
      and the sim's matching requester output (interface
      `semstreams.graph.mutation/v1`, subject `graph.mutation.>`, required)
- [x] 2.3 Mirror upstream `DefaultConfig` for graph-clustering's full port set
      (3 kv-read inputs + required `graph_mutations` output + `communities` kv-write),
      keeping `coalesce_ms: 500` on graph-index and all clustering knobs unchanged
- [x] 2.4 Services block: remove every `"name"` field; streams: add
      `max_age: "24h"`, `max_bytes: 2147483648`, `discard: "old"` to ENTITY;
      bump top-level `version` to `1.2.0`
- [x] 2.5 `go run ./cmd/semboids --config configs/flock.json --validate` passes strict
      config + flow validation (fresh NATS not required for --validate; iterate here
      until green)

## 3. Write-path migration

- [x] 3.1 Define the `semboids-neighbors` projection contract (entity pattern for boid
      IDs, group `neighbors`, mode reconcile, predicates `["flock.neighbor.of"]`) and
      construct one `projection.MutationClient` in the sim component (design D4)
- [x] 3.2 Publisher: replace `TripleRemover` with a `PredicateReconciler`-shaped seam;
      emptying transition issues `Reconcile` with empty desired set; bounded retry ×3
      on revision-conflict, `commit_unknown` logged + counted, never blind-retried
      (design D2); keep `prevHadNeighbors` and the one-snapshot-at-a-time invariant
- [x] 3.3 Cull reclaim (`internal/sim/lifecycle.go`): replace the raw
      `graph.mutation.entity.delete` request with `ReadAuthoritative → Delete` at the
      read revision, bounded retry ×5 on revision-conflict, reclaim-failure metric +
      log on `commit_unknown`/exhaustion, both round trips inside the drain-pool
      operation (design D3)
- [x] 3.4 Unit tests on the narrow seams: reconcile fired only on non-empty→empty,
      empty-reconcile retry classification (conflict retries, ambiguous does not),
      fenced-delete retry/exhaustion paths, drain-pool boundedness with the 2-RTT
      reclaim
- [x] 3.5 Fix the seed-flock lifecycle startup race found in the smoke boot:
      `Manager.Create` now exact-reads through the mutation responder, and the sim's
      seed creation can beat graph-ingest's responder coming up — bounded
      retry/backoff on "no responders" in the create drain path so seed boids are
      never silently lifecycle-less
- [x] 3.6 Sweep for leftover raw mutation-subject strings (`grep -rn 'graph.mutation'`)
      — none outside comments/docs referencing history

## 4. Test-fixture grammar migration

- [x] 4.1 Migrate every integration test's inline component config to the new port
      grammar (cull, roundtrip, probe, snapshot, zone ingest, clustering spike,
      debug index, neighbor-empty — same D5 mapping table), including the mutation
      provider port wherever graph-ingest is built in-proc
- [x] 4.2 Rewrite `TestNeighborEmptyGate` to pin both halves (design D6): raw stream
      publish of an empty set still lingers (merge-only limitation persists);
      `Reconcile`-empty clears ENTITY_STATES + INCOMING (native path works); align
      scenario names with the graph-snapshots delta
- [x] 4.3 `task check` green (vet, gofmt tree-wide, revive, `-race` unit); remember
      `gofmt -w` every new file immediately (CI's gofmt gate skips untracked files)
- [x] 4.4 `go test -race -tags=integration ./...` green against a fresh dev NATS

## 5. Fresh state and product proof

- [x] 5.1 Fresh NATS everywhere: `task dev:nats:clean` (dev), recreate
      `semboids-demo-nats` (demo); confirm no retained pre-160 buckets/streams
- [x] 5.2 `task check:push` green (full CI mirror incl. cross-compile + schema
      no-drift)
- [x] 5.3 Live product proof on the demo stack, evidence in the change dir: ingest at
      dial 1→4 Hz, all four rule toggles flip behavior visibly, spawn wave + cull
      chain reclaims (fenced delete confirmed in logs/metrics, zero
      reclaim-failures), community colours appear in the graph pane, SSE stream +
      reconnect, process restart comes back clean against retained KV config
      (version gate honors 1.2.0)
- [x] 5.4 Record the product-proof checklist the upstream guide requires (tag,
      migration commit, retired surfaces removed, suites green, e2e proof) in
      `evidence/product-proof.md`

## 6. Perf re-baseline and closeout

- [x] 6.1 Dial sweep vs the beta.158 baseline (fresh stack per run, interleaved,
      settle-waited per the beta.149 methodology); record in
      `docs/perf/beta160-migration-2026-08-12.md` with attribution notes separating
      migration effects from beta.159 add-lane dedup
- [x] 6.2 Comment + close semstreams#578 with the native-path verification evidence
      (TestNeighborEmptyGate both halves); note the census row (W=4) is cleared
- [ ] 6.3 Update CLAUDE.md's semstreams table if any package paths moved; conventional
      commits per milestone; PR with `task check:push` green; after merge,
      `/opsx:archive migrate-semstreams-beta160` (delta sync included)
