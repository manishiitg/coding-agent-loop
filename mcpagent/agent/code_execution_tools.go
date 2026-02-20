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
	"regexp"
	"sort"
	"strings"
	"time"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpcache/codegen"
)

// NOTE: shouldIncludeServerInDiscovery has been replaced by ToolFilter.ShouldIncludeServer()
// The unified ToolFilter provides consistent filtering across LLM tool registration and discovery

// handleDiscoverCodeFiles handles the discover_code_files virtual tool
func (a *Agent) handleDiscoverCodeFiles(ctx context.Context, args map[string]interface{}) (string, error) {
	generatedDir := a.getGeneratedDir()
	agentDir := a.getAgentGeneratedDir()

	// Debug: Log received arguments
	if a.Logger != nil {
		a.Logger.Debug("discover_code_files called", loggerv2.Any("args", args))
	}

	// Extract server_name (required)
	serverName, ok := args["server_name"].(string)
	if !ok || serverName == "" {
		return "", fmt.Errorf("server_name parameter is required")
	}

	// Normalize server_name: convert hyphens to underscores for consistency
	// This handles cases where LLM uses "virtual-tools" instead of "virtual_tools"
	serverName = strings.ReplaceAll(serverName, "-", "_")

	// Extract tool_names (required)
	toolNamesParam, ok := args["tool_names"]
	if !ok || toolNamesParam == nil {
		return "", fmt.Errorf("tool_names parameter is required (array of tool names)")
	}

	var toolNames []string
	// Handle array of tool names
	if toolNamesArray, ok := toolNamesParam.([]interface{}); ok {
		for _, tn := range toolNamesArray {
			if toolNameStr, ok := tn.(string); ok && toolNameStr != "" {
				toolNames = append(toolNames, toolNameStr)
			}
		}
	} else if toolNamesArray, ok := toolNamesParam.([]string); ok {
		toolNames = toolNamesArray
	} else {
		return "", fmt.Errorf("tool_names must be an array of strings")
	}

	// Validate that we have at least one tool name
	if len(toolNames) == 0 {
		return "", fmt.Errorf("tool_names array must contain at least one tool name")
	}

	// Determine package name - handle both base names and _tools-suffixed names
	// 🔧 FIX: If serverName already has _tools suffix, strip it first to get base name
	// This handles cases where LLM passes "workspace_tools" instead of "workspace"
	baseServerName := serverName
	if strings.HasSuffix(serverName, "_tools") {
		baseServerName = strings.TrimSuffix(serverName, "_tools")
	}

	var packageName string
	// Handle special case for virtual_tools
	if baseServerName == "virtual" || serverName == "virtual_tools" {
		packageName = "virtual_tools"
	} else {
		// Use GetPackageName to add _tools suffix (handles both "workspace" -> "workspace_tools" and "workspace_tools" -> "workspace_tools")
		packageName = codegen.GetPackageName(baseServerName)
	}

	// Check agent directory first, then fall back to shared directory
	var packageDir string
	agentPackageDir := filepath.Join(agentDir, packageName)
	if _, err := os.Stat(agentPackageDir); err == nil {
		// Found in agent directory
		packageDir = agentPackageDir
		if a.Logger != nil {
			a.Logger.Debug("🔍 Found package in agent directory", loggerv2.String("package_dir", packageDir))
		}
	} else {
		// Fall back to shared directory
		packageDir = filepath.Join(generatedDir, packageName)
		if a.Logger != nil {
			a.Logger.Debug("🔍 Using shared directory", loggerv2.String("package_dir", packageDir))
		}
	}

	// Check if package directory exists
	_, err := os.Stat(packageDir)
	packageDirExists := err == nil

	// Apply filtering using unified ToolFilter
	// Check if server/package should be included based on filtering configuration
	// 🔧 FIX: Check category using base name (without _tools) to match IsCategoryDirectory logic
	isVirtualTool := a.toolFilter.IsVirtualToolsDirectory(packageName)
	isCategoryDir := a.toolFilter.IsCategoryDirectory(baseServerName) || a.toolFilter.IsCategoryDirectory(packageName)

	if !isVirtualTool && !isCategoryDir {
		// MCP server - check filtering using server name directly
		// Package name is the same as server name now (no _tools suffix)
		baseServerName := serverName

		// Check with both underscore and hyphen formats
		shouldInclude := a.toolFilter.ShouldIncludeServer(baseServerName)
		if !shouldInclude {
			// Also try hyphen format (google_sheets -> google-sheets)
			baseServerNameWithHyphen := strings.ReplaceAll(baseServerName, "_", "-")
			if baseServerNameWithHyphen != baseServerName {
				shouldInclude = a.toolFilter.ShouldIncludeServer(baseServerNameWithHyphen)
			}
		}

		if !shouldInclude {
			return "", fmt.Errorf("server %s is filtered out and not available", baseServerName)
		}
	} else if a.Logger != nil {
		a.Logger.Debug("🔍 [DISCOVERY] Allowing access", loggerv2.String("package", packageName), loggerv2.Any("is_virtual_tool", isVirtualTool), loggerv2.Any("is_category_dir", isCategoryDir))
	}

	// Check if package directory exists
	if !packageDirExists {
		return "", fmt.Errorf("go code package directory not found for server: %s (expected at %s)", serverName, packageDir)
	}

	// Process all requested tool files
	var result strings.Builder
	var errors []string

	for i, toolName := range toolNames {
		// Convert tool name to snake_case to match filename
		fileName := codegen.ToolNameToSnakeCase(toolName) + ".go"
		filePath := filepath.Join(packageDir, fileName)

		// Check if the specific tool file exists
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("tool file not found: %s (expected at %s). Tool name '%s' converted to filename '%s'", toolName, filePath, toolName, fileName))
			continue
		}

		// Read the tool file
		//nolint:gosec // G304: filePath is constructed from validated inputs (serverName, toolName) using filepath.Join, preventing path traversal
		content, err := os.ReadFile(filePath)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to read tool file %s: %v", filePath, err))
			continue
		}

		// Add separator between files (except for the first one)
		if i > 0 {
			result.WriteString("\n\n")
			result.WriteString(strings.Repeat("=", 80))
			result.WriteString("\n\n")
		}

		// Add file header
		result.WriteString(fmt.Sprintf("// Package: %s\n", packageName))
		result.WriteString(fmt.Sprintf("// Server: %s\n", serverName))
		result.WriteString(fmt.Sprintf("// Tool: %s\n", toolName))
		result.WriteString(fmt.Sprintf("// File: %s\n\n", fileName))
		result.WriteString(string(content))
	}

	// If there were errors and no successful files, return error
	if len(errors) > 0 && result.Len() == 0 {
		return "", fmt.Errorf("failed to discover any files:\n%s", strings.Join(errors, "\n"))
	}

	// If there were some errors but some files succeeded, append errors as warnings
	if len(errors) > 0 {
		result.WriteString("\n\n")
		result.WriteString(strings.Repeat("=", 80))
		result.WriteString("\n")
		result.WriteString("// ⚠️ WARNINGS:\n")
		for _, errMsg := range errors {
			result.WriteString(fmt.Sprintf("// %s\n", errMsg))
		}
	}

	return result.String(), nil
}

