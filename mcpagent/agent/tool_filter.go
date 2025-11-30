package mcpagent

import (
	"strings"

	"mcpagent/logger"
	"mcpagent/mcpclient"
)

// ToolFilter centralizes all tool filtering logic to ensure consistency
// between LLM tool registration (agent.go) and discovery (code_execution_tools.go)
type ToolFilter struct {
	selectedTools        []string        // "server:tool" or "package:tool" format
	selectedServers      []string        // server names for "all tools" mode
	customToolCategories map[string]bool // known custom tool categories (e.g., "workspace", "human")
	mcpServerNames       map[string]bool // known MCP server names from Clients
	logger               logger.ExtendedLogger

	// Pre-computed lookup maps for efficient filtering
	normalizedToolSet        map[string]bool // normalized "package:tool" -> true
	serversWithAllTools      map[string]bool // servers with "server:*" pattern
	serversWithSpecificTools map[string]bool // servers with specific tools selected

	// System custom tool categories that should be included by default (like virtual tools)
	// These are workspace_tools and human_tools which are system tools, not MCP tools
	systemCategories map[string]bool
}

// NewToolFilter creates a new tool filter with the given configuration
// Parameters:
//   - selectedTools: list of "server:tool" or "package:*" patterns
//   - selectedServers: list of server names (for "all tools" mode)
//   - clients: MCP client map to identify MCP servers
//   - customCategories: list of custom tool category names (e.g., "workspace", "human")
//   - logger: for debug logging
func NewToolFilter(
	selectedTools []string,
	selectedServers []string,
	clients map[string]mcpclient.ClientInterface,
	customCategories []string,
	logger logger.ExtendedLogger,
) *ToolFilter {
	tf := &ToolFilter{
		selectedTools:            selectedTools,
		selectedServers:          selectedServers,
		customToolCategories:     make(map[string]bool),
		mcpServerNames:           make(map[string]bool),
		logger:                   logger,
		normalizedToolSet:        make(map[string]bool),
		serversWithAllTools:      make(map[string]bool),
		serversWithSpecificTools: make(map[string]bool),
		systemCategories:         make(map[string]bool),
	}

	// Initialize system categories that should always be included (like virtual tools)
	// These are workspace_tools and human_tools - system tools that should be available
	// regardless of MCP tool filtering, unless explicitly excluded
	systemCats := []string{"workspace", "human"}
	for _, cat := range systemCats {
		tf.systemCategories[cat] = true
		tf.systemCategories[cat+"_tools"] = true
	}

	// Build custom category lookup
	for _, cat := range customCategories {
		tf.customToolCategories[cat] = true
		// Also add the _tools suffix version for directory matching
		tf.customToolCategories[cat+"_tools"] = true
	}

	// Build MCP server name lookup (normalized)
	if clients != nil {
		for serverName := range clients {
			normalized := tf.NormalizeServerName(serverName)
			tf.mcpServerNames[normalized] = true
			tf.mcpServerNames[serverName] = true // Keep original too
		}
	}

	// Pre-compute lookup maps from selectedTools
	for _, fullName := range selectedTools {
		parts := strings.SplitN(fullName, ":", 2)
		if len(parts) == 2 {
			serverOrPkg := parts[0]
			toolName := parts[1]
			normalizedServer := tf.NormalizeServerName(serverOrPkg)

			if toolName == "*" {
				// "server:*" means all tools from this server/package
				tf.serversWithAllTools[normalizedServer] = true
				tf.serversWithAllTools[serverOrPkg] = true
			} else {
				// Specific tool selected
				tf.serversWithSpecificTools[normalizedServer] = true
				tf.serversWithSpecificTools[serverOrPkg] = true

				// Store normalized full name for exact lookup
				normalizedFull := normalizedServer + ":" + tf.NormalizeToolName(toolName)
				tf.normalizedToolSet[normalizedFull] = true
				// Also store original format
				tf.normalizedToolSet[fullName] = true
			}
		}
	}

	if logger != nil {
		logger.Debugf("🔧 [TOOL_FILTER] Created filter - selectedTools: %v, selectedServers: %v", selectedTools, selectedServers)
		logger.Debugf("🔧 [TOOL_FILTER] MCP servers: %v, Custom categories: %v", tf.mcpServerNames, tf.customToolCategories)
		logger.Debugf("🔧 [TOOL_FILTER] serversWithAllTools: %v, serversWithSpecificTools: %v", tf.serversWithAllTools, tf.serversWithSpecificTools)
	}

	return tf
}

// NormalizeServerName normalizes server/package names for comparison
// Handles hyphen vs underscore differences (e.g., "google-sheets" vs "google_sheets")
func (tf *ToolFilter) NormalizeServerName(name string) string {
	// Replace hyphens with underscores and lowercase
	return strings.ToLower(strings.ReplaceAll(name, "-", "_"))
}

