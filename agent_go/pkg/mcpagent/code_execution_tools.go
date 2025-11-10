package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode"

	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
	"mcp-agent/agent_go/pkg/mcpagent/codeexec"
	"mcp-agent/agent_go/pkg/mcpcache/codegen"
	"mcp-agent/agent_go/pkg/mcpclient"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// normalizeServerName normalizes server names by replacing hyphens with underscores
// This handles cases where directory names use underscores (google_sheets_tools)
// but config uses hyphens (google-sheets)
func normalizeServerName(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// findClientByNormalizedServerName finds a client in a.Clients by normalizing server names
// This handles the mismatch where config uses hyphens (google-sheets) but discovery
// extracts underscores from directory names (google_sheets)
func (a *Agent) findClientByNormalizedServerName(serverName string) mcpclient.ClientInterface {
	if a.Clients == nil {
		return nil
	}

	// Normalize the input server name
	normalizedInput := normalizeServerName(serverName)

	// Try direct lookup first (in case names already match)
	if client, exists := a.Clients[serverName]; exists {
		return client
	}

	// Iterate through all clients and compare normalized names
	for key, client := range a.Clients {
		normalizedKey := normalizeServerName(key)
		if normalizedKey == normalizedInput {
			return client
		}
	}

	return nil
}

// pascalCaseToSnakeCase converts PascalCase function names back to snake_case tool names
// Example: "DeleteWorkspaceFile" -> "delete_workspace_file"
func pascalCaseToSnakeCase(pascalCase string) string {
	if len(pascalCase) == 0 {
		return pascalCase
	}

	var result strings.Builder
	for i, r := range pascalCase {
		if i > 0 && r >= 'A' && r <= 'Z' {
			// Insert underscore before uppercase letter (except first)
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// shouldIncludeServerInDiscovery checks if a server should be included in discovery results
// based on selectedTools and selectedServers filtering
func (a *Agent) shouldIncludeServerInDiscovery(serverName string) bool {
	// Debug logging
	if a.Logger != nil {
		a.Logger.Debugf("🔍 [DISCOVERY FILTER] Checking server: %s, selectedServers: %v, selectedTools: %v", serverName, a.selectedServers, a.selectedTools)
	}

	// Normalize server name (handle hyphen vs underscore differences)
	normalizedServerName := normalizeServerName(serverName)

	// If selectedServers is not empty, only include servers in that list
	if len(a.selectedServers) > 0 {
		for _, selectedServer := range a.selectedServers {
			normalizedSelected := normalizeServerName(selectedServer)
			if normalizedSelected == normalizedServerName {
				if a.Logger != nil {
					a.Logger.Debugf("🔍 [DISCOVERY FILTER] Server %s included (in selectedServers, normalized: %s == %s)", serverName, normalizedServerName, normalizedSelected)
				}
				return true
			}
		}
		// Server not in selectedServers list, exclude it
		if a.Logger != nil {
			a.Logger.Debugf("🔍 [DISCOVERY FILTER] Server %s excluded (not in selectedServers: %v, normalized: %s)", serverName, a.selectedServers, normalizedServerName)
		}
		return false
	}

	// If selectedTools is not empty, check if this server has any tools selected
	if len(a.selectedTools) > 0 {
		// Check if this server appears in selectedTools (has specific tools selected)
		for _, fullName := range a.selectedTools {
			parts := strings.SplitN(fullName, ":", 2)
			if len(parts) == 2 {
				normalizedSelected := normalizeServerName(parts[0])
				if normalizedSelected == normalizedServerName {
					// Server has specific tools selected, include it (tools will be filtered at tool level)
					if a.Logger != nil {
						a.Logger.Debugf("🔍 [DISCOVERY FILTER] Server %s included (has tools in selectedTools, normalized: %s == %s)", serverName, normalizedServerName, normalizedSelected)
					}
					return true
				}
			}
		}
		// Server has no tools in selectedTools, exclude it
		if a.Logger != nil {
			a.Logger.Debugf("🔍 [DISCOVERY FILTER] Server %s excluded (no tools in selectedTools, normalized: %s)", serverName, normalizedServerName)
		}
		return false
	}

	// If no filtering is active (both selectedServers and selectedTools are empty), include all servers
	if a.Logger != nil {
		a.Logger.Debugf("🔍 [DISCOVERY FILTER] Server %s included (no filtering active)", serverName)
	}
	return true
}

// handleDiscoverCodeStructure handles the discover_code_structure virtual tool
func (a *Agent) handleDiscoverCodeStructure(ctx context.Context, args map[string]interface{}) (string, error) {
	generatedDir := a.getGeneratedDir()

	// Debug: Log received arguments
	if a.Logger != nil {
		a.Logger.Debugf("discover_code_structure called with args: %+v", args)
	}

	// Return discovery mode (list all servers/tools as JSON)
	return a.discoverAllServersAndTools(generatedDir)
}

// handleDiscoverCodeFiles handles the discover_code_files virtual tool
func (a *Agent) handleDiscoverCodeFiles(ctx context.Context, args map[string]interface{}) (string, error) {
	generatedDir := a.getGeneratedDir()

	// Debug: Log received arguments
	if a.Logger != nil {
		a.Logger.Debugf("discover_code_files called with args: %+v", args)
	}

	// Extract parameters (both required)
	serverName, ok := args["server_name"].(string)
	if !ok || serverName == "" {
		return "", fmt.Errorf("server_name parameter is required")
	}

	toolName, ok := args["tool_name"].(string)
	if !ok || toolName == "" {
		return "", fmt.Errorf("tool_name parameter is required")
	}

	// Determine package name first to check if it's a category directory
	var packageName string
	// Handle special cases for virtual_tools, custom_tools, and category directories (workspace_tools, human_tools, etc.)
	if serverName == "virtual_tools" {
		packageName = "virtual_tools"
	} else if serverName == "custom_tools" {
		packageName = "custom_tools"
	} else if strings.HasSuffix(serverName, "_tools") {
		// Category directory (workspace_tools, human_tools, etc.) - use as-is, don't add _tools suffix
		packageName = serverName
	} else {
		// MCP server - add _tools suffix
		packageName = codegen.GetPackageName(serverName)
	}
	packageDir := filepath.Join(generatedDir, packageName)

	// Check if package directory exists
	packageDirExists := false
	if serverName == "virtual_tools" || serverName == "custom_tools" {
		// For virtual_tools and custom_tools, check if directory exists
		if _, err := os.Stat(packageDir); err == nil {
			packageDirExists = true
		}
	} else if strings.HasSuffix(serverName, "_tools") {
		// Category directory (workspace_tools, human_tools, etc.)
		if _, err := os.Stat(packageDir); err == nil {
			packageDirExists = true
		}
	} else {
		// MCP server - check if directory exists
		if _, err := os.Stat(packageDir); err == nil {
			packageDirExists = true
		}
	}

	// Apply filtering: check if this server should be included
	// Category directories (workspace_tools, human_tools, etc.) are custom tools and should always be accessible
	// They are not MCP servers, so they should not be filtered by selectedServers or selectedTools
	if serverName == "virtual_tools" || serverName == "custom_tools" || strings.HasSuffix(serverName, "_tools") {
		// Category directories and special directories - always allow access (custom tools are always available)
		if a.Logger != nil {
			a.Logger.Debugf("🔍 [DISCOVERY] Allowing access to category/special directory %s (custom tools, not filtered)", packageName)
		}
	} else {
		// MCP server - check if it should be included
		if !a.shouldIncludeServerInDiscovery(serverName) {
			return "", fmt.Errorf("server %s is filtered out and not available", serverName)
		}
	}

	// Check if package directory exists
	if !packageDirExists {
		return "", fmt.Errorf("go code package directory not found for server: %s (expected at %s)", serverName, packageDir)
	}

	// Convert tool name to snake_case to match filename
	fileName := codegen.ToolNameToSnakeCase(toolName) + ".go"
	filePath := filepath.Join(packageDir, fileName)

	// Check if the specific tool file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", fmt.Errorf("tool file not found: %s (expected at %s). Tool name '%s' converted to filename '%s'", toolName, filePath, toolName, fileName)
	}

	// Read and return the single tool file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read tool file %s: %w", filePath, err)
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("// Package: %s\n", packageName))
	result.WriteString(fmt.Sprintf("// Server: %s\n", serverName))
	result.WriteString(fmt.Sprintf("// Tool: %s\n", toolName))
	result.WriteString(fmt.Sprintf("// File: %s\n\n", fileName))
	result.WriteString(string(content))

	return result.String(), nil
}

// discoverAllServersAndTools returns a JSON list of all available servers and their tools
func (a *Agent) discoverAllServersAndTools(generatedDir string) (string, error) {
	entries, err := os.ReadDir(generatedDir)
	if err != nil {
		return "", fmt.Errorf("failed to read generated directory: %w", err)
	}

	type ServerInfo struct {
		Name    string   `json:"name"`
		Package string   `json:"package"`
		Tools   []string `json:"tools"`
	}

	type CustomToolsInfo struct {
		Package string   `json:"package"`
		Tools   []string `json:"tools"`
	}

	type WorkspaceToolsInfo struct {
		Package string   `json:"package"`
		Tools   []string `json:"tools"`
	}

	type HumanToolsInfo struct {
		Package string   `json:"package"`
		Tools   []string `json:"tools"`
	}

	type VirtualToolsInfo struct {
		Package string   `json:"package"`
		Tools   []string `json:"tools"`
	}

	type DiscoveryResult struct {
		Servers        []ServerInfo        `json:"servers"`
		CustomTools    *CustomToolsInfo    `json:"custom_tools,omitempty"`
		WorkspaceTools *WorkspaceToolsInfo `json:"workspace_tools,omitempty"`
		HumanTools     *HumanToolsInfo     `json:"human_tools,omitempty"`
		VirtualTools   *VirtualToolsInfo   `json:"virtual_tools,omitempty"`
	}

	var result DiscoveryResult
	result.Servers = []ServerInfo{}

	// Scan for all *_tools directories
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirName := entry.Name()
		// Include directories ending with _tools or virtual_tools
		if !strings.HasSuffix(dirName, "_tools") && dirName != "virtual_tools" {
			continue
		}

		// Extract server/category name from package name
		// For category-specific directories (workspace_tools, human_tools), this will be the category
		// For MCP server directories (aws_tools, gdrive_tools), this will be the server name
		serverName := strings.TrimSuffix(dirName, "_tools")

		// Find all Go files in this directory
		packageDir := filepath.Join(generatedDir, dirName)
		packageEntries, err := os.ReadDir(packageDir)
		if err != nil {
			if a.Logger != nil {
				a.Logger.Warnf("Failed to read package directory %s: %v", packageDir, err)
			}
			continue
		}

		var tools []string
		// Parse all Go files in the package directory to extract function names
		for _, packageEntry := range packageEntries {
			if packageEntry.IsDir() || !strings.HasSuffix(packageEntry.Name(), ".go") {
				continue
			}

			goFile := filepath.Join(packageDir, packageEntry.Name())
			fset := token.NewFileSet()
			node, err := parser.ParseFile(fset, goFile, nil, parser.ParseComments)
			if err != nil {
				if a.Logger != nil {
					a.Logger.Warnf("Failed to parse Go file %s: %v", goFile, err)
				}
				continue
			}

			ast.Inspect(node, func(n ast.Node) bool {
				if fn, ok := n.(*ast.FuncDecl); ok {
					// Only include exported functions (starting with uppercase)
					if fn.Name != nil && len(fn.Name.Name) > 0 && fn.Name.Name[0] >= 'A' && fn.Name.Name[0] <= 'Z' {
						// Apply filtering: check if this tool should be included
						// For virtual_tools and category directories, always include
						shouldInclude := true

						// Dynamically check if this is a category directory
						// A directory is a category directory if it's not a known MCP server
						// This makes it fully dynamic - any custom tool category directory will be detected
						// Use normalized lookup to handle hyphen vs underscore mismatch
						isKnownMCPServer := a.findClientByNormalizedServerName(serverName) != nil
						isCategoryDirectory := !isKnownMCPServer

						if dirName != "virtual_tools" && !isCategoryDirectory {
							// For MCP server tools, check filtering
							shouldInclude = a.shouldIncludeToolInDiscovery(serverName, fn.Name.Name)
						}
						if shouldInclude {
							tools = append(tools, fn.Name.Name)
						}
					}
				}
				return true
			})
		}

		// Skip if no tools found (after filtering)
		if len(tools) == 0 {
			continue
		}

		// Dynamically determine if this is a category directory or MCP server directory
		// A directory is a category directory if:
		// 1. It ends with "_tools" (already checked)
		// 2. It's not "virtual_tools" (handled separately)
		// 3. It's not a known MCP server (check if serverName is in Clients)
		// 4. Any directory that matches these criteria is treated as a category directory

		// Check if serverName is a known MCP server (by checking if it's in Clients)
		// Use normalized lookup to handle hyphen vs underscore mismatch
		isKnownMCPServer := a.findClientByNormalizedServerName(serverName) != nil

		// If it's not a known MCP server and not virtual_tools, it's a category directory
		// This makes it fully dynamic - any custom tool category directory will be detected
		isCategoryDirectory := !isKnownMCPServer

		// Apply server-level filtering for MCP servers only
		// Category directories and virtual_tools are always included
		if dirName != "virtual_tools" && !isCategoryDirectory {
			if !a.shouldIncludeServerInDiscovery(serverName) {
				// Server is filtered out, skip it
				continue
			}
		}

		// For category directories (workspace_tools, human_tools, etc.), always include them
		// They are custom tool categories, not MCP servers, so they should not be filtered
		// by selectedServers or selectedTools filtering
		if isCategoryDirectory && dirName != "virtual_tools" {
			// Always include category directories - they are custom tools, not MCP servers
			// No filtering needed for custom tool categories
			if a.Logger != nil {
				a.Logger.Debugf("🔍 [DISCOVERY] Including category directory %s (custom tools, not filtered by server selection)", dirName)
			}
		}

		if dirName == "virtual_tools" {
			// Virtual tools - always include
			result.VirtualTools = &VirtualToolsInfo{
				Package: dirName,
				Tools:   tools,
			}
		} else if isCategoryDirectory {
			// This is a category-specific directory (workspace_tools, human_tools, etc.)
			// Category directories are created by GenerateCustomToolsCode based on tool categories
			// Dynamically determine which info struct to use based on the category name
			// Check registered tools to see what categories exist, or use virtual tools functions as fallback
			workspaceCategory := virtualtools.GetWorkspaceToolCategory()
			humanCategory := virtualtools.GetHumanToolCategory()

			// Also check registered tools for any additional categories
			allCategories := a.GetCustomToolCategories()
			categorySet := make(map[string]bool)
			categorySet[workspaceCategory] = true
			categorySet[humanCategory] = true
			for _, cat := range allCategories {
				categorySet[cat] = true
			}

			// Use specific info structs for workspace and human (for backward compatibility with JSON structure)
			// All other categories use CustomToolsInfo
			if serverName == workspaceCategory {
				// Workspace tools directory
				result.WorkspaceTools = &WorkspaceToolsInfo{
					Package: dirName,
					Tools:   tools,
				}
			} else if serverName == humanCategory {
				// Human tools directory
				result.HumanTools = &HumanToolsInfo{
					Package: dirName,
					Tools:   tools,
				}
			} else {
				// Any other category directories (memory, custom, or any future categories)
				// Use CustomToolsInfo for all other categories - fully dynamic
				result.CustomTools = &CustomToolsInfo{
					Package: dirName,
					Tools:   tools,
				}
			}
		} else {
			// MCP server tools - only include if server passes filter
			result.Servers = append(result.Servers, ServerInfo{
				Name:    serverName,
				Package: dirName,
				Tools:   tools,
			})
		}
	}

	// Convert to JSON
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal discovery result: %w", err)
	}

	return string(jsonData), nil
}

