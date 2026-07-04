import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    port: 5173,
    host: true,
    // No vite proxy: API/WS routing is handled by the Caddy gateway,
    // matching the sibling sem* UIs.
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
