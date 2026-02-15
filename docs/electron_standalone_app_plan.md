# Electron Standalone Desktop App (Mac) — Plan

A plan for packaging mcp-agent-builder-go as a downloadable Mac app that runs the agent and workspace servers automatically—no browser or manual server startup required.

---

## Overview

Today the app runs in a browser: users start the Go agent server (and optionally the workspace server), then open the frontend. This plan describes a **standalone Electron app** that:

- Bundles the **agent server** and **workspace server** as binaries inside the app.
- **Starts both servers automatically** when the user launches the app.
- Opens a **single window** loading the existing React UI, which talks to localhost.
- **Stops both servers** when the user quits the app.
- Produces a **shareable Mac package** (e.g. `.dmg`) that users can download and run.

Scope: **Mac only** (Apple Silicon primary; Intel/universal optional). Distribution is a single downloadable artifact (e.g. via GitHub Releases).

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     Electron App (user double-clicks)             │
├─────────────────────────────────────────────────────────────────┤
│  Main Process (Node)                                              │
│    1. Check ports 45678 / 45679 availability                      │
│    2. Spawn workspace-server (bundled binary) → port 45679        │
│    3. Spawn agent-server (bundled binary)       → port 45678      │
│    4. Poll /health on both until ready                            │
│    5. Create BrowserWindow → load http://127.0.0.1:45678          │
│    6. On quit: kill both child processes                          │
├─────────────────────────────────────────────────────────────────┤
│  Renderer (existing React frontend)                               │
│    - API base URL: http://127.0.0.1:45678                         │
│    - Workspace API URL: http://127.0.0.1:45679                    │
└─────────────────────────────────────────────────────────────────┘
```

- **Servers run on the user's machine** as child processes of the Electron app.
- **Binaries live inside the app bundle** (e.g. `MyApp.app/Contents/Resources/`).
- **No Docker or extra runtime** required; the app is self-contained.

---

## Implementation Plan

### Phase 1: Electron shell and server lifecycle — ✅ COMPLETED

| Step | Task | Notes |
|------|------|--------|
| 1.1 | Add `desktop/` package with Electron + electron-builder | Done. package.json and main.js initialized. |
| 1.2 | Implement server spawn in main process | Done. Uses obscure ports 45678/45679. |
| 1.3 | Port Availability Check | Done. Uses `detect-port` to prevent conflicts. |
| 1.4 | Health-check and window launch | Done. Polls /health endpoints with timeout. |
| 1.5 | Cleanup on quit | Done. Kills children on quit/close. |

### Phase 2: Go binaries and config — ✅ COMPLETED

| Step | Task | Notes |
|------|------|--------|
| 2.1 | Build agent server for Mac | Done. Compiled for darwin/arm64. |
| 2.2 | Build workspace server for Mac | Done. Compiled for darwin/arm64. |
| 2.3 | Workspace config and data dir | Done. Uses `app.getPath('userData')`. |
| 2.4 | Agent config | Done. Configured to talk to workspace on 45679. |

### Phase 3: Frontend and API URLs — ✅ COMPLETED

| Step | Task | Notes |
|------|------|--------|
| 3.1 | Build frontend for Electron | Done. Built with VITE_API_BASE_URL pointing to 45678/45679. |
| 3.2 | Serve frontend from agent server | Done. Frontend files moved to `agent_go/static/`. |
| 3.3 | (Future) Dynamic port | Deferred as fixed obscure ports are working well. |

### Phase 4: Local Packaging & Verification (No Signing) — 🚧 IN PROGRESS

| Step | Task | Notes |
|------|------|--------|
| 4.1 | electron-builder config | Target `mac` (dmg/zip). Include both Go binaries in `extraResources`. Point app at agent’s static dir. |
| 4.2 | Local Build Script | Created `desktop/dev-setup.sh` to build binaries and setup environment. |
| 4.3 | Verify Standalone App | 🚧 Pending final verification. Issues with hardcoded paths (`/app/...`) fixed by passing writable user paths via flags. |
| 4.4 | Verify Port Conflict Handling | Pending verification. |

### Phase 4.5: Hardening & Polishing — 🆕 ADDED

| Step | Task | Notes |
|------|------|--------|
| 4.5.1 | Fix hardcoded paths | ensure agent/workspace accept flags for all paths (DB, logs, docs) instead of assuming `/app/`. Done for workspace/root.go. |
| 4.5.2 | Verify read-only filesystem compatibility | Ensure no writes attempt to occur in binary location (use `userData`). |
| 4.5.3 | Bundle static assets | Ensure `agent_go/static` and `agent_go/configs` are correctly bundled in the app. |

### Phase 5: Production Release (Mac)

| Step | Task | Notes |
|------|------|--------|
| 5.1 | Code Signing | Update electron-builder config with Apple Developer ID (Developer ID Application). |
| 5.2 | Notarization | Configure notarization (xcrun notarytool) with Apple ID credentials. Required for distribution. |
| 5.3 | CI/CD Pipeline | Automate the build/sign/notarize flow (e.g. GitHub Actions) to produce the final `.dmg`. |
| 5.4 | Distribution | Upload the signed/notarized artifact to GitHub Releases. |

### Phase 6: Documentation and UX

| Step | Task | Notes |
|------|------|--------|
| 6.1 | README / install instructions | How to download, open the .dmg, drag to Applications, first launch (Gatekeeper), and that no Docker or terminal is needed. |
| 6.2 | In-app messaging | Polished error dialogs for startup failures (e.g. port conflicts). |
| 6.3 | CDP Connectivity Helper | Add a UI status/button to help users connect to their local Chrome via `--remote-debugging-port=9222`. Ensure the app can detect if Chrome is ready for automation. |
| 6.4 | Logs and debugging | Optionally write agent/workspace stdout/stderr to a log file under `userData` so support can ask for logs. |

---

## Directory / artifact layout (conceptual)

```
mcp-agent-builder-go/
  desktop/                    # New: Electron app
    package.json              # electron, electron-builder, main script
    main.js                   # Main process: spawn servers, health check, window, cleanup
    preload.js                # Optional: expose API base URL to renderer
    resources/                # Dev: place Go binaries here for local run
  agent_go/
    cmd/server/               # Agent server (build → agent-server for Mac)
    static/                   # Built frontend (Vite build output)
  workspace/                  # Workspace server (build → workspace-server for Mac)
  frontend/                   # Build with VITE_API_BASE_URL etc. for Electron
