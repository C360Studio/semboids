# Design — add-graph-pane

## Context

Zone steering proved rules + graph ingestion at event rates; the right pane
is still a placeholder and nothing writes *boids* into the graph. This
change adds the snapshot pipeline (the load dial v1), community detection,
and the sigma.js pane. Findings below verified against semstreams beta.133
source.

**Verified substrate facts this design builds on:**

- `graph-clustering` watches `ENTITY_STATES` (kv-watch), runs LPA on a
  configurable `detection_interval`, and writes to the `COMMUNITY_INDEX` KV
  bucket as `Community{ID, Level, Members []string, ParentID}`
  (`graph/clustering/types.go:10`). LLM enhancement is optional and stays
  off.
- Clustering **requires graph-index**: it waits for `OUTGOING_INDEX`
  (`component.go:777`) for adjacency — `graph-index` must join the flow.
- graph-ingest merges with **predicate-level replace** (`MergeTriples`,
  `graph/helpers.go:101`): all newer triples for a (subject, predicate)
  replace all existing ones. Publishing the full current neighbor set as
  `flock.neighbor` triples each snapshot correctly swaps the previous set —
  **except** when the new set is empty (no triples → no conflict → stale
  edges persist). See D6.

## Goals / Non-Goals

**Goals:**

- Boids in the graph at a tunable cadence with physics provably isolated
  from publication pressure.
- Flock membership visible as live community recoloring — the substrate's
  own analysis on display, including its lag when the dial cranks.
- The dial instrumented (published/dropped/lag metrics) so the later
  load-dial campaign only has to turn the knob and read.

**Non-Goals:** the melt campaign itself; pane interactivity beyond
hover/zoom/pan; retention tuning; any zone-steering behavior change.

## Decisions

### D1. Snapshot derivation: inline every Nth tick, copied out, never blocking

The sim derives a snapshot every `tickHz/graphHz` ticks inside the existing
tick loop (single-threaded engine ownership preserved): per boid, position/
velocity properties and the current neighbor set from a grid query (radius
configurable, default `NeighborRadius`; exposed because dense flocks may
need a smaller edge radius for distinct communities). The snapshot is
**copied** into a value struct and sent to the publisher channel with
`select`/`default` — if the publisher is behind, the snapshot is dropped
and a counter incremented. Derivation cost is O(N×k) reads (µs at 200
boids); the loop never blocks on JetStream.

### D2. Publisher goroutine + `internal/boidgraph`

A dedicated goroutine consumes snapshots and publishes each boid as a
BaseMessage-wrapped Graphable (`boids.boid.v1`, entity ID
`c360.semboids.sim.flock.boid.<id>`; triples `flock.position.x/y`,
`flock.velocity.x/y`, relationships `flock.neighbor` → neighbor boid IDs)
to `entity.boid.upsert` on the existing ENTITY stream. New
`internal/boidgraph` package holds payload + registration (mirrors
`internal/zone`). Prometheus: `semboids_graph_snapshots_published_total`,
`_dropped_total`, `_publish_duration_seconds`, and a cadence gauge.

### D3. Flow additions: graph-index + graph-clustering

`componentregistry` gains `graphindex.Register` and
`graphclustering.Register`; `configs/flock.json` wires: graph-index
(ENTITY_STATES → OUTGOING/INCOMING indexes), graph-clustering (kv-watch
ENTITY_STATES → COMMUNITY_INDEX; `detection_interval: "2s"`,
`min_community_size: 3`, `enable_llm: false`, and — spike 1.2 / beta.136 —
`entity_id_edges: {include_siblings: false, include_system_peers: false}`
so LPA runs on explicit `flock.neighbor` topology alone; with the ID-derived
virtual edges on, all same-type entities merge into one community).
Community reads filter to **level 0** (COMMUNITY_INDEX is hierarchical;
higher levels legitimately contain everything). Zone entities also live in
ENTITY_STATES; boid-only filtering happens at the read path (D4), and
communities containing zones are harmless (zones have no neighbor edges).

