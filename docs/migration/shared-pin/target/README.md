# Exact shared-pin compatibility evidence

The target is SemStreams `v1.0.0-beta.162.0.20260930150212-8b99efe9c66a`, full commit
`8b99efe9c66a4faa4fa509f9f62cc6bad8392128`, reused from the frozen SemEngine SETUP 03A consumer baseline.
The baseline remains `v1.0.0-beta.160` / `8403a2218000e45a31c5132fbfe01af42ed04f14`.
No newer tag or commit was selected. Both were built with Go 1.26.4 on darwin/arm64.

## Result

All ordinary backend gates pass, including uncached default-parallel real-NATS race tests under `TZ=UTC` after
the test correction below. High-load cold startup is **BLOCKED** on the target. The stronger delayed-snapshot
reclamation qualification remains **BLOCKED** on both pins: a snapshot prepared before deletion can recreate the entity after the exact-revision delete commits.
Do not interpret ordinary green gates as full migration qualification. Browser and measured load evidence are
recorded in the adjacent directories; those results do not remove this blocker.

| Final gate | Outcome | Evidence |
| --- | --- | --- |
| Native build | PASS | `build-native-utc.log`, `gate-status-utc.json` |
| Go vet, ordinary and integration | PASS | `vet-utc.log`, `vet-integration-utc.log` |
| gofmt and revive | PASS | `fmt-utc.log`, `revive-utc.log` |
| Uncached race unit tests, UTC | PASS, 203 test/subtest passes, no skips | `race-unit-utc.log` |
| Uncached real-NATS race integration, default parallelism, UTC | PASS, 221 passes, no skips | `race-integration-default-parallel.log` |
| Linux/amd64 build, CGO disabled | PASS | `linux-build-utc.log` |
| Schema drift | NOT APPLICABLE, existing CI explicitly defers this gate | `.github/workflows/ci.yml` |
| Delayed pre-cull snapshot qualification | FAIL on baseline and target | `red-reclamation-*.log` |
| High-load target cold startup | FAIL | `cold-start-failure.log.gz` |

Real-NATS tests used isolated testcontainers running `nats:2.14-alpine` with image digest
`sha256:4063edae0717ba5f7501bfde75f97fd9b57f5b93597b92c70b6a6fbbf6a74e06`, as did the recorded baseline gate.

## Compatibility changes and attribution

- Component and service shutdown take caller-owned contexts. The host keeps the run context live during controlled
  shutdown, owns a separate bounded Stop context, and joins partially started services on failure.
- Sim instances are one-shot. Every publisher, probe, spawn creator, cull watcher, churn loop and tick loop joins
  before the lifecycle drain pool is waited. This fixes a **pre-existing beta.160 race**, where cull submission
  could call WaitGroup.Add concurrently with Stop's Wait. `red-one-shot.log` records the new behavioral red test;
  the baseline full integration log records the original race. Incomplete joins return cancellation/deadline errors.
- Sim retains and drains its exact steering subscription; canceling the Start context alone does not own native
  subscription cleanup. The API graph views inherit the service run context and have bounded, observable teardown.
- Static sim ports describe its actual frames, zone events, steering, snapshot and mutation endpoints. The target's
  sealed composition rejects missing internal producers. No internal connection was bypassed with `external:true`.
- API live controls are captured through narrowly typed factory composition; the retired live component registry
  is not recreated. Rule toggles, graph cadence, spawning and churn use those captured controls.
- Effective authority is read after Config Manager establishes it. The removed `platform.instance_id` field and
  sim authority fallbacks are gone. Existing boid and zone IDs already use `org.platform.sim.flock.type.instance`;
  their canonical system/domain positions do not change. Domain delegation is explicit for the identity audit.
- Boid and zone payloads declare the control indexing floor. The boid registration carries its neighbor contract.
  Neighbor predicates declare the target's `entity_id` datatype; phase remains a string. Registered concrete
  payload decoder round trips preserve identity and triples. The real fixture proves accepted boid/lifecycle births.
