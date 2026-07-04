# boid-ui Specification (delta)

## ADDED Requirements

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
