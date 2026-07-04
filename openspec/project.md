# SemBoids Project Context

## Purpose

SemBoids is a **classic Reynolds boids simulator** — a fun, visible demo for the
C360 `sem*` family that celebrates the victory of simple-yet-detailed over
complex: three steering rules (separation, cohesion, alignment) producing
emergent flocking. It doubles as a **calibrated load generator** for the
SemStreams substrate: a fast-moving world whose graph-ingest rate is a dial we
crank until something melts, profile with pprof, and file upstream.

SemBoids is a **product, not a framework**. It composes SemStreams primitives
(component/port flows, the rule engine, the Lifecycle harness, graph ingestion,
websocket egress, metrics, pprof) and owns only its domain: boid physics, zones,
flock semantics, and the split-screen UI. Substrate gaps discovered here are
filed as SemStreams issues, never worked around with app-side parallel paths.

## Product Boundary

- **SemBoids owns**: the in-process physics engine (Reynolds steering, spatial
  hash, world bounds, tick loop); zone semantics (predator/food/wind) and the
  steering-modifier contract between rules and physics; flock domain payloads
  (Graphable boid/zone/flock entities); the load-dial (graph snapshot cadence);
  the split-screen UI (Canvas 2D spatial pane + sigma.js graph pane); and the
  profiling/benchmark harness that reports substrate throughput findings.
- **SemStreams owns** (consume, never reimplement): NATS/KV runtime and
  KV-twofer, `graph-ingest` as sole writer to `ENTITY_STATES`, graph
  query/gateway/clustering/indexes, the rule engine, the Lifecycle harness,
  `output/websocket`, the payload registry, metrics, and pprof service. Version
  pinned by tag in `go.mod` (no `replace` directives).
- **Cross-repo contracts**: none yet. If one emerges (e.g. a steering-signal
  vocabulary), record it as an ADR here and propose the substrate half upstream.

## Architecture Non-negotiables (decided 2026-07-04, before first code)

The hybrid split — physics in-process, substrate at event rates:

- **Per-tick physics state MUST NOT route through NATS, rules, lifecycle, or
  graph-ingest.** SemStreams' own ADR-040 retired boids-through-rules; the
  documented graph write budget (10–30ms/entity) is 30–450× too slow for
  200–500 boids at 10–30Hz, and the rule engine's KV-watch coalescing drops
  intermediate updates by design.
- **Physics**: plain Go loop, `time.NewTicker`, spatial hash for neighbors.
  Default 200 boids @ 30Hz.
- **Rules drive behavior, not physics**: zones are graph entities; rules fire on
  zone *transitions* and emit steering modifiers the physics loop applies.
  Toggling a rule visibly changes the flock.
- **Lifecycle** manages boid/wave spawn and despawn (real state transitions at
  human rates), never position updates.
- **Graph snapshots at a dial**: boid entities + neighbor triples flow through
  `graph-ingest` at a tunable cadence (default low; cranked deliberately for
  profiling). Flock membership in the UI = `graph-clustering` communities.
- **Egress**: `output/websocket` streams flock snapshots to the browser at tick
  rate (at-most-once; slow clients drop frames, never backpressure physics).

## Standing Technical Conventions

- Go 1.26.x, module `github.com/c360studio/semboids`; SemStreams pinned by tag.
- Layout: `cmd/semboids/` (API-only backend :8080, metrics :9090, pprof :6060
  when debug), domain packages in `internal/`, SvelteKit UI in `ui/`
  (Svelte 5 runes, adapter-node behind Caddy; vite dev :5173).
- Task runner: Taskfile (`task check` before push mirrors CI). Lint: revive via
  go.mod tool directive, warnings = failure.
- NATS dev: `nats:2.12-alpine -js -m 8222` on 4222/8222; env
  `SEMBOIDS_NATS_URLS`.
- Entity IDs are deterministic 6-part IDs
  (`org.platform.domain.system.type.instance`).
- Rules pass references, never bulky payloads.
- Large or cross-cutting changes go through OpenSpec (proposal + tasks + deltas)
  before code.
