# Product proof: semstreams v1.0.0-beta.160

Per the upstream migration guide's "Product proof" checklist. All runs on the
migration branch, freshly provisioned NATS (nats:2.12-alpine, isolated
:24222 demo container recreated; no retained pre-160 state anywhere).

- **Tag**: `v1.0.0-beta.160` (release commit `8403a221`), pinned in go.mod.
- **Migration commits**: openspec scoping + `feat(graph)!` migration +
  `fix(graph)` vocabulary/fixture round (see branch `migrate-semstreams-beta160`).
- **Compilation + generated surfaces**: `go build ./...` + `go vet
  -tags=integration ./...` clean; the complete compile harvest matched the
  audit's four predicted sites with no surprises (`compile-harvest.md`).
- **Strict configuration and flow validation**: `--validate` green; live boot
  passes flow validation with the `graph_mutations` provider/requester pairing
  declared (graph-ingest input; sim and graph-clustering requester outputs).
- **Retired surfaces removed**: no raw `graph.mutation.*` wire requests remain
  (`triple.remove` → typed Reconcile-empty; unfenced `entity.delete` → fenced
  ReadAuthoritative→Delete; test-only `entity.create_with_triples` → typed
  strict Create under a test-local contract). `ServiceConfig.Name`,
  `BuildPortFromDefinition`, flat port fields, manual `CreateService` pass —
  all gone.
- **Suites**: `task check` green; full `-race -tags=integration` suite green
  (api, boidgraph, flock, sim, zone). `TestNeighborEmptyGate` passes on the
  native path, pinning both halves: stream merge still does not clear an
  emptied neighbor set; reconcile-with-empty-desired clears ENTITY_STATES +
  INCOMING and a repeat reconcile writes no new revision.

## Live end-to-end (2026-08-12, fresh demo stack)

| Check | Result |
|---|---|
| Boot on fresh storage | Clean; streams created with declared bounds (`max_age 24h`, `max_bytes 2GiB`, `discard old`); zero errors, zero mutation rejections, zero seed-race warns |
| Seed flock lifecycle | 200/200 created (`boids_lifecycle_spawns_total 200`, `active 200`) — the beta.160 responder race is absorbed by the bounded-backoff retry |
| Ingest at the dial | 1 Hz: 30 snapshots / 6000 entities exact; dial → 4 Hz live via `PUT /boids/graph/hz`: ~840 entities/s (200 × 4.2 Hz) |
| Rule toggles | `flee` off→on both accepted and applied through the rule processor's runtime-reconfig trio (survives beta.160) |
| Spawn wave | `POST /boids/population/spawn {"n":25}` → active 200→225 |
| Cull → fenced reclaim | 7 culls, 7 reclaims, `reclaim_failures_total 0`. One live `revision_mismatch` (delete raced the boid's final snapshot write, expected 85539 vs current 85541) retried with a fresh read and landed — the design-D3 loop observed working in production |
| ENTITY_STATES accounting | 225 boids + 3 zones − 7 reclaimed = 221 live keys; stream shows 228 msgs = 221 + 7 delete tombstones, exact |
| Community colours | `KV_COMMUNITY_INDEX` 788 keys; clustering runs healthily on a lagging index view (post-#590 health gating), no starvation |
| SSE stream | `GET /boids/graph/stream` serves snapshot + deltas with entity + neighbor payloads; fresh connections attach cleanly |
| Restart on retained state | Clean boot against retained KV config (version gate at 1.2.0); seed flock re-adopted via `lifecycle.ErrAlreadyExists`-as-success (0 warns, `active 200`) — first attempt exposed 160 already-managed retry loops, fixed before this proof |
| Neighbor clears | `boids_graph_neighbor_clear_failures_total 0` throughout |

Watch items from the design's open questions, both settled: graph-clustering
functions with the mirrored DefaultConfig port set (detections + 788-key
index), and the stream-ingest batch lane carries the dial with no friction.
