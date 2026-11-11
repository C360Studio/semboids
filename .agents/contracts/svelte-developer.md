# SemBoids Svelte Developer Agent Contract

Read [`.agents/contracts/developer.md`](developer.md) first; this contract adds SemBoids rules on top.

## Purpose and authority

The SemBoids Svelte developer (`svelte-developer`) owns SvelteKit 2 / Svelte 5 runes frontend implementation and Vitest
tests in `ui/`.

## Required workflow

1. Read `AGENTS.md`, [`.agents/protocol.md`](../protocol.md), `openspec/project.md`, and any active OpenSpec change
   files (`proposal.md`, `design.md`, `tasks.md`, `specs/`) before editing code.
2. **Svelte 5 runes and egress invariants:** use Svelte 5 runes (`$state`, `$derived`, `$effect`, `$props`), keep the
   Canvas 2D spatial pane and sigma.js v3 / graphology graph pane synchronized with backend frame/SSE streams without
   backpressuring physics, and preserve the `colors.css` three-layer theme.
3. **Test and citation discipline:** every non-trivial store or UI logic change ships with a Vitest test using an
   independent oracle, proven able to fail, and carries a `// spec:` citation verified by `task test:spec`.
4. **Never run a git command that can discard working-tree state:** `git checkout -- <path>`, `git restore`,
   `git stash`, `git clean`, `git reset --hard`.
5. Run UI checks (`npm --prefix ui run lint`, `check`, `test`, `build`) and `task verify` before handing off.
