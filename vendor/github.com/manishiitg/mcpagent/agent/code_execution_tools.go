package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpcache/openapi"
)

// handleGetAPISpec handles the get_api_spec virtual tool.
// Returns the full OpenAPI spec for the requested tool(s) on a server.
// tool_name is required — accepts a single string or an array of strings.
// The system prompt already lists all servers and tool names, so no "list only" mode is needed.
func (a *Agent) handleGetAPISpec(ctx context.Context, args map[string]interface{}) (string, error) {
	serverName, ok := args["server_name"].(string)
	if !ok || serverName == "" {
		return "", fmt.Errorf("server_name parameter is required")
	}

	// Parse tool_name: accepts string or []string (JSON array)
	var toolNames []string
	if raw, exists := args["tool_name"]; exists && raw != nil {
		switch v := raw.(type) {
		case string:
			if v != "" {
				toolNames = append(toolNames, v)
			}
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					toolNames = append(toolNames, s)
				}
			}
		}
	}
	if len(toolNames) == 0 {
		return "", fmt.Errorf("tool_name parameter is required (string or array of strings)")
	}

	// Normalize: hyphens to underscores
	serverName = strings.ReplaceAll(serverName, "-", "_")

	// Build cache key from sorted tool names
	sortedNames := make([]string, len(toolNames))
	copy(sortedNames, toolNames)
	sort.Strings(sortedNames)
	cacheKey := serverName + ":" + strings.Join(sortedNames, ",")

	// Check cache
	a.openAPISpecCacheMu.RLock()
	if a.openAPISpecCache != nil {
		if cached, exists := a.openAPISpecCache[cacheKey]; exists {
			a.openAPISpecCacheMu.RUnlock()
			return string(cached), nil
		}
	}
	a.openAPISpecCacheMu.RUnlock()

	// Determine the API base URL
	baseURL := a.APIBaseURL
	if baseURL == "" {
		baseURL = os.Getenv("MCP_API_URL")
	}
	if baseURL == "" {
		baseURL = "http://localhost:8000"
	}

	// Build a set for fast lookup
	wantTools := make(map[string]bool, len(toolNames))
	for _, n := range toolNames {
		wantTools[n] = true
	}

	// Check if this is a custom tool category
	isCustomCategory := a.toolFilter.IsCategoryDirectory(serverName) ||
		a.toolFilter.IsCategoryDirectory(serverName+"_tools")

	if isCustomCategory {
		customToolsForSpec := make(map[string]openapi.CustomToolForOpenAPI)
		for toolName, ct := range a.customTools {
			if ct.Category == serverName || openapi.GetPackageName(ct.Category) == serverName+"_tools" {
				if wantTools[toolName] {
					customToolsForSpec[toolName] = openapi.CustomToolForOpenAPI{
						Definition: ct.Definition,
						Category:   ct.Category,
					}
				}
			}
		}
		if len(customToolsForSpec) == 0 {
			// Collect available tools in this category for helpful error message
			var availableInCategory []string
			for toolName, ct := range a.customTools {
				if ct.Category == serverName || openapi.GetPackageName(ct.Category) == serverName+"_tools" {
					availableInCategory = append(availableInCategory, toolName)
				}
			}
			sort.Strings(availableInCategory)
			return "", fmt.Errorf("tool(s) %v not found in category %q. Available tools in %q: %v", toolNames, serverName, serverName, availableInCategory)
		}

		specBytes, err := openapi.GenerateCustomToolsOpenAPISpec(serverName, customToolsForSpec, baseURL)
		if err != nil {
			return "", fmt.Errorf("failed to generate OpenAPI spec for %s: %w", serverName, err)
		}
		a.cacheSpec(cacheKey, specBytes)
		return string(specBytes), nil
	}

	// MCP server
	if !a.serverIsAvailable(serverName) {
		// Build list of available servers and custom tool categories for helpful error
		availableSet := make(map[string]bool)
		for _, srvName := range a.toolToServer {
			if srvName == "custom" {
				continue
			}
			normalized := strings.ReplaceAll(srvName, "-", "_")
			shouldInclude := a.toolFilter.ShouldIncludeServer(srvName)
			if !shouldInclude {
				shouldInclude = a.toolFilter.ShouldIncludeServer(normalized)
			}
			if shouldInclude {
				availableSet[normalized] = true
			}
		}
		// Add custom tool categories
		for _, ct := range a.customTools {
			if ct.Category != "" {
				availableSet[ct.Category] = true
			}
		}
		availableServers := make([]string, 0, len(availableSet))
		for s := range availableSet {
			availableServers = append(availableServers, s)
		}
		sort.Strings(availableServers)
		return "", fmt.Errorf("server %q is not available. Available servers/categories: %v. Use get_api_spec(server_name=\"<server>\", tool_name=\"<tool>\") with one of these server names", serverName, availableServers)
	}

	toolSource := a.Tools
	if a.UseCodeExecutionMode && len(a.allMCPToolDefs) > 0 {
		toolSource = a.allMCPToolDefs
	}

	var serverTools []llmtypes.Tool
	for toolName, srvName := range a.toolToServer {
		normalizedSrv := strings.ReplaceAll(srvName, "-", "_")
		if normalizedSrv == serverName && srvName != "custom" && wantTools[toolName] {
			for _, t := range toolSource {
				if t.Function != nil && t.Function.Name == toolName {
					serverTools = append(serverTools, t)
					break
				}
			}
		}
	}

	if len(serverTools) == 0 {
		// Collect available tools on this server
		var availableOnServer []string
		for toolName, srvName := range a.toolToServer {
			normalized := strings.ReplaceAll(srvName, "-", "_")
			if normalized == serverName && srvName != "custom" {
				availableOnServer = append(availableOnServer, toolName)
			}
		}
		sort.Strings(availableOnServer)
		return "", fmt.Errorf("tool(s) %v not found on server %q. Available tools on %q: %v", toolNames, serverName, serverName, availableOnServer)
	}

	specBytes, err := openapi.GenerateServerOpenAPISpec(serverName, serverTools, baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to generate OpenAPI spec for %s: %w", serverName, err)
	}
	a.cacheSpec(cacheKey, specBytes)

	if a.Logger != nil {
		a.Logger.Info("Generated OpenAPI spec",
			loggerv2.String("server", serverName),
			loggerv2.Int("tools_requested", len(toolNames)),
			loggerv2.Int("tools_found", len(serverTools)),
			loggerv2.Int("spec_bytes", len(specBytes)))
	}

	return string(specBytes), nil
}

