import path from "path"
import { fileURLToPath } from "url"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// ESM-safe __dirname: package.json has "type": "module" so the top-level
// CommonJS __dirname isn't defined when Vite loads this config.
const projectRoot = path.dirname(fileURLToPath(import.meta.url))

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(projectRoot, "./src"),
    },
  },
  server: {
    port: 5173,
    host: '0.0.0.0',
  },
})
