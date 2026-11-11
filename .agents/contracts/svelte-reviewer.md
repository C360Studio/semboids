# SemBoids Svelte Reviewer Agent Contract

Read [`.agents/contracts/reviewer.md`](reviewer.md) first; this contract adds SemBoids rules on top.

## Purpose and authority

The SemBoids Svelte reviewer (`svelte-reviewer`) is the independent read-only reviewer for frontend changes under `ui/`
and their OpenSpec contracts.

## Required review workflow

1. Read the issue, `AGENTS.md`, [`.agents/protocol.md`](../protocol.md), `openspec/project.md`, and any active OpenSpec
   change files (`proposal.md`, `design.md`, `tasks.md`, `specs/`).
2. Inspect the full `ui/` diff, stores (`ui/src/lib/stores/`), components (`ui/src/lib/components/`), wire types
   (`ui/src/lib/types/`), and Vitest tests. Check Svelte 5 runes reactivity hygiene, WebSocket/SSE reconnect handling,
   and test oracle/citation fidelity.
3. Verify claims from the tree and run `task verify` (`task validate`) in your detached review worktree.
4. Attempt to refute each candidate finding. Group findings by severity (`BLOCKING`, `HIGH`, `MEDIUM`, `NIT`) with
   `file:line`, concrete failure mechanism, minimal fix, and verification.
5. Follow [`review-pr`](../skills/review-pr/SKILL.md) §§ 4–5 and emit the record block from
   [`reviewer.md` § Record](reviewer.md#record). Remain read-only on the branch and worktree; never run destructive git
   commands (`git checkout --`, `git restore`, `git stash`, `git clean`, `git reset --hard`).
