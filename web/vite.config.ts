import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // No inline polyfill: the page is served with a strict CSP.
    modulePreload: { polyfill: false },
  },
  server: {
    proxy: { "/api": "http://localhost:8080", "/healthz": "http://localhost:8080" },
  },
});
