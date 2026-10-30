import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Stable public runtime bundle embedded in the engine. Feature artifacts are
// fetched from the engine's immutable asset endpoint and share this React.
export default defineConfig({
  base: "/",
  plugins: [react()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: {
    outDir: "../pkg/runtime/dist",
    emptyOutDir: true,
    rollupOptions: { input: { index: fileURLToPath(new URL("./index.html", import.meta.url)) } },
    sourcemap: false,
  },
});
