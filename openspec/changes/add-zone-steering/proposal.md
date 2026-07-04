# Add Zone Steering

## Why

The walking skeleton proves physics and egress, but the substrate's
centerpiece — the rule engine — hasn't fired once, and nothing has touched the
graph. Zone steering is the change where rules **visibly** drive the flock:
toggle a predator rule and 200 boids scatter; toggle it off and they regroup.
It also establishes the steering-modifier contract (rules → physics), the one
sanctioned feedback path into the hot loop that ADR-001 reserved, and puts the
first entities into `ENTITY_STATES` via graph-ingest.

## What Changes

- **Zone model**: predator/food/wind zones as circles (center, radius,
  type, strength), defined in `configs/flock.json` and ingested as graph
  entities through `graph-ingest` at startup — the repo's first
  `ENTITY_STATES` writes. Zones are static in this change.
- **Zone transition events**: the sim detects boid enter/exit against static
  zone geometry in-process (edge-triggered) and publishes compact transition
  events (`boid id, zone id, entered|exited`) — event rate, not tick rate.
- **Rule processor joins the flow**: JSON rules (new `configs/rules/*.json`)
  subscribe to transition events; matched rules publish **steering
  modifiers** (`boid id, vector | bias kind, ttl`) to a steering subject.
- **Modifier application in physics**: the sim subscribes to steering
  modifiers, buffers them, and the engine applies them as an additional
  steering term next tick (predator → flee, food → attract, wind →
  constant bias), clamped by the existing MaxForce budget.
- **Frame format extension**: frames carry zone geometry and
  currently-active modifier counts so the UI can render overlays and show
  rules acting.
- **UI**: zone overlays on the canvas pane (circles colored by type from
  the categorical palette) and live per-rule toggles wired to the backend.
  The toggle mechanism is a design.md decision (rule processor
  enable/disable surface vs. app-level gate); if the rule engine lacks a
  runtime toggle API, that is a SemStreams issue to file, with an
  app-level gate as the documented interim.

## Capabilities

### New Capabilities

- `zone-model` — zone definitions, validation, and graph ingestion as
  entities
- `zone-steering` — transition events, rule wiring, the steering-modifier
  contract, and modifier application semantics

### Modified Capabilities

- `flock-physics` — new requirement: applies external steering modifiers
  within existing clamps; hot-path guarantees restated to cover zone
  containment checks
- `flock-egress` — frames extended with zones/modifier state; transition
  events added as a second (event-rate) publication
- `boid-ui` — zone overlays and rule toggle controls

## Impact

- `internal/flock`: modifier term in steering; zone containment helpers.
- `internal/sim`: zone geometry, edge-triggered transition detection,
  steering-modifier subscription and buffer.
- `componentregistry` + `configs/flock.json`: register and wire
  `graph-ingest` and `processor/rule`; new `configs/rules/` directory.
- `ui/`: canvas overlay layer, toggle controls, frame-type extension.
- First real exercise of `processor/rule` and `graph-ingest` from a
  downstream app — substrate findings (throughput, config ergonomics,
  missing runtime rule toggling) go to the SemStreams issue queue. Known
  related: [#452](https://github.com/C360Studio/semstreams/issues/452)
  (rules-engine doc deprecation banner, already filed).

**Hot-path statement** (required by config rules): this change touches the
physics hot path. Zone containment is an in-process O(N×Z) check per tick
against a handful of static circles (trivial next to neighbor queries);
transition *events* publish only on state edges. Steering modifiers arrive
asynchronously and are applied from an in-memory buffer next tick — no
blocking reads, no per-tick NATS/rules/graph-ingest traffic. The per-boid
tick path stays off the substrate per ADR-001.

## Non-goals

- Moving, spawning, or lifecycle-managed zones (zones are static config;
  lifecycle is a later change).
- Click-to-place zone editing in the UI (config-driven placement only).
- Boid/neighbor graph snapshots, the sigma.js graph pane, LPA communities,
  or the load dial (subsequent changes).
- Predator *boids* (autonomous hunters) — predator is a zone type here.
- Any change to SemStreams itself; gaps are filed as issues, not patched
  app-side.
