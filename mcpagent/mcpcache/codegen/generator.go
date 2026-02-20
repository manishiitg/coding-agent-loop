package codegen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// CacheEntryForCodeGen represents a cache entry for code generation (to avoid import cycle)
type CacheEntryForCodeGen struct {
	ServerName string
	Tools      []llmtypes.Tool
}

// parseToolSchema extracts and parses the JSON schema from a tool's parameters
func parseToolSchema(toolName string, params interface{}, logger loggerv2.Logger) (map[string]interface{}, *GoStruct, error) {
	var schema map[string]interface{}
	if params != nil {
		paramsBytes, err := json.Marshal(params)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal parameters: %w", err)
		}
		if err := json.Unmarshal(paramsBytes, &schema); err != nil {
			return nil, nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
		}
	} else {
		schema = map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		}
	}

	goStruct, err := ParseJSONSchemaToGoStruct(toolName, schema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse schema: %w", err)
	}
	return schema, goStruct, nil
}

// GenerateServerToolsCode generates Go code for MCP server tools
// Creates one file per tool with snake_case file names
func GenerateServerToolsCode(entry *CacheEntryForCodeGen, serverName string, generatedDir string, logger loggerv2.Logger, timeout time.Duration) error {
	if entry == nil || len(entry.Tools) == 0 {
		logger.Debug("No tools to generate code for server", loggerv2.String("server", serverName))
		return nil
	}

	// Get package name
	packageName := GetPackageName(serverName)

	// Create package directory
	packageDir := filepath.Join(generatedDir, packageName)
	if err := os.MkdirAll(packageDir, 0755); err != nil { //nolint:gosec // 0755 permissions are intentional for generated code directories
		return fmt.Errorf("failed to create package directory: %w", err)
	}

	// Generate common API client file - always overwrite to ensure it matches current templates
	apiClientFile := filepath.Join(packageDir, "api_client.go")
	apiClientCode := GeneratePackageHeader(packageName) + "\n" + GenerateAPIClient(timeout)
	// Always overwrite to ensure it matches current templates
	if err := os.WriteFile(apiClientFile, []byte(apiClientCode), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for generated code files
		logger.Warn("Failed to write API client file", loggerv2.Error(err))
	} else {
		logger.Debug("Generated/updated common API client file", loggerv2.String("file", apiClientFile))
	}

	generatedCount := 0

	// Generate one file per tool
	for _, tool := range entry.Tools {
		if tool.Function == nil {
			continue
		}

		toolName := tool.Function.Name
		actualToolName := toolName // Keep original tool name for MCP call
		toolDescription := tool.Function.Description

		// Parse parameters schema
		_, goStruct, err := parseToolSchema(toolName, tool.Function.Parameters, logger)
		if err != nil {
			logger.Warn("Failed to parse schema for tool", loggerv2.Error(err), loggerv2.String("tool", toolName))
			continue
		}

		// Generate file name in snake_case
		fileName := ToolNameToSnakeCase(toolName) + ".go"
		goFile := filepath.Join(packageDir, fileName)

		var codeBuilder strings.Builder

		// Add minimal package header (tool files only need json and fmt)
		codeBuilder.WriteString(GenerateToolPackageHeader(packageName))
		codeBuilder.WriteString("\n")

		// No struct generation needed - functions accept map[string]interface{} directly
		// This simplifies code and makes HTTP API calls straightforward

		// Generate function code - pass original serverName so it uses correct name (with hyphens)
		codeBuilder.WriteString(GenerateFunctionWithParams(toolName, goStruct, actualToolName, toolDescription, serverName, timeout))

		// Write file
		if err := os.WriteFile(goFile, []byte(codeBuilder.String()), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for generated code files
			logger.Warn("Failed to write Go file for tool", loggerv2.Error(err), loggerv2.String("tool", toolName))
			continue
		}

		generatedCount++
		logger.Debug("Generated Go file for tool", loggerv2.String("tool", toolName), loggerv2.String("file", goFile))
	}

	logger.Info("Generated Go code for server",
		loggerv2.String("server", serverName),
		loggerv2.Int("tools_count", generatedCount),
		loggerv2.String("package_dir", packageDir))

	// Create go.mod file for the package if it doesn't exist
	// This is required for Go workspace to recognize the package
	goModPath := filepath.Join(packageDir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		goModContent := fmt.Sprintf("module %s\n\ngo 1.21\n", packageName)
		if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for generated code files
			logger.Warn("Failed to create go.mod for package", loggerv2.String("package", packageName), loggerv2.Error(err))
			// Don't fail the whole operation, but log the warning
		} else {
			logger.Debug("Created go.mod for package", loggerv2.String("package", packageName))
		}
	}

	// Regenerate index file
	if err := GenerateIndexFile(generatedDir, logger); err != nil {
		logger.Warn("Failed to regenerate index file", loggerv2.Error(err))
		// Don't fail the whole operation if index generation fails
	}

	return nil
}

