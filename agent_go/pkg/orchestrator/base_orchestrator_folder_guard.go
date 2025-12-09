package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

// ShouldFilterWriteTool checks if a write tool should be filtered out (not registered)
// Returns true if the tool is a write tool and there's no write access (folder guard enabled but no write paths)
func (bo *BaseOrchestrator) ShouldFilterWriteTool(toolName string) bool {
	// Check if folder guard paths are set
	useFolderGuardPaths := len(bo.folderGuardReadPaths) > 0 || len(bo.folderGuardWritePaths) > 0

	// If folder guard is not enabled, don't filter (allow all tools)
	if !useFolderGuardPaths {
		return false
	}

	// Define write tools
	writeTools := map[string]bool{
		"update_workspace_file":     true,
		"diff_patch_workspace_file": true,
		"delete_workspace_file":     true,
		"write_workspace_file":      true,
		"move_workspace_file":       true,
	}

	// If it's a write tool and there are no write paths, filter it out
	if writeTools[toolName] && len(bo.folderGuardWritePaths) == 0 {
		return true
	}

	return false
}

// EnhanceToolDescriptionWithFolderGuard enhances a tool description with directory access information
// based on folder guard settings. Returns the original description if folder guard is disabled.
func (bo *BaseOrchestrator) EnhanceToolDescriptionWithFolderGuard(toolName, originalDescription string) string {
	// Special tools that don't operate on specific directories - skip directory access restrictions
	// GitHub tools operate on the entire workspace/repository, not specific file paths
	// Human feedback is an interactive tool that doesn't use file paths
	// Note: human_feedback may be included in WorkspaceTools (combined in server.go createCustomTools)
	specialTools := map[string]bool{
		"sync_workspace_to_github":    true,
		"get_workspace_github_status": true,
		"human_feedback":              true,
	}
	if specialTools[toolName] {
		return originalDescription
	}

	// Check if folder guard paths are set
	useFolderGuardPaths := len(bo.folderGuardReadPaths) > 0 || len(bo.folderGuardWritePaths) > 0
	workspacePath := bo.GetWorkspacePath()

	// If no folder guard paths and no workspace path, return original description
	if !useFolderGuardPaths && workspacePath == "" {
		return originalDescription
	}

	// Tool classification (same as in WrapWorkspaceToolsWithFolderGuard)
	readOnlyTools := map[string]bool{
		"read_workspace_file":             true,
		"list_workspace_files":            true,
		"regex_search_workspace_files":    true,
		"semantic_search_workspace_files": true,
		"execute_shell_command":           true,
		"read_image":                      true,
	}

	writeTools := map[string]bool{
		"update_workspace_file":     true,
		"diff_patch_workspace_file": true,
		"delete_workspace_file":     true,
		"write_workspace_file":      true,
		"move_workspace_file":       true,
	}

	// Determine tool type
	isReadOnly := readOnlyTools[toolName]
	isWrite := writeTools[toolName]

	// Build directory access information with clear LLM instructions
	var accessInfo strings.Builder
	accessInfo.WriteString("\n\n📁 **DIRECTORY ACCESS RESTRICTIONS:**")

	if useFolderGuardPaths {
		if isWrite {
			// Write operations use writePaths only
			if len(bo.folderGuardWritePaths) > 0 {
				accessInfo.WriteString("\n\n⚠️ **IMPORTANT:** You can ONLY write to these directories. All file paths in your tool calls must be within these directories:\n")
				accessInfo.WriteString(strings.Join(bo.folderGuardWritePaths, "\n"))
				accessInfo.WriteString("\n\n✅ **SPECIAL ACCESS:** The 'Downloads/' folder is always accessible for both read and write operations, regardless of restrictions.")
				accessInfo.WriteString("\n\nUse ONLY these directories (or Downloads/) when calling this tool. Paths outside these directories will be rejected.")
			} else {
				accessInfo.WriteString("\n\n⚠️ **RESTRICTED:** You have NO write access to restricted directories.")
				accessInfo.WriteString("\n\n✅ **SPECIAL ACCESS:** The 'Downloads/' folder is always accessible for both read and write operations.")
				accessInfo.WriteString("\n\nYou can ONLY use the Downloads/ folder when calling this tool.")
			}
		} else if isReadOnly {
			// Read operations can use both readPaths AND writePaths
			// Combine readPaths and writePaths, removing duplicates
			allowedPathsMap := make(map[string]bool)
			for _, path := range bo.folderGuardReadPaths {
				allowedPathsMap[path] = true
			}
			for _, path := range bo.folderGuardWritePaths {
				allowedPathsMap[path] = true
			}
			// Convert map back to slice
			allowedPaths := make([]string, 0, len(allowedPathsMap))
			for path := range allowedPathsMap {
				allowedPaths = append(allowedPaths, path)
			}
			if len(allowedPaths) > 0 {
				accessInfo.WriteString("\n\n⚠️ **IMPORTANT:** You can ONLY read from these directories. All file/folder paths in your tool calls must be within these directories:\n")
				accessInfo.WriteString(strings.Join(allowedPaths, "\n"))
				accessInfo.WriteString("\n\n✅ **SPECIAL ACCESS:** The 'Downloads/' folder is always accessible for both read and write operations, regardless of restrictions.")
				accessInfo.WriteString("\n\nUse ONLY these directories (or Downloads/) when calling this tool. Paths outside these directories will be rejected.")
			} else {
				accessInfo.WriteString("\n\n⚠️ **RESTRICTED:** You have NO read access to restricted directories.")
				accessInfo.WriteString("\n\n✅ **SPECIAL ACCESS:** The 'Downloads/' folder is always accessible for both read and write operations.")
				accessInfo.WriteString("\n\nYou can ONLY use the Downloads/ folder when calling this tool.")
			}
		} else {
			// Unknown tool type - show both read and write paths
			if len(bo.folderGuardReadPaths) > 0 || len(bo.folderGuardWritePaths) > 0 {
				accessInfo.WriteString("\n\n⚠️ **IMPORTANT:** You can ONLY access these directories. All paths in your tool calls must be within these directories:\n")
				if len(bo.folderGuardReadPaths) > 0 {
					accessInfo.WriteString("\n**Read access:**\n")
					accessInfo.WriteString(strings.Join(bo.folderGuardReadPaths, "\n"))
				}
				if len(bo.folderGuardWritePaths) > 0 {
					accessInfo.WriteString("\n**Write access:**\n")
					accessInfo.WriteString(strings.Join(bo.folderGuardWritePaths, "\n"))
				}
				accessInfo.WriteString("\n\n✅ **SPECIAL ACCESS:** The 'Downloads/' folder is always accessible for both read and write operations, regardless of restrictions.")
				accessInfo.WriteString("\n\nUse ONLY these directories (or Downloads/) when calling this tool. Paths outside these directories will be rejected.")
			} else {
				accessInfo.WriteString("\n\n⚠️ **RESTRICTED:** You have NO access to restricted directories.")
				accessInfo.WriteString("\n\n✅ **SPECIAL ACCESS:** The 'Downloads/' folder is always accessible for both read and write operations.")
				accessInfo.WriteString("\n\nYou can ONLY use the Downloads/ folder when calling this tool.")
			}
		}
	} else {
		// Fallback to workspacePath (single path mode)
		if workspacePath != "" {
			accessInfo.WriteString("\n\n⚠️ **IMPORTANT:** You can ONLY access files within this workspace directory:\n")
			accessInfo.WriteString(workspacePath)
			accessInfo.WriteString("\n\n✅ **SPECIAL ACCESS:** The 'Downloads/' folder is always accessible for both read and write operations, regardless of workspace restrictions.")
			accessInfo.WriteString("\n\nUse ONLY paths within this workspace (or Downloads/) when calling this tool.")
		} else {
			// No restrictions - don't add confusing message
			return originalDescription
		}
	}

	return originalDescription + accessInfo.String()
}

