<script lang="ts">
  import FlockCanvas from "$lib/components/FlockCanvas.svelte";
  import GraphCanvas from "$lib/components/GraphCanvas.svelte";
  import { getFlockConnection } from "$lib/stores/flock.svelte";
  import { getGraphStream } from "$lib/stores/graph.svelte";
  import { getRuleGates } from "$lib/stores/rules.svelte";

  const conn = getFlockConnection();
  const gates = getRuleGates();
  const graph = getGraphStream();

  $effect(() => {
    conn.connect();
    graph.connect();
    void gates.load();
    // Singletons shared across the app; leaving them open on unmount keeps
    // data warm for other views.
  });

  const population = $derived(conn.frame?.boids.length ?? 0);

  // Zone types map to the modifier kinds their rules emit.
  const ruleChips = [
    { kind: "flee", label: "Predator", cls: "predator" },
    { kind: "attract", label: "Food", cls: "food" },
    { kind: "wind", label: "Wind", cls: "wind" },
  ];

  // Graph snapshot cadence presets (the load dial).
  const dialOptions = [0.5, 1, 5, 10, 30];
  let dialHz = $state(1);
  let dialError = $state<string | null>(null);

  async function setDial(hz: number) {
    const previous = dialHz;
    dialHz = hz;
    dialError = null;
    try {
      const res = await fetch("/boids/graph/hz", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ hz }),
      });
      if (!res.ok) throw new Error(`dial: ${res.status}`);
    } catch (err) {
      dialHz = previous;
      dialError = err instanceof Error ? err.message : String(err);
    }
  }
</script>

<div class="page">
  <header>
    <h1>SemBoids</h1>
    <div class="rules" role="group" aria-label="Zone rules">
      {#each ruleChips as chip (chip.kind)}
        <button
          class="chip {chip.cls}"
          class:off={gates.states[chip.kind] === false}
          disabled={gates.status !== "ready" && gates.status !== "error"}
          onclick={() => gates.toggle(chip.kind)}
          title="Toggle the {chip.label.toLowerCase()} rule"
        >
          {chip.label}
        </button>
      {/each}
      {#if gates.error}
        <span class="gate-error" title={gates.error}>toggle failed</span>
      {/if}
    </div>
    <div class="stats">
      <span class="status status-{conn.status}">{conn.status}</span>
      <span>{population} boids</span>
      <span>tick {conn.frame?.tick ?? "—"}</span>
    </div>
  </header>

  <main>
    <section class="pane" aria-label="Flock space">
      <FlockCanvas {conn} />
    </section>
    <section class="pane graph" aria-label="Graph view">
      <div class="pane-header">
        <span>substrate graph</span>
        <label>
          dial
          <select
            value={dialHz}
            onchange={(e) => setDial(Number(e.currentTarget.value))}
          >
            {#each dialOptions as hz (hz)}
              <option value={hz}>{hz} Hz</option>
            {/each}
          </select>
        </label>
        {#if dialError}
          <span class="gate-error" title={dialError}>dial failed</span>
        {/if}
      </div>
      <GraphCanvas stream={graph} />
    </section>
  </main>
</div>

<style>
  .page {
    display: flex;
    flex-direction: column;
    height: 100vh;
  }

  header {
    display: flex;
    align-items: center;
    gap: 1.5rem;
    padding: 0.5rem 1rem;
    border-bottom: 1px solid var(--ui-border-subtle);
    background: var(--ui-surface-secondary);
  }

  h1 {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
    letter-spacing: 0.02em;
  }

  .rules {
    display: flex;
    gap: 0.5rem;
    align-items: center;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.15rem 0.6rem;
    border-radius: 999px;
    border: 1px solid var(--ui-border-subtle);
    background: var(--ui-surface-primary);
    color: var(--ui-text-secondary);
    font-size: 0.75rem;
    cursor: pointer;
  }

  .chip::before {
    content: "●";
    font-size: 0.7rem;
  }

  .chip.predator::before {
    color: var(--status-error);
  }

  .chip.food::before {
    color: var(--status-success);
  }

  .chip.wind::before {
    color: var(--status-warning);
  }

  .chip.off {
    opacity: 0.45;
    text-decoration: line-through;
  }

  .chip:disabled {
    cursor: wait;
  }

  .gate-error {
    color: var(--status-error);
    font-size: 0.7rem;
  }

  .stats {
    display: flex;
    gap: 1rem;
    margin-left: auto;
    font-size: 0.8rem;
    color: var(--ui-text-secondary);
    font-variant-numeric: tabular-nums;
  }

  .status::before {
    content: "●";
    margin-right: 0.3rem;
  }

  .status-open::before {
    color: var(--status-success);
  }

  .status-connecting::before,
  .status-reconnecting::before {
    color: var(--status-warning);
  }

  .status-closed::before,
  .status-idle::before {
    color: var(--ui-text-tertiary);
  }

  main {
    display: grid;
    grid-template-columns: 1fr 1fr;
    flex: 1;
    min-height: 0;
  }

  .pane {
    min-width: 0;
    min-height: 0;
    border-right: 1px solid var(--ui-border-subtle);
  }

  .pane:last-child {
    border-right: none;
  }

  .graph {
    display: flex;
    flex-direction: column;
  }

  .pane-header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.25rem 0.75rem;
    font-size: 0.75rem;
    color: var(--ui-text-tertiary);
    border-bottom: 1px solid var(--ui-border-subtle);
    background: var(--ui-surface-secondary);
  }

  .pane-header label {
    display: flex;
    align-items: center;
    gap: 0.35rem;
  }

  .pane-header select {
    background: var(--ui-surface-primary);
    color: var(--ui-text-secondary);
    border: 1px solid var(--ui-border-subtle);
    border-radius: 4px;
    font-size: 0.75rem;
    padding: 0.1rem 0.25rem;
  }

  .graph :global(.graph-pane) {
    flex: 1;
    min-height: 0;
  }
</style>
