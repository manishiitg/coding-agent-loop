# Camoufox Stealth Browser

Anti-detect browser automation using Camoufox (a patched Firefox fork) for websites that block regular headless browsers.

---

## Overview

Some websites use aggressive bot detection (Cloudflare, DataDome, PerimeterX, etc.) that blocks headless Chrome and standard automation tools. Camoufox solves this by wrapping Playwright with a patched Firefox that includes:

- Fingerprint rotation (canvas, WebGL, fonts, screen resolution)
- Humanized cursor movement and typing
- WebRTC and WebGL leak prevention
- OS-level spoofing
- Anti-fingerprinting countermeasures

The stealth browser mode lets the agent write and execute Python scripts using Camoufox inside the workspace Docker container. No additional MCP servers are needed — it uses the existing `execute_shell_command` tool.

---

## Architecture

```
User selects "Stealth Browser (Camoufox)" in browser mode popup
    ↓
setBrowserMode('stealth') auto-selects stealth-browser skill
    ↓
Chat request includes selected_skills: ["stealth-browser"]
    ↓
Backend loads stealth-browser/SKILL.md into system prompt
    ↓
Agent reads skill, writes Python script using Camoufox
    ↓
execute_shell_command runs script in workspace Docker container
    ↓
Camoufox launches patched Firefox with anti-detect features
    ↓
Screenshots + results saved to Chats/ folder
```

### Key Differences from Other Browser Modes

| Mode | Tool | Browser | Use Case |
|------|------|---------|----------|
| Headless | `agent_browser` MCP | Chromium (in Docker) | Standard browsing |
| CDP | `agent_browser` MCP | Your local Chrome | Watch agent browse in real-time |
| Playwright | `playwright` MCP | Chromium (in Docker) | Advanced automation |
| **Stealth** | `execute_shell_command` | Camoufox Firefox (in Docker) | Bot-detection bypass |

Stealth mode does **not** use `agent_browser` or a dedicated MCP server. The agent writes Python code and runs it via shell execution, guided by the `stealth-browser` skill.

---

## Setup

### Docker Container

Camoufox is pre-installed in the workspace Docker image (`workspace/Dockerfile`):

```dockerfile
# Debian base (glibc required — Camoufox's Firefox binary is glibc-compiled)
FROM debian:bookworm-slim

# Install camoufox + fetch the patched Firefox binary
RUN pip install camoufox && python3 -m camoufox fetch
```

**Important:** The container must use a glibc-based image (Debian/Ubuntu). Camoufox's Firefox binary is incompatible with musl/Alpine.

### Skill

The skill file is at `workspace-docs/skills/stealth-browser/SKILL.md`. It provides:

- When to use Camoufox vs regular browser
- Python code templates (navigation, screenshots, forms, scraping, proxy, async)
- Execution patterns and output handling tips

---

## Usage

### 1. Enable in the UI

1. Click the **Browser** button in the chat input bar
2. Select **"Stealth Browser (Camoufox)"** (orange radio button)
3. The `stealth-browser` skill is auto-selected and workspace access is enabled

### 2. Ask the Agent

Example prompts:

- "Visit https://bot.sannysoft.com stealthily and tell me the results"
- "Scrape the product listings from [url] using stealth mode"
- "Go to [url] and take a screenshot — it blocks regular browsers"

### 3. How It Works

The agent will:
1. Write a Python script using Camoufox's sync API
2. Execute it via `execute_shell_command`
3. Take screenshots at key moments (saved to `Chats/`)
4. Extract and report the content

---

## Code Examples

### Basic Navigation + Screenshot

```python
from camoufox.sync_api import Camoufox

with Camoufox(headless=True) as browser:
    page = browser.new_page()
    page.goto("https://example.com", wait_until="domcontentloaded")
    page.screenshot(path="Chats/result.png", full_page=True)
    print(f"Title: {page.title()}")
```

### With Proxy

```python
from camoufox.sync_api import Camoufox

with Camoufox(
    headless=True,
    proxy={"server": "http://proxy:8080", "username": "user", "password": "pass"},
    geoip=True,  # Auto-set locale/timezone from proxy IP
) as browser:
    page = browser.new_page()
    page.goto("https://example.com")
    page.screenshot(path="Chats/proxy_result.png", full_page=True)
```

### Async Parallel Browsing

```python
import asyncio
from camoufox.async_api import AsyncCamoufox

async def scrape(url, index):
    async with AsyncCamoufox(headless=True) as browser:
        page = await browser.new_page()
        await page.goto(url, wait_until="domcontentloaded")
        await page.screenshot(path=f"Chats/page_{index}.png")
        return await page.title()

async def main():
    urls = ["https://example.com", "https://example.org"]
    results = await asyncio.gather(*[scrape(u, i) for i, u in enumerate(urls)])
    for url, title in zip(urls, results):
        print(f"{url}: {title}")

asyncio.run(main())
```

---

## Limitations

- **No video recording** — Playwright's `record_video_dir` uses `Browser.setScreencastOptions` which is Chromium-only. Camoufox uses Firefox/Juggler which does not support it. Use screenshots instead.
- **Headless only** — The Docker container has no display. Always use `headless=True`.
- **No live preview** — Unlike CDP mode, you can't watch the browser in real-time. Screenshots in `Chats/` provide visibility.
- **Debian/glibc required** — The Camoufox Firefox binary is compiled against glibc and will not run on Alpine/musl.

---

## Files

| File | Purpose |
|------|---------|
| `workspace/Dockerfile` | Installs `camoufox` + fetches Firefox binary |
| `workspace-docs/skills/stealth-browser/SKILL.md` | Skill with code templates injected into system prompt |
| `frontend/src/components/ChatInput.tsx` | Stealth mode radio button + `setBrowserMode('stealth')` logic |
| `frontend/src/stores/useChatStore.ts` | `browserMode` type includes `'stealth'` |

---

## Troubleshooting

### Camoufox fails to launch

Check that the Firefox binary was fetched during Docker build:
```bash
docker exec <container> python3 -c "from camoufox.sync_api import Camoufox; print('OK')"
```

### Import errors

Verify camoufox is installed:
```bash
docker exec <container> pip show camoufox
```

### Site still detects the bot

- Try adding a proxy with `geoip=True` for realistic geolocation
- Add delays between actions: `page.wait_for_timeout(2000)`
- Use `page.wait_for_load_state("networkidle")` before interacting
