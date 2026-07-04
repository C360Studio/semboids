# Design — add-flock-core

## World and physics

- **2D toroidal world**, unit-free floats (default 1600×900 to match a wide
  canvas). Wrap keeps the math trivial and flocks visually continuous; walls
  can arrive later as a zone type.
- **Boid**: `{ID uint32, Pos, Vel Vec2}` in a flat slice — array-of-structs is
  fine at this scale; no premature SoA.
- **Tick**: fixed timestep from `time.NewTicker(1/rate)`; steering computes
  accelerations from the *previous* tick's snapshot (double-buffer) so
  update order can't bias the flock.
- **Reynolds steering** per boid: separation (inverse-distance repulsion,
  radius ~25), cohesion (toward neighborhood centroid, radius ~50), alignment
  (toward neighborhood mean velocity, radius ~50), each weighted (defaults
  1.5/1.0/1.0), summed, clamped by max force, then velocity clamped by max
  speed. All radii/weights/clamps in a `Params` struct — these become live UI
  controls in a later change.
- **Spatial hash**: uniform grid, cell size = max neighbor radius, rebuilt
  each tick into reused buckets (O(N), zero steady-state allocation goal).
  Neighbor query scans 3×3 cells with toroidal wrap.
- **Determinism**: `rand.New(rand.NewPCG(seed, seed))` for initial placement;
  same seed + params ⇒ identical trajectories. This is what makes the physics
  testable (golden ticks) and later A/B-able (rule on vs. off).

## Egress

- The engine is wrapped in a **SemStreams Input component** (`sim`), the
  "external system" being the simulation itself. ServiceManager owns
  start/stop; the tick goroutine respects component ctx cancellation.
- One frame per tick published to core-NATS subject **`boids.frames`**
  (fire-and-forget; JetStream persistence is pointless for ephemeral frames).
- **Wire format** (JSON, compact arrays):

  ```json
  {"tick":1234,"t":1719936000123,"w":1600,"h":900,
   "boids":[[id,x,y,vx,vy], ...]}
  ```

  200 boids ≈ 8–10KB/frame ≈ 300KB/s at 30Hz — trivial for localhost/WS.
  Velocities ship so the UI can draw headings without differencing frames.
- `output/websocket` subscribes to `boids.frames`, broadcasts
  **at-most-once** with per-client write timeouts — a slow client drops
  frames; nothing propagates back to physics.
- Verified shape on the wire to browsers: the ws-output wraps each frame in
  its client envelope `{"type":"data","id":"msg-…","timestamp":…,
  "payload":{<frame>}}` — the UI store reads `payload` when
  `type === "data"`.

## Host wiring

- `cmd/semboids/main.go` mirrors the canonical semstreams host: blank-import
  `net/http/pprof`, `service.MaybeStartPProf(debug, 6060)` before NATS,
  `componentregistry.Register` + `boids.Register` (sim component + frame
  payload), ServiceManager, JSON flow config (`configs/flock.json`):
  `sim → boids.frames → output/websocket`.
- Flags/env: `--boids` (200), `--tick-hz` (30), `--seed`,
  `SEMBOIDS_NATS_URLS`, `--debug`.

## UI

- `ui/` per house conventions: Svelte 5 runes, SvelteKit 2, adapter-node,
  Caddyfile (WS + API → :8080, else → ui :3000), vite dev :5173.
- **WS store** (`src/lib/stores/flock.svelte.ts`): singleton service after
  semteams' `runtimeWebSocket.ts` (backoff reconnect), writing the latest
  frame into `$state` — *latest wins*, no frame queue.
- **Canvas pane** (`FlockCanvas.svelte`): `requestAnimationFrame` loop reads
  the latest frame and draws triangles oriented by velocity; devicePixelRatio
  aware; theme via semteams `colors.css` (categorical palette reserved for
  flock communities in the graph-pane change).
- Split-screen layout ships with an empty right pane placeholder — the sigma
  graph pane is a later change.
