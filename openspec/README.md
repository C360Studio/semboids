# SemBoids OpenSpec

SemBoids uses [OpenSpec](https://github.com/Fission-AI/OpenSpec) for
spec-driven development: specs are the current truth per capability, and every
non-trivial change is proposed as a delta against them before code. The CLI and
Claude Code skills are installed (`/opsx:new`, `/opsx:continue`, `/opsx:apply`,
`/opsx:archive`; `openspec list`, `openspec validate`).

The format converges with the rest of the `sem*` family (semstreams, semspec,
semteams) so a change authored in one repo reads the same in the next.

## Layout

- `project.md` — standing project context: Purpose, **Product Boundary**,
  architecture non-negotiables, and conventions. Read this first when scoping
  anything.
- `config.yaml` — machine context injected into artifact creation, plus
  per-artifact rules. The human-readable source of truth is `project.md`.
- `specs/<capability>/spec.md` — **current truth** for a capability:
  `Requirement` + `GIVEN/WHEN/THEN` scenarios describing what it does *today*.
- `changes/<id>/proposal.md` — why the change exists, what changes (`## Why`,
  `## What Changes`, `## Non-goals`).
- `changes/<id>/tasks.md` — implementation checklist in dependency order.
- `changes/<id>/specs/<capability>/spec.md` — the **delta**: the target-state
  requirements this change adds/modifies/removes.
- `changes/archive/` — completed changes, moved here on `openspec archive`.

## Discipline

1. **Seed specs lazily.** Create a spec when a change first touches that
   capability. Do NOT backfill up front — an unverified spec is just another
   drifting doc.
2. **Archive changes on completion.** `proposal → tasks → deltas → implement →
   archive`. On archive, durable requirements are promoted into the baseline
   `specs/`.

## Relationship to `docs/`

- `docs/adr/` — genuine **decisions** only (irreversible choices, cross-repo
  contracts). History.
- Tutorial/runbook content stays as docs; "how it works" content lives in specs.
