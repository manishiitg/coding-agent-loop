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

### Phase 5: Production Release (Mac) — ✅ COMPLETED (Local Packaging)

| Step | Task | Notes |
|------|------|--------|
| 5.1 | Code Signing | **Skipped** for local build. Requires Apple Developer ID identity. `npm run dist` produces a functional but unsigned .dmg. |
| 5.2 | Notarization | **Skipped** (requires signing). Users will need to right-click -> Open to bypass Gatekeeper on first launch. |
| 5.3 | CI/CD Pipeline | **In Progress.** GitHub Action `.github/workflows/desktop-release.yml` created. Currently debugging build failures related to dependency synchronization. |
| 5.4 | Distribution | Artifacts created in `desktop/dist/`: `.dmg` (~140MB) and `.zip`. |

---

## Current Blockers & Active Tasks (Feb 16, 2026)

### 1. Library Synchronization (Critical) — ✅ COMPLETED
The `agent_go` backend uses features (like `mcpagent.ToolExecutionLLMConfigKey`) that exist in the local `mcpagent/` directory but have not yet been released to the standalone `github.com/manishiitg/mcpagent` repository. This causes GitHub Actions to fail during compilation.
*   **Status:** Resolved. `mcpagent` tagged and released as `v1.2.9`. `agent_go/go.mod` updated to use this version.
*   **Action Required:** None.

### 2. Monorepo Path Resolution — ✅ COMPLETED
The GitHub runner fails to resolve `mcpagent` and `multi-llm-provider-go` because they are treated as external modules but the code is actually present in the monorepo.
*   **Status:** Resolved. `agent_go/go.mod` updated to point to released versions on GitHub, removing local `replace` directives for these libraries.

### 3. Frontend Syntax Errors — ✅ COMPLETED
Merge conflict resolution errors in `frontend/src/App.tsx` (specifically `Unexpected token` around line 1308) were blocking the frontend build.
*   **Status:** Resolved. Syntax error in `App.tsx` fixed and frontend build verified.

---


### Phase 6: Documentation and UX — ✅ COMPLETED (Initial)

| Step | Task | Notes |
|------|------|--------|
| 6.1 | README / install instructions | **Done.** Updated `desktop/README.md` with build and run instructions. |
| 6.2 | In-app messaging | Polished error dialogs for startup failures (e.g. port conflicts). |
| 6.3 | CDP Connectivity Helper | Add a UI status/button to help users connect to their local Chrome via `--remote-debugging-port=9222`. |
| 6.4 | Logs and debugging | **Done.** Updated `main.js` to pipe stdout/stderr from agent/workspace processes to `userData/logs/agent.log` and `userData/logs/workspace.log`. |

### Phase 7: Production Hardening (Future)

These steps are recommended for a commercial or wide public release to ensure robustness and security.

| Step | Task | Details |
|------|------|--------|
| 7.1 | Dynamic Port Allocation | **Critical for reliability.** Instead of hardcoded 45678/45679, let the OS assign random free ports (port 0). Have Go processes report these ports back to Electron (via stdout or temp file), and inject them into the React frontend at runtime. |
| 7.2 | IPC Authentication | **Critical for security.** Generate a random session token in Electron on startup. Pass it to Go processes as an env var. Require this token in an `Authorization` header for all API requests to prevent unauthorized access from other local software/scripts. |
| 7.3 | Zombie Process Prevention | **Reliability.** Implement a "parent heartbeat" or PID monitoring in the Go servers. If the Electron parent process dies unexpectedly (crash/force quit), the Go servers should automatically shut down to prevent orphaned background processes. |
| 7.4 | Auto-Update | Integrate `electron-updater` to pull new releases from GitHub. |

---

## Project Status Summary (Feb 16, 2026)

The standalone Electron application has been successfully prototyped and packaged.

### Artifacts Generated
- **DMG:** `desktop/dist/MCP Agent Builder-0.1.0-arm64.dmg` (~142MB)
- **ZIP:** `desktop/dist/MCP Agent Builder-0.1.0-arm64-mac.zip` (~138MB)

### Key Achievements
- **Zero-Dependency Startup:** Spawns `agent-server` and `workspace-server` sidecar binaries automatically on launch.
- **Filesystem Persistence:** Configured Go servers to use `app.getPath('userData')` for databases, logs, and search indices, ensuring compatibility with read-only application bundles.
- **Port Management:** Uses stable ports 45678/45679 with pre-flight conflict detection.
- **Integrated Frontend:** Frontend is served directly via the `agent-server` internal static file server, reducing process overhead.
- **Log Redirection:** Child process logs are correctly captured and redirected to the system's Application Support directory for troubleshooting.

### Next Steps (Post-Prototype)
- **Code Signing:** To distribute outside of local environments, the build process needs an Apple Developer ID.
- **Universal Build:** Configure `electron-builder` to produce universal binaries (arm64 + x64) for broader Mac compatibility.
- **In-App CDP Status:** Implement Phase 6.3 to help users with browser automation setup.

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
