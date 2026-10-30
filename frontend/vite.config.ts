import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// The admin SPA is embedded in the Go binary and served at /admin/. Keep the
// base path and the embed output directory in sync with the backend.
export default defineConfig({
  base: "/admin/",
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: {
    outDir: "../pkg/static/dist",
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 900,
    rollupOptions: {
      input: {
        index: fileURLToPath(new URL("./admin.html", import.meta.url)),
      },
    },
  },
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
  server: {
    port: 5173,
    strictPort: false,
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: true },
      "/media": { target: "http://localhost:8080", changeOrigin: true },
      "/health": { target: "http://localhost:8080", changeOrigin: true },
    },
  },
});
