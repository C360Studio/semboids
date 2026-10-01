# Shared-pin migration measurements

## Status and exact inputs

Eight matched post-readiness steady-state runs and separate process/transport probes are complete. Direct target
startup at graph 30Hz remains a qualification blocker. The steady-state runs do not establish a repeatable revision
speedup or slowdown: stable pair directions reverse, and churn demand differs with observed population. Physics
remained near 30FPS under graph overload. Neither the startup blocker nor the separately demonstrated delayed
snapshot resurrection is repaired or waived by these results.

- SemBoids baseline: `8c03cc53836ced93a5df7064473c63ff144e64f1`.
- Baseline SemStreams: `v1.0.0-beta.160`, `8403a2218000e45a31c5132fbfe01af42ed04f14`.
- Frozen target SemStreams: `v1.0.0-beta.162.0.20260930150212-8b99efe9c66a`,
  `8b99efe9c66a4faa4fa509f9f62cc6bad8392128`.
- NATS image: `nats@sha256:b270f5e2428354c0335612694d7dd2fb588148e567a5757fdff325ef9c9332e6`.
- Baseline binary SHA-256: `3167218148a14393aa46efea138c26f53faa05d0ab58bacc367b3d362b49b6e4`.
- Target binary SHA-256: `4153b224f0a204f2d5349ac0b167288d6ca1223b3082f6ac2368534b34e7f625`.
- Measured host: local macOS arm64, 12 logical CPUs, Go 1.26.4. Per-run manifests preserve platform and module metadata.

## Pre-upgrade stable overload diagnostic

Captured 2026-10-01, 06:28–06:29 America/Chicago, before upgrading `go.mod`: seed 1, 200 boids, physics 30Hz,
graph snapshots 30Hz, no zones, cull disabled, churn 0Hz; 20-second warmup and 45-second measurement.

| Signal | Observation |
|---|---:|
| Intended offered boid entities/s | 6,000 |
| Fully ACKed published boid entities/s | 5,999.48 |
| Committed entity writes/s | 2,376.31 |
| Consumer-completion inference | unavailable: finite retry limit |
| Outstanding work, pending plus ACK-pending | 71,867 → 234,923 |
| ACK-pending portion | 712 → 694 |
| Physics/NATS output FPS | 29.997 |
| Missing physics output ticks / snapshot offer drops | 0 / 0 |
| Maximum physics frame gap | 40.6ms |
| Sampled boid KV latency, p50 / p99 | 25.24s / 64.07s |
| SIGTERM shutdown | exit 0, 0.83s, no forced kill |

This is an overloaded queue, not a keep-up result. Consumer completion by stream conservation is withheld because the
finite delivery limit
can retire unprocessed work. Stream acceptance, authoritative backlog, and committed-write rates remain independent
observations; their numerical agreement is not a proof of successful processing. No WARN/ERROR log event occurred
inside the measured window; startup had
expected create collisions and revision conflicts, preserved in raw logs. Contract-rejection label families were
absent, so their values remain `null`; this is not proof of zero rejection.

A brief cached unit-test red run overlapped this diagnostic. The baseline agent disclosed the overlap after the
run; this sample is excluded from final paired performance attribution. It also had no WebSocket observer. All
final interleaved runs used the same harness with one ordinary WebSocket observer on both revisions.

## Baseline process qualification

Authoritative startup/restart run: `operations-baseline-final`. Port collision uses the separate corrected
`baseline-port-conflict` run. Earlier probes remain archived to show why checks were strengthened.
All use a separate isolated broker and 30 boids at 1Hz graph cadence; they are not performance comparisons.

| Probe | Observed result |
|---|---|
| Fresh startup | 30 boids, advancing frames and 30 graph entities |
| Application restart, retained broker | Same authority, 30 boids; entity-state sequence advanced 30 → 60 |
| Broker restart, retained storage | Graph stream sequence retained; frames resumed at tick 101 |
| Post-broker graph writes | Entity-state sequence 60 → 90 |
| Index consumer recovery | Separate post-reconnect progress 60 → 65, about 17s after frames resumed |
| Normal SIGTERM | exit 0, under 19ms in these small-population probes |
| SIGTERM during stalled initial NATS handshake | Process was alive when signaled; default-signal exit -15 in 1.3ms |
| Occupied API listen port | Logged bind error but process stayed alive for more than 25s |

The corrected dual-stack occupied-port case is a baseline startup defect. Earlier IPv4-only probes were invalid:
Go could bind IPv6, and their logs contained no bind error. Those assertions are withdrawn. The corrected probe
occupies both address families and requires an explicit address-in-use error. The cancellation case proves bounded
process termination but not graceful startup cleanup.
Broker recovery assertions require both graph writes and successive index progress; a single buffered index count
was deliberately rejected as insufficient evidence.

