package orchestrator

import (
	"strings"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// getToolNamesByCategory returns a set of tool names for a given category
// This uses the actual tool creation functions as the source of truth
func getToolNamesByCategory(category string) map[string]bool {
	toolNames := make(map[string]bool)

	switch category {
	case "workspace_tools":
		// Backward compatible - returns all LLM-visible workspace tools.
		executors := virtualtools.CreateWorkspaceAdvancedToolExecutors()
		for toolName := range executors {
			toolNames[toolName] = true
		}
		imageExecutors := virtualtools.CreateWorkspaceImageToolExecutors(virtualtools.ImageGenExecutorConfig{})
		for toolName := range imageExecutors {
			toolNames[toolName] = true
		}
		videoExecutors := virtualtools.CreateWorkspaceVideoToolExecutors(virtualtools.VideoGenExecutorConfig{})
		for toolName := range videoExecutors {
			toolNames[toolName] = true
		}
		browserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutors()
		for toolName := range browserExecutors {
			toolNames[toolName] = true
		}
	case "workspace_advanced":
		// LLM-visible advanced workspace tools
		executors := virtualtools.CreateWorkspaceAdvancedToolExecutors()
		for toolName := range executors {
			toolNames[toolName] = true
		}
		imageExecutors := virtualtools.CreateWorkspaceImageToolExecutors(virtualtools.ImageGenExecutorConfig{})
		for toolName := range imageExecutors {
			toolNames[toolName] = true
		}
		videoExecutors := virtualtools.CreateWorkspaceVideoToolExecutors(virtualtools.VideoGenExecutorConfig{})
		for toolName := range videoExecutors {
			toolNames[toolName] = true
		}
	case "workspace_image":
		executors := virtualtools.CreateWorkspaceImageToolExecutors(virtualtools.ImageGenExecutorConfig{})
		for toolName := range executors {
			toolNames[toolName] = true
		}
	case "human_tools":
		// Get tool names from human tool executors (source of truth)
		executors := virtualtools.CreateHumanToolExecutors()
		for toolName := range executors {
			toolNames[toolName] = true
		}
	case "workspace_browser":
		// Browser automation tools (1 tool: agent_browser)
		executors := virtualtools.CreateWorkspaceBrowserToolExecutors()
		for toolName := range executors {
			toolNames[toolName] = true
		}
	}

	return toolNames
}

// ConvertOldFormatToNewFormat converts old format (categories + tools) to new unified format
// Old: enabledCategories=["workspace_tools"], enabledTools=["read_workspace_file"]
// New: ["workspace_tools:*", "workspace_tools:read_workspace_file"]
//
// If enabledTools already contains entries with ":" (new format), returns them as-is
func ConvertOldFormatToNewFormat(enabledCategories []string, enabledTools []string) []string {
	// Check if enabledTools is already in new format (contains ":")
	if len(enabledTools) > 0 {
		firstEntry := enabledTools[0]
		if strings.Contains(firstEntry, ":") {
			// Already in new format, return as-is (ignore enabledCategories)
			return enabledTools
		}
	}

	// Old format - convert it
	result := make([]string, 0)

	// Convert categories to "category:*" format
	for _, category := range enabledCategories {
		result = append(result, category+":*")
	}

	// Convert specific tools - need to determine category for each tool
	allCategoryTools := make(map[string]string) // toolName -> category
	for _, category := range []string{"workspace_tools", "workspace_advanced", "workspace_browser", "human_tools", "workspace_image"} {
		categoryToolNames := getToolNamesByCategory(category)
		for toolName := range categoryToolNames {
			allCategoryTools[toolName] = category
		}
	}

	// Add specific tools with their category prefix
	for _, toolName := range enabledTools {
		if category, exists := allCategoryTools[toolName]; exists {
			result = append(result, category+":"+toolName)
		} else {
			// Unknown tool, add without category (will be skipped in parsing)
			result = append(result, "unknown:"+toolName)
		}
	}

	return result
}

// FilterCustomToolsByCategory filters custom tools and executors based on enabled tools
// Format: single array with entries like "category:tool" or "category:*"
//   - "workspace_tools:*" → all tools from CreateWorkspaceToolExecutors()
//   - "workspace_tools:read_workspace_file" → specific tool
//   - "human_tools:*" → all tools from CreateHumanToolExecutors()
//   - "human_tools:human_feedback" → specific tool
//
// Category identification uses the actual tool creation functions as the source of truth
// If enabledTools is empty, return all tools (backward compatible - default behavior)
func FilterCustomToolsByCategory(
	allTools []llmtypes.Tool,
	allExecutors map[string]interface{},
	enabledTools []string, // format: "category:tool" or "category:*"
) ([]llmtypes.Tool, map[string]interface{}) {
	// Build a set of enabled tool names
	enabledToolNames := make(map[string]bool)

	// Parse enabled tools array
	for _, entry := range enabledTools {
		// Format: "category:tool" or "category:*"
		// Use SplitN to handle tool names that might contain colons (split only on first colon)
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			// Invalid format, skip
			continue
		}

		category := parts[0]
		toolSpec := parts[1]

		if toolSpec == "*" {
			// Enable all tools from this category
			categoryToolNames := getToolNamesByCategory(category)
			for toolName := range categoryToolNames {
				enabledToolNames[toolName] = true
			}
		} else {
			// Enable specific tool
			enabledToolNames[toolSpec] = true
		}
	}

	// If nothing is specified, return all tools (backward compatible)
	if len(enabledTools) == 0 {
		return allTools, allExecutors
	}

	// Filter tools based on enabled tool names
	var filteredTools []llmtypes.Tool
	filteredExecutors := make(map[string]interface{})

	for _, tool := range allTools {
		toolName := tool.Function.Name

		// Check if tool is in the enabled set
		if enabledToolNames[toolName] {
			filteredTools = append(filteredTools, tool)
			// Include corresponding executor if it exists
			if executor, exists := allExecutors[toolName]; exists {
				filteredExecutors[toolName] = executor
			}
		}
	}

	return filteredTools, filteredExecutors
}

// PreparePhaseAgentTools returns a minimal tool set for phase agents (planning, evaluation, debugging, etc.)
// Phase agents only need shell_command (for file operations) and human tools (for feedback).
// They do NOT need workspace_basic, workspace_git, or other workspace_advanced tools.
func (bo *BaseOrchestrator) PreparePhaseAgentTools() ([]llmtypes.Tool, map[string]interface{}) {
	return FilterCustomToolsByCategory(
		bo.WorkspaceTools,
		bo.WorkspaceToolExecutors,
		[]string{
			"workspace_advanced:execute_shell_command",
			"human_tools:*",
		},
	)
}
