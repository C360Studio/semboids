#!/usr/bin/env bash
# The semboids "easy button": one command from a fresh clone to a running demo.
# Starts NATS (Docker), installs UI deps on first run, then runs the backend and
# the SvelteKit dev server together and opens the browser. Ctrl-C stops the
# backend + UI (NATS is left running for a fast restart — `task dev:nats:stop`
# to stop it).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

say() { printf '\033[1;36m▶ %s\033[0m\n' "$*"; }
ok() { printf '\033[1;32m✓ %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

# ── Preflight: fail early with an actionable message ──────────────────────────
command -v docker >/dev/null 2>&1 || die "Docker is required — install Docker Desktop or colima, then re-run."
docker info >/dev/null 2>&1 || die "Docker is installed but the daemon isn't running — start Docker, then re-run."
command -v go >/dev/null 2>&1 || die "Go is required (1.26+) — https://go.dev/dl, then re-run."
command -v npm >/dev/null 2>&1 || die "Node.js/npm is required (Node 22+) — https://nodejs.org, then re-run."

# ── Build the backend up front ────────────────────────────────────────────────
# Build (not `go run`): `go run` launches the server as a temp child process
# that a Ctrl-C on this script would orphan. A real binary is the process we
# start, so shutdown is clean — and a compile error fails fast, before NATS.
BIN="$root/cmd/semboids/semboids"
say "Building the backend …"
go build -o "$BIN" ./cmd/semboids || die "Backend build failed — see the error above."
ok "Backend built"

# ── NATS (isolated on :24222) ─────────────────────────────────────────────────
# The demo runs its OWN NATS on :24222, NOT the standard :4222, so it never
# collides with a sibling sem* stack that already owns :4222 (config-bucket
# collision, semstreams#459). The backend is pointed at it via
# SEMBOIDS_NATS_URLS. Container: semboids-demo-nats (distinct from the
# task dev:nats:start container).
NATS_NAME=semboids-demo-nats
export SEMBOIDS_NATS_URLS=nats://localhost:24222
say "Starting an isolated NATS (JetStream) on :24222 …"
if docker ps --format '{{.Names}}' | grep -q "^${NATS_NAME}$"; then
  ok "NATS already running (${NATS_NAME})"
elif docker ps -a --format '{{.Names}}' | grep -q "^${NATS_NAME}$"; then
  docker start "$NATS_NAME" >/dev/null && ok "NATS restarted (${NATS_NAME})"
else
  docker run -d --name "$NATS_NAME" -p 24222:4222 -p 28222:8222 nats:2.12-alpine -js -m 8222 >/dev/null \
    || die "Couldn't start NATS — is :24222 already in use? 'docker rm -f ${NATS_NAME}' and re-run."
  ok "NATS started (nats://localhost:24222, monitor :28222)"
fi
# Wait for NATS to accept connections.
for _ in $(seq 1 30); do
  curl -fsS --max-time 1 http://localhost:28222/healthz >/dev/null 2>&1 && break
  sleep 0.5
done

# ── UI deps (first run only) ──────────────────────────────────────────────────
if [ ! -d "$root/ui/node_modules" ]; then
  say "Installing UI dependencies (first run, ~30s) …"
  (cd "$root/ui" && npm install)
  ok "UI dependencies installed"
fi

# ── Run backend + UI together; clean up both on exit ──────────────────────────
pids=()
cleanup() {
  printf '\n'
  say "Stopping demo (backend + UI). NATS left running — 'docker stop ${NATS_NAME}' to stop it."
  for pid in "${pids[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

say "Starting backend (API :8080, frames :8081, metrics :9090) …"
( exec "$BIN" --config configs/flock.json ) &
pids+=($!)

say "Starting UI (SvelteKit dev server) …"
( cd "$root/ui" && exec npm run dev -- --host >/dev/null ) &
pids+=($!)

# Wait for the backend API and the UI to answer, then announce + open.
url="http://localhost:5173"
for _ in $(seq 1 60); do
  if curl -fsS --max-time 1 http://localhost:8080/boids/graph >/dev/null 2>&1 \
     && curl -fsS --max-time 1 "$url" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

printf '\n'
ok "SemBoids is live"
printf '\033[1;35m  →  Open %s\033[0m\n\n' "$url"
printf '   Flock canvas (left) + the substrate graph (right). Toggle rules,\n'
printf '   crank the graph dial, and hit "+ spawn" to grow the flock.\n'
printf '   Ctrl-C to stop.\n\n'

# Best-effort browser open (non-fatal).
if command -v open >/dev/null 2>&1; then open "$url" 2>/dev/null || true
elif command -v xdg-open >/dev/null 2>&1; then xdg-open "$url" 2>/dev/null || true
fi

wait
