# ADR-001: Hybrid Physics/Substrate Split — Per-Tick State Never Routes Through the Substrate

## Status

**Accepted — 2026-07-04.** Founding architecture decision, made before first
code. Fixes the boundary between the in-process Reynolds physics loop and the
SemStreams substrate for the life of the project.

## Context

SemBoids is a classic Reynolds boids simulator (separation/cohesion/alignment
over Euclidean positions) intended both as a fun `sem*` demo and as a
calibrated load generator to profile SemStreams graph throughput. The naive
architecture — every boid position update flowing through the rule engine,
lifecycle, and graph-ingest each tick — was evaluated against the SemStreams
codebase and rejected on evidence:

- **Write budget**: SemStreams documents 10–30ms total per entity update
  (`docs/advanced/03-performance.md`). 200–500 boids at 10–30Hz is 2,000–15,000
  updates/sec, requiring 0.067–0.5ms per update — **30–450× over budget**.
- **Coalescing**: the rule processor's KV entity watcher routes rapid updates
  through a `CoalescingSet` (`processor/rule/entity_watcher.go`) that collapses
  bursts into one evaluation against *latest* state. Intermediate positions are
  dropped **by design** — architecturally incompatible with per-tick physics.
- **Evaluation cost**: rule evaluation is O(all rules) per message with three
  map snapshots allocated per message (`processor/rule/message_handler.go`).
- **No tick source**: SemStreams cron is POSIX 5-field (minute granularity);
  no timer/ticker input component exists.
- **Lifecycle semantics**: every `Transition` is a CAS'd, audited, persisted KV
  write with up to 5 retries (`pkg/lifecycle/manager.go`) — right for
  spawn/despawn, catastrophic as a physics path.
- **Precedent**: SemStreams ADR-040 retired its own boid subsystem. Note the
  scope difference: that subsystem applied Reynolds' *rule names* to graph
  topology (k-hop separation, PageRank cohesion) to steer LLM agent context —
  it was never a spatial simulator, and its "36,000× slowdown" finding is
  LLM-in-the-loop specific. ADR-040 does not prohibit a downstream product
  from simulating spatial boids; it prohibits reintroducing boid coordination
  as a *substrate primitive*. SemBoids respects that: boids here are product
  domain, and the substrate is consumed at its intended rates.

## Decision

Split the system at the tick boundary:

1. **Physics is in-process.** A plain Go loop (`time.NewTicker`, default 30Hz)
   over a boid slice with a spatial hash for neighbor queries. No per-boid NATS
   writes and no rules, lifecycle, or graph participation on this path; the
   sole substrate touch per tick is one aggregated frame publish to the egress
   subject (a fire-and-forget core-NATS publish, ~30 msg/s total). Default 200
   boids.
2. **Rules drive behavior, not physics.** Zones (predator/food/wind) are graph
   entities. Rules fire on zone *transition events* (human/event rate) and emit
   steering modifiers; the physics loop applies them next tick. Toggling a rule
   visibly changes flock behavior — this is the rule-engine demo.
3. **Lifecycle owns population.** Boid/wave spawn and despawn are lifecycle
   transitions. Never position updates.
4. **Graph ingestion is a dial, not a firehose.** Boid entities + neighbor
   triples flow through `graph-ingest` at a tunable snapshot cadence (default
   low, e.g. 1Hz). Cranking the dial toward tick rate is a deliberate,
   labeled profiling exercise (pprof :6060 + Prometheus), not the demo path.
   Melt points and bottlenecks are filed as SemStreams issues.
5. **Egress is at-most-once.** `output/websocket` streams flock frames to the
   browser at tick rate; slow clients drop frames and never backpressure
   physics.

## Consequences

- The demo stays smooth at any substrate load: physics and rendering are
  decoupled from graph/rule throughput by construction.
- The throughput goal survives as a first-class feature: the ingest dial turns
  the sim into a calibrated load generator, and the melt point becomes data
  rather than a broken demo.
- Rules and lifecycle are demonstrated at the rates they were designed for —
  a fair showcase rather than a rigged failure.
- The steering-modifier contract (rules → physics) is the one place substrate
  output feeds the hot loop; it must stay small, reference-shaped, and
  tick-decoupled (applied on next tick, no blocking reads).
- If the substrate ever grows primitives that change this calculus (e.g. a
  batched high-rate ingest path), revisiting requires a new ADR superseding
  this one.
