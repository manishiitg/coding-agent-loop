# Camoufox Stealth Browser (MCP)

Anti-detect browser automation using [camofox-mcp](https://github.com/redf0x1/camofox-mcp) — an MCP server that bridges to a Camoufox-based browser (patched Firefox) running on the host.

---

## Overview

Unlike standard Playwright which is easily detected by anti-bot services (Cloudflare, Akamai, etc.), **Camoufox** uses a hardened version of Firefox designed to pass all major browser fingerprinting tests.

The integration works by:
1.  **Host Process**: `camofox-browser` runs as a persistent process on your host machine.
2.  **MCP Server**: The `camofox` MCP server connects to this host process via a WebSocket (default port `9377`).
3.  **Agent Interaction**: The agent uses the `camofox` tools (click, type, navigate, etc.) which are routed through the stealth browser.

## Backend Architecture

The backend provides a dedicated endpoint to ensure the browser is active before the agent starts using it.

### Camoufox Start Endpoint
- **URL**: `/api/camofox-start`
- **Method**: `POST`
- **Payload**: `{ "headed": boolean }`
- **Handler**: `handleCamofoxStart` in `agent_go/cmd/server/server.go`

This handler:
1.  Checks if a process is already listening on the Camoufox port.
2.  If not, it spawns `camofox-browser` (must be installed via `npm install -g camofox-browser`).
3.  Supports both **Headless** (background) and **Headed** (visible) modes via the payload.

## MCP Configuration

The `camofox` server is pre-configured in `agent_go/configs/mcp_servers_clean.json`:

```json
"camofox": {
  "command": "camofox-mcp",
  "args": [],
  "env": {
    "CAMOFOX_URL": "http://localhost:9377"
  }
}
```

## Frontend Integration

The UI provides a "Stealth Browser" mode in the browser settings:
- **Auto-Start**: When "Stealth Browser" is selected, the frontend calls `agentApi.startCamofox(headed)`.
- **Headed Toggle**: Users can toggle headed mode in real-time.
- **Visual Feedback**: The UI shows a "Starting Camoufox..." status while the process initializes.

## Prerequisites for Developers

To use Stealth Browser mode locally:

1.  **Install Camoufox**:
    ```bash
    npm install -g camofox-browser
    ```
2.  **First Run**: The first time it runs, it will download the patched Firefox binary (~200MB).
3.  **Binary Location**: Binaries are stored in `~/.camoufox`.

## Comparison with standard Playwright

| Feature | Playwright MCP | Camoufox MCP |
| :--- | :--- | :--- |
| **Engine** | Chromium (Default) | Firefox (Patched) |
| **Stealth** | Basic (easily detected) | High (hardened for evasion) |
| **Architecture** | Spawns per session | Shared host process |
| **Performance** | Faster startup | Slightly slower (heavy hardening) |
| **Tooling** | 15+ tools | 40+ specialized tools |

## Troubleshooting

### "camofox-browser not found"
Ensure you have installed the global package: `npm install -g camofox-browser`. The backend expects this command to be in the system PATH.

### Site still detects the bot
- Ensure `headed` mode is enabled; some sites use viewport consistency checks that fail in headless mode.
- Check if your IP address is flagged (Camoufox hides the browser, but not your network signature).
- Use `camofox_wait` to add human-like delays between actions.
