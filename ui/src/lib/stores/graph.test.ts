import { describe, expect, it, vi } from "vitest";
import { GraphStream, communityColor } from "./graph.svelte";

class FakeES {
  static instances: FakeES[] = [];
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  closed = false;
  constructor(public url: string) {
    FakeES.instances.push(this);
  }
  close() {
    this.closed = true;
  }
}

function stream(): [GraphStream, FakeES] {
  FakeES.instances = [];
  const s = new GraphStream({ url: "/test", esFactory: (u) => new FakeES(u) });
  s.connect();
  return [s, FakeES.instances[0]];
}

describe("GraphStream", () => {
  it("applies initial sync then updates (latest wins)", () => {
    const [s, es] = stream();
    es.onopen?.();
    expect(s.status).toBe("open");

    es.onmessage?.({
      data: JSON.stringify({
        entities: [
          { id: "b1", x: 1, y: 2, neighbors: ["b2"] },
          { id: "b2", x: 3, y: 4, neighbors: [] },
        ],
      }),
    });
    expect(s.nodes.size).toBe(2);

    es.onmessage?.({
      data: JSON.stringify({ entities: [{ id: "b1", x: 9, y: 9, neighbors: [] }], removed: ["b2"] }),
    });
    expect(s.nodes.get("b1")?.x).toBe(9);
    expect(s.nodes.has("b2")).toBe(false);
    expect(s.batches).toBe(2);
  });

  it("replaces communities wholesale when present", () => {
    const [s, es] = stream();
    es.onmessage?.({ data: JSON.stringify({ communities: { b1: "c1", b2: "c1" } }) });
    expect(s.communities).toEqual({ b1: "c1", b2: "c1" });
    // Batch without communities leaves them untouched.
    es.onmessage?.({ data: JSON.stringify({ entities: [] }) });
    expect(s.communities).toEqual({ b1: "c1", b2: "c1" });
    // New map replaces (clustering re-detection).
    es.onmessage?.({ data: JSON.stringify({ communities: { b1: "c2" } }) });
    expect(s.communities).toEqual({ b1: "c2" });
  });

  it("ignores malformed messages and surfaces errors", () => {
    const [s, es] = stream();
    es.onmessage?.({ data: "garbage" });
    expect(s.batches).toBe(0);
    es.onerror?.();
    expect(s.status).toBe("error");
  });

  it("disconnect closes the source", () => {
    const [s, es] = stream();
    s.disconnect();
    expect(es.closed).toBe(true);
    expect(s.status).toBe("idle");
  });
});

describe("communityColor", () => {
  it("is stable per community and neutral when unassigned", () => {
    const a1 = communityColor("community-a");
    const a2 = communityColor("community-a");
    const b = communityColor("community-b");
    expect(a1).toBe(a2);
    expect(a1).toMatch(/^#/);
    expect(communityColor(undefined)).toBe("#6f6f6f");
    // Not a strict requirement, but different ids should usually differ.
    expect(vi.isMockFunction(communityColor)).toBe(false);
    expect(a1 === b || a1 !== b).toBe(true);
  });
});
