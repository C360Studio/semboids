# SemBoids shared-pin migration evidence

Status: ordinary target gates pass, including UTC and default-parallel real-NATS race tests. Migration admission
is **HOLD**: a delayed pre-cull snapshot resurrects a reclaimed boid on both pins, and the target's first 30Hz
cold start failed while graph providers were starting. All eight repeated steady-state runs are complete.
The target fixes the reproduced SemBoids shutdown race. This migration remains on SemStreams; no SemEngine
cutover or admission is claimed.

[Draft PR #12](https://github.com/C360Studio/semboids/pull/12) carries the implementation and explicit qualification hold.

## Exact pins and architecture contract

[Machine-readable pins](pins.json) preserve both full upstream SHAs and SETUP 03A's selecting evidence.

| Point | Version | Full commit |
| --- | --- | --- |
| SemBoids baseline | Existing source | `8c03cc53836ced93a5df7064473c63ff144e64f1` |
| SemStreams baseline | `v1.0.0-beta.160` | `8403a2218000e45a31c5132fbfe01af42ed04f14` |
| Shared target | `v1.0.0-beta.162.0.20260930150212-8b99efe9c66a` | `8b99efe9c66a4faa4fa509f9f62cc6bad8392128` |

SETUP 03A selected its main commit while beta.163 was absent. A later release does not move the shared baseline.
The [proposal][proposal] and [architecture contract][design] preserve seeded physics, batched snapshots,
neighbor replacement/empty clearing, lifecycle facts, live zone rules, spawn/cull and fenced reclamation,
clustering, and browser transport. Architecture sign-off preceded failing-first adaptation. Final
[independent review](reviews/independent-review.md) approves the bounded compatibility and evidence work;
full workload qualification remains on HOLD.

[proposal]: ../../../openspec/changes/migrate-semstreams-shared-pin/proposal.md
[design]: ../../../openspec/changes/migrate-semstreams-shared-pin/design.md

## Captured baseline

[Baseline README](baseline/README.md) provides environment, exact commands, outcomes and reproduction.
[Gate results](baseline/gate-status.json) and [integration outcome](baseline/integration-status.json)
retain exit codes and durations. Go is `go1.26.4 darwin/arm64`; native race tests use CGO.

| Gate | Baseline outcome |
| --- | --- |
| Native build and Linux/amd64 cross-compile | Pass |
| Formatting, revive, vet and integration-tagged vet | Pass |
| Uncached race unit suite | Pass |
| Uncached real-NATS race integration suite | Fail: `TestPredatorCullReclaimsBoid` cleanup race |
| UI eslint, svelte-check, Vitest, production build | Pass; observed tool output, not retained stdout |
| Browser flock and graph panes, spawn and rule toggles | Pass with backend caveats below |
| Schema generation | Existing CI defers this because no schema exists |
| Retained process/broker restart and bounded cancellation | Pass for measured workload; see performance evidence |
| Target ordinary gates | Pass after UTC correction: 203 unit / 221 integration test and subtest passes |
| Interleaved load comparisons | Eight isolated A/B/A/B stable and churn runs complete; see [results](performance/results.md) |

The [race trace](baseline/race-integration.log) shows the cull watcher submitting work while Stop waits on
its drain-pool WaitGroup. This is an application defect reproduced before upgrading. The migration must
join submitters before pool waiting, preserve caller-owned Stop contexts, and report timeout rather than
silently claiming a complete join. The same test logs missing display-name validation errors in its minimal
rule fixture; a fixture correction must be attributed separately from substrate behavior.

## Browser and UI baseline

[The screenshot](browser/baseline.jpg) shows both panes, colored graph communities, 225 boids, tick 1385 and
the Predator toggle disabled. Browser verification began at 200 boids; one Spawn click increased population
to 225. Predator was toggled off and back on without process restart. A subsequent `/boids/rules` read
reported the enabled states. The browser session observed no console warnings/errors. These are observed
manual browser checks, not a standalone automated replay assertion.

[Backend logs](browser/baseline.log) independently preserve actual rule configuration/toggle messages and
shutdown: signal at `06:25:57.368` America/Chicago, completion at `06:26:01.412`, observed exit code zero.
They also contain graph-index staleness of approximately 7–16 seconds, rule action-firing-cap warnings,
and a canceled index submission at shutdown. A clean browser console does not mean a clean backend log
or prove stronger graph freshness. [UI dev-server output](browser/ui.log) contains a favicon 404.
[Artifact hashes](browser/SHA256SUMS.json) identify the preserved image and logs.

The coordinator observed the following commands pass on unchanged baseline UI source with Node `v22.20.0`:

```sh
cd ui
npm run lint
npm run check
npm test
npm run build
```

Svelte check reported zero errors and warnings; Vitest reported 24 tests passing. Raw stdout was delivered
through the tool session and was not saved to an artifact, so the evidence is the recorded observation.
Target browser verification also passed on the frozen production binary; no UI source changed. The
[observations](browser/target-observations.json), [screenshot](browser/target-final.jpg) and retained-restart
log record live toggles, spawn 195→220, dial 1→5→1, automatic reconnect, retained platform identity, and
clean process exit. Population was allowed to change through culling; this is not a graph/physics census proof.

## Dependency closure

The baseline [module graph](baseline/module-graph.txt), [selected modules](baseline/modules.tsv),
[production packages](baseline/production-packages.tsv), and
[integration-test packages](baseline/integration-test-packages.tsv) retain measured package reachability.
The [size ledger](baseline/closure-size.json) adds raw non-test Go lines by package directory.

| SemStreams scope | Packages | Non-test Go lines |
| --- | ---: | ---: |
| Direct production imports | 19 | 83,790 |
| Production closure from `cmd/semboids` | 60 | 140,957 |
| Integration-tagged test closure | 60 | 140,957 |
| Additional SemStreams packages only in test closure | 0 | 0 |

Total captured package entries, including standard library and test variants, are 517 production and 537
integration-test entries. Both closures reach 67 external modules. This is package reachability, not linked
symbols or executable size. Raw directory lines include all non-test `.go` source in selected package
directories, including files for other platforms/build tags. `natsclient/test_client.go` is ordinary package
source and brings testcontainers/Docker into production closure. Registry/service imports also retain
agentic and gated-DAG packages; direct imports alone are not an extraction boundary.

Reproduce the size ledger from captured TSVs and the immutable baseline source/module:

```sh
python3 docs/migration/shared-pin/measure_closure.py \
  --module-dir "$(go env GOMODCACHE)/github.com/c360studio/semstreams@v1.0.0-beta.160" \
  --consumer-dir /tmp/semboids-shared-pin/baseline \
  --evidence-dir docs/migration/shared-pin/baseline
```

The same census was repeated against the target checkout and TSVs. The [target ledger](target/README.md)
records compatibility changes and closure: 62 SemStreams packages,
131,066 directory-source lines, 514 production packages and 64 reached external modules. Selected modules
change from 298 to 303. The frozen baseline ledger is preserved.

## Measurement and attribution obligations

Performance evidence is maintained separately under `performance/`. Repeated interleaved runs must hold
seed, population, cadence, churn, warmup, duration, limits and immutable broker image constant and own
isolated stores. Every result must distinguish offered load, ACKed publication, applied graph writes,
pending/ACK-pending backlog, latency, drops and physics responsiveness. Missing observations are unknown.

Baseline `boids_graph_snapshots_published_total` increments even after a failed publish batch; it counts
coordinator snapshot attempts. `boids_graph_entities_published_total` advances only for wholly ACKed
batches and can omit partial successes. Neither proves applied graph work. Record actual increment sites,
HELP/TYPE, labels and raw samples before comparing counters across pins. See [design D6][design].
Committed entity-write counts include lifecycle and canonical writes as well as snapshots. Consumer-completion
inference is withheld because finite delivery limits can remove unsettled work without proving successful application.

The [eight-run evidence](performance/results.md) preserves every raw scrape and authoritative backlog sample.
At 200 boids and 30Hz snapshots, both pins overload the graph: committed entity writes range from 989 to
2,304/s against approximately 6,000 offered/s. Pair direction reverses, so no repeatable revision speedup or
regression is established; the cause of host/run variance remains unresolved. Physics remains near 30Hz,
without missing observed NATS ticks, and all six common stable physics state hashes match across the four runs.

At 1Hz snapshots with five new boids per second, both pins process approximately the offered population-driven
load. Actual culls are 0/1/0/6 in A/B/A/B order; differing event phase/population prevents interpreting raw write-rate
differences as throughput gains. Enabling culling is not proof that reclamation completed. Every run records a
60-second WebSocket disconnect and 15 missing observed ticks during reconnect; both pins share the deadline bug.
All eight processes stop with exit zero and no forced kill. These results do not close either correctness hold.

## Blockers and future SemEngine admission

The target ordinary race suite now proves lifecycle phase preservation under snapshots and replacement/empty
clearing of surviving-source relationships, including eventual index convergence. Reclamation qualification
remains blocked: both immutable-pin runs delete revision 1, then a held pre-cull snapshot recreates revision 3.
The separately invoked qualification assertion stays failing; ordinary CI does not imply this stronger guarantee.
See [target evidence](target/README.md) and the existing [upstream applied-input barrier request][barrier].
The app publisher and cull drain have no supported applied-input handoff; no private recovery subsystem was added.
Independent review approves the final compatibility implementation and measurement evidence but keeps admission
on HOLD. The first
30Hz target load trial then exposed a cold-start ordering failure: early simulator requests opened the shared
NATS circuit breaker before graph services finished starting. No throughput window was collected from that trial.
The initial [hosted CI attempt](ci/README.md) also exposed a UTC-dependent equality assertion in a new test.
That test-only correction passed the complete ordinary gates under UTC; production code and the frozen browser/load
binary are unchanged. [Measurement evidence](performance/results.md) preserves the aborted trial and the
observer's subsequently corrected tail-coverage gap. Query readiness cannot prove mutation-handler readiness;
no unsupported local startup barrier was added. Both pins' steady-state comparisons begin at zero snapshot load,
prove startup, then activate the same public dial before the same warmup. This cannot qualify cold startup at load.
The reviewed missing startup contract is filed as [SemStreams #1447][startup].

Operational and interleaved performance evidence is retained with its qualification limits. The initial
IPv4-only occupied-port probe was invalid on this Mac. The corrected dual-family probe
requires an explicit address-in-use error: baseline remains running beyond 25 seconds, while target
exits with status 1 in approximately 175 ms. Target startup cancellation is handled but waits for the
native NATS dial timeout (approximately five seconds). The raw invalid attempts remain distinguishable.

[barrier]: https://github.com/C360Studio/semstreams/issues/1444
[startup]: https://github.com/C360Studio/semstreams/issues/1447

The [future admission matrix][design] requires explicit graph mutation/ingest, relationships, rule activation,
lifecycle, clustering, transport, host and observation contracts in SemEngine, with owners, source provenance,
closure, side effects and proving workloads. SemSource qualification cannot silently admit SemBoids' extra
capabilities. Simulation semantics remain in SemBoids and this migration remains on SemStreams until that
separate consumer contract and workload qualify.
