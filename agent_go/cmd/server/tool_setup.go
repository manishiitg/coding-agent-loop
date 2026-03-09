package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"mcp-agent-builder-go/agent_go/pkg/common"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

// extractWorkspacePathFromObjective extracts the workspace path from the objective string
// Looks for pattern: "📁 Files in context: Workflow/[FolderName]"
func extractWorkspacePathFromObjective(objective string) string {
	// Look for pattern: "📁 Files in context: Workflow/[FolderName]"
	// This is the standard pattern used by workflow orchestrator
	prefix := "📁 Files in context: "
	if idx := strings.Index(objective, prefix); idx != -1 {
		// Find the start of the workspace path
		start := idx + len(prefix)
		// Find the end of the workspace path (typically before a newline or end of string)
		end := strings.Index(objective[start:], "\n")
		if end == -1 {
			return strings.TrimSpace(objective[start:])
		}
		return strings.TrimSpace(objective[start : start+end])
	}
	return ""
}

// extractFileContextWriteFolders parses "📁 Files in context: path1, path2" from query string
// and returns paths that should be granted write access in the FolderGuard.
// Files (last component contains '.') are returned as-is (exact match).
// Folders are returned as-is (prefix match in isPathAllowed).
// Skips _users/, Chats/ (already handled), and root-level files (no '/' in path).
func extractFileContextWriteFolders(query string) []string {
	prefix := "📁 Files in context: "
	idx := strings.Index(query, prefix)
	if idx == -1 {
		return nil
	}

	start := idx + len(prefix)
	end := strings.Index(query[start:], "\n")
	var line string
	if end == -1 {
		line = strings.TrimSpace(query[start:])
	} else {
		line = strings.TrimSpace(query[start : start+end])
	}

	if line == "" {
		return nil
	}

	seen := make(map[string]bool)
	var result []string

	parts := strings.Split(line, ",")
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		// Clean the path
		p = filepath.Clean(p)

		// Skip protected/already-handled paths
		pLower := strings.ToLower(p)
		if strings.HasPrefix(pLower, "_users") || strings.HasPrefix(pLower, "chats") {
			continue
		}

		// Skip root-level files (no directory component) — don't grant root write access
		if !strings.Contains(p, string(filepath.Separator)) && !strings.Contains(p, "/") {
			continue
		}

		// Deduplicate
		if seen[p] {
			continue
		}
		seen[p] = true
		result = append(result, p)
	}

	return result
}

// extractRootCauseError returns the raw error message without any processing
// It unwraps the error chain to find the deepest/most specific error
func extractRootCauseError(err error) string {
	if err == nil {
		return "unknown error"
	}

	// Unwrap the error chain to find the deepest error (the actual root cause)
	currentErr := err
	deepestErr := err
	maxDepth := 20 // Limit depth to prevent infinite loops

	for i := 0; i < maxDepth; i++ {
		// Try to unwrap using errors.Unwrap
		unwrapped := errors.Unwrap(currentErr)
		if unwrapped == nil {
			break
		}
		deepestErr = unwrapped
		currentErr = unwrapped
	}

	// Return the raw error message from the deepest error (no pattern matching, no filtering)
	return deepestErr.Error()
}

// collectVirtualToolNames extracts tool names from a list of llmtypes.Tool definitions.
// Used to build PreDiscoveredTools so all virtual/custom tools stay visible in tool search mode.
func collectVirtualToolNames(toolSets ...[]llmtypes.Tool) []string {
	var names []string
	for _, tools := range toolSets {
		for _, t := range tools {
			if t.Function != nil && t.Function.Name != "" {
				names = append(names, t.Function.Name)
			}
		}
	}
	return names
}

