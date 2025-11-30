package codeexec

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"mcpagent/logger"
	"mcpagent/mcpclient"

	"github.com/mark3labs/mcp-go/mcp"
)

// GoBuildError is a custom error type for Go build/compilation errors
type GoBuildError struct {
	Message string
	Output  string
}

func (e *GoBuildError) Error() string {
	return e.Message
}

// ToolRegistry holds references to MCP clients, custom tools, and virtual tools for runtime execution
type ToolRegistry struct {
	mcpClients   map[string]mcpclient.ClientInterface
	customTools  map[string]func(ctx context.Context, args map[string]interface{}) (string, error)
	virtualTools map[string]func(ctx context.Context, args map[string]interface{}) (string, error)
	toolToServer map[string]string
	mu           sync.RWMutex
	logger       logger.ExtendedLogger
}

var (
	globalRegistry *ToolRegistry
	registryMu     sync.Mutex
)

// InitRegistry initializes the global tool registry from an agent
func InitRegistry(mcpClients map[string]mcpclient.ClientInterface, customTools map[string]func(ctx context.Context, args map[string]interface{}) (string, error), toolToServer map[string]string, logger logger.ExtendedLogger) {
	InitRegistryWithVirtualTools(mcpClients, customTools, nil, toolToServer, logger)
}

// InitRegistryWithVirtualTools initializes or updates the global tool registry with virtual tools support
// If the registry already exists, it merges new tools/clients into the existing registry
// This allows multiple agents with different servers to coexist
func InitRegistryWithVirtualTools(mcpClients map[string]mcpclient.ClientInterface, customTools map[string]func(ctx context.Context, args map[string]interface{}) (string, error), virtualTools map[string]func(ctx context.Context, args map[string]interface{}) (string, error), toolToServer map[string]string, logger logger.ExtendedLogger) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if virtualTools == nil {
		virtualTools = make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))
	}

	if globalRegistry == nil {
		// First initialization - create new registry
		globalRegistry = &ToolRegistry{
			mcpClients:   make(map[string]mcpclient.ClientInterface),
			customTools:  make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error)),
			virtualTools: make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error)),
			toolToServer: make(map[string]string),
			logger:       logger,
		}
		if logger != nil {
			logger.Debugf("🔧 Creating new tool registry")
		}
	} else {
		// Registry exists - update logger if provided
		if logger != nil && globalRegistry.logger == nil {
			globalRegistry.logger = logger
		}
		if logger != nil {
			logger.Debugf("🔧 Updating existing tool registry (merging new tools/clients)")
		}
	}

	// Merge MCP clients
	for serverName, client := range mcpClients {
		if existing, exists := globalRegistry.mcpClients[serverName]; exists {
			if logger != nil {
				logger.Debugf("🔧 MCP client for server %s already exists, keeping existing", serverName)
			}
			_ = existing // Keep existing client
		} else {
			globalRegistry.mcpClients[serverName] = client
			if logger != nil {
				logger.Debugf("🔧 Added MCP client for server: %s", serverName)
			}
		}
	}

	// Merge custom tools - UPDATE existing ones (orchestrator-wrapped executors should replace unwrapped)
	// This ensures folder guard validation is always applied when orchestrator wraps executors
	for toolName, executor := range customTools {
		if existing, exists := globalRegistry.customTools[toolName]; exists {
			// Replace existing executor with new one (orchestrator may have wrapped it with folder guard)
			globalRegistry.customTools[toolName] = executor
			if logger != nil {
				logger.Debugf("🔧 Custom tool %s already exists, UPDATING with new executor (may be wrapped with folder guard)", toolName)
			}
			_ = existing // Reference for logging/debugging
		} else {
			globalRegistry.customTools[toolName] = executor
			if logger != nil {
				logger.Debugf("🔧 Added custom tool: %s", toolName)
			}
		}
	}

	// Merge virtual tools
	for toolName, executor := range virtualTools {
		if existing, exists := globalRegistry.virtualTools[toolName]; exists {
			if logger != nil {
				logger.Debugf("🔧 Virtual tool %s already exists, keeping existing", toolName)
			}
			_ = existing // Keep existing executor
		} else {
			globalRegistry.virtualTools[toolName] = executor
			if logger != nil {
				logger.Debugf("🔧 Added virtual tool: %s", toolName)
			}
		}
	}

	// Merge tool-to-server mapping
	for toolName, serverName := range toolToServer {
		if existing, exists := globalRegistry.toolToServer[toolName]; exists {
			if existing != serverName {
				if logger != nil {
					logger.Warnf("⚠️ Tool %s already mapped to server %s, new mapping to %s will be ignored", toolName, existing, serverName)
				}
			}
		} else {
			globalRegistry.toolToServer[toolName] = serverName
			if logger != nil {
				logger.Debugf("🔧 Mapped tool %s to server %s", toolName, serverName)
			}
		}
	}

	if logger != nil {
		logger.Infof("✅ Tool registry updated: %d MCP clients, %d custom tools, %d virtual tools, %d tool mappings",
			len(globalRegistry.mcpClients),
			len(globalRegistry.customTools),
			len(globalRegistry.virtualTools),
			len(globalRegistry.toolToServer))
	}
}

