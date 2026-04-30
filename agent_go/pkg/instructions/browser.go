package instructions

import (
	"fmt"

	"mcp-agent-builder-go/agent_go/pkg/browser"
	"mcp-agent-builder-go/agent_go/pkg/common"
	"mcp-agent-builder-go/agent_go/pkg/fsutil"
)

// agentBrowserSkillPath returns the absolute shell path to the agent-browser SKILL.md,
// resolved via fsutil.WorkspaceShellRoot() (honors WORKSPACE_SHELL_ROOT / WORKSPACE_DOCS_PATH).
func agentBrowserSkillPath() string {
	return fsutil.WorkspaceShellRoot() + "/skills/agent-browser/SKILL.md"
}

// cdpHost returns the hostname to use in CDP instructions.
// In native mode, use localhost. In Docker mode, use host.docker.internal.
func cdpHost() string {
	if common.IsNativeWorkspace() {
		return "localhost"
	}
	return "host.docker.internal"
}

// BrowserConfig holds the resolved browser state for prompt generation.
type BrowserConfig struct {
	HasPlaywright   bool
	HasAgentBrowser bool
	CdpPort         int    // >0 means CDP mode, 0 means headless (legacy, use Mode when set)
	Mode            string // "cdp", "headless", "playwright", "" (empty = fallback to CdpPort)
	IsIsolated      bool   // true when running in a share_browser=false sub-agent
}