// NormalizeToolName converts tool names to a consistent format for comparison
// Handles: snake_case, PascalCase, kebab-case
// All converted to lowercase snake_case
func (tf *ToolFilter) NormalizeToolName(name string) string {
	// First normalize: replace hyphens with underscores
	normalized := strings.ReplaceAll(name, "-", "_")

	// If already contains underscores, just lowercase
	if strings.Contains(normalized, "_") {
		return strings.ToLower(normalized)
	}

	// Convert PascalCase/camelCase to snake_case
	var result strings.Builder
	for i, r := range normalized {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// IsNoFilteringActive returns true if no filtering is configured
// (both selectedTools and selectedServers are empty)
func (tf *ToolFilter) IsNoFilteringActive() bool {
	return len(tf.selectedTools) == 0 && len(tf.selectedServers) == 0
}

// IsCategoryDirectory checks if a directory name represents a custom tool category
// Uses explicit category list instead of "not in Clients" check
func (tf *ToolFilter) IsCategoryDirectory(dirName string) bool {
	// Check against known custom categories
	normalized := tf.NormalizeServerName(dirName)

	// Direct category match (e.g., "workspace_tools")
	if tf.customToolCategories[normalized] {
		return true
	}

	// Check without _tools suffix (e.g., "workspace")
	withoutSuffix := strings.TrimSuffix(normalized, "_tools")
	if withoutSuffix != normalized && tf.customToolCategories[withoutSuffix] {
		return true
	}

	// If it's NOT a known MCP server and ends with _tools, treat as category
	// This handles dynamically registered custom tool categories
	if strings.HasSuffix(normalized, "_tools") {
		serverName := strings.TrimSuffix(normalized, "_tools")
		if !tf.mcpServerNames[serverName] && !tf.mcpServerNames[normalized] {
			return true
		}
	}

	return false
}

// IsVirtualToolsDirectory checks if a directory is the virtual_tools directory
func (tf *ToolFilter) IsVirtualToolsDirectory(dirName string) bool {
	return dirName == "virtual_tools"
}

// ShouldIncludeTool checks if a tool should be included based on filtering configuration
// This is the main filtering method used by both agent.go and code_execution_tools.go
//
// Parameters:
//   - packageOrServer: the server name (for MCP tools) or package name (for custom tools)
//   - toolName: the tool/function name
//   - isCustomTool: true if this is a custom tool (workspace, human, etc.), false for MCP tools
//   - isVirtualTool: true if this is a virtual tool (get_prompt, get_resource, etc.)
//
// Returns true if the tool should be included
func (tf *ToolFilter) ShouldIncludeTool(packageOrServer string, toolName string, isCustomTool bool, isVirtualTool bool) bool {
	// Virtual tools are ALWAYS included (system tools)
	if isVirtualTool {
		if tf.logger != nil {
			tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s included (virtual tool, always included)", packageOrServer, toolName)
		}
		return true
	}

	// If no filtering is active, include all tools
	if tf.IsNoFilteringActive() {
		if tf.logger != nil {
			tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s included (no filtering active)", packageOrServer, toolName)
		}
		return true
	}

	// Check if this is a system category (workspace_tools, human_tools)
	// System categories are included by default unless they have specific tools selected
	// (in which case only those specific tools are included)
	normalizedPkgForSystem := tf.NormalizeServerName(packageOrServer)
	if tf.systemCategories[normalizedPkgForSystem] || tf.systemCategories[packageOrServer] {
		// Check if this system category has specific tools selected
		// If not, include ALL tools from this category by default
		if !tf.serversWithSpecificTools[normalizedPkgForSystem] && !tf.serversWithSpecificTools[packageOrServer] {
			if tf.logger != nil {
				tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s included (system category, included by default)", packageOrServer, toolName)
			}
			return true
		}
		// System category has specific tools selected - fall through to check those
	}

	// Normalize names for comparison
	normalizedPkg := tf.NormalizeServerName(packageOrServer)
	normalizedTool := tf.NormalizeToolName(toolName)

	// Check if this package/server has "all tools" pattern (package:*)
	if tf.serversWithAllTools[normalizedPkg] || tf.serversWithAllTools[packageOrServer] {
		if tf.logger != nil {
			tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s included (package has '*' pattern)", packageOrServer, toolName)
		}
		return true
	}

	// Check if this package/server has specific tools selected FIRST
	// If specific tools are selected, we should filter to only those tools, even if server is in selectedServers
	if tf.serversWithSpecificTools[normalizedPkg] || tf.serversWithSpecificTools[packageOrServer] {
		// Check if this exact tool is in the selection
		normalizedFull := normalizedPkg + ":" + normalizedTool
		if tf.normalizedToolSet[normalizedFull] {
			if tf.logger != nil {
				tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s included (specific tool selected, normalized: %s)", packageOrServer, toolName, normalizedFull)
			}
			return true
		}

		// Also check original format
		originalFull := packageOrServer + ":" + toolName
		if tf.normalizedToolSet[originalFull] {
			if tf.logger != nil {
				tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s included (specific tool selected, original: %s)", packageOrServer, toolName, originalFull)
			}
			return true
		}

		// Package has specific tools but this one isn't selected
		if tf.logger != nil {
			tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s excluded (package has specific tools but this one not selected)", packageOrServer, toolName)
		}
		return false
	}

	// Check if server is in selectedServers (which means "ALL tools" from this server)
	// This only applies if no specific tools were selected for this server
	// selectedServers means "include all tools from this server" when no specific tools are in selectedTools
	if len(tf.selectedServers) > 0 {
		for _, selectedServer := range tf.selectedServers {
			normalizedSelected := tf.NormalizeServerName(selectedServer)
			if normalizedSelected == normalizedPkg || selectedServer == packageOrServer {
				if tf.logger != nil {
					tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s included (server in selectedServers, no specific tools selected)", packageOrServer, toolName)
				}
				return true
			}
		}
	}

	// Package/server has no tools in selectedTools and is not in selectedServers
	// Check if selectedServers is set but this server is not in it
	if len(tf.selectedServers) > 0 {
		// Server is not in selectedServers and has no specific tools - exclude

		// Not in selectedServers
		// For custom tools, also check if their category is in selectedTools
		if isCustomTool {
			// Custom tools might be filtered by category
			// Check if any tool from this category is selected (which would be in serversWithSpecificTools)
			// If the category isn't mentioned at all in selectedTools, exclude it
			if tf.logger != nil {
				tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s excluded (custom tool, category not in selectedServers or selectedTools)", packageOrServer, toolName)
			}
			return false
		}

		// MCP tool not in selectedServers
		if tf.logger != nil {
			tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s excluded (server not in selectedServers)", packageOrServer, toolName)
		}
		return false
	}

	// No selectedServers configured and no specific tools for this package
	// If selectedTools is set but this server isn't mentioned, EXCLUDE (strict filtering)
	// If selectedTools is empty AND selectedServers is empty, include all (no filtering)
	if len(tf.selectedTools) > 0 {
		// selectedTools is set but this package isn't in it at all
		if tf.logger != nil {
			tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s excluded (package not in selectedTools)", packageOrServer, toolName)
		}
		return false
	}

	// No selectedTools and no selectedServers - include all (backwards compatible)
	if tf.logger != nil {
		tf.logger.Debugf("🔧 [TOOL_FILTER] Tool %s:%s included (default: no restrictions on this package)", packageOrServer, toolName)
	}
	return true
}

// ShouldIncludeServer checks if a server/package should be included at all
// Used for server-level filtering before checking individual tools
func (tf *ToolFilter) ShouldIncludeServer(serverName string) bool {
	// If no filtering is active, include all servers
	if tf.IsNoFilteringActive() {
		return true
	}

	normalizedServer := tf.NormalizeServerName(serverName)

	// Check if server has "all tools" pattern
	if tf.serversWithAllTools[normalizedServer] || tf.serversWithAllTools[serverName] {
		return true
	}

	// Check if server has specific tools selected
	if tf.serversWithSpecificTools[normalizedServer] || tf.serversWithSpecificTools[serverName] {
		return true
	}

	// Check if server is in selectedServers
	for _, selected := range tf.selectedServers {
		if tf.NormalizeServerName(selected) == normalizedServer || selected == serverName {
			return true
		}
	}

	// Server not mentioned in any filter
	// If selectedServers is set, exclude servers not in the list
	if len(tf.selectedServers) > 0 {
		return false
	}

	// No selectedServers, so include by default
	return true
}

// GetToolCategory returns the category for a custom tool based on its package name
// Returns empty string if not a custom tool category
func (tf *ToolFilter) GetToolCategory(packageName string) string {
	normalized := tf.NormalizeServerName(packageName)

	// Remove _tools suffix to get category name
	category := strings.TrimSuffix(normalized, "_tools")

	if tf.customToolCategories[category] || tf.customToolCategories[normalized] {
		return category
	}

	return ""
}

// IsSystemCategory checks if a package/category is a system category
// System categories (workspace_tools, human_tools) are included by default
func (tf *ToolFilter) IsSystemCategory(packageName string) bool {
	normalized := tf.NormalizeServerName(packageName)
	return tf.systemCategories[normalized] || tf.systemCategories[packageName]
}
