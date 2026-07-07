<script lang="ts">
  // A help dialog explaining the two panes and the live controls. Presentation
  // only — no data or app state. Closes on the × button, a backdrop click, or
  // Escape; restores focus to the trigger on close.
  interface Props {
    open: boolean;
    onClose: () => void;
  }
  let { open, onClose }: Props = $props();

  let dialog = $state<HTMLDivElement | null>(null);

  $effect(() => {
    if (!open) return;
    const previouslyFocused = document.activeElement as HTMLElement | null;
    dialog?.focus();
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      previouslyFocused?.focus?.();
    };
  });
</script>

{#if open}
  <!-- Backdrop click closes; keyboard close is handled by Escape (above) and
       the × button, so the static-element click warning is safe to ignore. -->
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="backdrop" onclick={onClose}>
    <div
      class="modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="help-title"
      tabindex="-1"
      bind:this={dialog}
      onclick={(e) => e.stopPropagation()}
    >
      <header class="head">
        <h2 id="help-title">What am I looking at?</h2>
        <button class="close" onclick={onClose} aria-label="Close help">×</button>
      </header>

      <div class="body">
        <p class="lede">
          A classic <strong>Reynolds boids</strong> flock — three local steering
          rules (separation, cohesion, alignment) producing emergent flocking —
          running live as a load generator on the <strong>SemStreams</strong>
          substrate.
        </p>

        <h3>The two panes</h3>
        <dl>
          <dt>Flock space <span class="side">left</span></dt>
          <dd>
            The physics simulation: boids steer by the three local rules at 30Hz.
            A boid tints when a zone rule is actively pushing it.
          </dd>
          <dt>Substrate graph <span class="side">right</span></dt>
          <dd>
            The same boids as entities in SemStreams' graph, drawn at their real
            positions. Node colors are flock communities the substrate detects
            live (label-propagation clustering).
          </dd>
        </dl>

        <h3>The controls</h3>
        <dl>
          <dt><span class="dot predator"></span>Predator / Food / Wind</dt>
          <dd>
            Toggle the zone-steering rules in real time — boids flee predators,
            pool at food, and drift on wind. Each is a SemStreams rule flipped
            live; watch the flock react.
          </dd>
          <dt>dial <span class="side">graph pane</span></dt>
          <dd>
            How often (Hz) the flock is snapshotted into the graph. This is the
            <em>load dial</em>: crank it to stress the graph-ingest pipeline and
            watch the substrate keep up (or not).
          </dd>
          <dt>+ spawn</dt>
          <dd>
            Births a wave of boids through the lifecycle system — a second,
            create/delete load axis distinct from the snapshot dial.
          </dd>
          <dt>status · count · tick</dt>
          <dd>
            The header readouts: the frame-stream connection state, the live
            boid population, and the current simulation tick.
          </dd>
        </dl>
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: var(--modal-backdrop);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
    z-index: 100;
  }

  .modal {
    background: var(--modal-background);
    border: 1px solid var(--modal-border);
    border-radius: var(--modal-border-radius);
    box-shadow: var(--modal-shadow);
    max-width: 34rem;
    width: 100%;
    max-height: 85vh;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .modal:focus {
    outline: none;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.85rem 1.1rem;
    border-bottom: 1px solid var(--ui-border-subtle);
  }

  .head h2 {
    margin: 0;
    font-size: 0.95rem;
    font-weight: 600;
    color: var(--ui-text-primary);
  }

  .close {
    border: none;
    background: transparent;
    color: var(--ui-text-tertiary);
    font-size: 1.4rem;
    line-height: 1;
    cursor: pointer;
    padding: 0 0.25rem;
    border-radius: 4px;
  }

  .close:hover {
    color: var(--ui-text-primary);
  }

  .close:focus-visible {
    outline: 2px solid var(--ui-focus-ring);
    outline-offset: 1px;
  }

  .body {
    padding: 1.1rem;
    overflow-y: auto;
    color: var(--ui-text-secondary);
    font-size: 0.85rem;
    line-height: 1.5;
  }

  .lede {
    margin: 0 0 1rem;
  }

  .body strong {
    color: var(--ui-text-primary);
    font-weight: 600;
  }

  .body h3 {
    margin: 1.2rem 0 0.5rem;
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--ui-text-tertiary);
  }

  .body h3:first-of-type {
    margin-top: 0;
  }

  dl {
    margin: 0;
  }

  dt {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    font-weight: 600;
    color: var(--ui-text-primary);
    margin-top: 0.7rem;
  }

  dd {
    margin: 0.15rem 0 0;
  }

  .side {
    font-weight: 400;
    font-size: 0.7rem;
    color: var(--ui-text-tertiary);
    border: 1px solid var(--ui-border-subtle);
    border-radius: 4px;
    padding: 0.05rem 0.35rem;
  }

  .dot {
    width: 0.55rem;
    height: 0.55rem;
    border-radius: 50%;
    background: linear-gradient(
      90deg,
      var(--status-error) 0 33%,
      var(--status-success) 33% 66%,
      var(--status-warning) 66% 100%
    );
    flex: none;
  }
</style>
