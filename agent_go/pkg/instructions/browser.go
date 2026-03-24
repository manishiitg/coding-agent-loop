package instructions

// BrowserConfig holds the resolved browser state for prompt generation.
type BrowserConfig struct {
	HasPlaywright   bool
	HasCamofox      bool
	HasAgentBrowser bool
	CdpPort         int    // >0 means CDP mode, 0 means headless (legacy, use Mode when set)
	Mode            string // "cdp", "headless", "playwright", "stealth", "" (empty = fallback to CdpPort)
}

// BuildBrowserInstructions returns the complete browser system prompt
// (upload + mode-specific) for the given config, or "" if no browser tool is active.
func BuildBrowserInstructions(cfg BrowserConfig) string {
	if !cfg.HasPlaywright && !cfg.HasCamofox && !cfg.HasAgentBrowser {
		return ""
	}

	result := ""

	// Use Mode as primary decision, fall back to legacy CdpPort/Has* flags
	isCdp := cfg.Mode == "cdp" || (cfg.Mode == "" && cfg.CdpPort > 0)
	isPlaywright := cfg.Mode == "playwright" || (cfg.Mode == "" && cfg.HasPlaywright)
	isStealth := cfg.Mode == "stealth" || (cfg.Mode == "" && cfg.HasCamofox)

	if isPlaywright {
		result += "\n" + GetPlaywrightModeInstructions()
	} else if isStealth {
		result += "\n" + GetCamofoxInstructions()
	} else if isCdp {
		result += "\n" + GetCdpBrowserInstructions()
	} else {
		result += "\n" + GetHeadlessBrowserInstructions()
	}

	return result
}

// GetBrowserUploadInstructions returns system prompt instructions for browser file upload.
// Appended to the agent's system prompt when browser access or Playwright is active.
// Tells the LLM to use workspace-relative paths (e.g. "Downloads/file.pdf") — these are
// automatically resolved to absolute host paths by the toolArgTransformer before reaching
// the Playwright MCP server. For agent_browser (headless/CDP), relative paths work natively
// since the CLI runs inside the Docker container with workspace as its working directory.
func GetBrowserUploadInstructions() string {
	return `

## Browser File Upload

When a website has a file upload input (e.g. file picker, drag-and-drop zone), use these tools to upload workspace files:

### Using agent_browser (Headless/CDP mode)
1. Snapshot to find the file input: agent_browser(command="snapshot", args=["-i"])
2. Upload using the ref: agent_browser(command="upload", args=["@ref", "Downloads/report.pdf"])

### Using browser_file_upload (Playwright mode)
1. First use browser_snapshot to find the file input element
2. Upload: browser_file_upload(paths=["Downloads/report.pdf"], selector="input[type=file]")

### Path Rules
- Always use **workspace-relative paths** (e.g. "Downloads/report.pdf", "Chats/output.csv")
- Paths are automatically resolved to absolute paths — do NOT construct absolute paths yourself
- Files in "Downloads/" are user-uploaded files; files in "Chats/" are created during the conversation
- If you need to create a file first, save it to "Chats/" using execute_shell_command, then upload it
`
}

// GetCdpModeInstructions returns instructions specific to CDP mode (connected to user's real Chrome).
func GetCdpModeInstructions() string {
	return `
## Browser Mode: CDP (Connected to User's Chrome)

You are controlling the **user's real Chrome browser** via Chrome DevTools Protocol (CDP).

**Key behaviors:**
- The user can **see everything you do** in their browser — actions are visible in real-time
- The browser may have **existing cookies, login sessions, and tabs** — you can leverage authenticated sessions without re-logging in
- **Do NOT call close** unless the user asks — it will close their browser tab
- **Take screenshots** to show the user what you see, since they can also verify visually
- Sessions **persist across tool calls** — you don't need to re-open pages between interactions
- If a site requires login and the user is already logged in, just navigate directly to the target page

**Connection behavior (important):**
- CDP endpoint is already configured by the backend from the selected port.
- Do **NOT** ask the user for a websocket debugger URL or run "curl localhost:9222/json/version".
- In containerized runs, localhost points to the container, not the host browser.
- When you describe or troubleshoot the endpoint, use host.docker.internal:<port> (Linux: host IP such as 172.17.0.1:<port>).
- Do not suggest localhost:<port> for container-side agent_browser connectivity.

**Best practices:**
- Start with a **snapshot** to see the current page state before taking any action
- Use **session="default"** unless you need multiple isolated sessions
- Be careful with form submissions and purchases — this is the user's real browser with real accounts
`
}

