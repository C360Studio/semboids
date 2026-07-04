import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    port: 5173,
    host: true,
    // Prod routing goes through the Caddy gateway (ui/Caddyfile). The lone
    // dev proxy below exists because `task dev` runs vite + backend without
    // Caddy, and the rules API needs a same-origin path (CORS).
    proxy: {
      "/boids": "http://localhost:8080",
    },
  },
  test: {
    include: ["src/**/*.{test,spec}.{js,ts}"],
    environment: "jsdom",
    globals: true,
  },
  resolve: {
    conditions: ["browser"],
  },
});
