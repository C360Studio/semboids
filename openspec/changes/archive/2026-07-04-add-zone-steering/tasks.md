# Tasks — add-zone-steering

## 1. Spikes (resolve design open questions before building on them)

- [x] 1.1 Spike: message-path rule condition addressing — integration test
      wiring a real rule processor (testcontainer NATS) with a trivial rule
      against a BaseMessage-wrapped test event on a core-NATS port; determine
      the exact condition `field` paths that reach payload fields
      (`boid_id`, `zone_type`, `event`). If payload fields are unreachable,
      file the SemStreams issue and document the flattened-payload interim in
      design.md
- [x] 1.2 Spike: rule hot-reload surface — locate the component config-update
      endpoint on the service-manager API (:8080) and confirm flipping a
      rule's `enabled` via `ValidateConfigUpdate` takes effect live; document
      the endpoint in design.md. If no usable surface exists, file the
      SemStreams issue and switched task 5.3 to the app-side gate fallback (D5)

## 2. zone-model (`internal/zone`) — TDD

- [x] 2.1 Failing tests: config validation (unknown type, radius <= 0,
      duplicate ids rejected, wind requires direction); Graphable payload
      (6-part entity ID `c360.semboids.sim.flock.zone.<id>`, type/geometry/
      strength triples)
- [x] 2.2 Implement Zone struct + validation, Graphable payload, vocabulary,
      payload-registry registration
- [x] 2.3 Boot ingestion: ensure ENTITY stream from config, publish zones
      BaseMessage-wrapped to graph-ingest's JetStream input; integration
      test (graph-ingest in a test registry) asserting zone entities land in
      `ENTITY_STATES` with correct triples — host never writes the bucket

## 3. Engine modifiers + transition events — TDD

- [x] 3.1 Failing tests (`internal/flock`): external steering term summed
      before MaxForce clamp (clamps hold under extreme magnitudes);
      trajectory bends vs. same-seed unmodified run; determinism identical
      when the external term is empty
- [x] 3.2 Implement engine external-steering hook (per-boid vector slice
      staged before Tick; no locks/channels/I-O inside the per-boid loop)
- [x] 3.3 Failing tests (`internal/sim`): edge-triggered containment (one
      entered + one exited per visit, none steady-state); modifier table
      TTL decrement/cancel/expiry self-heal; flee/attract/wind vector
      derivation from zone geometry; malformed or unknown-kind modifiers
      dropped with warning
- [x] 3.4 Implement sim: containment tracking + event publishing
      (`boids.zone.events`, `core.json.v1` payload per spike 1.1),
      `boids.steering` subscription + mutex-staged table drained per tick,
      frame extension (zones array + 6th tuple element `m`)

## 4. Rules + flow wiring

- [x] 4.1 `configs/rules/zone-steering/`: predator-flee, food-attract,
      wind-bias rules (conditions per spike 1.1; publish actions with
      `$message.*` forwarding; exit → `cancel`), `enabled: true` defaults
- [x] 4.2 Wire the flow: register `graph-ingest` + `processor/rule` in
      componentregistry; extend `configs/flock.json` (rule processor with
      nats input on `boids.zone.events` + `rules_files`; graph-ingest;
      ENTITY stream); `--zones` off-switch flag for a zone-free run
- [x] 4.3 Integration test: full round trip — boid crosses a predator zone →
      rule fires → modifier arrives → trajectory bends; plus disabled-rule
      variant asserting no modifier and no reaction

## 5. UI

- [x] 5.1 Failing tests: frame parser accepts 5- and 6-element tuples and
      frames with/without zones; toggle store optimistic flip + revert on
      backend failure
- [x] 5.2 FlockCanvas: zone circles beneath boids (categorical palette per
      type), modifier-tinted boids; legacy frames render unchanged
- [x] 5.3 Rule toggle controls calling the spike-1.2 endpoint (or app-gate
      fallback) through the Caddy `/components/*` route; error state on
      rejection
- [x] 5.4 Wire zone/rule legend into the header or pane edge (type → color)

## 6. Verification

- [x] 6.1 `task check:push` green; live demo: flock scatters at predator,
      pools at food, drifts in wind; toggle predator off/on mid-run and
      observe stop/resume; capture screenshot for README
- [x] 6.2 Record rule-engine burst behavior (whole flock crossing a
      boundary) — `semstreams_rule_evaluations_total` + eval duration
      histogram — in `docs/perf/` alongside the baseline; note any upstream
      findings
- [x] 6.3 `openspec validate add-zone-steering --strict`; file spike-flagged
      upstream issues if any; update README roadmap; archive the change
