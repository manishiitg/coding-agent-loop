package mcpagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcpagent/mcpcache/codegen"
)

// NOTE: shouldIncludeServerInDiscovery has been replaced by ToolFilter.ShouldIncludeServer()
// The unified ToolFilter provides consistent filtering across LLM tool registration and discovery

// handleDiscoverCodeFiles handles the discover_code_files virtual tool
func (a *Agent) handleDiscoverCodeFiles(ctx context.Context, args map[string]interface{}) (string, error) {
	generatedDir := a.getGeneratedDir()
	agentDir := a.getAgentGeneratedDir()

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

	// Check agent directory first, then fall back to shared directory
	var packageDir string
	agentPackageDir := filepath.Join(agentDir, packageName)
	if _, err := os.Stat(agentPackageDir); err == nil {
		// Found in agent directory
		packageDir = agentPackageDir
		if a.Logger != nil {
			a.Logger.Debugf("🔍 Found package in agent directory: %s", packageDir)
		}
	} else {
		// Fall back to shared directory
		packageDir = filepath.Join(generatedDir, packageName)
		if a.Logger != nil {
			a.Logger.Debugf("🔍 Using shared directory: %s", packageDir)
		}
	}

	// Check if package directory exists
	_, err := os.Stat(packageDir)
	packageDirExists := err == nil

	// Apply filtering using unified ToolFilter
	// Check if server/package should be included based on filtering configuration
	isVirtualTool := a.toolFilter.IsVirtualToolsDirectory(packageName)
	isCategoryDir := a.toolFilter.IsCategoryDirectory(packageName)

	if !isVirtualTool && !isCategoryDir {
		// MCP server - check if it should be included
		if !a.toolFilter.ShouldIncludeServer(serverName) {
			return "", fmt.Errorf("server %s is filtered out and not available", serverName)
		}
	} else if a.Logger != nil {
		a.Logger.Debugf("🔍 [DISCOVERY] Allowing access to %s (isVirtualTool=%v, isCategoryDir=%v)", packageName, isVirtualTool, isCategoryDir)
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
	if a.Logger != nil {
		a.Logger.Infof("🔍 [DISCOVERY] ===== Starting Tool Discovery =====")
		a.Logger.Infof("🔍 [DISCOVERY] Discovery directory: %s", generatedDir)
		a.Logger.Infof("🔍 [DISCOVERY] Filter configuration:")
		a.Logger.Infof("🔍 [DISCOVERY]   - selectedServers: %v (count: %d)", a.selectedServers, len(a.selectedServers))
		a.Logger.Infof("🔍 [DISCOVERY]   - selectedTools: %v (count: %d)", a.selectedTools, len(a.selectedTools))
		a.Logger.Infof("🔍 [DISCOVERY]   - useCodeExecutionMode: %v", a.UseCodeExecutionMode)
		// Log available MCP clients for debugging server detection
		if a.Clients != nil {
			clientNames := make([]string, 0, len(a.Clients))
			for name := range a.Clients {
				clientNames = append(clientNames, name)
			}
			a.Logger.Infof("🔍 [DISCOVERY] Available MCP clients: %v (count: %d)", clientNames, len(clientNames))
		} else {
			a.Logger.Warnf("🔍 [DISCOVERY] No MCP clients available (a.Clients is nil)")
		}
	}

	entries, err := os.ReadDir(generatedDir)
	if err != nil {
		return "", fmt.Errorf("failed to read generated directory: %w", err)
	}

	if a.Logger != nil {
		a.Logger.Debugf("🔍 [DISCOVERY] Found %d entries in generated directory", len(entries))
	}

	type ServerInfo struct {
		Name    string   `json:"name"`
		Package string   `json:"package"`
		Tools   []string `json:"tools"`
	}

	type CustomToolsInfo struct {
		Category string   `json:"category"`
		Package  string   `json:"package"`
		Tools    []string `json:"tools"`
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
		CustomTools    []CustomToolsInfo   `json:"custom_tools,omitempty"`
		WorkspaceTools *WorkspaceToolsInfo `json:"workspace_tools,omitempty"`
		HumanTools     *HumanToolsInfo     `json:"human_tools,omitempty"`
		VirtualTools   *VirtualToolsInfo   `json:"virtual_tools,omitempty"`
	}

	var result DiscoveryResult
	result.Servers = []ServerInfo{}
	result.CustomTools = []CustomToolsInfo{}

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

		if a.Logger != nil {
			a.Logger.Debugf("🔍 [DISCOVERY] Processing directory: %s (serverName: %s)", dirName, serverName)
		}

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
						// Apply filtering using unified ToolFilter
						// This ensures discovery results match LLM tool registration

						// Determine tool type using ToolFilter
						isVirtualTool := a.toolFilter.IsVirtualToolsDirectory(dirName)
						isCategoryDir := a.toolFilter.IsCategoryDirectory(dirName)

						// Use unified filter to check if tool should be included
						// The package/server name format depends on the type:
						// - MCP servers: use serverName (without _tools suffix) e.g., "google_sheets"
						//   because selectedTools uses "google-sheets:ToolName" format
						// - Category dirs: use dirName (WITH _tools suffix) e.g., "workspace_tools"
						//   because selectedTools uses "workspace_tools:ToolName" format
						// - Virtual tools: use dirName e.g., "virtual_tools"
						//
						// isCustomTool = true for category directories, false for MCP servers
						// isVirtualTool = true for virtual_tools directory
						var packageOrServer string
						if isCategoryDir || isVirtualTool {
							// Category directories and virtual tools use the full directory name
							// e.g., "workspace_tools", "human_tools", "virtual_tools"
							packageOrServer = dirName
						} else {
							// MCP servers use the server name without _tools suffix
							// e.g., "google_sheets" (which normalizes to match "google-sheets" from config)
							packageOrServer = serverName
						}
						shouldInclude := a.toolFilter.ShouldIncludeTool(packageOrServer, fn.Name.Name, isCategoryDir, isVirtualTool)

						if a.Logger != nil {
							a.Logger.Debugf("🔍 [TOOL DISCOVERY] Tool %s: packageOrServer=%s (normalized: %s), isVirtualTool=%v, isCategoryDir=%v, shouldInclude=%v",
								fn.Name.Name, packageOrServer, a.toolFilter.NormalizeServerName(packageOrServer), isVirtualTool, isCategoryDir, shouldInclude)
						}

						if shouldInclude {
							tools = append(tools, fn.Name.Name)
						}
					}
				}
				return true
			})
		}

		// Use unified ToolFilter for directory type detection
		// This ensures consistency with LLM tool registration
		isVirtualToolsDir := a.toolFilter.IsVirtualToolsDirectory(dirName)
		isCategoryDirectory := a.toolFilter.IsCategoryDirectory(dirName)

		// Skip if no tools found (after filtering)
		if len(tools) == 0 {
			if a.Logger != nil {
				a.Logger.Warnf("🔍 [DISCOVERY] Skipping directory %s - no tools found after filtering (serverName=%s, isCategoryDirectory=%v, isVirtualToolsDir=%v)", dirName, serverName, isCategoryDirectory, isVirtualToolsDir)
			}
			continue
		}

		if a.Logger != nil {
			a.Logger.Debugf("🔍 [DISCOVERY] Directory %s: Found %d tools after filtering: %v", dirName, len(tools), tools)
			a.Logger.Debugf("🔍 [DISCOVERY] Directory %s: serverName=%s, isVirtualToolsDir=%v, isCategoryDirectory=%v", dirName, serverName, isVirtualToolsDir, isCategoryDirectory)
		}

		// Server-level filtering: Tools are filtered above, and server-level checks are done
		// before adding servers to the result to ensure excluded servers don't appear

		if dirName == "virtual_tools" {
			// Virtual tools are already registered as real tools to the LLM
			// They don't need to be in the system prompt's tool structure discovery
			// Skip adding them to the discovery result
			if a.Logger != nil {
				a.Logger.Debugf("🔍 [DISCOVERY] Skipping virtual_tools directory - tools are already registered as real LLM tools (not needed in system prompt)")
			}
			continue
		} else if isCategoryDirectory {
			// This is a category-specific directory (workspace_tools, human_tools, etc.)
			// Category directories are created by GenerateCustomToolsCode based on tool categories
			// Dynamically determine which info struct to use based on the category name
			// Check registered tools to see what categories exist, or use virtual tools functions as fallback
			workspaceCategory := "workspace"
			humanCategory := "human"

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
				// Any other category directories (memory, custom, tavily_search, or any future categories)
				// Append to CustomTools array - supports multiple custom tool categories
				result.CustomTools = append(result.CustomTools, CustomToolsInfo{
					Category: serverName, // Category name (e.g., "tavily_search", "memory", "custom")
					Package:  dirName,    // Package name (e.g., "tavily_search_tools", "memory_tools", "custom_tools")
					Tools:    tools,
				})
			}
		} else {
			// MCP server tools - check if server should be included
			// Try both the serverName (from directory) and check if it matches any configured server names
			shouldIncludeServer := a.toolFilter.ShouldIncludeServer(serverName)

			// Also check with hyphen format (google-sheets) in case config uses that
			serverNameWithHyphen := strings.ReplaceAll(serverName, "_", "-")
			if !shouldIncludeServer && serverNameWithHyphen != serverName {
				shouldIncludeServer = a.toolFilter.ShouldIncludeServer(serverNameWithHyphen)
			}

			if !shouldIncludeServer {
				if a.Logger != nil {
					a.Logger.Debugf("🔍 [DISCOVERY] Skipping MCP server %s - server filtered out (serverName=%s, normalized=%s)", dirName, serverName, a.toolFilter.NormalizeServerName(serverName))
				}
				continue
			}

			if a.Logger != nil {
				a.Logger.Debugf("🔍 [DISCOVERY] Adding MCP server: %s (package: %s, tools: %d)", serverName, dirName, len(tools))
			}
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

	if a.Logger != nil {
		// Log summary of what was discovered
		a.Logger.Infof("🔍 [DISCOVERY] Discovery complete - Summary:")
		a.Logger.Infof("🔍 [DISCOVERY]   - MCP Servers: %d", len(result.Servers))
		for _, server := range result.Servers {
			a.Logger.Infof("🔍 [DISCOVERY]     * %s (%s): %d tools", server.Name, server.Package, len(server.Tools))
		}
		a.Logger.Infof("🔍 [DISCOVERY]   - Custom Tool Categories: %d", len(result.CustomTools))
		for _, custom := range result.CustomTools {
			a.Logger.Infof("🔍 [DISCOVERY]     * %s (%s): %d tools", custom.Category, custom.Package, len(custom.Tools))
		}
		if result.WorkspaceTools != nil {
			a.Logger.Infof("🔍 [DISCOVERY]   - WorkspaceTools (%s): %d tools", result.WorkspaceTools.Package, len(result.WorkspaceTools.Tools))
		}
		if result.HumanTools != nil {
			a.Logger.Infof("🔍 [DISCOVERY]   - HumanTools (%s): %d tools", result.HumanTools.Package, len(result.HumanTools.Tools))
		}
		if result.VirtualTools != nil {
			a.Logger.Infof("🔍 [DISCOVERY]   - VirtualTools (%s): %d tools", result.VirtualTools.Package, len(result.VirtualTools.Tools))
			if len(result.VirtualTools.Tools) > 0 {
				a.Logger.Infof("🔍 [DISCOVERY]     * Tools: %v", result.VirtualTools.Tools)
			}
		}
		a.Logger.Infof("🔍 [DISCOVERY] Final tool structure JSON size: %d bytes", len(jsonData))
	}

	return string(jsonData), nil
}

