/**
 * Singleton WebSocket connection to the flock frame stream, after the
 * runtimeWebSocket pattern in the sibling sem* UIs: exponential-backoff
 * reconnect, runes state, and latest-wins frame storage — frames arriving
 * faster than rendering overwrite, never queue.
 */
import { parseFrame, type Frame } from "$lib/types/frame";

export type ConnectionStatus = "idle" | "connecting" | "open" | "reconnecting" | "closed";

export interface FlockConnectionOptions {
  url: string;
  /** Injectable for tests; defaults to the browser WebSocket. */
  wsFactory?: (url: string) => WebSocket;
  backoffBaseMs?: number;
  backoffCapMs?: number;
}

export class FlockConnection {
  status = $state<ConnectionStatus>("idle");
  frame = $state<Frame | null>(null);
  framesReceived = $state(0);

  private url: string;
  private wsFactory: (url: string) => WebSocket;
  private backoffBaseMs: number;
  private backoffCapMs: number;

  private ws: WebSocket | null = null;
  private attempts = 0;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private closedByUser = false;

  constructor(opts: FlockConnectionOptions) {
    this.url = opts.url;
    this.wsFactory = opts.wsFactory ?? ((url) => new WebSocket(url));
    this.backoffBaseMs = opts.backoffBaseMs ?? 500;
    this.backoffCapMs = opts.backoffCapMs ?? 30_000;
  }

  connect(): void {
    this.closedByUser = false;
    this.open("connecting");
  }

  disconnect(): void {
    this.closedByUser = true;
    if (this.retryTimer !== null) {
      clearTimeout(this.retryTimer);
      this.retryTimer = null;
    }
    const ws = this.ws;
    this.ws = null;
    ws?.close();
    this.status = "closed";
  }

  private open(pendingStatus: ConnectionStatus): void {
    this.status = pendingStatus;
    const ws = this.wsFactory(this.url);
    this.ws = ws;

    ws.onopen = () => {
      if (ws !== this.ws) return;
      this.attempts = 0;
      this.status = "open";
    };
    ws.onmessage = (ev) => {
      if (ws !== this.ws) return;
      const frame = parseFrame(String(ev.data));
      if (frame === null) return;
      // Latest wins: overwrite, never queue.
      this.frame = frame;
      this.framesReceived += 1;
    };
    ws.onclose = () => {
      if (ws !== this.ws) return;
      this.ws = null;
      if (this.closedByUser) {
        this.status = "closed";
        return;
      }
      this.scheduleReconnect();
    };
    ws.onerror = () => {
      // onclose follows onerror; reconnect is handled there.
    };
  }

  private scheduleReconnect(): void {
    this.status = "reconnecting";
    const delay = Math.min(this.backoffBaseMs * 2 ** this.attempts, this.backoffCapMs);
    this.attempts += 1;
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      if (this.closedByUser) return;
      this.open("reconnecting");
    }, delay);
  }
}

export function createFlockConnection(opts: FlockConnectionOptions): FlockConnection {
  return new FlockConnection(opts);
}

let singleton: FlockConnection | null = null;

/**
 * Browser singleton. The frame stream is served by the semstreams
 * websocket-output on :8081 in dev (Caddy proxies /ws in prod).
 */
export function getFlockConnection(): FlockConnection {
  if (singleton === null) {
    const url =
      typeof window === "undefined"
        ? "ws://localhost:8081/ws"
        : `ws://${window.location.hostname}:8081/ws`;
    singleton = new FlockConnection({ url });
  }
  return singleton;
}