// CustomToolForCodeGen represents a custom tool for code generation
type CustomToolForCodeGen struct {
	Definition llmtypes.Tool
	Category   string // Tool category (e.g., "workspace", "human", "memory") - REQUIRED, no default
}

// GenerateCustomToolsCode generates Go code for custom tools
// Groups tools by category and generates them into category-specific directories (workspace_tools, human_tools, etc.)
// Creates one file per tool with snake_case file names
func GenerateCustomToolsCode(customTools map[string]CustomToolForCodeGen, generatedDir string, logger loggerv2.Logger, timeout time.Duration) error {
	if len(customTools) == 0 {
		if logger != nil {
			logger.Debug("No custom tools to generate code for")
		}
		return nil
	}

	// Group tools by category
	toolsByCategory := make(map[string]map[string]CustomToolForCodeGen)
	for toolName, customTool := range customTools {
		// Determine category - REQUIRED, no default
		// All tools must have a category
		category := customTool.Category
		if category == "" {
			if logger != nil {
				logger.Error("Tool has empty category - category is REQUIRED! Skipping code generation for this tool", nil,
					loggerv2.String("tool", toolName))
			}
			// Skip this tool - don't generate code without a category
			continue
		} else if logger != nil {
			logger.Debug("Tool has category", loggerv2.String("tool", toolName), loggerv2.String("category", category))
		}

		// Initialize category map if needed
		if toolsByCategory[category] == nil {
			toolsByCategory[category] = make(map[string]CustomToolForCodeGen)
		}
		toolsByCategory[category][toolName] = customTool
	}

	if logger != nil {
		logger.Info("Grouped tools into categories",
			loggerv2.Int("total_tools", len(customTools)),
			loggerv2.Int("categories_count", len(toolsByCategory)))
		for category, tools := range toolsByCategory {
			logger.Info("Category tools", loggerv2.String("category", category), loggerv2.Int("tools_count", len(tools)))
		}
	}

	totalGenerated := 0

	// Generate code for each category
	for category, categoryTools := range toolsByCategory {
		// Determine package name based on category
		// All categories get their own directory with _tools suffix (workspace_tools, human_tools, etc.)
		// 🔧 FIX: Use GetPackageName to add _tools suffix consistently
		packageName := GetPackageName(category)

		// Create package directory
		packageDir := filepath.Join(generatedDir, packageName)
		if err := os.MkdirAll(packageDir, 0755); err != nil { //nolint:gosec // 0755 permissions are intentional for generated code directories
			if logger != nil {
				logger.Warn("Failed to create package directory", loggerv2.Error(err), loggerv2.String("package_dir", packageDir))
			}
			continue
		}

		// Generate common API client file - always overwrite to ensure it matches current templates
		apiClientFile := filepath.Join(packageDir, "api_client.go")
		apiClientCode := GeneratePackageHeader(packageName) + "\n" + GenerateAPIClient(timeout)
		// Always overwrite to ensure it matches current templates
		if err := os.WriteFile(apiClientFile, []byte(apiClientCode), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for generated code files
			if logger != nil {
				logger.Warn("Failed to write API client file", loggerv2.Error(err))
			}
		} else if logger != nil {
			logger.Debug("Generated/updated common API client file", loggerv2.String("file", apiClientFile))
		}

		generatedCount := 0

		// Generate one file per custom tool in this category
		for toolName, customTool := range categoryTools {
			if customTool.Definition.Function == nil {
				continue
			}

			// Generate file name in snake_case
			fileName := ToolNameToSnakeCase(toolName) + ".go"
			goFile := filepath.Join(packageDir, fileName)

			// Skip if file already exists (tool definitions are static, no need to regenerate)
			if _, err := os.Stat(goFile); err == nil {
				logger.Debug("Skipping tool - file already exists", loggerv2.String("tool", toolName))
				continue
			}

			actualToolName := toolName // Keep original tool name for custom tool call
			toolDescription := customTool.Definition.Function.Description

			// Parse parameters schema
			_, goStruct, err := parseToolSchema(toolName, customTool.Definition.Function.Parameters, logger)
			if err != nil {
				logger.Warn("Failed to parse schema for custom tool", loggerv2.Error(err), loggerv2.String("tool", toolName))
				continue
			}

			var codeBuilder strings.Builder

			// Add minimal package header (tool files only need json and fmt)
			codeBuilder.WriteString(GenerateToolPackageHeader(packageName))
			codeBuilder.WriteString("\n")

			// No struct generation needed - functions accept map[string]interface{} directly
			// This simplifies code and makes HTTP API calls straightforward

			// Generate function code (using HTTP API)
			codeBuilder.WriteString(GenerateCustomToolFunction(toolName, goStruct, actualToolName, toolDescription, timeout))

			// Write file
			if err := os.WriteFile(goFile, []byte(codeBuilder.String()), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for generated code files
				logger.Warn("Failed to write Go file for custom tool", loggerv2.Error(err), loggerv2.String("tool", toolName))
				continue
			}

			generatedCount++
			totalGenerated++
			logger.Debug("Generated Go file for custom tool",
				loggerv2.String("tool", toolName),
				loggerv2.String("category", category),
				loggerv2.String("package_dir", packageDir))
		}

		logger.Info("Generated Go code for category tools",
			loggerv2.String("category", category),
			loggerv2.Int("tools_count", generatedCount),
			loggerv2.String("package_dir", packageDir))

		// Create go.mod file for the package if it doesn't exist
		// This is required for Go workspace to recognize the package
		goModPath := filepath.Join(packageDir, "go.mod")
		if _, err := os.Stat(goModPath); os.IsNotExist(err) {
			goModContent := fmt.Sprintf("module %s\n\ngo 1.21\n", packageName)
			if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for generated code files
				if logger != nil {
					logger.Warn("Failed to create go.mod for package", loggerv2.String("package", packageName), loggerv2.Error(err))
				}
				// Don't fail the whole operation, but log the warning
			} else if logger != nil {
				logger.Debug("Created go.mod for package", loggerv2.String("package", packageName))
			}
		}
	}

	logger.Info("Generated Go code for custom tools",
		loggerv2.Int("total_tools", totalGenerated),
		loggerv2.Int("categories_count", len(toolsByCategory)))

	// Clean up old files from custom_tools/ that have been moved to category directories
	// This handles migration from the old single custom_tools/ directory to category-specific directories
	customToolsDir := filepath.Join(generatedDir, "custom_tools")
	if customToolsDirInfo, err := os.Stat(customToolsDir); err == nil && customToolsDirInfo.IsDir() {
		// Read all files in custom_tools directory
		customToolsEntries, err := os.ReadDir(customToolsDir)
		if err == nil {
			for _, entry := range customToolsEntries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
					continue
				}

				// Extract tool name from filename (remove .go extension and convert snake_case to original)
				// Check if this tool is now in a category directory
				fileName := entry.Name()
				oldFilePath := filepath.Join(customToolsDir, fileName)

				// Check if this file exists in any category directory
				// If a tool has been moved to a category-specific directory, remove it from custom_tools
				shouldRemove := false
				for category := range toolsByCategory {
					// 🔧 FIX: Use GetPackageName for consistency
					packageName := GetPackageName(category)
					categoryDir := filepath.Join(generatedDir, packageName)
					newFilePath := filepath.Join(categoryDir, fileName)
					if _, err := os.Stat(newFilePath); err == nil {
						// File exists in category directory, safe to remove from custom_tools
						shouldRemove = true
						break
					}
				}

				if shouldRemove {
					if err := os.Remove(oldFilePath); err != nil {
						logger.Warn("Failed to remove old file", loggerv2.Error(err), loggerv2.String("file", oldFilePath))
					} else {
						logger.Debug("Cleaned up old file from custom_tools", loggerv2.String("file", fileName))
					}
				}
			}
		}
	}

	// Regenerate index file
	if err := GenerateIndexFile(generatedDir, logger); err != nil {
		logger.Warn("Failed to regenerate index file", loggerv2.Error(err))
		// Don't fail the whole operation if index generation fails
	}

	return nil
}

