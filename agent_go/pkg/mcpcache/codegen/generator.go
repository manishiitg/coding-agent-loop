package codegen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/utils"
)

// CacheEntryForCodeGen represents a cache entry for code generation (to avoid import cycle)
type CacheEntryForCodeGen struct {
	ServerName string
	Tools      []llmtypes.Tool
}

// GenerateServerToolsCode generates Go code for MCP server tools
// Creates one file per tool with snake_case file names
func GenerateServerToolsCode(entry *CacheEntryForCodeGen, serverName string, generatedDir string, logger utils.ExtendedLogger) error {
	if entry == nil || len(entry.Tools) == 0 {
		logger.Debugf("No tools to generate code for server: %s", serverName)
		return nil
	}

	// Get package name
	packageName := GetPackageName(serverName)

	// Create package directory
	packageDir := filepath.Join(generatedDir, packageName)
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return fmt.Errorf("failed to create package directory: %w", err)
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
		var schema map[string]interface{}
		if tool.Function.Parameters != nil {
			// Convert Parameters to map[string]interface{}
			paramsBytes, err := json.Marshal(tool.Function.Parameters)
			if err != nil {
				logger.Warnf("Failed to marshal parameters for tool %s: %v", toolName, err)
				continue
			}
			if err := json.Unmarshal(paramsBytes, &schema); err != nil {
				logger.Warnf("Failed to unmarshal parameters for tool %s: %v", toolName, err)
				continue
			}
		} else {
			schema = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			}
		}

		// Generate struct
		goStruct, err := ParseJSONSchemaToGoStruct(toolName, schema)
		if err != nil {
			logger.Warnf("Failed to parse schema for tool %s: %v", toolName, err)
			continue
		}

		// Generate file name in snake_case
		fileName := ToolNameToSnakeCase(toolName) + ".go"
		goFile := filepath.Join(packageDir, fileName)

		var codeBuilder strings.Builder

		// Add package header
		codeBuilder.WriteString(GeneratePackageHeader(packageName))
		codeBuilder.WriteString("\n")

		// No struct generation needed - functions accept map[string]interface{} directly
		// This simplifies code and works natively with yaegi interpreter

		// Generate function code
		codeBuilder.WriteString(GenerateFunctionWithParams(toolName, goStruct, actualToolName, toolDescription))

		// Write file
		if err := os.WriteFile(goFile, []byte(codeBuilder.String()), 0644); err != nil {
			logger.Warnf("Failed to write Go file for tool %s: %v", toolName, err)
			continue
		}

		generatedCount++
		logger.Debugf("Generated Go file for tool %s: %s", toolName, goFile)
	}

	logger.Infof("Generated Go code for server %s: %d tools in %s", serverName, generatedCount, packageDir)

	// Regenerate index file
	if err := GenerateIndexFile(generatedDir, logger); err != nil {
		logger.Warnf("Failed to regenerate index file: %v", err)
		// Don't fail the whole operation if index generation fails
	}

	return nil
}

// CustomToolForCodeGen represents a custom tool for code generation
type CustomToolForCodeGen struct {
	Definition llmtypes.Tool
	Category   string // Tool category (e.g., "workspace", "human", "memory", "custom")
}

// GenerateCustomToolsCode generates Go code for custom tools
// Groups tools by category and generates them into category-specific directories (workspace_tools, human_tools, etc.)
// Creates one file per tool with snake_case file names
func GenerateCustomToolsCode(customTools map[string]CustomToolForCodeGen, generatedDir string, logger utils.ExtendedLogger) error {
	if len(customTools) == 0 {
		if logger != nil {
			logger.Debugf("No custom tools to generate code for")
		}
		return nil
	}

	// Group tools by category
	toolsByCategory := make(map[string]map[string]CustomToolForCodeGen)
	for toolName, customTool := range customTools {
		// Determine category (default to "custom" if not specified)
		category := customTool.Category
		if category == "" {
			category = "custom"
		}

		// Initialize category map if needed
		if toolsByCategory[category] == nil {
			toolsByCategory[category] = make(map[string]CustomToolForCodeGen)
		}
		toolsByCategory[category][toolName] = customTool
	}

	totalGenerated := 0

	// Generate code for each category
	for category, categoryTools := range toolsByCategory {
		// Determine package name based on category
		packageName := category + "_tools"
		if category == "custom" {
			packageName = "custom_tools" // Keep "custom_tools" for uncategorized tools
		}

		// Create package directory
		packageDir := filepath.Join(generatedDir, packageName)
		if err := os.MkdirAll(packageDir, 0755); err != nil {
			if logger != nil {
				logger.Warnf("Failed to create package directory %s: %v", packageDir, err)
			}
			continue
		}

		generatedCount := 0

		// Generate one file per custom tool in this category
		for toolName, customTool := range categoryTools {
			if customTool.Definition.Function == nil {
				continue
			}

			actualToolName := toolName // Keep original tool name for custom tool call
			toolDescription := customTool.Definition.Function.Description

			// Parse parameters schema
			var schema map[string]interface{}
			if customTool.Definition.Function.Parameters != nil {
				// Convert Parameters to map[string]interface{}
				paramsBytes, err := json.Marshal(customTool.Definition.Function.Parameters)
				if err != nil {
					logger.Warnf("Failed to marshal parameters for custom tool %s: %v", toolName, err)
					continue
				}
				if err := json.Unmarshal(paramsBytes, &schema); err != nil {
					logger.Warnf("Failed to unmarshal parameters for custom tool %s: %v", toolName, err)
					continue
				}
			} else {
				schema = map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
					"required":   []string{},
				}
			}

			// Generate struct
			goStruct, err := ParseJSONSchemaToGoStruct(toolName, schema)
			if err != nil {
				logger.Warnf("Failed to parse schema for custom tool %s: %v", toolName, err)
				continue
			}

			// Generate file name in snake_case
			fileName := ToolNameToSnakeCase(toolName) + ".go"
			goFile := filepath.Join(packageDir, fileName)

			var codeBuilder strings.Builder

			// Add package header
			codeBuilder.WriteString(GeneratePackageHeader(packageName))
			codeBuilder.WriteString("\n")

			// No struct generation needed - functions accept map[string]interface{} directly
			// This simplifies code and works natively with yaegi interpreter

			// Generate function code (using CallCustomTool)
			codeBuilder.WriteString(GenerateCustomToolFunction(toolName, goStruct, actualToolName, toolDescription))

			// Write file
			if err := os.WriteFile(goFile, []byte(codeBuilder.String()), 0644); err != nil {
				logger.Warnf("Failed to write Go file for custom tool %s: %v", toolName, err)
				continue
			}

			generatedCount++
			totalGenerated++
			logger.Debugf("Generated Go file for custom tool %s (category: %s) in %s", toolName, category, packageDir)
		}

		logger.Infof("Generated Go code for %s tools: %d tools in %s", category, generatedCount, packageDir)
	}

	logger.Infof("Generated Go code for custom tools: %d total tools across %d categories", totalGenerated, len(toolsByCategory))

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
				shouldRemove := false
				for category := range toolsByCategory {
					if category == "custom" {
						continue // Don't remove files that belong to custom category
					}
					packageName := category + "_tools"
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
						logger.Warnf("Failed to remove old file %s: %v", oldFilePath, err)
					} else {
						logger.Debugf("Cleaned up old file from custom_tools: %s (moved to category directory)", fileName)
					}
				}
			}
		}
	}

	// Regenerate index file
	if err := GenerateIndexFile(generatedDir, logger); err != nil {
		logger.Warnf("Failed to regenerate index file: %v", err)
		// Don't fail the whole operation if index generation fails
	}

	return nil
}