// NOTE: shouldIncludeToolInDiscovery has been replaced by ToolFilter.ShouldIncludeTool()
// The unified ToolFilter provides consistent filtering across LLM tool registration and discovery

// handleWriteCode handles the write_code virtual tool
func (a *Agent) handleWriteCode(ctx context.Context, args map[string]interface{}) (string, error) {
	code, ok := args["code"].(string)
	if !ok || code == "" {
		return "", fmt.Errorf("code parameter is required and must be a non-empty string")
	}

	// Generate unique timestamp for this code execution
	timestamp := time.Now().UnixNano()
	filename := fmt.Sprintf("code_%d.go", timestamp)

	// Get base workspace directory (use tool output handler's workspace if available)
	baseWorkspaceDir := "workspace"
	if a.toolOutputHandler != nil {
		baseWorkspaceDir = a.toolOutputHandler.GetToolOutputFolder()
	}

	// Create unique subdirectory for this code execution (isolated workspace)
	executionDir := fmt.Sprintf("code_%d", timestamp)
	workspaceDir := filepath.Join(baseWorkspaceDir, executionDir)

	// Ensure execution directory exists
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create execution directory: %w", err)
	}

	if a.Logger != nil {
		a.Logger.Infof("📁 Created isolated workspace directory: %s", workspaceDir)
	}

	// Write code to file
	filePath := filepath.Join(workspaceDir, filename)
	if err := os.WriteFile(filePath, []byte(code), 0644); err != nil {
		return "", fmt.Errorf("failed to write code file: %w", err)
	}

	if a.Logger != nil {
		a.Logger.Infof("✅ Written Go code to: %s (%d bytes)", filePath, len(code))
	}

	// Validate code for forbidden file I/O operations before execution
	if err := a.validateCodeForForbiddenFileIO(code); err != nil {
		if a.Logger != nil {
			a.Logger.Warnf("🚫 Code validation failed: %v", err)
		}
		// Return formatted error message for LLM
		return formatFileIOValidationError(err, code), nil
	}

	// Generate workspace_tools for this agent if not already generated
	// This includes folder guard validation built into the generated code
	agentGeneratedDir := a.getAgentGeneratedDir()
	if err := a.ensureAgentWorkspaceToolsGenerated(agentGeneratedDir); err != nil {
		if a.Logger != nil {
			a.Logger.Warnf("⚠️ Failed to generate workspace_tools: %v", err)
		}
		// Continue anyway - workspace_tools might already exist
	}

	// Parse code to find imported packages and set up Go workspace
	importedPackages, err := a.parseImportedPackages(code)
	if err != nil && a.Logger != nil {
		a.Logger.Warnf("⚠️ Failed to parse imports from code: %v", err)
	}

	// Set up Go workspace to import generated packages from their original location
	if len(importedPackages) > 0 {
		if err := a.setupGoWorkspace(workspaceDir, importedPackages); err != nil {
			if a.Logger != nil {
				a.Logger.Warnf("⚠️ Failed to set up Go workspace: %v", err)
			}
			// Don't fail - maybe the imports are standard library or external
		}
	}

	// Execute the Go code in-process and capture output
	output, err := a.executeGoCode(ctx, workspaceDir, filePath, code)
	if err != nil {
		// Log the full error details for debugging
		if a.Logger != nil {
			a.Logger.Errorf("❌ Code execution failed - Error: %v\nError details: %+v", err, err)
		}
		// Keep files on error for debugging - don't delete the execution directory
		// Return error output so LLM can see what went wrong
		// Format error message with clear structure for LLM to understand and fix
		errorMessage := formatCodeExecutionError(err, code)
		return errorMessage, nil
	}

	// Clean up entire execution directory after successful execution
	if err := os.RemoveAll(workspaceDir); err != nil {
		if a.Logger != nil {
			a.Logger.Debugf("⚠️ Failed to remove execution directory %s: %v", workspaceDir, err)
		}
	} else if a.Logger != nil {
		a.Logger.Debugf("🧹 Cleaned up execution directory: %s", workspaceDir)
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
	// Truncate output if it's too large to avoid context window issues
	// Use the same threshold as large output handling for consistency
	maxOutputLength := 20000 // Default fallback if handler is nil
	if a.toolOutputHandler != nil {
		maxOutputLength = a.toolOutputHandler.Threshold
	}
	if len(output) > maxOutputLength {
		truncatedOutput := output[:maxOutputLength]
		warningMsg := fmt.Sprintf("[Output truncated to %d characters. Please refine your code to produce smaller output.] ...\n", maxOutputLength)
		return warningMsg + truncatedOutput, nil
	}

	return output, nil
}

