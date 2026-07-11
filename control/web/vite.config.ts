import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Build output goes to control/internal/web/dist for go:embed.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});
