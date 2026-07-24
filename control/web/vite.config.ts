import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Build output goes to control/internal/web/dist for go:embed.
export default defineConfig({
  plugins: [react()],
  // Control is the portal shell and is served from the appliance root.
  base: "/",
  build: {
    outDir: "../internal/web/dist",
    // AnythingLLM emits root-relative /assets URLs. Keep the portal shell in a
    // distinct directory so the single-origin ingress can route both safely.
    assetsDir: "portal-assets",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});
