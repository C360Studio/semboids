# Design — add-zone-steering

## Context

The walking skeleton (archived `add-flock-core`) runs physics in-process and
streams frames out via `output/websocket`. Nothing yet exercises
`processor/rule` or `graph-ingest`. This change wires both, establishing the
steering-modifier contract — the one sanctioned feedback path into the hot
loop reserved by ADR-001. Findings below are verified against semstreams
`v1.0.0-beta.133` source.

**Verified substrate facts this design builds on:**

- The rule processor takes message-path inputs on **core NATS** ports
  (`processor.go:954`) as well as JetStream — transition events can be
  fire-and-forget.
- The `publish` action (`actions.go` `executePublish`) publishes
  `{entity_id, subject, timestamp, source:"rule_engine", properties}` with
  **`$message.*` variable substitution in both subject and properties** — a
  rule can forward `boid_id` from the triggering event into the modifier it
  emits.
- Rules support **hot-reload** through the component config-update path
  (`ValidateConfigUpdate`, `processor.go:1195`; the "hot-reload wire format"
  in `config_validation.go`) — flipping a rule's `enabled` field via a
  component config update takes effect without restart.
- `graph-ingest` subscribes **JetStream** on `entity.>` by default and is the
  sole writer to `ENTITY_STATES`; payloads must be BaseMessage-wrapped
  Graphables resolved via the payload registry.

## Goals / Non-Goals

**Goals:**

- Rules visibly change flock behavior with a live toggle (the demo moment).
- First `ENTITY_STATES` writes (zones as entities) and first rule firings
  from a downstream app.
- A steering-modifier contract small and tick-decoupled enough to keep the
  ADR-001 hot-path guarantees intact and re-usable by later changes
  (e.g. flock events, spawn governance).

**Non-Goals:**

- Moving/lifecycle-managed zones, click-to-place editing, predator boids.
- Graph snapshots of boids/neighbors, the sigma pane, the load dial.
- Durable delivery of transition events (see Decisions/Trade-offs).

## Decisions

### D1. Zones: config-defined, graph-ingested at boot

Zones live in `configs/flock.json` under the sim component config:
`{id, type: predator|food|wind, x, y, r, strength, [dx, dy for wind]}`.
A new `internal/zone` payload implements `Graphable` (6-part entity ID
`c360.semboids.sim.flock.zone.<id>`; triples for type/geometry/strength) and
registers in the payload registry. At startup the host publishes each zone
BaseMessage-wrapped to `entity.zone.upsert` (JetStream, covered by an
`ENTITY` stream ensured from config); `graph-ingest` lands them in
`ENTITY_STATES`. Zones are thereby real graph entities from day one — the
future graph pane and rules that watch entity state get them for free.

### D2. Transition events: edge-triggered, core NATS, flat payload

The sim holds zone geometry in memory. Each tick it computes per-boid zone
containment (O(N×Z), Z ≈ 2–5 static circles — trivial next to neighbor
queries) and publishes only on **edges**:

```json
subject: boids.zone.events
{"boid_id": 42, "zone_id": "pred-1", "zone_type": "predator",
 "event": "entered", "tick": 1234}
```