- Integration construction calls the registered factory directly instead of the now admission-token-only registry
  method. Production boot still exercises sealed composition. Previously minimal cluster/debug fixtures now register
  their boid payload and use the closed `float` datatype. Rule fixtures gain display names to remove their baseline
  event-validation errors; these are test-fixture adaptations, not simulation changes.
- Physics algorithms, seeds, frame encoding, snapshot batching and drop policy are unchanged. Reclamation retains
  five fresh-read attempts; neighbor clearing retains three. Definite revision conflicts/unavailability retry,
  missing entities succeed, ambiguous outcomes remain visible and are not blindly retried.

The new real-NATS regression compares phase and all `lifecycle.*` facts before and after snapshot updates and an
empty-neighbor clear, including a terminal phase. It proves the initial fixture reaches both indexes before asserting
replacement and cleanup. After deleting a neighbor, surviving snapshots replace and then empty their relationships;
authoritative facts plus incoming and outgoing indexes converge without a stale reference.

## Reclamation qualification blocker

Run the intentionally separate, unskipped qualification assertion:

```sh
GOCACHE=/tmp/semboids-go-build-cache go test -count=1 -race -tags=integration,qualification \
  -run '^TestReclaimedBoidRejectsDelayedPreCullSnapshot$' ./internal/boidgraph
```

On **both pins**, the same held snapshot (tick 1, serialized before deletion) is released after the exact-revision
delete. Once the consumer reports zero pending and zero ACK-pending messages, the entity has reappeared at revision 3;
the delete receipt reported revision 1. The red test retains the user's required no-resurrection assertion. It is not
skipped or weakened in ordinary gates; it is an explicit additional qualification gate that currently fails.