## Baseline slow-client probe

Separate run `baseline-slow-websocket`: same initial population/seed, graph 1Hz, two WebSocket connections (one
normal observer, one deliberately non-reading socket), two-second warmup and twenty-second observation.

- Physics output remained 29.994FPS with zero missing ticks and maximum gap 42.1ms.
- Graph offered/committed about 200 entities/s with zero outstanding work at both endpoints.
- The slow client generated a `client_timeout` error counter with value 1 and was removed.
- The ordinary client experienced a maximum five-second delivery gap and 9.772-second frame age.
- No tick was missing at the ordinary observer over this short run: buffered frames arrived in bursts. Its observed
  31.6 deliveries/s includes catch-up and must not be interpreted as faster physics.

This proves physics isolation from this slow client while exposing an existing normal-client latency limitation.
It does not prove universal transport loss behavior or a drop count. See `metrics.md` for exact counter semantics.

## Target operational behavior and attribution

Target binary `4153b224f0a204f2d5349ac0b167288d6ca1223b3082f6ac2368534b34e7f625` passed the strengthened
`operations-target-qualified` probe: 30 boids on startup and retained application restart, retained broker state,
new graph commits after broker restart (sequence 60 → 90), and continuing index updates. Normal SIGTERM completed
with exit 0 in 71–123ms. These probes ran alongside ordinary gates, so their durations are observations, not speed
comparisons.

The corrected `target-port-conflict` probe occupied both IPv4 and IPv6 listeners. Target startup exited 1 in 175ms
with an explicit bind error; baseline logged that same error but remained alive beyond 25 seconds. This improvement
matches the target native service manager's synchronous `net.Listen` error return, replacing the baseline's
asynchronous server failure. No SemBoids bind workaround was added.

During a deliberately stalled initial NATS handshake, the target observed SIGTERM and exited 1 after the native
five-second dial timeout, versus baseline default-signal termination. This matches moving signal capture ahead of
NATS connection in the host adapter. It is bounded cancellation, not instantaneous dial interruption.

The target slow-client probe matched the baseline limitation: physics 29.991FPS with no missing ticks, graph about
200 entities/s with no endpoint backlog, normal-client maximum gap 5.002s and maximum delivery age 9.771s. Both pins'
WebSocket broadcaster waits on each client send and its five-second timeout; this explains normal-client delay
while the independent physics output continues.

## Aborted load campaign and outstanding startup question

The first stable baseline trial completed: 6,001.65 accepted entities/s, 1,862.47 committed writes/s, outstanding
work 79,884 → 266,218 and physics 30.008FPS. The following target trial failed before measurement: early simulator
neighbor-clear and lifecycle requests had no responders, the shared NATS circuit breaker opened, then graph-index
startup failed creating `GRAPH_STATUS` with `not connected to NATS`. Broker logs remained healthy. This is a startup
ordering finding, not measured low throughput. The selected campaign below uses an explicitly separate, matched
post-readiness activation design. It does not
qualify or replace the failing cold-start workload.

That first baseline trial also exposed observer coverage: the receive-only WebSocket closed with code 1006 at
60 seconds. Its original summary reported zero internal tick gaps but missed the final 149 expected NATS ticks.
The corrected cross-check exposes the missing tail. That trial is excluded from final pairs. The observer now
reconnects after 500ms, matching the UI, and preserves timestamped close/reconnect events and whole-window delivery
deficits. A delivery deficit can include boundary/delay effects and is not automatically permanent packet loss.
Both pins set a 60-second `ReadMessage` deadline; their Pong handler updates `lastPing` without extending that
deadline. All eight selected runs reproduced the close and reconnect at both pins, supporting the shared source
attribution. Each had a 15-tick delivery deficit against the whole NATS measurement window.

The earlier preliminary boot failure on undeclared `rule-processor/zone_events` topology was corrected by declaring
the simulator's actual ports. That preliminary binary was never used for a timed target measurement.

## Selected interleaved steady-state campaign

All eight selected runs are in the `steady-state/` namespace. Actual order was A1, B1, A2, B2 for stable, then
A1, B1, A2, B2 for churn; A is beta.160, B is the frozen shared pin. Each used a fresh isolated broker with the
same immutable image, seed 1, initial 200 boids, physics 30Hz, 20-second warmup and 45-second measurement.
Stable used graph 30Hz, no zones and churn 0Hz. Churn used graph 1Hz, the same three configured zones, cull enabled
and churn 1Hz with five boids per wave. No competing local gates/builds ran during the selected campaign.

