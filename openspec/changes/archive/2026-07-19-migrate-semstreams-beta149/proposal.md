# Migrate SemStreams beta.146 → beta.149 (pre-v1 canonical-contract wave)

## Why

SemStreams cut its **pre-v1 canonical-contract breaking wave** across three
tags, all sharing **one destructive graph-state wipe window**:

- **beta.147** (2026-07-17, breaking): 6-part entity IDs enforced, canonical
  predicate contract, graph-index **#524 composite-key sharding activated**,
  **ADR-077** complete replacement semantics for NAME/PREDICATE/INCOMING,
  **ADR-078** raw `predicate3.entity6` keys with `PREDICATE_CATALOG` retired,
  PR #535 package-boundary break, **`pack_id` now required** on every rule
  processor, `enable_graph_integration` default flipped `true → false`, and
  graph-event constructors returning `(*Event, error)`.
- **beta.148** (fixes only): rule `.value` scalar substitution, agentic
  per-spawn budget — no new semboids surface.
- **beta.149** (tagged on `main`, `7db0cdcb`): beta.148 + a Stop-idempotency fix
  (#550) + an agentic-tools fix (#552).

**Where the enforcement actually lives** (corrected from the prep phase): the
canonical **predicate contract** (exactly 3 non-empty lower-kebab segments) and
the **entity-ID contract** (exactly 6 parts) are enforced **fail-closed at
graph-ingest since beta.147** — `vocabulary.ParsePredicate` via
`graph.ValidateEntityPredicates`, live in the beta.149 tag. Separately, the **v1
authoring policy** (`vocabulary.RequireDeclaredPredicate`, ADR-036) gates
*declaration surfaces* — a lifecycle workflow's PhasePredicate, `lifecycle:` tags,
and rule condition fields — requiring the predicate be registered. (The
`feat/enforce-structural-invariants` WIP with a `graph_ingest_structural_rejects_total`
metric did **not** merge into beta.149; the real counters are
`predicate_contract_rejections_total` / `entity_state_contract_rejections_total`.)

semstreams' own sister-repo checklist
(`docs/operations/31-sister-repo-cutover-checklist.md`) rates semboids **LIGHT**:
*"one rule-processor config needs `pack_id`; re-run load instrumentation against
the new watcher ordering — per-entity serialization and coalescing may shift
throughput; file verified issues for regressions."* Our own audit + the
integration tests found **four fixes, not one** — the checklist scanned
semstreams' corpus, not ours. The heaviest: the boid↔boid relationship predicate
**`flock.neighbor` is 2-part** (`internal/boidgraph/payload.go:73`), rejected
fail-closed by the predicate contract — this is the edge feeding the INCOMING
index and `graph-clustering`, so it is a functional break, not cosmetic. Two more
surfaced only at runtime: the lifecycle phase predicate needs vocabulary
registration, and rule condition fields need the `$message.*` namespace (below).

Because the index work "does not ship on a later routine bump — it consumes the
same pre-v1 wipe window as the identity changes" and "a missed window must not
create a second undeclared wipe," we migrate **146 → 149 in one shot**: one
dependency bump, one destructive reseed, everything fixed before the gate can
bite. And because semboids exists to answer exactly the checklist's throughput
question, this change makes a **before/after performance measurement** first-class.

## What Changes

- **Bump `go.mod`** `github.com/c360studio/semstreams` beta.146 → beta.149
  (once beta.149 is tagged; prep lands on a branch now).
- **Add `pack_id`** to the `rule-processor` config in `configs/flock.json`
  (`"pack_id": "semboids"` — one processor, composition-unique, grammar-safe;
  `enable_graph_integration` stays explicit `false`, which is already correct —
  our rules publish NATS steering, not graph events).
- **Rename the neighbor relationship predicate** `flock.neighbor` →
  **`flock.neighbor.of`** (3-part `domain.category.property`, keeping the
  `flock.neighbor.*` family alongside `flock.neighbor.count`). Touches the emit
  site (`payload.go`), the publisher wire field (`publisher.go`), the API edge
  mapping (`internal/api/graphstream.go`), and their tests.
- **Register the boid lifecycle phase predicate** (new
  `internal/boidgraph/vocabulary.go`, `init()` → `vocabulary.Register`).
  `flock.lifecycle.phase` is the workflow PhasePredicate + `lifecycle:` tag — a
  declaration surface (ADR-036), so it must be registered even though runtime
  snapshot predicates use only the syntax-only seam.
- **Namespace rule condition fields** `zone_type` / `event` →
  `$message.zone_type` / `$message.event` (3 zone-steering rule files + 2
  integration tests). beta.149's `validateConditionFields` requires a condition
  `field` to be `$message.*`/`$state.*` or a declared predicate; our *actions*
  already used `$message.*`.
- **Destructive wipe + canonical reseed runbook.** semboids graph state is 100%
  physics-derived (no external authoritative source), so the wipe is trivial:
  stop, remove the framework-owned + guard buckets (or start on fresh NATS —
  `task demo` already isolates :24222), restart, physics repopulates. Record the
  evidence envelope the checklist requires.
- **Before/after sweep.** Re-run `cmd/sweep` at representative dials on beta.146
  (baseline) vs beta.149, quantify the deltas in **index write amplification**,
  **incoming-index puts/s**, **events/s**, **e2e latency**, and **rejections**,
  and file verified upstream issues for any regression. Update `docs/perf/`.

## Capabilities

### New Capabilities

<!-- none: this migrates the substrate pin and our graph vocabulary; no new capability -->

### Modified Capabilities

- `graph-snapshots`: the published boid neighbor relationship uses the canonical
  3-part predicate `flock.neighbor.of` (was `flock.neighbor`). Snapshot cadence,
  decoupling, batch/ordering, and observability contracts are unchanged.
- `graph-pane`: sigma.js edges derive from `flock.neighbor.of` relationships
  (predicate rename only; rendering, community coloring, and lag behavior
  unchanged).
- `ingest-telemetry`: the sweep/telemetry surface exposes graph-ingest's
  canonical-contract reject counters (`predicate_contract_rejections_total`,
  `entity_state_contract_rejections_total`) so a non-conforming predicate or
  entity ID surfaces as a counted, classified reject rather than silent graph
  loss — the migration's proof-of-clean signal. (Also fixes a latent sweep bug:
  `mutation_rejections_total` is under subsystem `graph_ingest`, not
  `datamanager` — the old name read a non-existent series, a silent zero.)