// executeGoCode executes Go code using `go run` command
// This runs the code as a separate process with full Go language support
// Code can make HTTP calls to MCP API for tool execution
func (a *Agent) executeGoCode(ctx context.Context, workspaceDir, filePath, code string) (string, error) {
	if a.Logger != nil {
		a.Logger.Infof("🔧 Executing Go code using 'go run' command: %s", filePath)
	}

	// Extract just the filename since cmd.Dir is set to workspaceDir
	// This prevents path doubling (e.g., tool_output_folder/tool_output_folder/file.go)
	filename := filepath.Base(filePath)

	// Create command to run the Go code
	cmd := exec.CommandContext(ctx, "go", "run", filename)
	cmd.Dir = workspaceDir

	// Set environment variables for code to use
	cmd.Env = append(os.Environ(),
		"MCP_API_URL=http://localhost:8000",
		// Note: MCP_SERVER_NAME is NOT needed - server name is hardcoded in generated functions
	)

	// Capture combined output (stdout + stderr)
	output, err := cmd.CombinedOutput()

	if err != nil {
		if a.Logger != nil {
			a.Logger.Errorf("❌ go run failed: %v\nOutput: %s", err, string(output))
		}
		return "", fmt.Errorf("go run failed: %w\nOutput:\n%s", err, string(output))
	}

	if a.Logger != nil {
		a.Logger.Infof("✅ Code executed successfully, output length: %d bytes", len(output))
	}

	return string(output), nil
}

