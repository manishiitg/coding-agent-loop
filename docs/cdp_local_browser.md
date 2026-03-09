# CDP Local Browser Connection

Connect the agent's browser tools to your real Chrome browser via Chrome DevTools Protocol (CDP), allowing you to watch the agent navigate in real-time and reuse your logged-in sessions.

---

## Overview

By default, browser-based MCP servers (like `playwright` or `agent-browser`) run an isolated, headless browser inside a container. By using **CDP mode**, you can point these tools to a Chrome instance running on your host machine.

## How it Works

1.  **Launch Chrome with Remote Debugging**: You start Chrome on your host with a specific port (default `9222`).
2.  **Connection String**: The agent is given a CDP URL (e.g., `http://host.docker.internal:9222`).
3.  **Connectivity Check**: The frontend verifies the connection before starting the session.

## Configuration

### 1. Launch Chrome (Host)

**macOS:**
```bash
/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --remote-debugging-port=9222
```

**Windows:**
```cmd
"C:\Program Files\Google\Chrome\Application\chrome.exe" --remote-debugging-port=9222
```

### 2. Connection Logic

The system uses a two-tier connectivity check implemented in `frontend/src/services/api.ts` and `agentApi.checkCdpPort`:

1.  **Agent API Check**: Calls `agent_go`'s `/api/cdp-check`. This attempts a TCP dial from the agent server to the specified port.
2.  **Workspace API Check**: If the agent is running in Docker, it also tries the Workspace API's `/api/cdp-check`. This is critical because the browser tools actually execute inside the workspace container, which has a different network view than the `agent_go` container.

## Frontend UI Features

- **Check Connection Button**: Found in both the Preset Modal and Chat Input settings. It provides immediate feedback (Success/Failure).
- **Auto-Check**: The UI automatically debounces and checks the connection as you type the port number.
- **macOS Helper**: A direct link to download a pre-configured macOS launcher that handles the complex command line flags for you.

## macOS "Damaged Package" Fix

If you download the custom Chrome-CDP launcher on macOS, you may see a "Developer cannot be verified" or "App is damaged" error. This is due to macOS Gatekeeper.

**Fix:**
```bash
# Remove the quarantine attribute
xattr -cr /path/to/Chrome-CDP.app
```

## Network Paths (Docker)

If the agent is running inside Docker, use these host aliases:

- **macOS/Windows**: `http://host.docker.internal:9222`
- **Linux**: `http://172.17.0.1:9222` (or your host IP)

The backend handles the resolution of these aliases to ensure the MCP server receives a valid endpoint.

## Troubleshooting

### Connection Refused
- Ensure Chrome is actually running with the `--remote-debugging-port=9222` flag.
- Verify no other process is using port 9222 (`lsof -i :9222`).
- Close all other Chrome instances before launching with the flag, as Chrome sometimes fails to enable debugging if an existing profile is already active.

### Agent sees a blank page
- Chrome DevTools Protocol only allows one connection at a time to a specific tab. If you have the "Inspect" window open for that tab, the agent's tools may be blocked.
