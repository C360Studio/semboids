# boid-ui Delta — add-lifecycle-population

## ADDED Requirements

### Requirement: Live population count and spawn control
The UI SHALL display the live boid count (derived from the frame it already
receives) and provide a spawn-wave button that calls
`POST /boids/population/spawn`. No target-size or churn controls appear in this
slice.

#### Scenario: Count reflects births and deaths
- **GIVEN** the flock growing (spawn waves) and shrinking (culls)
- **WHEN** frames arrive
- **THEN** the header count tracks the live population

#### Scenario: Spawn button triggers a wave
- **WHEN** the operator clicks the spawn button
- **THEN** a spawn wave fires and the live count rises
