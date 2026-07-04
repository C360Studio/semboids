<script lang="ts">
  import type { GraphStream } from "$lib/stores/graph.svelte";
  import { communityColor } from "$lib/stores/graph.svelte";

  let { stream }: { stream: GraphStream } = $props();

  let container: HTMLDivElement;
  let hovered = $state<string | null>(null);

  // Sigma renders real world positions — no force layout anywhere. The
  // sync effect rebuilds node/edge attributes per batch and refreshes;
  // sigma (WebGL) handles this rate comfortably (per the sem* survey).
  $effect(() => {
    if (typeof window === "undefined") return;

    type SigmaLike = {
      kill(): void;
      refresh(): void;
      on(ev: string, fn: (e: { node: string }) => void): void;
    };
    let sigma: SigmaLike | null = null;
    let graph: {
      hasNode(n: string): boolean;
      addNode(n: string, attrs: object): void;
      dropNode(n: string): void;
      setNodeAttribute(n: string, k: string, v: unknown): void;
      clearEdges(): void;
      addEdge(a: string, b: string, attrs?: object): void;
      hasEdge(a: string, b: string): boolean;
      forEachNode(fn: (n: string) => void): void;
    } | null = null;
    let cancelled = false;

    void (async () => {
      const [{ default: Graph }, { default: Sigma }] = await Promise.all([
        import("graphology"),
        import("sigma"),
      ]);
      if (cancelled) return;
      graph = new Graph();
      sigma = new Sigma(graph as never, container, {
        labelColor: { color: "#c6c6c6" },
        renderLabels: false,
      }) as unknown as SigmaLike;
      sigma.on("enterNode", (e) => (hovered = e.node));
      sigma.on("leaveNode", () => (hovered = null));
    })();

    // Reactive sync: runs whenever the stream's maps change.
    const sync = $effect.root(() => {
      $effect(() => {
        const nodes = stream.nodes;
        const communities = stream.communities;
        if (!graph || !sigma) return;

        // Plain Set: a per-run local working set, deliberately not reactive.
        // eslint-disable-next-line svelte/prefer-svelte-reactivity
        const seen = new Set<string>();
        for (const [id, n] of nodes) {
          seen.add(id);
          // World y grows downward; sigma y grows upward — flip.
          const attrs = {
            x: n.x,
            y: -n.y,
            size: 3,
            color: communityColor(communities[id]),
          };
          if (graph.hasNode(id)) {
            graph.setNodeAttribute(id, "x", attrs.x);
            graph.setNodeAttribute(id, "y", attrs.y);
            graph.setNodeAttribute(id, "color", attrs.color);
          } else {
            graph.addNode(id, attrs);
          }
        }
        graph.forEachNode((id) => {
          if (!seen.has(id)) graph!.dropNode(id);
        });

        // Edges: rebuild per batch (bounded by population × k).
        graph.clearEdges();
        for (const [id, n] of nodes) {
          for (const other of n.neighbors) {
            if (nodes.has(other) && !graph.hasEdge(id, other) && !graph.hasEdge(other, id)) {
              graph.addEdge(id, other, { size: 0.5, color: "#393939" });
            }
          }
        }
        sigma.refresh();
      });
    });

    return () => {
      cancelled = true;
      sync();
      sigma?.kill();
    };
  });
</script>

<div class="graph-pane">
  <div class="sigma-host" bind:this={container}></div>
  {#if stream.nodes.size === 0}
    <div class="empty">
      <p>Graph view</p>
      <p class="hint">
        {stream.status === "error"
          ? "stream unavailable — retrying"
          : "waiting for graph snapshots…"}
      </p>
    </div>
  {/if}
  {#if hovered}
    <div class="hover-info">
      {hovered.split(".").pop()} · {stream.communities[hovered] ?? "no community"}
    </div>
  {/if}
</div>

<style>
  .graph-pane {
    position: relative;
    width: 100%;
    height: 100%;
    background: var(--ui-surface-primary);
  }

  .sigma-host {
    width: 100%;
    height: 100%;
  }

  .empty {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.25rem;
    color: var(--ui-text-tertiary);
    pointer-events: none;
  }

  .empty p {
    margin: 0;
  }

  .hint {
    font-size: 0.8rem;
  }

  .hover-info {
    position: absolute;
    bottom: 0.5rem;
    left: 0.5rem;
    padding: 0.2rem 0.5rem;
    font-size: 0.75rem;
    color: var(--ui-text-secondary);
    background: var(--ui-surface-secondary);
    border: 1px solid var(--ui-border-subtle);
    border-radius: 4px;
  }
</style>
