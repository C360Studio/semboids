# graph-pane Delta — migrate-semstreams-beta149

## MODIFIED Requirements

### Requirement: Sigma.js graph view of the substrate state
The right pane SHALL render the boid graph with sigma.js (WebGL): nodes at
the boids' real world positions (normalized from graph data — no force
layout), edges from `flock.neighbor.of` relationships, node color derived from
the entity's community assignment via the categorical palette (a neutral
color for unassigned). The pane SHALL reflect the SSE stream — the
substrate's view of the flock, including its lag relative to the canvas
pane when the dial exceeds ingest budget.

#### Scenario: Topology mirrors the flock
- **GIVEN** the demo running at dial 1Hz
- **WHEN** both panes are visible
- **THEN** the graph pane shows node clusters matching the canvas flocks,
  updating as snapshots land

#### Scenario: Communities recolor on merge/split
- **GIVEN** two flocks visible as two node colors
- **WHEN** the flocks merge and a detection interval elapses
- **THEN** the merged cluster converges to a single community color

#### Scenario: Lag is visible, not fatal
- **GIVEN** a dial setting beyond ingest budget
- **WHEN** the canvas pane keeps animating smoothly
- **THEN** the graph pane updates late or sparsely (dropped snapshots) but
  the page remains responsive and recovers when the dial lowers
