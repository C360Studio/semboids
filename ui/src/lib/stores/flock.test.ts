import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createFlockConnection, type FlockConnection } from "./flock.svelte";

/** Minimal fake WebSocket the store drives via the injected factory. */
class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  closed = false;

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  close() {
    this.closed = true;
    this.onclose?.();
  }

  // Test drivers
  serverOpen() {
    this.onopen?.();
  }
  serverSend(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) });
  }
  serverDrop() {
    this.onclose?.();
  }
}

function frameMsg(tick: number) {
  return {
    type: "data",
    payload: { tick, t: tick * 33, w: 1600, h: 900, boids: [[0, 1, 2, 3, 4]] },
  };
}

describe("flock connection store", () => {
  let conn: FlockConnection;

  beforeEach(() => {
    vi.useFakeTimers();
    FakeWebSocket.instances = [];
    conn = createFlockConnection({
      url: "ws://test/ws",
      wsFactory: (url) => new FakeWebSocket(url) as unknown as WebSocket,
      backoffBaseMs: 100,
      backoffCapMs: 3000,
    });
  });

  afterEach(() => {
    conn.disconnect();
    vi.useRealTimers();
  });

  it("transitions connecting -> open", () => {
    conn.connect();
    expect(conn.status).toBe("connecting");
    FakeWebSocket.instances[0].serverOpen();
    expect(conn.status).toBe("open");
  });

  it("keeps only the latest frame (latest wins, no queue)", () => {
    conn.connect();
    const ws = FakeWebSocket.instances[0];
    ws.serverOpen();
    ws.serverSend(frameMsg(1));
    ws.serverSend(frameMsg(2));
    ws.serverSend(frameMsg(3));
    expect(conn.frame?.tick).toBe(3);
    expect(conn.framesReceived).toBe(3);
  });

  it("ignores malformed messages without dropping the connection", () => {
    conn.connect();
    const ws = FakeWebSocket.instances[0];
    ws.serverOpen();
    ws.serverSend(frameMsg(1));
    ws.onmessage?.({ data: "garbage" });
    ws.serverSend({ type: "status", payload: {} });
    expect(conn.status).toBe("open");
    expect(conn.frame?.tick).toBe(1);
    expect(conn.framesReceived).toBe(1);
  });

  it("reconnects with exponential backoff after a drop", () => {
    conn.connect();
    const first = FakeWebSocket.instances[0];
    first.serverOpen();
    first.serverDrop();
    expect(conn.status).toBe("reconnecting");

    // First retry after backoffBaseMs.
    vi.advanceTimersByTime(100);
    expect(FakeWebSocket.instances).toHaveLength(2);
    FakeWebSocket.instances[1].serverDrop();

    // Second retry doubles.
    vi.advanceTimersByTime(100);
    expect(FakeWebSocket.instances).toHaveLength(2);
    vi.advanceTimersByTime(100);
    expect(FakeWebSocket.instances).toHaveLength(3);

    // Successful open resets backoff and status.
    FakeWebSocket.instances[2].serverOpen();
    expect(conn.status).toBe("open");
    FakeWebSocket.instances[2].serverDrop();
    vi.advanceTimersByTime(100);
    expect(FakeWebSocket.instances).toHaveLength(4);
  });

  it("resumes frames after reconnect", () => {
    conn.connect();
    const first = FakeWebSocket.instances[0];
    first.serverOpen();
    first.serverSend(frameMsg(5));
    first.serverDrop();
    vi.advanceTimersByTime(100);
    const second = FakeWebSocket.instances[1];
    second.serverOpen();
    second.serverSend(frameMsg(9));
    expect(conn.frame?.tick).toBe(9);
  });

  it("disconnect closes the socket and stops reconnecting", () => {
    conn.connect();
    const ws = FakeWebSocket.instances[0];
    ws.serverOpen();
    conn.disconnect();
    expect(ws.closed).toBe(true);
    expect(conn.status).toBe("closed");
    vi.advanceTimersByTime(60_000);
    expect(FakeWebSocket.instances).toHaveLength(1);
  });
});