// GetRegistry returns the global tool registry
func GetRegistry() *ToolRegistry {
	return globalRegistry
}

// CallMCPTool calls an MCP tool by name
func CallMCPTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error) {
	registry := GetRegistry()
	if registry == nil {
		return "", fmt.Errorf("tool registry not initialized")
	}

	registry.mu.RLock()
	defer registry.mu.RUnlock()

	// Look up server from tool name
	serverName, exists := registry.toolToServer[toolName]
	if !exists {
		// Debug: log available tools to help diagnose the issue
		if registry.logger != nil {
			availableTools := make([]string, 0, len(registry.toolToServer))
			for t := range registry.toolToServer {
				availableTools = append(availableTools, t)
			}
			registry.logger.Warnf("⚠️ Tool %s not found in tool-to-server mapping. Available tools (%d): %v", toolName, len(availableTools), availableTools)
		}
		return "", fmt.Errorf("tool %s not found in tool-to-server mapping", toolName)
	}

	// Get client for this server
	client, exists := registry.mcpClients[serverName]
	if !exists {
		return "", fmt.Errorf("MCP client for server %s not found", serverName)
	}

	// Call the tool
	if registry.logger != nil {
		registry.logger.Debugf("🔧 CallMCPTool: Calling tool %s on server %s with args: %v", toolName, serverName, args)
	}
	result, err := client.CallTool(ctx, toolName, args)
	if err != nil {
		if registry.logger != nil {
			registry.logger.Errorf("❌ CallMCPTool: Failed to call tool %s: %v", toolName, err)
		}
		return "", fmt.Errorf("failed to call MCP tool %s: %w", toolName, err)
	}

	// Check if the result itself indicates an error
	// Only treat as error if there are actual Go build/compilation errors
	// Otherwise, treat as success (MCP tools may incorrectly set IsError=true)
	if result != nil && result.IsError {
		// Extract error message from content - try multiple methods
		var errorMsg string
		var allContent strings.Builder

		// Method 1: Try to extract from TextContent
		if len(result.Content) > 0 {
			for _, content := range result.Content {
				if textContent, ok := content.(*mcp.TextContent); ok {
					if textContent.Text != "" {
						if errorMsg == "" {
							errorMsg = textContent.Text
						}
						allContent.WriteString(textContent.Text)
						allContent.WriteString("\n")
					}
				}
			}
		}

		// Method 2: If still empty, use ToolResultAsString to extract (handles all content types)
		if errorMsg == "" {
			// Use the same conversion logic as ToolResultAsString
			var parts []string
			for _, content := range result.Content {
				switch c := content.(type) {
				case *mcp.TextContent:
					parts = append(parts, c.Text)
					allContent.WriteString(c.Text)
					allContent.WriteString("\n")
				default:
					// Try to marshal other types to JSON
					if jsonBytes, err := json.Marshal(content); err == nil {
						parts = append(parts, string(jsonBytes))
						allContent.WriteString(string(jsonBytes))
						allContent.WriteString("\n")
					} else {
						parts = append(parts, fmt.Sprintf("[Unknown content type: %T]", content))
						allContent.WriteString(fmt.Sprintf("[Unknown content type: %T]", content))
						allContent.WriteString("\n")
					}
				}
			}
			errorMsg = strings.Join(parts, "\n")
		}

		// Method 3: If still empty, log content structure for debugging
		if errorMsg == "" {
			if registry.logger != nil {
				registry.logger.Warnf("⚠️ CallMCPTool: Tool %s returned error result with empty content. Content array length: %d", toolName, len(result.Content))
				for i, content := range result.Content {
					registry.logger.Warnf("⚠️ CallMCPTool: Content[%d] type: %T, value: %+v", i, content, content)
				}
			}
			errorMsg = fmt.Sprintf("tool returned error result (IsError=true) but no error message in content (content count: %d)", len(result.Content))
		}

		// Check if this is actually a Go build error
		// Use error type checking and content analysis to determine if it's a real build error
		isGoBuildError := isActualGoBuildError(errorMsg, allContent.String())

		// Only treat as error if it's an actual Go build/compilation error
		// Otherwise, treat as success (MCP tools may incorrectly set IsError=true)
		if isGoBuildError {
			// Real Go build error - treat as failure
			if registry.logger != nil {
				registry.logger.Errorf("❌ CallMCPTool: Tool %s returned error result with Go build error: %s", toolName, errorMsg)
			}
			return "", fmt.Errorf("tool %s execution failed: %s", toolName, errorMsg)
		} else {
			// No Go build errors - treat as success even if IsError=true
			if registry.logger != nil {
				registry.logger.Warnf("⚠️ CallMCPTool: Tool %s returned IsError=true but no Go build errors detected. Treating as success.", toolName)
				registry.logger.Debugf("🔍 CallMCPTool: Content (length: %d) does not contain Go build errors, treating as success", len(allContent.String()))
			}
			// Continue to success path below - don't return error
		}
	}

	// Convert successful result to string
	// Debug: log content structure before conversion
	if registry.logger != nil {
		registry.logger.Debugf("🔍 CallMCPTool: Tool %s result - IsError: %v, Content count: %d", toolName, result.IsError, len(result.Content))
		for i, content := range result.Content {
			registry.logger.Debugf("🔍 CallMCPTool: Content[%d] type: %T, value: %+v", i, content, content)
			// Try both pointer and value type assertions
			if textContent, ok := content.(*mcp.TextContent); ok {
				preview := textContent.Text
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				registry.logger.Debugf("🔍 CallMCPTool: Content[%d] *TextContent.Text (length: %d): %s", i, len(textContent.Text), preview)
			} else if textContent, ok := content.(mcp.TextContent); ok {
				preview := textContent.Text
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				registry.logger.Debugf("🔍 CallMCPTool: Content[%d] TextContent.Text (length: %d): %s", i, len(textContent.Text), preview)
			}
		}
	}
	resultStr := convertResultToString(result)
	if registry.logger != nil {
		preview := resultStr
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		registry.logger.Debugf("✅ CallMCPTool: Tool %s returned result (length: %d): %s", toolName, len(resultStr), preview)
	}
	return resultStr, nil
}