// formatCodeExecutionError formats code execution errors for clear LLM understanding
func formatCodeExecutionError(err error, code string) string {
	errorStr := err.Error()
	var builder strings.Builder

	builder.WriteString("**❌ EXECUTION ERROR**\n\n")
	builder.WriteString("**Error Details:**\n```\n")
	builder.WriteString(errorStr + "\n")
	builder.WriteString("```\n\n")

	// Check for common Go errors and provide helpful tips
	if strings.Contains(errorStr, "undefined:") {
		builder.WriteString("**💡 Tip:** The code references an undefined variable or function.\n")
		builder.WriteString("- Check for typos in variable/function names\n")
		builder.WriteString("- Ensure all required packages are imported\n")
		builder.WriteString("- Verify that tool functions are called correctly\n\n")
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
		builder.WriteString("- Ensure your code compiles correctly with `go run`\n")
		builder.WriteString("- Check that all HTTP API calls use the correct endpoints\n\n")
	}

	builder.WriteString("**Your Code:**\n```go\n")
	builder.WriteString(code)
	builder.WriteString("\n```\n")

	return builder.String()
}

// parseImportedPackages parses Go code to find imported packages
// Returns a list of package names that are likely generated packages (end with _tools)
func (a *Agent) parseImportedPackages(code string) ([]string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", code, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse code: %w", err)
	}

	var packages []string
	for _, imp := range node.Imports {
		importPath := strings.Trim(imp.Path.Value, "\"")
		// Only include packages that look like generated packages (end with _tools)
		// or are in the generated directory structure
		if strings.HasSuffix(importPath, "_tools") || strings.Contains(importPath, "generated/") {
			// Extract just the package name (last part of path)
			parts := strings.Split(importPath, "/")
			packageName := parts[len(parts)-1]
			packages = append(packages, packageName)
		}
	}

	return packages, nil
}

// FileIOValidationError represents validation errors for forbidden file I/O operations
type FileIOValidationError struct {
	ForbiddenImports []string
	ForbiddenCalls   []string
}

func (e *FileIOValidationError) Error() string {
	var parts []string
	if len(e.ForbiddenImports) > 0 {
		parts = append(parts, fmt.Sprintf("forbidden imports: %v", e.ForbiddenImports))
	}
	if len(e.ForbiddenCalls) > 0 {
		parts = append(parts, fmt.Sprintf("forbidden function calls: %v", e.ForbiddenCalls))
	}
	return strings.Join(parts, "; ")
}

// validateCodeForForbiddenFileIO validates Go code for forbidden file I/O operations
// Returns an error if forbidden operations are detected
func (a *Agent) validateCodeForForbiddenFileIO(code string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", code, parser.ParseComments)
	if err != nil {
		// If parsing fails, we can't validate - let it fail at execution time
		// This prevents blocking valid code due to parse errors
		return nil
	}

	var validationErr FileIOValidationError

	// Check for forbidden imports
	forbiddenImports := map[string]bool{
		"io/ioutil": true, // Always forbidden
	}

	// Track if "os" is imported (we'll check if it's used for file I/O)
	hasOSImport := false

	for _, imp := range node.Imports {
		importPath := strings.Trim(imp.Path.Value, "\"")
		if forbiddenImports[importPath] {
			validationErr.ForbiddenImports = append(validationErr.ForbiddenImports, importPath)
		}
		if importPath == "os" {
			hasOSImport = true
		}
	}

	// Forbidden function calls from os package
	forbiddenOSFunctions := map[string]bool{
		"WriteFile": true,
		"Create":    true,
		"OpenFile":  true,
		"ReadFile":  true,
		"Open":      true,
		"Mkdir":     true,
		"MkdirAll":  true,
		"Remove":    true,
		"RemoveAll": true,
		"Rename":    true,
		"Chmod":     true,
		"Chown":     true,
		"Truncate":  true,
	}

	// Allowed os functions (environment variables, exit, etc.)
	allowedOSFunctions := map[string]bool{
		"Getenv": true,
		"Setenv": true,
		"Exit":   true,
		"Args":   true,
	}

	// Forbidden ioutil functions
	forbiddenIoutilFunctions := map[string]bool{
		"WriteFile": true,
		"ReadFile":  true,
		"ReadDir":   true,
		"ReadAll":   true,
		"TempFile":  true,
		"TempDir":   true,
	}

	// Traverse AST to find forbidden function calls
	ast.Inspect(node, func(n ast.Node) bool {
		// Check for selector expressions (e.g., os.WriteFile, ioutil.WriteFile)
		if selExpr, ok := n.(*ast.SelectorExpr); ok {
			if ident, ok := selExpr.X.(*ast.Ident); ok {
				packageName := ident.Name
				functionName := selExpr.Sel.Name

				// Check os package function calls
				if packageName == "os" && hasOSImport {
					// Whitelist behavior: check allowed first, then apply forbidden logic
					if allowedOSFunctions[functionName] {
						// Function is explicitly allowed, skip forbidden check
					} else if forbiddenOSFunctions[functionName] {
						// Function is not allowed and is in forbidden list
						validationErr.ForbiddenCalls = append(validationErr.ForbiddenCalls, fmt.Sprintf("os.%s", functionName))
					}
				}

				// Check ioutil package function calls
				if packageName == "ioutil" {
					if forbiddenIoutilFunctions[functionName] {
						validationErr.ForbiddenCalls = append(validationErr.ForbiddenCalls, fmt.Sprintf("ioutil.%s", functionName))
					}
				}
			}
		}

		return true
	})

	// If no violations found, return nil
	if len(validationErr.ForbiddenImports) == 0 && len(validationErr.ForbiddenCalls) == 0 {
		return nil
	}

	return &validationErr
}

