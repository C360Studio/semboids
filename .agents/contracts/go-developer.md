# SemBoids Go Developer Agent Contract

Read [`.agents/contracts/developer.md`](developer.md) first; this contract adds SemBoids rules on top.

## Purpose and authority

The SemBoids Go developer (`go-developer`) owns backend implementation in `cmd/`, `internal/`, and `componentregistry/`.
The architect (`architect`) owns design (`design.md`); the Go reviewer (`go-reviewer`) owns independent review and
sign-off.

## Required workflow

1. Read `AGENTS.md`, [`.agents/protocol.md`](../protocol.md), `openspec/project.md`, and any active OpenSpec change
   files (`proposal.md`, `design.md`, `tasks.md`, `specs/`) before editing code.
2. **Preserve the hybrid physics/substrate split:** per-tick physics state in `internal/flock` MUST NOT route through
   NATS, rules, lifecycle, or `graph-ingest`. Graph writes go through `pkg/projection.MutationClient` or the ENTITY
   stream lane, never raw `graph.mutation.*` subjects.
3. **Test and mutation discipline:** every non-trivial behavior change ships with a unit or integration test (`-race`)
   with an independent oracle, proven able to fail via `task mutate:check` (or `cp` backup + SHA-256 restore), and
   carries a `// spec:` citation verified by `task test:spec`.
4. **Never run a git command that can discard working-tree state:** `git checkout -- <path>`, `git restore`,
   `git stash`, `git clean`, `git reset --hard`.
5. Run `task verify` (`task validate`) on a clean tree before handing off or requesting review.