// shouldIncludeToolInDiscovery checks if a tool should be included in discovery results
// based on selectedTools filtering
func (a *Agent) shouldIncludeToolInDiscovery(serverName string, toolName string) bool {
	// If no filtering is active, include all tools
	if len(a.selectedTools) == 0 {
		return true
	}

	// Build set for fast lookup of specific tools
	selectedToolSet := make(map[string]bool)
	for _, fullName := range a.selectedTools {
		selectedToolSet[fullName] = true
	}

	// Build map of which servers have specific tools selected
	serversWithSpecificTools := make(map[string]bool)
	for _, fullName := range a.selectedTools {
		parts := strings.SplitN(fullName, ":", 2)
		if len(parts) == 2 {
			serversWithSpecificTools[parts[0]] = true
		}
	}

	// Check if this server has specific tools selected
	hasSpecificTools := serversWithSpecificTools[serverName]

	if hasSpecificTools {
		// Server has specific tools - check if this tool is selected
		fullName := fmt.Sprintf("%s:%s", serverName, toolName)
		return selectedToolSet[fullName]
	} else {
		// Server has no specific tools - include ALL tools from this server
		// (this is "all tools" mode for this server)
		return true
	}
}

// handleWriteCode handles the write_code virtual tool
func (a *Agent) handleWriteCode(ctx context.Context, args map[string]interface{}) (string, error) {
	code, ok := args["code"].(string)
	if !ok || code == "" {
		return "", fmt.Errorf("code parameter is required and must be a non-empty string")
	}

	// Generate unique filename automatically
	filename := fmt.Sprintf("code_%d.go", time.Now().UnixNano())

	// Get workspace directory (use tool output handler's workspace if available)
	workspaceDir := "workspace"
	if a.toolOutputHandler != nil {
		workspaceDir = a.toolOutputHandler.GetToolOutputFolder()
	}

	// Ensure workspace directory exists
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create workspace directory: %w", err)
	}

	// Write code to file (no registry injection needed - we'll execute in-process)
	filePath := filepath.Join(workspaceDir, filename)
	if err := os.WriteFile(filePath, []byte(code), 0644); err != nil {
		return "", fmt.Errorf("failed to write code file: %w", err)
	}

	if a.Logger != nil {
		a.Logger.Infof("✅ Written Go code to: %s (%d bytes)", filePath, len(code))
	}

	// Execute the Go code in-process and capture output
	output, err := a.executeGoCode(ctx, workspaceDir, filePath, code)
	if err != nil {
		// Log the full error details for debugging
		if a.Logger != nil {
			a.Logger.Errorf("❌ Code execution failed - Error: %v\nError details: %+v", err, err)
		}
		// Keep files on error for debugging - don't delete them
		// Return error output so LLM can see what went wrong
		// Format error message with clear structure for LLM to understand and fix
		errorMessage := formatCodeExecutionError(err, code)
		return errorMessage, nil
	}

	// Clean up original code file after successful execution
	if err := os.Remove(filePath); err != nil {
		if a.Logger != nil {
			a.Logger.Debugf("⚠️ Failed to remove code file %s: %v", filePath, err)
		}
	} else if a.Logger != nil {
		a.Logger.Debugf("🧹 Cleaned up code file: %s", filePath)
	}

	// Ensure we always return meaningful content to the LLM
	// If output is empty, provide a message indicating successful execution with no output
	if output == "" {
		if a.Logger != nil {
			a.Logger.Infof("⚠️ Code execution succeeded but produced no output (empty stdout/stderr)")
		}
		return "Code executed successfully. No output was produced (stdout/stderr were empty).", nil
	}

	// Return the execution output (this will be shown in UI and passed to LLM)
	return output, nil
}

