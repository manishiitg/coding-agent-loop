# Runloop — Plan

A plan for packaging coding-agent-loop as a downloadable Mac app that runs the agent and workspace servers automatically—no browser or manual server startup required.

---

## Overview

Today the app runs in a browser: users start the Go agent server (and optionally the workspace server), then open the frontend. This plan describes a **standalone Electron app** for Runloop that:

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

### Phase 4: Local Packaging & Verification (No Signing) — ✅ COMPLETED

| Step | Task | Notes |
|------|------|--------|
| 4.1 | electron-builder config | Target `mac` (dmg/zip). Include both Go binaries in `extraResources`. Point app at agent’s static dir. |
| 4.2 | Local Build Script | Created `desktop/dev-setup.sh` to build binaries and setup environment. |
| 4.3 | Verify Standalone App | ✅ Verified. Fixed hardcoded paths in workspace and agent servers. |
| 4.4 | Verify Port Conflict Handling | Verified logic in `desktop/main.js`. |

### Phase 4.5: Hardening & Polishing — ✅ COMPLETED

| Step | Task | Notes |
|------|------|--------|
| 4.5.1 | Fix hardcoded paths | ensure agent/workspace accept flags for all paths (DB, logs, docs, data, mcp-config). Done for workspace/root.go (data-dir) and agent. |
| 4.5.2 | Verify read-only filesystem compatibility | ✅ Done. Agent/Workspace now write to `userData` (db, logs, data). Default config is copied to `userData`. |
| 4.5.3 | Bundle static assets | ✅ Done via `extraResources` in `package.json`. |

### Phase 5: Production Release (Mac) — ✅ COMPLETED

| Step | Task | Notes |
|------|------|--------|
| 5.1 | Code Signing | **Skipped** for local build. Requires Apple Developer ID identity. `npm run dist` produces a functional but unsigned .dmg. |
| 5.2 | Notarization | **Skipped** (requires signing). Users will need to right-click -> Open to bypass Gatekeeper on first launch. |
| 5.3 | CI/CD Pipeline | **✅ Done.** GitHub Action `.github/workflows/desktop-release.yml` created and verified. Automatically builds DMG/ZIP on tag push. |
| 5.4 | Distribution | Artifacts created automatically in GitHub Releases. Version synced from git tag. |

---

## Current Blockers & Active Tasks (Feb 16, 2026)

### 1. v1.21.0 Release — ✅ COMPLETED
*   **Status:** Released.
*   **Notes:** Dynamic ports implemented. Frontend sync features refined. Binaries built and packaged.

---


### Phase 6: Documentation and UX — ✅ COMPLETED (Initial)

| Step | Task | Notes |
|------|------|--------|
| 6.1 | README / install instructions | **Done.** Updated `desktop/README.md` with build and run instructions. |
| 6.2 | In-app messaging | Polished error dialogs for startup failures (e.g. port conflicts). |
| 6.3 | CDP Connectivity Helper | Add a UI status/button to help users connect to their local Chrome via `--remote-debugging-port=9222`. |
| 6.4 | Logs and debugging | **Done.** Updated `main.js` to pipe stdout/stderr from agent/workspace processes to `userData/logs/agent.log` and `userData/logs/workspace.log`. |

### Phase 7: Production Hardening (In Progress)

These steps are recommended for a commercial or wide public release to ensure robustness and security.

| Step | Task | Details |
|------|------|--------|
| 7.1 | Dynamic Port Allocation | **✅ Done.** Implemented in v1.21.0. Servers bind to port 0 (dynamic), Electron parses the assigned port from stdout, and injects it into the frontend via IPC. No more port conflicts or hardcoded ports. |
| 7.2 | IPC Authentication | **Critical for security.** Generate a random session token in Electron on startup. Pass it to Go processes as an env var. Require this token in an `Authorization` header for all API requests to prevent unauthorized access from other local software/scripts. |
| 7.3 | Zombie Process Prevention | **Reliability.** Implement a "parent heartbeat" or PID monitoring in the Go servers. If the Electron parent process dies unexpectedly (crash/force quit), the Go servers should automatically shut down to prevent orphaned background processes. |
| 7.4 | Auto-Update | Integrate `electron-updater` to pull new releases from GitHub. |

---

## Project Status Summary (Feb 16, 2026)

The standalone Electron application has been successfully packaged and released as **v1.21.0**.

### Artifacts Generated
- **DMG:** `Runloop-1.21.0-arm64.dmg`
- **ZIP:** `Runloop-1.21.0-arm64-mac.zip`

### Key Achievements
- **Dynamic Port Allocation:** Servers now bind to random free ports provided by the OS, eliminating port conflicts with other running instances or services.
- **Zero-Dependency Startup:** Spawns `agent-server` and `workspace-server` sidecar binaries automatically on launch.
- **Filesystem Persistence:** Configured Go servers to use `app.getPath('userData')` for databases, logs, and search indices.
- **Integrated Frontend:** Frontend is served directly via the `agent-server`.
- **Reliable Dev Mode:** `npm start` (with `DEV_URL`) correctly connects to external backends on standard ports (8000/8081) while packaged app uses internal dynamic ports.

### Next Steps (Post-v1.21.0)
- **Code Signing:** To distribute outside of local environments, the build process needs an Apple Developer ID.
- **Universal Build:** Configure `electron-builder` to produce universal binaries (arm64 + x64) for broader Mac compatibility.
- **Security Hardening:** Implement Phase 7.2 (IPC Authentication) to secure the local API.

---


## Directory / artifact layout (conceptual)

```
coding-agent-loop/
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