Core NATS, fire-and-forget, BaseMessage-wrapped as **`core.json.v1`
(GenericJSONPayload)** — spike finding (task 1.1): the rule engine's
message-path conditions and `$message.*` substitution only see
`GenericJSONPayload.Data` (`expression_factory.go:119`,
`message_handler.go:410`); a custom registered payload type would decode but
never match. Condition `field`s therefore address the flat `data` map
directly (`boid_id`, `zone_type`, `event`), and `entity_id` in the data
(the boid's 6-part ID) keys per-boid rule state. Event rate is bounded by
flock dynamics (bursts when a flock crosses a boundary), not tick rate.

### D3. Rules: JSON message-path rules; modifiers via the publish action

New `configs/rules/zone-steering/*.json`, loaded via the rule processor's
`rules_files`. **Two rules per zone type** (spike 1.1 refinement): an
`entered` rule whose `on_enter` publishes the modifier, and an `exited` rule
whose `on_enter` publishes the `cancel` — each rule's per-entity state is
keyed by the boid's `entity_id`, so enter/exit pairs fire exactly once per
visit. Predator entered rule:

```json
{
  "id": "predator-flee",
  "enabled": true,
  "conditions": [
    {"field": "zone_type", "operator": "eq", "value": "predator"},
    {"field": "event", "operator": "eq", "value": "entered"}
  ],
  "on_enter": [{
    "type": "publish",
    "subject": "boids.steering",
    "properties": {
      "boid_id": "$message.boid_id",
      "zone_id": "$message.zone_id",
      "kind": "flee",
      "ttl_ticks": 60
    }
  }]
}
```

Exit events publish `kind: "cancel"` for the same boid/zone pair (needed by
wind/food, harmless for flee). The modifier arriving at the sim is the
`publish` action's fixed envelope — raw JSON (not BaseMessage):
`{entity_id, subject, timestamp, source: "rule_engine", properties: {...}}`
— so the sim's steering parser reads `properties` and coerces `boid_id`
leniently (substitution may render numbers as strings).

### D4. Modifier application: buffered, TTL'd, clamped

The sim subscribes (core NATS) to `boids.steering` and maintains an
in-memory table `boidID → {kind, zoneID, ttlTicks}`. Each tick it derives a
per-boid external steering vector from the table + current geometry (flee:
away from zone center; attract: toward center; wind: fixed direction) and
passes it to the engine. The engine adds the external term into `steer()`
**before the existing MaxForce clamp** — modifiers can never exceed the force
budget, so all `flock-physics` clamp guarantees hold unchanged. TTLs
decrement per tick; `cancel` removes entries; expiry self-heals a missed
exit. No locks in the hot path: the subscription goroutine writes to a
mutex-guarded staging map the tick loop drains once per tick.

### D5. Live toggles: app-side modifier gate (spike outcome — fallback path)

Spike 1.2 finding: the component API's `PUT …/config/<name>` hot-applies
only via `UpdateConfig(ctx, json.RawMessage)`
(`component_manager_http.go:701`); the rule processor implements the
*service*-flavored `RuntimeConfigurable.ApplyConfigUpdate` instead, whose
only driver serves services (`service_manager.go:807`). The rule engine's
hot-reload machinery is unreachable over HTTP, and the PUT reports success
without applying — filed as
[semstreams#455](https://github.com/C360Studio/semstreams/issues/455).

Adopted fallback (as designed): a minimal semboids `boids-api` service
(registered with the service manager like semdragons' `game` service)
exposing `GET /boids/rules` and `PUT /boids/rules/{kind}` on :8080. It
gates modifier *kinds* in the sim's steering intake (a gated kind's
modifiers are dropped on arrival and its active entries cleared) — visibly
identical demo behavior, no rule-engine reimplementation. The Caddyfile
gains a `/boids/*` handle. When #455 lands, the gate swaps for real rule
toggling without UI changes.

### D6. Frame format: zones + per-boid modifier flag

Frames gain `"zones":[[type,x,y,r],...]` (sent every frame; a few dozen
bytes) and the boid tuple gains a 6th element — active modifier kind
(0 none, 1 flee, 2 attract, 3 wind): `[id,x,y,vx,vy,m]`. The UI tints
affected boids and draws zone circles in categorical palette colors; the
parser accepts both 5- and 6-element tuples for compatibility.

## Risks / Trade-offs

- **At-most-once events**: a dropped `entered` means one boid doesn't react
  this visit; a dropped `exited` is healed by TTL expiry. Acceptable for a
  demo; JetStream durability is available later by flipping port types.
- **Rule-engine latency**: transition → rule → modifier crosses NATS twice;
  at event rates this is milliseconds — visible as a natural "reaction
  time", which reads well in the demo.
- **Burst behavior**: a whole flock entering a zone fires ~flock-size events
  within a tick or two. Rule eval is O(all rules) per message (documented
  upstream concern) — with 3 rules this is nothing, but it is the first
  real data point for the load story; note eval metrics in verification.
- **Toggle surface uncertainty** (D5): mitigated by the app-side gate
  fallback; either way the demo works, and a gap becomes an upstream issue.

## Open Questions

All resolved by the phase-1 spikes (2026-07-04):

- ~~Component config-update endpoint~~ → unreachable for the rule
  processor; app-side gate adopted, semstreams#455 filed (see D5).
- ~~Condition field addressing~~ → events publish as `core.json.v1`;
  conditions address the flat data map; `$message.*` works (see D2/D3).
