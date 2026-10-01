# Independent migration review

Reviewer: separate review agent, with no ownership of production code, tests, or the measurement harness.
Date: 2026-10-01. Status: **HOLD — delayed-snapshot reclamation and high-load cold startup are unqualified.**
The first production snapshot was reviewed after both owners declared it frozen. Review reopened after an
isolated target load run failed during startup; the supported contract audit led to an explicit blocker and
no production workaround. The same frozen production binary remains the subject of browser and load evidence.
Corrected WebSocket observer coverage was used for all eight final matched runs. Historical artifact hashes
are in `initial-measurement-SHA256SUMS.json` and `source-SHA256SUMS.json`; the UTC test correction and current
reviewed source identities are recorded separately in `final-source-SHA256SUMS.json`.

## Required correctness blocker

`TestReclaimedBoidRejectsDelayedPreCullSnapshot`, behind `integration && qualification`, fails on both the
frozen beta.160 baseline and frozen shared target. Each log reports a successful revision-fenced deletion
with receipt revision 1 followed by a held, pre-delete Graphable recreating the entity at revision 3.
Reviewed logs: `/tmp/semboids-shared-pin/red-reclamation-baseline.log` and
`/tmp/semboids-shared-pin/red-reclamation-target.log`.

This is an existing substrate/consumer contract gap, not evidence of an introduced target regression.
A successful delete, zero reclaim-failure counter, normal integration pass, or an empty graph at one instant
does not establish the user's no-resurrection requirement. Ordinary migration gates exclude this explicit
qualification tag, so they must not be presented as complete correctness qualification. Keep the PR draft
and keep this blocker visible. The fix requires an upstream admission/contract decision, not an app-side
tombstone store or private recovery subsystem.

## Measurement findings and resolved dispositions

- **M1 (P2): conservation eligibility.** `analyze.py` treats unchanged `first_seq == 1` as proof of no retention
  loss. Interior stream removals, filter coverage, changed consumer identity/config, and documented redelivery
  prerequisites are not checked. Qualify accounting from captured stream/consumer state, or return null with
  an explicit reason.
- **M2 (P2): cleanup interruption.** `load.py` calls WebSocket wait and socket shutdown before process/container
  cleanup without independent protection. Either exception skips remaining cleanup. Ensure every owned
  resource is stopped even when an earlier cleanup fails; preserve failure evidence.
- **M3 (P2): live-probe assertions.** `operations.py` marks probes passed after five rows and an advanced
  stream sequence; it does not assert advancing ticks or expected population. Assert those conditions or
  narrow the claimed check. Current baseline rows demonstrate them, but final probes should enforce them.

These findings were sent to the coordinator before final interleaved measurements. No harness edits were
made by the reviewer. They do not invalidate raw baseline evidence: conclusions must be restricted to what
the retained state and samples actually establish.

**M1–M3 resolved.** Independently inspected the remediation and ran all eight Python tests successfully.
The analyzer now checks retained sequence density, deletion records, consumer identity/config, full subject
coverage, ACK/delivery policy, and redelivery evidence. It conservatively returns null for finite delivery
limits because retirement can resemble successful work. Cleanup independently attempts each resource even
when WebSocket wait or frame socket close throws; injected failures prove application and broker cleanup
still run. Live probes now require strictly advancing ticks, population 30 in every observed row, and graph
sequence progress. Fixed harness hashes are in `measurement-remediation-SHA256SUMS.json`.

A ninth regression test subsequently passed after adding population-weighted offered-load inference.
The analyzer sums physics-frame population at the same snapshot-eligible ticks used by the producer,
returns a null rate on observed tick gaps/reordering, and discloses receipt-versus-scrape boundary skew.
This is an inferred offer count, separate from whole-batch ACKed publication and committed entity writes.

## Evidence and accounting audit

- Independently ran all three Python harness regression tests: pass. They cover absent series, ACK-pending
  outstanding work, labeled counter totals, histogram window subtraction, and overflow lower bounds.
