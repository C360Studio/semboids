#!/usr/bin/env bash
# Report tool versions and compare each against its single pin. Starts no workloads.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
ok()   { printf '  ok    %-15s %s\n' "$1" "$2"; }
bad()  { printf '  FAIL  %-15s %s\n' "$1" "$2"; fail=1; }
warn() { printf '  warn  %-15s %s\n' "$1" "$2"; }

# version_ge A B: true when A >= B (dotted numeric versions).
version_ge() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]; }

echo "SemBoids doctor"

# go: go.mod's go directive is the pin; the toolchain must be at least that.
if command -v go >/dev/null; then
  want=$(awk '$1=="go"{print $2; exit}' go.mod)
  have=$(go env GOVERSION | sed 's/^go//')
  if version_ge "$have" "$want"; then ok go "$have (go.mod: $want)"; else bad go "$have < go.mod $want"; fi
else bad go "not found"; fi

# task: Taskfile runner.
if command -v task >/dev/null; then
  have=$(task --version | grep -Eo '[0-9]+\.[0-9]+\.[0-9]+' | head -n1)
  ok task "$have"
else bad task "not found"; fi

# node: Node 22+ for SvelteKit UI and Vitest.
if command -v node >/dev/null; then
  have=$(node --version | sed 's/^v//')
  if version_ge "$have" "22.0.0"; then ok node "$have"; else bad node "$have < 22.0.0"; fi
else bad node "not found"; fi

if command -v npm >/dev/null; then ok npm "$(npm --version)"; else bad npm "not found"; fi

# openspec: required by task spec:check / task openspec:validate.
[ -d "$PWD/node_modules/.bin" ] && PATH="$PWD/node_modules/.bin:$PATH"
if command -v openspec >/dev/null; then
  have_openspec=$(openspec --version 2>&1 | grep -Eo '[0-9]+\.[0-9]+\.[0-9]+' | head -n1 || true)
  ok openspec "${have_openspec:-present}"
else bad openspec "not found (npm install --global @fission-ai/openspec)"; fi

# ateam: pinned in Taskfile.yml, .agents/ateam.json, and .github/workflows/ci.yml (v0.4.1).
main_root="$PWD"
if [ -f "$PWD/.git" ]; then
  read -r gitdir <"$PWD/.git"
  gitdir="${gitdir#gitdir: }"
  main_root="${gitdir%/.git/worktrees/*}"
fi
for tools_bin in "$PWD/.tools/bin" "$main_root/.tools/bin"; do
  [ -d "$tools_bin" ] && PATH="$tools_bin:$PATH"
done
if command -v ateam >/dev/null; then
  want_ateam="v0.4.1"
  have_ateam=$(ateam version 2>&1 | grep -Eo 'v[0-9]+\.[0-9]+\.[0-9]+' | head -n1 || true)
  if [ "$have_ateam" = "$want_ateam" ]; then ok ateam "$have_ateam"; else bad ateam "${have_ateam:-unknown} != $want_ateam (run 'task tools:ateam')"; fi
else bad ateam "not found (run 'task tools:ateam')"; fi

# ui/node_modules: required for UI checks and task validate.
if [ -d "ui/node_modules" ]; then
  ok ui/node_modules "present"
else
  warn ui/node_modules "missing (run 'npm --prefix ui ci' before UI checks or task validate)"
fi

# docker: version and daemon reachability only; no containers are started.
daemon=false
if command -v docker >/dev/null; then
  ok docker "client $(docker --version | sed 's/^Docker version //; s/,.*//')"
  if docker info >/dev/null 2>&1; then ok docker-daemon "reachable"; daemon=true
  else warn docker-daemon "not reachable (task test:integration and task validate need it)"; fi
else bad docker "not found"; fi

if $daemon; then
  context=$(docker context show 2>/dev/null || echo unknown)
  endpoint=$(docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null || echo unknown)
  ok docker-context "$context ($endpoint)"
  if [ -n "${DOCKER_HOST:-}" ]; then warn docker-host "DOCKER_HOST=$DOCKER_HOST overrides the context"; fi
fi

if [ "$fail" -ne 0 ]; then echo "doctor: FAILED"; exit 1; fi
echo "doctor: ok"