// GetHeadlessModeInstructions returns instructions specific to headless browser mode.
func GetHeadlessModeInstructions() string {
	return `
## Browser Mode: Headless (Container Browser)

You are controlling a **headless Chromium browser** running inside a container.

**Key behaviors:**
- The browser is **fresh** — no existing cookies, sessions, or tabs. You must login from scratch if needed.
- The user **cannot see** the browser — take **screenshots** to show them what's happening
- You can freely **open and close** tabs/sessions without affecting the user
- Browser state is **ephemeral** — it resets between sessions

**Best practices:**
- Take screenshots at key moments so the user can verify progress
- Handle login flows explicitly (fill credentials, handle 2FA via human_feedback if needed)
- Use **session="default"** unless you need parallel browser instances
`
}

// GetPlaywrightModeInstructions returns instructions specific to Playwright MCP mode.
func GetPlaywrightModeInstructions() string {
	return `
## Browser Mode: Playwright (MCP Server)

You are using the Playwright MCP tools (browser_* functions), not agent_browser.

**Key behaviors:**
- Use browser_snapshot to inspect the current page and discover element refs/selectors.
- Prefer browser_click/browser_type/browser_press for interactions.
- Use browser_screenshot when visual proof is needed.
- Keep interactions deterministic: snapshot -> act -> snapshot.

**File uploads:**
- Use browser_file_upload with workspace-relative paths (e.g. "Downloads/file.pdf").
- Do not construct absolute filesystem paths manually.

**Best practices:**
- Re-check page state after every navigation or major interaction.
- If an element is missing, refresh snapshot before retrying.
`
}

// GetAgentBrowserQuickStartInstructions returns inline instructions for using the agent-browser tool.
// Appended to the agent's system prompt when browser access (agent-browser skill) is enabled.
func GetAgentBrowserQuickStartInstructions() string {
	return `## Browser Automation (Quick Start)

You have the ` + "`agent_browser`" + ` tool for browser automation. Basic workflow:

1. **Open a page:** agent_browser(command="open", args=["https://example.com"], session="default")
2. **Take a snapshot** to see interactive elements: agent_browser(command="snapshot", args=["-i"], session="default")
3. **Interact** using element refs from snapshot:
   - Click: agent_browser(command="click", args=["@e1"], session="default")
   - Fill text: agent_browser(command="fill", args=["@e2", "search query"], session="default")
   - Press key: agent_browser(command="press", args=["Enter"], session="default")
4. **Re-snapshot** after each interaction to see the updated page state
5. **Screenshot:** agent_browser(command="screenshot", args=["page.png"], session="default")

Key commands: open, snapshot, click, fill, type, press, screenshot, wait, get, scroll, select, hover, upload, download, close, eval, back, forward, reload.

**In code execution mode:** Call agent_browser via HTTP API:
` + "```python\nimport requests, os\nurl = os.environ[\"MCP_API_URL\"] + \"/tools/mcp/workspace_browser/agent_browser\"\nheaders = {\"Authorization\": f\"Bearer {os.environ['MCP_API_TOKEN']}\", \"Content-Type\": \"application/json\"}\nresp = requests.post(url, json={\"command\": \"snapshot\", \"args\": [\"-i\"], \"session\": \"default\"}, headers=headers)\nprint(resp.json()[\"result\"])\n```" + `

For detailed usage, read: execute_shell_command(command="cat /app/workspace-docs/skills/agent-browser/SKILL.md")`
}

