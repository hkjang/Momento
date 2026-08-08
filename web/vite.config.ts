import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": "http://localhost:8080",
      "/collect": "http://localhost:8080",
      "/mcp": "http://localhost:8080",
    },
  },
  build: {
    outDir: "dist",
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (
            id.includes("node_modules/echarts") ||
            id.includes("node_modules/zrender")
          )
            return "charts";
          if (id.includes("node_modules")) return "vendor";
        },
      },
    },
  },
});
