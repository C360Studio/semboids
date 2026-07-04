<script lang="ts">
  import FlockCanvas from "$lib/components/FlockCanvas.svelte";
  import { getFlockConnection } from "$lib/stores/flock.svelte";
  import { getRuleGates } from "$lib/stores/rules.svelte";

  const conn = getFlockConnection();
  const gates = getRuleGates();

  $effect(() => {
    conn.connect();
    void gates.load();
    // The connection is a singleton shared across the app; leaving it open
    // on unmount keeps frames warm for other views.
  });

  const population = $derived(conn.frame?.boids.length ?? 0);

  // Zone types map to the modifier kinds their rules emit.
  const ruleChips = [
    { kind: "flee", label: "Predator", cls: "predator" },
    { kind: "attract", label: "Food", cls: "food" },
    { kind: "wind", label: "Wind", cls: "wind" },
  ];
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
    <section class="pane placeholder" aria-label="Graph view">
      <p>Graph view</p>
      <p class="hint">Neighbor topology &amp; flock communities — coming in a later change.</p>
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

  .placeholder {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.25rem;
    color: var(--ui-text-tertiary);
    background: var(--ui-surface-primary);
  }

  .placeholder p {
    margin: 0;
  }

  .hint {
    font-size: 0.8rem;
  }
</style>
