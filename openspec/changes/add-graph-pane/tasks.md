# Tasks — add-graph-pane

> Briefly paused 2026-07-04 on
> [semstreams#461](https://github.com/C360Studio/semstreams/issues/461)
> (spike 1.2 finding); **resumed same day on beta.136**, which added the
> `entity_id_edges` config — `TestLPADistinguishesDisjointFlocks` now passes
> with `include_siblings/include_system_peers: false`.

## 1. Spikes (D6 — resolve before building on them)

- [x] 1.1 Spike: owned-projection replace from a producer — check the
      `graph.mutation.*` API envelope (`update_with_triples` /
      `replace_owned`, ADR-055/056) for publishing boid entities so an
      empty neighbor set replaces the previous one; if impractical from a
      plain producer, adopt the fallback (always-present
      `flock.neighbor.count` property + documented cosmetic staleness) and
      note whether a "producer-side owned replace" gap is worth a
      SemStreams issue
- [x] 1.2 Spike: COMMUNITY_INDEX consumption — run graph-index +
      graph-clustering against a testcontainer with hand-published boid
      entities; confirm bucket key/entry shape (`Community{ID, Level,
      Members}`), detection timing at `detection_interval: 2s`, and that
      LPA over `flock.neighbor` edges produces >1 community for two
      disjoint clusters

## 2. graph-snapshots (`internal/boidgraph` + sim) — TDD

- [ ] 2.1 Failing tests: boid Graphable payload (entity ID, position/
      velocity triples, `flock.neighbor` relationships per spike 1.1
      outcome); snapshot derivation (correct boids/neighbors from a known
      engine state; cadence — N snapshots per M ticks); drop-oldest channel
      (stalled consumer → drops counted, send never blocks)
- [ ] 2.2 Implement `internal/boidgraph` payload + registration; sim
      snapshot derivation on the tick loop (copied value, non-blocking
      send) with runtime-adjustable cadence (atomic), publisher goroutine,
      Prometheus counters/gauges
- [ ] 2.3 Integration test: sim + graph-ingest on a testcontainer — boid
      entities land in ENTITY_STATES with current neighbor sets; second
      snapshot replaces (no union); physics tick timing unaffected with a
      deliberately stalled publisher
- [ ] 2.4 Boids API: `PUT /boids/graph/hz` + `GET` state (mirror the rule
      gates pattern), unit tests

## 3. flock-communities (flow + bridge)

- [ ] 3.1 Wire the flow: register `graph-index` + `graph-clustering`;
      extend `configs/flock.json` (index ports; clustering kv-watch,
      `detection_interval: "2s"`, `min_community_size: 3`,
      `enable_llm: false`); `--graph-hz` flag plumbing
- [ ] 3.2 Failing tests: SSE bridge — initial sync snapshot; per-entity
      coalescing between flushes (latest wins); community inversion
      (Members[] → per-entity map); client disconnect cleans watchers
- [ ] 3.3 Implement `GET /boids/graph/stream` in the boids service: KV
      watchers (ENTITY_STATES boid keys + COMMUNITY_INDEX), batched
      ~500ms flushes; Caddyfile SSE flush handling (`flush_interval -1`)
- [ ] 3.4 Integration test: full chain — sim → ingest → index → clustering
      → SSE emits entities with community assignments (spike 1.2 wiring
      reused)

## 4. graph-pane (`ui/`)

- [ ] 4.1 Add sigma + graphology deps; failing tests: SSE store (initial
      sync, batch application, reconnect); community→color mapping stable
      across batches; world→normalized coordinate transform
- [ ] 4.2 Implement `graphStream.svelte.ts` (EventSource store, semdragons
      pattern) and `graphStore.svelte.ts` ($state maps for
      entities/communities)
- [ ] 4.3 `GraphCanvas.svelte`: sigma instance (SSR-safe dynamic import per
      the semspec pattern), node/edge sync from store with
      `sigma.refresh()` per batch — no force layout; community colors from
      the categorical palette; empty/connecting status states
- [ ] 4.4 Replace the placeholder pane; cadence select wired to
      `PUT /boids/graph/hz` with failure surfacing; hover shows boid id +
      community
- [ ] 4.5 `svelte-check`/eslint/vitest/build green with the new deps

## 5. Verification

- [ ] 5.1 `task check:push` green; live demo on isolated NATS (:24222 —
      semstreams#459): graph pane mirrors flocks at dial 1Hz, communities
      recolor on a flock merge; screenshot for README
- [ ] 5.2 Dial exercise (not the formal campaign): step 1 → 5 → 10 → 30 Hz;
      record published/dropped/ingest metrics and pane lag observations in
      `docs/perf/graph-dial-first-look.md`; file upstream issues if the
      substrate misbehaves (vs. merely lagging)
- [ ] 5.3 `openspec validate add-graph-pane --strict`; README status +
      roadmap update; archive the change