// createCustomTools creates workspace and human tools for orchestrator/workflow agents
// workflowMode: if true, includes advanced + human + todo tools for workflow mode
//
//	if false, only workspace_advanced tools for chat mode (shell, image, web fetch, PDF)
//
// Returns: tools, executors, and a map of tool names to their categories
// Tools from CreateWorkspaceAdvancedTools() get category "workspace_advanced"
// All tools from CreateHumanTools() get category "human_tools"
//
// Note: workspace_basic and workspace_git tools are deprecated — shell_command covers
// all file operations and git is handled via shell_command when needed.
func createCustomTools(workflowMode bool) ([]llmtypes.Tool, map[string]interface{}, map[string]string) {
	var allTools []llmtypes.Tool
	allExecutors := make(map[string]interface{})
	toolCategories := make(map[string]string)

	// Create workspace advanced tools (always included)
	workspaceAdvancedCategory := virtualtools.GetWorkspaceAdvancedToolCategory()
	workspaceAdvancedTools := virtualtools.CreateWorkspaceAdvancedTools()
	workspaceAdvancedExecutors := virtualtools.CreateWorkspaceAdvancedToolExecutors()

	// Add advanced tools
	allTools = append(allTools, workspaceAdvancedTools...)
	for name, executor := range workspaceAdvancedExecutors {
		allExecutors[name] = executor
	}

	// Advanced tools get workspace_advanced category
	for _, tool := range workspaceAdvancedTools {
		if tool.Function != nil {
			toolCategories[tool.Function.Name] = workspaceAdvancedCategory
		}
	}

	// Workflow mode: include human + todo tools + workspace_basic executors (for internal Go operations)
	if workflowMode {
		// Add workspace_basic executors ONLY (not tool definitions) — needed for internal
		// Go operations (ReadWorkspaceFile, WriteWorkspaceFile, ListWorkspaceFiles, etc.)
		// These are NOT exposed to LLMs as tools; shell_command handles all LLM file operations.
		workspaceBasicCategory := virtualtools.GetWorkspaceBasicToolCategory()
		workspaceBasicExecutors := virtualtools.CreateWorkspaceBasicToolExecutors()
		for name, executor := range workspaceBasicExecutors {
			allExecutors[name] = executor
			toolCategories[name] = workspaceBasicCategory
		}

		// Add human tools
		humanCategory := virtualtools.GetHumanToolCategory()
		humanTools := virtualtools.CreateHumanTools()
		humanExecutors := virtualtools.CreateHumanToolExecutors()

		allTools = append(allTools, humanTools...)
		for name, executor := range humanExecutors {
			allExecutors[name] = executor
		}

		// Assign category to human tools
		for _, tool := range humanTools {
			if tool.Function != nil {
				toolCategories[tool.Function.Name] = humanCategory
			}
		}

		// Add todo tools for todo task orchestrator
		todoCategory := virtualtools.GetTodoToolCategory()
		todoTools := virtualtools.CreateTodoTools()
		todoExecutors := virtualtools.CreateTodoToolExecutors()

		allTools = append(allTools, todoTools...)
		for name, executor := range todoExecutors {
			allExecutors[name] = executor
		}

		// Assign category to todo tools
		for _, tool := range todoTools {
			if tool.Function != nil {
				toolCategories[tool.Function.Name] = todoCategory
			}
		}

		// Note: Browser tools are NOT added unconditionally here.
		// They are added conditionally based on preset.EnableBrowserAccess in workflow initialization.
		// See the workflow initialization section where browser tools are added if enabled.
	}

	return allTools, allExecutors, toolCategories
}

// enhanceToolDescriptionForChatMode enhances a tool description with chat-mode-specific directory access information
func enhanceToolDescriptionForChatMode(toolName, originalDescription string) string {
	// Special tools that don't operate on specific directories
	specialTools := map[string]bool{
		"sync_workspace_to_github":    true,
		"get_workspace_github_status": true,
		"human_feedback":              true,
	}
	if specialTools[toolName] {
		return originalDescription
	}

	// Write tools are restricted to Chats/
	writeTools := map[string]bool{
		"update_workspace_file":     true,
		"diff_patch_workspace_file": true,
		"delete_workspace_file":     true,
		"write_workspace_file":      true,
		"move_workspace_file":       true,
		"execute_shell_command":     true, // Shell can write too
	}

	var accessInfo strings.Builder
	accessInfo.WriteString("\n\n📁 **DIRECTORY ACCESS RESTRICTIONS (CHAT MODE):**")

	if writeTools[toolName] {
		accessInfo.WriteString("\n\n⚠️ **IMPORTANT:** You can ONLY write/modify files in the 'Chats/' folder. All other folders are read-only.")
		accessInfo.WriteString("\nExample: 'Chats/output.txt', 'Chats/data.json'.")
	} else {
		accessInfo.WriteString("\n\nYou have READ access to all workspace folders (Workflow/, skills/, etc.), but you can only WRITE to the 'Chats/' folder.")
	}

	return originalDescription + accessInfo.String()
}