// CallCustomTool calls a custom tool by name
func CallCustomTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error) {
	registry := GetRegistry()
	if registry == nil {
		return "", fmt.Errorf("tool registry not initialized")
	}

	registry.mu.RLock()
	defer registry.mu.RUnlock()

	// Get custom tool executor
	executor, exists := registry.customTools[toolName]
	if !exists {
		return "", fmt.Errorf("custom tool %s not found", toolName)
	}

	// Call the executor
	return executor(ctx, args)
}

// CallVirtualTool calls a virtual tool by name
func CallVirtualTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error) {
	registry := GetRegistry()
	if registry == nil {
		return "", fmt.Errorf("tool registry not initialized")
	}

	registry.mu.RLock()
	defer registry.mu.RUnlock()

	// Get virtual tool executor
	executor, exists := registry.virtualTools[toolName]
	if !exists {
		return "", fmt.Errorf("virtual tool %s not found", toolName)
	}

	// Call the executor
	return executor(ctx, args)
}

// convertResultToString converts MCP CallToolResult to string
// Simply extracts all text content directly without any JSON parsing
func convertResultToString(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}

	// Extract all text content directly
	var textParts []string
	for _, content := range result.Content {
		// Try both pointer and value type assertions
		if textContent, ok := content.(*mcp.TextContent); ok {
			// Return text content directly, no JSON parsing
			textParts = append(textParts, textContent.Text)
		} else if textContent, ok := content.(mcp.TextContent); ok {
			// Handle value type (not pointer)
			textParts = append(textParts, textContent.Text)
		} else if embedded, ok := content.(*mcp.EmbeddedResource); ok {
			// Extract text from embedded resources
			switch r := embedded.Resource.(type) {
			case *mcp.TextResourceContents:
				textParts = append(textParts, r.Text)
			}
		}
		// Ignore ImageContent and other types - we only want text
	}

	joined := strings.Join(textParts, "\n")

	if result.IsError {
		if joined == "" {
			return "Tool execution error (no error details available)"
		}
		return fmt.Sprintf("Error: %s", joined)
	}

	if joined == "" {
		return "Tool execution completed (no output returned)"
	}

	return joined
}

