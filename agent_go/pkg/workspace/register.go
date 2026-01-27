package workspace

import (
	"context"

	mcpagent "github.com/manishiitg/mcpagent/agent"
)

// RegisterWorkspaceTools registers all workspace tools with an mcpagent.Agent
// This allows the agent to use workspace tools via code execution mode
// The executors will use the provided client for API calls
func RegisterWorkspaceTools(agent *mcpagent.Agent, client *Client) error {
	// Get all tool definitions
	basicTools := GetBasicToolDefinitions()
	advancedTools := GetAdvancedToolDefinitions()
	gitTools := GetGitToolDefinitions()

	// Get all executors
	basicExecutors := NewBasicExecutor(client)
	advancedExecutors := NewAdvancedExecutor(client)
	gitExecutors := NewGitExecutor(client)

	// Register basic tools
	for _, tool := range basicTools {
		if tool.Function == nil {
			continue
		}
		name := tool.Function.Name
		executor := basicExecutors[name]
		if executor == nil {
			continue
		}

		// Convert parameters to map[string]interface{}
		params := toolParamsToMap(tool.Function.Parameters)

		if err := agent.RegisterCustomTool(name, tool.Function.Description, params, executor, "workspace"); err != nil {
			return err
		}
	}

	// Register advanced tools
	for _, tool := range advancedTools {
		if tool.Function == nil {
			continue
		}
		name := tool.Function.Name
		executor := advancedExecutors[name]
		if executor == nil {
			continue
		}

		params := toolParamsToMap(tool.Function.Parameters)

		if err := agent.RegisterCustomTool(name, tool.Function.Description, params, executor, "workspace"); err != nil {
			return err
		}
	}

	// Register git tools
	for _, tool := range gitTools {
		if tool.Function == nil {
			continue
		}
		name := tool.Function.Name
		executor := gitExecutors[name]
		if executor == nil {
			continue
		}

		params := toolParamsToMap(tool.Function.Parameters)

		if err := agent.RegisterCustomTool(name, tool.Function.Description, params, executor, "workspace"); err != nil {
			return err
		}
	}

	return nil
}

// RegisterWorkspaceToolsWithFolderGuard registers workspace tools with folder guard configuration
func RegisterWorkspaceToolsWithFolderGuard(agent *mcpagent.Agent, baseURL string, folderGuard *FolderGuardConfig) error {
	client := NewClient(baseURL, WithFolderGuard(folderGuard))
	return RegisterWorkspaceTools(agent, client)
}

// toolParamsToMap converts tool parameters to map[string]interface{}
func toolParamsToMap(params interface{}) map[string]interface{} {
	if params == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	// The parameters are already stored as a map in llmtypes.Parameters
	// We need to extract the underlying map
	switch p := params.(type) {
	case map[string]interface{}:
		return p
	default:
		// For llmtypes.Parameters, it has a method to get the underlying map
		// Try to marshal and unmarshal to get the map
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
}

// CreateExecutorFunc creates a generic executor function type for use with mcpagent
type ExecutorFunc func(ctx context.Context, args map[string]interface{}) (string, error)

// GetAllExecutors returns all workspace tool executors
func GetAllExecutors(client *Client) map[string]ExecutorFunc {
	executors := make(map[string]ExecutorFunc)

	// Merge all executor maps
	for name, exec := range NewBasicExecutor(client) {
		executors[name] = exec
	}
	for name, exec := range NewAdvancedExecutor(client) {
		executors[name] = exec
	}
	for name, exec := range NewGitExecutor(client) {
		executors[name] = exec
	}
	for name, exec := range NewBrowserExecutor(client) {
		executors[name] = exec
	}

	return executors
}