// executeGoCode executes Go code in-process using yaegi interpreter
// Yaegi supports full Go code execution (loops, conditionals, functions, etc.) without compilation
// No fallback needed - yaegi works on all platforms and supports full Go language
func (a *Agent) executeGoCode(ctx context.Context, workspaceDir, filePath, code string) (string, error) {
	result, err := a.executeGoCodeViaYaegi(ctx, code)
	if err != nil {
		// Return a helpful error message
		return "", fmt.Errorf("yaegi execution failed: %w\n\n💡 Tip: Ensure your code has an execute() function that returns string, or wrap your code in execute()", err)
	}
	return result, nil
}

// executeGoCodeViaYaegi executes Go code using yaegi interpreter
// Supports full Go code execution including loops, conditionals, functions, etc.
// No compilation needed - runs directly in the same process
func (a *Agent) executeGoCodeViaYaegi(ctx context.Context, code string) (string, error) {
	if a.Logger != nil {
		a.Logger.Debugf("🔧 Using yaegi interpreter for code execution (supports full Go code)")
	}

	// Create yaegi interpreter
	i := interp.New(interp.Options{
		GoPath: os.Getenv("GOPATH"),
	})

	// Import standard library (includes fmt, strings, context, encoding/json, etc.)
	if err := i.Use(stdlib.Symbols); err != nil {
		return "", fmt.Errorf("failed to import stdlib: %w", err)
	}

	// Inject registry functions so user code can call tools
	// Create wrapper functions that yaegi can call
	// We inject them as a package so user code can use: codeexec.CallCustomTool(ctx, toolName, args)
	// The signature must match: func(ctx context.Context, toolName string, args map[string]interface{}) (string, error)
	registrySymbols := map[string]reflect.Value{
		"CallMCPTool": reflect.ValueOf(func(ctxParam context.Context, toolName string, args map[string]interface{}) (string, error) {
			// Use the context from the parameter (user code provides it)
			return codeexec.CallMCPTool(ctxParam, toolName, args)
		}),
		"CallCustomTool": reflect.ValueOf(func(ctxParam context.Context, toolName string, args map[string]interface{}) (string, error) {
			// Use the context from the parameter (user code provides it)
			return codeexec.CallCustomTool(ctxParam, toolName, args)
		}),
		"CallVirtualTool": reflect.ValueOf(func(ctxParam context.Context, toolName string, args map[string]interface{}) (string, error) {
			// Use the context from the parameter (user code provides it)
			return codeexec.CallVirtualTool(ctxParam, toolName, args)
		}),
	}

	// Create a codeexec package in yaegi's namespace
	// Use the exact package path that user code imports: "codeexec/codeexec"
	if err := i.Use(interp.Exports{
		"codeexec/codeexec": registrySymbols,
	}); err != nil {
		if a.Logger != nil {
			a.Logger.Errorf("❌ Failed to inject registry functions: %v", err)
		}
		return "", fmt.Errorf("failed to inject codeexec functions into yaegi: %w", err)
	}

	if a.Logger != nil {
		a.Logger.Debugf("✅ Successfully injected codeexec functions: CallMCPTool, CallCustomTool, CallVirtualTool")
	}

	// No package injection needed - user code should use codeexec.CallCustomTool() directly
	// This is simpler and works natively with yaegi interpreter
	// Example: codeexec.CallCustomTool(ctx, "read_workspace_file", params)

	// Wrap user code to capture output and provide context
	// User code should have an execute() function that returns string
	wrappedCode := a.wrapCodeForYaegi(code)

	if a.Logger != nil {
		a.Logger.Debugf("📝 Wrapped code for yaegi execution:\n%s", wrappedCode)
	}

	// Evaluate the code
	_, evalErr := i.Eval(wrappedCode)
	if evalErr != nil {
		return "", fmt.Errorf("yaegi evaluation failed: %w", evalErr)
	}

	// Look up and call the execute function
	executeFunc, err := i.Eval("execute")
	if err != nil {
		return "", fmt.Errorf("failed to find execute function: %w", err)
	}

	// Call the execute function
	results := executeFunc.Call(nil)
	if len(results) != 1 {
		return "", fmt.Errorf("execute function should return one value (string), got %d", len(results))
	}

	// Extract result
	resultValue := results[0]
	if !resultValue.IsValid() {
		return "", fmt.Errorf("execute function returned invalid value")
	}

	// Convert to string
	var result string
	if resultValue.Kind() == reflect.String {
		result = resultValue.String()
	} else {
		result = fmt.Sprintf("%v", resultValue.Interface())
	}

	if a.Logger != nil {
		if len(result) == 0 {
			a.Logger.Infof("✅ Yaegi execution completed - No output")
		} else {
			preview := result
			if len(preview) > 500 {
				preview = preview[:500] + "... (truncated)"
			}
			a.Logger.Infof("✅ Yaegi execution completed - Output length: %d bytes\n📝 Output preview:\n%s", len(result), preview)
			a.Logger.Debugf("📝 Full output from yaegi execution:\n%s", result)
		}
	}

	return result, nil
}

