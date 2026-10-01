# Proposal: migrate-semstreams-shared-pin

## Why

SemBoids must establish its own correctness and load baseline on SemEngine SETUP 03A's exact SemStreams
revision while SemEngine development proceeds. SemSource's workload does not qualify SemBoids' rules,
lifecycle churn, clustering, or browser delivery. The current SemBoids reference is
`8c03cc53836ced93a5df7064473c63ff144e64f1`, using SemStreams `v1.0.0-beta.160` at
`8403a2218000e45a31c5132fbfe01af42ed04f14`.

SETUP 03A already froze main `8b99efe9c66a4faa4fa509f9f62cc6bad8392128`, represented in Go as
`v1.0.0-beta.162.0.20260930150212-8b99efe9c66a`, because beta.163 was absent when that baseline began.
Reuse that pin; a later tag or main commit does not change this migration. The evidence and source links
are in [design.md](design.md).

## What Changes

- Open a draft migration PR before adapting production code. Record reproducible baseline build, semantic,
  browser, dependency-closure, and load results before upgrading.
- Compile and adapt to the frozen revision using failing-first behavior tests. Expected breaks include
  caller-owned shutdown contexts, removal of live instances from Registry, sealed composition, canonical
  authority and effective platform identity, registered payload contracts, and live rule activation.
  Compilation success is only the first gate; exact target source and execution determine compatibility.
- Preserve seeded physics, batched graph snapshots, neighbor replacement and empty-set clearing,
  lifecycle facts, rule-driven steering and live toggles, spawn/cull, explicit fenced-reclaim retries,
  clustering, and WebSocket/SSE/UI delivery. Prove removal of stale relationships and restart behavior.
- Run existing build/lint/vet/race/real-NATS integration gates and both browser panes. Compare interleaved
  baseline/target load runs with identical seeds, population, cadence, churn, and isolated NATS storage.
  Verify each metric's name, labels, and accounting boundary before reporting a performance change.
- Record every difference, dependency closure, blocker, and independent review. File substrate defects
  against SemStreams with reproductions; do not invent app-side graph/rule/lifecycle implementations.

The physics hot path is unchanged: in-process Reynolds steering, spatial hash, seeded randomness, and
population/modifier staging remain SemBoids domain code. It acquires no NATS, KV, rule, or graph I/O.

## Capabilities

### New Capabilities

- `migration-qualification`: exact shared pin, retained workload, operational lifecycle, dependency closure,
  controlled performance comparison, and independent admission evidence.

### Modified Capabilities

None are intentionally removed or relaxed. Existing `flock-physics`, `graph-snapshots`, `boid-lifecycle`,
`population-control`, `zone-steering`, `flock-communities`, `flock-egress`, `graph-pane`, `boid-ui`, and
`ingest-telemetry` remain acceptance constraints. Any discovered incompatible contract must be recorded
explicitly before altering its spec.

## Non-goals

- No SemEngine dependency, module-path cutover, or assumption that SemEngine already admits this workload.
- No simulation semantics in either substrate; no changes to steering weights, seeded physics, or zone rules
  to make performance or correctness comparisons pass.
- No independently chosen revision, `replace` directive, patched module cache, or cherry-picked upstream fix.
- No per-tick or ordinary per-snapshot request/reply graph writes, app-owned graph store, rule engine,
  lifecycle harness, clustering implementation, or transport substitute.
- No destructive cleanup of shared developer NATS storage. Each comparison owns its broker and data directory.
- No pass claims from stale artifacts, silent logs, missing metrics interpreted as zero, or API reachability alone.

## Impact

Production boundary: `cmd/semboids`, `componentregistry`, `internal/sim`, `internal/api`,
`internal/boidgraph`, `internal/zone`, configuration, and module closure. UI behavior should remain stable;
UI changes require their own frontend review. See the architecture matrix and ordered tasks for handoffs.
