# SemBoids Project Context

## Project Overview

SemBoids is a classic Reynolds boids simulator — a fun demo for the C360 `sem*`
family celebrating simple-yet-detailed over complex: three steering rules
(separation, cohesion, alignment) producing emergent flocking. It doubles as a
calibrated load generator for the SemStreams substrate: the graph-ingest rate
is a dial we crank until something melts, profile with pprof, and file upstream.

## Tech Stack

- Go 1.26.x backend, `github.com/c360studio/semstreams` pinned by tag
- NATS JetStream (KV + streams) via SemStreams runtime
- SvelteKit 2 + Svelte 5 runes UI in `ui/` (adapter-node behind Caddy)
- Canvas 2D for the boid spatial pane; sigma.js v3 + graphology for the graph pane
- Taskfile runner, revive lint, Prometheus metrics, pprof

## Architecture (CRITICAL — the hybrid split)

**Per-tick physics state never routes through NATS, rules, lifecycle, or
graph-ingest.** SemStreams ADR-040 retired boids-through-rules; the graph write
budget (10–30ms/entity) is 30–450× too slow for per-tick updates, and rule
KV-watch coalescing drops intermediate updates by design. Decided 2026-07-04.

```
┌─ semboids process ─────────────────────────┐
│  physics loop (30Hz ticker, spatial hash)  │
│    │ positions        ▲ steering modifiers │
│    ▼                  │                    │
│  ┌─ semstreams components ──────────────┐  │
│  │ ws-output ──► browser canvas         │  │
│  │ graph-ingest ◄─ snapshot @ DIAL Hz   │  │
│  │ rule proc ◄─ zone transitions        │  │
│  │ lifecycle ◄─ spawn/despawn           │  │
│  │ graph-clustering ─► flock communities│  │
│  └──────────────────────────────────────┘  │
│  pprof :6060   metrics :9090   api :8080   │
└────────────────────────────────────────────┘
```

- **Physics**: plain Go loop (`internal/flock`), `time.NewTicker`, spatial hash
  neighbors. Default 200 boids @ 30Hz.
- **Rules drive behavior, not physics**: zones (predator/food/wind) are graph
  entities; rules fire on zone *transitions* and emit steering modifiers the
  physics loop applies. Toggling a rule visibly changes the flock.
- **Lifecycle**: boid/wave spawn and despawn only — never position updates.
- **Graph snapshots at a dial**: boid entities + neighbor triples through
  `graph-ingest` at tunable cadence; flock membership = `graph-clustering`
  (LPA) communities recolored live in the graph pane.
- **Egress**: SemStreams `output/websocket`, at-most-once — slow clients drop
  frames, never backpressure physics.

## Spec-driven development (OpenSpec)

| Home | Holds |
|---|---|
| `openspec/specs/<capability>/spec.md` | Current truth (Requirement + GIVEN/WHEN/THEN), seeded lazily |
| `openspec/changes/<id>/` | Proposals: `proposal.md` + `tasks.md` + spec deltas; archived on completion |
| `docs/adr/` | Genuine decisions only (irreversible choices, cross-repo contracts) |
| `docs/` (other) | Tutorial/runbook content |

Non-trivial changes go through OpenSpec before code (`/opsx:new`,
`/opsx:continue`, `/opsx:apply`, `/opsx:archive`; `openspec validate <id>
--strict`). Read `openspec/project.md` first when scoping anything.

## Common Tasks

```bash
task dev:nats:start   # nats:2.12-alpine -js on 4222/8222 (container semboids-nats)
task check            # lint + test (run before push)
task check:push       # full CI mirror
task dev              # backend + vite dev (ui on :5173)
```

(Taskfile lands with the first change — see `openspec/changes/`.)

## CI Requirements (IMPORTANT)

CI must be green before push: `go vet`, `go fmt` clean, revive (warnings =
failure), `go test -race ./...` + `-tags=integration`, cross-compile of
`./cmd/semboids`, schema-generation no-drift. UI workflow (paths-filtered to
`ui/**`, Node 22): eslint, svelte-check, vitest, build.

## Semstreams Relationship (CRITICAL)

SemBoids imports SemStreams as a library — never reimplement substrate. If a
rule-engine, lifecycle, or graph primitive is missing, file a SemStreams issue;
don't carve an app-side parallel path.

| Need | Use (from semstreams) |
|---|---|
| NATS connection | `natsclient` |
| Component wiring | `component.Registry`, `componentregistry.Register` |
| Graph entities | `graph` (Graphable payloads → `ENTITY_STATES` via graph-ingest) |
| Rules | `processor/rule` (JSON rules, zone transitions) |
| Spawn/despawn | `pkg/lifecycle` (Participant + Manager) |
| Browser egress | `output/websocket` |
| Profiling | `service.MaybeStartPProf` (:6060 when debug) |
| Metrics | `metric` (Prometheus, :9090) |
| Errors/retry | `pkg/errs`, `pkg/retry` |

Graph writes go through the `graph.mutation.*` API — `graph-ingest` is the sole
writer to `ENTITY_STATES`. Rules pass references, never bulky payloads.

## Related Repos

- `semstreams` — the substrate framework (this repo's only Go dependency of
  note); canonical NATS dev setup, revive.toml, Taskfile shape.
- `semspec` — UI graph-pane origin (`SigmaCanvas.svelte` quad).
- `semdragon` — SSE service + runes `worldStore` live-sim store pattern.
- `semteams` — `colors.css` three-layer theme.