// enhanceToolDescriptionForPlanMode augments workspace tool descriptions for multi-agent plan mode.
// Tells the LLM that its primary write folder is Plans/ (not Chats/).
func enhanceToolDescriptionForPlanMode(toolName, originalDescription string) string {
	specialTools := map[string]bool{
		"sync_workspace_to_github":    true,
		"get_workspace_github_status": true,
		"human_feedback":              true,
	}
	if specialTools[toolName] {
		return originalDescription
	}

	writeTools := map[string]bool{
		"update_workspace_file":     true,
		"diff_patch_workspace_file": true,
		"delete_workspace_file":     true,
		"write_workspace_file":      true,
		"move_workspace_file":       true,
		"execute_shell_command":     true,
	}

	var accessInfo strings.Builder
	accessInfo.WriteString("\n\n📁 **DIRECTORY ACCESS RESTRICTIONS (MULTI-AGENT MODE):**")

	if writeTools[toolName] {
		accessInfo.WriteString("\n\n⚠️ **IMPORTANT:** You can write to 'Plans/' (primary) and 'Chats/' folders. All other folders are read-only.")
		accessInfo.WriteString("\nSave plan outputs inside the plan folder (e.g. 'Plans/{plan_id}/output.txt').")
	} else {
		accessInfo.WriteString("\n\nYou have READ access to all workspace folders. WRITE access is restricted to 'Plans/' and 'Chats/' folders.")
	}

	return originalDescription + accessInfo.String()
}

