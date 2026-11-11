# Proposal: Adopt ATeam v0.4.1, `.tools/bin` Shim, and Merge Check Gate

Approved-by: Coby Leuschke (2026-11-11) sha256:6b89e4b9044d

## Why

`semboids` had no `ateam` multi-agent harness, no `.agents/` directory, no hook or MCP configurations, no `Merge check`
workflow, and duplicated `CLAUDE.md` / `AGENTS.md` files. Adopting `ateam v0.4.1` aligns `semboids` with `semteams`
(`#14`).

## What Changes

- Commit `.agents/ateam.json` (`{"version": 1, "ateam_version": "v0.4.1"}`), `scripts/ateam` shim, and `task tools:ateam`
  installing `ateam@v0.4.1` into `<main-checkout>/.tools/bin/ateam`.
- Replace `CLAUDE.md` with the `@AGENTS.md` import, add numbered rules to `AGENTS.md` mirrored in `.agents/rules-index.md`,
  and install `.agents/protocol.md`, base + specialist contracts, canonical skills, `.agents/review-policy.json`, and
  `.agents/hosts/enjoy.toml`.
- Add `scripts/doctor.sh`, `scripts/merge-check.sh`, `scripts/validate.sh`, `scripts/verify.sh`, `taskfiles/openspec.yml`,
  `.github/workflows/ci.yml` (`Go`, `UI`, `Governance`, `Required`), `.github/workflows/merge-check.yml` (`awaiting` with
  `checks: read` + `merge-check`), and contract tests in `test/contract/`.

## Non-goals

- Modifying SemBoids physics, zone steering, SemStreams runtime wiring, or UI components.

Closes #14.