- Read the baseline gate records and full failure location: unit race passes; integration race fails in
  `TestPredatorCullReclaimsBoid`, with `drainPool` Add/Wait ownership racing during cleanup. The baseline
  documentation preserves that failure rather than retrying it into a pass.
- Checked source increment sites at both pins for the cumulative consumer delivered/ACK/redelivery trap.
  The harness excludes those faulty rate counters from interpretation.
- Checked the application batch ACK and snapshot counters, lifecycle cull timing, graph-ingest commit
  counter, and WebSocket send/error update sites. The metric contract correctly distinguishes accepted
  publications, completed snapshot attempts, committed writes, staged removals, and socket writes.
- `pending + ACK-pending` is retained from server state. Missing sparse series and empty latency windows
  remain null; no absent error family is represented as proof of zero errors.
- The pre-upgrade overload diagnostic is correctly excluded from final ratios because a cached test
  overlapped it and it lacked the later WebSocket observer.
- The initial occupied-port result was **invalidated** after the workers found that an IPv4-only blocker
  did not prevent Go's IPv6 listener on this Mac. Successful binding was visible in the logs; remaining
  alive was not a startup defect. The initial review accepted that erroneous classification and withdraws
  it explicitly. A corrected dual-family probe must supply a real address-in-use error before comparing
  failure behavior. The startup SIGTERM and separate slow-socket evidence remain independent of this error.
- Corrected `baseline-port-conflict` and `target-port-conflict` probes now satisfy that requirement. Both
  hold IPv4 and IPv6 listeners and show explicit bind errors in the application logs. The target exits 1
  in 0.175s; the baseline remains alive beyond 25s and then receives SIGTERM. This supports a real target
  failed-start improvement with valid evidence, without rehabilitating the earlier invalid probes.
- The slow socket probe supports physics isolation and exposes ordinary-client latency; it does not claim
  a universal transport drop count or healthy ordinary-client latency.
- Each load run creates fresh NATS storage and uses the same immutable image digest; process/binary hashes,
  generated configs, raw metrics, frame rows, logs, and direct server consumer state are retained. Runs must
  remain serial and free of competing build/race/browser workloads for performance attribution.
- Independently verified the baseline archive SHA against its index and recomputed baseline metrics directly
  from archived raw exposition: 2,376.305 committed writes/s, p50 25.240s, p99 64.065s, outstanding work
  71,867 to 234,923. Both ENTITY endpoints have contiguous retained sequences and the consumer filter is
  `entity.>`, matching the stream. However, finite `max_deliver` means this arithmetic cannot distinguish
  completed work from retirement. The earlier independent-review statement supporting consumer completion
  is withdrawn. The directly measured committed-write counter remains valid; conservation is null.

## Frozen production and test review

No additional production-code defect was found in the frozen source beyond the disclosed reclamation and
startup contract gaps. This statement is bounded by the listed source hashes and is not migration acceptance.

- `internal/flock`, steering, tracker, frame/population staging, publisher, probe, reclaimer, and drain-pool
  production bodies are unchanged. Seeded physics and sequential batched publications retain their original
  algorithms. The shared pin and dependency sums change without a `replace` or SemEngine dependency.
- The host resolves effective identity only after config manager Start. Components and boot-time zone births
  receive that authority. Payload registration declares indexing floors, neighbor contract ownership, and
  `entityid` relationship datatype. The actual boid/zone ID byte layout already used canonical system-before-
  domain order; the stale zone comment was corrected rather than changing entity identity ordering again.
- Static declarations retain upstream factory metadata and declare authored sim steering, zone events,
  snapshots, frames, and graph mutation endpoints. API access uses two narrow, synchronized handles captured
  during factory creation; there is no replacement general live-instance registry.
