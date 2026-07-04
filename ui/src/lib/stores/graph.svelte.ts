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
  close(): void;
};

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
    // EventSource reconnects automatically; surface the state.
    es.onerror = () => {
      this.status = "error";
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

  disconnect(): void {
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
