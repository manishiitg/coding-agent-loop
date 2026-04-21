package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/common"
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
// Skips Chats/ (already handled) and root-level files (no '/' in path).
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
		if strings.HasPrefix(pLower, "chats") {
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

// extractWorkflowContextFolders normalizes workflow context paths selected via the #workflow picker
// so they can participate in folder guard setup just like @context paths.
// These paths come from trusted UI workflow selections, but we still clean/dedupe them and
// drop protected/invalid values before they reach the folder guard.
func extractWorkflowContextFolders(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var result []string

	for _, raw := range paths {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}

		p = filepath.Clean(p)
		if p == "." || p == string(filepath.Separator) || p == "/" || filepath.IsAbs(p) {
			continue
		}
		if strings.HasPrefix(p, ".."+string(filepath.Separator)) || p == ".." || strings.HasPrefix(p, "../") {
			continue
		}

		pLower := strings.ToLower(p)
		if strings.HasPrefix(pLower, "chats") {
			continue
		}

		if seen[p] {
			continue
		}
		seen[p] = true
		result = append(result, p)
	}

	return result
}

// collectAdditionalFolderGuardFolders merges extra folder guard paths from @file context
// and #workflow context, preserving order and removing duplicates.
// DEPRECATED: Use collectSplitFolderGuardFolders instead which separates write vs read-only paths.
func collectAdditionalFolderGuardFolders(query string, workflowContextPaths []string) []string {
	combined := append([]string{}, extractFileContextWriteFolders(query)...)
	combined = append(combined, extractWorkflowContextFolders(workflowContextPaths)...)
	return common.DeduplicateStrings(combined)
}