### D4. Read path: SSE bridge in the boids service (substrate view, not sim view)

The graph pane must show **what is in the graph** — including its lag when
the dial exceeds budget — so it reads the substrate, not the frame stream.
The boids API service adds `GET /boids/graph/stream` (SSE, semdragons'
live-sim pattern): KV watchers on `ENTITY_STATES` (keys
`*.flock.boid.*`) and `COMMUNITY_INDEX`, an initial full sync, then
**batched flushes every ~500ms**:

```json
{"entities": [{"id": "...boid.7", "x": 812.4, "y": 301.2,
               "neighbors": ["...boid.12", "...boid.31"]}],
 "communities": {"...boid.7": "community-3"}}
```

(COMMUNITY_INDEX's `Community.Members` inverts to a per-entity map at the
bridge.) The Caddyfile's `/boids/*` handle gains `flush_interval -1` (SSE
must not be buffer-compressed — same lesson as semteams' SSE routes); the
vite dev proxy for `/boids` already exists.

### D5. Sigma pane: real positions, no force layout

Adapt the semspec `SigmaCanvas` pattern *without* its layout half: boid
positions are real, so ForceAtlas2, `sigma-layout.ts`, and the
triple-shaped `graphology-adapter` are all skipped. A `graphStore.svelte.ts`
consumes the SSE store into `$state` maps; a slim `GraphCanvas.svelte`
holds a graphology `Graph`, syncs nodes (`x`, `y` normalized from world
coords, color = community hash into the categorical `--domain-*` palette,
default gray for un-communitied), edges from neighbor lists, and calls
`sigma.refresh()` per batch — the high-frequency path the original survey
confirmed sigma handles. The pane replaces the placeholder; a small cadence
select (0.5/1/5/10/30 Hz) sits in the pane header.

### D6. Runtime dial + empty-neighbor staleness (spikes)

- **Dial at runtime**: `--graph-hz` flag/config seeds it; the boids API
  gains `PUT /boids/graph/hz` flipping an atomic in the sim (same pattern
  as the rule gates) so dial experiments don't restart the world.
- **Empty-neighbor staleness** (spike 1.1 outcome, 2026-07-04): the
  mutation lane's `update_with_triples` supports predicate removal but is
  must-exist request/reply per entity — the wrong lane for volume. Adopted
  hybrid: snapshots ride the JetStream Graphable lane (the canonical
  ingest path the dial is meant to stress); every snapshot carries a
  `flock.neighbor.count` property (always changing → merge always fires);
  and on a boid's non-empty→empty neighbor transition the publisher issues
  one idempotent `graph.mutation.triple.remove`
  (`RemoveTripleRequest{Subject, Predicate: "flock.neighbor"}`,
  `mutations.go:277`) — rare, cheap, exact. No upstream gap; the
  primitives compose.

## Risks / Trade-offs

- **One giant community**: LPA over a dense flock may label everything
  community-1. Mitigations: snapshot neighbor radius tunable (D1),
  `min_community_size` and detection interval tunable; worst case the pane
  still shows live topology, just monochrome. Visual tuning is expected
  during verification.
- **SSE volume**: 200 entities/s at dial 1Hz is fine batched; at 30Hz the
  bridge coalesces per key between flushes (latest wins per entity) so
  browser traffic is bounded by flush rate, not dial rate.
- **Index/clustering cost under load**: intentionally the experiment;
  metrics from D2 plus the existing rule/ingest metrics make the lag
  measurable rather than anecdotal.
- **KV watcher fan-in on the bridge**: two watchers per SSE client is
  wasteful with many clients; acceptable for a demo (one or two viewers),
  noted for the load-dial change if it matters.

## Open Questions

- D6 spike outcomes (mutation-API producer ergonomics; staleness fallback
  choice) — resolved in early implementation tasks, both paths designed.
