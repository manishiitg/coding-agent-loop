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
			Description: "Execute browser automation commands using agent-browser CLI. Supports navigation, interaction, screenshots, video recording, console/errors, network inspection/HAR, and data extraction. It also exposes the version-matched documentation bundled with the installed CLI through command='skills'. The system prompt states whether this run is headless or connected to one or more explicitly authorized Chrome CDP profiles. Multiple CDP ports are only for specialized multi-login testing inside one workflow. IMPORTANT: Load the core agent-browser skill before first use, then follow the snapshot → interact → re-snapshot workflow.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command to execute. Before first browser use, call skills with args=['get','core']; use ['get','core','--full'] only when the overview lacks an exact command, and ['list'] to discover specialized version-matched skills such as dogfood for exploratory QA. Browser commands include open, snapshot, click, fill, type, press, screenshot, wait, get, scroll, select, hover, upload, download, eval, console, errors, network (requests/route/HAR), record (start/stop WebM), trace, profiler, back, forward, reload, close. Use reset only to force-kill a genuinely broken browser session and clear all state. In shared CDP mode, do NOT reset for a missing-tab error; use command='tab' to get the selected tab hint, select a known tab, or create a stable labeled tab, then retry open with URL-only args.",
					},
					"args": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Arguments for the command. Do not include the command name inside args. Normal/headless mode has no tab requirement. In CDP mode, EVERY call, including skills documentation calls, must begin with ['--cdp', '<authorized-endpoint>']; the system prompt lists the authorized endpoint(s), and the backend rejects any other port. When multiple profiles are authorized, choose the endpoint for the intended login/account on every call and keep separate labeled tabs per profile. A skills documentation call does not need a tab. For browser actions, command='tab' with no additional args lists tabs; create a stable tab with ['--cdp','<endpoint>','new','--label','<label>','<url>']. All other CDP page actions must then include the tab inline: ['--cdp','<endpoint>','tab','<tab-id-or-label>',...]. Examples: skills ['--cdp','<endpoint>','get','core']; snapshot ['--cdp','<endpoint>','tab','t1','-i']; click ['--cdp','<endpoint>','tab','t1','@e1']; network requests ['--cdp','<endpoint>','tab','t1','requests']; record start ['--cdp','<endpoint>','tab','t1','start','qa-run.webm']; record stop ['--cdp','<endpoint>','tab','t1','stop']. CDP open remains URL-only after the --cdp prefix because it uses the selected tab. Do not repeat the command name inside args.",
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
