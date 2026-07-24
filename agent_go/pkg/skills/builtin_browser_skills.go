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
			Description: "Use agent-browser through Builder's managed tool. Load version-matched core/specialized skills from the installed CLI, then follow Builder-specific CDP tab ownership, locking, file, and safety rules.",
			Content:     agentBrowserSkillContent,
			Source:      llmtypes.SkillSource{Origin: "builtin"},
		}
	default:
		return nil
	}
}

const agentBrowserSkillContent = `# Agent Browser In Builder

Use the Builder MCP tool ` + "`workspace_browser.agent_browser`" + `. Do not run the ` + "`agent-browser`" + ` CLI directly through shell. Builder's tool owns CDP validation, shared-tab locking, session isolation, and workspace file guards.

## Query Live Mode First

CDP reachability is runtime state. It is never taken from saved conversation
history. Before the first browser action—and again after any availability
error—query Builder's backend-owned status operation:

` + "```python" + `
status = agent_browser("status", [], session="main")
` + "```" + `

If ` + "`effective_mode`" + ` is ` + "`cdp`" + `, use one endpoint from
` + "`authorized_endpoints`" + ` on every later call. If it is ` + "`headless`" + `,
call without ` + "`--cdp`" + `. Do not probe Chrome's ` + "`/json/version`" + ` URL
through shell.

## Load Version-Matched Agent-Browser Skills

The installed CLI bundles skills that exactly match its command surface. Before the first browser action in a task, load the current core overview through the managed tool. In headless mode:

` + "```python" + `
agent_browser("skills", ["get", "core"])
` + "```" + `

In CDP mode, include the configured ` + "`--cdp`" + ` prefix as shown in the CDP wrapper below. Documentation calls do not require a selected tab.

Use ` + "`agent_browser(\"skills\", [\"get\", \"core\", \"--full\"])`" + ` only when the overview does not contain the exact command or flag. Use ` + "`agent_browser(\"skills\", [\"list\"])`" + ` to discover specialized skills and load one only when the task needs it, for example ` + "`dogfood`" + ` for exploratory QA/bug hunts or ` + "`electron`" + ` for desktop Electron apps.

Treat upstream shell examples as logical agent-browser commands. Translate ` + "`agent-browser <command> <args...>`" + ` into ` + "`agent_browser(\"<command>\", [\"<args>\", ...])`" + `; never copy those examples into ` + "`execute_shell_command`" + `.

## Recording Context Handoff

In CDP mode, upstream ` + "`record start`" + ` creates a fresh temporary browser context/page because video cannot be enabled retroactively on the existing context. Builder detects that page and returns an ` + "`AGENTWORKS_RECORDING_CONTEXT`" + ` notice with its real tab id. Discard all refs from the original tab and call ` + "`snapshot`" + ` immediately. Builder blocks interactions until that snapshot succeeds and automatically routes subsequent page actions to the recorded context even if a stale original tab id was supplied. Do not select or create another tab while recording. On ` + "`record stop`" + `, Builder closes the temporary page, restores the original tab, and requires normal snapshot-before-interaction discipline again.

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

Always include an endpoint returned by the latest live status result, such as ` + "`--cdp http://localhost:9222`" + `. Workflow configuration limits which ports can be authorized; live status determines which configured ports are currently reachable. The backend rechecks every auto-mode action and rejects a missing, unavailable, or unconfigured port. When status returns multiple endpoints, they represent independent Chrome profiles/login identities for specialized multi-account testing within this workflow. Choose the intended profile on every call and keep distinct labeled tabs per account. Do not create extra CDP browsers merely because workflows or steps are concurrent.

` + "```python" + `
CDP = ["--cdp", "http://localhost:9222"]

def browser(command, args=None, session="main"):
    return agent_browser(command, CDP + (args or []), session=session)

# Documentation calls are read-only and do not need a selected tab, but still
# carry --cdp so the tool trace remains explicit about this run's mode.
core = browser("skills", ["get", "core"])
` + "```" + `

### Installing a CDP Browser on macOS

If the user wants CDP and no authorized endpoint is available, tell them to
install the default visible Chrome launcher with:

` + "```bash" + `
curl -fsSL 'https://raw.githubusercontent.com/manishiitg/coding-agent-loop/main/scripts/install-chrome-cdp-macOS.sh' | bash
` + "```" + `

For a specialized additional login identity, the same installer accepts a
port and creates a separate app and Chrome profile:

` + "```bash" + `
curl -fsSL 'https://raw.githubusercontent.com/manishiitg/coding-agent-loop/main/scripts/install-chrome-cdp-macOS.sh' | bash -s -- --port 9333
curl http://127.0.0.1:9333/json/version
` + "```" + `

Do not run the installer yourself unless the user explicitly asks you to
install and open a visible local browser. After the endpoint is reachable, the
workflow builder must add it to ` + "`cdp_ports`" + `; the current run cannot
invent or use a port that its prompt did not authorize. Extra profiles are for
specialized multi-login testing, not ordinary workflow concurrency.

Tab handling is mandatory in shared CDP mode:

1. Call ` + "`browser(\"tab\", [])`" + ` first to inspect the current tab list.
2. Reuse the workflow's existing labeled tab first, or a pre-existing tab whose displayed URL exactly matches the target. Tab-list output includes the real ` + "`tN`" + ` id and a query-free URL for this decision.
3. If no suitable tab exists, create one stable labeled tab:

` + "```python" + `
browser("tab", ["new", "--label", "workflow-step-name", "https://example.com"])
` + "```" + `

The backend performs the same exact-URL reuse check atomically before ` + "`tab new`" + `. It returns either ` + "`Reused existing CDP tab`" + ` or ` + "`Created CDP tab`" + ` with the real ` + "`tN`" + ` id and actual URL. Keep that ` + "`tN`" + ` for subsequent calls. If the tab list cannot be refreshed, creation fails instead of risking a duplicate.

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

Never call the top-level ` + "`close`" + ` command in CDP mode; it can disrupt the user's real Chrome session. At normal workflow completion, leave workflow-created labeled tabs open for review. Builder automatically closes only those registered workflow-owned ` + "`tN`" + ` tabs one hour after the final run releases its browser lease; pre-existing or exact-URL-reused user tabs are never part of that cleanup. Use ` + "`browser(\"tab\", [\"close\", \"<owned-label-or-tN>\"])`" + ` only when the user explicitly requests immediate cleanup or the workflow must replace one of its own labeled tabs. Never close a pre-existing user tab.

## QA Evidence and Network Debugging

Keep these operations inside the Builder ` + "`agent_browser`" + ` tool so CDP tab selection and locking remain enforced:

` + "```python" + `
browser("network", ["tab", "t1", "requests"])
browser("console", ["tab", "t1"])
browser("errors", ["tab", "t1"])
browser("record", ["tab", "t1", "start", "qa-run.webm"])
# reproduce the issue
browser("record", ["tab", "t1", "stop"])
` + "```" + `

HAR files and videos can capture credentials or other visible secrets. Create them only when the user or workflow requested QA evidence and review before sharing.

## Headless Rules

Headless mode uses an isolated container browser. It usually has no user cookies or saved login state. Use it for unattended automation when user-authenticated Chrome is not needed.

Use a descriptive session name for parallel work and close headless sessions when done to free browser slots.

## Workflow Downloads

In workflow steps, use the run-scoped ` + "`Downloads/`" + ` folder given in the prompt. Do not read from or write to the root workspace ` + "`Downloads/`" + ` folder unless the prompt explicitly grants it.

CDP caveat: native Chrome downloads can land in the host ` + "`~/Downloads`" + ` folder. If the step prompt grants a read-only host Downloads path, copy the needed file into the run-scoped ` + "`Downloads/`" + ` folder before reading or parsing it. Never write, move, or delete files in host Downloads.

The live step prompt and folder guard are authoritative. If they grant a host Downloads path, that path is readable for the current run even when an older workflow learning says it is inaccessible. After a native download, inspect the granted host folder for newly created completed files before declaring the download unavailable; do not infer current access from historical learnings.

Use workspace-relative paths for downloads/uploads. Builder securely stages
upload inputs for the persistent daemon and stages explicit ` + "`download`" + `
outputs before publishing them into the authorized run folder. A normal click
in visible Chrome can still download into host ` + "`~/Downloads`" + `, which
remains read-only to the agent.

## Selector Discipline

Snapshot refs such as ` + "`@e1`" + ` are session-local. They are fine for immediate actions in the same run, but never persist them in ` + "`main.py`" + `, learnings, db, or KB.

For saved code, use deterministic selectors: test ids, stable id/name, aria-label, role plus accessible name, label, placeholder, visible text, or structural CSS only as a last resort.

After every navigation or major action, re-snapshot before the next action.

## Auth Checks

For authenticated sites, verify by page content, not just URL load. For X/Twitter, ` + "`https://x.com/home`" + ` loading is not enough; confirm the authenticated home feed or account UI is visible. A sign-in page means connected but not authenticated.
`
