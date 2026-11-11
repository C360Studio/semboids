#!/usr/bin/env bash
# Merge check (openspec spec repository-governance).
# The check is `ateam review gate` (github.com/c360studio/ateam internal/review/gate.go)
# on GitHub and on Gitea; this wrapper keeps the entrypoint `task merge:check` calls.
# The binary is $ATEAM, else ateam on PATH.
#
# Usage: merge-check.sh <pr-number>
#   Exit 0 pass, 1 fail, 2 unavailable (a read failed or was not the shape asked for).
#   The kind of run comes from GITHUB_EVENT_NAME only when GITHUB_ACTIONS is true.
set -euo pipefail
export ATEAM_REQUIRED_CHECKS="${ATEAM_REQUIRED_CHECKS:-Required,Merge check}"
exec "${ATEAM:-ateam}" review gate "$@"
