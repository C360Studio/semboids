# Add Lifecycle Population

## Why

Population is still fixed at start — the last substrate pillar from ADR-001
(`pkg/lifecycle` owns spawn/despawn) is unproven, and the demo can't show
births, deaths, or consequences. This change makes boids **lifecycle
participants**: spawn waves and despawn become real, audited, KV-backed
phase transitions at exactly the rates the harness was built for — and the
predator zone gains teeth, with a rule's `lifecycle_transition` action
culling boids that linger. Rules, lifecycle, graph, and physics in one
visible causal chain.

## What Changes

- **Boid lifecycle workflow**: a declared workflow (phases
  `active → culled | expired`, completing on removal) registered with
  `lifecycle.NewManager` in the host (semdragons wiring pattern). Spawn
  calls `Manager.Create`; despawn transitions then completes. Per-boid
  participants — the harness's intended granularity (drone-mission scale),
  exercised at event rates only.
- **Predator cull rule**: a new zone-steering rule — boids *remaining* in a
  predator zone past a grace period get transitioned to `culled` via the
  rule engine's `lifecycle_*` actions; the sim observes the transition and
  removes the boid (physics + frames + graph).
- **Engine population APIs**: `AddBoids`/`RemoveBoids` with stable IDs,
  applied between ticks by the same staging discipline as steering
  modifiers; determinism preserved for fixed populations.
- **Population control surface**: `GET/PUT /boids/population` (target size,
  spawn-wave trigger) on the boids API; UI gains a population control and
  live count; graph entities of despawned boids are removed via the
  mutation API.
- **Wave provenance**: each spawn wave is a graph entity linking its
  members — visible in the graph pane as new nodes arriving together.

**Hot-path statement** (required): population changes are event-rate
lifecycle operations. The tick loop reads a staged population delta exactly
as it reads staged steering modifiers — no locks, KV, or lifecycle calls
per tick. The harness's CAS'd, audited writes happen on spawn/cull events
only, per ADR-001 §3.

## Capabilities

### New Capabilities

- `boid-lifecycle` — the workflow declaration, host wiring, spawn/despawn
  transitions, graph cleanup on removal
- `population-control` — the API surface, spawn waves, UI controls

### Modified Capabilities

- `flock-physics` — population add/remove between ticks (stable IDs,
  clamps/determinism guarantees restated)
- `zone-steering` — predator cull rule joins the flee pair (lifecycle
  actions from rules)
- `boid-ui` — population control + spawn-wave button in the header

## Impact

- `internal/flock`: population APIs; `internal/sim`: staged deltas,
  lifecycle observation, spawn/despawn orchestration; `internal/api`:
  population endpoints; `cmd/semboids`: lifecycle Manager wiring;
  `configs/`: cull rule; `ui/`: controls.
- First downstream exercise of `pkg/lifecycle` + the rule engine's
  `lifecycle_*` actions — gaps file to the SemStreams queue as usual.

## Non-goals

- Predator *boids* or any autonomous hunter behavior (culling is
  zone+rule-driven).
- Population dynamics beyond waves and culls (no breeding, energy, aging).
- Lifecycle-managed zones (still static config).
- The formal load-dial campaign (unchanged; next change).
- Rebalancing flock physics for varying population (the engine already
  handles any N).
