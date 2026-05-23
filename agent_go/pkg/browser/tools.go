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
			Description: "Execute browser automation commands using agent-browser CLI. Supports navigation, interaction, screenshots, and data extraction. IMPORTANT: Read the agent-browser skill documentation to understand how to use this tool effectively (snapshot → interact → re-snapshot workflow).",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command to execute. Common commands: open (navigate to URL), snapshot (get page elements with refs like @e1), click (click element by ref), fill (clear and type text), type (type without clearing), press (keyboard key), screenshot (capture image), wait (wait for element/text/URL/network), get (get text/html/value/attr/title/url/count), scroll (scroll page/element into view), select (dropdown option), hover, upload, download, close, eval, back, forward, reload. Use reset only to force-kill a genuinely broken browser session and clear all state. In shared CDP mode, do NOT reset for a missing-tab error; use command='tab' to get the selected tab hint, select a known tab, or create a stable labeled tab, then retry open with URL-only args.",
					},
					"args": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Arguments for the command. Do not include the command name inside args. Normal/headless mode has no tab requirement: use standard agent-browser args such as ['https://google.com'] for open, ['-i'] for snapshot, ['@e1'] for click, ['@e2', 'search query'] for fill, ['Enter'] for press. In shared CDP mode, command='tab' with args=[] tries the real tab list for 15s, then falls back to the selected tab hint if Chrome/CDP is stuck. If no tab is selected yet, create one stable labeled tab with command='tab', args=['new', '--label', '<label>', '<url>']; then call open with URL-only args. All other CDP page actions MUST include the tab inline before the command args: ['tab', '<tab-id-or-label>', ...] or ['--tab', '<tab-id-or-label>', ...]. CDP examples: ['tab', 't1', '-i'] for snapshot, ['tab', 't1', '@e1'] for click, ['tab', 't1', '@e2', 'search query'] for fill. CDP wait examples: ['tab', 't1', '6000'], ['tab', 't1', '--load', 'networkidle'], ['tab', 't1', '--text', 'Welcome']; do not call command='wait' with args ['tab', 't1', 'wait', '6s'].",
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
