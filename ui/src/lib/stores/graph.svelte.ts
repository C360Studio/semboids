/**
 * Graph pane store: consumes the boids API's SSE stream
 * (/boids/graph/stream) of coalesced graph batches — the substrate's view
 * of the flock (deliberately not the frame stream, so dial-induced lag is
 * visible). EventSource pattern after semdragons' sse service.
 */

export interface GraphBatch {
  entities?: { id: string; x: number; y: number; neighbors: string[] }[];
  removed?: string[];
  communities?: Record<string, string>;
}

export interface GraphNode {
  id: string;
  x: number;
  y: number;
  neighbors: string[];
}

export type GraphStreamStatus = "idle" | "connecting" | "open" | "error";

import { SvelteMap } from "svelte/reactivity";

type EventSourceLike = {
  onopen: (() => void) | null;
  onerror: (() => void) | null;
  onmessage: ((ev: { data: string }) => void) | null;
  readyState?: number;
  close(): void;
};

// EventSource.CLOSED. A connection in this state will never retry on its own.
const ES_CLOSED = 2;

// Backoff before re-arming a permanently-closed stream. Long enough not to
// hammer a backend that is still starting, short enough that the pane comes
// back on its own while someone is watching it.
const RECONNECT_DELAY_MS = 2000;

export interface GraphStreamOptions {
  url?: string;
  esFactory?: (url: string) => EventSourceLike;
}

export class GraphStream {
  status = $state<GraphStreamStatus>("idle");
  nodes = new SvelteMap<string, GraphNode>();
  communities = $state<Record<string, string>>({});
  batches = $state(0);

  private url: string;
  private esFactory: (url: string) => EventSourceLike;
  private es: EventSourceLike | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(opts: GraphStreamOptions = {}) {
    this.url = opts.url ?? "/boids/graph/stream";
    this.esFactory = opts.esFactory ?? ((url) => new EventSource(url) as EventSourceLike);
  }

  connect(): void {
    if (this.es !== null) return;
    this.status = "connecting";
    const es = this.esFactory(this.url);
    this.es = es;
    es.onopen = () => {
      this.status = "open";
    };
    es.onerror = () => {
      this.status = "error";
      // EventSource only auto-reconnects on NETWORK-level failures. When the
      // server answers with a non-200 — a proxy 502 while the backend
      // restarts, or our own 503 while the shared graph view is still
      // attaching — the browser closes the connection permanently
      // (readyState CLOSED, no retry) and the pane would stay blank until a
      // manual reload. Re-arm ourselves so recovery needs no user action,
      // which the graph-pane spec requires.
      if (es.readyState === ES_CLOSED) {
        this.scheduleReconnect();
      }
    };
    es.onmessage = (ev) => {
      let batch: GraphBatch;
      try {
        batch = JSON.parse(ev.data) as GraphBatch;
      } catch {
        return;
      }
      this.apply(batch);
    };
  }

  // scheduleReconnect tears the dead stream down and re-arms after a backoff.
  // Guarded so overlapping error events cannot stack timers.
  private scheduleReconnect(): void {
    if (this.reconnectTimer !== null) return;
    this.es?.close();
    this.es = null;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, RECONNECT_DELAY_MS);
  }

  disconnect(): void {
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.es?.close();
    this.es = null;
    this.status = "idle";
  }

  /** apply merges one batch into the reactive maps (exported for tests). */
  apply(batch: GraphBatch): void {
    for (const e of batch.entities ?? []) {
      this.nodes.set(e.id, { id: e.id, x: e.x, y: e.y, neighbors: e.neighbors ?? [] });
    }
    for (const id of batch.removed ?? []) {
      this.nodes.delete(id);
    }
    if (batch.communities !== undefined) {
      this.communities = batch.communities;
    }
    this.batches += 1;
  }
}

let singleton: GraphStream | null = null;

/** Browser singleton. */
export function getGraphStream(): GraphStream {
  if (singleton === null) {
    singleton = new GraphStream();
  }
  return singleton;
}

/**
 * communityColor maps a community id to a stable categorical color.
 * Unassigned entities get the neutral color.
 */
const PALETTE = ["#a56eff", "#08bdba", "#ff7eb6", "#4589ff", "#f1c21b", "#42be65", "#fa4d56", "#d12771"];

export function communityColor(communityID: string | undefined): string {
  if (!communityID) return "#6f6f6f"; // neutral until assigned
  let hash = 0;
  for (let i = 0; i < communityID.length; i++) {
    hash = (hash * 31 + communityID.charCodeAt(i)) | 0;
  }
  return PALETTE[Math.abs(hash) % PALETTE.length];
}
