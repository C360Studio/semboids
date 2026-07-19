# Tasks — migrate-semstreams-beta149

> One-shot 146 → 149 across the pre-v1 canonical-contract wave, in the single
> allowed wipe window. Turned out to be FOUR app-side fixes, not two — the
> integration tests shook out two runtime-only breaks the static audit could not
> see. Substrate regressions are filed upstream, not worked around (Product
> Boundary).
>
> **Enforcement reality (corrected during apply):** the canonical **predicate
> contract** (arity=3, lower-kebab segments) and **entity-ID contract** are
> enforced **fail-closed at graph-ingest since beta.147** (`vocabulary.ParsePredicate`
> via `graph.ValidateEntityPredicates`), and the **v1 authoring policy**
> (`vocabulary.RequireDeclaredPredicate`, ADR-036) gates declaration surfaces —
> lifecycle PhasePredicate, `lifecycle:` tags, and rule condition fields. The
> separate `feat/enforce-structural-invariants` WIP (with a
> `graph_ingest_structural_rejects_total` metric) did NOT merge into beta.149;
> the real beta.149 counters are `predicate_contract_rejections_total` and
> `entity_state_contract_rejections_total`.

## 0. Gate — beta.149 tagged

- [x] 0.1 `v1.0.0-beta.149` tagged on `main` at `7db0cdcb` (2026-07-18). It is
      beta.148 + Stop-idempotency (#550) + agentic-tools (#552); the predicate/
      entity contracts arrived earlier in the beta.147 wave and are live in the tag.
- [x] 0.2 Capture the beta.146 baseline for A/B (freeze `docs/perf/` or re-run
      fresh on the same box; record dials + boid counts).

## 1. Predicate rename `flock.neighbor` → `flock.neighbor.of` — DONE

- [x] 1.1 Tests updated to expect `flock.neighbor.of`
      (`internal/boidgraph/{payload,publisher,snapshot_integration,clustering_spike_integration}_test.go`,
      `internal/api/graphstream_test.go`).
- [x] 1.2 Renamed the three production sites (`payload.go:73` + doc comments,
      `publisher.go:189`, `internal/api/graphstream.go:85`); `flock.neighbor.count`
      untouched.
- [x] 1.3 Bounded rename verified: 12 bare tokens → `.of`, 10 `.count` untouched,
      0 bare `"flock.neighbor"` remain in `internal/`.

## 2. `pack_id` on the rule processor — DONE

- [x] 2.1 Added `"pack_id": "semboids"` to `rule-processor.config` in
      `configs/flock.json`; `enable_graph_integration` kept explicit `false`.
- [x] 2.2 Added `"pack_id": "semboids-test"` to the two inline rule-processor
      configs in `internal/sim/{cull,roundtrip}_integration_test.go` (they build
      their own config maps, not from `flock.json`).

## 3. Register the boid lifecycle predicate (NEW — runtime break) — DONE

> Surfaced at `Manager.Register`: `predicate "flock.lifecycle.phase" is canonical
> but not declared in the vocabulary registry`. The PhasePredicate + the
> `lifecycle:"phase,predicate=…"` tag are declaration surfaces (ADR-036), so the
> predicate must be registered even though our runtime snapshot predicates need
> only the syntax-only seam.

- [x] 3.1 `internal/boidgraph/vocabulary.go`: `RegisterVocabulary()` registers
      `flock.lifecycle.phase` (`WithDescription` + `WithDataType("string")`),
      invoked from `init()` so both the composition root and the tests that drive
      `Manager.Register` see the declaration.

## 4. Rule condition fields → `$message.*` (NEW — runtime break) — DONE

> Surfaced at rule load: beta.149 `validateConditionFields` (ADR-036 Rule 1)
> requires every condition `field` to be `$message.*`/`$state.*`-prefixed or a
> declared predicate; our bare `zone_type`/`event` failed. Our actions already
> used `$message.*`, so this is consistent.

- [x] 4.1 Prefixed `zone_type`→`$message.zone_type`, `event`→`$message.event`
      across the 3 zone-steering rule files and the two integration tests
      (18 fields; message field names in `component.go` unchanged).

## 5. Dependency bump + verify — DONE (measurement pending)

- [x] 5.1 `go get …@v1.0.0-beta.149 && go mod tidy`; `go.mod`/`go.sum` bumped
      (no `replace`).
- [x] 5.2 `go build ./...` + `go vet ./...` clean — nothing beyond predicates/
      config breaks at compile time (no removed-package or signature impact).
- [x] 5.3 `task check` green (vet, gofmt, revive, `-race` unit). Integration
      `-race -tags=integration` green: the full cull chain
      (`TestPredatorCullReclaimsBoid`) and roundtrip rule loop pass on beta.149 —
      `$message.` rules fire, `flock.neighbor.of` ingests past the predicate
      contract, and the lifecycle cull transition reclaims through the graph.

## 6. Surface the contract-reject signal (`cmd/sweep`) — DONE

- [x] 6.1 Fixed a latent sweep bug: `mutation_rejections_total` lives under
      subsystem `graph_ingest`, not `datamanager` — the sweep had been reading a
      non-existent `semstreams_datamanager_mutation_rejections_total` (silent 0)
      since it was written. Corrected to `semstreams_graph_ingest_mutation_rejections_total`.
- [x] 6.2 Added `predicate_contract_rejections_delta` +
      `entity_state_contract_rejections_delta` fields (from
      `semstreams_graph_ingest_{predicate,entity_state}_contract_rejections_total`),
      a `contract-reject` classification branch, and both in the printed summary —
      the migration's proof-of-clean signal (expected flat 0 on our corpus).

## 7. Destructive wipe + canonical reseed (the one window)

- [x] 7.1 Boot beta.149 on a fresh NATS volume (isolated :24222 per `task demo`);
      let one snapshot land. Prove: config loads with `pack_id`, boids/zones appear
      in `ENTITY_STATES`, `PREDICATE_INDEX` holds raw `predicate3.entity6` keys, no
      `PREDICATE_CATALOG`, INCOMING edges + graph-clustering communities render, and
      `predicate_contract_rejections_total` / `entity_state_contract_rejections_total`
      are 0. Restart once with no intervening write; prove replay parity.

## 8. Performance A/B + evidence

- [x] 8.1 Run the matched sweep on beta.149 vs the beta.146 baseline (same dials/
      boid counts); record `index_write_amp`, `index_puts_per_s`,
      `incoming_index_puts_per_s`, events/s, `consumer_pending` inflection, and
      e2e-latency quantiles. Report amp's components (raw-key vs replacement vs
      sharding), not just the total.
- [x] 8.2 Write the A/B to `docs/perf/` (baseline vs beta.149, per-metric delta,
      watcher-ordering finding). File a verified semstreams issue for any
      regression, sweep JSON attached.
- [x] 8.3 Complete the checklist evidence envelope (dependency transition,
      composition + `pack_id`s, wipe, reseed/replay parity, event-consumer = none,
      verification commands + result).

## 9. Close out

- [x] 9.1 `openspec validate migrate-semstreams-beta149 --strict` (update the
      proposal/design enforcement framing to match the corrected reality above,
      and the ingest-telemetry delta to the real metric names).
- [x] 9.2 Refresh the README semstreams-pin line + memory (beta.146 → beta.149;
      the four fixes; the amp/throughput finding). Commit in conventional splits;
      archive the change.
- [ ] 9.3 Follow-up (separate change): ADR-077 replacement semantics may now
      express "now-zero neighbors" — retire the `flock.neighbor.count` staleness
      fallback if the sweep confirms it.