- Component Start subscribes before spawning owners, is explicitly one-shot, and starts every loop in one
  lifetime wait group. The finalizer joins submitters before drain-pool Wait, fixing the baseline Add/Wait
  race. Stop retains and drains the steering subscription, cancels owners, and reports incomplete joins.
- `runServices` distinguishes startup abort from controlled shutdown, joins Start before failed-start
  cleanup, and supplies a fresh bounded Stop context. The native NATS dial still determines how quickly an
  in-progress initial connection honors cancellation; no stronger guarantee is inferred from unit fakes.
- Graph views derive from the service Start lifetime. Their owned finalizer joins native watcher cleanup;
  caller Stop deadlines return an error without claiming that native Stop has finished. The host worker's
  red test caught API Stop leaving BaseService running on a view timeout; the final `errors.Join` calls both
  stop paths. Reviewed red log `host-evidence/12-stop-red.txt` and green `13-final-race.txt`.
- The lifecycle/snapshot integration fixture now proves both indexes saw the fixture before asserting
  retraction. It compares lifecycle-owned facts around active/terminal snapshots and empty reconciliation.
  This demonstrates convergence after surviving sources publish their new relationships, not a stronger
  promise that deletion itself removes other entities' assertions.
- Retry tests exercise unavailable/conflict reads and deletes, already-absent completion, ambiguity,
  invalid outcomes, five-attempt exhaustion, and strictly fresh revisions on repeated deletes. Existing
  neighbor-clear classification and sequential-batch tests remain. The delayed-publication qualification
  is a separate red assertion and remains an admission blocker.

Focused host/API/registry race tests passed in reviewed `13-final-race.txt`. Final full ordinary integration
race log `target-evidence/race-integration-final.log` has seven tested packages passing and no skips or race
warnings. Reviewed `gate-status.json` records successful native build, vet, integration vet, empty gofmt,
revive, unit race, and Linux/amd64 build. Initial target gate failures remain separate evidence: the debug
fixture omitted required payload registration, the strengthened index fixture timed out, and the original
physics budget test failed under the race-plus-coverage run. Final pass must not erase those observations.
The debug fixture registration correction is appropriate and does not change product semantics. Detailed
fixture/performance attribution in `target/README.md` has now been reviewed: the index fixture assumed
identity appeared only in values, whereas incoming indexes identify it in composite keys. The correction
accepts the native key/value representation while preserving both pre-removal and post-removal assertions.
The ordinary final race suite records 215 test/subtest passes; the unit suite records 197, both without skips.

Hosted CI then exposed a test-only time-zone portability bug. Independent review approves the correction:
boid and zone round trips now cover UTC, Local, and a fixed offset, compare the exact timestamp instant with
`Time.Equal`, and retain full equality checks for every remaining triple field. The earlier reflection check
compared `time.Location` representation after JSON decoding. The local UTC failing-first and passing logs are
retained; production payloads do not change. The final UTC unit race suite reports 203 passes and the uncached,
default-parallel real-NATS race suite 221, with no skips. Native/Linux builds, both vet modes, gofmt and revive
also pass in `gate-status-utc.json`. These ordinary gates still exclude the deliberately red qualification tag.
The original measured binary remains `4153b224f0a204f2d5349ac0b167288d6ca1223b3082f6ac2368534b34e7f625`;
the compilation revalidation binary has a different embedded VCS stamp, documented in `rebuild-metadata.diff`.

## Dependency closure review

Independently recounted the retained TSVs: production package entries change 517 to 514, SemStreams packages
60 to 62, reached external modules 67 to 64, and selected modules 298 to 303. The target explicitly retains
testcontainers/Docker plus agentic/provider reachability. The module selection delta distinguishes selected
dependencies from production-reachable modules; it does not claim that added selected modules are linked
into the executable. Directory line counts intentionally include alternate-platform/tag files, and the
documentation distinguishes them from binary size or an approved SemEngine extraction boundary.