// GenerateVirtualToolsCode generates Go code for virtual tools
// Creates one file per tool with snake_case file names
func GenerateVirtualToolsCode(virtualTools []llmtypes.Tool, generatedDir string, logger utils.ExtendedLogger) error {
	if len(virtualTools) == 0 {
		logger.Debugf("No virtual tools to generate code for")
		return nil
	}

	// Create package directory
	packageDir := filepath.Join(generatedDir, "virtual_tools")
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return fmt.Errorf("failed to create virtual_tools directory: %w", err)
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
		var schema map[string]interface{}
		if tool.Function.Parameters != nil {
			// Convert Parameters to map[string]interface{}
			paramsBytes, err := json.Marshal(tool.Function.Parameters)
			if err != nil {
				logger.Warnf("Failed to marshal parameters for virtual tool %s: %v", toolName, err)
				continue
			}
			if err := json.Unmarshal(paramsBytes, &schema); err != nil {
				logger.Warnf("Failed to unmarshal parameters for virtual tool %s: %v", toolName, err)
				continue
			}
		} else {
			schema = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			}
		}

		// Generate struct
		goStruct, err := ParseJSONSchemaToGoStruct(toolName, schema)
		if err != nil {
			logger.Warnf("Failed to parse schema for virtual tool %s: %v", toolName, err)
			continue
		}

		// Generate file name in snake_case
		fileName := ToolNameToSnakeCase(toolName) + ".go"
		goFile := filepath.Join(packageDir, fileName)

		var codeBuilder strings.Builder

		// Add package header
		codeBuilder.WriteString(GeneratePackageHeader("virtual_tools"))
		codeBuilder.WriteString("\n")

		// No struct generation needed - functions accept map[string]interface{} directly
		// This simplifies code and works natively with yaegi interpreter

		// Generate function code (using CallVirtualTool)
		codeBuilder.WriteString(GenerateVirtualToolFunction(toolName, goStruct, actualToolName, toolDescription))

		// Write file
		if err := os.WriteFile(goFile, []byte(codeBuilder.String()), 0644); err != nil {
			logger.Warnf("Failed to write Go file for virtual tool %s: %v", toolName, err)
			continue
		}

		generatedCount++
		logger.Debugf("Generated Go file for virtual tool %s: %s", toolName, goFile)
	}

	logger.Infof("Generated Go code for virtual tools: %d tools in %s", generatedCount, packageDir)

	// Regenerate index file
	if err := GenerateIndexFile(generatedDir, logger); err != nil {
		logger.Warnf("Failed to regenerate index file: %v", err)
		// Don't fail the whole operation if index generation fails
	}

	return nil
}

// GenerateIndexFile generates an index.go file that re-exports all tools
func GenerateIndexFile(generatedDir string, logger utils.ExtendedLogger) error {
	// Scan for all *_tools directories
	entries, err := os.ReadDir(generatedDir)
	if err != nil {
		return fmt.Errorf("failed to read generated directory: %w", err)
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

// No tool packages available yet
`
		if err := os.WriteFile(indexFile, []byte(emptyIndex), 0644); err != nil {
			return fmt.Errorf("failed to write empty index file: %w", err)
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
	codeBuilder.WriteString("// Example: import \"mcp-agent/agent_go/generated/context7_tools\"\n\n")

	for _, pkg := range packages {
		codeBuilder.WriteString(fmt.Sprintf("// Package %s: Import as \"mcp-agent/agent_go/generated/%s\"\n", pkg, pkg))
	}

	// Write file
	if err := os.WriteFile(indexFile, []byte(codeBuilder.String()), 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}

	logger.Debugf("Generated index file: %s", indexFile)
	return nil
}