// formatFileIOValidationError formats file I/O validation errors for LLM understanding
func formatFileIOValidationError(err error, code string) string {
	var validationErr *FileIOValidationError
	if !errors.As(err, &validationErr) {
		return fmt.Sprintf("Code validation failed: %v\n\nPlease review your code and use workspace_tools for file operations.", err)
	}

	var errorMsg strings.Builder
	errorMsg.WriteString("❌ CODE VALIDATION FAILED: Forbidden file I/O operations detected\n\n")

	if len(validationErr.ForbiddenImports) > 0 {
		errorMsg.WriteString("**Forbidden imports detected:**\n")
		for _, imp := range validationErr.ForbiddenImports {
			errorMsg.WriteString(fmt.Sprintf("- %s\n", imp))
		}
		errorMsg.WriteString("\n")
	}

	if len(validationErr.ForbiddenCalls) > 0 {
		errorMsg.WriteString("**Forbidden function calls detected:**\n")
		for _, call := range validationErr.ForbiddenCalls {
			errorMsg.WriteString(fmt.Sprintf("- %s\n", call))
		}
		errorMsg.WriteString("\n")
	}

	errorMsg.WriteString("**Why this is wrong:**\n")
	errorMsg.WriteString("Files written with standard Go file I/O (os.WriteFile, os.Create, etc.) go to the execution directory (tool_output_folder/), NOT the workspace!\n")
	errorMsg.WriteString("This causes files to be lost or written to the wrong location.\n\n")

	errorMsg.WriteString("**✅ CORRECT approach:**\n")
	errorMsg.WriteString("ALWAYS use workspace_tools for file operations:\n\n")
	errorMsg.WriteString("```go\n")
	errorMsg.WriteString("import \"workspace_tools\"\n\n")
	errorMsg.WriteString("// For writing files:\n")
	errorMsg.WriteString("params := workspace_tools.UpdateWorkspaceFileParams{\n")
	errorMsg.WriteString("    Filepath: \"data/results.json\",\n")
	errorMsg.WriteString("    Content:  data,\n")
	errorMsg.WriteString("}\n")
	errorMsg.WriteString("result, err := workspace_tools.UpdateWorkspaceFile(params)\n\n")
	errorMsg.WriteString("// For reading files:\n")
	errorMsg.WriteString("params := workspace_tools.ReadWorkspaceFileParams{\n")
	errorMsg.WriteString("    Filepath: \"data/results.json\",\n")
	errorMsg.WriteString("}\n")
	errorMsg.WriteString("content, err := workspace_tools.ReadWorkspaceFile(params)\n")
	errorMsg.WriteString("```\n\n")

	errorMsg.WriteString("**Allowed os functions:**\n")
	errorMsg.WriteString("- os.Getenv() - for environment variables\n")
	errorMsg.WriteString("- os.Setenv() - for environment variables\n")
	errorMsg.WriteString("- os.Exit() - for program termination\n\n")

	errorMsg.WriteString("Please rewrite your code using workspace_tools instead of standard file I/O operations.")

	return errorMsg.String()
}

// getAgentGeneratedDir returns the agent-specific generated directory
// Format: generated/agents/<trace_id>/
func (a *Agent) getAgentGeneratedDir() string {
	baseDir := a.getGeneratedDir()
	agentDir := filepath.Join(baseDir, "agents", string(a.TraceID))

	// Ensure directory exists
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		if a.Logger != nil {
			a.Logger.Warnf("Failed to create agent generated directory %s: %v", agentDir, err)
		}
	}

	return agentDir
}

// ensureAgentWorkspaceToolsGenerated generates workspace_tools package for this agent
// with folder guard validation built into the generated functions
func (a *Agent) ensureAgentWorkspaceToolsGenerated(agentDir string) error {
	workspaceToolsDir := filepath.Join(agentDir, "workspace_tools")

	// Check if already generated (check for api_client.go as indicator)
	apiClientFile := filepath.Join(workspaceToolsDir, "api_client.go")
	if _, err := os.Stat(apiClientFile); err == nil {
		// Already generated, skip
		if a.Logger != nil {
			a.Logger.Debugf("🔧 Workspace tools already generated for agent %s", string(a.TraceID))
		}
		return nil
	}

	// Generate workspace_tools with folder guard validation
	if a.Logger != nil {
		a.Logger.Infof("🔧 Generating workspace_tools for agent %s with folder guards", string(a.TraceID))
	}

	return a.generateWorkspaceToolsWithFolderGuards(workspaceToolsDir)
}

// generateWorkspaceToolsWithFolderGuards generates workspace_tools package with runtime path validation
func (a *Agent) generateWorkspaceToolsWithFolderGuards(workspaceToolsDir string) error {
	// Ensure directory exists
	if err := os.MkdirAll(workspaceToolsDir, 0755); err != nil {
		return fmt.Errorf("failed to create workspace_tools directory: %w", err)
	}

	// Get tool timeout
	toolTimeout := getToolExecutionTimeout(a)

	// Create go.mod for workspace_tools package
	goModPath := filepath.Join(workspaceToolsDir, "go.mod")
	goModContent := "module workspace_tools\n\ngo 1.21\n"
	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		return fmt.Errorf("failed to create go.mod for workspace_tools: %w", err)
	}

	// Generate API client
	apiClientCode := codegen.GeneratePackageHeader("workspace_tools") + "\n" + codegen.GenerateAPIClient(toolTimeout)
	apiClientFile := filepath.Join(workspaceToolsDir, "api_client.go")
	if err := os.WriteFile(apiClientFile, []byte(apiClientCode), 0644); err != nil {
		return fmt.Errorf("failed to write API client: %w", err)
	}

	// Generate path validation helper
	pathValidationCode := a.generatePathValidationHelper()
	pathValidationFile := filepath.Join(workspaceToolsDir, "path_validation.go")
	if err := os.WriteFile(pathValidationFile, []byte(pathValidationCode), 0644); err != nil {
		return fmt.Errorf("failed to write path validation: %w", err)
	}

	// Generate workspace tool functions with validation
	workspaceTools := CreateWorkspaceTools()
	for _, tool := range workspaceTools {
		if tool.Function == nil {
			continue
		}

		toolName := tool.Function.Name
		// Convert snake_case to PascalCase for Go function names
		goFuncName := a.snakeToPascalCase(toolName)

		// Determine if this is a read or write operation
		isWrite := a.isWriteOperation(toolName)

		// Generate function with path validation
		funcCode := a.generateWorkspaceToolFunction(goFuncName, tool, isWrite, toolTimeout)

		// Write function file
		fileName := toolName + ".go"
		funcFile := filepath.Join(workspaceToolsDir, fileName)
		if err := os.WriteFile(funcFile, []byte(funcCode), 0644); err != nil {
			if a.Logger != nil {
				a.Logger.Warnf("Failed to write workspace tool %s: %v", toolName, err)
			}
			continue
		}
	}

	if a.Logger != nil {
		a.Logger.Infof("✅ Generated workspace_tools package with folder guard validation")
	}

	return nil
}

