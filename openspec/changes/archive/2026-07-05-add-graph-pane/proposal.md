# Add Graph Pane

## Why

The split screen's right half has been a placeholder since the walking
skeleton, and the project's stated purpose — profile SemStreams under a
fast-moving graph — still lacks its instrument: nothing writes boids into the
graph. This change builds both at once: boid/neighbor snapshots flowing
through `graph-ingest` at a **tunable cadence (the load dial, v1)**, and a
sigma.js pane where LPA communities from `graph-clustering` recolor flocks
live as they merge and split — the graph substrate made visible.

## What Changes

- **Graph snapshots (the load dial)**: the sim derives boid entities
  (position/velocity triples) and `flock.neighbor` relationship triples from
  the spatial hash it already maintains, every Nth physics tick
  (`--graph-hz`, default 1Hz), publishing BaseMessage-wrapped Graphables to
  the ENTITY stream for `graph-ingest`. Cranking the dial toward tick rate
  is the deliberate stress experiment from ADR-001 §4.
- **Publisher decoupling**: snapshot derivation is inline and bounded
  (µs-scale reads of engine state); JetStream publication runs on a
  dedicated goroutine fed by a drop-oldest channel — under pressure the
  graph *lags*, physics never blocks.
- **`graph-clustering` joins the flow**: LPA community detection over the
  neighbor graph; community assignments become flock membership.
- **Graph read path for the UI**: the browser receives graph state (boid
  nodes with positions, neighbor edges, community ids) — exact transport
  (graph-gateway query vs. KV-watch bridge vs. piggyback subject) is a
  design.md decision from investigating what `graph-clustering` emits.
- **Sigma.js pane**: the semspec `SigmaCanvas` quad (adapted) fills the
  reserved right pane — nodes at real world positions (no force layout
  needed), edges = neighbor relations, node color = community. A cadence
  control exposes the dial in the UI next to the existing rule chips.
- **Metrics visibility**: snapshot publish counts/lag surface alongside the
  existing per-rule metrics so dial experiments read from Prometheus.

**Hot-path statement** (required): this change touches the physics hot path
only at snapshot derivation — an every-Nth-tick, bounded O(N×k) read of the
current buffer using the existing grid, feeding a non-blocking channel.
Publication, ingest, clustering, and UI reads all happen off-loop at
substrate rates. At extreme dial settings the documented 10–30ms/entity
ingest budget will be exceeded **by design**; the failure mode is lagging
graph state (dropped snapshots, counted in metrics), never a stalled or
slowed tick.

## Capabilities

### New Capabilities

- `graph-snapshots` — boid/neighbor derivation, the cadence dial, decoupled
  publication through graph-ingest
- `flock-communities` — LPA clustering wiring and community consumption
- `graph-pane` — the sigma.js view: nodes/edges/community coloring, live
  updates, dial control

### Modified Capabilities

- `boid-ui` — the "split-screen with reserved right pane" requirement is
  replaced: the right pane hosts the graph view

## Impact

- `internal/sim`: snapshot derivation + publisher goroutine; `--graph-hz`
  CLI/config plumbing.
- `internal/boidgraph` (new): boid/neighbor Graphable payloads and
  vocabulary.
- `componentregistry` + `configs/flock.json`: `graph-clustering` (and the
  chosen read-path component) join the flow.
- `ui/`: sigma + graphology dependencies land; graph pane components, a
  community/graph store, dial control.
- First real load data for the substrate: expected upstream findings around
  ingest throughput and the ws-output re-encode already flagged in
  `docs/perf/baseline-200boids-30hz.md`. Any melt-point discoveries file to
  the SemStreams issue queue with pprof evidence.

## Non-goals

- The formal melt-point benchmark campaign and its report (a later
  `load-dial` change runs the campaign; this change builds the instrument
  and proves it at moderate settings).
- Graph pane interactivity beyond hover/zoom/pan — no entity expansion,
  query UI, or GraphQL playground surfacing.
- Retention/eviction tuning for boid entities (accepting default KV
  behavior; revisit when the load campaign characterizes it).
- Lifecycle-managed spawn/despawn (still a later change) and any change to
  zone steering behavior.
- No SemStreams changes; gaps file as issues.