// discoverAllServersAndTools returns a JSON list of all available servers and their tools
func (a *Agent) discoverAllServersAndTools(generatedDir string) (string, error) {
	entries, err := os.ReadDir(generatedDir)
	if err != nil {
		return "", fmt.Errorf("failed to read generated directory: %w", err)
	}

	type PackageInfo struct {
		Tools []string `json:"tools"`
	}

	// DiscoveryResult is a flat map where keys are package names (e.g., "google_sheets_tools", "workspace_tools")
	// and values contain the tools available in that package
	type DiscoveryResult map[string]PackageInfo

	var result DiscoveryResult = make(map[string]PackageInfo)

	// ============================================================================
	// PHASE 1: Discover MCP servers from agent's internal state (Clients & toolToServer)
	// ============================================================================
	// This phase ensures servers are included even if generated code directories
	// don't exist yet. Code generation happens asynchronously when tools are cached,
	// but the system prompt needs to be built immediately during agent initialization.
	//
	// Why this is needed:
	// - Generated Go code directories (e.g., generated/google_sheets/) may not
	//   exist when discoverAllServersAndTools() is called
	// - Code generation is triggered by the cache manager when tools are discovered
	//   and cached, which happens asynchronously
	// - The system prompt must include available tools at agent initialization time
	// - We can discover servers immediately from the agent's active connections
	//
	// This approach provides immediate server discovery, while Phase 2 (directory scan)
	// will supplement or replace this information with more accurate data from actual
	// generated Go files (which contain the exact function names).
	// ============================================================================
	if a.Clients != nil && a.toolToServer != nil {
		// Group tools by server name
		// toolToServer maps: toolName (snake_case) -> serverName (e.g., "google-sheets" or "custom")
		serverToolsMap := make(map[string]map[string]bool)
		customToolsByCategory := make(map[string]map[string]bool) // category -> toolName -> funcName

		for toolName, serverName := range a.toolToServer {
			// Separate custom tools from MCP servers
			if serverName == "custom" {
				// This is a custom tool - group by category instead of server
				customTool, exists := a.customTools[toolName]
				if !exists || customTool.Category == "" {
					// Skip custom tools without category (shouldn't happen, but be safe)
					if a.Logger != nil {
						a.Logger.Warn("Custom tool found without category, skipping from discovery", loggerv2.String("tool", toolName))
					}
					continue
				}

				// Initialize category entry if it doesn't exist
				if customToolsByCategory[customTool.Category] == nil {
					customToolsByCategory[customTool.Category] = make(map[string]bool)
				}
				// Convert tool name from snake_case to PascalCase for Go function name
				// Example: "get_document" -> "GetDocument"
				funcName := a.snakeToPascalCase(toolName)
				customToolsByCategory[customTool.Category][funcName] = true
			} else {
				// This is an MCP server tool
				// Initialize server entry if it doesn't exist
				if serverToolsMap[serverName] == nil {
					serverToolsMap[serverName] = make(map[string]bool)
				}
				// Convert tool name from snake_case to PascalCase for Go function name
				// Example: "get_document" -> "GetDocument"
				funcName := a.snakeToPascalCase(toolName)
				serverToolsMap[serverName][funcName] = true
			}
		}

		// Add discovered MCP servers to result
		for serverName, toolsSet := range serverToolsMap {
			// Apply server-level filtering using ToolFilter
			// This ensures only selected/allowed servers are included
			shouldIncludeServer := a.toolFilter.ShouldIncludeServer(serverName)

			// Also check with underscore format (google-sheets -> google_sheets)
			// Server names in config may use hyphens, but package names use underscores
			// We need to check both formats to match correctly
			serverNameWithUnderscore := strings.ReplaceAll(serverName, "-", "_")
			if !shouldIncludeServer && serverNameWithUnderscore != serverName {
				shouldIncludeServer = a.toolFilter.ShouldIncludeServer(serverNameWithUnderscore)
			}

			// Skip this server if it's not included by the filter
			if !shouldIncludeServer {
				continue
			}

			// Convert tools set to sorted slice for consistent output
			tools := make([]string, 0, len(toolsSet))
			for toolName := range toolsSet {
				tools = append(tools, toolName)
			}
			// Sort for deterministic output (helps with debugging and testing)
			sort.Strings(tools)

			// Normalize server name for package naming (use underscore format)
			// Package names in Go must use underscores, not hyphens
			// Example: "google-sheets" -> "google_sheets"
			packageName := strings.ReplaceAll(serverName, "-", "_")

			// Add server to discovery result
			// Note: This may be replaced or augmented by Phase 2 (directory scan) if
			// the generated code directory exists, which will have more accurate function names
			result[packageName] = PackageInfo{
				Tools: tools,
			}
		}

		// Add discovered custom tools grouped by category to result
		// This matches Phase 2's structure and ensures consistency
		for category, toolsSet := range customToolsByCategory {
			// Convert tools set to sorted slice for consistent output
			tools := make([]string, 0, len(toolsSet))
			for toolName := range toolsSet {
				tools = append(tools, toolName)
			}
			// Sort for deterministic output
			sort.Strings(tools)

			// Package name uses GetPackageName to add _tools suffix (e.g., "workspace" -> "workspace_tools")
			// This matches how packages are generated in the generated/ directory
			packageName := codegen.GetPackageName(category)

			// Add custom tools to discovery result
			// Note: This may be replaced or augmented by Phase 2 (directory scan) if
			// the generated code directory exists, which will have more accurate function names
			result[packageName] = PackageInfo{
				Tools: tools,
			}
		}
	}

	// ============================================================================
	// PHASE 2: Scan generated code directories (supplement/override Phase 1 data)
	// ============================================================================
	// This phase scans the filesystem for generated package directories.
	// It serves two purposes:
	// 1. Discover custom tools (workspace, human, etc.) that aren't MCP servers
	// 2. Supplement or replace Phase 1 MCP server data with more accurate information
	//    from actual generated Go files (which contain exact function names)
	//
	// Why we prefer directory-scanned data when available:
	// - Generated Go files contain the actual function names that will be used
	// - Phase 1 uses snake_case->PascalCase conversion which may not match exactly
	// - Directory scan provides the ground truth from what's actually generated
	// ============================================================================
	// Scan for all package directories (contain Go files)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirName := entry.Name()
		// Skip virtual_tools (handled separately) and hidden directories
		if dirName == "virtual_tools" || strings.HasPrefix(dirName, ".") {
			continue
		}

		// Check if this directory contains Go files (is a valid package)
		packageDir := filepath.Join(generatedDir, dirName)
		packageEntries, err := os.ReadDir(packageDir)
		if err != nil {
			continue
		}

		hasGoFiles := false
		for _, packageEntry := range packageEntries {
			if !packageEntry.IsDir() && strings.HasSuffix(packageEntry.Name(), ".go") {
				hasGoFiles = true
				break
			}
		}

		if !hasGoFiles {
			continue
		}

		// Package name is the directory name directly (no suffix stripping needed)
		serverName := dirName

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
					a.Logger.Warn("Failed to parse Go file", loggerv2.String("file", goFile), loggerv2.Error(err))
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
						// Package/server name is the directory name (with _tools suffix)
						// - MCP servers: e.g., "google_sheets_tools"
						// - Category dirs: e.g., "workspace_tools", "human_tools"
						// - Virtual tools: "virtual_tools"
						packageOrServer := dirName
						shouldInclude := a.toolFilter.ShouldIncludeTool(packageOrServer, fn.Name.Name, isCategoryDir, isVirtualTool)

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
		isCategoryDirectory := a.toolFilter.IsCategoryDirectory(dirName)

		// Skip if no tools found (after filtering)
		if len(tools) == 0 {
			continue
		}

		// Server-level filtering: Tools are filtered above, and server-level checks are done
		// before adding servers to the result to ensure excluded servers don't appear

		if dirName == "virtual_tools" {
			// Virtual tools are already registered as real tools to the LLM
			// They don't need to be in the system prompt's tool structure discovery
			// Skip adding them to the discovery result
			continue
		} else if isCategoryDirectory {
			// This is a category-specific directory (workspace, human, data, etc.)
			// Category directories are created by GenerateCustomToolsCode based on tool categories
			// Use the directory version because it has actual function names from Go files
			// This ensures we use the most accurate data (ground truth from generated code)

			// 🔧 FIX: Normalize directory name to match Phase 1's naming convention
			// Phase 1 uses GetPackageName(category) which adds _tools suffix
			// We need to ensure Phase 2 uses the same key to avoid duplicates
			var packageName string
			if strings.HasSuffix(dirName, "_tools") {
				// Directory already has _tools suffix, use as-is
				packageName = dirName
			} else {
				// Directory doesn't have _tools suffix, normalize it to match Phase 1
				// GetPackageName always adds _tools, so we can use it directly on the base name
				packageName = codegen.GetPackageName(dirName)
			}

			// Use normalized package name as key to match Phase 1
			result[packageName] = PackageInfo{
				Tools: tools,
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
				continue
			}

			// Replace or add server with directory version (more accurate - has actual function names from Go files)
			// This ensures we use the most accurate data (ground truth from generated code)
			// The package name (dirName) is used as the key in the map
			result[dirName] = PackageInfo{
				Tools: tools,
			}
		}
	}

	// Convert to JSON
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal discovery result: %w", err)
	}

	// Log summary only if logger is available
	if a.Logger != nil {
		totalPackages := len(result)
		totalTools := 0
		for _, pkg := range result {
			totalTools += len(pkg.Tools)
		}
		a.Logger.Info("🔍 [DISCOVERY] Discovery complete",
			loggerv2.Int("total_packages", totalPackages),
			loggerv2.Int("total_tools", totalTools),
			loggerv2.Int("json_size_bytes", len(jsonData)))
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

	// Extract optional CLI arguments
	var cliArgs []string
	if argsParam, exists := args["args"]; exists && argsParam != nil {
		// Handle array of strings
		if argsArray, ok := argsParam.([]interface{}); ok {
			for _, arg := range argsArray {
				if argStr, ok := arg.(string); ok {
					cliArgs = append(cliArgs, argStr)
				} else {
					// Convert non-string args to strings
					cliArgs = append(cliArgs, fmt.Sprintf("%v", arg))
				}
			}
		} else if argsArray, ok := argsParam.([]string); ok {
			// Direct string array (less common but handle it)
			cliArgs = argsArray
		}
		if a.Logger != nil && len(cliArgs) > 0 {
			a.Logger.Info(fmt.Sprintf("📝 CLI arguments provided: %v", cliArgs))
		}
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
	if err := os.MkdirAll(workspaceDir, 0755); err != nil { //nolint:gosec // 0755 permissions are intentional for user-accessible workspace directories
		return "", fmt.Errorf("failed to create execution directory: %w", err)
	}

	// Workspace directory creation is internal - no need to log

	// Write code to file
	filePath := filepath.Join(workspaceDir, filename)
	if err := os.WriteFile(filePath, []byte(code), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for user-accessible code files
		return "", fmt.Errorf("failed to write code file: %w", err)
	}

	// Log the code that was written for debugging
	if a.Logger != nil {
		a.Logger.Info("📝 [CODE_EXECUTION] Code written",
			loggerv2.String("file_path", filePath),
			loggerv2.Int("code_length", len(code)),
			loggerv2.String("code", code))
	}

	// Validate code for forbidden file I/O operations before execution
	if err := a.validateCodeForForbiddenFileIO(code); err != nil {
		if a.Logger != nil {
			a.Logger.Warn("🚫 Code validation failed", loggerv2.Error(err))
		}
		// Return formatted error message for LLM
		return formatFileIOValidationError(err, code), nil
	}

	// NOTE: Workspace tools are now registered dynamically via workspace.RegisterWorkspaceTools()
	// from mcp-agent-builder-go. Folder guard validation is done server-side in the executors.
	// The workspace_tools package is generated via the normal custom tools code generation path.

	// Parse code to find imported packages and set up Go workspace
	// Try AST parsing first, with regex fallback if parsing fails (e.g., syntax errors)
	importedPackages, err := a.parseImportedPackages(code)
	if err != nil {
		if a.Logger != nil {
			a.Logger.Warn(fmt.Sprintf("⚠️ Failed to parse imports from code (both AST and regex methods): %v", err))
		}
		// Even if parsing completely fails, try to extract common packages from the code string
		// This is a last resort to ensure workspace setup happens
		importedPackages = a.extractImportsWithRegex(code)
		if len(importedPackages) == 0 {
			// Check if code mentions common packages even if imports aren't properly formatted
			// 🔧 FIX: Check for _tools suffix since all generated packages now use it
			if strings.Contains(code, "workspace_tools") {
				importedPackages = append(importedPackages, "workspace_tools")
			}
			if strings.Contains(code, "google_sheets_tools") {
				importedPackages = append(importedPackages, "google_sheets_tools")
			}
			// Add other common package patterns (with _tools suffix)
			commonPackages := []string{"aws_tools", "github_tools", "filesystem_tools"}
			for _, pkg := range commonPackages {
				if strings.Contains(code, pkg) && !contains(importedPackages, pkg) {
					importedPackages = append(importedPackages, pkg)
				}
			}
		}
		if a.Logger != nil && len(importedPackages) > 0 {
			a.Logger.Info(fmt.Sprintf("🔍 Extracted %d packages using fallback methods: %v", len(importedPackages), importedPackages))
		}
	}

	// Set up Go workspace to import generated packages from their original location
	// This is CRITICAL - if workspace setup fails, code execution will fail with "package not found" errors
	// Track whether we set up workspace (needed for generated packages)
	hasGeneratedPackages := len(importedPackages) > 0

	// Always attempt workspace setup if we found any packages, even if parsing had issues
	if hasGeneratedPackages {
		if a.Logger != nil {
			a.Logger.Info(fmt.Sprintf("🔧 Setting up Go workspace for %d packages: %v", len(importedPackages), importedPackages))
		}
		if err := a.setupGoWorkspace(workspaceDir, importedPackages); err != nil {
			if a.Logger != nil {
				a.Logger.Error("❌ Failed to set up Go workspace", err, loggerv2.Any("error", err))
				a.Logger.Error("❌ This will cause 'package not found' errors during code execution", nil)
			}
			// Return error immediately - workspace setup is required for generated packages
			errorMsg := fmt.Sprintf("**❌ WORKSPACE SETUP FAILED**\n\nFailed to set up Go workspace (required for generated packages): %v\n\nThis error occurs when the workspace cannot be configured to find generated tool packages.\nPlease check that:\n- Generated packages exist in the generated/ directory\n- Package directories have go.mod files\n- File permissions allow creating go.work file", err)
			return errorMsg, nil
		}
		if a.Logger != nil {
			a.Logger.Info(fmt.Sprintf("✅ Go workspace set up successfully with %d packages: %v", len(importedPackages), importedPackages))
		}
	} else if a.Logger != nil {
		a.Logger.Debug("ℹ️ No generated packages detected in code - skipping workspace setup (code only uses standard library)")
	}

	// Execute the Go code in-process and capture output
	// Pass hasGeneratedPackages flag so executeGoCode knows if go.work is expected
	output, err := a.executeGoCode(ctx, workspaceDir, filePath, code, cliArgs, hasGeneratedPackages)
	if err != nil {
		// Log the full error details for debugging
		if a.Logger != nil {
			a.Logger.Error("❌ Code execution failed", err, loggerv2.Any("error_details", err))
		}
		// Keep files on error for debugging - don't delete the execution directory
		// Return error output so LLM can see what went wrong
		// Format error message with clear structure for LLM to understand and fix
		errorMessage := formatCodeExecutionError(err, code)
		return errorMessage, nil
	}

	// Clean up entire execution directory after successful execution
	if err := os.RemoveAll(workspaceDir); err != nil {
		// Log only on error
		if a.Logger != nil {
			a.Logger.Warn("⚠️ Failed to remove execution directory", loggerv2.String("workspace_dir", workspaceDir), loggerv2.Error(err))
		}
	}

	// Ensure we always return meaningful content to the LLM
	// If output is empty, provide a message indicating successful execution with no output
	if output == "" {
		return "Code executed successfully. No output was produced (stdout/stderr were empty).", nil
	}

	// Return the execution output (this will be shown in UI and passed to LLM)
	// Truncate output if it's too large to avoid context window issues
	// Use the same threshold as context offloading for consistency
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
// cliArgs are optional command-line arguments passed to the program (accessible via os.Args)
// hasGeneratedPackages indicates whether generated packages are used (go.work is only needed in this case)
func (a *Agent) executeGoCode(ctx context.Context, workspaceDir, filePath, code string, cliArgs []string, hasGeneratedPackages bool) (string, error) {
	if a.Logger != nil {
		if len(cliArgs) > 0 {
			a.Logger.Info(fmt.Sprintf("🔧 Executing Go code using 'go run' command: %s with args: %v", filePath, cliArgs))
		} else {
			a.Logger.Info(fmt.Sprintf("🔧 Executing Go code using 'go run' command: %s", filePath))
		}
	}

	// Extract just the filename since cmd.Dir is set to workspaceDir
	// This prevents path doubling (e.g., tool_output_folder/tool_output_folder/file.go)
	filename := filepath.Base(filePath)

	// Build command arguments: go run filename.go [args...]
	cmdArgs := []string{"run", filename}
	cmdArgs = append(cmdArgs, cliArgs...)

	// Create command to run the Go code
	//nolint:gosec // G204: Code execution mode intentionally executes user-provided Go code. Code is validated via validateCodeForForbiddenFileIO before execution, and cmdArgs only contains "run" and validated file paths.
	cmd := exec.CommandContext(ctx, "go", cmdArgs...)
	cmd.Dir = workspaceDir

	// SECURITY: Start with a safe, empty environment
	cmd.Env = BuildSafeEnvironment()

	// 🔧 FIX: Only check for go.work if we have generated packages
	// go.work is only needed when importing generated packages, not for standard library
	if hasGeneratedPackages {
		// Ensure go.work is used by explicitly setting GOWORK environment variable
		// Go will automatically find go.work in the current directory or parent directories,
		// but explicitly setting it ensures it's used even if there are other go.work files
		goWorkPath := filepath.Join(workspaceDir, "go.work")
		if _, err := os.Stat(goWorkPath); err == nil {
			// go.work exists, set GOWORK to use it explicitly
			absGoWorkPath, err := filepath.Abs(goWorkPath)
			if err == nil {
				cmd.Env = append(cmd.Env, fmt.Sprintf("GOWORK=%s", absGoWorkPath))
				if a.Logger != nil {
					a.Logger.Info("🔧 Set GOWORK environment variable", loggerv2.String("path", absGoWorkPath))
				}
			}
		} else {
			// go.work doesn't exist but we need it - this is a problem!
			if a.Logger != nil {
				a.Logger.Error("❌ go.work file not found in workspace directory (required for generated packages)", err,
					loggerv2.String("workspace_dir", workspaceDir),
					loggerv2.String("go_work_path", goWorkPath))
			}
		}
	} else {
		// No generated packages - go.work not needed, just use standard Go execution
		if a.Logger != nil {
			a.Logger.Debug("ℹ️ No generated packages - executing with standard Go (go.work not needed)")
		}
	}

	// Set environment variables for code to use
	// Check if MCP_API_URL is already set in environment, otherwise use default
	mcpAPIURL := os.Getenv("MCP_API_URL")
	if mcpAPIURL == "" {
		mcpAPIURL = "http://localhost:8000"
	}

	// Set environment variables
	// Note: We don't set GOWORK explicitly - since cmd.Dir is set to workspaceDir,
	// Go will automatically find go.work in that directory if it exists
	cmd.Env = append(cmd.Env,
		"MCP_API_URL="+mcpAPIURL,
		// Note: MCP_SERVER_NAME is NOT needed - server name is hardcoded in generated functions
	)

	// 🔧 Pass session ID for MCP connection reuse (e.g., Playwright browser sharing)
	// When set, the executor will use session registry instead of creating new connections
	if a.SessionID != "" {
		cmd.Env = append(cmd.Env, "MCP_SESSION_ID="+a.SessionID)
		if a.Logger != nil {
			a.Logger.Info("🔗 Set MCP_SESSION_ID for code execution", loggerv2.String("session_id", a.SessionID))
		}
	}

	// Capture combined output (stdout + stderr)
	output, err := cmd.CombinedOutput()

	if err != nil {
		if a.Logger != nil {
			a.Logger.Error("❌ [CODE_EXECUTION] go run failed", err,
				loggerv2.String("file_path", filePath),
				loggerv2.String("output", string(output)),
				loggerv2.String("code", code))
		}
		return "", fmt.Errorf("go run failed: %w\nOutput:\n%s", err, string(output))
	}

	// Log the successful execution output for debugging
	if a.Logger != nil {
		a.Logger.Info("✅ [CODE_EXECUTION] Code executed successfully",
			loggerv2.String("file_path", filePath),
			loggerv2.Int("output_length", len(output)),
			loggerv2.String("output", string(output)))
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
	builder.WriteString("```\n")

	return builder.String()
}

// extractImportsWithRegex extracts import statements using regex as a fallback when AST parsing fails
// This is useful when code has syntax errors but we still need to set up the workspace
func (a *Agent) extractImportsWithRegex(code string) []string {
	var packages []string

	// Pattern to match import statements: import "package_name" or import ("package1" "package2")
	// This handles both single and multi-line import blocks
	importPattern := regexp.MustCompile(`import\s+(?:\(([^)]+)\)|"([^"]+)")`)

	// Find all import blocks
	matches := importPattern.FindAllStringSubmatch(code, -1)

	for _, match := range matches {
		if len(match) > 1 {
			// Handle multi-line import block (match[1] contains the block content)
			if match[1] != "" {
				// Extract individual imports from the block
				blockContent := match[1]
				// Pattern to match individual quoted imports within the block
				quotedImports := regexp.MustCompile(`"([^"]+)"`)
				individualImports := quotedImports.FindAllStringSubmatch(blockContent, -1)
				for _, imp := range individualImports {
					if len(imp) > 1 {
						importPath := imp[1]
						if strings.HasSuffix(importPath, "_tools") || strings.Contains(importPath, "generated/") {
							parts := strings.Split(importPath, "/")
							packageName := parts[len(parts)-1]
							packages = append(packages, packageName)
						}
					}
				}
			} else if len(match) > 2 && match[2] != "" {
				// Handle single import statement
				importPath := match[2]
				if strings.HasSuffix(importPath, "_tools") || strings.Contains(importPath, "generated/") {
					parts := strings.Split(importPath, "/")
					packageName := parts[len(parts)-1]
					packages = append(packages, packageName)
				}
			}
		}
	}

	// Remove duplicates
	seen := make(map[string]bool)
	var uniquePackages []string
	for _, pkg := range packages {
		if !seen[pkg] {
			seen[pkg] = true
			uniquePackages = append(uniquePackages, pkg)
		}
	}

	return uniquePackages
}

// contains checks if a string slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Returns a list of package names that are likely generated packages (end with _tools)
// Uses AST parsing first, falls back to regex if parsing fails (e.g., due to syntax errors)
func (a *Agent) parseImportedPackages(code string) ([]string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", code, parser.ParseComments)
	if err != nil {
		// AST parsing failed - try regex fallback
		if a.Logger != nil {
			a.Logger.Debug(fmt.Sprintf("⚠️ AST parsing failed, using regex fallback to extract imports: %v", err))
		}
		packages := a.extractImportsWithRegex(code)
		if len(packages) > 0 {
			if a.Logger != nil {
				a.Logger.Debug(fmt.Sprintf("✅ Regex fallback extracted %d packages: %v", len(packages), packages))
			}
			// Return packages even though parsing failed - this allows workspace setup to proceed
			return packages, nil
		}
		// Both methods failed
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
		return fmt.Sprintf("Code validation failed: %v\n\nPlease review your code and use workspace package for file operations.", err)
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
	errorMsg.WriteString("ALWAYS use workspace_tools package for file operations:\n\n")
	errorMsg.WriteString("```go\n")
	errorMsg.WriteString("import \"workspace_tools\"\n\n")
	errorMsg.WriteString("// For writing files:\n")
	errorMsg.WriteString("params := workspace_tools.UpdateWorkspaceFileParams{\n")
	errorMsg.WriteString("    Filepath: \"data/results.json\",\n")
	errorMsg.WriteString("    Content:  data,\n")
	errorMsg.WriteString("}\n")
	errorMsg.WriteString("result := workspace_tools.UpdateWorkspaceFile(params)\n\n")
	errorMsg.WriteString("// For reading files:\n")
	errorMsg.WriteString("params := workspace_tools.ReadWorkspaceFileParams{\n")
	errorMsg.WriteString("    Filepath: \"data/results.json\",\n")
	errorMsg.WriteString("}\n")
	errorMsg.WriteString("content := workspace_tools.ReadWorkspaceFile(params)\n")
	errorMsg.WriteString("```\n\n")

	errorMsg.WriteString("**Allowed os functions:**\n")
	errorMsg.WriteString("- os.Getenv() - for environment variables\n")
	errorMsg.WriteString("- os.Setenv() - for environment variables\n")
	errorMsg.WriteString("- os.Exit() - for program termination\n\n")

	errorMsg.WriteString("Please rewrite your code using workspace_tools package instead of standard file I/O operations.")

	return errorMsg.String()
}

// getAgentGeneratedDir returns the agent-specific generated directory
// Format: generated/agents/<trace_id>/
// Only creates the directory if code execution mode is enabled
func (a *Agent) getAgentGeneratedDir() string {
	baseDir := a.getGeneratedDir()
	agentDir := filepath.Join(baseDir, "agents", string(a.TraceID))

	// Only create directory if code execution mode is enabled
	// In simple agent mode, we don't need the generated directory
	if a.UseCodeExecutionMode {
		if err := os.MkdirAll(agentDir, 0755); err != nil { //nolint:gosec // 0755 permissions are intentional for user-accessible directories
			if a.Logger != nil {
				a.Logger.Warn("Failed to create agent generated directory", loggerv2.String("agent_dir", agentDir), loggerv2.Error(err))
			}
		}
	}

	return agentDir
}

// NOTE: Workspace tools generation functions have been removed.
// Workspace tools are now registered dynamically via workspace.RegisterWorkspaceTools()
// from mcp-agent-builder-go. Folder guard validation is done server-side in the executors.
// The workspace_tools package is generated via the normal custom tools code generation path
// (GenerateCustomToolsCode) when tools are registered with category "workspace".

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
	// 🔧 FIX: Use unique module name "code_execution" to avoid conflict with generated "workspace_tools" package
	// The generated workspace_tools package uses "module workspace_tools", so the execution workspace must use a different name
	goModPath := filepath.Join(workspaceDir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		goModContent := "module code_execution\n\ngo 1.21\n"
		if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for user-accessible files
			return fmt.Errorf("failed to create go.mod: %w", err)
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
			continue
		}

		// Skip if already processed
		if processedDirs[packageDir] {
			continue
		}
		processedDirs[packageDir] = true

		// Create go.mod for the package if it doesn't exist
		// This is REQUIRED for the package to be included in go.work
		pkgGoModPath := filepath.Join(packageDir, "go.mod")
		if _, err := os.Stat(pkgGoModPath); os.IsNotExist(err) {
			pkgGoModContent := fmt.Sprintf("module %s\n\ngo 1.21\n", packageName)
			if err := os.WriteFile(pkgGoModPath, []byte(pkgGoModContent), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for user-accessible files
				if a.Logger != nil {
					a.Logger.Error(fmt.Sprintf("❌ Failed to create go.mod for package %s: %v", packageName, err), err)
				}
				return fmt.Errorf("failed to create go.mod for package %s (required for workspace): %w", packageName, err)
			}
		}
	}

	// Build go.work content - only add directories that have go.mod files
	var builder strings.Builder
	builder.WriteString("go 1.21\n\n")
	builder.WriteString("use (\n")
	// 🔧 FIX: List generated packages FIRST, then execution workspace
	// This ensures Go resolves imports to generated packages before checking execution workspace
	// Generated packages are dependencies that need to be imported
	// Execution workspace is listed last as it's the main program context

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
						//nolint:gosec // G304: goModPath is constructed from validated directory structure (agentDir/entry.Name()), not user input
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
						//nolint:gosec // G304: goModPath is constructed from validated directory structure (generatedDir/entry.Name()), not user input
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
								}
							}
						}
					}
				}
			}
		}
	}

	// Include execution workspace LAST - this is where the main program runs
	// It has module code_execution (not workspace_tools) so it won't conflict with generated workspace_tools package
	// Listing it last ensures generated packages are resolved first for imports
	builder.WriteString(fmt.Sprintf("    %s\n", absWorkspaceDir))
	builder.WriteString(")\n")

	// Write go.work file
	goWorkPath := filepath.Join(workspaceDir, "go.work")
	goWorkContent := builder.String()
	if err := os.WriteFile(goWorkPath, []byte(goWorkContent), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for user-accessible files
		return fmt.Errorf("failed to create go.work: %w", err)
	}

	// Log go.work content for debugging import issues
	if a.Logger != nil {
		a.Logger.Info("📝 Created go.work file",
			loggerv2.String("path", goWorkPath),
			loggerv2.String("content", goWorkContent),
			loggerv2.Int("modules_count", len(addedModules)+1)) // +1 for execution workspace
	}

	// Run 'go work sync' to initialize the workspace and resolve modules
	// This ensures Go recognizes the workspace modules correctly
	if err := a.syncGoWorkspace(workspaceDir); err != nil {
		// Clean up go.work file on failure to avoid inconsistent state
		if removeErr := os.Remove(goWorkPath); removeErr != nil {
			if a.Logger != nil {
				a.Logger.Warn("⚠️ Failed to remove go.work file after sync failure", loggerv2.Error(removeErr))
			}
		}
		return fmt.Errorf("failed to sync Go workspace: %w", err)
	}

	// 🔧 FIX: Run 'go mod tidy' in execution workspace to ensure it's aware of workspace modules
	// This helps Go resolve imports correctly when using go.work
	execModTidyCmd := exec.Command("go", "mod", "tidy")
	execModTidyCmd.Dir = workspaceDir
	if output, err := execModTidyCmd.CombinedOutput(); err != nil {
		if a.Logger != nil {
			a.Logger.Warn("⚠️ 'go mod tidy' in execution workspace failed (non-fatal)",
				loggerv2.Error(err),
				loggerv2.String("output", string(output)))
		}
		// Non-fatal - continue even if tidy fails
	}

	return nil
}

// syncGoWorkspace runs 'go work sync' to initialize the workspace and resolve modules
// This ensures Go recognizes the workspace modules correctly
func (a *Agent) syncGoWorkspace(workspaceDir string) error {
	// Create command to run 'go work sync'
	cmd := exec.Command("go", "work", "sync")
	cmd.Dir = workspaceDir

	// Capture output for debugging
	output, err := cmd.CombinedOutput()
	if err != nil {
		if a.Logger != nil {
			a.Logger.Warn("⚠️ 'go work sync' failed", loggerv2.Error(err), loggerv2.String("output", string(output)))
		}
		return fmt.Errorf("go work sync failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// BuildSafeEnvironment creates a minimal, safe environment for shell commands
// Only includes essential variables, excludes all secrets
// Exported so it can be used by workspace security and other packages
func BuildSafeEnvironment() []string {
	return []string{
		// Essential shell variables
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/tmp",
		"USER=agent",
		"SHELL=/bin/sh",

		// Locale settings
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",

		// DO NOT include:
		// - DATABASE_URL
		// - API_KEYS
		// - JWT_SECRET
		// - GITHUB_TOKEN
		// - Any other secrets from parent process
	}
}
