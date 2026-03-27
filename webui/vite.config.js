import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import path from "node:path";

const apiTarget = process.env.VITE_API_TARGET || "http://127.0.0.1:48080";
const devPort = Number(process.env.WEBUI_PORT || process.env.VITE_PORT || 48081);

export default defineConfig({
  plugins: [vue()],
  base: "/",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    assetsDir: "assets",
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  server: {
    host: "0.0.0.0",
    port: devPort,
    strictPort: true,
    watch: {
      usePolling: true,
      interval: 300,
    },
    proxy: {
      "/api": apiTarget,
    },
  },
});
