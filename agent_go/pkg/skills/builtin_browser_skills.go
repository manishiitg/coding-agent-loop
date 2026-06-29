package skills

import "github.com/manishiitg/multi-llm-provider-go/llmtypes"

// IsBuiltinSkill reports whether folderName is served from the hardcoded
// builtin registry rather than the workspace skills/ folder. Builtin names
// must not exist on disk — a disk copy would be shadowed at attach time and
// could carry contradictory guidance.
func IsBuiltinSkill(folderName string) bool {
	return builtinAttachableSkill(folderName) != nil
}

func builtinAttachableSkill(folderName string) *llmtypes.Skill {
	switch folderName {
	case "agent-browser":
		return &llmtypes.Skill{
			Name:        "agent-browser",
			Description: "Builder-specific agent_browser automation rules for CDP/shared Chrome, tab selection, authenticated browser checks, screenshots, scraping, form fills, and downloads.",
			Content:     agentBrowserSkillContent,
			Source:      llmtypes.SkillSource{Origin: "builtin"},
		}
	case "playwright":
		return &llmtypes.Skill{
			Name:        "playwright",
			Description: "Builder-specific Playwright MCP automation rules for deterministic browser automation, run-scoped Downloads, and shared-vs-isolated browser handling.",
			Content:     playwrightSkillContent,
			Source:      llmtypes.SkillSource{Origin: "builtin"},
		}
	default:
		return nil
	}
}

const agentBrowserSkillContent = `# Agent Browser In Builder

Use the Builder MCP tool ` + "`workspace_browser.agent_browser`" + `. Do not run the ` + "`agent-browser`" + ` CLI directly through shell for browser actions unless the task is explicitly documentation/debug discovery such as ` + "`agent-browser skills get core`" + `.

## HTTP Tool Call Pattern

In code execution mode, call the MCP bridge:

` + "```python" + `
import os
import requests

BROWSER = os.environ["MCP_API_URL"] + "/tools/mcp/workspace_browser/agent_browser"
HEADERS = {
    "Authorization": f"Bearer {os.environ['MCP_API_TOKEN']}",
    "Content-Type": "application/json",
}

def agent_browser(command, args=None, session="main"):
    response = requests.post(
        BROWSER,
        json={"command": command, "args": args or [], "session": session},
        headers=HEADERS,
        timeout=120,
    )
    response.raise_for_status()
    payload = response.json()
    if not payload.get("success", False):
        raise RuntimeError(payload.get("error") or payload)
    return payload.get("result")
` + "```" + `

## CDP Shared Chrome Rules

CDP mode controls the user's real Chrome. The user can see the browser and it may already be authenticated.

Always include ` + "`--cdp http://localhost:9222`" + ` in args unless the prompt gives a different CDP endpoint.

` + "```python" + `
CDP = ["--cdp", "http://localhost:9222"]

def browser(command, args=None, session="main"):
    return agent_browser(command, CDP + (args or []), session=session)
` + "```" + `

Tab handling is mandatory in shared CDP mode:

1. Call ` + "`browser(\"tab\", [])`" + ` first to inspect the current tab list.
2. Reuse an existing relevant tab when possible.
3. If no suitable tab exists, create one stable labeled tab:

` + "```python" + `
browser("tab", ["new", "--label", "workflow-step-name", "https://example.com"])
` + "```" + `

4. ` + "`open`" + ` is URL-only. Correct:

` + "```python" + `
browser("open", ["https://x.com/home"])
` + "```" + `

Wrong:

` + "```python" + `
browser("open", ["tab", "t1", "https://x.com/home"])
` + "```" + `

5. After ` + "`open`" + `, include the chosen tab inline for page actions:

` + "```python" + `
snapshot = browser("snapshot", ["tab", "t1", "-i"])
browser("click", ["tab", "t1", "@e1"])
browser("fill", ["tab", "t1", "@e2", "text"])
browser("wait", ["tab", "t1", "3000"])
browser("screenshot", ["tab", "t1", "proof.png"])
` + "```" + `

If a command says shared-browser mode requires selecting or creating a tab, do not treat CDP as unavailable. Select or create a tab, then retry.

Do not call ` + "`close`" + ` in CDP mode unless the user explicitly asks. It can close the user's real tab.

## Headless Rules

Headless mode uses an isolated container browser. It usually has no user cookies or saved login state. Use it for unattended automation when user-authenticated Chrome is not needed.

Use a descriptive session name for parallel work and close headless sessions when done to free browser slots.

## Workflow Downloads

In workflow steps, use the run-scoped ` + "`Downloads/`" + ` folder given in the prompt. Do not read from or write to the root workspace ` + "`Downloads/`" + ` folder unless the prompt explicitly grants it.

CDP caveat: native Chrome downloads can land in the host ` + "`~/Downloads`" + ` folder. If the step prompt grants a read-only host Downloads path, copy the needed file into the run-scoped ` + "`Downloads/`" + ` folder before reading or parsing it. Never write, move, or delete files in host Downloads.

Use workspace-relative paths for downloads/uploads whenever possible.

## Selector Discipline

Snapshot refs such as ` + "`@e1`" + ` are session-local. They are fine for immediate actions in the same run, but never persist them in ` + "`main.py`" + `, learnings, db, or KB.

For saved code, use deterministic selectors: test ids, stable id/name, aria-label, role plus accessible name, label, placeholder, visible text, or structural CSS only as a last resort.

After every navigation or major action, re-snapshot before the next action.

## Auth Checks

For authenticated sites, verify by page content, not just URL load. For X/Twitter, ` + "`https://x.com/home`" + ` loading is not enough; confirm the authenticated home feed or account UI is visible. A sign-in page means connected but not authenticated.
`