Both revisions started at graph 0Hz with churn/cull disabled. Admission required all six components started and
healthy, 200 completed seed creates, zero culls, complete graph-ingest/index bootstrap, readiness 1, lag 0, zero
snapshot publications and zero authoritative outstanding work. Every readiness poll is preserved. Only then did
public PUT activate graph cadence with HTTP 200 and matching reply, followed by the cull/churn controls and warmup.
This fresh-instance observed barrier is not a general mutation-readiness contract. Exact activation timestamps are
in `steady-state-comparison.json` and each `load-activation.json`.

Offered rates below are inferred from actual population at snapshot-eligible physics ticks, not from the initial
population or ACK counter. NATS stream acceptance and successful committed entity writes are independent signals;
commits include lifecycle operations. Consumer-completion inference is unavailable in every run because the consumer
has a finite delivery limit. Endpoint backlog includes both unread and ACK-pending work. Receipt/scrape boundary
alignment can shift one snapshot; no missing NATS ticks invalidated offer inference.

| Run | Offered/s | Accepted/s | Committed writes/s | Outstanding start → end |
|---|---:|---:|---:|---:|
| baseline-stable-1 | 5999.27 | 5984.71 | 1386.16 | 76,959 → 283,926 |
| target-stable-1 | 6006.02 | 5990.52 | 988.82 | 97,635 → 322,989 |
| baseline-stable-2 | 6001.60 | 5997.15 | 2123.39 | 85,747 → 260,149 |
| target-stable-2 | 6003.52 | 6002.90 | 2303.85 | 71,579 → 238,062 |
| baseline-churn-1 | 409.97 | 409.97 | 414.97 | 0 → 0 |
| target-churn-1 | 409.77 | 409.77 | 414.79 | 0 → 0 |
| baseline-churn-2 | 409.96 | 409.96 | 414.54 | 0 → 13 |
| target-churn-2 | 408.78 | 408.78 | 412.95 | 0 → 36 |

| Run | Physics FPS | Max gap ms | Snapshot drops | KV latency p50 / p99 seconds |
|---|---:|---:|---:|---:|
| baseline-stable-1 | 29.996 | 124.2 | 4 | 23.131 / 62.191 |
| target-stable-1 | 30.030 | 92.0 | 3 | 38.532 / 64.996 |
| baseline-stable-2 | 30.008 | 49.1 | 0 | 29.032 / 64.654 |
| target-stable-2 | 30.018 | 40.5 | 0 | 25.765 / 64.215 |
| baseline-churn-1 | 29.998 | 52.5 | 0 | 0.111 / 0.430 |
| target-churn-1 | 29.995 | 48.1 | 0 | 0.117 / 0.401 |
| baseline-churn-2 | 29.997 | 43.8 | 0 | 0.114 / 0.473 |
| target-churn-2 | 29.997 | 47.0 | 0 | 0.117 / 0.445 |

All eight had zero missing NATS frame ticks, zero neighbor-clear failure and reclaim-exhaustion deltas, and normal
SIGTERM exit 0 without forced kills. This is not proof that deletion races cannot occur; delayed stale snapshots
fail the separate correctness qualifier. Sparse rejection/error families remain absent (`null`), not proven zero.

Stable runs overload the graph at about 6,000 offered entities/s. The target/baseline committed-write ratios were
0.713 in pair 1 and 1.085 in pair 2. The reversed direction and large within-pin variance do not support a revision
speedup or slowdown. Both pins' backlog grew sharply and sampled KV latency reached tens of seconds while physics
kept advancing. Cause of throughput variance is unresolved; no CPU profile was collected or causal bottleneck claim
made. Host one-minute load averages (start → end) were A1 3.82 → 14.32, B1 18.06 → 22.12, A2 22.12 → 15.28,
B2 15.28 → 11.32. Load average is not CPU utilization and retains preceding-run history. Full 1/5/15-minute values
and five-second samples remain in raw evidence.

The four stable runs have identical serialized boid-state SHA-256 values at the six common sampled ticks
300, 600, 900, 1200, 1500 and 1800. This supports seeded stable-physics preservation at those checkpoints;
it is not an all-frame identity claim. Churn state hashes are not compared as deterministic physics: zone steering,
rule delivery and lifecycle application are asynchronous, including during variable readiness waits.

| Churn run | Measured population | Successful spawns | Observed culls | Rule cap skip WARNs |
|---|---:|---:|---:|---:|
| baseline-churn-1 | 295 → 520 | 225 | 0 | 4 |
| target-churn-1 | 300 → 519 | 225 | 1 | 17 |
| baseline-churn-2 | 295 → 520 | 225 | 0 | 8 |
| target-churn-2 | 300 → 514 | 225 | 6 | 24 |