// serverIsAvailable checks if a server passes the tool filter.
func (a *Agent) serverIsAvailable(serverName string) bool {
	if a.toolFilter.ShouldIncludeServer(serverName) {
		return true
	}
	serverNameWithHyphen := strings.ReplaceAll(serverName, "_", "-")
	return a.toolFilter.ShouldIncludeServer(serverNameWithHyphen)
}

// cacheSpec stores a generated spec in the cache.
func (a *Agent) cacheSpec(key string, specBytes []byte) {
	a.openAPISpecCacheMu.Lock()
	if a.openAPISpecCache == nil {
		a.openAPISpecCache = make(map[string][]byte)
	}
	a.openAPISpecCache[key] = specBytes
	a.openAPISpecCacheMu.Unlock()
}

// buildToolIndex returns a JSON index of available servers and their tool names.
// This is included in the system prompt so the LLM knows what's available.
// It builds the index purely from agent internal state (no filesystem scanning).
func (a *Agent) buildToolIndex() (string, error) {
	type ServerInfo struct {
		Tools []string `json:"tools"`
	}

	index := make(map[string]ServerInfo)

	// Build MCP server tool index from toolToServer mapping
	serverToolsMap := make(map[string]map[string]bool)
	for toolName, serverName := range a.toolToServer {
		if serverName == "custom" {
			continue // Custom tools are handled separately
		}

		// Apply server-level filtering
		shouldInclude := a.toolFilter.ShouldIncludeServer(serverName)
		if !shouldInclude {
			normalized := strings.ReplaceAll(serverName, "-", "_")
			shouldInclude = a.toolFilter.ShouldIncludeServer(normalized)
		}
		if !shouldInclude {
			continue
		}

		normalized := strings.ReplaceAll(serverName, "-", "_")
		if serverToolsMap[normalized] == nil {
			serverToolsMap[normalized] = make(map[string]bool)
		}
		serverToolsMap[normalized][toolName] = true
	}

	for serverName, toolsSet := range serverToolsMap {
		tools := make([]string, 0, len(toolsSet))
		for toolName := range toolsSet {
			tools = append(tools, toolName)
		}
		sort.Strings(tools)
		index[serverName] = ServerInfo{Tools: tools}
	}

	// Add custom tools grouped by category to the tool index.
	// Even in code execution mode, custom tools must appear here so that Claude Code
	// (which uses the MCP bridge and can only discover tools via get_api_spec) can
	// find and call them via HTTP API. For non-Claude-Code providers, the tools are
	// also available as direct LLM calls — having them in the index is harmless.
	customToolsByCategory := make(map[string][]string)
	for toolName, ct := range a.customTools {
		category := ct.Category
		if category == "" {
			continue
		}
		customToolsByCategory[category] = append(customToolsByCategory[category], toolName)
	}
	for category, tools := range customToolsByCategory {
		sort.Strings(tools)
		index[category] = ServerInfo{Tools: tools}
	}

	jsonData, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal tool index: %w", err)
	}

	if a.Logger != nil {
		totalTools := 0
		for _, pkg := range index {
			totalTools += len(pkg.Tools)
		}
		a.Logger.Info("Built tool index",
			loggerv2.Int("servers", len(index)),
			loggerv2.Int("total_tools", totalTools))
	}

	return string(jsonData), nil
}

// getAgentGeneratedDir returns the agent-specific generated directory
// Format: generated/agents/<trace_id>/
// Only creates the directory if code execution mode is enabled
func (a *Agent) getAgentGeneratedDir() string {
	baseDir := a.getGeneratedDir()
	agentDir := filepath.Join(baseDir, "agents", string(a.TraceID))

	if a.UseCodeExecutionMode {
		if err := os.MkdirAll(agentDir, 0755); err != nil { //nolint:gosec // 0755 permissions are intentional for user-accessible directories
			if a.Logger != nil {
				a.Logger.Warn("Failed to create agent generated directory", loggerv2.String("agent_dir", agentDir), loggerv2.Error(err))
			}
		}
	}

	return agentDir
}

// BuildSafeEnvironment creates a minimal, safe environment for shell commands.
// Only includes essential variables, excludes all secrets.
// Exported so it can be used by workspace security and other packages.
func BuildSafeEnvironment() []string {
	return []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/tmp",
		"USER=agent",
		"SHELL=/bin/sh",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}
}
