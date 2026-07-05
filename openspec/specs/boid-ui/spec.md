# boid-ui Specification

## Purpose
TBD - created by archiving change add-flock-core. Update Purpose after archive.
## Requirements
### Requirement: SvelteKit app per sem* conventions
The UI SHALL live in `ui/` as a SvelteKit 2 / Svelte 5 (runes) TypeScript app
using adapter-node behind a Caddy gateway (WS and API paths to the backend
:8080, all else to the UI server), with vite dev on :5173 and the semteams
three-layer `colors.css` theme.

#### Scenario: Dev serving path
- **WHEN** a developer runs the dev stack
- **THEN** the browser reaches the UI on the gateway, and WebSocket frames
  flow from the backend through the same gateway origin

### Requirement: Latest-wins frame store
A singleton WebSocket store (`.svelte.ts`, runes) SHALL maintain connection
status with exponential-backoff reconnect and hold only the most recent frame
in `$state`; frames arriving faster than rendering SHALL overwrite, never
queue.

#### Scenario: No frame backlog
- **GIVEN** frames arriving at 30Hz and a renderer running slower
- **WHEN** the renderer reads the store
- **THEN** it gets the newest frame and no backlog accumulates

#### Scenario: Reconnect
- **GIVEN** a dropped WebSocket
- **WHEN** the backend becomes reachable again
- **THEN** the store reconnects with backoff and resumes updating status and
  frames

### Requirement: Canvas 2D spatial pane
The flock SHALL render on a Canvas 2D pane driven by `requestAnimationFrame`,
drawing each boid as a triangle oriented by its velocity, devicePixelRatio
aware, sustaining smooth rendering at 200 boids / 30Hz frames. The page
layout SHALL be split-screen: the left pane hosts the spatial canvas and the
right pane hosts the graph view (see the `graph-pane` capability).

#### Scenario: Smooth render at target scale
- **GIVEN** a live 200-boid simulation at 30Hz
- **WHEN** the spatial pane renders
- **THEN** animation is visually smooth and boid headings match motion

#### Scenario: Renders only latest state
- **WHEN** the tab is backgrounded and later foregrounded
- **THEN** rendering resumes from the current frame without replaying stale
  frames

#### Scenario: Both panes live simultaneously
- **GIVEN** the demo running with graph data flowing
- **WHEN** the page renders
- **THEN** the canvas pane animates at frame rate while the graph pane
  updates at its own cadence, without interfering with each other

### Requirement: Zone overlays on the spatial pane
The canvas pane SHALL render each zone from frame data as a circle at its
world position, colored by zone type from the categorical palette
(predator/food/wind visually distinct), beneath the boids. The frame parser
SHALL accept both 5-element (legacy) and 6-element boid tuples and frames
with or without zones.

#### Scenario: Zones visible
- **GIVEN** a running simulation with three configured zones
- **WHEN** the spatial pane renders
- **THEN** three circles appear at the correct letterboxed positions with
  type-distinct colors

#### Scenario: Legacy frames still render
- **WHEN** a frame without zones or modifier flags is received
- **THEN** boids render normally and no overlay errors occur

### Requirement: Boids under influence are visually distinct
Boids whose tuple carries a non-zero modifier flag SHALL render in the
modifier kind's color (matching its zone type color) instead of the default,
so rule effects are directly visible in the flock.

#### Scenario: Fleeing boids tinted
- **GIVEN** boids scattering from a predator zone
- **WHEN** the pane renders
- **THEN** the affected boids render in the predator color until their
  modifier ends

### Requirement: Live rule toggles
The UI SHALL present a toggle per zone rule showing its current enabled
state; toggling SHALL take effect on the running demo without a page reload
or backend restart, and the control SHALL reflect failure if the backend
rejects the change.

#### Scenario: Toggle changes the flock
- **GIVEN** the demo running with the predator rule enabled
- **WHEN** the user switches the predator toggle off
- **THEN** boids stop reacting to predator zones within a few seconds while
  the page stays live

#### Scenario: Failed toggle surfaces
- **GIVEN** the backend unreachable
- **WHEN** the user flips a toggle
- **THEN** the control reverts and an error state is shown