// wrapCodeForYaegi wraps user code to work with yaegi interpreter
// User code should have an execute() function that returns string
func (a *Agent) wrapCodeForYaegi(userCode string) string {
	// Check if user code already has package declaration
	hasPackage := strings.Contains(userCode, "package ")
	hasImports := strings.Contains(userCode, "import")

	var wrapped strings.Builder

	// Only add package/imports if user code doesn't have them
	if !hasPackage {
		wrapped.WriteString("package main\n\n")
	}

	if !hasImports {
		// Add imports that user code might need
		wrapped.WriteString("import (\n")
		wrapped.WriteString(`	"context"` + "\n")
		wrapped.WriteString(`	"fmt"` + "\n")
		wrapped.WriteString(`	"strings"` + "\n")
		wrapped.WriteString(`	"encoding/json"` + "\n")
		wrapped.WriteString(`	codeexec "codeexec/codeexec"` + "\n")
		wrapped.WriteString(")\n\n")
	}

	// Check if user code already has execute() function
	if strings.Contains(userCode, "func execute()") {
		// User code already has execute() - remove any imports of generated tool packages
		// (they're injected into yaegi namespace, so imports aren't needed)
		userCode = a.removeGeneratedToolImports(userCode)
		// Ensure codeexec import is present if user code has imports
		if hasImports && !strings.Contains(userCode, `codeexec "codeexec/codeexec"`) && !strings.Contains(userCode, `"codeexec/codeexec"`) {
			// Add codeexec import to existing imports
			userCode = a.addCodeexecImport(userCode)
		}
		// User code already has execute() - just add it (without package/imports if already present)
		if hasPackage {
			// User code is complete, use as-is
			wrapped.WriteString(userCode)
		} else {
			// Add user code after our package/imports
			wrapped.WriteString(userCode)
		}
	} else if strings.Contains(userCode, "func main()") {
		// Replace main() with execute()
		// Also remove any imports of generated tool packages (they're injected into yaegi namespace)
		modifiedCode := a.removeGeneratedToolImports(userCode)
		modifiedCode = strings.ReplaceAll(modifiedCode, "func main()", "func execute()")
		wrapped.WriteString(modifiedCode)
	} else {
		// Wrap entire code in execute() function
		// If user code has package/imports, preserve them; otherwise add our own
		if hasPackage {
			// Extract package and imports, then wrap the rest
			// Remove any imports of generated tool packages (they're injected into yaegi namespace)
			userCode = a.removeGeneratedToolImports(userCode)
			lines := strings.Split(userCode, "\n")
			inImportBlock := false
			importEnded := false
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "package ") {
					wrapped.WriteString(line + "\n")
					continue
				}
				if strings.HasPrefix(trimmed, "import") {
					inImportBlock = true
					wrapped.WriteString(line + "\n")
					if trimmed == "import (" {
						continue
					} else if trimmed == "import" || strings.HasPrefix(trimmed, "import \"") {
						// Single line import
						wrapped.WriteString(line + "\n")
						importEnded = true
						continue
					}
					continue
				}
				if inImportBlock {
					if trimmed == ")" {
						inImportBlock = false
						importEnded = true
						wrapped.WriteString(line + "\n")
						continue
					}
					wrapped.WriteString(line + "\n")
					continue
				}
				if importEnded || (!hasPackage && !hasImports) {
					// Now we're past imports, wrap in execute()
					if i == 0 || (i > 0 && strings.TrimSpace(lines[i-1]) == "") {
						wrapped.WriteString("func execute() string {\n")
					}
					if strings.TrimSpace(line) != "" {
						wrapped.WriteString("\t" + line + "\n")
					} else {
						wrapped.WriteString("\n")
					}
				} else {
					wrapped.WriteString(line + "\n")
				}
			}
			if !strings.Contains(wrapped.String(), "func execute()") {
				// Didn't add execute wrapper, add it at the end
				wrapped.WriteString("func execute() string {\n")
				wrapped.WriteString("\treturn \"\"\n")
				wrapped.WriteString("}\n")
			} else {
				// Close execute function
				if !strings.HasSuffix(wrapped.String(), "}\n") {
					wrapped.WriteString("}\n")
				}
			}
		} else {
			// No package/imports in user code, wrap everything
			wrapped.WriteString("func execute() string {\n")
			lines := strings.Split(userCode, "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					wrapped.WriteString("\t" + line + "\n")
				} else {
					wrapped.WriteString("\n")
				}
			}
			wrapped.WriteString("}\n")
		}
	}

	return wrapped.String()
}

