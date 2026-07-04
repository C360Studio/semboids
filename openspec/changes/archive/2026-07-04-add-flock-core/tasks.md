# Tasks — add-flock-core

## 1. Repo scaffold

- [x] 1.1 `go mod init github.com/c360studio/semboids`; `go get
      github.com/c360studio/semstreams@v1.0.0-beta.133`; add revive via go.mod
      `tool` directive
- [x] 1.2 Copy/adapt `revive.toml` from semstreams (warnings = failure)
- [x] 1.3 Root `Taskfile.yml` + `taskfiles/{build,test,lint,clean,dev}.yml`;
      `task dev:nats:start` (`nats:2.12-alpine -js -m 8222`, container
      `semboids-nats`, 4222/8222); composite `task check` / `task check:push`
- [x] 1.4 `.github/workflows/ci.yml` (lint / test `-race` / build
      cross-compile `./cmd/semboids` / status-check; GO_VERSION 1.26) — defer
      schema-validation job until a schema exists
- [x] 1.5 `.env.example` with `SEMBOIDS_NATS_URLS=nats://localhost:4222`

## 2. flock-physics (`internal/flock`) — TDD

- [x] 2.1 Failing tests: Vec2 ops; toroidal wrap/distance; spatial-hash
      neighbor queries (incl. wrap-around edges); each Reynolds rule in
      isolation (separation repels, cohesion attracts, alignment converges
      headings); determinism (same seed+params ⇒ identical state after N
      ticks); clamps (max speed/force never exceeded)
- [x] 2.2 Implement `Params`, `Vec2`, spatial hash (reused buckets), engine
      with double-buffered tick
- [x] 2.3 Benchmark: `BenchmarkTick` at 200 and 500 boids — assert headroom
      (tick ≪ 33ms) and track allocations (steady-state ~0 allocs/tick goal)

## 3. Sim component + host

- [x] 3.1 Failing tests: frame payload marshals to the compact wire format;
      sim component publishes one frame per tick; ctx cancellation stops the
      loop cleanly (explicit synchronization, no sleeps)
- [x] 3.2 Implement frame payload + `sim` Input component (engine owned by
      component; ticker goroutine; core-NATS publish to `boids.frames`)
- [x] 3.3 `cmd/semboids/main.go` host: pprof-before-NATS, registry, flags
      (`--boids`, `--tick-hz`, `--seed`, `--debug`), metrics :9090
- [x] 3.4 `configs/flock.json` flow: `sim → boids.frames →
      output/websocket`; verify end-to-end against local NATS
      (`task dev:nats:start`, `websocat` smoke)

## 4. boid-ui (`ui/`)

- [x] 4.1 SvelteKit skeleton per conventions: Svelte 5 + Kit 2 + Vite + TS +
      Vitest, adapter-node (`out: 'build'`), `ui/Caddyfile`, package name
      `semboids-ui`; copy semteams `colors.css`
- [x] 4.2 Failing tests: frame parsing; store keeps latest frame only;
      reconnect/backoff state transitions
- [x] 4.3 `src/lib/stores/flock.svelte.ts` singleton WS store ($state latest
      frame, status)
- [x] 4.4 `FlockCanvas.svelte`: rAF loop, velocity-oriented triangles, DPR
      scaling; split-screen layout with graph-pane placeholder
- [x] 4.5 `.github/workflows/ui.yml` (paths `ui/**`, Node 22: eslint,
      svelte-check, vitest, build)

## 5. Verification

- [x] 5.1 `task check:push` green; run the demo (NATS + backend + ui) and
      confirm visible flocking at 200 boids / 30Hz with smooth rendering
- [x] 5.2 Capture a baseline pprof CPU profile of the running sim (evidence
      for later load-dial comparisons)
- [x] 5.3 `openspec validate add-flock-core --strict`; update root README
      status; archive the change
