# boid-ui Specification (delta)

## ADDED Requirements

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
aware, sustaining smooth rendering at 200 boids / 30Hz frames. The page layout
SHALL be split-screen with the right pane reserved (placeholder) for the
future graph view.

#### Scenario: Smooth render at target scale
- **GIVEN** a live 200-boid simulation at 30Hz
- **WHEN** the spatial pane renders
- **THEN** animation is visually smooth and boid headings match motion

#### Scenario: Renders only latest state
- **WHEN** the tab is backgrounded and later foregrounded
- **THEN** rendering resumes from the current frame without replaying stale
  frames
