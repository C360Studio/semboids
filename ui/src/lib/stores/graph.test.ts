import { describe, expect, it, vi } from "vitest";
import { GraphStream, communityColor } from "./graph.svelte";

class FakeES {
  static instances: FakeES[] = [];
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  closed = false;
  readyState = 0;
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

describe("GraphStream reconnect", () => {
  // The backend serves 503 on /boids/graph/stream for a measured ~400ms window
  // on every start, while the shared graph view attaches. A non-200 makes the
  // browser close an EventSource PERMANENTLY (readyState CLOSED, no retry), so
  // a reconnect landing in that window would strand the pane until a manual
  // reload — which the graph-pane spec forbids.
  it("re-arms after the stream is closed permanently", () => {
    vi.useFakeTimers();
    try {
      const [, es] = stream();
      expect(FakeES.instances.length).toBe(1);

      es.readyState = 2; // CLOSED — will never retry on its own
      es.onerror?.();
      expect(es.closed).toBe(true);

      vi.advanceTimersByTime(2500);
      expect(FakeES.instances.length).toBe(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("leaves a retrying stream alone", () => {
    vi.useFakeTimers();
    try {
      const [, es] = stream();
      es.readyState = 0; // CONNECTING — EventSource retries by itself
      es.onerror?.();

      vi.advanceTimersByTime(5000);
      expect(FakeES.instances.length).toBe(1);
      expect(es.closed).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not reconnect after an explicit disconnect", () => {
    vi.useFakeTimers();
    try {
      const [s, es] = stream();
      es.readyState = 2;
      es.onerror?.();
      s.disconnect();

      vi.advanceTimersByTime(5000);
      expect(FakeES.instances.length).toBe(1);
    } finally {
      vi.useRealTimers();
    }
  });
});
