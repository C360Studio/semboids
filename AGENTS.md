# SemBoids Project Context

## Project Overview

SemBoids is a classic Reynolds boids simulator — a fun demo for the C360 `sem*`
family celebrating simple-yet-detailed over complex: three steering rules
(separation, cohesion, alignment) producing emergent flocking. It doubles as a
calibrated load generator for the SemStreams substrate: the graph-ingest rate
is a dial we crank until something melts, profile with pprof, and file upstream.

## Tech Stack

- Go 1.26.x backend, `github.com/c360studio/semstreams` pinned by tag
- NATS JetStream (KV + streams) via SemStreams runtime
- SvelteKit 2 + Svelte 5 runes UI in `ui/` (adapter-node behind Caddy)
- Canvas 2D for the boid spatial pane; sigma.js v3 + graphology for the graph pane
- Taskfile runner, revive lint, Prometheus metrics, pprof

## Architecture (CRITICAL — the hybrid split)

**Per-tick physics state never routes through NATS, rules, lifecycle, or
graph-ingest.** SemStreams ADR-040 retired boids-through-rules; the graph write
budget (10–30ms/entity) is 30–450× too slow for per-tick updates, and rule
KV-watch coalescing drops intermediate updates by design. Decided 2026-07-04.

```
┌─ semboids process ─────────────────────────┐
│  physics loop (30Hz ticker, spatial hash)  │
│    │ positions        ▲ steering modifiers │
│    ▼                  │                    │
│  ┌─ semstreams components ──────────────┐  │
│  │ ws-output ──► browser canvas         │  │
│  │ graph-ingest ◄─ snapshot @ DIAL Hz   │  │
│  │ rule proc ◄─ zone transitions        │  │
│  │ lifecycle ◄─ spawn/despawn           │  │
│  │ graph-clustering ─► flock communities│  │
│  └──────────────────────────────────────┘  │
│  pprof :6060   metrics :9090   api :8080   │
└────────────────────────────────────────────┘
```

- **Physics**: plain Go loop (`internal/flock`), `time.NewTicker`, spatial hash
  neighbors. Default 200 boids @ 30Hz.
- **Rules drive behavior, not physics**: zones (predator/food/wind) are graph
  entities; rules fire on zone *transitions* and emit steering modifiers the
  physics loop applies. Toggling a rule visibly changes the flock.
- **Lifecycle**: boid/wave spawn and despawn only — never position updates.
- **Graph snapshots at a dial**: boid entities + neighbor triples through
  `graph-ingest` at tunable cadence; flock membership = `graph-clustering`
  (LPA) communities recolored live in the graph pane.
- **Egress**: SemStreams `output/websocket`, at-most-once — slow clients drop
  frames, never backpressure physics.

## Spec-driven development (OpenSpec)

| Home | Holds |
|---|---|
| `openspec/specs/<capability>/spec.md` | Current truth (Requirement + GIVEN/WHEN/THEN), seeded lazily |
| `openspec/changes/<id>/` | Proposals: `proposal.md` + `tasks.md` + spec deltas; archived on completion |
| `docs/adr/` | Genuine decisions only (irreversible choices, cross-repo contracts) |
| `docs/` (other) | Tutorial/runbook content |

Non-trivial changes go through OpenSpec before code (`task spec:check`,
`task spec:queue`). Read `openspec/project.md` and `.agents/protocol.md` first
when scoping anything.

## Common Tasks

```bash
task tools:ateam      # install pinned ateam (v0.4.1) into <main>/.tools/bin/ateam
task dev:nats:start   # nats:2.12-alpine -js on 4222/8222 (container semboids-nats)
task check            # fast Go lint + unit tests
task verify           # full read-only CI mirror (Go, UI, Governance; alias: task validate)
task merge:check -- N # local Merge check gate for PR N
task dev              # backend + vite dev (ui on :5173)
```

Hooks and MCP registrations call `scripts/ateam`, which runs the main checkout's
pinned `.tools/bin/ateam` (shared by every worktree) and falls back to `PATH`.

## Semstreams Relationship (CRITICAL)

