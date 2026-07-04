<script lang="ts">
  import type { FlockConnection } from "$lib/stores/flock.svelte";
  import { MOD_FLEE, MOD_ATTRACT, MOD_WIND } from "$lib/types/frame";

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
    const cssVar = (name: string, fallback: string) =>
      styles.getPropertyValue(name).trim() || fallback;

    const boidColor = cssVar("--ui-interactive-primary", "#78a9ff");
    const bgColor = cssVar("--ui-surface-primary", "#161616");
    // Kind colors: shared by zone circles and modifier-tinted boids so the
    // cause→effect reads directly on the canvas.
    const kindColors: Record<string, string> = {
      predator: cssVar("--status-error", "#fa4d56"),
      food: cssVar("--status-success", "#42be65"),
      wind: cssVar("--status-warning", "#f1c21b"),
    };
    const modColors: Record<number, string> = {
      [MOD_FLEE]: kindColors.predator,
      [MOD_ATTRACT]: kindColors.food,
      [MOD_WIND]: kindColors.wind,
    };

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

      // Zones beneath the boids: translucent fill + stroke in kind color.
      for (const [type, zx, zy, zr] of frame.zones ?? []) {
        const color = kindColors[type] ?? boidColor;
        const px = offsetX + zx * scale;
        const py = offsetY + zy * scale;
        ctx.beginPath();
        ctx.arc(px, py, zr * scale, 0, Math.PI * 2);
        ctx.globalAlpha = 0.12;
        ctx.fillStyle = color;
        ctx.fill();
        ctx.globalAlpha = 0.5;
        ctx.strokeStyle = color;
        ctx.lineWidth = 1.5 * dpr;
        ctx.stroke();
        ctx.globalAlpha = 1;
      }

      const size = 5 * dpr; // triangle half-length in device px
      for (const b of frame.boids) {
        const [, x, y, vx, vy] = b;
        const m = b[5] ?? 0;
        const px = offsetX + x * scale;
        const py = offsetY + y * scale;
        const angle = Math.atan2(vy, vx);
        ctx.fillStyle = modColors[m] ?? boidColor;
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