```

Packaged app (simplified):

```
MyApp.app/
  Contents/
    MacOS/Electron
    Resources/
      app.asar (or unpacked)   # Electron app code
      agent-server            # Go binary
      workspace-server        # Go binary
```

---

## Alternatives considered

- **Docker**: Run agent and workspace via `docker compose` from the Electron app. Possible, but requires users to have Docker installed; not chosen for “download and run” simplicity.
- **Hosted backend**: No local servers; app talks to cloud API. Not in scope for this plan; would be a different product.
- **Windows / Linux**: Out of scope for this plan; can be added later using the same pattern (build Go for each OS, electron-builder targets).

---

## Success criteria

- User downloads a single Mac artifact (e.g. `.dmg`) from a release page.
- User opens the app; agent and workspace start automatically; window shows the existing UI.
- No browser, Docker, or manual server startup required.
- User quits the app; both servers stop; no lingering processes.
- (With signing/notarization) App opens without Gatekeeper blocking it.

---

## References

- Current browser-based setup: Go server serves frontend from `./static/`; frontend uses `VITE_API_BASE_URL` and `VITE_WORKSPACE_API_URL` (see `frontend/src/services/api.ts`).
- Workspace server: `workspace/`, typically port 8081; docker-compose in repo root.
- Agent server: `agent_go/cmd/server/server.go`; static files from `./static/`.