// GenerateVirtualToolsCode generates Go code for virtual tools
// Creates one file per tool with snake_case file names
func GenerateVirtualToolsCode(virtualTools []llmtypes.Tool, generatedDir string, logger loggerv2.Logger, timeout time.Duration) error {
	if len(virtualTools) == 0 {
		logger.Debug("No virtual tools to generate code for")
		return nil
	}

	// Create package directory
	packageDir := filepath.Join(generatedDir, "virtual_tools")
	if err := os.MkdirAll(packageDir, 0755); err != nil { //nolint:gosec // 0755 permissions are intentional for generated code directories
		return fmt.Errorf("failed to create virtual_tools directory: %w", err)
	}

	// Generate common API client file once per package
	apiClientFile := filepath.Join(packageDir, "api_client.go")
	apiClientCode := GeneratePackageHeader("virtual_tools") + "\n" + GenerateAPIClient(timeout)
	// Always overwrite to ensure it matches current templates
	if err := os.WriteFile(apiClientFile, []byte(apiClientCode), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for generated code files
		logger.Warn("Failed to write API client file", loggerv2.Error(err))
	} else {
		logger.Debug("Generated/updated common API client file", loggerv2.String("file", apiClientFile))
	}

	generatedCount := 0

	// Generate one file per virtual tool
	for _, tool := range virtualTools {
		if tool.Function == nil {
			continue
		}

		toolName := tool.Function.Name
		actualToolName := toolName // Keep original tool name for virtual tool call
		toolDescription := tool.Function.Description

		// Parse parameters schema
		_, goStruct, err := parseToolSchema(toolName, tool.Function.Parameters, logger)
		if err != nil {
			logger.Warn("Failed to parse schema for virtual tool", loggerv2.Error(err), loggerv2.String("tool", toolName))
			continue
		}

		// Generate file name in snake_case
		fileName := ToolNameToSnakeCase(toolName) + ".go"
		goFile := filepath.Join(packageDir, fileName)

		var codeBuilder strings.Builder

		// Add minimal package header (tool files only need json and fmt)
		codeBuilder.WriteString(GenerateToolPackageHeader("virtual_tools"))
		codeBuilder.WriteString("\n")

		// No struct generation needed - functions accept map[string]interface{} directly
		// This simplifies code and makes HTTP API calls straightforward

		// Generate function code (using CallVirtualTool)
		codeBuilder.WriteString(GenerateVirtualToolFunction(toolName, goStruct, actualToolName, toolDescription, timeout))

		// Write file
		if err := os.WriteFile(goFile, []byte(codeBuilder.String()), 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for generated code files
			logger.Warn("Failed to write Go file for virtual tool", loggerv2.Error(err), loggerv2.String("tool", toolName))
			continue
		}

		generatedCount++
		logger.Debug("Generated Go file for virtual tool", loggerv2.String("tool", toolName), loggerv2.String("file", goFile))
	}

	logger.Info("Generated Go code for virtual tools",
		loggerv2.Int("tools_count", generatedCount),
		loggerv2.String("package_dir", packageDir))

	// Regenerate index file
	if err := GenerateIndexFile(generatedDir, logger); err != nil {
		logger.Warn("Failed to regenerate index file", loggerv2.Error(err))
		// Don't fail the whole operation if index generation fails
	}

	return nil
}