// WrapWorkspaceToolsWithFolderGuard wraps workspace tool executors with path validation
// Uses folderGuardReadPaths and folderGuardWritePaths if set, otherwise falls back to workspacePath
func (bo *BaseOrchestrator) WrapWorkspaceToolsWithFolderGuard(executors map[string]interface{}) map[string]interface{} {
	// Check if folder guard paths are set
	useFolderGuardPaths := len(bo.folderGuardReadPaths) > 0 || len(bo.folderGuardWritePaths) > 0
	workspacePath := bo.GetWorkspacePath()

	// If no folder guard paths and no workspace path, return executors unchanged
	if !useFolderGuardPaths && workspacePath == "" {
		return executors
	}

	// Tools that need path validation with their parameter names
	// Classify as read-only or write operations
	readOnlyTools := map[string][]string{
		"read_workspace_file":             {"filepath"},
		"list_workspace_files":            {"folder"},
		"regex_search_workspace_files":    {"folder"},
		"semantic_search_workspace_files": {"folder"},
		"execute_shell_command":           {"working_directory"},
	}

	writeTools := map[string][]string{
		"update_workspace_file":     {"filepath"},
		"diff_patch_workspace_file": {"filepath"},
		"delete_workspace_file":     {"filepath"},
		"write_workspace_file":      {"filepath"},
		"move_workspace_file":       {"source_filepath", "destination_filepath"}, // Both use writePaths
	}

	// Combine all tools for iteration
	toolsToValidate := make(map[string][]string)
	for tool, params := range readOnlyTools {
		toolsToValidate[tool] = params
	}
	for tool, params := range writeTools {
		toolsToValidate[tool] = params
	}

	wrappedExecutors := make(map[string]interface{})

	for toolName, executor := range executors {
		paramsToValidate, needsValidation := toolsToValidate[toolName]

		if !needsValidation {
			// Tool doesn't need validation - pass through unchanged
			wrappedExecutors[toolName] = executor
			continue
		}

		// Type assert executor to function type
		originalExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error))
		if !ok {
			// Type assertion failed - pass through unchanged
			wrappedExecutors[toolName] = executor
			continue
		}

		// Determine if this is a read-only or write tool
		_, isReadOnly := readOnlyTools[toolName]
		_, isWrite := writeTools[toolName]

		// Create wrapper function with proper variable capture
		toolNameCopy := toolName
		paramsToValidateCopy := paramsToValidate
		isReadOnlyCopy := isReadOnly
		isWriteCopy := isWrite
		wrappedExecutor := func(ctx context.Context, args map[string]interface{}) (string, error) {
			// Determine which paths to use for validation
			var allowedPaths []string

			if useFolderGuardPaths {
				if isWriteCopy {
					// Write operations use writePaths only
					allowedPaths = bo.folderGuardWritePaths
				} else if isReadOnlyCopy {
					// Read operations can use both readPaths AND writePaths (if you can write, you can read)
					// Combine readPaths and writePaths, removing duplicates
					allowedPathsMap := make(map[string]bool)
					for _, path := range bo.folderGuardReadPaths {
						allowedPathsMap[path] = true
					}
					for _, path := range bo.folderGuardWritePaths {
						allowedPathsMap[path] = true
					}
					// Convert map back to slice
					allowedPaths = make([]string, 0, len(allowedPathsMap))
					for path := range allowedPathsMap {
						allowedPaths = append(allowedPaths, path)
					}
				} else {
					// Unknown tool type - use readPaths + writePaths as default (read-like behavior)
					allowedPathsMap := make(map[string]bool)
					for _, path := range bo.folderGuardReadPaths {
						allowedPathsMap[path] = true
					}
					for _, path := range bo.folderGuardWritePaths {
						allowedPathsMap[path] = true
					}
					allowedPaths = make([]string, 0, len(allowedPathsMap))
					for path := range allowedPathsMap {
						allowedPaths = append(allowedPaths, path)
					}
				}
			} else {
				// Fallback to workspacePath (single path mode)
				if workspacePath != "" {
					allowedPaths = []string{workspacePath}
				}
			}

			// Validate and normalize all path parameters
			for _, paramName := range paramsToValidateCopy {
				if paramValue, exists := args[paramName]; exists {
					if pathStr, ok := paramValue.(string); ok {
						// Empty string or "." means workspace root - normalize to ""
						if pathStr == "" || pathStr == "." {
							args[paramName] = ""
							// Removed verbose logging
							continue
						}

						// Validate the path against allowed paths
						if err := validatePathInAllowedPaths(allowedPaths, pathStr); err != nil {
							bo.GetLogger().Warn(fmt.Sprintf("⚠️ Path validation failed for tool %s, parameter %s: %v", toolNameCopy, paramName, err))
							return "", err
						}

						// Normalize the path
						normalizedPath, _, err := normalizePathForAllowedPaths(allowedPaths, pathStr)
						if err != nil {
							bo.GetLogger().Warn(fmt.Sprintf("⚠️ Path normalization failed for tool %s, parameter %s: %v", toolNameCopy, paramName, err))
							return "", err
						}
						// Update the args with normalized path
						args[paramName] = normalizedPath
					}
				}
			}

			// Inject event emitter into context before calling executor
			ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, bo.contextAwareBridge)

			// All validations passed and paths normalized - call original executor
			return originalExecutor(ctx, args)
		}

		wrappedExecutors[toolName] = wrappedExecutor
	}

	return wrappedExecutors
}

// PrepareWorkspaceToolsWithFolderGuard wraps executors and enhances tool descriptions with folder guard information
// This is a convenience method that combines WrapWorkspaceToolsWithFolderGuard and EnhanceToolDescriptionWithFolderGuard
// Returns wrapped executors and enhanced tools (tools are modified in-place)
func (bo *BaseOrchestrator) PrepareWorkspaceToolsWithFolderGuard(tools []llmtypes.Tool, executors map[string]interface{}) ([]llmtypes.Tool, map[string]interface{}) {
	// Wrap executors with folder guard
	wrappedExecutors := bo.WrapWorkspaceToolsWithFolderGuard(executors)

	// Enhance tool descriptions with folder guard information automatically
	for i := range tools {
		if tools[i].Function != nil {
			tools[i].Function.Description = bo.EnhanceToolDescriptionWithFolderGuard(
				tools[i].Function.Name,
				tools[i].Function.Description,
			)
		}
	}

	return tools, wrappedExecutors
}