// wrapExecutorsWithChatModeFolderGuard wraps workspace tool executors to restrict chat mode writes.
// By default, only Chats/ is writable. Pass additionalWriteFolders to allow extra folders (e.g. "skills/custom/").
// This creates a wrapper that:
// 1. BLOCKS access to _users/ directory (internal structure, prevents accessing other users' data)
// 2. ALLOWS read access to all other folders (skills/, Workflow/, Downloads/, etc.)
// 3. ONLY ALLOWS write access to Chats/ folder (user's workspace, plus any additionalWriteFolders)
// 4. Restricts shell writes to allowed folders
// Note: Chats/ and Downloads/ are routed to /_users/{user_id}/ by the workspace API via X-User-ID header
func wrapExecutorsWithChatModeFolderGuard(executors map[string]func(ctx context.Context, args map[string]interface{}) (string, error), additionalWriteFolders ...string) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	// Protected folders - block ALL access (read and write)
	// Only _users/ is blocked to prevent direct access to internal user directory structure
	protectedFolders := []string{"_users"}

	// Build the list of allowed write folders from per-user folders.
	allowedWriteFolders := make([]string, 0, len(common.PerUserFolders))
	for _, f := range common.PerUserFolders {
		allowedWriteFolders = append(allowedWriteFolders, f+"/")
	}
	allowedWriteFolders = append(allowedWriteFolders, additionalWriteFolders...)

	// For shell sandboxing, pass all allowed write folders
	shellAllowedFolders := make([]string, len(allowedWriteFolders))
	copy(shellAllowedFolders, allowedWriteFolders)

	// Check if any allowed write folder grants Workflow/ access (case-insensitive)
	hasWorkflowAccess := false
	for _, f := range allowedWriteFolders {
		if strings.HasPrefix(strings.ToLower(filepath.Clean(f)), "workflow") {
			hasWorkflowAccess = true
			break
		}
	}

	// Write tools that should be restricted
	writeTools := map[string]bool{
		"update_workspace_file":     true,
		"delete_workspace_file":     true,
		"move_workspace_file":       true,
		"diff_patch_workspace_file": true,
		"write_workspace_file":      true,
	}

	// Path parameters to check for all tools (both read and write)
	allPathParams := []string{"filepath", "source_filepath", "destination_filepath", "folder", "pattern"}

	// Path parameters specific to write operations
	writePathParams := []string{"filepath", "source_filepath", "destination_filepath"}

	// Helper: check if a cleaned path is within a protected folder
	isPathProtected := func(cleanedPath string) bool {
		pathLower := strings.ToLower(cleanedPath)
		for _, folder := range protectedFolders {
			folderLower := strings.ToLower(folder)
			if pathLower == folderLower ||
				strings.HasPrefix(pathLower, folderLower+"/") ||
				strings.HasPrefix(pathLower, folderLower+"\\") {
				return true
			}
		}
		return false
	}

	// Helper: check if a cleaned path is within any allowed write folder
	isPathAllowed := func(cleanedPath string) bool {
		for _, folder := range allowedWriteFolders {
			folderClean := filepath.Clean(folder)
			if cleanedPath == folderClean ||
				strings.HasPrefix(cleanedPath, folderClean+"/") ||
				strings.HasPrefix(cleanedPath, folderClean+"\\") {
				return true
			}
		}
		return false
	}

	wrappedExecutors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	for toolName, executor := range executors {
		toolNameCopy := toolName
		originalExecutor := executor

		wrappedExecutor := func(ctx context.Context, args map[string]interface{}) (string, error) {
			// FIRST: Check for protected folder access (applies to ALL tools)
			for _, paramName := range allPathParams {
				if paramValue, exists := args[paramName]; exists {
					if pathStr, ok := paramValue.(string); ok && pathStr != "" {
						cleanedPath := filepath.Clean(pathStr)
						if isPathProtected(cleanedPath) {
							log.Printf("[CHAT MODE FOLDER GUARD] Blocked access to protected folder '%s' for tool %s", pathStr, toolNameCopy)
							return "", fmt.Errorf("access denied: '%s' is a protected system folder (Chats/, Downloads/, _users/ are off-limits)", pathStr)
						}
					}
				}
			}

			// For shell commands, check for protected folder references and restrict writes
			if toolNameCopy == "execute_shell_command" {
				if cmdValue, exists := args["command"]; exists {
					if cmdStr, ok := cmdValue.(string); ok {
						cmdLower := strings.ToLower(cmdStr)

						// Check if shell command references protected folders
						for _, folder := range protectedFolders {
							folderLower := strings.ToLower(folder)
							if strings.Contains(cmdLower, folderLower+"/") ||
								strings.Contains(cmdLower, folderLower+" ") ||
								strings.Contains(cmdLower, " "+folderLower) ||
								strings.Contains(cmdLower, "/"+folderLower) ||
								strings.HasSuffix(cmdLower, folderLower) {
								log.Printf("[CHAT MODE FOLDER GUARD] Blocked shell command referencing protected folder %s: %s", folder, cmdStr)
								return "", fmt.Errorf("access denied: shell commands cannot reference '%s/' folder (protected system folder)", folder)
							}
						}

						// Check if shell command references Workflow/ folder (blocked unless @context grants access)
						if !hasWorkflowAccess {
							workflowLower := "workflow"
							if strings.Contains(cmdLower, workflowLower+"/") ||
								strings.Contains(cmdLower, workflowLower+" ") ||
								strings.Contains(cmdLower, " "+workflowLower) ||
								strings.Contains(cmdLower, "/"+workflowLower) ||
								strings.HasSuffix(cmdLower, workflowLower) {
								log.Printf("[CHAT MODE FOLDER GUARD] Blocked shell command referencing Workflow/: %s", cmdStr)
								return "", fmt.Errorf("access denied: shell commands cannot reference 'Workflow/' folder in chat mode")
							}
						}
					}
				}
				// Inject allowed write folders for kernel-level sandboxing
				ctx = context.WithValue(ctx, common.FolderGuardAllowedWriteFolderKey, shellAllowedFolders)
				// Set chat-mode read paths: all standard user folders + shared resources
				chatReadFolders := []string{"Chats/", "Downloads/", "Plans/", "skills/", "subagents/", "Workflow/"}
				ctx = context.WithValue(ctx, common.FolderGuardReadPathsKey, chatReadFolders)
				// Default working directory for chat mode — "." → "Chats"
				ctx = context.WithValue(ctx, common.DefaultWorkingDirKey, "Chats")
				fmt.Printf("[CHAT FOLDER GUARD WRAPPER] Injected FolderGuardAllowedWriteFolderKey=%v ReadPaths=%v for %s\n", shellAllowedFolders, chatReadFolders, toolNameCopy)
			}

			// For WRITE tools, ONLY allow writes to allowed folders
			if writeTools[toolNameCopy] {
				for _, paramName := range writePathParams {
					if paramValue, exists := args[paramName]; exists {
						if pathStr, ok := paramValue.(string); ok && pathStr != "" {
							cleanedPath := filepath.Clean(pathStr)

							if !isPathAllowed(cleanedPath) {
								log.Printf("[CHAT MODE FOLDER GUARD] Blocked WRITE to '%s' (cleaned: '%s') for tool %s - allowed folders: %v", pathStr, cleanedPath, toolNameCopy, allowedWriteFolders)
								return "", fmt.Errorf("access denied: cannot write to '%s' (allowed write folders: %v)", pathStr, allowedWriteFolders)
							}
						}
					}
				}
			}

			// Call original executor
			return originalExecutor(ctx, args)
		}

		wrappedExecutors[toolNameCopy] = wrappedExecutor
	}

	log.Printf("[CHAT MODE FOLDER GUARD] Wrapped %d executors - protected folders: %v, allowed write folders: %v", len(wrappedExecutors), protectedFolders, allowedWriteFolders)
	return wrappedExecutors
}