// injectGeneratedToolPackages is no longer needed
// User code should use codeexec.CallCustomTool(), CallMCPTool(), or CallVirtualTool() directly
// This function is kept for backward compatibility but does nothing
func (a *Agent) injectGeneratedToolPackages(i *interp.Interpreter, ctx context.Context) error {
	// No package injection needed - user code should use codeexec functions directly
	// Example: codeexec.CallCustomTool(ctx, "read_workspace_file", params)
	if a.Logger != nil {
		a.Logger.Debugf("✅ Code execution ready - use codeexec.CallCustomTool/CallMCPTool/CallVirtualTool directly")
	}
	return nil
}

// functionNameToToolName converts PascalCase function name to snake_case tool name
// Example: ListWorkspaceFiles -> list_workspace_files
func (a *Agent) functionNameToToolName(funcName string) string {
	var result strings.Builder
	for i, r := range funcName {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
}

// addCodeexecImport adds codeexec import to user code if it's missing
func (a *Agent) addCodeexecImport(code string) string {
	lines := strings.Split(code, "\n")
	var result []string
	inImportBlock := false
	importAdded := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		result = append(result, line)

		// Detect start of import block
		if strings.HasPrefix(trimmed, "import (") {
			inImportBlock = true
			continue
		}

		// Detect single-line import
		if strings.HasPrefix(trimmed, "import \"") {
			// Add codeexec import after this line
			result = append(result, `	codeexec "codeexec/codeexec"`)
			importAdded = true
			continue
		}

		// Inside import block
		if inImportBlock {
			// End of import block
			if trimmed == ")" {
				if !importAdded {
					// Insert codeexec import before closing parenthesis
					result[len(result)-1] = `	codeexec "codeexec/codeexec"`
					result = append(result, ")")
				}
				inImportBlock = false
				continue
			}
		}
	}

	return strings.Join(result, "\n")
}

