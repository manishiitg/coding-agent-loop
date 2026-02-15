# MCP Agent Builder — Desktop (Phase 1)

Electron shell for the standalone Mac app. Phase 1 implements the server lifecycle: port check → spawn workspace + agent → health check → window → cleanup on quit.

## Prerequisites

- Node 18+
- Built **agent-server** and **workspace-server** binaries in `desktop/resources/` (see `resources/README.md`)

## Run locally (unpackaged)

```bash
cd desktop
npm install
npm start
```

This starts Electron; the main process will check ports 45678 and 45679, spawn both servers from `resources/`, wait for health, then open a window at `http://127.0.0.1:45678`. Closing the window or quitting the app kills both processes.

## Ports

- **45678** — Agent server (API + static frontend when served from agent)
- **45679** — Workspace server

If either port is in use, the app shows an error dialog and exits.

## Scripts

- `npm start` — Run Electron (development)
- `npm run build` — Build unpacked Mac app (requires binaries in `resources/`)
- `npm run dist` — Build DMG/ZIP (requires binaries; signing/notarization not configured in Phase 1)
