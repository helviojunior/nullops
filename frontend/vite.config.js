import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

const src = (p) => path.resolve(__dirname, "src", p);

// Bare imports like "components/ui/button" (same convention as the
// sec_base_system jsconfig baseUrl=src).
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      components: src("components"),
      contexts: src("contexts"),
      lib: src("lib"),
      pages: src("pages"),
      i18n: src("i18n"),
    },
  },
  build: {
    outDir: "dist",
    assetsDir: "assets",
    sourcemap: false,
    emptyOutDir: true,
    chunkSizeWarningLimit: 1024,
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.js"],
  },
  server: {
    // `npm run dev` proxies the API to a running `fmweb web --no-tls`.
    proxy: { "/api": "http://127.0.0.1:8181" },
  },
});
