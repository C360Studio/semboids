#!/usr/bin/env bash
# validate.sh — authoritative read-only local validation mirroring Repository CI
# (.github/workflows/ci.yml: go, ui, and governance lanes).
# Invoked by both `task validate` and `task verify`.
#
# Guarantees:
#   1. Fails explicitly before running lanes when any prerequisite tool or
#      daemon is missing (go, gofmt, task, node, npm, docker, openspec, ateam,
#      git, or ui/node_modules).
#   2. Writes Go build artifacts only to a temporary directory outside the
#      repository.
#   3. Preserves all tracked, staged, unstaged, and untracked worktree content
#      whether validation passes or fails.
#   4. Reports per-lane wall-clock timings using bash's builtin SECONDS.

set -euo pipefail

fail() {
  echo "::error::validate: $*" >&2
  exit 1
}

REPO_ROOT="${VALIDATE_REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$REPO_ROOT"

HAD_UI_BUILD=0
if [ -e "$REPO_ROOT/ui/build" ]; then
  HAD_UI_BUILD=1
fi
HAD_UI_SVELTE_KIT=0
if [ -e "$REPO_ROOT/ui/.svelte-kit" ]; then
  HAD_UI_SVELTE_KIT=1
fi

TMP_DIR=""
cleanup() {
  if [ -n "${TMP_DIR:-}" ] && [ -d "$TMP_DIR" ]; then
    rm -rf -- "$TMP_DIR"
  fi
  if [ "$HAD_UI_BUILD" -eq 0 ] && [ -e "$REPO_ROOT/ui/build" ]; then
    rm -rf -- "$REPO_ROOT/ui/build"
  fi
  if [ "$HAD_UI_SVELTE_KIT" -eq 0 ] && [ -e "$REPO_ROOT/ui/.svelte-kit" ]; then
    rm -rf -- "$REPO_ROOT/ui/.svelte-kit"
  fi
}
trap cleanup EXIT

check_prereqs() {
  local missing=()
  local tool
  for tool in go gofmt task node npm docker openspec ateam git; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      missing+=("$tool")
    fi
  done
  if [ "${#missing[@]}" -gt 0 ]; then
    fail "missing required prerequisite(s): ${missing[*]}"
  fi
  if ! docker info >/dev/null 2>&1; then
    fail "prerequisite check failed: docker daemon is not reachable (docker info failed)"
  fi
  if [ ! -d "$REPO_ROOT/ui/node_modules" ]; then
    fail "missing required prerequisite: ui/node_modules (run 'npm --prefix ui ci' first)"
  fi
}

main_root="$REPO_ROOT"
if [ -f "$REPO_ROOT/.git" ]; then
  read -r gitdir <"$REPO_ROOT/.git"
  gitdir="${gitdir#gitdir: }"
  main_root="${gitdir%/.git/worktrees/*}"
fi
for tools_bin in "$REPO_ROOT/.tools/bin" "$main_root/.tools/bin"; do
  [ -d "$tools_bin" ] && PATH="$tools_bin:$PATH"
done

check_prereqs

TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/semboids-validate.XXXXXX")
SECONDS=0

echo "=== [1/3] Go lane ==="
go_start=$SECONDS
task lint
go vet -tags=integration ./...
task test
task test:integration

mkdir -p "$TMP_DIR/bin"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o "$TMP_DIR/bin/semboids-linux-amd64" ./cmd/semboids
go_elapsed=$((SECONDS - go_start))

echo "=== [2/3] UI lane ==="
ui_start=$SECONDS
(
  cd "$REPO_ROOT/ui"
  npm run lint
  npm run check
  npm test
  npm run build
)
ui_elapsed=$((SECONDS - ui_start))

echo "=== [3/3] Governance lane ==="
gov_start=$SECONDS
task ateam:doctor
task openspec:validate
task test:spec
gov_elapsed=$((SECONDS - gov_start))

echo
echo "lane timings:"
echo "  go          ${go_elapsed}s"
echo "  ui          ${ui_elapsed}s"
echo "  governance  ${gov_elapsed}s"
echo "✅ validate: all Repository CI lanes passed (Go, UI, Governance)"
