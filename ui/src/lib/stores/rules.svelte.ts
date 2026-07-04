/**
 * Rule-gate toggle store backed by the boids API (/boids/rules). Toggles
 * flip optimistically and revert if the backend rejects or is unreachable.
 *
 * Interim mechanism note: the backend gates modifier kinds app-side while
 * rule-engine hot reload is unreachable upstream
 * (https://github.com/C360Studio/semstreams/issues/455); these endpoints
 * keep the same shape when that lands.
 */

type FetchFn = (input: string, init?: RequestInit) => Promise<Response>;

export type GateStatus = "idle" | "loading" | "ready" | "error";

export class RuleGates {
  states = $state<Record<string, boolean>>({});
  status = $state<GateStatus>("idle");
  error = $state<string | null>(null);

  private fetchFn: FetchFn;

  constructor(fetchFn?: FetchFn) {
    this.fetchFn = fetchFn ?? ((input, init) => fetch(input, init));
  }

  async load(): Promise<void> {
    this.status = "loading";
    try {
      const res = await this.fetchFn("/boids/rules");
      if (!res.ok) throw new Error(`GET /boids/rules: ${res.status}`);
      this.states = (await res.json()) as Record<string, boolean>;
      this.status = "ready";
      this.error = null;
    } catch (err) {
      this.status = "error";
      this.error = err instanceof Error ? err.message : String(err);
    }
  }

  async toggle(kind: string): Promise<void> {
    const previous = this.states[kind];
    const next = !previous;
    // Optimistic flip; revert on any failure.
    this.states = { ...this.states, [kind]: next };
    this.error = null;
    try {
      const res = await this.fetchFn(`/boids/rules/${kind}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled: next }),
      });
      if (!res.ok) throw new Error(`toggle ${kind}: ${res.status}`);
    } catch (err) {
      this.states = { ...this.states, [kind]: previous };
      this.error = err instanceof Error ? err.message : String(err);
    }
  }
}

let singleton: RuleGates | null = null;

/** Browser singleton. */
export function getRuleGates(): RuleGates {
  if (singleton === null) {
    singleton = new RuleGates();
  }
  return singleton;
}
