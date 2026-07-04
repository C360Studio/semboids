import { describe, expect, it } from "vitest";
import { parseFrame } from "./frame";

const bare = {
  tick: 42,
  t: 1719936000123,
  w: 1600,
  h: 900,
  boids: [
    [0, 100, 200, 1.5, -2],
    [1, 0.25, 900, 0, 3],
  ],
};

const withZones = {
  tick: 43,
  t: 1719936000156,
  w: 1600,
  h: 900,
  zones: [
    ["predator", 500, 450, 90],
    ["wind", 800, 700, 140],
  ],
  boids: [
    [0, 100, 200, 1.5, -2, 0],
    [1, 0.25, 900, 0, 3, 1],
  ],
};

describe("parseFrame", () => {
  it("parses a bare frame", () => {
    const f = parseFrame(JSON.stringify(bare));
    expect(f).not.toBeNull();
    expect(f?.tick).toBe(42);
    expect(f?.w).toBe(1600);
    expect(f?.boids).toHaveLength(2);
    expect(f?.boids[0]).toEqual([0, 100, 200, 1.5, -2]);
  });

  it("unwraps the semstreams websocket-output data envelope", () => {
    const envelope = {
      type: "data",
      id: "msg-123",
      timestamp: 1719936000123,
      payload: bare,
    };
    const f = parseFrame(JSON.stringify(envelope));
    expect(f).not.toBeNull();
    expect(f?.tick).toBe(42);
    expect(f?.boids).toHaveLength(2);
  });

  it("parses 6-element tuples and zones (zone-steering format)", () => {
    const f = parseFrame(JSON.stringify(withZones));
    expect(f).not.toBeNull();
    expect(f?.boids[1][5]).toBe(1); // flee modifier flag
    expect(f?.zones).toHaveLength(2);
    expect(f?.zones?.[0]).toEqual(["predator", 500, 450, 90]);
  });

  it("legacy 5-element frames without zones still parse", () => {
    const f = parseFrame(JSON.stringify(bare));
    expect(f).not.toBeNull();
    expect(f?.boids[0]).toHaveLength(5);
    expect(f?.zones ?? []).toEqual([]);
  });

  it("returns null for non-frame messages", () => {
    expect(parseFrame(JSON.stringify({ type: "status", payload: {} }))).toBeNull();
    expect(parseFrame(JSON.stringify({ hello: "world" }))).toBeNull();
    expect(parseFrame("not json")).toBeNull();
    expect(parseFrame(JSON.stringify({ ...bare, boids: "nope" }))).toBeNull();
  });
});
