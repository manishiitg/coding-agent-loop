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
						"description": "Command to execute. Common commands: open (navigate to URL), snapshot (get page elements with refs like @e1), click (click element by ref), fill (clear and type text), type (type without clearing), press (keyboard key), screenshot (capture image), wait (wait for element/text/URL/network), get (get text/html/value/attr/title/url/count), scroll (scroll page/element into view), select (dropdown option), hover (hover over element), upload (file to input), download (file by clicking), close (close browser), eval (run JavaScript), back, forward, reload. Use reset to force-kill a broken session and clear all state — do this when open/close keep failing with CDP errors, then call open again for a clean start.",
					},
					"args": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Arguments for the command. Examples: ['https://google.com'] for open, ['-i'] for snapshot (interactive elements), ['@e1'] for click, ['@e2', 'search query'] for fill, ['Enter'] for press, ['page.png'] for screenshot, ['text', '@e1'] for get.",
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
