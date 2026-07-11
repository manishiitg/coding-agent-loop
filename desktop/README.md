# AgentWorks — Desktop

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

### Quick local build (unsigned, no GitHub upload)

```bash
cd desktop
./dev-setup.sh                                              # only first time / when Go code changes
CSC_IDENTITY_AUTO_DISCOVERY=false \
  npx electron-builder --mac dmg --publish never
```

Artifacts currently keep the legacy `Runloop-<version>-arm64.dmg` filename for updater compatibility during the AgentWorks rename. Install: `open desktop/dist/Runloop-*.dmg` → drag the app to Applications. First launch: right-click → Open to bypass Gatekeeper.

### Cutting a release via CI (publishes to GitHub Releases)

Releases are built by `.github/workflows/desktop-release.yml`. The `release` job runs on tag pushes (`v*`).

1. **Confirm the next version** — latest released tag wins. Check:

   ```bash
   git ls-remote --tags origin | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sort -V | tail -3
   ```

   Pick the next semver. Auto-update only triggers if your tag is **higher** than the current "Latest".

2. **Bump `package.json` `version`** to match the tag (without the `v` prefix). The CI's `npm version` step is idempotent now, so committing the bump first is safe:

   ```bash
   # in desktop/package.json, set "version": "1.25.7"
   git add desktop/package.json
   git commit -m "Bump desktop version to 1.25.7"
   ```

3. **(If shipping code changes)** commit them too, then push to `main`:

   ```bash
   git push origin main
   ```

4. **Tag and push**:

   ```bash
   git tag v1.25.7
   git push origin v1.25.7
   ```

5. **Watch the workflow** (~10 min on `macos-15-intel`):

   ```bash
   gh run watch $(gh run list --workflow desktop-release.yml --limit 1 --json databaseId --jq '.[0].databaseId')
   ```

6. **Publish the draft release**. electron-builder creates the GitHub Release as a draft by default. Promote it:

   ```bash
   gh release edit v1.25.7 --draft=false
   gh release view v1.25.7 --json url,isDraft,tagName
   ```

   Now visible at `https://github.com/<org>/<repo>/releases/tag/v1.25.7`. Auto-update in already-installed apps will offer the upgrade on next launch.

### Tag pre-releases (don't disturb "Latest")

```bash
git tag v1.25.7-test1
git push origin v1.25.7-test1
gh release edit v1.25.7-test1 --prerelease --draft=false
```

### Versioning notes

- Both jobs in `desktop-release.yml` (artifact + release) run on every push and tag respectively. The `release` job uses `npm version $TAG --no-git-tag-version` to sync `package.json` to the tag — but only if they differ (idempotent guard added in the workflow).
- The dmg is **unsigned** (no `mac.identity` / no notarization). Existing users right-click → Open on first launch. To ship signed/notarized builds, add `mac.identity` to `package.json` and provide an Apple Developer ID + notarization credentials via repo secrets.

### What goes in the dmg

- `main.js`, `preload.js`, `settings.html`, `auth-prompt.html` (renderer/main)
- `resources/agent-server`, `resources/workspace-server` (Go binaries built by `dev-setup.sh` or CI)
- `resources/icons/`, `agent_go/configs/`, `agent_go/static/` via `extraResources` in `package.json`

### Host prerequisites

- Claude Code experimental mode requires `tmux` 3.x or newer as a local runtime dependency. The curl installer attempts `brew install tmux` when Homebrew is available; otherwise install it manually with `brew install tmux`.
- The curl installer ensures `mcpbridge` is installed to `~/go/bin` for Claude Code/Codex MCP bridge access. If Go is missing, it installs Go through Homebrew when available; otherwise it asks the user to install Go and rerun the same curl command.

## Runtime Details

### Window and Server Lifecycle

On macOS, closing the AgentWorks window keeps the app process and bundled servers running in the background so scheduled jobs can continue. Click the Dock icon or the AgentWorks menu bar icon to reopen the UI.

Right-click the menu bar icon and choose **Quit AgentWorks (Stop Servers)**, or use **Quit AgentWorks** / `Cmd+Q`, to fully exit the app. Quitting stops both bundled servers.

### Ports
- **45678**: Agent server (API + static frontend)
- **45679**: Workspace server

### Data & Logs
The app stores data in the system's Application Support directory:
`~/Library/Application Support/Runloop/`

The data path is still legacy-compatible while the rename is in progress. Do not rename it blindly without a migration.

- **Logs:** `logs/agent.log`, `logs/workspace.log`
- **Database:** `chat_history.db`
- **Configuration:** `configs/mcp_servers.json`
- **Workspace Data:** `data/` and `workspace-docs/`

If ports are in use or servers fail to start, the app shows an error dialog and exits. Check the log files in the directory above for details.
