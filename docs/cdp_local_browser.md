# CDP Local Browser Connection

Connect the agent's browser tools to your real Chrome browser via Chrome DevTools Protocol (CDP), so you can watch the agent navigate in real-time.

---

## Overview

By default, `agent-browser` launches a headless Chromium inside the workspace Docker container. With CDP mode, it connects to your **local Chrome** instead — you see every page load, click, and form fill as it happens.

CDP mode is available in **local mode only** (`LOCAL_MODE=true`). In server/cloud deployments, headless Chromium is used.

---

## Quick Start

### 1. Launch Chrome with Remote Debugging

Close all Chrome windows first, then:

**macOS:**
```bash
/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --remote-debugging-port=9222
```

**Linux:**
```bash
google-chrome --remote-debugging-port=9222
```

### 2. Enable CDP in the UI

1. Click the **Browser** button in the chat input bar
2. A popup appears — select **"Connect to Local Chrome (CDP)"**
3. Set the port (default: 9222)
4. Click **"Check Connection"** — should show green "Connected"
5. Click **"Done"**

The browser button now shows a green **CDP** badge. All `agent_browser` tool calls will connect to your Chrome.

### 3. Verify

Send a message like "open https://example.com in the browser" — your Chrome window should navigate to the page.

---

## Architecture

```
┌─────────────┐     cdp_port: 9222      ┌──────────────┐
│   Frontend   │ ───────────────────────▶│   agent_go   │
│  (ChatInput) │   in API request body   │   (server)   │
└─────────────┘                          └──────┬───────┘
                                                │
                                  ExecuteCommand with
                                  --cdp http://${HOST_IP}:9222
                                                │
                                         ┌──────▼───────┐
                                         │ workspace-api│
                                         │   (Docker)   │
                                         └──────┬───────┘
                                                │
                                         sh -c "HOST_IP=$(getent hosts
                                           host.docker.internal ...) &&
                                           agent-browser --cdp
                                           http://${HOST_IP}:9222 ..."
                                                │
                                         ┌──────▼───────┐
                                         │    Chrome     │
                                         │  (host:9222)  │
                                         └──────────────┘
```

### Key Design Decisions

- **LLM transparency**: The LLM calls `agent_browser(command="open", args=["..."])` identically in both modes. The `--cdp` flag is injected by the executor layer — no tool definition changes needed.
- **Docker networking**: The workspace container uses `extra_hosts: ["host.docker.internal:host-gateway"]` to reach the host. Chrome rejects non-IP `Host` headers, so the command resolves `host.docker.internal` to its IP at runtime via `getent hosts`.
- **CDP check from container**: The "Check Connection" button calls the workspace API's `/api/cdp-check` endpoint (not agent_go), verifying connectivity from where `agent-browser` actually runs.

---

## Data Flow

### Frontend → Backend

The frontend sends `cdp_port` in the API request body:

```json
{
  "query": "open https://example.com",
  "enable_browser_access": true,
  "cdp_port": 9222
}
```

`cdp_port` is only included when both browser access **and** CDP mode are enabled. When absent/zero, the executor uses headless mode.

### Backend Processing

1. `server.go` reads `CdpPort` from `QueryRequest`
2. Passes it to `CreateWorkspaceBrowserToolExecutors(cdpPort)`
3. Factory creates `browser.NewExecutor(client, browser.WithCdpPort(9222))`
4. `executor.go` prepends `--cdp http://host.docker.internal:9222` to CLI args
5. `client.go` wraps the command with shell-level IP resolution before sending to workspace API

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LOCAL_MODE` | `false` | Enables local-only features including CDP option in UI |
| `CDP_HOST` | `host.docker.internal` | Override the CDP host address (e.g., `localhost` when not using Docker) |

### Docker Compose

The workspace-api service needs `extra_hosts` to reach the host machine:

```yaml
# docker-compose.yml
services:
  workspace-api:
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

---

## Files

| File | Role |
|------|------|
| `agent_go/pkg/browser/executor.go` | Injects `--cdp` flag, builds CDP URL |
| `agent_go/pkg/browser/client.go` | Resolves `host.docker.internal` to IP at runtime |
| `agent_go/pkg/browser/types.go` | `ShellExecuteRequest` with `WorkingDirectory` |
| `agent_go/cmd/server/server.go` | `CdpPort` in `QueryRequest`, capabilities, routing |
| `agent_go/cmd/server/auth_middleware.go` | `IsLocalMode()` function |
| `agent_go/cmd/server/virtual-tools/workspace_browser_tools.go` | Factory passes CDP port to executor |
| `workspace/handlers/cdp_check.go` | `GET /api/cdp-check` — connectivity check from container |
| `workspace/models/shell.go` | `WorkingDirectory` no longer required |
| `frontend/src/services/api.ts` | `checkCdpPort()` calls workspace API |
| `frontend/src/services/api-types.ts` | `cdp_port` in request, `local_mode` in capabilities |
| `frontend/src/stores/useChatStore.ts` | `useCdp`, `cdpPort` in tab config |
| `frontend/src/stores/useCapabilitiesStore.ts` | `isLocalMode()` helper |
| `frontend/src/utils/chatSubmitHelpers.ts` | Sends `cdp_port` when CDP enabled |
| `frontend/src/components/ChatInput.tsx` | CDP config popup, browser button with mode badge |
| `frontend/src/components/PresetModal.tsx` | CDP config in preset editor |
| `docker-compose.yml` | `extra_hosts` for host.docker.internal |
| `agent_go/run_server_with_logging.sh` | Sets `LOCAL_MODE=true` |

---

## API Endpoints

### `GET /api/capabilities` (agent_go)

Returns `local_mode: true/false` based on `LOCAL_MODE` env var.

### `GET /api/cdp-check?port=9222` (workspace API)

Checks TCP connectivity to Chrome's CDP port from inside the Docker container. Returns:

```json
{"connected": true}
```
or
```json
{"connected": false, "error": "Cannot reach CDP on port 9222: ..."}
```

---

## Troubleshooting

### macOS: "Package is damaged" or app won't open (Chrome CDP launcher)

If you downloaded the Chrome CDP launcher zip and macOS says the app is damaged or can't be opened:

1. **Remove quarantine** — In Terminal, run (e.g. if the app is in Downloads; adjust the path if you moved it):
   ```bash
   xattr -c ~/Downloads/Chrome\ CDP.app
   ```
   (Use `-c` only; `-r` is not supported on all macOS versions.)
   Then open the app again from Spotlight or LaunchPad.

2. **Or use Right-click → Open** — Right-click "Chrome CDP.app" → **Open** → click **Open** in the dialog. You may only need to do this once.

### "Chrome is not reachable on port 9222"

1. **Chrome not running with CDP**: Close all Chrome windows and relaunch with `--remote-debugging-port=9222`
2. **Port conflict**: Another process may be using port 9222. Check with `lsof -i :9222`
3. **Docker networking**: Ensure `extra_hosts` is set in `docker-compose.yml` and container was recreated (`docker compose up -d workspace-api`)

### Verify Chrome CDP is running

```bash
curl -s http://localhost:9222/json/version
```

Should return JSON with `Browser`, `Protocol-Version`, etc.

### Verify workspace container can reach Chrome

```bash
curl -s "http://localhost:8081/api/cdp-check?port=9222"
```

Should return `{"connected": true}`.

### Test agent-browser directly inside container

```bash
docker exec <workspace-container> sh -c \
  'HOST_IP=$(getent hosts host.docker.internal | awk "{print \$1}") && \
   agent-browser --cdp "http://${HOST_IP}:9222" snapshot --session test --json' \
  | head -c 200
```
