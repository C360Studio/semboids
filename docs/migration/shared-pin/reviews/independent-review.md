# Independent migration review

Reviewer: separate review agent, with no ownership of production code, tests, or the measurement harness.
Date: 2026-10-01. Status: **HOLD — qualification is incomplete and a required reclamation invariant is red.**
The production snapshot has been reviewed after both owners declared it frozen. Final performance and
operational evidence is still pending. Exact measurement artifact hashes are in
`initial-measurement-SHA256SUMS.json`; reviewed source hashes are in `source-SHA256SUMS.json`.

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

No additional production-code defect was found in the frozen snapshot. This statement is bounded by the
listed source hashes; it is not migration acceptance while the required qualification remains red.

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
fixture/performance attribution in the target evidence is still being prepared.

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

## Final review still required

Review final target operational probes, detailed target gate attribution, dependency closure, and repeated
interleaved results when their artifacts are frozen. Preserve exact reviewed hashes and dispositions.
Do not promote this staged review to full migration acceptance while the reclamation invariant is red.
No competing Go or Docker workload was started by this reviewer during measured runs.
