<script lang="ts">
  import type { FlockConnection } from "$lib/stores/flock.svelte";

  let { conn }: { conn: FlockConnection } = $props();

  let container: HTMLDivElement;
  let canvas: HTMLCanvasElement;

  // The rAF loop reads conn.frame imperatively each animation frame —
  // latest wins, no reactive re-subscription per frame. Backgrounded tabs
  // pause rAF; on foreground the loop resumes from the current frame.
  $effect(() => {
    const ctx = canvas.getContext("2d");
    if (ctx === null) return;

    const styles = getComputedStyle(canvas);
    const boidColor = styles.getPropertyValue("--ui-interactive-primary").trim() || "#78a9ff";
    const bgColor = styles.getPropertyValue("--ui-surface-primary").trim() || "#161616";

    let dpr = window.devicePixelRatio || 1;

    const resize = () => {
      dpr = window.devicePixelRatio || 1;
      canvas.width = Math.max(1, Math.round(container.clientWidth * dpr));
      canvas.height = Math.max(1, Math.round(container.clientHeight * dpr));
    };
    resize();
    const observer = new ResizeObserver(resize);
    observer.observe(container);

    let rafId = 0;
    const draw = () => {
      rafId = requestAnimationFrame(draw);
      const frame = conn.frame;

      ctx.fillStyle = bgColor;
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      if (frame === null) return;

      // Letterbox the world into the canvas, preserving aspect.
      const scale = Math.min(canvas.width / frame.w, canvas.height / frame.h);
      const offsetX = (canvas.width - frame.w * scale) / 2;
      const offsetY = (canvas.height - frame.h * scale) / 2;

      const size = 5 * dpr; // triangle half-length in device px
      ctx.fillStyle = boidColor;
      for (const [, x, y, vx, vy] of frame.boids) {
        const px = offsetX + x * scale;
        const py = offsetY + y * scale;
        const angle = Math.atan2(vy, vx);
        ctx.save();
        ctx.translate(px, py);
        ctx.rotate(angle);
        ctx.beginPath();
        ctx.moveTo(size, 0);
        ctx.lineTo(-size * 0.6, size * 0.5);
        ctx.lineTo(-size * 0.6, -size * 0.5);
        ctx.closePath();
        ctx.fill();
        ctx.restore();
      }
    };
    draw();

    return () => {
      cancelAnimationFrame(rafId);
      observer.disconnect();
    };
  });
</script>

<div class="flock-canvas" bind:this={container}>
  <canvas bind:this={canvas}></canvas>
</div>

<style>
  .flock-canvas {
    position: relative;
    width: 100%;
    height: 100%;
    overflow: hidden;
    background: var(--ui-surface-primary);
  }

  canvas {
    display: block;
    width: 100%;
    height: 100%;
  }
</style>
