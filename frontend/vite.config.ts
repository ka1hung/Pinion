import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import wails from "@wailsio/runtime/plugins/vite";
import { writeFileSync } from "node:fs";

// keep frontend/dist/.gitkeep so `go build` (embed) works before the first frontend build
const keepDist = { name: "keep-dist", closeBundle: () => writeFileSync("dist/.gitkeep", "") };

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  build: {
    // Monaco is bundled locally (no CDN) so the editor works offline on site.
    chunkSizeWarningLimit: 8000,
    target: "es2022",
  },
  worker: { format: "es" },
  plugins: [react(), tailwindcss(), wails("./bindings"), keepDist],
});
