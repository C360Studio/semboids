# boid-ui Specification (delta)

## MODIFIED Requirements

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
