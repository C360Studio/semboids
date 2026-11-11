# Repository Governance Specification

## Purpose

Define the CI, local verification, and multi-agent harness invariants for SemBoids: one aggregate `Required` status
check covering Go, UI, and Governance lanes, a separate `Merge check` workflow enforcing `ateam review gate` with the
standing-pass `awaiting` probe, and the repo-local `.tools/bin/ateam` shim (`v0.4.1`).

## Requirements

### Requirement: Repository CI runs the obvious repository checks

Repository CI (`.github/workflows/ci.yml`) and local `task verify` (`scripts/validate.sh`) SHALL run the Go lane
(`task lint`, `go vet -tags=integration ./...`, `task test`, `task test:integration`, and `linux/amd64` cross-compile of
`./cmd/semboids`), the UI lane (`npm run lint`, `npm run check`, `npm test`, and `npm run build` in `ui/`), and the
Governance lane (`task ateam:doctor`, `task openspec:validate`, and `task test:spec` scoped by `TEST_SPEC_BASE`).
Hooks, MCP registrations, and Taskfile `ateam` targets SHALL resolve the pinned `ateam` release (`v0.4.1` in
`.agents/ateam.json` and `Taskfile.yml`) through `scripts/ateam` (`<main>/.tools/bin/ateam` → `<worktree>/.tools/bin/ateam`
→ `PATH`) without forking `git`.

#### Scenario: Governance checks multi-agent parity and test-to-spec citations

- **WHEN** `Repository CI` runs on a push to `main` or a non-draft pull request targeting `main`
- **THEN** the `governance` job checks out full history (`fetch-depth: 0`), installs UI dependencies in `ui/`, installs
  `ateam@v0.4.1`, and runs `task ateam:doctor`, `task openspec:validate`, and `task test:spec` with `TEST_SPEC_BASE` set
  to the pull request base SHA or the push's previous commit

#### Scenario: Repo-local ateam shim resolves main checkout before worktree and PATH

- **WHEN** `scripts/ateam` is invoked from a linked worktree or the primary checkout
- **THEN** it executes `<main-checkout>/.tools/bin/ateam` first, falls back to `<worktree>/.tools/bin/ateam` then `PATH`
  without invoking `git`, and exits `127` naming `task tools:ateam` when no `ateam` binary exists

### Requirement: One unconditional repository CI workflow reports one aggregate

`.github/workflows/ci.yml` SHALL define a `required` job depending on `[go, ui, governance]` that fails unless all
three lanes report `success`, and every job in `.github/workflows/ci.yml` SHALL skip on draft pull requests under a job
name that never evaluates to `Required`.

#### Scenario: Required fails when any lane does not succeed

- **WHEN** any of `go`, `ui`, or `governance` has a result other than `success` on a non-draft run
- **THEN** the `required` job exits non-zero and fails the `Required` check run

### Requirement: Merge check is its own required workflow

`.github/workflows/merge-check.yml` SHALL trigger on `pull_request` events (`opened`, `synchronize`, `reopened`,
`ready_for_review`, `edited`) with queued per-PR concurrency (`cancel-in-progress: false`), run the `awaiting` probe job
with `checks: read` from the pull request's base commit (`ateam@v0.4.1`), skip the `merge-check` job on draft pull
requests or while `awaiting == 'true'`, and otherwise run `scripts/merge-check.sh` from the base commit with
`ATEAM_REQUIRED_CHECKS: Required,Merge check`.

#### Scenario: Awaiting probe skips Merge check until a review record or standing pass exists

- **WHEN** a non-draft code pull request has no `reviewed-by:` line in its description and no passing `Merge check`
  check run at its head
- **THEN** `awaiting` outputs `awaiting=true` and `merge-check` is skipped under `Merge check (awaiting review record, skipped)`
