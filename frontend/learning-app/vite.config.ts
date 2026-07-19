import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'

// Isolated Family Learning MVP app: its own entry, dev script, and port, so it
// never disrupts the AgentWorks app. But it deliberately REUSES AgentWorks:
//  - dependencies come from ../node_modules (no second install)
//  - server.fs.allow exposes the AgentWorks frontend so we can import shared
//    components/primitives from ../src/* as the MVP grows.
const agentworksFrontend = fileURLToPath(new URL('..', import.meta.url))

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: 5174,
    strictPort: true,
    fs: { allow: [fileURLToPath(new URL('.', import.meta.url)), agentworksFrontend] },
  },
  resolve: { dedupe: ['react', 'react-dom'] },
})