## Impact

- **Dependency**: `go.mod`/`go.sum` beta.146 → beta.149 (no `replace`; pinned by
  tag). Rebuild against the removed-package boundary — semboids imports none of
  the removed facades (OGC / github-webhook / A2A), so this is a verify, not a
  port.
- **Code**: `internal/boidgraph/payload.go`, `internal/boidgraph/publisher.go`,
  `internal/api/graphstream.go`, and their `_test.go` files (predicate rename);
  `internal/boidgraph/vocabulary.go` (new — predicate registration);
  `cmd/sweep/main.go` (fix rejection metric + surface the contract-reject
  counters).
- **Config**: `configs/flock.json` (`pack_id` on `rule-processor`);
  `configs/rules/zone-steering/*.json` (`$message.*` condition fields);
  the two inline rule configs in `internal/sim/*_integration_test.go`.
- **Specs**: `graph-snapshots`, `graph-pane`, `ingest-telemetry` deltas.
- **Docs/evidence**: `docs/perf/` A/B note + the cutover evidence envelope;
  `docs/adr/` only if a genuine cross-repo contract emerges (none expected).

## Non-goals

- **Re-architecting neighbor replacement.** ADR-077 replacement semantics may
  now express "now-zero neighbors" and retire our cosmetic-staleness fallback
  (the always-present `flock.neighbor.count`). That is a *measured follow-up*,
  not part of this migration — this change only renames the predicate and
  measures the new write pattern.
- **Reimplementing any substrate primitive.** Every fix here is app-side config,
  our own predicate vocabulary, or measurement. Substrate regressions found by
  the sweep are filed upstream, never worked around.
- **Isolating the ingest ceiling.** The A/B answered the index-write-amp
  question decisively (~14× lower on beta.149); the ingest-drain delta under a
  shared-box melt is confounded and needs a dedicated isolated-ceiling run
  (single burst, `graph_hz=0`) to attribute — tracked as a follow-up, not this
  change's scope.
