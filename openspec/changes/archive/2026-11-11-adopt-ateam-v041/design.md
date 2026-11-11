# Design: Adopt ATeam v0.4.1, `.tools/bin` Shim, and Merge Check Gate

## Context

`semboids` is a Go 1.26 + SvelteKit 2 repository consuming `github.com/c360studio/semstreams`. Prior to `#14`, it had
no `.agents/` harness, no `Merge check` workflow, separate `ci.yml` and `ui.yml` workflows without draft-PR skipping,
and duplicated `AGENTS.md` / `CLAUDE.md` files.

## Goals / Non-Goals

Goals: adopt `ateam v0.4.1` following the `semteams` pattern (`.agents/ateam.json` pin, `<main>/.tools/bin/ateam` +
`scripts/ateam` shim, `review_launch: "agent"` with `.agents/hosts/enjoy.toml`, unified `Repository CI` + `Merge check`
workflows, read-only `task validate` / `task verify`, and `test/contract` gate tests). Non-goals: runtime, physics, or
UI feature changes.

## Decisions

### D1. Repo-local `ateam` installation and shim (`semteams` pattern)

- **A (selected): Install `ateam@v0.4.1` into `<main-checkout>/.tools/bin/ateam` via `task tools:ateam` and route hooks,
  MCP configs, and Taskfile commands through `scripts/ateam`.**
  - `scripts/ateam` reads `.git` directly in linked worktrees without forking `git` (~1.3 ms hook execution, well under
    `ateam doctor`'s 10 ms budget).
  - `.agents/ateam.json` pins `"ateam_version": "v0.4.1"` so `task ateam:doctor` verifies both the running binary and
    the hook binary.
  - Cost: one 22-line POSIX shell script. Forecloses nothing.
- **B: Rely on global `PATH` without `.agents/ateam.json` or `scripts/ateam`.** Rejected: drifts across sibling repos
  pinning different `ateam` releases.

Adopter seam: `scripts/ateam`, `Taskfile.yml` (`tools:ateam`), and `.agents/ateam.json`.

### D2. Unified `Repository CI` (`Required`) and separate `Merge check` workflow

- **A (selected): Fold `ui.yml` into `.github/workflows/ci.yml` (`go`, `ui`, `governance`, `required`) and add
  `.github/workflows/merge-check.yml` (`awaiting` probe with `checks: read` + `merge-check`).**
  - Matches `semteams`: every job skips on draft PRs under a name that never evaluates to `Required` or `Merge check`,
    `Required` fails closed unless all three lanes succeed, and `Merge check` runs `scripts/merge-check.sh` from the
    base commit with `ATEAM_REQUIRED_CHECKS: Required,Merge check`.
- **B: Keep paths-filtered `ui.yml` separate.** Rejected: paths-filtered workflows do not report a status check on
  backend-only PRs, preventing a single unconditional required status check.

Adopter seam: `.github/workflows/ci.yml`, `.github/workflows/merge-check.yml`, `scripts/validate.sh`, and
`test/contract/workflow_gate_test.go`.