This schedule is reachable because the snapshot publisher and cull watcher/reclaimer run independently, without an
applied-input barrier between the last submitted snapshot and reclamation. A successful fenced delete protects its
current revision; it does not publicly promise to reject future Graphable births. This stronger consumer requirement
maps to [SemStreams #1444](https://github.com/C360Studio/semstreams/issues/1444). No local tombstone, substrate fork,
or retry-on-ambiguous recovery scheme was introduced. Frozen-pin migration qualification remains blocked pending an
upstream contract/disposition. The baseline reproduction used a separate archive overlay with the same new test files;
the original baseline archive and binary were unchanged.

## Cold-start qualification blocker

The first interleaved target run started 200 seeded boids with 30 Hz snapshots on a fresh isolated broker and
failed during startup. The complete log is retained as `cold-start-failure.log.gz`. At 06:53:42.065–.114 -05:00,
neighbor clearing received no-responder errors. At .117, the shared client reported an open circuit; tick 3's
200-entity batch could not enqueue. Graph-ingest registered its query and mutation handlers at .145; graph-index
then failed to start at .147 because its `GRAPH_STATUS` initialization reported not connected. The process exited
with startup failure. This is operational failure evidence, not a throughput observation.

The frozen source's `service/component_manager.go:startComponentsBarrier` starts StoreProviders first, then all
ordinary components concurrently. `Registration.Dependencies` declares framework boot resources rather than
component-to-component ordering. Sim's off-loop snapshot and seed-create workers can therefore run before graph
handlers exist. This application/provider startup boundary is unqualified on the target; the failure alone does
not establish a beta.160-to-target regression or isolate a throughput cause.

The supported `natsclient.RequestReadyClassified` first-read primitive deliberately avoids counting expected
cold-start no-responders toward the shared circuit breaker. It is insufficient by itself here: graph-ingest installs
query handlers before its four mutation handlers. A query reply cannot license launching lifecycle mutations.
The public `graph/readiness.Watcher` also cannot prove a new process has installed those handlers: its first reading
can be a recent retained `GRAPH_STATUS` value, considered fresh within 15 seconds; the public reading has no
producer boot identity or revision to distinguish that value from this startup. Retained-broker restart matters.
No private provider barrier, fabricated mutation probe, arbitrary sleep, changed retry policy, or substrate fork was
added. A supported handler-readiness/start-order contract is needed before this startup requirement can be admitted.

Any subsequent matched performance campaign that starts both pins at zero snapshot cadence, waits for observed
startup readiness, then activates the same cadence and warmup is a **steady-state comparison only**. It does not
remove this cold-start blocker. The original failed run remains retained and excluded from throughput claims.

## UTC test correction

The first hosted CI run exposed a new test's `reflect.DeepEqual` comparison of `time.Location` pointers. JSON
preserved the instant, but `time.UnixMilli` used Local while decoding a `Z` timestamp used UTC. The failure
reproduces locally under `TZ=UTC` in `red-roundtrip-utc.log`. Both boid and zone round-trip tests now cover UTC,
Local and a fixed offset; they compare timestamps using `time.Equal`, then compare every other triple field.
`green-roundtrip-utc.log` is the passing focused race rerun. Production payloads and physics are unchanged.
The original frozen target executable remains SHA256
`4153b224f0a204f2d5349ac0b167288d6ca1223b3082f6ac2368534b34e7f625` for measured runs and browser evidence.
The rebuild used only to revalidate compilation has SHA256
`27ec0269033802e33836ef120d760b2f9df338515b08a5f8a1c928d04f1540fb`; its embedded VCS stamp changes from the
pre-commit working tree to commit `97d24dc082016aa5af1840bb0a52c87021048a18`. `rebuild-metadata.diff` records
the metadata difference. The rebuilt artifact does not replace the frozen measured binary.

## Coverage and intermediate failures

A diagnostic `-race -coverprofile` integration run measured the critical operations: snapshot publication 92.0%,
empty-neighbor clearing 94.7%, fenced reclamation 95.8%, spawn retry 91.7%, and sim Stop 93.8%. The sim package reached
81.8%, zone 80.3%, API 78.1%; coverage is reported by behavior, not used to hide unproven contracts.
`coverage-functions.txt` and `coverage-integration.out` retain that diagnostic observation.

That instrumented run was **not** a passing gate: it found an unregistered diagnostic fixture, an incorrect new test
assumption that incoming-index values contained identity (the identity is in the keys), and a 10.10ms physics timing
measurement against a 5ms bound under combined race/coverage overhead. The first two fixtures were corrected and
passed focused reruns. Physics code and its bound were unchanged. The required ordinary race gate was then rerun
without coverage instrumentation and passed, including the timing test. The original diagnostic failure remains in
`race-integration.log`; `race-integration-final.log` is the passing ordinary gate.

## Dependency closure

The same census methodology was used for both pins. `non-test-loc.json` comes from the shared
`measure_closure.py`: raw non-test Go directory lines for reachable packages, including other platform/build-tag
files. It is not linked binary size or an extraction admission decision. `closure-summary.json` also retains the
build-selected GoFiles census: 131061 target lines (140952 baseline), five fewer than the directory census on each
pin. The five lines are `processor/graph-index/race_enabled.go`, excluded by ordinary Go-list in favor of
`race_disabled.go`; the directory census includes both. Both methods are labeled explicitly in the summary.

| Census | Baseline | Target |
| --- | ---: | ---: |
| Selected modules, including SemBoids | 298 | 303 |
| Production packages, including standard library | 517 | 514 |
| Integration test package closure | 537 | 538 |
| External modules reached by production packages | 67 | 64 |
| SemStreams production packages | 60 | 62 |
| SemStreams reachable non-test directory lines | 140957 | 131066 |

`dependency-delta.json` records the exact changes. Six selected modules are added: Antithesis SDK, go-tpm,
HighwayHash, NATS JWT, NATS server and rapid. Google gofuzz is removed. The selected reflect2 version becomes 1.0.2;
json-iterator, modern-go/concurrent and reflect2 are no longer reached by production packages. Testcontainers/Docker
remain in the production package closure through SemStreams' ordinary `natsclient/test_client.go`; registration also
retains agentic/provider-related reachability. No SemEngine capability was admitted by this measurement.

`modules.tsv`, package TSVs and `module-graph.log` retain the dependency closure. Full Go-list JSON remains under
`/tmp/semboids-shared-pin/target-evidence` for this session. Package capture uses `-buildvcs=false` to avoid a sandbox
stat-cache write; this changes build metadata collection, not imports. `SHA256SUMS.json` identifies retained artifacts.
