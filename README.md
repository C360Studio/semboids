# SemBoids

A classic [Reynolds boids](https://www.red3d.com/cwr/boids/) simulator for the
C360 `sem*` family — a celebration of simple-yet-detailed over complex: three
steering rules (separation, cohesion, alignment) producing emergent flocking.

Built on [SemStreams](https://github.com/c360studio/semstreams). Physics runs
in-process at 30Hz; the substrate does what it's good at — rule-driven zone
steering, lifecycle-managed spawn/despawn, graph snapshots with live flock
community detection, and websocket egress to a split-screen UI (Canvas 2D
space + sigma.js graph).

SemBoids is also a calibrated load generator: the graph-ingest cadence is a
dial we crank to profile SemStreams under a fast-moving graph (pprof +
Prometheus). Substrate findings are filed upstream.

## Status

Pre-code. Architecture fixed in
[ADR-001](docs/adr/001-hybrid-physics-substrate-split.md); work proceeds
through [OpenSpec](openspec/README.md) changes.

## Development

See [CLAUDE.md](CLAUDE.md) for architecture, conventions, and common tasks.
