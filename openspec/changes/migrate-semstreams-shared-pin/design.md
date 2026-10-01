# Shared-pin migration design

## Status and authority

Architecture contract approved for failing-first compatibility implementation on 2026-10-01. This sign-off
approves the boundary and proving obligations; it does not claim target compatibility, gate success, baseline
admission, or PR readiness. Independent backend/frontend review and measured qualification remain required.

| Reference | Exact identity |
| --- | --- |
| SemBoids baseline | `8c03cc53836ced93a5df7064473c63ff144e64f1` |
| SemStreams baseline | `v1.0.0-beta.160`, `8403a2218000e45a31c5132fbfe01af42ed04f14` |
| Shared target | `v1.0.0-beta.162.0.20260930150212-8b99efe9c66a` |
| Shared target full SHA | `8b99efe9c66a4faa4fa509f9f62cc6bad8392128` |
| SETUP 03A source checkout read | SemSource `d90f0432253dab36062608583802edb4246e3129` |

Pin evidence: SemSource `docs/testing/setup-03a/pins.json` and `compatibility.md`, read from
`/Users/coby/.codex/worktrees/setup-03a-migration/semsource`. Both explicitly state beta.163 was absent at
selection and the selected main commit is frozen. SemEngine `docs/setup-plan.md` SETUP 03A and
`docs/provenance.md` prohibit silently moving the shared extraction baseline.

- [Versioned SETUP 03A pins][pins]
- [SETUP 03A consumer draft PR][setup-pr] and [SemEngine SETUP 03A issue][setup-issue]
- [Exact target migration guide][migration], including ADRs 102–104
- [Operations directory][contexts]: `migration-restore-go-lifecycle-ownership.md` and
  `migration-restart-safe-nats-client.md` (native lifecycle ownership)

[pins]: https://github.com/C360Studio/semsource/blob/d90f0432253d/docs/testing/setup-03a/pins.json
[setup-pr]: https://github.com/C360Studio/semsource/pull/213
[setup-issue]: https://github.com/C360Studio/semengine/issues/7
[migration]: https://github.com/C360Studio/semstreams/blob/8b99efe9c66a/docs/operations/migration-beta162-to-beta163.md
[contexts]: https://github.com/C360Studio/semstreams/tree/8b99efe9c66a/docs/operations
[native]: https://github.com/C360Studio/semstreams/tree/8b99efe9c66a/docs/operations

## D1: Preserve the application/substrate boundary

SemBoids owns deterministic physics, zone geometry, boid/flock meanings, modifier TTLs, population staging,
the snapshot dial, and UI. SemStreams owns graph authority, rule evaluation, lifecycle persistence, indexes,
clustering, and transport. Keep `graph-ingest` as the sole writer of `ENTITY_STATES`; use supported mutation
clients for exceptional empty-neighbor reconciliation and reclamation. Never recreate a missing substrate
capability locally. A failure at the frozen pin remains a documented blocker until an authorized disposition.

## D2: Inventory the actual contracts

This is the pre-change inventory, derived from production imports/configuration and existing tests, not an
assertion that every contract survives the target unchanged.

| Consumer | Required contract | Proving risk |
| --- | --- | --- |
| Host `main.go` | Config boot, effective authority, service lifecycle | First boot, retained restart, Stop contexts |
| Component registry | Sim, ingest, index, rule, clustering, WebSocket | Sealed composition and admitted ports |
| `internal/flock` | Seeded Reynolds physics and spatial hash | Repeatability, population and modifiers |
| Sim component | 30Hz ticks, bounded offers, aggregated frames | Cancellation, pressure isolation, joins |
| Boid publisher | One joined async batch, ordered snapshots | Ordering, real ingest, no hot-path I/O |
| Boid payload | Graphable boids, full current neighbors | Canonical ID, registration, relationship datatype |
| Neighbor contract | Empty desired set clears only its group | Authoritative triples and both indexes |
| Boid lifecycle | Active to culled/expired in same graph entity | Seed/spawn; snapshot preserves phase |
| Sim lifecycle | Browser-independent cull, bounded drain | Fenced delete; no resurrection/stale edges |
| Steering/tracker | Enter/exit/linger, modifiers, TTL | Round trip, bad input, disable/re-enable |
| API service | Rule activation and sim controls | Explicit injection; no live-instance Registry |
| `internal/zone` | Registered zones under effective authority | Readback and steering IDs agree |
| API graphstream | Shared graphview, SSE snapshot/upsert/delete | Watch loss, fanout, reconnect, stale edges |
| Index/clustering | Derived adjacency and LPA communities | Deterministic groups and retirement |
| WebSocket/UI | At-most-once frames, slow-client isolation | Both panes, controls and reconnect |

Nineteen directly imported SemStreams package paths were found in production Go source:
`component`, `config`, `graph`, `message`, `metric`, `natsclient`, `payloadbuiltins`, `payloadregistry`,
`pkg/graphview`, `pkg/lifecycle`, `pkg/projection`, `service`, `types`, `vocabulary`,
`processor/graph-ingest`, `processor/graph-index`, `processor/graph-clustering`, `processor/rule`, and
`output/websocket`. This is a direct-import census only, not the measured transitive closure.

