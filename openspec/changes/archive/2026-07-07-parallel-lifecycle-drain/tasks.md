# Tasks — parallel-lifecycle-drain

> App-side throughput: parallelize the off-loop lifecycle Create/delete drain
> across graph-ingest's keyed-concurrent lanes (beta.142 ADR-072). Physics hot
> path untouched (ADR-001). Structural fixes stay upstream (#497 despawn, #498
> batch); this consumes existing lanes, waits on neither.

## 1. Bounded worker pool (`internal/sim`) — TDD

- [x] 1.1 Failing tests for a `drainPool` (semaphore + `sync.WaitGroup`):
      `submit` never runs more than N closures concurrently (assert observed
      max in-flight ≤ N under a flood); `submit` blocks (backpressure) when N
      are in flight rather than spawning unbounded goroutines; `wait` joins all
      in-flight; a cancelled ctx makes `submit` stop accepting. Use explicit
      synchronization (channels/atomics), no sleeps.
- [x] 1.2 Implement `drainPool` (`newDrainPool(n)`, `submit(ctx, func())`,
      `wait()`); n clamps to ≥ 1.

## 2. Wire create + cull drains through the pool (`internal/sim`) — TDD

- [x] 2.1 Failing tests: `createPending` submits each drained ID to the pool
      (concurrent creates, ≤ N in flight) and still calls `observeSpawn` per
      boid; the cull watcher offloads each `deleteEntity`+`observeCull` to the
      pool while `stageRemoval` stays synchronous in the watch goroutine (a
      slow/blocked delete does not stall observing the next cull). Assert
      same-boid create-before-delete causality is preserved (delete only
      submitted after the culled phase is observed).
- [x] 2.2 Add `lifecycle_drain_concurrency` to the sim `Config` (+ ConfigSchema,
      default 8, clamp ≥ 1); construct one `drainPool` on the `Component`;
      route `createPending` and `runCullWatcher`/`deleteEntity` through
      `pool.submit`; `pool.wait()` in `Stop` (bounded by the Stop timeout).
      Promote `golang.org/x/sync` to a direct dep if used (else drop it).

## 3. Verify + perf characterization

- [x] 3.1 `task check:push` green (lint, `-race` unit + integration — the
      existing `cull_integration_test.go` full chain still passes under the
      concurrent drain).
- [x] 3.2 Re-run the churn campaign at `lifecycle_drain_concurrency ∈ {1, 8}`
      (and one higher value): record the create/cull ceiling and the
      decline-with-N curve for each, confirm physics holds 30fps, and append
      the A/B result to `docs/perf/churn-lifecycle-2026-07-06.md` (or a
      follow-up note). Quantify the speed-up vs the serial baseline.
- [x] 3.3 `openspec validate parallel-lifecycle-drain --strict`; refresh the
      README churn line + upstream ledger if the numbers move materially;
      archive the change.