The target's two source-size numbers have different scopes: 131,061 lines in build-selected non-test GoFiles
versus 131,066 lines across all non-test files in the selected package directories. Independently traced the
five-line difference to `processor/graph-index/race_enabled.go`, which ordinary Go-list ignores while selecting
`race_disabled.go`. The final machine summary preserves both historical values and explicitly labels each
method and the exact five-line difference; this resolves the documentation finding.

The shared-pin authority and exact baseline/target identities match `go.mod`, recorded module provenance,
and the migration design. The future SemEngine matrix requires explicit graph, relationship, rules,
lifecycle, clustering, transport, and host admission; the change does not cut over the consumer.

## Browser evidence review

Independently inspected `browser/target-final.jpg` and `target-observations.json`. Both panes visibly render:
the flock has zone geometry and boids; the graph has nodes, edges, and multiple community colors. The header
shows open delivery, 220 boids, tick 1254, and graph cadence 1Hz. The observation record documents live
predator toggle off/on, cadence changes, a 25-boid spawn wave, disconnect/reconnect after application restart,
and retained effective authority. The image alone does not prove those temporal actions; the coordinator's
interaction record and associated process logs provide their evidence.

The final browser binary is SHA-256
`4153b224f0a204f2d5349ac0b167288d6ca1223b3082f6ac2368534b34e7f625`.
The record explicitly avoids claiming graph/physics population equality because culling remained enabled.
No UI production code changed. Browser evidence hashes are retained in `browser/SHA256SUMS.json`.

## Cold-start contract and experiment scope

The first final target load attempt exposed a bootstrap ordering failure: sim lifecycle/snapshot work reached
the broker before graph providers were ready, a circuit breaker opened, and graph-index startup failed while
the broker remained healthy. This raw run is retained. It establishes an observed target failure, not a
beta.160-to-target regression or a throughput comparison.

The frozen source supports `RequestReadyClassified` for first reads and avoids charging expected readiness
misses to the shared breaker. It cannot establish the complete provider barrier required here: graph-ingest
installs query handlers before mutation handlers, and query admission checks entity bootstrap, not completion
of mutation registration. The public retained readiness envelope lacks a producer boot identity or KV revision;
a value less than 15 seconds old can belong to the prior process. The component manager orders StoreProviders
before concurrently starting ordinary components, without a general component dependency barrier.

