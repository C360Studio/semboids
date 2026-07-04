/**
 * Flock frame wire format, published by the Go sim component per tick:
 *   {"tick":N,"t":unixMillis,"w":W,"h":H,"boids":[[id,x,y,vx,vy],...]}
 *
 * On the browser socket, the semstreams websocket-output wraps each frame in
 * a client envelope: {"type":"data","id":...,"timestamp":...,"payload":<frame>}.
 * parseFrame accepts both shapes.
 */

export type BoidTuple = [id: number, x: number, y: number, vx: number, vy: number];

export interface Frame {
  tick: number;
  t: number;
  w: number;
  h: number;
  boids: BoidTuple[];
}

function isFrame(value: unknown): value is Frame {
  if (typeof value !== "object" || value === null) return false;
  const f = value as Record<string, unknown>;
  return (
    typeof f.tick === "number" &&
    typeof f.t === "number" &&
    typeof f.w === "number" &&
    typeof f.h === "number" &&
    Array.isArray(f.boids)
  );
}

/**
 * Parse a websocket message into a Frame, unwrapping the ws-output data
 * envelope when present. Returns null for anything that isn't a frame —
 * malformed JSON, status messages, foreign payloads.
 */
export function parseFrame(raw: string): Frame | null {
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return null;
  }
  if (isFrame(value)) return value;

  if (typeof value === "object" && value !== null) {
    const env = value as Record<string, unknown>;
    if (env.type === "data" && isFrame(env.payload)) return env.payload;
  }
  return null;
}
