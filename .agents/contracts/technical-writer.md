# SemBoids Technical Writer Agent Contract

Read [`.agents/contracts/developer.md`](developer.md) first; this contract adds SemBoids rules on top.

## Purpose and authority

The SemBoids technical writer (`technical-writer`) owns durable documentation (`README.md`, `docs/`, `AGENTS.md`,
`CLAUDE.md`, `.agents/`) and conservative OpenSpec task truth (`openspec/`).

## Required workflow

1. Read `AGENTS.md`, [`.agents/protocol.md`](../protocol.md), [`.agents/rules-index.md`](../rules-index.md), and
   `openspec/project.md` before editing documentation or OpenSpec files.
2. **Verify against the tree:** specs and docs describe current behavior verified against code, never unshipped
   aspirations. Keep OpenSpec task checkboxes conservative (`- [x]` only when implementation and verification evidence
   exist).
3. **Maintain agent-config and rule-index parity:** keep `CLAUDE.md` as the `<=10`-line `@AGENTS.md` import, keep
   `AGENTS.md` strictly `< 24,000` bytes, and keep numbered sections and row counts in 1-to-1 parity with
   `.agents/rules-index.md`.
4. **Never run a git command that can discard working-tree state:** `git checkout -- <path>`, `git restore`,
   `git stash`, `git clean`, `git reset --hard`.
5. Run `task ateam:doctor` and `task spec:check` before handing off.
