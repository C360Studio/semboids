# Design — migrate-semstreams-beta149

## The breaking wave, and what actually touches semboids

The wave is documented across three semstreams operations docs
(`29-entity-id-contract-clean-cutover.md`, `30-rule-event-identity-clean-cutover.md`,
`31-sister-repo-cutover-checklist.md`) and four decisions (ADR-077, ADR-078, and
the `graph-index-replacement-semantics` / `predicate-raw-key-representation`
OpenSpec changes).

**Where enforcement lives** (corrected during apply): the canonical **predicate
contract** (exactly 3 non-empty lower-kebab segments) and **entity-ID contract**
(exactly 6 parts) are enforced **fail-closed at graph-ingest since beta.147**
(`vocabulary.ParsePredicate` → `graph.ValidateEntityPredicates`, live in the
beta.149 tag) — this is the runtime *persistence* seam (syntax-only). Separately,
the **v1 authoring policy** `vocabulary.RequireDeclaredPredicate` (ADR-036) gates
*declaration surfaces* — a lifecycle workflow's PhasePredicate, `lifecycle:` tags,
and rule condition fields — requiring registration, not just structure. That
split is why the static audit caught two items and the integration tests caught
two more. (The `feat/enforce-structural-invariants` WIP branch — with a
`graph_ingest_structural_rejects_total` metric — did **not** merge into beta.149.)

| Breaking item | semboids exposure | Verdict |
|---|---|---|
| Predicate = exactly 3 non-empty parts (fail-closed at graph-ingest since beta.147) | `flock.neighbor` is **2-part** (`internal/boidgraph/payload.go:73`) — the boid↔boid edge feeding INCOMING + graph-clustering | 🔴 **Fix**: → `flock.neighbor.of` |
| `pack_id` required on every rule processor (schema-required, even with graph integration off) | `rule-processor` in `configs/flock.json` (and the two inline test configs) had none | 🔴 **Fix**: add `pack_id` |
| Declaration-surface predicates must be registered (ADR-036 `RequireDeclaredPredicate`) — **runtime-only** | lifecycle workflow PhasePredicate + `lifecycle:` tag `flock.lifecycle.phase` was unregistered → `Manager.Register` fails | 🔴 **Fix**: register in `vocabulary.go` |
| Rule condition `field` must be `$message.*`/`$state.*` or a declared predicate (ADR-036 Rule 1) — **runtime-only** | conditions used bare `zone_type` / `event` → rule load fails | 🔴 **Fix**: `$message.*` prefix |
| Entity IDs MUST be exactly 6 non-empty parts, charset `[A-Za-z0-9_-]`, no dotted instance | boid `c360.semboids.sim.flock.boid.<n>`, zone `…zone.<pred-1>` — both 6-part, charset-clean | 🟢 conforms |
| `enable_graph_integration` default `true → false` | already explicit `false` — correct: our rules `publish` to `boids.steering` + `lifecycle_transition`, never graph events | 🟢 conforms |
| All other predicates | `flock.position.{x,y}`, `flock.velocity.{x,y}`, `flock.neighbor.count`, `zone.{classification,geometry,behavior,wind}.*` — all 3-part | 🟢 conforms |
| Graph-event constructors → `(*Event, error)` | we never call them — spawn/despawn go through `lifecycle.Manager.Create` + `graph.mutation.entity.delete`; entities land via Graphable payloads → graph-ingest | 🟢 not called |
| PR #535 package-boundary removals (OGC / github-webhook / A2A) | our imports: `component`, `config`, `graph/clustering`, `message`, `metric`, `natsclient`, `output/websocket`, `payloadbuiltins`, `payloadregistry`, `pkg/lifecycle`, `processor/{graph-clustering,graph-index,graph-ingest,rule}`, `service`, `types` — none removed | 🟢 verify build |
| Framework alert/trigger identity change (`alert_*` → digest) | our rules create no alert/trigger entities (actions are steering-publish + lifecycle_transition) | 🟢 not affected |
| Destructive wipe of derived graph state | state is 100% physics-derived, no external source to reseed from | 🟢 trivial (below) |

The checklist rated us LIGHT and named only `pack_id`; its audit scanned
semstreams' own reference-config/vocabulary corpus, not ours. Our audit is the
sister-repo half — and the two runtime items (`flock.lifecycle.phase`
registration, `$message.*` conditions) surfaced only when the integration tests
drove `Manager.Register` and rule load against beta.149, underscoring that a
static predicate/entity grep is necessary but not sufficient for this wave.

## Predicate rename: `flock.neighbor` → `flock.neighbor.of`

`domain.category.property`: `flock` / `neighbor` / `of`. Reads as the relation
"A is neighbor-of B", and keeps the whole neighbor family under `flock.neighbor.*`
(alongside the existing `flock.neighbor.count`), so a `flock.neighbor.*` wildcard
still groups every neighbor fact. Under ADR-078 the predicate is embedded whole
into the raw PREDICATE_INDEX key (`predicate3.entity6`) and into the gh#474
composite INCOMING key, so the rename only changes key *content* — the fresh
rebuild from `ENTITY_STATES` establishes the new keys with no migration.