// generatePathValidationHelper generates the path validation helper function
func (a *Agent) generatePathValidationHelper() string {
	// Convert slices to Go slice literal syntax
	readPathsGo := a.sliceToGoLiteral(a.FolderGuardReadPaths)
	writePathsGo := a.sliceToGoLiteral(a.FolderGuardWritePaths)

	return fmt.Sprintf(`package workspace_tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// folderGuardReadPaths contains allowed read paths for this agent
var folderGuardReadPaths = %s

// folderGuardWritePaths contains allowed write paths for this agent
var folderGuardWritePaths = %s

// isPathAllowed checks if a path is within any of the allowed paths
func isPathAllowed(inputPath string, allowedPaths []string) bool {
	// Empty allowed paths means allow all
	if len(allowedPaths) == 0 {
		return true
	}

	// Normalize input path
	inputPath = filepath.Clean(inputPath)

	// Check each allowed path
	for _, allowedPath := range allowedPaths {
		allowedPath = filepath.Clean(allowedPath)

		// Check if input is the allowed path or a subdirectory
		if inputPath == allowedPath {
			return true
		}

		// For relative paths, check if it's under the allowed path
		rel, err := filepath.Rel(allowedPath, inputPath)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return true
		}

		// Also check if relative input is under allowed path
		if !filepath.IsAbs(inputPath) {
			allowedBase := filepath.Base(allowedPath)
			if strings.HasPrefix(inputPath, allowedBase+"/") || inputPath == allowedBase {
				return true
			}
		}
	}

	return false
}

// validatePath validates a path against folder guard restrictions
// Returns error if path is not allowed
func validatePath(path string, isWrite bool) error {
	var allowedPaths []string
	if isWrite {
		allowedPaths = folderGuardWritePaths
	} else {
		// Read operations can use both read and write paths
		allowedPaths = append(folderGuardReadPaths, folderGuardWritePaths...)
	}

	if !isPathAllowed(path, allowedPaths) {
		opType := "read"
		if isWrite {
			opType = "write"
		}
		return fmt.Errorf("path %%q is outside allowed %%s boundaries (allowed: %%v)", path, opType, allowedPaths)
	}

	return nil
}
`, readPathsGo, writePathsGo)
}

// sliceToGoLiteral converts a []string to Go slice literal syntax
func (a *Agent) sliceToGoLiteral(paths []string) string {
	if len(paths) == 0 {
		return "[]string{}"
	}

	var builder strings.Builder
	builder.WriteString("[]string{")
	for i, path := range paths {
		if i > 0 {
			builder.WriteString(", ")
		}
		// Escape the string properly for Go
		builder.WriteString(fmt.Sprintf("%q", path))
	}
	builder.WriteString("}")
	return builder.String()
}

// generateWorkspaceToolFunction generates a workspace tool function with path validation
// Uses typed structs like other generated tools, but adds path validation before API call
func (a *Agent) generateWorkspaceToolFunction(funcName string, tool llmtypes.Tool, isWrite bool, timeout time.Duration) string {
	toolName := tool.Function.Name
	description := tool.Function.Description

	// Parse parameters schema to generate struct
	var schema map[string]interface{}
	if tool.Function.Parameters != nil {
		paramsBytes, _ := json.Marshal(tool.Function.Parameters)
		json.Unmarshal(paramsBytes, &schema)
	} else {
		schema = map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		}
	}

	// Generate struct using the same logic as other tools
	goStruct, err := codegen.ParseJSONSchemaToGoStruct(toolName, schema)
	if err != nil {
		// Fallback to empty struct if parsing fails
		goStruct = &codegen.GoStruct{
			Name:   funcName + "Params",
			Fields: []codegen.GoField{},
		}
	}

	// Build function code
	var code strings.Builder

	// Add package header and imports
	code.WriteString("package workspace_tools\n\n")
	code.WriteString("import (\n")
	code.WriteString("\t\"encoding/json\"\n")
	code.WriteString("\t\"fmt\"\n")
	code.WriteString(")\n\n")

	// Generate struct definition
	if goStruct != nil {
		code.WriteString(codegen.GenerateStruct(goStruct))
		code.WriteString("\n\n")
	}

	// Add function comment
	if description != "" {
		lines := strings.Split(description, "\n")
		for _, line := range lines {
			code.WriteString("// ")
			code.WriteString(line)
			code.WriteString("\n")
		}
	}
	code.WriteString("//\n")
	code.WriteString("// Usage: Import package and call with typed struct\n")
	code.WriteString(fmt.Sprintf("// Example: output, err := %s(%s{...})\n", funcName, goStruct.Name))
	code.WriteString("//\n")

	// Function signature with typed struct
	code.WriteString(fmt.Sprintf("func %s(params %s) (string, error) {\n", funcName, goStruct.Name))

	// Add path validation for path-related parameters BEFORE converting to map
	pathParams := a.getPathParameters(toolName)
	for _, paramName := range pathParams {
		// Find the field name and type in the struct
		fieldName := ""
		fieldType := ""
		for _, field := range goStruct.Fields {
			if field.JSONTag == paramName {
				fieldName = field.Name
				fieldType = field.Type
				break
			}
		}
		if fieldName == "" {
			continue
		}

		// Check if field is a pointer type
		isPointer := strings.HasPrefix(fieldType, "*")

		code.WriteString(fmt.Sprintf("\t// Validate %s path\n", paramName))

		if isPointer {
			// Handle pointer type - check if not nil and not empty
			code.WriteString(fmt.Sprintf("\tif params.%s != nil && *params.%s != \"\" {\n", fieldName, fieldName))
			code.WriteString(fmt.Sprintf("\t\tif err := validatePath(*params.%s, %v); err != nil {\n", fieldName, isWrite))
		} else {
			// Handle non-pointer type - check if not empty
			code.WriteString(fmt.Sprintf("\tif params.%s != \"\" {\n", fieldName))
			code.WriteString(fmt.Sprintf("\t\tif err := validatePath(params.%s, %v); err != nil {\n", fieldName, isWrite))
		}
		code.WriteString("\t\t\treturn \"\", err\n")
		code.WriteString("\t\t}\n")
		code.WriteString("\t}\n")
	}

	// Convert struct to map for API call (same pattern as other tools)
	code.WriteString("\t// Convert params struct to map for API call\n")
	code.WriteString("\tparamsBytes, err := json.Marshal(params)\n")
	code.WriteString("\tif err != nil {\n")
	code.WriteString("\t\treturn \"\", fmt.Errorf(\"failed to marshal parameters: %w\", err)\n")
	code.WriteString("\t}\n")
	code.WriteString("\tvar paramsMap map[string]interface{}\n")
	code.WriteString("\tif err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {\n")
	code.WriteString("\t\treturn \"\", fmt.Errorf(\"failed to unmarshal parameters: %w\", err)\n")
	code.WriteString("\t}\n\n")

	// Build request payload and call custom API (workspace_tools are custom tools, not MCP tools)
	code.WriteString("\t// Build request payload and call custom API\n")
	code.WriteString("\tpayload := map[string]interface{}{\n")
	code.WriteString(fmt.Sprintf("\t\t\"tool\": \"%s\",\n", toolName))
	code.WriteString("\t\t\"args\": paramsMap,\n")
	code.WriteString("\t}\n")
	code.WriteString("\treturn callAPI(\"/api/custom/execute\", payload)\n")
	code.WriteString("}\n")

	return code.String()
}