// collectSplitFolderGuardFolders returns separate write and read-only folder lists.
// @file context paths get write access; #workflow context paths get read-only access.
func collectSplitFolderGuardFolders(query string, workflowContextPaths []string) (writeFolders, readOnlyFolders []string) {
	writeFolders = common.DeduplicateStrings(extractFileContextWriteFolders(query))
	readOnlyFolders = common.DeduplicateStrings(extractWorkflowContextFolders(workflowContextPaths))
	return
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

// createCustomTools creates workspace and human tools for orchestrator/workflow agents
// workflowMode: if true, includes advanced + human + todo tools for workflow mode
//
//	if false, only workspace_advanced tools for chat mode (shell, image, web fetch, PDF)
//
// Returns: tools, executors, and a map of tool names to their categories
// Tools from CreateWorkspaceAdvancedTools() get category "workspace_advanced"
// All tools from CreateHumanTools() get category "human_tools"
//
// Note: workspace_basic and workspace_git are internal/deprecated and are not
// exposed to LLMs as workspace tool categories.
func createCustomTools(workflowMode bool, sessionInfo ...string) ([]llmtypes.Tool, map[string]interface{}, map[string]string) {
	// sessionInfo: optional [userID, sessionID] for session-aware workspace executors
	var userID, sessionID string
	if len(sessionInfo) >= 2 {
		userID = sessionInfo[0]
		sessionID = sessionInfo[1]
	}

	var allTools []llmtypes.Tool
	allExecutors := make(map[string]interface{})
	toolCategories := make(map[string]string)

	// Create workspace advanced tools (always included)
	workspaceAdvancedCategory := virtualtools.GetWorkspaceAdvancedToolCategory()
	workspaceAdvancedTools := virtualtools.CreateWorkspaceAdvancedTools()
	workspaceImageCategory := virtualtools.GetWorkspaceImageToolCategory()
	workspaceImageTools := virtualtools.CreateWorkspaceImageTools()
	workspaceVideoTools := virtualtools.CreateWorkspaceVideoTools()

	// Use session-aware executors when session info is provided
	var workspaceAdvancedExecutors map[string]func(ctx context.Context, args map[string]any) (string, error)
	if sessionID != "" {
		workspaceAdvancedExecutors, _ = virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSession(userID, sessionID)
	} else {
		workspaceAdvancedExecutors = virtualtools.CreateWorkspaceAdvancedToolExecutors()
	}
	// Add advanced tools
	allTools = append(allTools, workspaceAdvancedTools...)
	allTools = append(allTools, workspaceImageTools...)
	allTools = append(allTools, workspaceVideoTools...)
	for name, executor := range workspaceAdvancedExecutors {
		allExecutors[name] = executor
	}
	virtualtools.MergeImageToolExecutorsUntyped(virtualtools.ImageGenExecutorConfig{
		WorkspaceAPIURL: getWorkspaceAPIURL(),
		UserID:          userID,
	}, allExecutors, nil)
	virtualtools.MergeVideoToolExecutorsUntyped(virtualtools.VideoGenExecutorConfig{
		WorkspaceAPIURL: getWorkspaceAPIURL(),
		UserID:          userID,
	}, allExecutors, nil)

	// Advanced tools get workspace_advanced category
	for _, tool := range workspaceAdvancedTools {
		if tool.Function != nil {
			toolCategories[tool.Function.Name] = workspaceAdvancedCategory
		}
	}
	for _, tool := range workspaceImageTools {
		if tool.Function != nil {
			toolCategories[tool.Function.Name] = workspaceImageCategory
		}
	}
	for _, tool := range workspaceVideoTools {
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

		// Note: Todo tools (create_todo, complete_todo, etc.) have been removed.
		// The todo task orchestrator manages tasks directly via shell commands.

		// Note: Browser tools are NOT added unconditionally here.
		// They are added conditionally based on preset.EnableBrowserAccess in workflow initialization.
		// See the workflow initialization section where browser tools are added if enabled.
	}

	return allTools, allExecutors, toolCategories
}

// enhanceToolDescriptionForChatMode enhances a tool description with chat-mode-specific directory access information.
// chatsFolder is the full per-user path (e.g. "_users/default/Chats").
func enhanceToolDescriptionForChatMode(toolName, originalDescription, chatsFolder string) string {
	specialTools := map[string]bool{
		"sync_workspace_to_github":    true,
		"get_workspace_github_status": true,
	}
	if specialTools[toolName] {
		return originalDescription
	}

	writeTools := map[string]bool{
		"diff_patch_workspace_file": true,
		"execute_shell_command":     true,
	}

	var accessInfo strings.Builder
	accessInfo.WriteString("\n\n📁 **DIRECTORY ACCESS RESTRICTIONS (CHAT MODE):**")

	if writeTools[toolName] {
		accessInfo.WriteString(fmt.Sprintf("\n\n⚠️ **IMPORTANT:** You can ONLY write/modify files in '%s/'. All other folders are read-only.", chatsFolder))
		accessInfo.WriteString(fmt.Sprintf("\nExample: '%s/output.txt', '%s/data.json'.", chatsFolder, chatsFolder))
	} else {
		accessInfo.WriteString(fmt.Sprintf("\n\nYou have READ access to all workspace folders (Workflow/, skills/, etc.), but you can only WRITE to '%s/'.", chatsFolder))
	}

	return originalDescription + accessInfo.String()
}

// enhanceToolDescriptionForMultiAgentMode augments workspace tool descriptions for multi-agent plan mode.
// chatsFolder is the full per-user path (e.g. "_users/default/Chats").
func enhanceToolDescriptionForMultiAgentMode(toolName, originalDescription, chatsFolder string) string {
	specialTools := map[string]bool{
		"sync_workspace_to_github":    true,
		"get_workspace_github_status": true,
	}
	if specialTools[toolName] {
		return originalDescription
	}

	writeTools := map[string]bool{
		"diff_patch_workspace_file": true,
		"execute_shell_command":     true,
	}

	var accessInfo strings.Builder
	accessInfo.WriteString("\n\n📁 **DIRECTORY ACCESS RESTRICTIONS (MULTI-AGENT MODE):**")

	if writeTools[toolName] {
		accessInfo.WriteString(fmt.Sprintf("\n\n⚠️ **IMPORTANT:** You can write to '%s/' (primary). All other folders are read-only unless explicitly allowed.", chatsFolder))
		accessInfo.WriteString(fmt.Sprintf("\nSave plan outputs inside the plan folder (e.g. '%s/{plan_id}/output.txt').", chatsFolder))
	} else {
		accessInfo.WriteString(fmt.Sprintf("\n\nYou have READ access to all workspace folders. WRITE access is restricted to '%s/' and any explicitly allowed subfolders.", chatsFolder))
	}

	return originalDescription + accessInfo.String()
}

// wrapExecutorsWithChatModeFolderGuard wraps workspace tool executors to restrict chat mode writes.
// The default writable folder is Downloads/ only — the per-user Chats folder is supplied by callers
// via additionalWriteFolders so each session writes only to its own _users/<id>/Chats/ subtree.
// Pass additionalWriteFolders to allow extra folders (e.g. "_users/<id>/Chats/", "skills/custom/").
// Pass blockedWriteFolders to deny writes to specific paths within otherwise-allowed prefixes —
// used by the chat-agent #workflow path to grant `Workflow/<name>/` as a broad write prefix
// while still denying `Workflow/<name>/planning/` (planning files must go through typed
// plan-mod tools, never raw writes). Reads remain allowed on blockedWriteFolders — agents
// still need to inspect plan.json / step_config.json.
// This creates a wrapper that:
// 1. ALLOWS read access to all folders (skills/, Workflow/, Downloads/, etc.)
// 2. ONLY ALLOWS write access to Downloads/ + any additionalWriteFolders the caller passed
// 3. DENIES writes to any blockedWriteFolders prefix even when it's under an allowed write prefix
// 4. Restricts shell writes to allowed folders
func wrapExecutorsWithChatModeFolderGuard(executors map[string]func(ctx context.Context, args map[string]interface{}) (string, error), readOnlyFolders []string, blockedWriteFolders []string, additionalWriteFolders ...string) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	// No protected folders — all users share the same filesystem
	protectedFolders := []string{}

	// blockedWritePrefixes denies writes (tool path params + shell write patterns) even when
	// the path is under an allowed write prefix. Used by chat-agent #workflow to block raw
	// writes to Workflow/<name>/planning/ while keeping the rest of the workflow writable.
	blockedWritePrefixes := make([]string, 0, len(blockedWriteFolders))
	for _, f := range blockedWriteFolders {
		cleaned := filepath.Clean(f)
		if cleaned != "" && cleaned != "." {
			blockedWritePrefixes = append(blockedWritePrefixes, cleaned)
		}
	}

	// Default writable: Downloads/ only. The per-user Chats folder must come from the caller
	// via additionalWriteFolders — this prevents accidental writes to the legacy global Chats/.
	allowedWriteFolders := []string{"Downloads/"}
	allowedWriteFolders = append(allowedWriteFolders, additionalWriteFolders...)

	// For shell sandboxing, pass all allowed write folders
	shellAllowedFolders := make([]string, len(allowedWriteFolders))
	copy(shellAllowedFolders, allowedWriteFolders)

	// Check if any allowed write folder OR read-only folder grants Workflow/ access (case-insensitive)
	hasWorkflowAccess := false
	allAccessFolders := append(append([]string{}, allowedWriteFolders...), readOnlyFolders...)
	for _, f := range allAccessFolders {
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

	// Helper: check if a cleaned path is under any blocked-write prefix. Reads remain
	// allowed; this is the exception-list that pairs with broad write prefixes like
	// Workflow/<name>/ so planning/ stays non-writable even inside the workflow folder.
	isPathBlockedWrite := func(cleanedPath string) bool {
		for _, prefix := range blockedWritePrefixes {
			if cleanedPath == prefix ||
				strings.HasPrefix(cleanedPath, prefix+"/") ||
				strings.HasPrefix(cleanedPath, prefix+"\\") {
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
							return "", fmt.Errorf("access denied: '%s' is a protected system folder", pathStr)
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
				// Set chat-mode read paths: shared resources + the per-user folders the caller already
				// passed in shellAllowedFolders (typically _users/<id>/Chats/ and _users/<id>/memories/).
				// The legacy global "Chats/" is NOT in defaults — per-user paths come from shellAllowedFolders.
				chatReadFolders := []string{"Downloads/", "skills/", "subagents/", "Workflow/", "config/"}
				chatReadFolders = append(chatReadFolders, shellAllowedFolders...)
				chatReadFolders = append(chatReadFolders, readOnlyFolders...)
				ctx = context.WithValue(ctx, common.FolderGuardReadPathsKey, chatReadFolders)
				// Default working directory for chat mode — workspace root
				ctx = context.WithValue(ctx, common.DefaultWorkingDirKey, "")
				fmt.Printf("[CHAT FOLDER GUARD WRAPPER] Injected FolderGuardAllowedWriteFolderKey=%v ReadPaths=%v for %s\n", shellAllowedFolders, chatReadFolders, toolNameCopy)
			}

			// For WRITE tools (diff_patch_workspace_file primarily), check blocked-write
			// prefixes first and then allowed-write prefixes. Shell commands are handled
			// via the isolator's BlockedPaths (kernel-level enforcement, set up at
			// SetSessionFolderGuard call site) — no string-scanning needed here.
			if writeTools[toolNameCopy] {
				for _, paramName := range writePathParams {
					if paramValue, exists := args[paramName]; exists {
						if pathStr, ok := paramValue.(string); ok && pathStr != "" {
							cleanedPath := filepath.Clean(pathStr)

							if isPathBlockedWrite(cleanedPath) {
								log.Printf("[CHAT MODE FOLDER GUARD] Blocked WRITE to '%s' (cleaned: '%s') for tool %s — path is under a blocked-write prefix (%v)", pathStr, cleanedPath, toolNameCopy, blockedWritePrefixes)
								return "", fmt.Errorf("access denied: '%s' is under a blocked-write prefix (%v) — this folder is read-only even though its parent is writable", pathStr, blockedWritePrefixes)
							}

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
// Like wrapExecutorsWithChatModeFolderGuard but uses the plan folder (e.g. "Chats/{planID}")
// instead of the whole "Chats/" tree as the allowed write folder. This ensures sub-agents only write to their assigned plan folder.
func wrapExecutorsWithPlanFolderGuard(executors map[string]func(ctx context.Context, args map[string]interface{}) (string, error), planFolder string, readOnlyFolders []string, additionalWriteFolders ...string) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	protectedFolders := []string{}

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
				shellReadFolders := make([]string, 0, len(shellAllowedFolders)+2+len(readOnlyFolders))
				shellReadFolders = append(shellReadFolders, shellAllowedFolders...)
				shellReadFolders = append(shellReadFolders, readOnlyFolders...)
				// For chat-backed plan mode (planFolder starts with "Chats"), add shared resources.
				// For prototype mode (planFolder starts with "Projects/"), the project
				// folder is self-contained — no extra reads needed.
				if strings.HasPrefix(planFolder, "Chats") {
					shellReadFolders = append(shellReadFolders, "skills/", "subagents/", "Downloads/")
				}
				ctx = context.WithValue(ctx, common.FolderGuardReadPathsKey, shellReadFolders)
				// Inject the session-level default working directory so execute_shell_command
				// can substitute it when the LLM passes ".".
				// planFolder is "Projects/{name}" for prototype mode, "Chats/{name}" for plan mode.
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
							return "", fmt.Errorf("access denied: cannot access protected folder")
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

// loadWorkflowMemory reads all .md files from the memory/ folder in the workspace.
// Falls back to legacy instructions.md if memory/ folder doesn't exist.
// Returns concatenated content or empty string.
func loadWorkflowMemory(workspacePath string, readFile func(context.Context, string) (string, error), ctx context.Context) string {
	// Try reading memory/ folder via shell to list files
	// Since we only have readFile, try reading a few common patterns
	// First try legacy instructions.md as a simple fallback
	var parts []string

	// Try memory/ folder — read individual files by listing via the workspace
	// We'll use the readFile function to read memory/memory.md (the index/main file)
	memoryPath := workspacePath + "/memory"

	// Try reading the main memory file
	if content, err := readFile(ctx, memoryPath+"/memory.md"); err == nil && content != "" {
		parts = append(parts, strings.TrimSpace(content))
	}

	// If no memory/ files found, fall back to legacy instructions.md
	if len(parts) == 0 {
		if content, err := readFile(ctx, workspacePath+"/instructions.md"); err == nil && content != "" {
			return strings.TrimSpace(content)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// extractStepSummary parses plan JSON and returns a compact step summary string.
// Format: "1. step-id [type] - Title\n2. ..."
// For todo_task steps, also lists sub-agent routes indented.
func extractStepSummary(planJSON string) string {
	var plan struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return ""
	}
	if len(plan.Steps) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, raw := range plan.Steps {
		var step struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Title string `json:"title"`
			// For todo_task steps
			PredefinedRoutes []struct {
				RouteID      string `json:"route_id"`
				SubAgentStep struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"sub_agent_step"`
			} `json:"predefined_routes,omitempty"`
			// For decision/conditional steps
			IfTrueSteps  []json.RawMessage `json:"if_true_steps,omitempty"`
			IfFalseSteps []json.RawMessage `json:"if_false_steps,omitempty"`
		}
		if err := json.Unmarshal(raw, &step); err != nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("%d. `%s` [%s] — %s\n", i+1, step.ID, step.Type, step.Title))

		// Show sub-agents for todo_task steps
		for _, route := range step.PredefinedRoutes {
			if route.SubAgentStep.ID != "" {
				sb.WriteString(fmt.Sprintf("   ↳ `%s` — %s\n", route.SubAgentStep.ID, route.SubAgentStep.Title))
			}
		}
	}
	return sb.String()
}