Blast radius (all first-party, no UI hardcode — the UI reads pre-built edges
from `graphstream.go` over SSE):

- `internal/boidgraph/payload.go:73` — emit site (+ doc comments lines 2, 52)
- `internal/boidgraph/publisher.go:189` — wire `"predicate"` field
- `internal/api/graphstream.go:85` — API edge mapping (`case "flock.neighbor"`)
- tests: `boidgraph/{payload,publisher,snapshot_integration,clustering_spike_integration}_test.go`,
  `internal/api/graphstream_test.go`

## The one wipe window

ADR-078 §2 and the cutover doc are emphatic: there is no dual reader/writer,
mixed-format mode, old-key path, export, or rollback; the index layout change
consumes the **same** pre-v1 wipe as the identity changes, and a missed window
"must not create a second undeclared wipe." We are still on beta.146 and have not
taken the wipe, so our first migration *is* that window — going straight to
beta.149 spends it once for the whole wave.

For semboids the wipe is trivial because nothing outside the process owns the
graph — the physics loop regenerates every boid/zone entity on the next
snapshot. The deletion set (union of `graph.FrameworkOwnedBuckets()` + graph-ingest
guard buckets, intersected with what our composition actually enables):

```
ENTITY_STATES  ENTITY_SUFFIX_INDEX  GRAPH_INGEST_APPLIED_SEQ
OUTGOING_INDEX  INCOMING_INDEX  ALIAS_INDEX  PREDICATE_INDEX
NAME_INDEX  CONTEXT_INDEX  COMMUNITY_INDEX
```

We never configured `PREDICATE_CATALOG` (retired by ADR-078) — nothing to remove,
and a fresh deployment must not recreate it. `task demo` already runs an isolated
NATS on :24222, so the cleanest reseed is simply a fresh volume rather than
per-bucket `nats kv rm`. Restart, let one snapshot land, then restart once more
with no intervening write to prove replay parity.

## Performance: measured result

Matched A/B, **fresh isolated NATS per run, same box**, identical sweep binary
and parameters (200 boids @ 30 Hz, 20 s warmup + 60 s window); beta.146 built
from a `main` worktree = baseline code + pin. Full write-up:
`docs/perf/beta149-migration-2026-07-18.md`.

| Metric | beta.146 | beta.149 | Δ |
|---|---:|---:|---|
| index write-amp (puts/idx-entity) | 21.3 | 1.47 | **−93% (~14×)** |
| index puts/s | 978.8 | 152.6 | −84% |
| incoming-index puts/s | 564.7 | 47.8 | −92% |
| index events/s (idx throughput) | 46.0 | 104.2 | +126% |
| entities updated/s (ingest drain) | 1668 | 1331 | −20% (confounded) |
| e2e p99 / physics fps / contract rejects | 65.1 s / 30 / 0 | 64.9 s / 30 / 0 | flat |

**The headline is a ~14× drop in index write amplification** — unconfounded (a
ratio), and the ADR reasoning is confirmed by the raw `kv_operations_total`:

1. **Change-detection + raw predicate keys (ADR-078).** The `predicate` bucket
   did **1662 puts against 37224 list-checks** — a write only when a boid's
   *predicate membership set* changes, which for a boid it never does. `predicate3.entity6`
   raw keys with `PREDICATE_CATALOG` retired (verified absent) removes the catalog
   double-write. This is the bulk of the win.
2. **Replacement semantics (ADR-077).** INCOMING moved from append to
   owner-discovery + complete replacement; incoming puts/s fell 565 → 48.
3. **#524 sharding — confirmed neutral for us**, as predicted: bounded-degree
   flock, no hub runaway, so no shared-list contention to relieve. The win came
   from (1)+(2), not sharding.
4. **Index throughput +2.3×** (46 → 104 events/s): cheaper per-event work drains
   the graph-index KV-watch consumer faster.

**Open — the ingest ceiling.** The ingest-drain delta (−20%) is *not* a clean
signal: the two melt windows differed in publish rate (6000 vs 5493 entities/s)
and starting backlog (44 k vs 0), and beta.149 grew *less* backlog. The box was
also under other load. The `#480` ingest wall is in both (ADR-072 keyed-concurrent,
unchanged by this wave) and e2e p99 is flat, so there is no evidence of a
watcher-ordering regression — but attributing the ingest ceiling needs a
dedicated isolated run (single burst, `graph_hz=0`, à la
`parallel-lifecycle-drain`) on a quiet box. Tracked as a follow-up.

## Outcome

Implemented on branch `migrate-semstreams-beta149`, all green (`task check` +
`-race -tags=integration`), verified live on a real beta.149 stack (entities
land, zero contract rejections, `PREDICATE_CATALOG` absent). The bump was a clean
compile — nothing beyond predicates/config broke, confirming the package-boundary
row above. semboids did its load-generator job: it measured and validated a real
substrate efficiency win (~14× less index write amplification) with no
correctness or physics cost.