The outcome of closed SemStreams #874 also provides no hidden startup retry: the canonical mutation client
sends exactly one `RequestClassified` request, classifies no responders as unavailable and post-send transport
ambiguity as commit unknown. Retry policy remains explicit and caller-owned. Independent review rejects a
query-only gate as a full startup fix, approves recording this gap as [SemStreams #1447](https://github.com/C360Studio/semstreams/issues/1447),
and approves design D7/D8's explicit future SemEngine admission requirements. No app-side provider framework,
arbitrary delay, mutation probe, private tombstone or ambiguity retry is justified by this migration.

The separate steady-state harness is approved for fresh isolated brokers only. Both pins start at snapshot
cadence zero. Before activating load it requires all six required components enabled, started and healthy;
200 successful seed creates and no culls; complete/ready graph-ingest and graph-index with zero lag; zero
pending plus ACK-pending consumer work; and zero graph publications with the dial still zero. Missing values
fail admission. Every poll is retained. The public dial activation must return HTTP 200 and the requested
cadence before the identical warmup and measurement windows begin. This protocol does not qualify direct
30Hz cold startup or retained-state readiness. The eleventh harness regression test exercises partial startup,
missing metrics, unhealthy components, missing seed creates, culls and outstanding work. After the campaign
released its exclusive window, this reviewer independently ran all eleven harness tests: pass.

## Final performance and evidence review

The original WebSocket observer closed at roughly 60 seconds with code 1006, leaving the final part of a
measurement window uncovered. This required exposing disconnect/reconnect intervals and preserving tick gaps
across them, followed by rerunning both revisions with the corrected instrument.

The instrument correction is now reviewed: a tenth regression test explicitly drives disconnect, reconnect,
and resumed frame delivery. All ten tests pass. The observer timestamps close/error/connect events, retries
after 500ms, and retains tick continuity. The analyzer compares received WebSocket ticks against NATS frames
over the full measurement window, so an absent tail no longer appears lossless merely because its internal
sequence has no gaps. Reconnect durations and raw events remain visible. This measures a windowed delivery
deficit, not universal permanent loss. The aborted cohort remains excluded from matched comparisons.

Independently recomputed all eight selected runs from raw evidence, without calling the production analyzer:
Prometheus committed-write deltas, NATS accepted stream sequences, pending plus ACK-pending backlog, offered
population at eligible ticks, histogram window p50/p99, physics FPS, snapshot drops, and whole-window WebSocket
tick deficits. They match the final aggregate. `independent-load-accounting.json` retains the calculations and
104 input-file hashes. The archive has all 104 matching members. Its SHA-256 is
`b1322f5f8dc30c45bc32768a1155a076b83cb2c91c34502416944005a5542401` (3,660,867 bytes, 21 run directories).
The separate exclusion ledger distinguishes the aborted root-level cohort from selected `steady-state/` runs;
invalid original port and observer assertions remain withdrawn rather than silently rewritten.

Manifest timestamps confirm A/B/A/B stable runs followed by A/B/A/B churn runs, with the same exact two binary
hashes, immutable NATS image, seed, initial population, tick rate, per-profile zones/cadence/churn, warmup and
window. Every admission and dial exchange succeeded. All eight processes exited zero without forced kills.
The six common sampled stable-state hashes at ticks 300, 600, 900, 1200, 1500 and 1800 match in all four runs;
this supports those checkpoints, not all-frame identity or deterministic asynchronous churn.

Stable target/baseline committed-write ratios are 0.713 and 1.085. Their reversed direction and within-pin
variance do not justify a migration speedup or slowdown. Both pins accumulate large backlogs under roughly
6,000 offered entities/s, while physics stays near 30Hz without missing observed NATS ticks. The first pair
drops four baseline and three target snapshot offers; the second drops zero. Churn offers roughly 409 entities/s,
but its population and cull counts differ: observed culls are 0/1/0/6. The report correctly withholds causal
attribution for that difference and does not present sparse culls as sustained reclamation qualification.
Finite delivery limits leave consumer-completion inference null in every run; committed writes include
lifecycle and other mutation paths and remain a distinct observation.

Every selected run reproduces the same receive-only WebSocket disconnect/reconnect, a 15-tick window deficit,
and approximately 0.5-second reconnect. Source read-deadline behavior at both pins supports that shared
limitation. Separate slow-client observations support physics isolation while exposing ordinary-client latency;
they do not enter load ratios. Rule firing-cap warnings remain visible and are distinct from snapshot/frame
drops. The final measurement report, main summary, metric contract and exclusion ledger respect these limits.

Final source and evidence identities are in `final-source-SHA256SUMS.json` and
`final-evidence-SHA256SUMS.json`. The reviewed hosted CI record is green for source head
`58d9b7fe4007c65732c9aa6661aad85feb5f8161`; ordinary CI does not execute the failing stronger reclamation tag.
No competing Go, Docker, or harness test workload was started by this reviewer during measured runs.

## Independent disposition

**Approve the reviewed compatibility implementation, test corrections, dependency inventory and bounded evidence
for the draft migration PR. Hold migration/workload acceptance** on the observed delayed-snapshot resurrection
([#1444](https://github.com/C360Studio/semstreams/issues/1444)) and high-load cold-start contract
([#1447](https://github.com/C360Studio/semstreams/issues/1447)). No further local implementation workaround is
approved. The proposal stays open, the exact frozen pin stays unchanged, and SemEngine requires the separately
owned consumer contract and workload admission recorded in design D7/D8.