// GenerateIndexFile generates an index.go file that re-exports all tools
// This is a backward-compatible wrapper that uses context.Background()
func GenerateIndexFile(generatedDir string, logger loggerv2.Logger) error {
	return GenerateIndexFileWithContext(context.Background(), generatedDir, logger)
}

// GenerateIndexFileWithContext generates an index.go file that re-exports all tools with context support
func GenerateIndexFileWithContext(ctx context.Context, generatedDir string, logger loggerv2.Logger) error {
	// Check context before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Scan for all *_tools directories
	// Use a goroutine to make ReadDir cancellable
	readDone := make(chan struct {
		entries []os.DirEntry
		err     error
	}, 1)
	go func() {
		entries, err := os.ReadDir(generatedDir)
		readDone <- struct {
			entries []os.DirEntry
			err     error
		}{entries, err}
	}()

	var entries []os.DirEntry
	var err error
	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-readDone:
		entries = result.entries
		err = result.err
	}

	if err != nil {
		return fmt.Errorf("failed to read generated directory: %w", err)
	}

	// Check context before processing
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	var packages []string
	for _, entry := range entries {
		// Include all _tools directories and virtual_tools directory
		if entry.IsDir() && (strings.HasSuffix(entry.Name(), "_tools") || entry.Name() == "virtual_tools") {
			packages = append(packages, entry.Name())
		}
	}

	if len(packages) == 0 {
		// No packages to export, create empty index
		indexFile := filepath.Join(generatedDir, "index.go")
		emptyIndex := `package generated

// Available tool packages:
// No packages have been generated yet.
`
		// Use goroutine to make WriteFile cancellable
		writeDone := make(chan error, 1)
		go func() {
			writeDone <- os.WriteFile(indexFile, []byte(emptyIndex), 0644) //nolint:gosec // 0644 permissions are intentional for generated code files
		}()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-writeDone:
			if err != nil {
				return fmt.Errorf("failed to write empty index file: %w", err)
			}
		}
		return nil
	}

	// Generate index file
	indexFile := filepath.Join(generatedDir, "index.go")

	var codeBuilder strings.Builder
	codeBuilder.WriteString("package generated\n\n")

	// For now, we'll generate a simple index that documents available packages
	// Full re-export would require parsing each package file to get function names
	// This is a simplified version - can be enhanced later
	codeBuilder.WriteString("// Available tool packages:\n")
	codeBuilder.WriteString("// Import these packages in your code using the full module path:\n")
	codeBuilder.WriteString("// Example: import \"mcp-agent-builder-go/agent_go/generated/context7_tools\"\n\n")

	for _, pkg := range packages {
		codeBuilder.WriteString(fmt.Sprintf("// Package %s: Import as \"mcp-agent-builder-go/agent_go/generated/%s\"\n", pkg, pkg))
	}

	// Check context before writing
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Write file using goroutine to make it cancellable
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- os.WriteFile(indexFile, []byte(codeBuilder.String()), 0644) //nolint:gosec // 0644 permissions are intentional for generated code files
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-writeDone:
		if err != nil {
			return fmt.Errorf("failed to write index file: %w", err)
		}
	}

	logger.Debug("Generated index file", loggerv2.String("file", indexFile))
	return nil
}