Churn was mostly population growth, with sparse target culls; it is not a matched sustained-reclamation stress test.
The cull metric observes phase changes, not successful fenced deletion. One pin's first measured frame can already
include a wave at the scrape boundary, explaining why a table starts at 295 versus 300 despite the same admitted
200-seed population and warmup. The full tick/population traces determine offered work. Both target repetitions
accepted slightly fewer entities as population diverged; raw committed-rate differences must not be called slower
processing. Why the sparse cull counts differ is unresolved by this campaign. Separate focused tests cover cull
and reclamation contracts, including their blocking stale-snapshot failure.

All churn runs recorded `Action firing cap reached, skipping` for `predator-flee` publish actions, fired 3/cap 3,
source `framework_default`. Both pinned `processor/rule/stateful_evaluator.go` implementations enforce that same
cap (baseline line 409, target line 410). These are skipped rule actions, separate from snapshot or frame drops;
the differing counts are not evidence of changed cap semantics. No other WARN/ERROR class occurred during the
selected measurement windows. Stable measurement windows had no WARN/ERROR events.

Each ordinary WebSocket observer closed with code 1006 around connection age 60 seconds and reconnected after
0.501–0.505 seconds. Each run missed 15 expected NATS ticks in the measurement window and had a 0.530–0.539-second
maximum WebSocket gap. This repeated at both revisions, matching the unchanged read-deadline/Pong behavior described
above. The observer exposes the gap instead of silently stopping at 60 seconds. Ordinary maximum frame age was
14–173ms; the separate slow-client probes expose the much larger shared 5-second gap and about 9.77-second age.

## Evidence and reproduction

`qualification-evidence.tar.gz` contains all diagnostic and selected run configs, binary manifests, every readiness
poll, raw metrics, NATS stream/consumer snapshots, complete frame timing/population traces, WebSocket events and
process/broker logs. `qualification-evidence-index.json` records archive hash, size, included run IDs and harness
hashes. `summaries/` contains readable manifest/summary/cross-check/operation/shutdown copies.
`steady-state-comparison.json` preserves exact selected values and descriptive pair arithmetic; `exclusions.json`
names every excluded cohort and withdrawn assertion. Original root-level `baseline-stable-1` and failed
`target-stable-1` are diagnostics; only `steady-state/...` runs enter the final comparison. The older
`baseline-evidence.tar.gz` and `evidence-index.json` preserve the pre-upgrade package separately.

The baseline source archive must be checked out at the exact baseline SemBoids SHA. The target source contains the
reviewed compatibility changes and frozen shared module pin. Rebuild with `go build -o <binary> ./cmd/semboids`,
record its new SHA-256 and `go version -m`, then copy the source configs/rules beside each immutable binary. A rebuilt
binary can differ due to VCS stamping; never silently replace one mid-campaign. The measured immutable hashes are
listed above and in every run manifest. Node 22 and the already pulled immutable NATS image are required.

```bash
python3 -m unittest discover -s scripts/qualification -p 'test_*.py' -v
python3 scripts/qualification/campaign.py \
  --baseline-binary /tmp/semboids-shared-pin/baseline-semboids \
  --baseline-source /tmp/semboids-shared-pin/baseline \
  --target-binary /tmp/semboids-shared-pin/target-qualified-semboids \
  --target-source /tmp/semboids-shared-pin/target-qualified-source \
  --out /tmp/semboids-shared-pin/performance/steady-state
python3 scripts/qualification/aggregate.py \
  /tmp/semboids-shared-pin/performance/steady-state \
  --out docs/migration/shared-pin/performance/steady-state-comparison.json
python3 scripts/qualification/pack.py \
  --runs /tmp/semboids-shared-pin/performance \
  --destination docs/migration/shared-pin/performance
```

Every output directory must be new; repeat campaigns with a new path. Reserve an exclusive measurement window.
The runner actively polls authoritative NATS state every five seconds and stops on failure. It independently cleans
up observers, app and broker even if another cleanup step fails. No owned qualification containers remained after
this campaign. The eleven harness tests include injected cleanup failure, sparse-series handling, ACK-pending
accounting, conservative conservation prerequisites, changing-population offers, observer reconnect and readiness.

Separate process/transport commands, excluded from throughput ratios:

```bash
python3 scripts/qualification/operations.py \
  --binary /tmp/semboids-shared-pin/target-qualified-semboids \
  --source /tmp/semboids-shared-pin/target-qualified-source \
  --label target-qualified \
  --out /tmp/semboids-shared-pin/performance/operations-target-qualified
python3 scripts/qualification/load.py \
  --binary /tmp/semboids-shared-pin/target-qualified-semboids \
  --source /tmp/semboids-shared-pin/target-qualified-source \
  --label target-slow-websocket \
  --out /tmp/semboids-shared-pin/performance/target-slow-websocket \
  --profile stable --hz 1 --warmup 2 --window 20 --slow-client
```