// getPathParameters returns the path-related parameter names for a tool
func (a *Agent) getPathParameters(toolName string) []string {
	pathParams := map[string][]string{
		"read_workspace_file":             {"filepath"},
		"list_workspace_files":            {"folder"},
		"regex_search_workspace_files":    {"folder"},
		"semantic_search_workspace_files": {"folder"},
		"update_workspace_file":           {"filepath"},
		"write_workspace_file":            {"filepath"},
		"diff_patch_workspace_file":       {"filepath"},
		"delete_workspace_file":           {"filepath"},
		"move_workspace_file":             {"source_filepath", "destination_filepath"},
		"read_image":                      {"filepath"},
		"execute_shell_command":           {"working_directory"},
	}

	if params, ok := pathParams[toolName]; ok {
		return params
	}
	return []string{}
}

// isWriteOperation determines if a tool is a write operation
func (a *Agent) isWriteOperation(toolName string) bool {
	writeOps := map[string]bool{
		"update_workspace_file":     true,
		"write_workspace_file":      true,
		"diff_patch_workspace_file": true,
		"delete_workspace_file":     true,
		"move_workspace_file":       true,
	}
	return writeOps[toolName]
}

// snakeToPascalCase converts snake_case to PascalCase
func (a *Agent) snakeToPascalCase(s string) string {
	parts := strings.Split(s, "_")
	var result strings.Builder
	for _, part := range parts {
		if len(part) > 0 {
			result.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return result.String()
}

// setupGoWorkspace creates a go.work file in workspace to import generated packages
// This uses Go 1.18+ workspace feature - no copying needed, packages stay in place
// Includes both agent-specific and shared generated packages
func (a *Agent) setupGoWorkspace(workspaceDir string, packageNames []string) error {
	generatedDir := a.getGeneratedDir()
	agentDir := a.getAgentGeneratedDir()

	// Get absolute path to workspace directory
	absWorkspaceDir, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute workspace path: %w", err)
	}

	// Create a minimal go.mod in workspace if it doesn't exist
	goModPath := filepath.Join(workspaceDir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		goModContent := "module workspace\n\ngo 1.21\n"
		if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
			return fmt.Errorf("failed to create go.mod: %w", err)
		}
		if a.Logger != nil {
			a.Logger.Debugf("✅ Created go.mod in workspace")
		}
	}

	// Track which directories we've processed to avoid duplicates
	processedDirs := make(map[string]bool)

	// Ensure each generated package has a go.mod (check both agent and shared directories)
	for _, packageName := range packageNames {
		// Check agent directory first
		agentPackageDir := filepath.Join(agentDir, packageName)
		sharedPackageDir := filepath.Join(generatedDir, packageName)

		var packageDir string
		if _, err := os.Stat(agentPackageDir); err == nil {
			packageDir = agentPackageDir
		} else if _, err := os.Stat(sharedPackageDir); err == nil {
			packageDir = sharedPackageDir
		} else {
			if a.Logger != nil {
				a.Logger.Debugf("Package %s not found in agent or shared directory, skipping", packageName)
			}
			continue
		}

		// Skip if already processed
		if processedDirs[packageDir] {
			continue
		}
		processedDirs[packageDir] = true

		// Create go.mod for the package if it doesn't exist
		pkgGoModPath := filepath.Join(packageDir, "go.mod")
		if _, err := os.Stat(pkgGoModPath); os.IsNotExist(err) {
			pkgGoModContent := fmt.Sprintf("module %s\n\ngo 1.21\n", packageName)
			if err := os.WriteFile(pkgGoModPath, []byte(pkgGoModContent), 0644); err != nil {
				if a.Logger != nil {
					a.Logger.Warnf("⚠️ Failed to create go.mod for package %s: %v", packageName, err)
				}
				continue
			}
			if a.Logger != nil {
				a.Logger.Debugf("✅ Created go.mod for package %s", packageName)
			}
		}
	}

	// Build go.work content - only add directories that have go.mod files
	var builder strings.Builder
	builder.WriteString("go 1.21\n\n")
	builder.WriteString("use (\n")
	builder.WriteString(fmt.Sprintf("    %s\n", absWorkspaceDir))

	// Track which module names we've already added to avoid duplicates
	addedModules := make(map[string]string) // module name -> directory path

	// Scan agent directory for packages with go.mod files (priority - agent packages override shared)
	if _, err := os.Stat(agentDir); err == nil {
		agentEntries, err := os.ReadDir(agentDir)
		if err == nil {
			for _, entry := range agentEntries {
				if entry.IsDir() {
					pkgDir := filepath.Join(agentDir, entry.Name())
					goModPath := filepath.Join(pkgDir, "go.mod")
					if _, err := os.Stat(goModPath); err == nil {
						// Read module name from go.mod to check for duplicates
						goModContent, err := os.ReadFile(goModPath)
						if err == nil {
							// Extract module name (simple parsing - look for "module " line)
							lines := strings.Split(string(goModContent), "\n")
							moduleName := ""
							for _, line := range lines {
								line = strings.TrimSpace(line)
								if strings.HasPrefix(line, "module ") {
									moduleName = strings.TrimSpace(strings.TrimPrefix(line, "module "))
									break
								}
							}
							if moduleName != "" {
								absPkgDir, err := filepath.Abs(pkgDir)
								if err == nil {
									addedModules[moduleName] = absPkgDir
									builder.WriteString(fmt.Sprintf("    %s\n", absPkgDir))
									if a.Logger != nil {
										a.Logger.Debugf("✅ Added agent package %s (module: %s) to workspace: %s", entry.Name(), moduleName, absPkgDir)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Scan shared generated directory for packages with go.mod files
	// Skip packages that were already added from agent directory
	if _, err := os.Stat(generatedDir); err == nil {
		generatedEntries, err := os.ReadDir(generatedDir)
		if err == nil {
			for _, entry := range generatedEntries {
				if entry.IsDir() {
					// Skip the agents directory (we already scanned it above)
					if entry.Name() == "agents" {
						continue
					}
					pkgDir := filepath.Join(generatedDir, entry.Name())
					goModPath := filepath.Join(pkgDir, "go.mod")
					if _, err := os.Stat(goModPath); err == nil {
						// Read module name from go.mod to check for duplicates
						goModContent, err := os.ReadFile(goModPath)
						if err == nil {
							// Extract module name
							lines := strings.Split(string(goModContent), "\n")
							moduleName := ""
							for _, line := range lines {
								line = strings.TrimSpace(line)
								if strings.HasPrefix(line, "module ") {
									moduleName = strings.TrimSpace(strings.TrimPrefix(line, "module "))
									break
								}
							}
							// Only add if not already added from agent directory
							if moduleName != "" && addedModules[moduleName] == "" {
								absPkgDir, err := filepath.Abs(pkgDir)
								if err == nil {
									addedModules[moduleName] = absPkgDir
									builder.WriteString(fmt.Sprintf("    %s\n", absPkgDir))
									if a.Logger != nil {
										a.Logger.Debugf("✅ Added shared package %s (module: %s) to workspace: %s", entry.Name(), moduleName, absPkgDir)
									}
								}
							} else if moduleName != "" && addedModules[moduleName] != "" {
								if a.Logger != nil {
									a.Logger.Debugf("⏭️  Skipped shared package %s (module: %s) - already added from agent directory", entry.Name(), moduleName)
								}
							}
						}
					}
				}
			}
		}
	}

	builder.WriteString(")\n")

	// Write go.work file
	goWorkPath := filepath.Join(workspaceDir, "go.work")
	if err := os.WriteFile(goWorkPath, []byte(builder.String()), 0644); err != nil {
		return fmt.Errorf("failed to create go.work: %w", err)
	}

	if a.Logger != nil {
		a.Logger.Infof("✅ Created go.work in workspace with %d packages", len(packageNames))
		a.Logger.Debugf("📄 go.work content:\n%s", builder.String())
	}

	// Run 'go work sync' to initialize the workspace and resolve modules
	// This ensures Go recognizes the workspace modules correctly
	if err := a.syncGoWorkspace(workspaceDir); err != nil {
		// Clean up go.work file on failure to avoid inconsistent state
		if removeErr := os.Remove(goWorkPath); removeErr != nil {
			if a.Logger != nil {
				a.Logger.Warnf("⚠️ Failed to remove go.work file after sync failure: %v", removeErr)
			}
		} else if a.Logger != nil {
			a.Logger.Debugf("🧹 Cleaned up go.work file after sync failure")
		}
		return fmt.Errorf("failed to sync Go workspace: %w", err)
	}

	return nil
}

// syncGoWorkspace runs 'go work sync' to initialize the workspace and resolve modules
// This ensures Go recognizes the workspace modules correctly
func (a *Agent) syncGoWorkspace(workspaceDir string) error {
	if a.Logger != nil {
		a.Logger.Debugf("🔄 Running 'go work sync' in %s", workspaceDir)
	}

	// Create command to run 'go work sync'
	cmd := exec.Command("go", "work", "sync")
	cmd.Dir = workspaceDir

	// Capture output for debugging
	output, err := cmd.CombinedOutput()
	if err != nil {
		if a.Logger != nil {
			a.Logger.Warnf("⚠️ 'go work sync' failed: %v\nOutput: %s", err, string(output))
		}
		return fmt.Errorf("go work sync failed: %w\nOutput: %s", err, string(output))
	}

	if a.Logger != nil {
		a.Logger.Debugf("✅ 'go work sync' completed successfully")
		if len(output) > 0 {
			a.Logger.Debugf("📋 Sync output: %s", string(output))
		}
	}

	return nil
}