// BuildBrowserInstructions returns the complete browser system prompt
// (upload + mode-specific) for the given config, or "" if no browser tool is active.
func BuildBrowserInstructions(cfg BrowserConfig) string {
	if !cfg.HasPlaywright && !cfg.HasAgentBrowser {
		return ""
	}

	result := ""

	// Use Mode as primary decision, fall back to legacy CdpPort/Has* flags
	isCdp := cfg.Mode == "cdp" || (cfg.Mode == "" && cfg.CdpPort > 0)
	isPlaywright := cfg.Mode == "playwright" || (cfg.Mode == "" && cfg.HasPlaywright)

	if isPlaywright {
		result += "\n" + GetPlaywrightModeInstructions()
	} else if isCdp {
		result += "\n" + GetCdpBrowserInstructions()
	} else {
		result += "\n" + GetHeadlessBrowserInstructions()
	}

	// Add session limits — applies to all browser types
	closeRule := "- Always **close the browser** when done (agent_browser command=\"close\" or browser_close) to free the session slot."
	if isCdp {
		// CDP connects to user's real browser — closing would kill their tab
		closeRule = "- **Do NOT close the browser** when done — it is the user's real browser. Only close if the user explicitly asks."
	}
	result += fmt.Sprintf("\n\n## Browser Session Limits\n"+
		"- **Per agent:** max %d concurrent browser session(s). Do NOT open multiple browsers — use one at a time.\n"+
		"- **Per workflow:** max %d concurrent browser sessions across all agents.\n"+
		"- **Global:** max %d concurrent browser sessions across all workflows.\n"+
		"%s\n"+
		"- **Multiple browsers in a workflow:** Each parallel agent MUST use a **unique session name** "+
		"(e.g. session=\"twitter_research\", session=\"linkedin_lookup\"). "+
		"If two agents both use session=\"default\", they will share the same browser instead of getting separate ones. "+
		"Pick a descriptive name related to the agent's task.",
		browser.MaxBrowserSessionsPerAgent,
		browser.MaxBrowserSessionsPerWorkflow,
		browser.MaxBrowserSessionsGlobal,
		closeRule,
	)

	if cfg.IsIsolated {
		result += "\n- You have an **isolated** browser session. Close it when finished to free the slot for other agents."
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
	host := cdpHost()
	return fmt.Sprintf(`
## Browser Mode: CDP (Connected to User's Chrome)

You are controlling the **user's real Chrome browser** via Chrome DevTools Protocol (CDP).

**Key behaviors:**
- The user can **see everything you do** in their browser ��� actions are visible in real-time
- The browser may have **existing cookies, login sessions, and tabs** — you can leverage authenticated sessions without re-logging in
- **Do NOT call close** unless the user asks — it will close their browser tab
- **Take screenshots** to show the user what you see, since they can also verify visually
- Sessions **persist across tool calls** — you don't need to re-open pages between interactions
- If a site requires login and the user is already logged in, just navigate directly to the target page

**Connection behavior (important):**
- CDP endpoint is already configured by the backend from the selected port.
- Do **NOT** ask the user for a websocket debugger URL or run "curl localhost:9222/json/version".
- When you describe or troubleshoot the endpoint, use %s:<port>.

**Best practices:**
- Start with a **snapshot** to see the current page state before taking any action
- Use **session="default"** unless you need multiple isolated sessions
- Be careful with form submissions and purchases — this is the user's real browser with real accounts
`, host)
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

**Important:** Do NOT use agent_browser tool or the agent-browser CLI via shell. Only use the Playwright browser_* tools listed above.
`
}

// GetAgentBrowserQuickStartInstructions returns inline instructions for using the agent-browser tool.
// Appended to the agent's system prompt when browser access (agent-browser skill) is enabled.
func GetAgentBrowserQuickStartInstructions() string {
	return fmt.Sprintf(`## Browser Automation (Quick Start)

Call agent_browser via HTTP API:

` + "```python\nimport requests, os\nBROWSER = os.environ[\"MCP_API_URL\"] + \"/tools/mcp/workspace_browser/agent_browser\"\nHEADERS = {\"Authorization\": f\"Bearer {os.environ['MCP_API_TOKEN']}\", \"Content-Type\": \"application/json\"}\n\ndef browser(command, args=None, session=\"default\"):\n    resp = requests.post(BROWSER, json={\"command\": command, \"args\": args or [], \"session\": session}, headers=HEADERS, timeout=120)\n    resp.raise_for_status()\n    return resp.json().get(\"result\", \"\")\n\n# Basic workflow\nbrowser(\"open\", [\"https://example.com\"])\nsnap = browser(\"snapshot\", [\"-i\"])   # see interactive elements with refs like @e1\nbrowser(\"click\", [\"@e1\"])\nbrowser(\"fill\", [\"@e2\", \"search query\"])\nbrowser(\"press\", [\"Enter\"])\nsnap = browser(\"snapshot\", [\"-i\"])   # re-snapshot after each interaction\nbrowser(\"screenshot\", [\"page.png\"])\n\n# If open/click/snapshot keep failing with CDP errors — reset and retry:\nbrowser(\"reset\")                      # force-kills daemon, clears all state\nbrowser(\"open\", [\"https://example.com\"])  # fresh start\n```" + `

Key commands: open, snapshot, click, fill, type, press, screenshot, wait, get, scroll, select, hover, upload, download, close, eval, back, forward, reload, reset.

For detailed usage, read: execute_shell_command(command="cat %s")`, agentBrowserSkillPath())
}

// GetCdpBrowserInstructions returns a single merged section for CDP mode (agent_browser + CDP behaviors).
func GetCdpBrowserInstructions() string {
	host := cdpHost()
	cdpURL := fmt.Sprintf("http://%s:9222", host)
	return fmt.Sprintf(`## Browser Automation (CDP — Connected to User's Chrome)

You have the `+"`agent_browser`"+` tool controlling the **user's real Chrome browser** via Chrome DevTools Protocol.

Call agent_browser via HTTP API. Always include `+"`--cdp %[1]s`"+` in args:

`+"```python\nimport requests, os\nBROWSER = os.environ[\"MCP_API_URL\"] + \"/tools/mcp/workspace_browser/agent_browser\"\nHEADERS = {\"Authorization\": f\"Bearer {os.environ['MCP_API_TOKEN']}\", \"Content-Type\": \"application/json\"}\n\ndef browser(command, args=None, session=\"default\"):\n    resp = requests.post(BROWSER, json={\"command\": command, \"args\": [\"--cdp\", \"%[1]s\"] + (args or []), \"session\": session}, headers=HEADERS, timeout=120)\n    resp.raise_for_status()\n    return resp.json().get(\"result\", \"\")\n\nbrowser(\"open\", [\"https://example.com\"])\nsnap = browser(\"snapshot\", [\"-i\"])\nbrowser(\"click\", [\"@e1\"])\nbrowser(\"fill\", [\"@e2\", \"text\"])\nsnap = browser(\"snapshot\", [\"-i\"])  # re-snapshot after each interaction\n```"+`

Key commands: open, snapshot, click, fill, type, press, screenshot, wait, get, scroll, select, hover, upload, download, close, eval, back, forward, reload, reset.

### CDP-Specific Behaviors
- The user can **see everything you do** in their browser — actions are visible in real-time
- The browser may have **existing cookies, login sessions, and tabs** — leverage authenticated sessions without re-logging in
- **Do NOT call close** unless the user asks — it will close their browser tab
- Sessions **persist across tool calls** — you don't need to re-open pages between interactions
- If a site requires login and the user is already logged in, navigate directly to the target page

### Advanced: Direct CDP WebSocket Access
For operations that need more control (targeting specific tabs, running complex JS, inspecting DOM):
`+"```python\nimport json, websocket\n\n# 1. List open tabs\nimport requests\ntabs = requests.get('%[1]s/json/list', headers={'Host': 'localhost'}).json()\nfor t in tabs:\n    print(f\"{t['id']}: {t['title']} - {t['url']}\")\n\n# 2. Connect to a specific tab (use suppress_origin=True)\ntarget_id = tabs[0]['id']\nws = websocket.create_connection(\n    f'ws://%[2]s:9222/devtools/page/{target_id}',\n    header=['Host: localhost'], suppress_origin=True\n)\n\n# 3. Run JS on the page\nws.send(json.dumps({'id': 1, 'method': 'Runtime.evaluate', 'params': {'expression': 'document.title', 'returnByValue': True}}))\nresult = json.loads(ws.recv())\nprint(result['result']['result']['value'])\nws.close()\n```"+`
**Rules for direct CDP:** Always use `+"`Host: localhost`"+` header and `+"`suppress_origin=True`"+` for WebSocket. Prefer agent_browser for standard navigation/interaction — use direct CDP only when you need tab-level control or complex JS evaluation.

For detailed usage, read: execute_shell_command(command="cat %[3]s")`, cdpURL, host, agentBrowserSkillPath())
}

// GetHeadlessBrowserInstructions returns a single merged section for headless mode (agent_browser + headless behaviors).
func GetHeadlessBrowserInstructions() string {
	return fmt.Sprintf(`## Browser Automation (Headless Container Browser)

You have the ` + "`agent_browser`" + ` tool controlling a **headless Chromium browser** inside a container.

Call agent_browser via HTTP API:

` + "```python\nimport requests, os\nBROWSER = os.environ[\"MCP_API_URL\"] + \"/tools/mcp/workspace_browser/agent_browser\"\nHEADERS = {\"Authorization\": f\"Bearer {os.environ['MCP_API_TOKEN']}\", \"Content-Type\": \"application/json\"}\n\ndef browser(command, args=None, session=\"default\"):\n    resp = requests.post(BROWSER, json={\"command\": command, \"args\": args or [], \"session\": session}, headers=HEADERS, timeout=120)\n    resp.raise_for_status()\n    return resp.json().get(\"result\", \"\")\n\nbrowser(\"open\", [\"https://example.com\"])\nsnap = browser(\"snapshot\", [\"-i\"])   # see interactive elements with refs like @e1\nbrowser(\"click\", [\"@e1\"])\nbrowser(\"fill\", [\"@e2\", \"search query\"])\nbrowser(\"press\", [\"Enter\"])\nsnap = browser(\"snapshot\", [\"-i\"])   # re-snapshot after each interaction\n\n# If commands keep failing with CDP errors — reset and retry:\nbrowser(\"reset\")                      # force-kills daemon, clears all state\nbrowser(\"open\", [\"https://example.com\"])  # fresh start\n```" + `

Key commands: open, snapshot, click, fill, type, press, screenshot, wait, get, scroll, select, hover, upload, download, close, eval, back, forward, reload, reset.

### Headless-Specific Behaviors
- The browser is **fresh** — no existing cookies, sessions, or tabs. You must login from scratch if needed.
- The user **cannot see** the browser — take **screenshots** to show them what's happening
- You can freely **open and close** tabs/sessions without affecting the user
- Browser state is **ephemeral** — it resets between sessions
- Handle login flows explicitly (fill credentials, handle 2FA via human_feedback if needed)

For detailed usage, read: execute_shell_command(command="cat %s")`, agentBrowserSkillPath())
}
