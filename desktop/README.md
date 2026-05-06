# Runloop — Desktop

Electron shell for the standalone Mac app. It bundles the `agent-server` and `workspace-server` binaries, managing their lifecycle automatically.

## Prerequisites

- Node 18+
- Go 1.22+ (to build backend binaries)
- macOS (Apple Silicon or Intel)

## Development Setup

The `dev-setup.sh` script builds the Go binaries and installs Node dependencies:

```bash
cd desktop
./dev-setup.sh
```

## Run Locally (Unpackaged)

```bash
cd desktop
npm start
```

This starts Electron; the main process will check ports **45678** and **45679**, spawn both servers from `resources/`, wait for health, then open a window at `http://127.0.0.1:45678`.

## Development with Docker

If you are already running the frontend and workspace services via Docker Compose, you can run the Electron shell as a wrapper without spawning internal servers:

1.  **Start your Docker services:**
    ```bash
    docker-compose up
    ```
2.  **Start Electron in "Dev Mode"** by pointing it to your local frontend:
    ```bash
    cd desktop
    DEV_URL=http://localhost:5173 npm start
    ```

In this mode, Electron skips port conflict checks and server lifecycle management, directly loading your development URL.

## Build & Package (Distribution)

To create a distributable `.dmg` and `.zip`:

```bash
cd desktop
npm run dist
```

Artifacts will be output to `desktop/dist/`.
**Note:** The local build is unsigned. On first launch, you may need to Right-Click the app -> Open to bypass Gatekeeper.

## Runtime Details

### Window and Server Lifecycle

On macOS, closing the Runloop window keeps the app process and bundled servers running in the background so scheduled jobs can continue. Click the Dock icon or the Runloop menu bar icon to reopen the UI.

Right-click the menu bar icon and choose **Quit Runloop (Stop Servers)**, or use **Quit Runloop** / `Cmd+Q`, to fully exit the app. Quitting stops both bundled servers.

### Ports
- **45678**: Agent server (API + static frontend)
- **45679**: Workspace server

### Data & Logs
The app stores data in the system's Application Support directory:
`~/Library/Application Support/Runloop/`

- **Logs:** `logs/agent.log`, `logs/workspace.log`
- **Database:** `chat_history.db`
- **Configuration:** `configs/mcp_servers.json`
- **Workspace Data:** `data/` and `workspace-docs/`

If ports are in use or servers fail to start, the app shows an error dialog and exits. Check the log files in the directory above for details.