SemBoids imports SemStreams as a library — never reimplement substrate. If a
rule-engine, lifecycle, or graph primitive is missing, file a SemStreams issue;
don't carve an app-side parallel path.

| Need | Use (from semstreams) |
|---|---|
| NATS connection | `natsclient` |
| Component wiring | `component.Registry`, `componentregistry.Register` |
| Graph entities | `graph` (Graphable payloads → `ENTITY_STATES` via graph-ingest) |
| Typed graph mutations | `pkg/projection` (`MutationClient`: reconcile/fenced delete; contracts declare intent) |
| Rules | `processor/rule` (JSON rules, zone transitions) |
| Spawn/despawn | `pkg/lifecycle` (Participant + Manager) |
| Browser egress | `output/websocket` |
| Profiling | `service.MaybeStartPProf` (:6060 when debug) |
| Metrics | `metric` (Prometheus, :9090) |
| Errors/retry | `pkg/errs`, `pkg/retry` |

Graph writes go through the typed `pkg/projection.MutationClient` (never raw
`graph.mutation.*` subjects — beta.160 closed the wire) — `graph-ingest` is the
sole writer to `ENTITY_STATES`. Bulk snapshots stay on the ENTITY stream lane.
Rules pass references, never bulky payloads.

## 1. Ponytail decision ladder and code discipline

| Gate | Rule | Enforcement & Test |
| --- | --- | --- |
| `verify+review` | Follow the 6-step Ponytail decision ladder; no unrequested abstractions, wrappers, or dependencies, and keep per-tick physics state off NATS, rules, lifecycle, and graph-ingest | `scripts/ateam hook`; review |
| `verify+review` | Every intentional deferral uses `// shortcut: <the limit>, <when to upgrade>`, scanned by `ateam state debt` | `scripts/ateam state debt` |
| `verify+review` | Non-trivial logic ships with a test using an independent oracle and proven able to fail via non-destructive mutation check (`task mutate:check` or `cp` + SHA-256; never `git checkout`/`restore`/`stash`/`reset`), reporting `detection`, `survivor`, `invalid`, or `inconclusive` | `scripts/ateam mutate`; review |
| `verify+review` | Changes with interacting inputs, stated laws (round-trip, idempotence), or order-dependent histories record whether generated checks (`testing.F` / PBT) are used or why examples suffice, prove boundary reach and assertion activation, and check histories against a reference model | `task test`; review |
| `verify+review` | Every new or changed Go Test/Fuzz/executable Example and Vitest test carries attached spec references (`// spec:` or `//aip:verifies`) resolved by `task test:spec`; syntactic attribution does not prove semantic coverage | `scripts/ateam test-spec --base`; `.agents/protocol.md` |
| `verify` | Go packages pass `gofmt`, `go vet`, and `revive` with zero warnings, and UI passes `eslint`, `svelte-check`, `vitest`, and `vite build` | `Taskfile.yml`; `scripts/validate.sh` |
| `review` | Task conclusions and handoffs end with 1–2 lines stating what was skipped/not checked and any residual risk; acknowledgements and progress updates need no footer but still name blockers, failed or unavailable checks, and changed risks | review |

## 2. Hooks, MCP budget, and multi-agent parity

| Gate | Rule | Enforcement & Test |
| --- | --- | --- |
| `verify` | `AGENTS.md` stays under 24,000 bytes, `CLAUDE.md` only imports `@AGENTS.md`, and rule row counts match `.agents/rules-index.md` | `task ateam:doctor` |
| `verify` | Canonical skills in `.agents/skills/*/SKILL.md` have matching thin adapters in `.claude/skills/` and `.gemini/commands/`, and specialist role contracts in `.agents/contracts/` have matching `.claude/agents/` and `.codex/agents/` adapters | `task ateam:doctor`; `TestAgentSetupParity` |
| `verify` | Hook configs in `.claude/settings.json`, `.codex/hooks.json`, `.gemini/settings.json`, and `.agents/hooks.json` map native events to `scripts/ateam hook` at the pinned `ateam_version` (`v0.4.1`) in `.agents/ateam.json` | `task ateam:doctor`; `TestAteamShimResolution` |
| `verify` | `ateam mcp` exposes only the 2 multiplexed tools (`ateam_state`, `ateam_task`) with `<300` total schema tokens | `task ateam:doctor` |
| `verify` | `ateam hook` executes normalized lifecycle events in `<10ms` target budget (`doctor` warns `>10ms`, fails `--check` `>100ms`) with zero external runtime dependencies | `task ateam:doctor` |

