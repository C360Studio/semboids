# Graph Dial — First Look (200 boids, beta.137)

Captured 2026-07-04 during `add-graph-pane` verification (task 5.2). Not
the formal campaign — a first characterization of the instrument.

## Setup

Isolated NATS (:24222, semstreams#459), full flow (sim + graph-ingest +
graph-index + graph-clustering @2s + rule-processor + ws-output), dial
stepped via `PUT /boids/graph/hz`, 10s windows.

## Results

| Dial | Achieved snapshots/s | Entity publishes/s | Drops (10s) | Physics |
|---|---|---|---|---|
| 1 Hz | 1.0 | 200 | 0 | 30.0 fps |
| 5 Hz | 5.0 | 1,000 | 0 | 30.0 fps |
| 10 Hz | 10.0 | 2,000 | 0 | 30.0 fps |
| 30 Hz | **~21.6** | ~4,300 | **83 (~28%)** | **30.0 fps** |

## Findings

1. **The saturation point at 200 boids sits between 10 and ~22
   snapshots/s** (2,000–4,300 entity publishes/s through JetStream +
   graph-ingest). Beyond it, the drop-oldest publisher sheds snapshots
   exactly as designed — graph state lags, drops are counted, and
   **physics holds 30.0 fps throughout** (ADR-001 isolation, measured).
   *Post-hoc attribution (2026-07-05): this ceiling is the instrument's,
   not the substrate's.* The publisher issues 200 serial synchronous
   `PublishToStream` calls per snapshot; at the measured ~231µs ack RTT
   that is 46ms/snapshot = 21.6/s exactly, and the ~28% drop rate at the
   30Hz dial matches. graph-ingest melt remains uncharacterized beyond
   4.3k entities/s — filed as semstreams#470 (async/pipelined publish).
2. **beta.137's predicate-level merge (#466) verified live**: entity
   triple counts stable across tens of thousands of snapshot updates.
3. Clustering at `detection_interval: 2s` finds 15–19 level-0 communities
   for 200 boids in ~8 flocks (small flocks fragment below
   min_community_size churn); visual quality is good — flocks read as
   distinct colors, wrap-around torus neighbors render as long chords.
4. Host gotcha found during bring-up: the boids payload type must be
   registered in the host's payload registry (graph-ingest logs
   "unregistered payload type" WARNs and consumes nothing otherwise) —
   fixed in cmd/semboids.

The formal campaign (longer windows, pprof capture, boid-count sweep,
ingest-side latency histograms) remains the `load-dial` change.
