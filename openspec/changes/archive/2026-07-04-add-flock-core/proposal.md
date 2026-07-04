# Add Flock Core

## Why

The repo is empty. Before zones, lifecycle, graph snapshots, or the load dial
can exist, we need the walking skeleton that proves the hybrid split
(ADR-001) end to end: an in-process Reynolds physics loop producing real
emergent flocking, streamed through SemStreams `output/websocket`, rendered
live on a browser canvas. Everything later builds on this slice — and it is
the fastest path to the demo being *fun*, which is the point.

## What Changes

- **Repo scaffold** to `sem*` house conventions: `go.mod`
  (`github.com/c360studio/semboids`, Go 1.26.x, SemStreams pinned
  `v1.0.0-beta.133`), `revive.toml`, Taskfile + `taskfiles/`, NATS dev task
  (`nats:2.12-alpine -js`, 4222/8222), CI workflows (`ci.yml`, `ui.yml`).
- **`internal/flock`** — the physics engine: Reynolds steering
  (separation/cohesion/alignment), spatial-hash neighbor queries, toroidal 2D
  world, deterministic seeding, configurable tick rate (default 200 boids @
  30Hz). Pure Go, no substrate imports.
- **Sim component + host** — the physics loop wrapped as a SemStreams Input
  component publishing one aggregated frame per tick to `boids.frames`;
  `cmd/semboids` host wiring the component registry, flow config
  (`sim → output/websocket`), Prometheus metrics (:9090), and
  `service.MaybeStartPProf` (:6060 debug).
- **`ui/`** — SvelteKit skeleton per conventions (Svelte 5 runes,
  adapter-node, Caddyfile, vite dev :5173): a WebSocket store (singleton +
  backoff, after semteams' `runtimeWebSocket.ts` pattern) and a Canvas 2D
  spatial pane rendering the flock via `requestAnimationFrame`.

Hot-path statement (per `openspec/config.yaml` rules): this change *creates*
the physics hot path. It stays off NATS/rules/graph-ingest per ADR-001; the
one sanctioned substrate touch is the single aggregated frame publish per tick
to the egress subject.

## Capabilities

### New Capabilities

- `flock-physics` — in-process Reynolds simulation loop
- `flock-egress` — frame publication and WebSocket broadcast
- `boid-ui` — SvelteKit app with the Canvas 2D spatial pane

No `sem*` products consume these capabilities; SemBoids is a leaf demo.

## Impact

- All files are new; no existing capabilities are modified.
- Upstream: none expected in this slice. If `output/websocket` shows gaps
  under 30Hz frame broadcast (e.g. per-message allocation hot spots), file on
  the SemStreams issue queue with pprof evidence — no app-side workaround.

## Non-goals

- Zone steering, rules, or the steering-modifier contract (next change).
- Lifecycle-managed spawn/despawn (population is fixed at start in this slice).
- Graph ingestion, neighbor triples, the load dial, `graph-clustering`
  communities, or the sigma.js graph pane.
- Profiling/benchmark harness beyond having pprof reachable.
- 3D worlds, obstacle avoidance, predator boids, or any steering rule beyond
  Reynolds' three.
- Binary wire format (JSON first; revisit only with evidence it matters).