// isActualGoBuildError checks if an error is actually a Go build/compilation error
// Uses multiple heuristics to avoid false positives:
// 1. Explicit build error markers
// 2. Go compiler error pattern matching (filename:line:column:)
// 3. Compilation error keywords in build context
// 4. Error type checking (for future enhancement)
func isActualGoBuildError(errorMsg, fullContent string) bool {
	// Normalize for case-insensitive comparison
	errorLower := strings.ToLower(errorMsg)
	contentLower := strings.ToLower(fullContent)

	// Heuristic 1: Check for explicit Go build error markers
	// These are definitive indicators of build errors
	buildErrorMarkers := []string{
		"failed to build plugin",
		"build output:",
		"go build",
	}

	for _, marker := range buildErrorMarkers {
		if strings.Contains(errorLower, marker) || strings.Contains(contentLower, marker) {
			return true
		}
	}

	// Heuristic 2: Check for Go compiler error patterns
	// Go compiler errors have specific formats like "filename:line:column: message"
	// Pattern: file.go:line:column: error message
	goCompilerPattern := `\.go:\d+:\d+:`
	if matched, _ := regexp.MatchString(goCompilerPattern, errorMsg); matched {
		return true
	}
	if matched, _ := regexp.MatchString(goCompilerPattern, fullContent); matched {
		return true
	}

	// Heuristic 3: Check for specific Go compilation error keywords
	// Only if they appear in a build context (not just random text)
	compilationErrors := []string{
		"syntax error",
		"undefined:",
		"cannot use",
		"wrong signature",
		"does not export",
		"cannot find package",
		"no required module",
		"missing go.sum",
	}

	// Check if any compilation error appears AND it's in a build context
	for _, errKeyword := range compilationErrors {
		if strings.Contains(contentLower, errKeyword) {
			// Additional check: make sure it's not just part of normal output
			// Build errors typically appear with file paths or in error contexts
			if strings.Contains(contentLower, ".go:") ||
				strings.Contains(contentLower, "compilation") ||
				strings.Contains(contentLower, "build") {
				return true
			}
		}
	}

	// Heuristic 4: Check error chain for GoBuildError type
	// This would be set if the error was properly wrapped
	// (Future enhancement: if we wrap build errors with GoBuildError type)
	// For now, we can check if errors.Is would match (if we implement it)

	// If none of the heuristics match, it's not a Go build error
	return false
}
