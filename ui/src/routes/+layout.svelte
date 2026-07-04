<script lang="ts">
  import "../styles/colors.css";

  let { children } = $props();
</script>

{@render children()}

<style>
  :global(html, body) {
    margin: 0;
    padding: 0;
    height: 100%;
    background: var(--ui-surface-primary);
    color: var(--ui-text-primary);
    font-family:
      "IBM Plex Sans",
      -apple-system,
      BlinkMacSystemFont,
      "Segoe UI",
      sans-serif;
  }
</style>