Keep the existing explicit component registration cut. Measure `go list -deps ./...` and
`go list -deps -test -tags=integration ./...` at both pins, retain package lists, module versions, commands,
and non-test line counts, and distinguish production, test-only, and registry-driven dependencies. The
`payloadbuiltins` closure must be visible even when it contains unused agentic/provider packages. Do not
call a smaller direct-import set a qualified extraction boundary.

## D3: Compatibility work follows evidence

The exact target contains the following breaking obligations. Record compile/runtime failures and add
behavior tests before adapting callers; do not infer success from the release-note descriptions.

1. Services/components and managers take `Stop(context.Context)`. Create a fresh bounded shutdown context
   at the process root. Controlled Stop keeps Start's context live until Stop returns; cancellation of the
   Start context first is the abort path. Join all app-owned loops and test both paths and failed start.
   The baseline signal handler currently cancels the run context before StopAll. Its sim Stop joins only
   the tick loop and drain pool, leaving publisher/probe/spawn/watch/churn join obligations unresolved.
   Stop admission and join pool submitters before waiting for the pool; report a bounded-join timeout
   instead of silently treating it as completed. Baseline race integration has reproduced the
   cull-watcher WaitGroup Add versus Stop Wait race in `TestPredatorCullReclaimsBoid`; preserve that
   baseline failure and attribute its fix to application lifecycle ownership. Preserve the target's
   one-shot component lifecycle;
   application restart means a fresh process/component set, not reviving a stopped instance.
   Client Close does not discover or stop children. Native consumer ownership documentation explicitly
   does not promise a stronger Connect/Close protocol, settled async publishes on Close, or restart proof.
2. Runtime instances leave `component.Registry` (ADR-096). Capture narrowly typed application control
   interfaces at factory composition and inject them into the API. Do not create a general live registry.
   Rule definitions retain a dedicated live activation path; sealed component composition does not
   authorize regressing the existing live toggle behavior.
3. ADR-102 canonical order is `org.platform.system.domain.type.instance`; use registered system `sim`,
   effective authority, and consistent boid/zone IDs. Remove `platform.instance_id` and fallback authority.
   Read the minted effective platform only after config Manager.Start (ADR-104), propagate it everywhere,
   and verify retained restart does not mint a second authority. Fresh brokers avoid incompatible old state.
4. ADR-103 makes the payload registry the type/contract authority. Preserve actual boid, zone, and lifecycle
   birth registrations, indexing floors and contracts. Verify a production decoder round trip and accepted
   births. Neighbor references must retain relationship semantics, not merely resemble entity IDs.
5. ADR-107 closes predicate datatypes. Current phase/neighbor declarations use accepted `string`, but the
   neighbor's intended relationship semantics require explicit review. Do not treat successful registration
   as proof that indexing and clustering consume its objects as edges.

## D4: Preserve independent writer facts and exact removal semantics

Snapshot writes own position, velocity, neighbor count and current neighbor relationships. Lifecycle owns
`flock.lifecycle.phase`. Repeated snapshots must preserve active and terminal phase facts; the neighbor
clear must affect only its declared group. Prove replacement A→B and nonempty→empty from authoritative
entity readback and eventual indexes, not the UI count sentinel alone.

Reclamation policy is explicit and remains stable: at most five read-authoritative/delete attempts;
`revision_conflict` or `unavailable` is a definite non-commit and permits a fresh read and retry;
`not_found` is already reclaimed; `commit_unknown` is never blindly retried and increments the failure
metric. Empty-neighbor clear permits at most three attempts with the same categories. Unknown errors,
exhaustion and ambiguity remain visible. Existing claim that final in-flight snapshots always drain before
reclamation converges must be exercised, not assumed: pause/release delayed work to detect resurrection.

Successful cull qualification requires no deleted entity, no surviving authoritative boid relationship to
that entity after the controlled workload quiesces, and no stale incoming/outgoing index or graph-pane
edge. Distinguish allowed propagation lag from persistent stale state. The substrate delete intentionally
preserves surviving sources' incoming assertions; current surviving boids must replace/clear those assertions
through later snapshots before the workload is quiesced. A missing relationship target can be legal in the
substrate generally; this workload's stronger no-stale-boid-relationship contract needs
its own evidence. Do not relabel a deleted entity with stale inbound relationships as success.

## D5: Baselines precede the pin change

Save immutable baseline binaries/source identity and effective config before changing go.mod. Run current
unit/race/integration gates, known-answer cases, startup/retained restart/cancellation/shutdown, and browser
proof. Add the same behavioral assertions to both comparison builds where possible. Record existing
failures as baseline failures; do not erase them by changing expectations on the target.

