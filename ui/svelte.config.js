import adapter from "@sveltejs/adapter-node";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

/** @type {import('@sveltejs/kit').Config} */
const config = {
  preprocess: vitePreprocess(),

  kit: {
    // Build a Node.js server for deployment behind the Caddy reverse proxy,
    // matching the sibling sem* UIs.
    adapter: adapter({
      out: "build",
      precompress: false,
    }),
  },
};

export default config;
