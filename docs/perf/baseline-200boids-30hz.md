# Baseline CPU Profile — 200 boids @ 30Hz

Captured 2026-07-04 during `add-flock-core` verification (task 5.2), as the
comparison point for future load-dial profiling (graph-ingest cadence cranked
toward tick rate).

## Setup

- `./cmd/semboids/semboids --config configs/flock.json --debug` (pprof :6060)
- Local NATS (`task dev:nats:start`), one browser client on the vite dev UI
- `go tool pprof -top -seconds 10 http://localhost:6060/debug/pprof/profile`
- Host: Apple M3 Pro, darwin/arm64

## Headline

- **Total samples: 650ms over 10.15s — ~6.4% of one core** for the whole
  system: physics + frame marshal + NATS publish + websocket broadcast.
- Physics is nearly invisible: `flock.(*grid).neighbors` at 1.5% of samples
  (~0.1% of a core). Consistent with `BenchmarkTick200` ≈ 114µs/tick.
- Dominant costs are runtime scheduling/syscalls (ticker wakeups, publish
  syscalls): `pthread_cond_signal` 24.6%, `rawsyscalln` 21.5%.
- The largest in-process application cost is the **websocket-output JSON
  re-parse**: `output/websocket.handleNATSMessageData` unmarshals every frame
  to inject `subject`/`timestamp`, then re-marshals (`appendCompact`,
  `decodeState.skip`, `structEncoder.encode` in the profile). At 30 msg/s
  this is noise; at load-dial rates it is the first upstream candidate to
  watch — filed as semstreams#471 (pass-through mode that skips the
  envelope re-encode for pre-validated JSON).

## Top nodes (flat)

```
     160ms 24.62%  runtime.pthread_cond_signal
     140ms 21.54%  syscall.rawsyscalln
      50ms  7.69%  runtime.madvise
      40ms  6.15%  runtime.kevent
      30ms  4.62%  runtime.memclrNoHeapPointers
      30ms  4.62%  runtime.pthread_cond_wait
      20ms  3.08%  encoding/json.appendCompact
      20ms  3.08%  internal/strconv.fmtF
      10ms  1.54%  encoding/json.(*decodeState).skip
      10ms  1.54%  flock.(*grid).neighbors
```

## Reproduce

```bash
task dev:nats:start
go run ./cmd/semboids --config configs/flock.json --debug &
go tool pprof -top -seconds 10 http://localhost:6060/debug/pprof/profile
```
