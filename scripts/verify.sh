#!/usr/bin/env bash
# verify.sh — SemEngine/SemTeams-standard alias entrypoint for scripts/validate.sh.
# Both `task verify` and `task validate` run the same read-only CI parity gate.
set -euo pipefail
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/validate.sh" "$@"
