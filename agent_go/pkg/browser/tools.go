package browser

import (
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetToolDefinition returns the tool definition for the agent_browser tool
func GetToolDefinition() llmtypes.Tool {
	return llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "agent_browser",
			Description: "Execute browser automation commands using agent-browser CLI. Call command='status' before first browser use to query the live effective mode and reachable authorized CDP endpoints; CDP availability is never inferred from saved chat state. Supports navigation, interaction, screenshots, video recording, console/errors, network inspection/HAR, and data extraction. It also exposes version-matched documentation through command='skills'. Multiple configured CDP ports are only for specialized multi-login testing inside one workflow. IMPORTANT: Load the core agent-browser skill before first use, then follow the snapshot → interact → re-snapshot workflow.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command to execute. Call status first; it is a backend-only live check and takes no args. Then call skills with args=['get','core'] in headless mode or with the status-returned --cdp endpoint in CDP mode. Use ['get','core','--full'] only when the overview lacks an exact command, and ['list'] to discover specialized version-matched skills. Browser commands include open, snapshot, click, fill, type, press, screenshot, wait, get, scroll, select, hover, upload, download, eval, console, errors, network (requests/route/HAR), record (start/stop WebM), trace, profiler, back, forward, reload, close. Use reset only to force-kill a genuinely broken browser session and clear all state. In shared CDP mode, do NOT reset for a missing-tab error; use command='tab' to get the selected tab hint, select a known tab, or create a stable labeled tab, then retry open with URL-only args.",
					},
					"args": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Arguments for the command. Do not include the command name inside args. status takes no args. Headless mode has no tab requirement. When live status reports CDP, EVERY later call, including skills documentation calls, must begin with ['--cdp', '<status-authorized-endpoint>']; the backend rechecks and rejects unavailable or unconfigured ports. When multiple profiles are reachable, choose the endpoint for the intended login/account on every call and keep separate labeled tabs per profile. A skills documentation call does not need a tab. For browser actions, command='tab' with no additional args lists tabs; create a stable tab with ['--cdp','<endpoint>','new','--label','<label>','<url>']. All other CDP page actions must then include the tab inline. CDP open remains URL-only after the --cdp prefix because it uses the selected tab.",
					},
					"session": map[string]interface{}{
						"type":        "string",
						"description": "Session name for the browser instance. Required. Use different session names to run multiple browsers in parallel.",
					},
				},
				"required": []string{"command", "session"},
			}),
		},
	}
}
