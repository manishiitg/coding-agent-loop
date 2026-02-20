package mcpagent

import "github.com/manishiitg/multi-llm-provider-go/llmtypes"

// NOTE: Workspace tools have been moved to mcp-agent-builder-go/agent_go/pkg/workspace
// Use workspace.RegisterWorkspaceTools(agent, client) to register workspace tools dynamically.
// This eliminates code duplication and allows mcp-agent-builder-go to be the single source of truth.

// GetWorkspaceToolCategory returns the category name for workspace tools
func GetWorkspaceToolCategory() string {
	return "workspace"
}

// GetHumanToolCategory returns the category name for human tools
func GetHumanToolCategory() string {
	return "human"
}

// GetToolSearchToolCategory returns the category name for tool search tools
func GetToolSearchToolCategory() string {
	return "tool_search"
}

// CreateToolSearchTools creates the search_tools virtual tool for tool search mode
func CreateToolSearchTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	// Add search_tools tool
	searchToolsTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "search_tools",
			Description: "Search for available tools by name or description using regex patterns. Returns matching tools but DOES NOT add them to your toolkit. You must use 'add_tool' to load the tools you find.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search pattern to find tools. Can be:\n- Simple text: 'weather' matches tools with 'weather' in name/description\n- Regex pattern: 'database.*query' matches tools like 'database_query', 'database_raw_query'\n- Case-insensitive: '(?i)slack' matches 'Slack', 'SLACK', 'slack'\n- Alternation: 'file|folder' matches tools with 'file' OR 'folder'\n- Prefix match: 'get_.*' matches all tools starting with 'get_'",
					},
				},
				"required": []string{"query"},
			}),
		},
	}
	tools = append(tools, searchToolsTool)

	// Add add_tool tool
	addToolTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "add_tool",
			Description: "Add one or more tools to your available tools. Use this after finding tools with search_tools.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tool_names": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Array of exact names of the tools to add (e.g., ['read_file', 'weather_get']).",
					},
				},
				"required": []string{"tool_names"},
			}),
		},
	}
	tools = append(tools, addToolTool)

	// Add show_all_tools tool
	showAllToolsTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "show_all_tools",
			Description: "List all available tool names. Returns names only - use search_tools with a tool name to get its description.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}),
		},
	}
	tools = append(tools, showAllToolsTool)

	return tools
}
