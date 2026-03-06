# Camofox Stealth Browser (MCP)

Anti-detect browser automation using [camofox-mcp](https://github.com/redf0x1/camofox-mcp) — an MCP server that bridges to a Camoufox-based browser (patched Firefox) running on the host.

---

## Overview

Some websites use aggressive bot detection (Cloudflare, DataDome, PerimeterX, etc.) that blocks headless Chrome and standard automation tools. Camofox solves this with:

- Anti-detect Firefox fork with fingerprint rotation
- **43 MCP tools** the agent calls directly (no code generation needed)
- **Accessibility-tree snapshots** instead of screenshots (~90% fewer tokens)
- **Headed mode** — visible browser window on your machine
- **Session/cookie persistence** across tabs and conversations
- 14 search macros and 8 geo presets built-in

---

## Architecture

```
User selects "Stealth Browser (Camofox)" in browser mode popup
    |
setBrowserMode('stealth') adds 'camofox' to selectedServers
    |
Chat request includes enabled_servers: ["camofox"]
    |
Backend spawns camofox-mcp via: npx -y camofox-mcp@latest
    |
camofox-mcp connects to camofox-browser on host (http://localhost:9377)
    |
Agent calls MCP tools directly (navigate, click, snapshot, etc.)
    |
Accessibility tree snapshots returned (not screenshots)
```

### Two Services

| Service | Where | Purpose |
|---------|-------|---------|
| `camofox-browser` | Host machine | Runs the anti-detect Firefox browser |
| `camofox-mcp` | Spawned by backend | MCP bridge between agent and browser |

### Key Differences from Other Browser Modes

| Mode | Tool | Browser | Use Case |
|------|------|---------|----------|
| Headless | `agent_browser` MCP | Chromium (in Docker) | Standard browsing |
| CDP | `agent_browser` MCP | Your local Chrome | Watch agent browse in real-time |
| Playwright | `playwright` MCP | Chromium (in Docker) | Advanced automation |
| **Stealth** | `camofox` MCP | Camofox Firefox (on host) | Bot-detection bypass, headed mode |

---

## Setup

### 1. Start camofox-browser on host

```bash
npx camofox-browser@latest
```

For headed mode (visible browser window):
```bash
CAMOFOX_HEADLESS=false npx camofox-browser@latest
```

### 2. Verify health

```bash
curl http://localhost:9377/health
# Expected: {"ok":true,"browserConnected":true}
```

### 3. MCP Server Config

The `camofox` MCP server is pre-configured in `agent_go/configs/mcp_servers_clean.json`:

```json
"camofox": {
  "command": "npx",
  "args": ["-y", "camofox-mcp@latest"],
  "env": {
    "CAMOFOX_URL": "http://localhost:9377"
  }
}
```

The backend spawns this process automatically when stealth mode is selected.

---

## Usage

### 1. Enable in the UI

1. Click the **Browser** button in the chat input bar
2. Select **"Stealth Browser (Camofox)"** (orange radio button)
3. A green status dot confirms the MCP server is connected
4. The `camofox` MCP server is auto-added to enabled servers

### 2. Ask the Agent

Example prompts:

- "Visit https://bot.sannysoft.com and tell me the results"
- "Scrape the product listings from [url] using stealth mode"
- "Go to [url] and take a screenshot — it blocks regular browsers"
- "Search Google for [query] and extract the results"

### 3. How It Works

The agent calls camofox MCP tools directly — no Python code generation needed:

1. `camofox_navigate` — navigate to a URL
2. `camofox_snapshot` — get accessibility tree (text content, not screenshot)
3. `camofox_click` / `camofox_fill` — interact with elements
4. `camofox_screenshot` — take visual screenshot when needed

---

## Available Tools (43 total)

### Navigation
- `camofox_navigate` — Go to URL
- `camofox_go_back` / `camofox_go_forward` — Browser history
- `camofox_wait` — Wait for content/navigation

### Content
- `camofox_snapshot` — Accessibility tree snapshot (primary output)
- `camofox_screenshot` — Visual screenshot
- `camofox_get_text` — Extract text content

### Interaction
- `camofox_click` — Click elements
- `camofox_fill` — Fill input fields
- `camofox_select_option` — Select dropdown values
- `camofox_check` / `camofox_uncheck` — Checkboxes
- `camofox_hover` — Hover over elements
- `camofox_scroll` — Scroll the page
- `camofox_drag` — Drag and drop

### Tabs & Sessions
- `camofox_new_tab` — Open new tab
- `camofox_select_tab` — Switch tabs
- `camofox_close_tab` — Close tab
- Session/cookie persistence is automatic

### Search Macros
- `camofox_search_google` — Google search
- `camofox_search_bing` — Bing search
- Plus 12 more search engines

### Geo Presets
- 8 geographic presets for location-specific browsing

---

## Headed Mode

When `CAMOFOX_HEADLESS=false` is set, the browser window is visible on your host machine. This lets you:

- Watch the agent navigate in real-time
- See bot detection challenges being solved
- Debug navigation issues visually
- Demonstrate automation to others

---

## Files

| File | Purpose |
|------|---------|
| `agent_go/configs/mcp_servers_clean.json` | Camofox MCP server entry |
| `frontend/src/components/ChatInput.tsx` | Stealth mode radio button + `setBrowserMode('stealth')` logic |
| `frontend/src/stores/useChatStore.ts` | `browserMode` type includes `'stealth'` |
| `agent_go/cmd/server/server.go` | `hasCamofox` detection for browser upload instructions |

---

## Troubleshooting

### Camofox server not found (red dot in UI)

The `camofox` MCP server entry is missing from config. Check `agent_go/configs/mcp_servers_clean.json`.

### Camofox server has errors (amber dot in UI)

The MCP server process failed to start. Common causes:
- `camofox-browser` is not running on host (`npx camofox-browser@latest`)
- Port 9377 is not reachable from the container
- Network configuration blocking localhost access

### Health check fails

```bash
curl http://localhost:9377/health
```

If this returns an error, `camofox-browser` is not running. Start it with:
```bash
npx camofox-browser@latest
```

### Site still detects the bot

- Use headed mode (`CAMOFOX_HEADLESS=false`) for better evasion
- Try a geo preset for realistic geolocation
- Add delays between actions using `camofox_wait`