// removeGeneratedToolImports removes import statements for generated tool packages
// These packages are injected into yaegi's namespace, so explicit imports aren't needed
// and will cause errors since yaegi can't resolve the module paths
func (a *Agent) removeGeneratedToolImports(code string) string {
	lines := strings.Split(code, "\n")
	var result []string
	inImportBlock := false
	importBlockStart := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect start of import block
		if strings.HasPrefix(trimmed, "import (") {
			inImportBlock = true
			importBlockStart = i
			result = append(result, line)
			continue
		}

		// Detect single-line import
		if strings.HasPrefix(trimmed, "import \"") {
			// Check if this is a generated tool package import
			if strings.Contains(trimmed, "mcp-agent/agent_go/generated/") &&
				(strings.Contains(trimmed, "_tools\"") || strings.Contains(trimmed, "virtual_tools\"")) {
				// Skip this import line
				continue
			}
			result = append(result, line)
			continue
		}

		// Inside import block
		if inImportBlock {
			// Check if this line is a generated tool package import
			if strings.Contains(trimmed, "\"mcp-agent/agent_go/generated/") &&
				(strings.Contains(trimmed, "_tools\"") || strings.Contains(trimmed, "virtual_tools\"")) {
				// Skip this import line
				continue
			}

			// End of import block
			if trimmed == ")" {
				inImportBlock = false
				// If import block is now empty, remove the entire block
				if importBlockStart >= 0 {
					// Check if there are any non-empty lines between start and here
					hasNonEmptyImports := false
					for j := importBlockStart + 1; j < i; j++ {
						lineTrimmed := strings.TrimSpace(lines[j])
						if lineTrimmed != "" && !strings.Contains(lineTrimmed, "mcp-agent/agent_go/generated/") {
							hasNonEmptyImports = true
							break
						}
					}
					if !hasNonEmptyImports {
						// Remove the entire import block
						result = result[:len(result)-1] // Remove "import ("
						continue
					}
				}
			}
			result = append(result, line)
			continue
		}

		// Regular line, keep it
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// formatCodeExecutionError formats code execution errors for clear LLM understanding
// Simplified version for yaegi interpreter errors only
func formatCodeExecutionError(err error, code string) string {
	errorStr := err.Error()
	var builder strings.Builder

	builder.WriteString("**❌ EXECUTION ERROR**\n\n")
	builder.WriteString("**Error Details:**\n```\n")
	builder.WriteString(errorStr + "\n")
	builder.WriteString("```\n\n")

	// Check for common yaegi errors and provide helpful tips
	if strings.Contains(errorStr, "undefined:") {
		builder.WriteString("**💡 Tip:** The code references an undefined variable or function.\n")
		builder.WriteString("- Check for typos in variable/function names\n")
		builder.WriteString("- Ensure all required packages are imported\n")
		builder.WriteString("- Verify that tool functions are called correctly (e.g., `workspace_tools.ListWorkspaceFiles`)\n\n")
	} else if strings.Contains(errorStr, "cannot use") {
		builder.WriteString("**💡 Tip:** Type mismatch error.\n")
		builder.WriteString("- Check that function parameters match expected types\n")
		builder.WriteString("- Verify struct field types match the tool's expected parameters\n\n")
	} else if strings.Contains(errorStr, "syntax error") || strings.Contains(errorStr, "expected") {
		builder.WriteString("**💡 Tip:** Syntax error detected.\n")
		builder.WriteString("- Check for missing brackets, parentheses, or semicolons\n")
		builder.WriteString("- Verify that all strings are properly quoted\n")
		builder.WriteString("- Ensure function signatures are correct\n\n")
	} else {
		builder.WriteString("**💡 Tip:** Review the error message above for specific details about what went wrong.\n")
		builder.WriteString("- Ensure your code has an `execute()` function that returns `string`\n")
		builder.WriteString("- Check that all tool calls use the correct syntax (e.g., `workspace_tools.ListWorkspaceFiles(ctx, params)`)\n\n")
	}

	builder.WriteString("**Your Code:**\n```go\n")
	builder.WriteString(code)
	builder.WriteString("\n```\n")

	return builder.String()
}