Run repeated interleaved pairs (A/B, B/A or A/B/A/B) on isolated brokers and stores, using one immutable
NATS image, seed, starting population, cadence, churn schedule, tick rate, runtime duration, warmup,
CPU/memory limits, Go toolchain and app flags. Restart creates a fresh process on that run's retained store;
independent repetitions get fresh stores. Save exact commands/config hashes and machine/container identity.
Avoid concurrent heavy work that invalidates comparisons. No numeric improvement target is assumed.

Authoritative state polling must accompany logs. At 30–60 second intervals capture HTTP process state,
JetStream consumer/stream state, graph revisions/population and wall-clock progress. A stagnant operation
beyond twice its expected step time requires diagnosis and prompt abort when wedged is proven.

## D6: Name the accounting boundary before interpreting metrics

| Signal | Required interpretation |
| --- | --- |
| Offered load | Planned cadence × time-varying population; record actual offers/drops separately |
| `boids_graph_entities_published_total` | Entities in wholly ACKed publish batches; not applied graph writes |
| `boids_graph_snapshots_published_total` | Baseline increments even after batch failure; coordinator iterations |
| `boids_graph_snapshots_dropped_total` | New offers dropped when the bounded publisher channel is full |
| `boids_graph_publish_duration_seconds` | One coordinator snapshot including joined batch and empty clears |
| `boids_graph_e2e_latency_seconds` | Observed ENTITY_STATES writes minus observed_at, subject to watcher delivery |
| Substrate processed load | Verify exact target/baseline counter, labels and increment site before use |
| JetStream backlog | Report pending and ACK-pending separately; missing scrape is unknown, never zero |
| Physics responsiveness | Tick/frame progress and observed intervals under load, not process CPU alone |
| Browser loss/read-side lag | WebSocket drops and graphview coalescing/watcher loss remain distinct |

Baseline source evidence: `publisher.go` advances the snapshot counter outside the publish-success branch;
its entity counter advances only after the entire batch returns success. Partial batch successes can thus
be absent from the counter. The current spec's historical heading says drop-oldest while implementation
and scenario text drop new offers. Preserve and disclose this behavior; do not silently change the
instrument during a substrate comparison. A corrected instrument requires same-instrument reruns of both pins.

Capture raw Prometheus HELP/TYPE and samples on both pins, source increment sites, JetStream consumer
identities, latency distributions (count/buckets, not unsupported precision), drops, memory, CPU, and tick
responsiveness. Attribute changes to app adaptation, substrate behavior, metric semantics, or run variance.
If evidence cannot distinguish causes, label the result unresolved rather than making a causal claim.

## D7: Future SemEngine admission is a separate decision

These are SemBoids requirements for a later consumer contract, not capabilities already admitted to SemEngine.

| Capability to admit | Required scope and proving workload |
| --- | --- |
| Graph mutation/ingest | Batched Graphable lane, registered births, partial predicate ownership, exact reads |
| Relationship correctness | Full replacement, empty reconcile, fenced delete, applied-input barrier, index convergence |
| Rules | Message-triggered conditions, substitution, publish actions, lifecycle actions, live activation |
| Lifecycle | Seed/spawn active, conditional transitions, independent writers, explicit ambiguous outcomes |
| Clustering | Retained graph-index adjacency and LPA/community lifecycle consumed by the graph pane |
| Transport | Core-NATS frames/events, JetStream ACK contract, bounded WebSocket clients, graphview watches |
| Host/operations | Effective identity, sealed boot, mutation-provider readiness, controls, cancel/drain/join/restart |
| Measurement | Stable throughput/backlog/latency/drop semantics, profiling, required dependency closure |

Each admitted row needs a named SemEngine owner, keep/change/defer ruling, source provenance at the frozen
pin, transitive and test-only closure, side effects, critical tests and workload results. The SemSource
first-consumer contract cannot silently admit SemBoids' additional rules/lifecycle/clustering/transport
closure. Simulation semantics stay in SemBoids. Until the separate contract and complete workload qualify,
this migration and its reference runs use SemStreams exclusively.

## D8: Observed cold-start contract gap

The first target 30Hz trial failed before measurement: simulator requests reached graph handlers during their
startup window, the shared NATS circuit breaker opened, and graph-index could not create `GRAPH_STATUS` despite
a healthy broker. The component manager starts `StoreProvider` components first, then all ordinary components
concurrently. Its boot-resource dependencies do not provide general component ordering.

Independent review rejected a query-only readiness workaround. Graph-ingest registers query handlers before
mutation handlers; a successful query does not prove mutation readiness. Its public readiness reading also
lacks a producer boot identity or KV revision, so a recent retained envelope can satisfy a fast restart.
No application-side readiness or recovery framework is added. This remains a qualification blocker at the
frozen pin, separately from delayed-snapshot resurrection. The reviewed provider contract request is
[SemStreams #1447](https://github.com/C360Studio/semstreams/issues/1447).

Steady-state comparisons may start both pins at zero snapshot cadence on fresh isolated stores, require
actual complete startup and provider progress, then activate the identical public snapshot dial before the
same warmup and measured windows. Activation evidence and the original cold-start failure must be retained.
Passing that separate experiment does not qualify startup at the requested load or retained-state readiness.