## 3. State, OpenSpec, Forge (GitHub/Gitea), and Definition of Done

| Gate | Rule | Enforcement & Test |
| --- | --- | --- |
| `review` | Clarify Deliverable, Scope/non-goals, Definition of Done, and Stop boundary before mutating state | `.agents/protocol.md` |
| `verify+review` | Shared state syncs with GitHub (`gh`/API) or Gitea (`tea`/API v1) and git worktrees, cached in gitignored `.ateam/state.json` | `scripts/ateam state pickup` |
| `verify` | OpenSpec changes/specs validate via `task spec:check` (`openspec validate --all --strict` + `ateam state spec-check`), and `Hold:` / caveats (`HALT`, `RED`, `BLOCKED`, `WONTDO`, `OPEN-Q`) are parsed by `task spec:queue` | `task spec:check` |
| `review` | Non-trivial work's OpenSpec `proposal.md` stays one page (≤ 60 lines; `spec:check` warns past it) and carries `Approved-by:`, empty until the owner fills `<name> (<YYYY-MM-DD>) sha256:<12 hex>`, the digest `spec:queue` prints, which `spec:check` fails once `proposal.md` or `design.md` changes; the change's first task holds `Hold: OPEN-Q proposal awaits owner approval` until the owner removes it in the same edit, and an agent whose Stop boundary is proposal approval stops there and hands off | `task spec:check`; review |
| `review` | A non-trivial change declares the `quick` or `design` track at Clarify; `design` is required when it adds a spec Requirement, touches more than one capability under `openspec/specs/`, adds exported surface reachable from an adopting repository, or touches a boot path, durability/ack behavior, or a gate file, and then carries a `design.md` (≤ 120 lines; `spec:check` warns past it) drafted by the read-only `design` skill and approved with the proposal under the one `OPEN-Q` hold | `task spec:check`; review |
| `verify` | `task verify` (`task validate`) runs read-only Go, UI, and Governance lanes mirroring `Repository CI` without mutating the worktree | `scripts/validate.sh`; `TestCIWorkflowGate` |
| `verify+review` | Review requires an independent cross-provider reviewer not weaker than every implementer by model tier, then effort band (`.agents/review-policy.json`) | `scripts/merge-check.sh`; `.agents/contracts/reviewer.md` |
| `merge:check+review` | Non-draft code PRs carry `implemented-by:` and `reviewed-by:` each naming an agent (`claude`, `codex`, `gemini`) with its model and native effort in parentheses (`claude (opus, high)`), the reviewer not weaker by tier then band (`ateam review compare`) and a different agent (`cross_provider: true`), plus a PR comment review record (`reviewed-by: <agent>` or `### Independent Cross-Agent Review`) naming a PR commit with only clean `main` merges or docs-only commits after it, checked alongside active `main` ruleset (`Required,Merge check`) and open `class:flake` issues by `task merge:check` | `.github/workflows/merge-check.yml`; `scripts/merge-check.sh`; `TestMergeCheckWorkflow` |
| `review` | A PR archives its completed OpenSpec change before it is marked ready; once its required check is green on an up-to-date branch and the squash auto-merge (GitHub or Gitea) has landed, the claiming agent removes its worktree and remote branch (the `main` run is informational), and a review checkout is removed when its record is posted | `.agents/protocol.md` § Close; `scripts/ateam state pickup` |

## Related Repos

- `semstreams` — the substrate framework (this repo's only Go dependency of
  note); canonical NATS dev setup, revive.toml, Taskfile shape.
- `semspec` — UI graph-pane origin (`SigmaCanvas.svelte` quad).
- `semdragon` — SSE service + runes `worldStore` live-sim store pattern.
- `semteams` — `colors.css` three-layer theme and `ateam` multi-agent harness pattern.