// wrapExecutorsWithPlanFolderGuard wraps workspace tool executors to restrict writes to a specific plan folder.
// Like wrapExecutorsWithChatModeFolderGuard but uses the plan folder (e.g. "Plans/{planID}")
// instead of "Chats/" as the allowed write folder. This ensures sub-agents only write to their assigned plan folder.
func wrapExecutorsWithPlanFolderGuard(executors map[string]func(ctx context.Context, args map[string]interface{}) (string, error), planFolder string, additionalWriteFolders ...string) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	protectedFolders := []string{"_users"}

	// Use the plan folder as the allowed write folder (instead of Chats/)
	planFolderWithSlash := strings.TrimSuffix(planFolder, "/") + "/"
	allowedWriteFolders := []string{planFolderWithSlash}
	allowedWriteFolders = append(allowedWriteFolders, additionalWriteFolders...)

	shellAllowedFolders := make([]string, len(allowedWriteFolders))
	copy(shellAllowedFolders, allowedWriteFolders)

	writeTools := map[string]bool{
		"update_workspace_file":     true,
		"delete_workspace_file":     true,
		"move_workspace_file":       true,
		"diff_patch_workspace_file": true,
		"write_workspace_file":      true,
	}

	allPathParams := []string{"filepath", "source_filepath", "destination_filepath", "folder", "pattern"}
	writePathParams := []string{"filepath", "source_filepath", "destination_filepath"}

	isPathProtected := func(cleanedPath string) bool {
		pathLower := strings.ToLower(cleanedPath)
		for _, folder := range protectedFolders {
			folderLower := strings.ToLower(folder)
			if pathLower == folderLower || strings.HasPrefix(pathLower, folderLower+"/") {
				return true
			}
		}
		return false
	}

	isWriteAllowed := func(cleanedPath string) bool {
		pathLower := strings.ToLower(cleanedPath)
		for _, folder := range allowedWriteFolders {
			folderLower := strings.ToLower(folder)
			if strings.HasPrefix(pathLower, folderLower) || pathLower == strings.TrimSuffix(folderLower, "/") {
				return true
			}
		}
		return false
	}

	wrappedExecutors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	for toolName, executor := range executors {
		toolNameCopy := toolName
		originalExecutor := executor

		wrappedExecutor := func(ctx context.Context, args map[string]interface{}) (string, error) {
			if toolNameCopy == "execute_shell_command" {
				ctx = context.WithValue(ctx, common.FolderGuardAllowedWriteFolderKey, shellAllowedFolders)
				// Set mode-specific read paths so the shell isolator scopes reads
				// to the relevant folders instead of the entire workspace (".").
				// The write folder is always readable; add common shared folders
				// (skills, subagents) for plan mode so sub-agents can read resources.
				shellReadFolders := make([]string, 0, len(shellAllowedFolders)+2)
				shellReadFolders = append(shellReadFolders, shellAllowedFolders...)
				// For plan mode (planFolder starts with "Plans"), add shared resources.
				// For prototype mode (planFolder starts with "Projects/"), the project
				// folder is self-contained — no extra reads needed.
				if strings.HasPrefix(planFolder, "Plans") {
					shellReadFolders = append(shellReadFolders, "skills/", "subagents/", "Downloads/")
				}
				ctx = context.WithValue(ctx, common.FolderGuardReadPathsKey, shellReadFolders)
				// Inject the session-level default working directory so execute_shell_command
				// can substitute it when the LLM passes ".".
				// planFolder is "Projects/{name}" for prototype mode, "Plans" for plan mode.
				// execute_shell_command reads this via DefaultWorkingDirKey (execute_shell_command.go).
				defaultDir := strings.TrimSuffix(planFolder, "/")
				ctx = context.WithValue(ctx, common.DefaultWorkingDirKey, defaultDir)
			}

			if writeTools[toolNameCopy] {
				for _, paramName := range writePathParams {
					if pathValue, exists := args[paramName]; exists {
						if pathStr, ok := pathValue.(string); ok {
							cleanedPath := filepath.Clean(pathStr)
							if !isWriteAllowed(cleanedPath) {
								return "", fmt.Errorf("access denied: writes restricted to %s (got: %s)", planFolderWithSlash, cleanedPath)
							}
						}
					}
				}
			}

			for _, paramName := range allPathParams {
				if pathValue, exists := args[paramName]; exists {
					if pathStr, ok := pathValue.(string); ok {
						cleanedPath := filepath.Clean(pathStr)
						if isPathProtected(cleanedPath) {
							return "", fmt.Errorf("access denied: cannot access protected folder _users/")
						}
					}
				}
			}

			return originalExecutor(ctx, args)
		}

		wrappedExecutors[toolNameCopy] = wrappedExecutor
	}

	log.Printf("[PLAN FOLDER GUARD] Wrapped %d executors - writes restricted to: %v", len(wrappedExecutors), allowedWriteFolders)
	return wrappedExecutors
}