const playwrightSkillContent = `# Playwright In Builder

Use the Playwright MCP server tools. Do not use ` + "`agent_browser`" + ` in a Playwright step unless the task explicitly asks to switch browser backends.

Typical tools: ` + "`browser_snapshot`" + `, ` + "`browser_click`" + `, ` + "`browser_type`" + `, ` + "`browser_press`" + `, ` + "`browser_evaluate`" + `, ` + "`browser_run_code`" + `, ` + "`browser_screenshot`" + `, ` + "`browser_file_upload`" + `, and ` + "`browser_close`" + `.

## Core Workflow

Use a tight loop:

1. ` + "`browser_snapshot`" + ` to inspect current page state.
2. Act with Playwright MCP tools or ` + "`browser_run_code`" + `.
3. Re-snapshot after navigation, modal opens, form submission, or DOM changes.
4. Save screenshots when visual proof helps.

## Deterministic Selectors

Snapshot refs are session-local. Use them only for immediate actions in the same run. Never save refs into ` + "`main.py`" + `, learnings, db, or KB.

For saved scripts and durable learnings, use deterministic selectors: test ids, stable id/name, aria-label, role plus accessible name, label, placeholder, visible text, or structural CSS only as a last resort.

Prefer Playwright locator APIs in ` + "`browser_run_code`" + ` for durable actions:

` + "```javascript" + `
await page.getByRole('button', { name: 'Continue' }).click()
await page.getByLabel('Email').fill(email)
await page.getByPlaceholder('Search').fill(query)
` + "```" + `

Avoid brittle generated classes and rotating ids such as Radix ids, React ` + "`:r...:`" + ` ids, Angular Material generated ids, or UUID-like ids.

## Downloads Folder

Builder configures Playwright's download/output directory to the workflow run-scoped ` + "`Downloads/`" + ` folder. Use that folder for all browser downloads, screenshots, generated upload files, and cleanup.

In CDP mode, a browser-native download may still go to the host ` + "`~/Downloads`" + ` folder. When the prompt grants a read-only host Downloads path, import the file by copying it into the run-scoped ` + "`Downloads/`" + ` folder, then work from the copied file.

Use workspace-relative paths when calling tools:

` + "```text" + `
Downloads/input.csv
Downloads/proof.png
` + "```" + `

Do not manually construct absolute host paths unless the step prompt explicitly requires it. Do not use the root workspace ` + "`Downloads/`" + ` folder when the step prompt provides a run-scoped downloads folder.

## Shared Browser Vs Isolated Browser

` + "`share_browser=true`" + ` means a sub-agent reuses the parent browser context. Use this when the sub-agent needs the same logged-in session or page state.

` + "`share_browser=false`" + ` gives the sub-agent an isolated browser session. Use this for parallel browser work, different accounts, unrelated sites, or any task where two agents might navigate/click at the same time.

Do not run multiple parallel agents against the same shared page unless the workflow explicitly coordinates them. They can steal focus, navigate away, or invalidate each other's refs.

Close isolated browser sessions when done. For shared parent browser sessions, close only when the workflow/sub-agent owns the browser lifecycle or the user explicitly asks.

## Page State Checks

Verify real page state after every important action: expected URL, visible required element, success/failure state, and authenticated UI for login-gated pages.

For login-gated sites, a loaded sign-in page is not success.
`