// GetCdpBrowserInstructions returns a single merged section for CDP mode (agent_browser + CDP behaviors).
func GetCdpBrowserInstructions() string {
	return `## Browser Automation (CDP — Connected to User's Chrome)

You have the ` + "`agent_browser`" + ` tool controlling the **user's real Chrome browser** via Chrome DevTools Protocol.

### Basic Workflow
1. **Open a page:** agent_browser(command="open", args=["--cdp", "http://host.docker.internal:9222", "https://example.com"], session="default")
2. **Take a snapshot** to see interactive elements: agent_browser(command="snapshot", args=["--cdp", "http://host.docker.internal:9222", "-i"], session="default")
3. **Interact** using element refs from snapshot:
   - Click: agent_browser(command="click", args=["--cdp", "http://host.docker.internal:9222", "@e1"], session="default")
   - Fill text: agent_browser(command="fill", args=["--cdp", "http://host.docker.internal:9222", "@e2", "search query"], session="default")
   - Press key: agent_browser(command="press", args=["--cdp", "http://host.docker.internal:9222", "Enter"], session="default")
4. **Re-snapshot** after each interaction to see the updated page state
5. **Screenshot:** agent_browser(command="screenshot", args=["--cdp", "http://host.docker.internal:9222", "page.png"], session="default")

Key commands: open, snapshot, click, fill, type, press, screenshot, wait, get, scroll, select, hover, upload, download, close, eval, back, forward, reload.

### CDP-Specific Behaviors
- The user can **see everything you do** in their browser — actions are visible in real-time
- The browser may have **existing cookies, login sessions, and tabs** — leverage authenticated sessions without re-logging in
- **Do NOT call close** unless the user asks — it will close their browser tab
- Sessions **persist across tool calls** — you don't need to re-open pages between interactions
- If a site requires login and the user is already logged in, navigate directly to the target page

### CDP Connection
Always pass ` + "`--cdp http://host.docker.internal:9222`" + ` in the args array for every agent_browser call. Do NOT use localhost.

**In code execution mode:** Call agent_browser via HTTP API:
` + "```python\nimport requests, os\nurl = os.environ[\"MCP_API_URL\"] + \"/tools/mcp/workspace_browser/agent_browser\"\nheaders = {\"Authorization\": f\"Bearer {os.environ['MCP_API_TOKEN']}\", \"Content-Type\": \"application/json\"}\nresp = requests.post(url, json={\"command\": \"snapshot\", \"args\": [\"--cdp\", \"http://host.docker.internal:9222\", \"-i\"], \"session\": \"default\"}, headers=headers)\nprint(resp.json()[\"result\"])\n```" + `
**As direct tool call:** agent_browser(command="snapshot", args=["--cdp", "http://host.docker.internal:9222", "-i"], session="default")

### Advanced: Direct CDP WebSocket Access
For operations that need more control (targeting specific tabs, running complex JS, inspecting DOM):
` + "```python\nimport json, websocket\n\n# 1. List open tabs\nimport requests\ntabs = requests.get('http://host.docker.internal:9222/json/list', headers={'Host': 'localhost'}).json()\nfor t in tabs:\n    print(f\"{t['id']}: {t['title']} - {t['url']}\")\n\n# 2. Connect to a specific tab (use suppress_origin=True)\ntarget_id = tabs[0]['id']\nws = websocket.create_connection(\n    f'ws://host.docker.internal:9222/devtools/page/{target_id}',\n    header=['Host: localhost'], suppress_origin=True\n)\n\n# 3. Run JS on the page\nws.send(json.dumps({'id': 1, 'method': 'Runtime.evaluate', 'params': {'expression': 'document.title', 'returnByValue': True}}))\nresult = json.loads(ws.recv())\nprint(result['result']['result']['value'])\nws.close()\n```" + `
**Rules for direct CDP:** Always use ` + "`Host: localhost`" + ` header and ` + "`suppress_origin=True`" + ` for WebSocket. Prefer agent_browser for standard navigation/interaction — use direct CDP only when you need tab-level control or complex JS evaluation.

For detailed usage, read: execute_shell_command(command="cat /app/workspace-docs/skills/agent-browser/SKILL.md")`
}

// GetHeadlessBrowserInstructions returns a single merged section for headless mode (agent_browser + headless behaviors).
func GetHeadlessBrowserInstructions() string {
	return `## Browser Automation (Headless Container Browser)

You have the ` + "`agent_browser`" + ` tool controlling a **headless Chromium browser** inside a container.

### Basic Workflow
1. **Open a page:** agent_browser(command="open", args=["https://example.com"], session="default")
2. **Take a snapshot** to see interactive elements: agent_browser(command="snapshot", args=["-i"], session="default")
3. **Interact** using element refs from snapshot:
   - Click: agent_browser(command="click", args=["@e1"], session="default")
   - Fill text: agent_browser(command="fill", args=["@e2", "search query"], session="default")
   - Press key: agent_browser(command="press", args=["Enter"], session="default")
4. **Re-snapshot** after each interaction to see the updated page state
5. **Screenshot:** agent_browser(command="screenshot", args=["page.png"], session="default")

Key commands: open, snapshot, click, fill, type, press, screenshot, wait, get, scroll, select, hover, upload, download, close, eval, back, forward, reload.

### Headless-Specific Behaviors
- The browser is **fresh** — no existing cookies, sessions, or tabs. You must login from scratch if needed.
- The user **cannot see** the browser — take **screenshots** to show them what's happening
- You can freely **open and close** tabs/sessions without affecting the user
- Browser state is **ephemeral** — it resets between sessions
- Handle login flows explicitly (fill credentials, handle 2FA via human_feedback if needed)

**In code execution mode:** Call agent_browser via HTTP API:
` + "```python\nimport requests, os\nurl = os.environ[\"MCP_API_URL\"] + \"/tools/mcp/workspace_browser/agent_browser\"\nheaders = {\"Authorization\": f\"Bearer {os.environ['MCP_API_TOKEN']}\", \"Content-Type\": \"application/json\"}\nresp = requests.post(url, json={\"command\": \"snapshot\", \"args\": [\"-i\"], \"session\": \"default\"}, headers=headers)\nprint(resp.json()[\"result\"])\n```" + `

For detailed usage, read: execute_shell_command(command="cat /app/workspace-docs/skills/agent-browser/SKILL.md")`
}

// GetCamofoxInstructions returns system prompt instructions specific to the Camofox stealth browser.
func GetCamofoxInstructions() string {
	return `

## Camofox Stealth Browser

You have access to the Camofox stealth browser — an anti-detect Firefox fork that bypasses bot detection.
Use the camofox MCP tools (snapshot, click, type_text, navigate, etc.) to interact with websites.
Always prefer snapshot over screenshot — it returns an accessibility tree which is much more token-efficient.

### Tab Management (IMPORTANT)

**Before creating a new tab, always check for existing ones:**
1. Call list_tabs() first — reuse an existing tab if one is already open rather than creating a new one.
2. Only call create_tab() if no suitable tab exists.

**Always clean up when done:**
- Close individual tabs with close_tab(tabId="...") when you no longer need them.
- At the very end of your task, call camofox_close_session() to close ALL remaining tabs and free resources.
- Never leave tabs open after completing your work — each run should start fresh.

### File Upload (Camofox)
Camofox does not have a direct file upload tool. To upload a file to a website:
1. Use snapshot(tabId) to find the file input element ref
2. Use camofox_evaluate_js to set the file via JavaScript DataTransfer API or trigger the input programmatically
Note: For headed mode, the user can also manually select a file when the file picker opens.

### Session Persistence

Camofox has built-in session/cookie persistence using named profiles:

**Saving a session (after login):**
1. save_profile(tabId="tab-id", profileId="my-site-login") — saves cookies to a named profile

**Restoring a session (in a new tab/conversation):**
1. create_tab(url="https://example.com") — create a new tab
2. load_profile(tabId="tab-id", profileId="my-site-login") — restore saved cookies
3. refresh(tabId="tab-id") — reload page with restored cookies

**Managing profiles:**
- list_profiles() — see all saved profiles
- delete_profile(profileId="old-profile") — remove a profile

**Importing cookies directly:**
- import_cookies(tabId="tab-id", cookies="[{...}]", userId="user1") — import raw cookie JSON

Always save a profile after successful login so the session can be reused later without re-authentication.

### Downloads

Camofox manages downloads internally (not saved to workspace filesystem):

**Retrieving downloads:**
1. list_downloads(tabId="tab-id") — see all downloaded files
2. get_download(downloadId="id", includeContent=true) — get file content as base64

**Batch downloading resources from a page:**
- batch_download(tabId="tab-id", selector="table.files", types="documents") — extract and download all matching resources

**To save a download to workspace:**
1. Get the download content: get_download(downloadId="id", includeContent=true)
2. Save to workspace using execute_shell_command with base64 decode
`
}
