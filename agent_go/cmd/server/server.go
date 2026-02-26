package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof" // Register pprof handlers
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"mcp-agent-builder-go/agent_go/internal/events"
	agent "mcp-agent-builder-go/agent_go/pkg/agentwrapper"
	"mcp-agent-builder-go/agent_go/pkg/database"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	todo_creation_human "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	orchEvents "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	orchtypes "mcp-agent-builder-go/agent_go/pkg/orchestrator/types"

	unifiedevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/executor"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"mcp-agent-builder-go/agent_go/pkg/common"
	"mcp-agent-builder-go/agent_go/pkg/logger"
	"mcp-agent-builder-go/agent_go/pkg/skills"
	"mcp-agent-builder-go/agent_go/pkg/subagents"

	"github.com/joho/godotenv"

	eventbridge "mcp-agent-builder-go/agent_go/cmd/server/event_bridge"
	slackservice "mcp-agent-builder-go/agent_go/cmd/server/services"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
	"strconv"

	mcpagent "github.com/manishiitg/mcpagent/agent"
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

	// Build the list of allowed write folders from per-user folders
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
				fmt.Printf("[CHAT FOLDER GUARD WRAPPER] Injected FolderGuardAllowedWriteFolderKey=%v for %s\n", shellAllowedFolders, toolNameCopy)
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

// ServerCmd represents the server command
var ServerCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the streaming API server",
	Long: `Start the streaming API server that provides HTTP endpoints and Server-Sent Events (SSE) support 
for real-time agent streaming. This server enables frontend integration with the MCP agent.

The server provides:
- REST API endpoints for agent queries
- Server-Sent Events (SSE) for real-time streaming
- Polling API for event retrieval
- Multi-provider LLM support (Bedrock, OpenAI, Anthropic)
- Full observability and tracing

Examples:
  mcp-agent server                           # Start server with default settings
  mcp-agent server --port 8000              # Start on custom port
  mcp-agent server --provider openai        # Use OpenAI provider
  mcp-agent server --cors-origins "*"       # Enable CORS for all origins`,
	Run: runServer,
}

// Server configuration
type ServerConfig struct {
	Port          int      `json:"port"`
	Host          string   `json:"host"`
	CORSOrigins   []string `json:"cors_origins"`
	Provider      string   `json:"provider"`
	ModelID       string   `json:"model_id"`
	Temperature   float64  `json:"temperature"`
	MaxTurns      int      `json:"max_turns"`
	MCPConfigPath string   `json:"mcp_config_path"`
}

// ActiveSessionInfo represents an active session for page refresh recovery
type ActiveSessionInfo struct {
	SessionID    string    `json:"session_id"`
	AgentMode    string    `json:"agent_mode"`
	Status       string    `json:"status"` // "running", "paused", "completed"
	LastActivity time.Time `json:"last_activity"`
	CreatedAt    time.Time `json:"created_at"`
	Query        string    `json:"query,omitempty"`
	LLMGuidance  string    `json:"llm_guidance,omitempty"` // LLM guidance message for this session
	UserID       string    `json:"-"`                      // User ID for session isolation (not exposed in JSON)
}

// StreamingAPI represents the streaming API server
type StreamingAPI struct {
	config ServerConfig

	// Note: Removed session management - fresh agents created per request

	// Agent cancel functions for proper context cancellation: sessionID -> context.CancelFunc
	agentCancelFuncs map[string]context.CancelFunc
	agentCancelMux   sync.RWMutex

	// Workflow orchestrator sessions: sessionID -> orchestrator.Orchestrator

	// Workflow orchestrator contexts for cancellation: queryID -> context.CancelFunc
	// Using queryID (not sessionID) ensures each execution is independent
	workflowOrchestratorContexts   map[string]context.CancelFunc
	workflowOrchestratorContextMux sync.RWMutex

	// Mapping of sessionID -> []queryID to track which executions belong to which session
	// Used by handleStopSession to cancel all executions for a session
	sessionQueryIDs   map[string][]string
	sessionQueryIDMux sync.RWMutex

	// Workflow objectives: sessionID -> objective
	workflowObjectives   map[string]string
	workflowObjectiveMux sync.RWMutex

	// Workflow step IDs: presetQueryID -> stepID (temporary storage for step-specific phase execution)
	workflowStepIDs   map[string]string
	workflowStepIDMux sync.RWMutex

	// Conversation history storage: sessionID -> conversation history
	conversationHistory map[string][]llmtypes.MessageContent
	conversationMux     sync.RWMutex

	// Database for chat history storage
	chatDB database.Database

	// Polling system components
	eventStore *events.EventStore

	// Workflow orchestrator configuration
	provider      string
	model         string
	mcpConfigPath string
	temperature   float64
	workspaceRoot string

	// Active session tracking for page refresh recovery
	activeSessions    map[string]*ActiveSessionInfo
	activeSessionsMux sync.RWMutex

	// Per-session plan phase tracking for multi-agent mode
	planSessionStates    map[string]*virtualtools.PlanSessionState
	planSessionStatesMux sync.RWMutex

	// Session reactivation lock: prevents race conditions when calculating baseIndex
	// and initializing the event store for reactivated sessions
	sessionReactivationMux sync.Mutex

	// Orchestrator objects in memory for guidance injection
	workflowOrchestrators    map[string]orchestrator.Orchestrator
	workflowOrchestratorsMux sync.RWMutex

	// Background agent registry for async delegation in multi-agent mode
	bgAgentRegistry *BackgroundAgentRegistry

	// Session busy tracking — prevents synthetic turns from overlapping with user turns
	sessionBusy   map[string]bool
	sessionBusyMu sync.RWMutex

	// Pending completions queue — background agent IDs that finished while session was busy
	pendingCompletions map[string][]string
	pendingMu          sync.RWMutex

	// Last query request per session — used to construct synthetic turns
	lastQueryRequests map[string]QueryRequest
	lastQueryMu       sync.RWMutex

	// Stored agent instances for synthetic turns (plan mode only)
	// Reused directly via StreamWithEvents() instead of re-creating agents per synthetic turn
	sessionAgents    map[string]*agent.LLMAgentWrapper
	sessionAgentsMux sync.RWMutex

	// Claude Code CLI session IDs for --resume (our sessionID -> CLI session_id)
	claudeCodeSessionIDs map[string]string

	// Gemini CLI session IDs for --resume (our sessionID -> CLI session_id)
	geminiSessionIDs map[string]string

	// Background completion loop tracking — prevents multiple loops per session
	completionLoopStarted   map[string]bool
	completionLoopStartedMu sync.Mutex

	toolStatus    map[string]ToolStatus
	enabledTools  map[string][]string // queryID/sessionID -> enabled tool names
	toolStatusMux sync.RWMutex
	mcpConfig     *mcpclient.MCPConfig

	// Background tool discovery
	discoveryRunning       bool
	discoveryMux           sync.RWMutex
	lastDiscovery          time.Time
	discoveryTicker        *time.Ticker
	discoveryFailedServers map[string]string // serverName -> error reason (skipped on subsequent discovery cycles)

	// Per-server install/connection logs
	serverLogs    map[string][]ServerLogEntry
	serverLogsMux sync.RWMutex

	// Logger for structured logging
	logger loggerv2.Logger

	// Bot conversation manager for Slack/Discord/Telegram bot sessions
	botManager *slackservice.BotConversationManager

	// Web simulator connector for testing bot flow without Slack
	webSimulator *slackservice.WebSimulatorConnector

	// API token for bearer auth on per-tool endpoints (code execution mode)
	apiToken string
}

// QueryRequest represents an agent query request
type QueryRequest struct {
	Query          string                  `json:"query"`
	Message        string                  `json:"message,omitempty"` // Alias for Query (used by frontend)
	Servers        []string                `json:"servers,omitempty"`
	EnabledServers []string                `json:"enabled_servers,omitempty"`
	SelectedTools  []string                `json:"selected_tools,omitempty"` // Array of "server:tool" strings
	Provider       string                  `json:"provider,omitempty"`
	ModelID        string                  `json:"model_id,omitempty"`
	Temperature    float64                 `json:"temperature,omitempty"`
	MaxTurns       int                     `json:"max_turns,omitempty"`
	AgentMode      string                  `json:"agent_mode,omitempty"`
	LLMConfig      *orchestrator.LLMConfig `json:"llm_config,omitempty"`
	PresetQueryID  string                  `json:"preset_query_id,omitempty"`
	LLMGuidance    string                  `json:"llm_guidance,omitempty"` // LLM guidance message
	// Code execution mode: When enabled, only virtual tools are added to LLM
	// MCP tools are accessed via generated Go code using discover_code_files and write_code
	UseCodeExecutionMode bool `json:"use_code_execution_mode,omitempty"`
	// Tool search mode: When enabled, LLM discovers tools on-demand via search_tools
	UseToolSearchMode bool `json:"use_tool_search_mode,omitempty"`
	// Execution options from frontend (for workflow execution phase)
	ExecutionOptions *ExecutionOptions `json:"execution_options,omitempty"`
	// Context summarization configuration
	EnableContextSummarization     *bool   `json:"enable_context_summarization,omitempty"`       // Enable context summarization feature (nil = inherit default, true/false = explicit override)
	SummarizeOnTokenThreshold      *bool   `json:"summarize_on_token_threshold,omitempty"`       // Enable token-based summarization trigger (nil = inherit default, true/false = explicit override)
	TokenThresholdPercent          float64 `json:"token_threshold_percent,omitempty"`            // Percentage of context window to trigger summarization (0.0-1.0, default: 0.8 = 80%)
	SummarizeOnFixedTokenThreshold *bool   `json:"summarize_on_fixed_token_threshold,omitempty"` // Enable fixed token-based summarization trigger (nil = inherit default, true/false = explicit override)
	FixedTokenThreshold            int     `json:"fixed_token_threshold,omitempty"`              // Fixed token threshold to trigger summarization (default: 200000 = 200k tokens, matches orchestrator)
	SummaryKeepLastMessages        int     `json:"summary_keep_last_messages,omitempty"`         // Number of recent messages to keep when summarizing (default: 4, matches orchestrator)
	// Context editing configuration
	EnableContextEditing        *bool `json:"enable_context_editing,omitempty"`         // Enable context editing (nil = inherit default, true/false = explicit override)
	ContextEditingThreshold     int   `json:"context_editing_threshold,omitempty"`      // Token threshold for context editing (0 = use default: 100)
	ContextEditingTurnThreshold int   `json:"context_editing_turn_threshold,omitempty"` // Turn age threshold for context editing (0 = use default: 5)
	// Workspace access configuration
	EnableWorkspaceAccess *bool `json:"enable_workspace_access,omitempty"` // Enable/disable workspace file access tools (nil = inherit default, true/false = explicit override)
	// Browser automation access configuration
	EnableBrowserAccess *bool `json:"enable_browser_access,omitempty"` // Enable/disable browser automation tool (nil = inherit default, true/false = explicit override)
	// CDP port for connecting to an existing Chrome browser (local mode only)
	CdpPort *int `json:"cdp_port,omitempty"` // When set and > 0, connect to Chrome via CDP on this port instead of launching headless
	// Selected skills to include in chat context
	SelectedSkills []string `json:"selected_skills,omitempty"` // Array of skill folder names
	// Selected sub-agent templates to make available for delegation
	SelectedSubAgents []string `json:"selected_subagents,omitempty"` // Array of sub-agent template folder names
	// Delegation mode: 'spawn' = simple delegate only, 'plan' = plan-driven + delegate, '' = disabled
	DelegationMode string `json:"delegation_mode,omitempty"`
	// Plan phase override: 'planning' = plan first (default), 'execution' = skip planning and execute directly
	PlanPhase string `json:"plan_phase,omitempty"`
	// Delegation tier configuration: Maps reasoning levels (high/medium/low) to specific provider/model pairs
	DelegationTierConfig *virtualtools.DelegationTierConfig `json:"delegation_tier_config,omitempty"`
	// Decrypted secrets to inject into agent system prompt
	DecryptedSecrets []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"decrypted_secrets,omitempty"`
	// Selected global secret names to include (if nil/absent, all global secrets are included)
	SelectedGlobalSecrets *[]string `json:"selected_global_secrets,omitempty"`
	// Workspace paths of workflows to inject context for (via # selector in chat)
	WorkflowContextPaths []string `json:"workflow_context_paths,omitempty"`

	// Internal: user ID for synthetic turn reconstruction (not from JSON)
	userID string `json:"-"`
}

// getCdpPort returns the CDP port from a QueryRequest, or 0 if not set
func getCdpPort(req QueryRequest) int {
	if req.CdpPort != nil {
		return *req.CdpPort
	}
	return 0
}

// CrossProviderFallback represents cross-provider fallback configuration
type CrossProviderFallback struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

// QueryResponse represents an agent query response
type QueryResponse struct {
	QueryID   string `json:"query_id"`
	SessionID string `json:"session_id"` // The actual session ID used for conversation history
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

// LLMGuidanceRequest represents a request to set LLM guidance for a session
type LLMGuidanceRequest struct {
	SessionID string `json:"session_id"`
	Guidance  string `json:"guidance"`
}

// LLMGuidanceResponse represents the response for LLM guidance operations
type LLMGuidanceResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Guidance  string `json:"guidance,omitempty"`
}

// HumanFeedbackRequest represents a request to submit human feedback
type HumanFeedbackRequest struct {
	UniqueID string `json:"unique_id"`
	Response string `json:"response"`
}

// HumanFeedbackResponse represents the response for human feedback operations
type HumanFeedbackResponse struct {
	UniqueID string `json:"unique_id"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

// --- TOOL MANAGEMENT API ---

func init() {
	// Add server command flags
	ServerCmd.Flags().IntP("port", "p", 8000, "Server port")
	ServerCmd.Flags().StringP("host", "H", "0.0.0.0", "Server host")
	ServerCmd.Flags().StringSlice("cors-origins", []string{"*"}, "CORS allowed origins")
	ServerCmd.Flags().String("provider", "bedrock", "LLM provider (bedrock, openai, anthropic)")
	ServerCmd.Flags().String("model", "", "Model ID (uses provider default if empty)")
	ServerCmd.Flags().Float64("temperature", 0.0, "Temperature for LLM")
	ServerCmd.Flags().Int("max-turns", 100, "Maximum conversation turns")
	ServerCmd.Flags().String("mcp-config", "configs/mcp_servers_clean.json", "MCP servers configuration path")

	// Chat History Database flags
	ServerCmd.Flags().String("db-path", "/app/chat_history.db", "SQLite database path for chat history")
	ServerCmd.Flags().String("db-type", "sqlite", "Database type (sqlite, postgres)")

	// Bind flags to viper
	viper.BindPFlags(ServerCmd.Flags())
}

func runServer(cmd *cobra.Command, args []string) {
	// Load configuration
	config := ServerConfig{
		Port:          viper.GetInt("port"),
		Host:          viper.GetString("host"),
		CORSOrigins:   viper.GetStringSlice("cors-origins"),
		Provider:      viper.GetString("provider"),
		ModelID:       viper.GetString("model"),
		Temperature:   viper.GetFloat64("temperature"),
		MaxTurns:      viper.GetInt("max-turns"),
		MCPConfigPath: viper.GetString("mcp-config"),
	}

	log.Printf("[SERVER DEBUG] Using MCP config file: %s", config.MCPConfigPath)

	// Load .env file for environment variables (OPENAI_API_KEY, etc.)
	// Only load if not already loaded
	if os.Getenv("MCP_ENV_LOADED") == "" {
		if err := godotenv.Load(); err == nil {
			os.Setenv("MCP_ENV_LOADED", "1")
			fmt.Println("[ENV] Loaded .env file for LLM config")
		}
	}

	// Show execution agent LLM config at startup
	agentProvider := os.Getenv("AGENT_PROVIDER")
	if agentProvider == "" {
		agentProvider = "bedrock" // fallback default
	}
	agentModel := os.Getenv("AGENT_MODEL")
	if agentModel == "" {
		agentModel = os.Getenv("BEDROCK_PRIMARY_MODEL") // Use .env configuration
	}
	fmt.Printf("\U0001F916 Agent:   %s | Model: %s\n", agentProvider, agentModel)

	// Apply environment overrides to config (ensure Terraform env vars take precedence)
	if val := os.Getenv("AGENT_PROVIDER"); val != "" {
		config.Provider = val
	}
	// Also apply model override if set (and not just falling back to defaults)
	if agentModel != "" && (os.Getenv("AGENT_MODEL") != "" || os.Getenv("BEDROCK_PRIMARY_MODEL") != "") {
		config.ModelID = agentModel
	}
	// Show cross-provider fallback configuration
	bedrockOpenAIFallback := os.Getenv("BEDROCK_OPENAI_FALLBACK_MODELS")
	if bedrockOpenAIFallback != "" {
		fmt.Printf("🔄 Cross-Provider Fallback: Bedrock → OpenAI (%s)\n", bedrockOpenAIFallback)
	} else {
		fmt.Printf("⚠️  Cross-Provider Fallback: Not configured (set BEDROCK_OPENAI_FALLBACK_MODELS)\n")
	}

	// Validate provider
	llmProvider, err := llm.ValidateProvider(config.Provider)
	if err != nil {
		log.Fatalf("Invalid provider: %v", err)
	}

	// Set default model if not specified
	if config.ModelID == "" {
		config.ModelID = llm.GetDefaultModel(llmProvider)
	}

	// In multi-user mode, AUTH_SECRET must be explicitly set for secure JWT signing.
	// In single-user mode, auth is bypassed entirely so AUTH_SECRET is not needed.
	if IsMultiUserMode() && os.Getenv("AUTH_SECRET") == "" {
		log.Fatal("[AUTH] FATAL: AUTH_SECRET env var must be set when MULTI_USER_MODE=true. Generate a random secret and add it to your deployment configuration.")
	}

	fmt.Printf("🚀 Starting Streaming API Server\n")
	fmt.Printf("📡 Host: %s:%d\n", config.Host, config.Port)
	fmt.Printf("🤖 Primary Provider: %s | Model: %s\n", config.Provider, config.ModelID)
	// Show tracing configuration
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}
	fmt.Printf("📊 Tracing: %s\n", tracingProvider)

	fmt.Printf("🌐 CORS Origins: %v\n", config.CORSOrigins)
	fmt.Printf("🔒 LLM Config Locked: %v (Env: %s)\n", isGlobalLLMConfigLocked(), os.Getenv("LLM_CONFIG_LOCKED"))
	fmt.Printf("📋 Supported Providers: %s\n", os.Getenv("SUPPORTED_LLM_PROVIDERS"))
	fmt.Printf("📁 Config: %s\n", config.MCPConfigPath)

	// Create streaming API server
	configPath := config.MCPConfigPath

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Printf("⚠️  MCP config file not found at %s, initializing empty config...", configPath)

		// Ensure directory exists
		configDir := filepath.Dir(configPath)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			log.Fatalf("Failed to create config directory: %v", err)
		}

		// Create empty config file
		emptyConfig := &mcpclient.MCPConfig{MCPServers: make(map[string]mcpclient.MCPServerConfig)}
		if err := mcpclient.SaveConfig(configPath, emptyConfig); err != nil {
			log.Fatalf("Failed to create empty MCP config: %v", err)
		}
		log.Printf("✅ Created empty MCP config at %s", configPath)
	}

	mcpConfig, err := mcpclient.LoadConfig(configPath, nil) // Logger not yet available, will be created later
	if err != nil {
		log.Fatalf("Failed to load MCP config: %v", err)
	}

	// Initialize polling system (activity callback will be set after api is created)
	eventStore := events.NewEventStore(10000) // Max 10000 events per session

	// Initialize chat history database
	dbType := viper.GetString("db-type")
	if dbType == "" {
		dbType = "sqlite"
	}

	var chatDB database.Database
	var connInfo string

	if dbType == "postgres" {
		connStr := os.Getenv("DATABASE_URL")
		if connStr == "" {
			log.Fatalf("DATABASE_URL environment variable is required for postgres")
		}
		chatDB, err = database.NewSupabaseDB(connStr)
		connInfo = "PostgreSQL (Supabase)"
	} else {
		dbPath := os.Getenv("DB_PATH")
		if dbPath == "" {
			dbPath = viper.GetString("db-path")
		}
		if dbPath == "" {
			dbPath = "/app/chat_history.db" // Default SQLite database path
		}
		chatDB, err = database.NewSQLiteDB(dbPath)
		connInfo = fmt.Sprintf("SQLite (%s)", dbPath)
	}

	if err != nil {
		log.Printf("⚠️  Failed to initialize chat history database: %v", err)
		if dbType == "sqlite" {
			// SQLite may fail on network storage (Azure Files/SMB) which doesn't support
			// POSIX file locking. Fall back to local ephemeral storage.
			fallbackPath := "/tmp/chat_history.db"
			log.Printf("⚠️  Retrying with local ephemeral storage: %s", fallbackPath)
			chatDB, err = database.NewSQLiteDB(fallbackPath)
			if err != nil {
				log.Fatalf("Failed to initialize chat history database after fallback: %v", err)
			}
			connInfo = fmt.Sprintf("SQLite (%s) [fallback from network storage]", fallbackPath)
		} else {
			log.Fatalf("Failed to initialize chat history database: %v", err)
		}
	}
	defer chatDB.Close()

	fmt.Printf("💾 Chat History Database: %s\n", connInfo)

	// Initialize Slack service for human feedback
	slackSvc, err := slackservice.InitSlackService(chatDB.GetDB())
	if err != nil {
		log.Printf("⚠️  Failed to initialize Slack service: %v (Slack integration will be disabled)", err)
	} else {
		log.Printf("✅ Slack service initialized")
		// Set feedback store function for test connections only
		// Note: For receiving feedback, notification manager handles it
		slackservice.SetFeedbackStoreFuncs(
			func(uniqueID string, message string) error {
				store := virtualtools.GetHumanFeedbackStore()
				if store != nil {
					return store.CreateRequest(uniqueID, message)
				}
				return nil
			},
		)
		// Register Slack service with notification manager
		notificationManager := slackservice.GetNotificationManager()
		if notificationManager != nil && slackSvc != nil {
			notificationManager.RegisterConnector(slackSvc)
			// Set feedback store function so notification manager can update feedback store
			notificationManager.SetFeedbackResponseFunc(
				func(uniqueID string, response string) error {
					store := virtualtools.GetHumanFeedbackStore()
					if store != nil {
						return store.SubmitResponse(uniqueID, response)
					}
					return nil
				},
			)
			log.Printf("✅ Slack service registered with notification manager")
		}
	}

	api := &StreamingAPI{
		config:                       config,
		agentCancelFuncs:             make(map[string]context.CancelFunc),
		workflowOrchestratorContexts: make(map[string]context.CancelFunc),
		sessionQueryIDs:              make(map[string][]string),
		workflowObjectives:           make(map[string]string),
		conversationHistory:          make(map[string][]llmtypes.MessageContent),
		chatDB:                       chatDB,
		eventStore:                   eventStore,
		provider:                     config.Provider,
		model:                        config.ModelID,
		mcpConfigPath:                configPath,
		temperature:                  config.Temperature,
		workspaceRoot:                "./Tasks",
		toolStatus:                   make(map[string]ToolStatus),
		enabledTools:                 make(map[string][]string),
		mcpConfig:                    mcpConfig,
		serverLogs:                   make(map[string][]ServerLogEntry),
		logger:                       createServerLogger(),
		// Initialize background discovery fields
		discoveryRunning:       false,
		lastDiscovery:          time.Time{},
		discoveryTicker:        nil,
		discoveryFailedServers: make(map[string]string),
		// Initialize active session tracking
		activeSessions: make(map[string]*ActiveSessionInfo),
		// Initialize plan session state tracking for multi-agent mode
		planSessionStates: make(map[string]*virtualtools.PlanSessionState),
		// Initialize orchestrator storage
		workflowOrchestrators: make(map[string]orchestrator.Orchestrator),
		// Initialize workflow step ID storage
		workflowStepIDs: make(map[string]string),
		// Initialize background agent infrastructure
		bgAgentRegistry:       NewBackgroundAgentRegistry(),
		sessionBusy:           make(map[string]bool),
		pendingCompletions:    make(map[string][]string),
		lastQueryRequests:     make(map[string]QueryRequest),
		sessionAgents:         make(map[string]*agent.LLMAgentWrapper),
		completionLoopStarted: make(map[string]bool),
		claudeCodeSessionIDs:  make(map[string]string),
		geminiSessionIDs:      make(map[string]string),
	}

	// Generate API token for code execution mode per-tool endpoints
	api.apiToken = executor.GenerateAPIToken()

	// Set env vars for code execution mode (mcpagent reads these as fallback)
	// MCP_API_URL = Docker-reachable URL (for shell commands inside Docker + OpenAPI spec base URLs)
	// MCP_BRIDGE_API_URL = host-reachable URL (for mcpbridge binary running on the host)
	os.Setenv("MCP_API_URL", api.GetCodeExecAPIURL())
	os.Setenv("MCP_BRIDGE_API_URL", api.GetAPIURL())
	os.Setenv("MCP_API_TOKEN", api.apiToken)

	// Load global secrets from GLOBAL_SECRET_* environment variables
	loadGlobalSecrets()

	// Setup routes
	router := mux.NewRouter()

	// CORS middleware
	router.Use(api.corsMiddleware)

	// Auth middleware - applies to all API routes
	// Note: AuthMiddleware handles skipping auth for public endpoints (login, register, health, shared)
	router.Use(AuthMiddleware)

	// API routes
	apiRouter := router.PathPrefix("/api").Subrouter()

	// Authentication API routes (public - no auth required, handled by AuthMiddleware)
	apiRouter.HandleFunc("/auth/register", api.handleRegister).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/login", api.handleLogin).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/logout", api.handleLogout).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/me", api.handleGetCurrentUser).Methods("GET")
	apiRouter.HandleFunc("/auth/mode", api.handleGetAuthMode).Methods("GET")
	// Multi-provider OAuth routes
	apiRouter.HandleFunc("/auth/start", api.handleAuthStart).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/callback", api.handleAuthCallback).Methods("GET")

	// Session sharing routes
	apiRouter.HandleFunc("/sessions/{session_id}/share", api.handleCreateShare).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/sessions/{session_id}/shares", api.handleListShares).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/share/{share_id}", api.handleRevokeShare).Methods("DELETE", "OPTIONS")

	// Public shared session access (no auth required)
	apiRouter.HandleFunc("/shared/{share_token}", api.handleGetSharedSession).Methods("GET")
	apiRouter.HandleFunc("/query", api.handleQuery).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/chat", api.handleQuery).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/chat/stream", api.handleQuery).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/health", api.handleHealth).Methods("GET")
	apiRouter.HandleFunc("/capabilities", api.handleCapabilities).Methods("GET")
	apiRouter.HandleFunc("/cdp-check", api.handleCdpCheck).Methods("GET")
	apiRouter.HandleFunc("/downloads/chrome-cdp-macOS.zip", api.handleChromeCdpDownload).Methods("GET")
	apiRouter.HandleFunc("/llm-config/defaults", api.handleGetLLMDefaults).Methods("GET")
	apiRouter.HandleFunc("/llm-config/models/metadata", api.handleGetModelMetadata).Methods("GET")
	apiRouter.HandleFunc("/llm-config/azure/deployments", api.handleGetAzureDeployedModels).Methods("POST")
	apiRouter.HandleFunc("/llm-config/validate-key", api.handleValidateAPIKey).Methods("POST")
	apiRouter.HandleFunc("/llm-config/delegation-tiers", api.handleGetDelegationTierDefaults).Methods("GET")
	apiRouter.HandleFunc("/session/stop", api.handleStopSession).Methods("POST")
	apiRouter.HandleFunc("/session/clear", api.handleClearSession).Methods("POST")

	// Tool management routes (from tools.go)
	apiRouter.HandleFunc("/tools", api.handleGetTools).Methods("GET")
	apiRouter.HandleFunc("/tools/detail", api.handleGetToolDetail).Methods("GET")
	apiRouter.HandleFunc("/tools/enabled", api.handleSetEnabledTools).Methods("POST")
	apiRouter.HandleFunc("/tools/add", api.handleAddServer).Methods("POST")
	apiRouter.HandleFunc("/tools/edit", api.handleEditServer).Methods("POST")
	apiRouter.HandleFunc("/tools/remove", api.handleRemoveServer).Methods("POST")

	// Tool execution APIs - handlers provided by mcpagent/executor library
	// Pass server logger for proper debugging of session registry usage
	executorHandlers := executor.NewExecutorHandlers(api.mcpConfigPath, api.logger)
	apiRouter.HandleFunc("/mcp/execute", executorHandlers.HandleMCPExecute).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/custom/execute", executorHandlers.HandleCustomExecute).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/virtual/execute", executorHandlers.HandleVirtualExecute).Methods("POST", "OPTIONS")

	// Per-tool endpoints for code execution mode (bearer token auth, bypasses JWT)
	// LLM-generated code calls these directly, so they use API token auth instead of JWT.
	toolsRouter := router.PathPrefix("/tools").Subrouter()
	toolsRouter.Use(executor.AuthMiddleware(api.apiToken))
	toolsRouter.HandleFunc("/mcp/{server}/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		executorHandlers.HandlePerToolMCPRequest(w, r, vars["server"], vars["tool"])
	}).Methods("POST", "OPTIONS")
	toolsRouter.HandleFunc("/custom/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		executorHandlers.HandlePerToolCustomRequest(w, r, vars["tool"])
	}).Methods("POST", "OPTIONS")
	toolsRouter.HandleFunc("/virtual/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		executorHandlers.HandlePerToolVirtualRequest(w, r, vars["tool"])
	}).Methods("POST", "OPTIONS")

	// Session-scoped per-tool endpoints: /s/{session_id}/tools/...
	// These routes bake the session_id into the URL path, so the LLM-generated code
	// doesn't need to explicitly include session_id in request bodies.
	// The session_id is extracted from the path and injected as X-Session-ID header,
	// which the per-tool handler reads as a fallback when body session_id is empty.
	sessionToolsRouter := router.PathPrefix("/s/{session_id}/tools").Subrouter()
	sessionToolsRouter.Use(executor.AuthMiddleware(api.apiToken))
	sessionToolsRouter.HandleFunc("/mcp/{server}/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		r.Header.Set("X-Session-ID", vars["session_id"])
		executorHandlers.HandlePerToolMCPRequest(w, r, vars["server"], vars["tool"])
	}).Methods("POST", "OPTIONS")
	sessionToolsRouter.HandleFunc("/custom/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		r.Header.Set("X-Session-ID", vars["session_id"])
		executorHandlers.HandlePerToolCustomRequest(w, r, vars["tool"])
	}).Methods("POST", "OPTIONS")
	sessionToolsRouter.HandleFunc("/virtual/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		r.Header.Set("X-Session-ID", vars["session_id"])
		executorHandlers.HandlePerToolVirtualRequest(w, r, vars["tool"])
	}).Methods("POST", "OPTIONS")

	// MCP Config API routes (from mcp_config_routes.go)
	apiRouter.HandleFunc("/mcp-config", api.handleGetMCPConfig).Methods("GET")
	apiRouter.HandleFunc("/mcp-config", api.handleSaveMCPConfig).Methods("POST")
	apiRouter.HandleFunc("/mcp-config/discover", api.handleDiscoverServers).Methods("POST")
	apiRouter.HandleFunc("/mcp-config/status", api.handleGetMCPConfigStatus).Methods("GET")
	apiRouter.HandleFunc("/mcp-config/logs", api.handleGetServerLogs).Methods("GET")

	// Secrets encryption API routes (from secrets_routes.go)
	apiRouter.HandleFunc("/secrets/encrypt", api.handleEncryptSecret).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/secrets/decrypt", api.handleDecryptSecret).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/secrets/global", api.handleGetGlobalSecrets).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/secrets/store", api.handleStoreUserSecret).Methods("PUT", "OPTIONS")
	apiRouter.HandleFunc("/secrets/store/{name}", api.handleDeleteUserSecret).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/secrets/stored", api.handleListStoredSecrets).Methods("GET", "OPTIONS")

	// OAuth API routes (from oauth_routes.go)
	apiRouter.HandleFunc("/oauth/start", api.handleOAuthStart).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/oauth/callback", api.handleOAuthCallback).Methods("GET")
	apiRouter.HandleFunc("/oauth/status", api.handleOAuthStatus).Methods("GET")
	apiRouter.HandleFunc("/oauth/logout", api.handleOAuthLogout).Methods("POST", "OPTIONS")

	// Observer APIs removed - events are now stored by sessionID, no observers needed

	// Active Session API routes (from polling.go)
	apiRouter.HandleFunc("/sessions/active", api.handleGetActiveSessions).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/events", api.handleGetSessionEvents).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/events/stream", api.handleSSEStream).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/reconnect", api.handleReconnectSession).Methods("POST")
	apiRouter.HandleFunc("/sessions/{session_id}/status", api.handleGetSessionStatus).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/dismiss", api.handleDismissSession).Methods("POST", "OPTIONS")

	// LLM Guidance API routes
	apiRouter.HandleFunc("/sessions/{session_id}/llm-guidance", api.handleSetLLMGuidance).Methods("POST", "OPTIONS")

	// Context Summarization API routes
	apiRouter.HandleFunc("/sessions/{session_id}/summarize", api.handleSummarizeConversation).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/sessions/{session_id}/compact", api.handleCompactContext).Methods("POST", "OPTIONS")

	// Human Feedback API
	apiRouter.HandleFunc("/human-feedback/submit", api.handleSubmitHumanFeedback).Methods("POST", "OPTIONS")

	// Chat History API routes
	apiRouter.HandleFunc("/chat-history/sessions", createChatSessionHandler(chatDB)).Methods("POST")
	apiRouter.HandleFunc("/chat-history/sessions", listChatSessionsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}", getChatSessionHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}", updateChatSessionHandler(chatDB)).Methods("PUT")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}", deleteChatSessionHandler(chatDB)).Methods("DELETE")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}/events", getSessionEventsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/events", searchEventsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/health", chatHistoryHealthCheckHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/costs", getAllSessionCostsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}/costs", getSessionCostsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/delegation-logs", getAllDelegationLogsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}/delegation-logs", getDelegationLogsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}/delegation-logs/{delegation_id}/events", getDelegationEventsHandler(chatDB)).Methods("GET")

	// Preset Queries API routes
	PresetQueryRoutes(router, chatDB)

	// Slack Feedback API routes
	SlackFeedbackRoutes(router, api, chatDB)

	// Initialize Bot Conversation Manager
	workspaceURL := os.Getenv("WORKSPACE_API_URL")
	if workspaceURL == "" {
		workspaceURL = "http://localhost:8081"
	}
	botManager := slackservice.NewBotConversationManager(chatDB, configPath, workspaceURL)
	botManager.SetEventSubscriber(NewBotEventSubscriberAdapter(eventStore))
	// Bot sessions use ONLY delegation tier config from DB for LLM selection — no server defaults needed
	api.botManager = botManager
	// Wire startSessionInternal after api is created (closure captures api)
	botManager.SetStartSessionFunc(api.startSessionInternal)
	botManager.SetFollowUpFunc(api.sendFollowUpInternal)
	botManager.SetUserSecretsLoader(func(ctx context.Context, userID string) ([]slackservice.DecryptedSecret, error) {
		stored, err := chatDB.ListUserSecrets(ctx, userID)
		if err != nil {
			return nil, err
		}
		var result []slackservice.DecryptedSecret
		for _, s := range stored {
			plaintext, err := decryptSecretValue(s.EncryptedValue, userID)
			if err != nil {
				log.Printf("[SECRETS] Failed to decrypt stored secret %q for user %s: %v", s.Name, userID, err)
				continue // skip broken secrets
			}
			result = append(result, slackservice.DecryptedSecret{Name: s.Name, Value: plaintext})
		}
		return result, nil
	})

	// Wire bot session checker for human feedback (skip 2-min delay for bot sessions)
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	if feedbackStore != nil {
		feedbackStore.SetBotSessionChecker(func(sessionID string) bool {
			return botManager.IsBotSession(sessionID)
		})
	}

	// Register Slack as a bot connector if bot_mode is enabled
	if slackSvc != nil {
		botConfig, _ := chatDB.GetBotConnectorConfig(context.Background(), "slack")
		if botConfig != nil && botConfig.BotMode {
			botManager.RegisterConnector(slackSvc)
			slackSvc.StartListening(context.Background())
			log.Printf("✅ Slack bot mode enabled")
		}
	}

	// Register web simulator connector (always available, no config needed)
	webSimulator := slackservice.NewWebSimulatorConnector()
	botManager.RegisterConnector(webSimulator)
	api.webSimulator = webSimulator
	log.Printf("✅ Web bot simulator enabled")

	// Register bot routes
	BotRoutes(router, api, chatDB)
	BotSimulatorRoutes(router, api, chatDB)

	// Set activity callback for event store to update session LastActivity when events are added
	eventStore.SetActivityCallback(func(sessionID string) {
		api.updateSessionActivity(sessionID)
	})

	// Start background cleanup goroutine to mark inactive sessions (10 minute timeout)
	go api.cleanupInactiveSessions()

	// Workflow API routes
	apiRouter.HandleFunc("/workflow/create", api.handleCreateWorkflow).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/status", api.handleGetWorkflowStatus).Methods("GET")
	apiRouter.HandleFunc("/workflow/update", api.handleUpdateWorkflow).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/constants", orchtypes.HandleWorkflowConstants).Methods("GET")

	// Consolidated workspace state endpoint (NEW - loads everything in one call)
	apiRouter.HandleFunc("/workspace/state", api.handleLoadWorkspaceState).Methods("GET", "OPTIONS")

	// Legacy individual endpoints (kept for backward compatibility)
	apiRouter.HandleFunc("/workflow/run-folders", api.handleGetRunFolders).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/run-folder", api.handleCreateRunFolder).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/progress", api.handleGetProgress).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/run-folder", api.handleDeleteRunFolder).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/workflow/learnings", api.handleDeleteStepLearnings).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/workflow/learnings/all", api.handleGetAllStepLearnings).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/variable-groups", api.handleGetVariableGroups).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/variable-groups", api.handleUpdateVariableGroups).Methods("POST", "PUT", "OPTIONS")
	apiRouter.HandleFunc("/workflow/logs", api.handleGetExecutionLogs).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/logs/file", api.handleGetLogFile).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/costs", api.handleGetCosts).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/evaluation-reports", api.handleGetEvaluationReports).Methods("GET", "OPTIONS")

	// Plan and Step Config API routes
	apiRouter.HandleFunc("/workflow/plan/update-step", api.handleUpdatePlanStep).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/update-step-config", api.handleUpdateStepConfig).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/batch-update-steps", api.handleBatchUpdateSteps).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/delete-step", api.handleDeleteStep).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/add-step", api.handleAddStep).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/step-override", api.handleGetStepOverride).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/step-override", api.handleUpdateStepOverride).Methods("POST", "OPTIONS")

	// Skills API routes (from skill_routes.go)
	RegisterSkillRoutes(apiRouter, api)

	// Sub-agent template API routes (from subagent_routes.go)
	RegisterSubAgentRoutes(apiRouter, api)

	// User-defined command routes (from command_routes.go)
	RegisterCommandRoutes(apiRouter, api)

	// Public file sharing routes — filepath passed as base64 query param
	apiRouter.HandleFunc("/public/file", api.handlePublicFile).Methods("GET")
	apiRouter.HandleFunc("/public/folder", api.handlePublicFolder).Methods("GET")
	apiRouter.HandleFunc("/public/folder/download", api.handlePublicFolderDownload).Methods("GET")

	// pprof routes for profiling (must be before static file serving)
	router.PathPrefix("/debug/pprof/").Handler(http.DefaultServeMux)

	// Static file serving (for frontend)
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
		WriteTimeout: time.Second * 30,  // Increased for streaming
		ReadTimeout:  time.Second * 30,  // Increased for streaming
		IdleTimeout:  time.Second * 300, // 5 min idle timeout to prevent early closes during long queries
		Handler:      router,
	}

	// Start server in a goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	fmt.Printf("✅ Server started on %s:%d\n", config.Host, config.Port)
	fmt.Printf("🔗 API endpoint: http://%s:%d/api/query\n", config.Host, config.Port)
	fmt.Printf("📡 Polling API: http://%s:%d/api/sessions/{session_id}/events\n", config.Host, config.Port)

	// Initialize tool cache on server startup
	fmt.Printf("🔄 Initializing tool cache on server startup...\n")
	api.initializeToolCache()

	// Wait for interrupt signal to gracefully shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c

	fmt.Println("\n🛑 Shutting down server...")

	// Stop background discovery
	fmt.Println("⏹️ Stopping background tool discovery...")
	api.stopPeriodicRefresh()

	// Close all MCP session connections to prevent orphaned subprocesses
	fmt.Println("🧹 Closing all MCP sessions...")
	mcpagent.CloseAllSessions()

	// Create a deadline for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Shutdown server
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	fmt.Println("✅ Server shutdown complete")
}

// GetAPIURL returns the base URL for the API server
// It handles replacing 0.0.0.0 with 127.0.0.1 for local loopback calls
func (api *StreamingAPI) GetAPIURL() string {
	host := api.config.Host
	if host == "0.0.0.0" || host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, api.config.Port)
}

// GetCodeExecAPIURL returns the API URL as seen from inside the workspace container.
// Shell commands in code execution mode run inside the Docker workspace-api container,
// so they need host.docker.internal to reach the Go server on the host.
// Falls back to the normal API URL when workspace is not Dockerized.
func (api *StreamingAPI) GetCodeExecAPIURL() string {
	wsURL := getWorkspaceAPIURL()
	// If workspace API points to localhost (Docker-mapped port), shell commands
	// run inside Docker and need host.docker.internal to reach the host
	if strings.Contains(wsURL, "localhost") || strings.Contains(wsURL, "127.0.0.1") {
		return fmt.Sprintf("http://host.docker.internal:%d", api.config.Port)
	}
	// In Docker Compose networking, use the Go server's service name or host
	return api.GetAPIURL()
}

// mergeGlobalSecrets prepends global secrets to user-supplied secrets.
// User secrets take priority on name collision.
// If selectedGlobalNames is non-nil, only global secrets whose name is in the list are included.
func mergeGlobalSecrets(userSecrets []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}, selectedGlobalNames *[]string) []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
} {
	globals := getGlobalSecrets()
	if len(globals) == 0 {
		return userSecrets
	}
	// Build filter set from selected global names (nil = include all)
	var allowedGlobals map[string]bool
	if selectedGlobalNames != nil {
		allowedGlobals = make(map[string]bool, len(*selectedGlobalNames))
		for _, name := range *selectedGlobalNames {
			allowedGlobals[name] = true
		}
	}
	// Build a set of user-supplied secret names for dedup
	userNames := make(map[string]bool, len(userSecrets))
	for _, s := range userSecrets {
		userNames[s.Name] = true
	}
	// Prepend globals that don't collide with user secrets and are in the allowed set
	var merged []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	for _, g := range globals {
		if userNames[g.Name] {
			continue
		}
		if allowedGlobals != nil && !allowedGlobals[g.Name] {
			continue
		}
		merged = append(merged, struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{Name: g.Name, Value: g.Value})
	}
	merged = append(merged, userSecrets...)
	return merged
}

// getOrCreatePlanSessionState returns the session-level plan state, creating one if needed.
// On first access for a session, it restores plan state from the database if available,
// allowing resumed sessions to pick up existing plans.
func (api *StreamingAPI) getOrCreatePlanSessionState(sessionID string) *virtualtools.PlanSessionState {
	api.planSessionStatesMux.Lock()
	defer api.planSessionStatesMux.Unlock()
	if state, exists := api.planSessionStates[sessionID]; exists {
		return state
	}
	state := virtualtools.NewPlanSessionState()

	// Try to restore plan state from database session config
	if api.chatDB != nil {
		if chatSession, err := api.chatDB.GetChatSession(context.Background(), sessionID); err == nil && chatSession != nil {
			if config, err := chatSession.GetConfig(); err == nil && config != nil && config.PlanID != "" {
				state.PlanID = config.PlanID
				state.PlanFolder = config.PlanFolder
				if config.PlanPhase != "" {
					state.Phase = config.PlanPhase
				}
				log.Printf("[PLAN STATE] Restored plan state from DB for session %s: plan=%s folder=%s phase=%s",
					sessionID, config.PlanID, config.PlanFolder, state.Phase)
			}
		}
	}

	api.planSessionStates[sessionID] = state
	return state
}

// CORS middleware
func (api *StreamingAPI) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		for _, allowed := range api.config.CORSOrigins {
			if allowed == "*" || allowed == origin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				break
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Session-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Health check endpoint
func (api *StreamingAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Get current tracing provider
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"time":    time.Now(),
		"version": llmtypes.VERSION,
		"config": map[string]interface{}{"provider": api.config.Provider,
			"model":            api.config.ModelID,
			"temperature":      api.config.Temperature,
			"max_turns":        api.config.MaxTurns,
			"tracing_provider": tracingProvider,
		},
	})
}

// handlePublicFile serves workspace files via a shareable URL.
// GET /api/public/file?path=<base64-encoded-filepath>
func (api *StreamingAPI) handlePublicFile(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("path")
	if encoded == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	// Decode base64 filepath
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			log.Printf("[PUBLIC-FILE] Failed to decode base64 path: %s, error: %v", encoded, err)
			http.Error(w, "invalid file path encoding", http.StatusBadRequest)
			return
		}
	}
	filePath := string(decoded)

	// Use uid from query param (owner's ID for cross-user sharing), fall back to auth context
	uid := r.URL.Query().Get("uid")
	if uid == "" {
		uid = GetUserIDFromContext(r.Context())
	}
	log.Printf("[PUBLIC-FILE] Serving file: %s for user: %s", filePath, uid)

	// URL-encode each path segment for the workspace API
	segments := strings.Split(filePath, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	encodedPath := strings.Join(segments, "/")

	wsURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath + "/raw"
	log.Printf("[PUBLIC-FILE] Proxying to workspace: %s", wsURL)

	req, err := http.NewRequestWithContext(r.Context(), "GET", wsURL, nil)
	if err != nil {
		log.Printf("[PUBLIC-FILE] Failed to create request: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("X-User-ID", uid)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PUBLIC-FILE] Workspace request failed: %v", err)
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[PUBLIC-FILE] Workspace returned %d: %s", resp.StatusCode, string(body))
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Proxy content-type and body — force inline display (no download)
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Disposition", "inline")
	io.Copy(w, resp.Body)
}

// handlePublicFolder lists workspace folder contents via a shareable URL.
// GET /api/public/folder?path=<base64-encoded-folderpath>
func (api *StreamingAPI) handlePublicFolder(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("path")
	if encoded == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			log.Printf("[PUBLIC-FOLDER] Failed to decode base64 path: %s, error: %v", encoded, err)
			http.Error(w, "invalid path encoding", http.StatusBadRequest)
			return
		}
	}
	folderPath := string(decoded)

	// Use uid from query param (owner's ID for cross-user sharing), fall back to auth context
	uid := r.URL.Query().Get("uid")
	if uid == "" {
		uid = GetUserIDFromContext(r.Context())
	}
	log.Printf("[PUBLIC-FOLDER] Listing folder: %s for user: %s", folderPath, uid)

	wsURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(r.Context(), "GET", wsURL, nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	q := req.URL.Query()
	q.Set("folder", folderPath)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("X-User-ID", uid)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PUBLIC-FOLDER] Workspace request failed: %v", err)
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handlePublicFolderDownload exports a shared folder as a ZIP download.
// GET /api/public/folder/download?path=<base64-encoded-folderpath>&uid=<owner-id>
func (api *StreamingAPI) handlePublicFolderDownload(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("path")
	if encoded == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			http.Error(w, "invalid path encoding", http.StatusBadRequest)
			return
		}
	}
	folderPath := string(decoded)

	uid := r.URL.Query().Get("uid")
	if uid == "" {
		uid = GetUserIDFromContext(r.Context())
	}
	log.Printf("[PUBLIC-FOLDER-DOWNLOAD] Exporting folder: %s for user: %s", folderPath, uid)

	// Proxy to workspace export endpoint
	wsURL := getWorkspaceAPIURL() + "/api/workspace/export"
	body, _ := json.Marshal(map[string]string{"workspace_path": folderPath})
	req, err := http.NewRequestWithContext(r.Context(), "POST", wsURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uid)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PUBLIC-FOLDER-DOWNLOAD] Workspace request failed: %v", err)
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[PUBLIC-FOLDER-DOWNLOAD] Workspace returned %d: %s", resp.StatusCode, string(respBody))
		http.Error(w, "export failed", resp.StatusCode)
		return
	}

	// Proxy ZIP response headers and body
	for _, h := range []string{"Content-Type", "Content-Disposition", "Content-Length"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	io.Copy(w, resp.Body)
}

// API Key Validation endpoint - validates API keys for OpenRouter and OpenAI
// Capabilities endpoint
func (api *StreamingAPI) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Get current tracing provider
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"providers":   []string{"bedrock", "openai", "anthropic"},
		"streaming":   true,
		"sse":         true,
		"agent_modes": []string{"simple", "orchestrator", "workflow"},
		"tracing": map[string]interface{}{
			"enabled":  tracingProvider != "noop",
			"provider": tracingProvider,
		},
		"workspace": map[string]interface{}{
			"semantic_search_enabled": workspace.IsSemanticSearchEnabled(),
			"github_sync_enabled":     workspace.IsGitSyncEnabled(),
		},
		"servers":    []string{},
		"local_mode": IsLocalMode(),
	})
}

// handleCdpCheck checks if a CDP port is reachable on localhost
func (api *StreamingAPI) handleCdpCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	portStr := r.URL.Query().Get("port")
	if portStr == "" {
		portStr = "9222"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
			"error":     "invalid port number",
		})
		return
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
		})
		return
	}
	conn.Close()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"connected": true,
	})
}

// getSupportedProviders returns the list of supported LLM providers based on environment configuration
func getSupportedProviders() []string {
	allProviders := []string{"openrouter", "bedrock", "openai", "vertex", "anthropic", "azure", "claude-code", "gemini-cli"}
	envValue := os.Getenv("SUPPORTED_LLM_PROVIDERS")
	if envValue == "" {
		return allProviders
	}

	// Parse comma-separated list
	parts := strings.Split(envValue, ",")
	validProviders := make(map[string]bool)
	for _, p := range allProviders {
		validProviders[p] = true
	}

	var supported []string
	for _, part := range parts {
		provider := strings.ToLower(strings.TrimSpace(part))
		if provider == "" {
			continue
		}
		if validProviders[provider] {
			supported = append(supported, provider)
		} else {
			log.Printf("Warning: ignoring invalid provider '%s' in SUPPORTED_LLM_PROVIDERS", part)
		}
	}

	// If no valid providers found, return all
	if len(supported) == 0 {
		log.Printf("Warning: no valid providers found in SUPPORTED_LLM_PROVIDERS, enabling all providers")
		return allProviders
	}

	return supported
}

// isGlobalLLMConfigLocked returns true if all LLM configuration is locked
func isGlobalLLMConfigLocked() bool {
	val := os.Getenv("LLM_CONFIG_LOCKED")
	return val == "true" || val == "1"
}

// getLockedProviders returns a list of locked providers from the environment variable
func getLockedProviders() []string {
	val := os.Getenv("LLM_CONFIG_LOCKED")
	if val == "true" || val == "1" {
		return []string{"all"}
	}
	if val == "" || val == "false" || val == "0" {
		return []string{}
	}
	// Split by comma
	parts := strings.Split(val, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(strings.ToLower(parts[i]))
	}
	return parts
}

// isProviderLocked returns true if the specific provider is locked
func isProviderLocked(provider string) bool {
	if provider == "" {
		return false
	}
	locked := getLockedProviders()
	for _, p := range locked {
		if p == "all" || p == strings.ToLower(provider) {
			return true
		}
	}
	return false
}

// isAllowedDefaultLLM returns true when (provider, modelID) is in the default published LLMs list (for locked mode).
func isAllowedDefaultLLM(provider, modelID string) bool {
	if provider == "" || modelID == "" {
		return false
	}
	defaults := llm.GetLLMDefaults()
	// Only restrict to defaults if the *specific* provider is locked
	if !isProviderLocked(provider) {
		return true
	}

	// Allow any model listed in AvailableModels for this provider
	if models, ok := defaults.AvailableModels[provider]; ok {
		for _, m := range models {
			if m == modelID {
				return true
			}
		}
	}

	list := getDefaultPublishedLLMs(true, defaults.PrimaryConfig)
	for _, entry := range list {
		p, _ := entry["provider"].(string)
		m, _ := entry["model_id"].(string)
		if p == provider && m == modelID {
			return true
		}
	}
	return false
}

// getPrimaryProviderAndModelFromDefaults extracts provider and model_id from llm.GetLLMDefaults().PrimaryConfig.
func getPrimaryProviderAndModelFromDefaults() (provider, modelID string) {
	defaults := llm.GetLLMDefaults()
	bytes, err := json.Marshal(defaults.PrimaryConfig)
	if err != nil {
		return "openrouter", llm.GetDefaultModel(llm.Provider("openrouter"))
	}
	var m map[string]interface{}
	if err := json.Unmarshal(bytes, &m); err != nil {
		return "openrouter", llm.GetDefaultModel(llm.Provider("openrouter"))
	}
	if p, _ := m["provider"].(string); p != "" {
		provider = p
	} else {
		provider = "openrouter"
	}
	if mid, _ := m["model_id"].(string); mid != "" {
		modelID = mid
	} else {
		modelID = llm.GetDefaultModel(llm.Provider(provider))
	}
	return provider, modelID
}

// buildProviderAPIKeysFromEnv builds llm.ProviderAPIKeys from environment variables (for locked mode).
func buildProviderAPIKeysFromEnv() *llm.ProviderAPIKeys {
	keys := &llm.ProviderAPIKeys{}
	if s := os.Getenv("OPENROUTER_API_KEY"); s != "" {
		keys.OpenRouter = &s
	}
	if s := os.Getenv("OPENAI_API_KEY"); s != "" {
		keys.OpenAI = &s
	}
	if s := os.Getenv("ANTHROPIC_API_KEY"); s != "" {
		keys.Anthropic = &s
	}
	if s := os.Getenv("VERTEX_API_KEY"); s != "" {
		keys.Vertex = &s
	} else if s := os.Getenv("GOOGLE_API_KEY"); s != "" {
		keys.Vertex = &s
	} else if s := os.Getenv("GEMINI_API_KEY"); s != "" {
		keys.Vertex = &s
	} else if s := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); s != "" {
		keys.Vertex = &s
	}
	if region := os.Getenv("BEDROCK_REGION"); region != "" {
		keys.Bedrock = &llm.BedrockConfig{Region: region}
	}
	if s := os.Getenv("GEMINI_API_KEY"); s != "" {
		keys.GeminiCLI = &s
	}
	if endpoint := os.Getenv("AZURE_AI_ENDPOINT"); endpoint != "" {
		apiKey := os.Getenv("AZURE_AI_API_KEY")
		apiVer := os.Getenv("AZURE_AI_API_VERSION")
		region := os.Getenv("AZURE_AI_REGION")
		keys.Azure = &llm.AzureAPIConfig{
			Endpoint:   endpoint,
			APIKey:     apiKey,
			APIVersion: apiVer,
			Region:     region,
		}
	}
	return keys
}

// stripSecretsFromMap recursively removes api_key, endpoint, and other sensitive fields from m (for locked mode).
// endpoint is stripped so Azure tenant URLs (e.g. https://tenant-name.openai.azure.com/) are not sent to the client.
func stripSecretsFromMap(m map[string]interface{}) {
	delete(m, "api_key")
	delete(m, "endpoint")
	for _, v := range m {
		if nested, ok := v.(map[string]interface{}); ok {
			stripSecretsFromMap(nested)
		}
	}
}

// getDefaultPublishedLLMs returns the list of default published LLMs from env, file, or primary config.
// When locked is true, entries must not include api_key or endpoint (Azure tenant URL).
func getDefaultPublishedLLMs(locked bool, primaryConfig interface{}) []map[string]interface{} {
	stripEntrySecrets := func(entry map[string]interface{}) {
		delete(entry, "api_key")
		delete(entry, "endpoint")
	}
	// 1) Try DEFAULT_PUBLISHED_LLMS (JSON string)
	if s := os.Getenv("DEFAULT_PUBLISHED_LLMS"); s != "" {
		var list []map[string]interface{}
		if err := json.Unmarshal([]byte(s), &list); err == nil && len(list) > 0 {
			for i := range list {
				provider, _ := list[i]["provider"].(string)
				if locked || isProviderLocked(provider) {
					stripEntrySecrets(list[i])
				}
			}
			return list
		}
	}
	// 2) Try DEFAULT_PUBLISHED_LLMS_PATH (path to JSON file)
	if path := os.Getenv("DEFAULT_PUBLISHED_LLMS_PATH"); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			var list []map[string]interface{}
			if err := json.Unmarshal(data, &list); err == nil && len(list) > 0 {
				for i := range list {
					provider, _ := list[i]["provider"].(string)
					if locked || isProviderLocked(provider) {
						stripEntrySecrets(list[i])
					}
				}
				return list
			}
		}
	}

	// 3) Auto-generate defaults from AvailableModels for locked providers
	var entries []map[string]interface{}
	defaults := llm.GetLLMDefaults()
	providers := []string{"azure", "bedrock", "openrouter", "openai", "anthropic", "vertex"}

	for _, p := range providers {
		// If provider is locked (or global lock is on), include its available models
		if isProviderLocked(p) || locked {
			if models, ok := defaults.AvailableModels[p]; ok {
				for _, m := range models {
					entry := map[string]interface{}{
						"id":       "default-" + p + "-" + strings.ReplaceAll(m, "/", "-"),
						"name":     m, // Simple name
						"provider": p,
						"model_id": m,
					}
					// Secrets are stripped by default since we don't add them here
					entries = append(entries, entry)
				}
			}
		}
	}

	if len(entries) > 0 {
		// Also ensure the primary config is included if not already
		// (Optional, but good for safety. For now, let's just return the generated list as it covers available models)
		return entries
	}

	// 4) Fallback: Build one entry from primary config if nothing else found
	var provider, modelID string
	if pc, ok := primaryConfig.(map[string]interface{}); ok {
		if p, _ := pc["provider"].(string); p != "" {
			provider = p
		}
		if m, _ := pc["model_id"].(string); m != "" {
			modelID = m
		}
	}
	if provider == "" {
		provider = "openrouter"
	}
	if modelID == "" {
		modelID = llm.GetDefaultModel(llm.Provider(provider))
	}
	entry := map[string]interface{}{
		"id":       "default-" + provider + "-" + strings.ReplaceAll(modelID, "/", "-"),
		"name":     "Default (" + provider + ")",
		"provider": provider,
		"model_id": modelID,
	}

	isLocked := locked || isProviderLocked(provider)

	if !isLocked {
		if key := os.Getenv("OPENROUTER_API_KEY"); provider == "openrouter" && key != "" {
			entry["api_key"] = key
		} else if key := os.Getenv("OPENAI_API_KEY"); provider == "openai" && key != "" {
			entry["api_key"] = key
		} else if key := os.Getenv("ANTHROPIC_API_KEY"); provider == "anthropic" && key != "" {
			entry["api_key"] = key
		}
	}
	return []map[string]interface{}{entry}
}

// handleGetLLMDefaults returns default LLM configurations from environment variables.
// When LLM_CONFIG_LOCKED=true (or specific provider is locked), api_key and endpoint are stripped.
func (api *StreamingAPI) handleGetLLMDefaults(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request for LLM defaults")
	defaults := llm.GetLLMDefaults()

	globalLocked := isGlobalLLMConfigLocked()
	lockedProviders := getLockedProviders()

	// Build response (same shape as before)
	response := map[string]interface{}{
		"primary_config":      defaults.PrimaryConfig,
		"openrouter_config":   defaults.OpenrouterConfig,
		"bedrock_config":      defaults.BedrockConfig,
		"openai_config":       defaults.OpenaiConfig,
		"anthropic_config":    defaults.AnthropicConfig,
		"azure_config":        defaults.AzureConfig,
		"available_models":    defaults.AvailableModels,
		"supported_providers": getSupportedProviders(),
		"locked_providers":    lockedProviders,
	}

	// Helper to safely strip secrets from a specific config map
	stripSecrets := func(configKey string) {
		if cfg, ok := response[configKey].(map[string]interface{}); ok {
			// Marshal/unmarshal to deep copy/clean if needed, but direct map manipulation is fine for simple removal
			delete(cfg, "api_key")
			delete(cfg, "endpoint")
			response[configKey] = cfg
		}
	}

	// Strip secrets based on locking status
	if globalLocked {
		// Strip from all
		stripSecretsFromMap(response)
	} else {
		// Strip from specifically locked providers
		for _, p := range lockedProviders {
			switch p {
			case "openrouter":
				stripSecrets("openrouter_config")
			case "bedrock":
				stripSecrets("bedrock_config")
			case "openai":
				stripSecrets("openai_config")
			case "anthropic":
				stripSecrets("anthropic_config")
			case "azure":
				stripSecrets("azure_config")
			case "vertex":
				stripSecrets("vertex_config")
			}
		}
	}

	response["llm_config_locked"] = globalLocked
	response["default_published_llms"] = getDefaultPublishedLLMs(globalLocked, defaults.PrimaryConfig)
	response["default_published_llms_locked"] = globalLocked

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleValidateAPIKey validates API keys for OpenRouter, OpenAI, Bedrock, Vertex, Anthropic, and Claude Code
func (api *StreamingAPI) handleValidateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req llm.APIKeyValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode API key validation request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Received API key validation request for provider: %s", req.Provider)

	// Claude Code uses the local CLI — validate by running a test prompt
	if req.Provider == "claude-code" {
		response := validateClaudeCodeCLI()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Gemini CLI uses the local CLI — validate by checking it exists and sending a test prompt
	if req.Provider == "gemini-cli" {
		response := validateGeminiCLI(req.APIKey)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := llm.ValidateAPIKey(req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// validateClaudeCodeCLI validates the Claude Code CLI by checking it exists and sending a test prompt
func validateClaudeCodeCLI() llm.APIKeyValidationResponse {
	log.Printf("[CLAUDE-CODE VALIDATION] Starting CLI validation")

	// Step 1: Check if claude CLI is on PATH
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		log.Printf("[CLAUDE-CODE VALIDATION] CLI not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Claude Code CLI not found. Install it with: npm install -g @anthropic-ai/claude-code",
		}
	}
	log.Printf("[CLAUDE-CODE VALIDATION] CLI found at: %s", claudePath)

	// Step 2: Send a test prompt via the CLI and check for a response
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "--print", "--dangerously-skip-permissions", "Say hello in one short sentence.")
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := string(output)
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[CLAUDE-CODE VALIDATION] CLI test timed out")
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: "Claude Code CLI timed out after 60s. Check that you are authenticated (run 'claude' to log in).",
			}
		}
		log.Printf("[CLAUDE-CODE VALIDATION] CLI test failed: %v — output: %s", err, errMsg)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Claude Code CLI error: %s", strings.TrimSpace(errMsg)),
		}
	}

	responseText := strings.TrimSpace(string(output))
	if responseText == "" {
		log.Printf("[CLAUDE-CODE VALIDATION] CLI returned empty response")
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Claude Code CLI returned an empty response. Check authentication with 'claude'.",
		}
	}

	log.Printf("[CLAUDE-CODE VALIDATION SUCCESS] Got response: %s", responseText)
	return llm.APIKeyValidationResponse{
		Valid:   true,
		Message: fmt.Sprintf("Claude Code CLI is working. Response: %s", responseText),
	}
}

// validateGeminiCLI validates the Gemini CLI by checking it exists and sending a test prompt
func validateGeminiCLI(apiKey string) llm.APIKeyValidationResponse {
	log.Printf("[GEMINI-CLI VALIDATION] Starting CLI validation")

	// Step 1: Check if gemini CLI is on PATH
	geminiPath, err := exec.LookPath("gemini")
	if err != nil {
		log.Printf("[GEMINI-CLI VALIDATION] CLI not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Gemini CLI not found. Install it with: npm install -g @anthropic-ai/gemini-cli (see https://github.com/google-gemini/gemini-cli)",
		}
	}
	log.Printf("[GEMINI-CLI VALIDATION] CLI found at: %s", geminiPath)

	// Step 2: Send a test prompt via the CLI and check for a response
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gemini", "--approval-mode", "yolo", "--prompt", "Say hello in one short sentence.")
	// Pass API key as env var if provided (from frontend or server .env)
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey != "" {
		cmd.Env = append(os.Environ(), "GEMINI_API_KEY="+apiKey)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := string(output)
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[GEMINI-CLI VALIDATION] CLI test timed out")
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: "Gemini CLI timed out after 60s. Check that you are authenticated (run 'gemini' to log in).",
			}
		}
		log.Printf("[GEMINI-CLI VALIDATION] CLI test failed: %v — output: %s", err, errMsg)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Gemini CLI error: %s", strings.TrimSpace(errMsg)),
		}
	}

	responseText := strings.TrimSpace(string(output))
	if responseText == "" {
		log.Printf("[GEMINI-CLI VALIDATION] CLI returned empty response")
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Gemini CLI returned an empty response. Check authentication with 'gemini'.",
		}
	}

	log.Printf("[GEMINI-CLI VALIDATION SUCCESS] Got response: %s", responseText)
	return llm.APIKeyValidationResponse{
		Valid:   true,
		Message: fmt.Sprintf("Gemini CLI is working. Response: %s", responseText),
	}
}

// Query endpoint - handles POST requests to start agent streaming
func (api *StreamingAPI) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Parse request body first
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorMsg := fmt.Sprintf("Invalid request body: %v", err)
		http.Error(w, errorMsg, http.StatusBadRequest)
		return
	}

	// Handle alias: Map Message to Query if Query is empty
	if req.Query == "" && req.Message != "" {
		req.Query = req.Message
	}

	// Validate required fields
	if req.Query == "" {
		errorMsg := "Query is required"
		http.Error(w, errorMsg, http.StatusBadRequest)
		return
	}

	// Record start time for duration calculation
	startTime := time.Now()
	log.Printf("[LATENCY_DEBUG] T+0ms | Request received | query_preview=%q", truncateForLog(req.Query, 80))

	// Generate query ID
	queryID := fmt.Sprintf("query_%d", time.Now().UnixNano())

	// Initialize Langfuse tracing - single trace for entire conversation
	// Read tracing provider from environment variable, default to "noop"
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}
	tracer := observability.GetTracer(tracingProvider)
	traceName := fmt.Sprintf("agent-conversation: %s", r.Header.Get("X-Session-ID"))
	if traceName == "agent-conversation: " {
		traceName = fmt.Sprintf("agent-conversation: %s", queryID)
	}
	traceID := tracer.StartTrace(traceName, map[string]interface{}{
		"method":     r.Method,
		"url":        r.URL.String(),
		"user_agent": r.Header.Get("User-Agent"),
		"session_id": r.Header.Get("X-Session-ID"),
		"query":      req.Query,
		"query_id":   queryID,
	})

	// NOTE: For workflow mode, LLM selection follows priority: temp override → step config → preset LLM
	// No orchestrator default fallback. req.Provider and req.ModelID are not used for workflow agents.
	// For non-workflow agents (simple/chat mode), req.Provider and req.ModelID come from the frontend request.
	// Environment variable fallbacks have been removed - frontend always sends these values.

	// Default maxTurns from environment variable or 100 if not provided or 0 (applies to both workflow and simple agent modes)
	if req.MaxTurns <= 0 {
		req.MaxTurns = orchestrator.GetDefaultMaxTurnsFromEnv()
		log.Printf("[AGENT] MaxTurns not provided or 0, defaulting to %d (from env or 100)", req.MaxTurns)
	}

	// Use enabled_servers if provided, otherwise fall back to servers
	selectedServers := req.EnabledServers
	if len(selectedServers) == 0 {
		selectedServers = req.Servers
	}

	var serverList string
	// Check for explicit "NO_SERVERS" request (pure LLM mode, no tools)
	if len(selectedServers) == 1 && selectedServers[0] == mcpclient.NoServers {
		// Keep NoServers constant as-is - this will be handled by integration code
		serverList = mcpclient.NoServers
	} else {
		// Default to no servers if none specified (user didn't select any)
		if len(selectedServers) == 0 {
			serverList = mcpclient.NoServers
		} else {
			// Convert server array to comma-separated string for agent compatibility
			serverList = strings.Join(selectedServers, ",")
		}
	}

	// Extract sessionID from header/cookie or fallback to queryID
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		sessionID = queryID // fallback: use queryID as sessionID if not provided
	}

	// Get current user ID for session isolation
	currentUserID := GetUserIDFromContext(r.Context())
	log.Printf("[USER_ID_DEBUGGING] HTTP handler: currentUserID=%q (from auth context)", currentUserID)

	// Create or get chat session for this query
	// The agent will modify the session ID to agent-init-{sessionID}-{timestamp}
	// So we need to create the chat session with the original sessionID
	// and the events will use the modified sessionID
	chatSession, err := api.chatDB.GetChatSessionWithUser(r.Context(), sessionID, currentUserID)
	if err != nil {
		// Chat session doesn't exist, create a new one
		// Extract title from req.Query (user message)
		// Remove file context suffix if present (format: "...\n\n📁 Files in context: ...")
		title := req.Query
		if idx := strings.Index(title, "\n\n📁 Files in context:"); idx != -1 {
			title = title[:idx]
		}
		title = strings.TrimSpace(title)
		// Truncate to 50 characters
		if len(title) > 50 {
			title = title[:50] + "..."
		}

		// Build typed config from request
		var configJSON json.RawMessage
		hasConfig := len(req.Servers) > 0 || len(req.EnabledServers) > 0 || req.UseCodeExecutionMode || req.EnableContextSummarization != nil || req.Provider != "" || req.ModelID != "" || req.LLMConfig != nil || len(req.SelectedSkills) > 0 || len(req.SelectedSubAgents) > 0 || req.DelegationMode != ""
		if hasConfig {
			config := &database.ChatSessionConfig{
				SelectedServers:      req.Servers,
				EnabledServers:       req.EnabledServers,
				UseCodeExecutionMode: req.UseCodeExecutionMode,
				SelectedSkills:       req.SelectedSkills,
				SelectedSubAgents:    req.SelectedSubAgents,
				EnableContextSummarization: func() *bool {
					if req.EnableContextSummarization != nil {
						val := *req.EnableContextSummarization
						return &val
					}
					return nil
				}(),
			}

			// Extract LLM config (prefer LLMConfig field, fallback to Provider/ModelID)
			if req.LLMConfig != nil {
				config.LLMConfig = &database.LLMConfigForStorage{
					Provider: req.LLMConfig.Primary.Provider,
					ModelID:  req.LLMConfig.Primary.ModelID,
				}
				// Convert Fallbacks to FallbackModels for storage
				if len(req.LLMConfig.Fallbacks) > 0 {
					for _, fallback := range req.LLMConfig.Fallbacks {
						config.LLMConfig.FallbackModels = append(config.LLMConfig.FallbackModels, fallback.ModelID)
					}
				}
			} else if req.Provider != "" || req.ModelID != "" {
				config.LLMConfig = &database.LLMConfigForStorage{
					Provider: req.Provider,
					ModelID:  req.ModelID,
				}
			}

			// Store delegation mode and tier config for session persistence
			if req.DelegationMode != "" {
				config.DelegationMode = req.DelegationMode
			}
			if req.DelegationTierConfig != nil {
				tierJSON, marshalErr := json.Marshal(req.DelegationTierConfig)
				if marshalErr == nil {
					config.DelegationTierConfig = tierJSON
				}
			}

			// If LLM config is locked and not provided by frontend, use server defaults from env
			if config.LLMConfig == nil && isGlobalLLMConfigLocked() {
				provider, modelID := getPrimaryProviderAndModelFromDefaults()
				if provider != "" && modelID != "" {
					config.LLMConfig = &database.LLMConfigForStorage{
						Provider: provider,
						ModelID:  modelID,
					}
					log.Printf("[CONFIG DEBUG] Using locked LLM config from env: provider=%s, model=%s", provider, modelID)
				}
			}

			var err error
			configJSON, err = config.ToJSON()
			if err != nil {
				log.Printf("[CONFIG DEBUG] Failed to marshal config: %v", err)
				configJSON = nil
			}
		}

		chatSession, err = api.chatDB.CreateChatSessionWithUser(r.Context(), &database.CreateChatSessionRequest{
			SessionID:     sessionID,
			Title:         title,
			AgentMode:     req.AgentMode,
			PresetQueryID: req.PresetQueryID,
			Config:        configJSON,
		}, currentUserID)
		if err != nil {
			log.Printf("[DATABASE DEBUG] Failed to create chat session: %v", err)
			// Continue without chat session - events won't be stored but query can proceed
		}
	} else {
		// Prepare update request
		updateReq := &database.UpdateChatSessionRequest{}
		shouldUpdate := false

		// 1. Update PresetQueryID if provided and currently missing or different
		// This fixes "orphan" sessions by associating them with the current preset
		if req.PresetQueryID != "" {
			currentID := ""
			if chatSession.PresetQueryID != nil {
				currentID = *chatSession.PresetQueryID
			}
			if currentID != req.PresetQueryID {
				updateReq.PresetQueryID = req.PresetQueryID
				shouldUpdate = true
				log.Printf("[SESSION UPDATE] Updating session %s PresetQueryID from '%s' to '%s'", sessionID, currentID, req.PresetQueryID)
			}
		}

		// 2. Reactivate session if it was stopped/completed/error
		if chatSession.Status == "stopped" || chatSession.Status == "completed" || chatSession.Status == "error" {
			updateReq.Status = "active"
			updateReq.CompletedAt = nil // Clear completion timestamp when reactivating
			shouldUpdate = true
			log.Printf("[SESSION REACTIVATION] Reactivating session %s (old status: %s)", sessionID, chatSession.Status)
		}

		// 3. Update config if skills or other settings changed
		// This ensures selected_skills and other settings are persisted on each query
		hasConfigToUpdate := len(req.SelectedSkills) > 0 || len(req.SelectedSubAgents) > 0 || len(req.EnabledServers) > 0 || req.UseCodeExecutionMode || req.LLMConfig != nil || req.DelegationMode != ""
		if hasConfigToUpdate {
			config := &database.ChatSessionConfig{
				SelectedServers:      req.Servers,
				EnabledServers:       req.EnabledServers,
				UseCodeExecutionMode: req.UseCodeExecutionMode,
				SelectedSkills:       req.SelectedSkills,
				SelectedSubAgents:    req.SelectedSubAgents,
				EnableContextSummarization: func() *bool {
					if req.EnableContextSummarization != nil {
						val := *req.EnableContextSummarization
						return &val
					}
					return nil
				}(),
			}

			// Extract LLM config
			if req.LLMConfig != nil {
				config.LLMConfig = &database.LLMConfigForStorage{
					Provider: req.LLMConfig.Primary.Provider,
					ModelID:  req.LLMConfig.Primary.ModelID,
				}
				if len(req.LLMConfig.Fallbacks) > 0 {
					for _, fallback := range req.LLMConfig.Fallbacks {
						config.LLMConfig.FallbackModels = append(config.LLMConfig.FallbackModels, fallback.ModelID)
					}
				}
			} else if req.Provider != "" || req.ModelID != "" {
				config.LLMConfig = &database.LLMConfigForStorage{
					Provider: req.Provider,
					ModelID:  req.ModelID,
				}
			}

			// Preserve delegation mode and tier config on session updates
			if req.DelegationMode != "" {
				config.DelegationMode = req.DelegationMode
			}
			if req.DelegationTierConfig != nil {
				tierJSON, marshalErr := json.Marshal(req.DelegationTierConfig)
				if marshalErr == nil {
					config.DelegationTierConfig = tierJSON
				}
			}

			configJSON, err := config.ToJSON()
			if err != nil {
				log.Printf("[CONFIG DEBUG] Failed to marshal config for update: %v", err)
			} else {
				updateReq.Config = configJSON
				shouldUpdate = true
				log.Printf("[SESSION UPDATE] Updating session %s config with selected_skills=%v", sessionID, req.SelectedSkills)
			}
		}

		// Apply updates if needed
		if shouldUpdate {
			_, err := api.chatDB.UpdateChatSession(r.Context(), sessionID, updateReq)
			if err != nil {
				log.Printf("[DATABASE ERROR] Failed to update chat session %s: %v", sessionID, err)
				// Continue with existing session state - critical path should not fail
			}
		}

		// Initialize EventStore for reactivated session to ensure new events are stored correctly
		// Only needed if we actually reactivated (status changed)
		if chatSession.Status == "stopped" || chatSession.Status == "completed" || chatSession.Status == "error" {
			// CRITICAL: Lock session reactivation to prevent race conditions
			// Multiple concurrent requests could calculate different baseIndex values
			// and initialize the session with misaligned event indices
			api.sessionReactivationMux.Lock()

			// Calculate existing event count to use as baseIndex for polling
			// Use COUNT query instead of fetching all events — much faster for large sessions
			var baseIndex int
			countQuery := "SELECT COUNT(*) FROM events WHERE chat_session_id = ?"
			if isPostgresDB(api.chatDB) {
				countQuery = "SELECT COUNT(*) FROM events WHERE chat_session_id = $1"
			}
			err := api.chatDB.GetDB().QueryRowContext(r.Context(), countQuery, chatSession.ID).Scan(&baseIndex)
			if err != nil {
				log.Printf("[SESSION REACTIVATION] Failed to count existing events for session %s: %v", sessionID, err)
				baseIndex = 0
			} else {
				log.Printf("[SESSION REACTIVATION] Found %d existing events for session %s, setting baseIndex", baseIndex, sessionID)
			}
			api.eventStore.InitializeSession(sessionID, baseIndex)

			api.sessionReactivationMux.Unlock()
		}
	}

	// Track active session for page refresh recovery (no observer needed)
	api.trackActiveSession(sessionID, req.AgentMode, req.Query, currentUserID)
	log.Printf("[LATENCY_DEBUG] T+%dms | Session setup complete | sessionID=%s", time.Since(startTime).Milliseconds(), sessionID)

	// Create fresh agent for this request
	// Use LLM configuration from request if provided, otherwise use request defaults
	var finalProvider string
	var finalModelID string
	var fallbacks []agent.FallbackModel

	if isGlobalLLMConfigLocked() {
		// Locked mode: use server env for API keys; allow provider/model only if in default_published_llms
		if req.LLMConfig != nil && req.LLMConfig.Primary.Provider != "" && req.LLMConfig.Primary.ModelID != "" {
			p, m := req.LLMConfig.Primary.Provider, req.LLMConfig.Primary.ModelID
			if isAllowedDefaultLLM(p, m) {
				finalProvider, finalModelID = p, m
			} else {
				finalProvider, finalModelID = getPrimaryProviderAndModelFromDefaults()
			}
		} else {
			finalProvider, finalModelID = getPrimaryProviderAndModelFromDefaults()
		}
		supported := getSupportedProviders()
		if len(supported) > 0 {
			allowed := make(map[string]bool)
			for _, p := range supported {
				allowed[p] = true
			}
			if !allowed[finalProvider] {
				finalProvider = supported[0]
				finalModelID = llm.GetDefaultModel(llm.Provider(finalProvider))
			}
		}
		fallbacks = nil
	} else if req.LLMConfig != nil {
		// Use LLM configuration from frontend (new unified structure)
		finalProvider = req.LLMConfig.Primary.Provider
		finalModelID = req.LLMConfig.Primary.ModelID

		// Fallback to request defaults if LLMConfig is partially empty
		if finalProvider == "" {
			finalProvider = req.Provider
		}
		if finalModelID == "" {
			finalModelID = req.ModelID
		}

		// Convert Fallbacks to agent.FallbackModel slice
		for _, fallback := range req.LLMConfig.Fallbacks {
			fallbacks = append(fallbacks, agent.FallbackModel{
				Provider: fallback.Provider,
				ModelID:  fallback.ModelID,
			})
		}
	} else {
		// Fall back to request defaults
		finalProvider = req.Provider
		finalModelID = req.ModelID
	}

	// If provider is still empty (e.g. follow-up message with no config), load from stored session config
	if finalProvider == "" && chatSession != nil && len(chatSession.Config) > 0 {
		var storedConfig database.ChatSessionConfig
		if err := json.Unmarshal(chatSession.Config, &storedConfig); err == nil && storedConfig.LLMConfig != nil {
			if storedConfig.LLMConfig.Provider != "" {
				finalProvider = storedConfig.LLMConfig.Provider
				finalModelID = storedConfig.LLMConfig.ModelID
				log.Printf("[SESSION FALLBACK] Loaded provider/model from stored session config: %s/%s", finalProvider, finalModelID)
			}
		}
	}

	// Handle workflow mode - use workflow orchestrator
	if req.AgentMode == "workflow" {

		// Check if preset_id is provided and workflow is approved
		if req.PresetQueryID != "" {
			log.Printf("[WORKFLOW CHECK] Checking workflow approval status for preset_id: %s", req.PresetQueryID)

			// Get workflow from database to check approval status
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			workflow, err := api.chatDB.GetWorkflowByPresetQueryID(ctx, req.PresetQueryID)
			if err != nil {
				log.Printf("[WORKFLOW CHECK ERROR] Workflow not found for preset_id %s: %v", req.PresetQueryID, err)
				// Continue with planning phase if workflow not found
			} else {
				log.Printf("[WORKFLOW CHECK] Found workflow: workflowStatus=%s", workflow.WorkflowStatus)

				// If workflow is approved, proceed with execution using user's query
				if workflow.WorkflowStatus == database.WorkflowStatusPostVerification {
					log.Printf("[WORKFLOW CHECK] Workflow is approved - proceeding with execution using user query: %s", req.Query)
				} else {
					log.Printf("[WORKFLOW CHECK] Workflow is not approved yet - proceeding with planning phase")
				}
			}
		}

		// Create workflow event bridge for event emission
		// Note: ChatDB is set to nil - workflow events are stored in memory only (for polling API)
		// Chat history database storage is disabled for workflows to reduce database load
		workflowEventBridge := &eventbridge.WorkflowEventBridge{
			BaseEventBridge: &eventbridge.BaseEventBridge{
				EventStore: api.eventStore,
				SessionID:  sessionID,
				Logger:     api.logger,
				ChatDB:     nil, // Disable database storage for workflows
				BridgeName: "workflow",
			},
		}

		// Create custom tools for workflow agents (workspace tools + human tools)
		// Workflow agents can be Simple or ReAct agents, tools are registered based on mode
		// TODO: Memory tools removed from workflow - only needed for individual React agents
		// memoryTools := virtualtools.CreateMemoryTools()
		// memoryExecutors := virtualtools.CreateMemoryToolExecutors()
		allTools, allExecutors, toolCategories := createCustomTools(true) // Workflow mode: all tools (basic + git + advanced + human)

		// NOTE: Workspace executor replacement with session + secrets happens after secrets are merged (see below).

		// Load selected tools, code execution mode, tool search mode, skills, and preset LLM config from preset if available (for workflow agents)
		var selectedTools []string
		var useCodeExecutionMode bool
		var useToolSearchMode bool
		var preDiscoveredTools []string
		var selectedSkills []string
		var presetLLMConfig *database.PresetLLMConfig
		if req.PresetQueryID != "" {
			ctx := context.Background()
			preset, err := api.chatDB.GetPresetQuery(ctx, req.PresetQueryID)
			if err == nil {
				// Load selected tools
				if preset.SelectedTools != "" {
					if err := json.Unmarshal([]byte(preset.SelectedTools), &selectedTools); err != nil {
						log.Printf("[TOOLS] Failed to parse selected tools from preset: %v", err)
					} else {
						if len(selectedTools) > 0 {
							log.Printf("[TOOLS] Loaded %d specific tools from preset", len(selectedTools))
						} else {
							log.Printf("[TOOLS] Preset has empty tool selection - will use ALL tools from selected servers")
						}
					}
				}
				// Load preset LLM config for agent defaults
				if len(preset.LLMConfig) > 0 {
					log.Printf("[PRESET LLM DEBUG] Raw preset LLM config JSON: %s", string(preset.LLMConfig))
					if err := json.Unmarshal(preset.LLMConfig, &presetLLMConfig); err != nil {
						log.Printf("[PRESET LLM] Failed to parse preset LLM config: %v", err)
					} else {
						log.Printf("[PRESET LLM] Loaded preset LLM config with agent defaults")
						// Debug: log what was loaded
						if presetLLMConfig != nil {
							log.Printf("[PRESET LLM DEBUG] Legacy: provider=%s, model=%s", presetLLMConfig.Provider, presetLLMConfig.ModelID)
							if presetLLMConfig.ExecutionLLM != nil {
								log.Printf("[PRESET LLM DEBUG] ExecutionLLM: provider=%s, model=%s", presetLLMConfig.ExecutionLLM.Provider, presetLLMConfig.ExecutionLLM.ModelID)
							}
							if presetLLMConfig.ValidationLLM != nil {
								log.Printf("[PRESET LLM DEBUG] ValidationLLM: provider=%s, model=%s", presetLLMConfig.ValidationLLM.Provider, presetLLMConfig.ValidationLLM.ModelID)
							}
							if presetLLMConfig.LearningLLM != nil {
								log.Printf("[PRESET LLM DEBUG] LearningLLM: provider=%s, model=%s", presetLLMConfig.LearningLLM.Provider, presetLLMConfig.LearningLLM.ModelID)
							}
							if presetLLMConfig.PhaseLLM != nil {
								log.Printf("[PRESET LLM DEBUG] PhaseLLM: provider=%s, model=%s", presetLLMConfig.PhaseLLM.Provider, presetLLMConfig.PhaseLLM.ModelID)
							} else {
								log.Printf("[PRESET LLM DEBUG] PhaseLLM: nil (will use fallback)")
							}
						} else {
							log.Printf("[PRESET LLM DEBUG] presetLLMConfig is nil after unmarshal")
						}
					}
				} else {
					log.Printf("[PRESET LLM DEBUG] No preset LLM config found (empty or null)")
				}
				// Load code execution mode from preset
				useCodeExecutionMode = preset.UseCodeExecutionMode
				if useCodeExecutionMode {
					log.Printf("[CODE_EXECUTION] Code execution mode enabled from preset")
				}
				// Load tool search mode from preset
				useToolSearchMode = preset.UseToolSearchMode
				if useToolSearchMode {
					log.Printf("[TOOL_SEARCH] Tool search mode enabled from preset")
				}
				// Load pre-discovered tools from preset
				if preset.PreDiscoveredTools != "" {
					if err := json.Unmarshal([]byte(preset.PreDiscoveredTools), &preDiscoveredTools); err != nil {
						log.Printf("[TOOL_SEARCH] Failed to parse pre-discovered tools from preset: %v", err)
					} else if len(preDiscoveredTools) > 0 {
						log.Printf("[TOOL_SEARCH] Loaded %d pre-discovered tools from preset", len(preDiscoveredTools))
					}
				}
				// Load selected skills from preset
				if preset.SelectedSkills != "" {
					var skills []string
					if err := json.Unmarshal([]byte(preset.SelectedSkills), &skills); err != nil {
						log.Printf("[SKILLS] Failed to parse selected skills from preset: %v", err)
					} else if len(skills) > 0 {
						selectedSkills = skills
						log.Printf("[SKILLS] Loaded %d skills from preset: %v", len(skills), skills)
					}
				}
				// Load browser access mode from preset
				if preset.EnableBrowserAccess {
					// Add browser tools to the available tools pool
					browserCategory := virtualtools.GetWorkspaceBrowserToolCategory()
					browserTools := virtualtools.CreateWorkspaceBrowserTools()
					browserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutors(getCdpPort(req))

					allTools = append(allTools, browserTools...)
					for name, executor := range browserExecutors {
						allExecutors[name] = executor
					}

					// Assign category to browser tools
					for _, tool := range browserTools {
						if tool.Function != nil {
							toolCategories[tool.Function.Name] = browserCategory
						}
					}
					log.Printf("[WORKFLOW] Added browser tools (enable_browser_access: true)")

					// Auto-add agent-browser skill if not already selected
					hasAgentBrowserSkill := false
					for _, skill := range selectedSkills {
						if skill == "agent-browser" {
							hasAgentBrowserSkill = true
							break
						}
					}
					if !hasAgentBrowserSkill {
						selectedSkills = append(selectedSkills, "agent-browser")
						log.Printf("[WORKFLOW] Auto-adding agent-browser skill for browser access")
					}
				}
			}
		}

		// Use selected tools from request if preset didn't provide any
		if len(selectedTools) == 0 && len(req.SelectedTools) > 0 {
			selectedTools = req.SelectedTools
			if len(selectedTools) > 0 {
				log.Printf("[TOOLS] Using %d specific tools from request", len(selectedTools))
			} else {
				log.Printf("[TOOLS] Request has empty tool selection - will use ALL tools from selected servers")
			}
		} else if len(selectedTools) == 0 {
			log.Printf("[TOOLS] No tool selection specified - will use ALL tools from selected servers")
		}

		// Use code execution mode from request if preset didn't provide any
		if !useCodeExecutionMode && req.UseCodeExecutionMode {
			useCodeExecutionMode = req.UseCodeExecutionMode
			log.Printf("[CODE_EXECUTION] Code execution mode enabled from request")
		}

		// Auto-enable code execution mode for claude-code provider (workflow path).
		// Claude Code accesses MCP tools via the HTTP bridge, which requires code execution mode.
		if req.Provider == "claude-code" && !useCodeExecutionMode {
			useCodeExecutionMode = true
			log.Printf("[CLAUDE CODE] Auto-enabled code execution mode for MCP tool access via bridge (workflow)")
		}

		// Auto-enable code execution mode for gemini-cli provider (workflow path).
		// Gemini CLI accesses MCP tools via the pre-configured bridge, which requires code execution mode.
		if req.Provider == "gemini-cli" && !useCodeExecutionMode {
			useCodeExecutionMode = true
			log.Printf("[GEMINI CLI] Auto-enabled code execution mode for MCP tool access via bridge (workflow)")
		}

		// Use tool search mode from request if preset didn't provide any
		if !useToolSearchMode && req.UseToolSearchMode {
			useToolSearchMode = req.UseToolSearchMode
			log.Printf("[TOOL_SEARCH] Tool search mode enabled from request")
		}

		// Create workflow orchestrator for this request
		// Note: req.MaxTurns is already defaulted to 100 earlier in the handler
		// Note: provider and model parameters removed - LLM selection uses temp override → step config → preset LLM
		workflowOrchestrator, err := orchtypes.NewWorkflowOrchestrator(
			api.mcpConfigPath,    // mcpConfigPath
			api.temperature,      // temperature
			"workflow",           // agentMode
			api.logger,           // logger
			workflowEventBridge,  // eventBridge
			tracer,               // tracer
			selectedServers,      // selectedServers
			selectedTools,        // NEW: selectedTools
			useCodeExecutionMode, // NEW: code execution mode
			useToolSearchMode,    // NEW: tool search mode
			preDiscoveredTools,   // NEW: pre-discovered tools
			allTools,             // customTools
			allExecutors,         // customToolExecutors
			req.LLMConfig,        // llmConfig
			req.MaxTurns,         // maxTurns (defaults to 100 if not provided)
			toolCategories,       // NEW: toolCategories
			presetLLMConfig,      // preset LLM config for agent defaults
		)
		if err != nil {
			log.Printf("[WORKFLOW ERROR] Failed to create workflow orchestrator: %v", err)
			http.Error(w, fmt.Sprintf("Failed to create workflow orchestrator: %v", err), http.StatusInternalServerError)
			return
		}

		// Set selected skills on the orchestrator
		if len(selectedSkills) > 0 {
			workflowOrchestrator.SetSelectedSkills(selectedSkills)
			log.Printf("[SKILLS] Applied %d skills to workflow orchestrator: %v", len(selectedSkills), selectedSkills)
		}

		// Merge global secrets with user-supplied secrets, then set on orchestrator
		allSecrets := mergeGlobalSecrets(req.DecryptedSecrets, req.SelectedGlobalSecrets)
		if len(allSecrets) > 0 {
			entries := make([]orchestrator.SecretEntry, len(allSecrets))
			for i, s := range allSecrets {
				entries[i] = orchestrator.SecretEntry{Name: s.Name, Value: s.Value}
			}
			workflowOrchestrator.SetSecrets(entries)
			log.Printf("[SECRETS] Applied %d secrets (%d global + %d user) to workflow orchestrator", len(entries), len(entries)-len(req.DecryptedSecrets), len(req.DecryptedSecrets))
		}

		// Replace workspace advanced executors with session-aware versions that include secrets.
		// This must happen AFTER secrets are merged so secrets are available as shell env vars.
		// createCustomTools uses the no-session CreateWorkspaceAdvancedToolExecutors(),
		// which means MCP_SESSION_ID won't be set and secrets won't be in the shell env.
		secretEnvVars := make(map[string]string, len(allSecrets))
		for _, s := range allSecrets {
			secretEnvVars[s.Name] = s.Value
		}
		sessionAwareExecutors, workspaceEnv := virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv(currentUserID, sessionID, secretEnvVars)
		for name, executor := range sessionAwareExecutors {
			allExecutors[name] = executor
		}
		log.Printf("[WORKFLOW] Replaced workspace executors with session-aware versions (userID=%q, sessionID=%q, secrets=%d)", currentUserID, sessionID, len(secretEnvVars))

		// Store workspace env map reference on orchestrator so that when the MCP session ID
		// changes (e.g., per-group in batch execution), MCP_API_URL in the env is updated
		// automatically. This prevents session registry misses that cause new browser instances.
		workflowOrchestrator.SetWorkspaceEnvRef(workspaceEnv)
		log.Printf("[WORKFLOW] Stored workspace env ref on orchestrator (MCP_API_URL=%s)", workspaceEnv["MCP_API_URL"])

		// Store workflow orchestrator for guidance injection
		api.storeWorkflowOrchestrator(sessionID, workflowOrchestrator)

		// Create a cancellable context for workflow execution using background context
		// This prevents the workflow from being canceled when the HTTP request ends
		workflowCtx, workflowCancel := context.WithCancel(context.Background())

		// Inject user ID into the workflow context for per-user folder isolation
		// This allows workspace tools to route per-user folders correctly
		workflowCtx = context.WithValue(workflowCtx, common.UserIDKey, currentUserID)

		// Store the cancel function for potential cancellation (keyed by queryID for independent executions)
		api.workflowOrchestratorContextMux.Lock()
		api.workflowOrchestratorContexts[queryID] = workflowCancel
		api.workflowOrchestratorContextMux.Unlock()

		// Track which queryIDs belong to this session (for handleStopSession)
		api.sessionQueryIDMux.Lock()
		api.sessionQueryIDs[sessionID] = append(api.sessionQueryIDs[sessionID], queryID)
		api.sessionQueryIDMux.Unlock()

		// Return immediate response with query ID
		response := QueryResponse{
			QueryID:   queryID,
			SessionID: sessionID, // Include the actual session ID used for conversation history
			Status:    "started",
			Message:   "Query processing started. Use polling API to get real-time updates.",
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
			return
		}

		// Execute workflow asynchronously
		go func() {
			defer func() {
				// Clean up the cancel function when done (keyed by queryID)
				api.workflowOrchestratorContextMux.Lock()
				delete(api.workflowOrchestratorContexts, queryID)
				api.workflowOrchestratorContextMux.Unlock()

				// Remove queryID from session tracking
				api.sessionQueryIDMux.Lock()
				if queryIDs, exists := api.sessionQueryIDs[sessionID]; exists {
					// Filter out this queryID
					newQueryIDs := make([]string, 0, len(queryIDs)-1)
					for _, qid := range queryIDs {
						if qid != queryID {
							newQueryIDs = append(newQueryIDs, qid)
						}
					}
					if len(newQueryIDs) > 0 {
						api.sessionQueryIDs[sessionID] = newQueryIDs
					} else {
						delete(api.sessionQueryIDs, sessionID)
					}
				}
				api.sessionQueryIDMux.Unlock()
			}()

			// Check database for workflow approval status if preset_id is provided
			workflowStatus := database.WorkflowStatusPreVerification // Default status
			var selectedOptions *database.WorkflowSelectedOptions
			var stepID string
			if req.PresetQueryID != "" {
				// Check workflow approval status from database
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				workflow, err := api.chatDB.GetWorkflowByPresetQueryID(ctx, req.PresetQueryID)
				if err == nil {
					workflowStatus = workflow.WorkflowStatus
					selectedOptions = workflow.SelectedOptions
					log.Printf("[WORKFLOW CHECK] Database check: workflowStatus=%s", workflowStatus)
					if selectedOptions != nil {
						log.Printf("[WORKFLOW CHECK] Found selected options: %+v", selectedOptions)
						log.Printf("[WORKFLOW CHECK] Selected options details - PhaseID: %s, Selections count: %d", selectedOptions.PhaseID, len(selectedOptions.Selections))
						for i, selection := range selectedOptions.Selections {
							log.Printf("[WORKFLOW CHECK] Selection[%d] - Group: %s, OptionID: %s, OptionLabel: %s", i, selection.Group, selection.OptionID, selection.OptionLabel)
						}
					} else {
						log.Printf("[WORKFLOW CHECK] No selected options found")
					}
				} else {
					log.Printf("[WORKFLOW CHECK] Could not check database: %v", err)
				}

				// Retrieve step_id if it was stored for this preset
				api.workflowStepIDMux.RLock()
				if api.workflowStepIDs != nil {
					if storedStepID, exists := api.workflowStepIDs[req.PresetQueryID]; exists {
						stepID = storedStepID
						log.Printf("[WORKFLOW CHECK] Found step_id for preset: %s", stepID)
						// Clear it after retrieval (one-time use)
						delete(api.workflowStepIDs, req.PresetQueryID)
					}
				}
				api.workflowStepIDMux.RUnlock()
			} else {
				log.Printf("[WORKFLOW CHECK] No preset_query_id provided, using default workflowStatus: %s", workflowStatus)
			}

			log.Printf("[WORKFLOW EXECUTION] Executing workflow with status: %s", workflowStatus)
			if stepID != "" {
				log.Printf("[WORKFLOW EXECUTION] Step-specific execution for step: %s", stepID)
			}

			// Get the actual objective from preset (not from the query string)
			workflowObjective := req.Query // Default to query if preset not available
			workflowWorkspacePath := ""
			if req.PresetQueryID != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				preset, err := api.chatDB.GetPresetQuery(ctx, req.PresetQueryID)
				if err == nil && preset != nil {
					// Use preset's Query field as the objective (the actual workflow objective)
					// Fall back to req.Query if preset query is empty (e.g. workflow builder sets empty preset query)
					if preset.Query != "" {
						workflowObjective = preset.Query
					}
					log.Printf("[WORKFLOW EXECUTION] Using objective: %s (preset.Query=%q, req.Query=%q)", workflowObjective, preset.Query, req.Query)

					// Extract workspace path from preset's selected folder
					if preset.SelectedFolder.Valid && preset.SelectedFolder.String != "" {
						workflowWorkspacePath = preset.SelectedFolder.String
						log.Printf("[WORKFLOW EXECUTION] Using preset workspace path: %s", workflowWorkspacePath)
					}
				} else {
					log.Printf("[WORKFLOW WARNING] Could not get preset for objective: %v", err)
				}
			}

			// Fallback: Extract workspace path from objective if not found in preset
			if workflowWorkspacePath == "" {
				workflowWorkspacePath = extractWorkspacePathFromObjective(workflowObjective)
				if workflowWorkspacePath == "" {
					log.Printf("[WORKFLOW ERROR] Workspace path not found in objective for query %s", queryID)
					workflowWorkspacePath = "default_workspace" // fallback
				}
			}

			// Prepare options for the Execute method
			workflowOptions := map[string]interface{}{
				"workflowStatus":  workflowStatus,  // Current workflow status
				"selectedOptions": selectedOptions, // Pass selected options from database
			}
			if stepID != "" {
				workflowOptions["stepId"] = stepID // Pass step ID for step-specific phase execution
			}

			// Pass execution options from frontend if provided
			log.Printf("[EXECUTION_OPTIONS_DEBUG] [Backend] Received request - req.ExecutionOptions is nil: %v", req.ExecutionOptions == nil)
			if req.ExecutionOptions != nil {
				log.Printf("[EXECUTION_OPTIONS_DEBUG] [Backend] Execution options received: %+v", req.ExecutionOptions)
				log.Printf("[WORKFLOW EXECUTION] Frontend execution options provided: run_mode=%s, strategy=%s, run_folder=%s, resume_from_step=%d, enabled_group_ids=%v, skip_learning_temp_llm1=%v, skip_learning_temp_llm2=%v, save_validation_responses=%v",
					req.ExecutionOptions.RunMode, req.ExecutionOptions.ExecutionStrategy, req.ExecutionOptions.SelectedRunFolder, req.ExecutionOptions.ResumeFromStep, req.ExecutionOptions.EnabledGroupIDs, req.ExecutionOptions.SkipLearningWhenTempLLM1, req.ExecutionOptions.SkipLearningWhenTempLLM2, req.ExecutionOptions.SaveValidationResponses)

				// Convert to controller ExecutionOptions and pass to workflow orchestrator
				controllerOpts := &todo_creation_human.ExecutionOptions{
					RunMode:                        req.ExecutionOptions.RunMode,
					SelectedRunFolder:              req.ExecutionOptions.SelectedRunFolder,
					ExecutionStrategy:              req.ExecutionOptions.ExecutionStrategy,
					ResumeFromStep:                 req.ExecutionOptions.ResumeFromStep,
					FastExecuteEndStep:             req.ExecutionOptions.FastExecuteEndStep,
					PlanChangeAction:               req.ExecutionOptions.PlanChangeAction,
					AllStepsCompletedAction:        req.ExecutionOptions.AllStepsCompletedAction,
					FallbackToOriginalLLMOnFailure: req.ExecutionOptions.FallbackToOriginalLLMOnFailure,
					SkipLearningWhenTempLLM1:       req.ExecutionOptions.SkipLearningWhenTempLLM1,
					SkipLearningWhenTempLLM2:       req.ExecutionOptions.SkipLearningWhenTempLLM2,
					EnabledGroupIDs:                req.ExecutionOptions.EnabledGroupIDs,
					SaveValidationResponses:        req.ExecutionOptions.SaveValidationResponses,
					SkipExecutionCleanup:           req.ExecutionOptions.SkipExecutionCleanup,
				}

				// Convert TempOverrideLLM if present
				if req.ExecutionOptions.TempOverrideLLM != nil {
					controllerOpts.TempOverrideLLM = &todo_creation_human.AgentLLMConfig{
						Provider: req.ExecutionOptions.TempOverrideLLM.Provider,
						ModelID:  req.ExecutionOptions.TempOverrideLLM.ModelID,
					}
					log.Printf("[WORKFLOW EXECUTION] Temp override LLM 1 included: %s/%s", controllerOpts.TempOverrideLLM.Provider, controllerOpts.TempOverrideLLM.ModelID)
				} else {
					// Explicitly set to nil to ensure backend clears any existing override
					controllerOpts.TempOverrideLLM = nil
					log.Printf("[WORKFLOW EXECUTION] Temp override LLM 1 not provided (disabled or not set) - will clear existing override")
				}

				// Convert TempOverrideLLM2 if present
				if req.ExecutionOptions.TempOverrideLLM2 != nil {
					controllerOpts.TempOverrideLLM2 = &todo_creation_human.AgentLLMConfig{
						Provider: req.ExecutionOptions.TempOverrideLLM2.Provider,
						ModelID:  req.ExecutionOptions.TempOverrideLLM2.ModelID,
					}
					log.Printf("[WORKFLOW EXECUTION] Temp override LLM 2 included: %s/%s", controllerOpts.TempOverrideLLM2.Provider, controllerOpts.TempOverrideLLM2.ModelID)
				} else {
					// Explicitly set to nil to ensure backend clears any existing override
					controllerOpts.TempOverrideLLM2 = nil
					log.Printf("[WORKFLOW EXECUTION] Temp override LLM 2 not provided (disabled or not set) - will clear existing override")
				}

				// Convert TempLearningLLM if present
				if req.ExecutionOptions.TempLearningLLM != nil {
					controllerOpts.TempLearningLLM = &todo_creation_human.AgentLLMConfig{
						Provider: req.ExecutionOptions.TempLearningLLM.Provider,
						ModelID:  req.ExecutionOptions.TempLearningLLM.ModelID,
					}
					log.Printf("[WORKFLOW EXECUTION] Temp learning LLM included: %s/%s", controllerOpts.TempLearningLLM.Provider, controllerOpts.TempLearningLLM.ModelID)
				} else {
					// Explicitly set to nil to ensure backend clears any existing override
					controllerOpts.TempLearningLLM = nil
					log.Printf("[WORKFLOW EXECUTION] Temp learning LLM not provided - will clear existing override")
				}

				// Set execution options on the workflow orchestrator
				log.Printf("[EXECUTION_OPTIONS_DEBUG] [Backend] Setting execution options on orchestrator: %+v", controllerOpts)
				workflowOrchestrator.SetExecutionOptions(controllerOpts)
				log.Printf("[EXECUTION_OPTIONS_DEBUG] [Backend] Execution options set on orchestrator successfully")
			} else {
				log.Printf("[EXECUTION_OPTIONS_DEBUG] [Backend] No execution options provided in request - req.ExecutionOptions is nil")
			}

			// Execute workflow with the preset objective (not the phase query)
			log.Printf("[WORKFLOW DEBUG] Starting workflow execution for query %s with objective: %s, workspace: %s", queryID, workflowObjective, workflowWorkspacePath)
			_, err := workflowOrchestrator.Execute(
				workflowCtx,
				workflowObjective, // Use preset objective instead of req.Query
				workflowWorkspacePath,
				workflowOptions,
			)
			if err != nil {
				log.Printf("[WORKFLOW ERROR] Workflow execution failed for query %s: %v", queryID, err)

				// Extract root cause error from the error chain
				rootCauseError := extractRootCauseError(err)
				fullError := err.Error()

				// Emit UnifiedCompletionEvent with root cause error (for UI display)
				errorEventData := unifiedevents.NewUnifiedCompletionEventWithError(
					"workflow",            // agentType
					"workflow",            // agentMode
					workflowObjective,     // question
					rootCauseError,        // root cause error message
					time.Since(startTime), // duration
					0,                     // turns
				)
				agentEvent := unifiedevents.NewAgentEvent(errorEventData)
				agentEvent.SessionID = sessionID
				completionEvent := events.Event{
					ID:        fmt.Sprintf("workflow_completion_error_%s_%d", queryID, time.Now().UnixNano()),
					Type:      string(unifiedevents.EventTypeUnifiedCompletion),
					Timestamp: time.Now(),
					Data:      agentEvent,
					SessionID: sessionID,
				}
				api.eventStore.AddEvent(sessionID, completionEvent)
				log.Printf("[WORKFLOW ERROR] Emitted UnifiedCompletionEvent with root cause error for query %s: %s", queryID, rootCauseError)

				// Also send workflow_error event with both root cause and full chain
				errorData := map[string]interface{}{
					"error":       rootCauseError, // Root cause (most important)
					"error_chain": fullError,      // Full error chain for debugging
					"query_id":    queryID,
				}
				api.eventStore.AddEvent(sessionID, events.Event{
					ID:        fmt.Sprintf("workflow_error_%s_%d", queryID, time.Now().UnixNano()),
					Type:      "workflow_error",
					Timestamp: time.Now(),
					Data: &unifiedevents.AgentEvent{
						Type:      "workflow_error",
						Timestamp: time.Now(),
						Data: &unifiedevents.GenericEventData{
							Data: errorData,
						},
					},
					SessionID: sessionID,
				})

				// --- BEGIN: Update chat session status to error ---
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				chatSession, err := api.chatDB.GetChatSession(ctx, sessionID)
				cancel()
				if err == nil && chatSession != nil {
					updateReq := &database.UpdateChatSessionRequest{
						Title:     chatSession.Title,     // Preserve existing title
						AgentMode: chatSession.AgentMode, // Preserve existing agent_mode
						Status:    "error",
					}
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					_, err := api.chatDB.UpdateChatSession(ctx, sessionID, updateReq)
					cancel()
					if err != nil {
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to error (workflow): %v", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to error status (workflow)", sessionID)
					}
				}
				// --- END: Update chat session status to error ---

				// Update active session status to error
				log.Printf("[WORKFLOW COMPLETION] Updating session %s status to error", sessionID)
				api.updateSessionStatus(sessionID, "error")
			} else {
				log.Printf("[WORKFLOW DEBUG] Workflow execution completed for query %s", queryID)
				// Workflow completion events are now handled by the workflow orchestrator itself

				// --- BEGIN: Update chat session status to completed ---
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				chatSession, err := api.chatDB.GetChatSession(ctx, sessionID)
				cancel()
				if err == nil && chatSession != nil {
					// Update session status to completed with completion timestamp
					completedAt := time.Now()
					updateReq := &database.UpdateChatSessionRequest{
						Status:      "completed",
						CompletedAt: &completedAt,
					}
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					_, err := api.chatDB.UpdateChatSession(ctx, sessionID, updateReq)
					cancel()
					if err != nil {
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to completed (workflow): %v", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to completed status (workflow)", sessionID)
					}
				}
				// --- END: Update chat session status to completed ---

				// Update active session status to completed
				log.Printf("[WORKFLOW COMPLETION] Updating session %s status to completed", sessionID)
				api.updateSessionStatus(sessionID, "completed")
			}
		}()
		return
	}

	// Load preset LLM config for chat/simple mode (for feature toggle fallbacks)
	var presetLLMConfig *database.PresetLLMConfig
	if req.PresetQueryID != "" {
		ctx := context.Background()
		preset, err := api.chatDB.GetPresetQuery(ctx, req.PresetQueryID)
		if err == nil && len(preset.LLMConfig) > 0 {
			if err := json.Unmarshal(preset.LLMConfig, &presetLLMConfig); err != nil {
				log.Printf("[PRESET LLM] Failed to parse preset LLM config for chat mode: %v", err)
			}
		}
	}

	// Return immediate response with query ID
	response := QueryResponse{
		QueryID:   queryID,
		SessionID: sessionID, // Include the actual session ID used for conversation history
		Status:    "started",
		Message:   "Query processing started. Use polling API to get real-time updates.",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}

	// Don't clear events - let the frontend handle event continuation
	// The deduplication logic in the frontend will handle any duplicates

	// Store last query request for synthetic turns and set session busy
	if req.DelegationMode == "plan" {
		req.userID = currentUserID
		api.lastQueryMu.Lock()
		api.lastQueryRequests[sessionID] = req
		api.lastQueryMu.Unlock()
		api.setSessionBusy(sessionID, true)
	}

	// Process the query in the background
	go func() {
		// Clear session busy when the agent turn completes
		if req.DelegationMode == "plan" {
			defer func() {
				api.setSessionBusy(sessionID, false)
				// Drain pending completions after turn ends
				pending := api.drainPendingCompletions(sessionID)
				for _, pendingAgentID := range pending {
					go api.processBackgroundAgentCompletion(sessionID, pendingAgentID)
				}
			}()
		}

		// Helper function to send error and continue (not terminate)
		sendError := func(errorMsg string, shouldTerminate bool) {
			if shouldTerminate {
				tracer.EndTrace(traceID, map[string]interface{}{
					"status": "failed",
				})

				// Update chat session status to error
				if chatSession != nil {
					updateReq := &database.UpdateChatSessionRequest{
						Title:     chatSession.Title,     // Preserve existing title
						AgentMode: chatSession.AgentMode, // Preserve existing agent_mode
						Status:    "error",
					}
					_, err := api.chatDB.UpdateChatSession(r.Context(), sessionID, updateReq)
					if err != nil {
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to error: %v", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to error status", sessionID)
					}
				}

				// Emit server-level error completion event
				// Create an error completion event using UnifiedCompletionEvent
				errorEventData := unifiedevents.NewUnifiedCompletionEventWithError(
					"server",              // agentType
					req.AgentMode,         // agentMode
					req.Query,             // question
					errorMsg,              // error message
					time.Since(startTime), // duration
					0,                     // turns
				)

				agentEvent := unifiedevents.NewAgentEvent(errorEventData)
				agentEvent.SessionID = sessionID

				serverErrorEvent := events.Event{
					ID:        fmt.Sprintf("server_error_%s_%d", queryID, time.Now().UnixNano()),
					Type:      string(unifiedevents.EventTypeUnifiedCompletion),
					Timestamp: time.Now(),
					Data:      agentEvent,
					SessionID: sessionID,
				}
				api.eventStore.AddEvent(sessionID, serverErrorEvent)
				log.Printf("[SERVER DEBUG] Emitted server error completion event for query %s", queryID)
			}
		}

		// Validate provider
		llmProvider, err := llm.ValidateProvider(req.Provider)
		if err != nil {
			sendError(fmt.Sprintf("Invalid provider: %v", err), true)
			return
		}

		// Validate LLM provider - no need to initialize since agent wrapper handles it
		_ = llmProvider // Use provider variable to avoid unused variable error

		// Create context with timeout for the entire streaming operation
		streamCtx, cancel := context.WithTimeout(context.Background(), 60*3*time.Minute)
		defer cancel()

		// Load selected tools and code execution mode from preset if available (for simple/ReAct agents)
		var selectedTools []string
		var useCodeExecutionMode bool
		var useToolSearchMode bool
		var presetSetCodeExecutionMode bool // Track if preset explicitly set the value

		if req.PresetQueryID != "" {
			ctx := context.Background()
			preset, err := api.chatDB.GetPresetQuery(ctx, req.PresetQueryID)
			if err == nil {
				if preset.SelectedTools != "" {
					if err := json.Unmarshal([]byte(preset.SelectedTools), &selectedTools); err != nil {
						log.Printf("[TOOLS] Failed to parse selected tools from preset: %v", err)
					} else {
						if len(selectedTools) > 0 {
							log.Printf("[TOOLS] Loaded %d specific tools from preset", len(selectedTools))
						} else {
							log.Printf("[TOOLS] Preset has empty tool selection - will use ALL tools from selected servers")
						}
					}
				}
				// Load code execution mode from preset
				useCodeExecutionMode = preset.UseCodeExecutionMode
				presetSetCodeExecutionMode = true
				if useCodeExecutionMode {
					log.Printf("[CODE_EXECUTION] Code execution mode enabled from preset")
				} else {
					log.Printf("[CODE_EXECUTION] Code execution mode disabled from preset")
				}
				// Load tool search mode from preset
				useToolSearchMode = preset.UseToolSearchMode
				if useToolSearchMode {
					log.Printf("[TOOL_SEARCH] Tool search mode enabled from preset")
				}
			}
		}

		// Use selected tools from request if preset didn't provide any
		if len(selectedTools) == 0 && len(req.SelectedTools) > 0 {
			selectedTools = req.SelectedTools
			if len(selectedTools) > 0 {
				log.Printf("[TOOLS] Using %d specific tools from request", len(selectedTools))
			} else {
				log.Printf("[TOOLS] Request has empty tool selection - will use ALL tools from selected servers")
			}
		} else if len(selectedTools) == 0 {
			log.Printf("[TOOLS] No tool selection specified - will use ALL tools from selected servers")
		}

		// CRITICAL: Always respect request value when explicitly set (frontend always sends explicit value)
		// The frontend explicitly sends use_code_execution_mode (true or false), so we should use it
		// This ensures that when user selects "simple" mode without preset, the request value is respected
		useCodeExecutionMode = req.UseCodeExecutionMode
		if useCodeExecutionMode {
			log.Printf("[CODE_EXECUTION] Code execution mode enabled from request")
		} else {
			if presetSetCodeExecutionMode {
				log.Printf("[CODE_EXECUTION] Code execution mode disabled by request (overriding preset value)")
			} else {
				log.Printf("[CODE_EXECUTION] Code execution mode disabled (default)")
			}
		}

		// Auto-enable code execution mode for claude-code provider.
		// Claude Code accesses MCP tools via the HTTP bridge (mcpbridge stdio binary),
		// which requires code execution mode to expose per-tool API endpoints.
		if req.Provider == "claude-code" && !useCodeExecutionMode {
			useCodeExecutionMode = true
			log.Printf("[CLAUDE CODE] Auto-enabled code execution mode for MCP tool access via bridge")
		}

		// Auto-enable code execution mode for gemini-cli provider.
		// Gemini CLI accesses MCP tools via the pre-configured bridge,
		// which requires code execution mode to expose per-tool API endpoints.
		if req.Provider == "gemini-cli" && !useCodeExecutionMode {
			useCodeExecutionMode = true
			log.Printf("[GEMINI CLI] Auto-enabled code execution mode for MCP tool access via bridge")
		}

		// CRITICAL: Always respect request value for tool search mode when explicitly set
		// The frontend explicitly sends use_tool_search_mode (true or false), so we should use it
		useToolSearchMode = req.UseToolSearchMode
		if useToolSearchMode {
			log.Printf("[TOOL_SEARCH] Tool search mode enabled from request")
		}

		// In plan delegation mode, the orchestrator should NEVER use code execution mode
		// UNLESS the provider is claude-code (which requires it for MCP bridge tool access).
		// The orchestrator only researches, plans, and delegates — it should not write/execute code itself.
		// Sub-agents get their mode from the explicit tool_mode parameter on the delegate call.
		// However, tool search mode is auto-enabled when many MCP servers are selected (>3)
		// so the orchestrator can efficiently discover tools for research.
		if req.DelegationMode == "plan" {
			if useCodeExecutionMode && req.Provider != "claude-code" && req.Provider != "gemini-cli" {
				log.Printf("[CODE_EXECUTION] Disabling code execution mode for orchestrator in plan delegation mode")
				useCodeExecutionMode = false
			}
			// Count real servers (exclude "all" and "NO_SERVERS" sentinels)
			realServerCount := 0
			for _, s := range selectedServers {
				if s != "all" && s != mcpclient.NoServers {
					realServerCount++
				}
			}
			if realServerCount > 3 && !useToolSearchMode {
				log.Printf("[TOOL_SEARCH] Auto-enabling tool search mode for orchestrator — %d MCP servers selected (>3)", realServerCount)
				useToolSearchMode = true
			} else if useToolSearchMode && realServerCount <= 3 {
				log.Printf("[TOOL_SEARCH] Disabling tool search mode for orchestrator in plan delegation mode (%d servers)", realServerCount)
				useToolSearchMode = false
			}
		}

		// In plan delegation mode, orchestrator always uses the high reasoning tier model
		if req.DelegationMode == "plan" {
			tierConfig := resolveDelegationTierConfig(req.DelegationTierConfig)
			if tierConfig != nil && tierConfig.High != nil &&
				tierConfig.High.Provider != "" && tierConfig.High.ModelID != "" {
				finalProvider = tierConfig.High.Provider
				finalModelID = tierConfig.High.ModelID
				log.Printf("[DELEGATION] Orchestrator using high tier model: %s/%s",
					finalProvider, finalModelID)
			}
		}

		// Create new agent with streamCtx instead of r.Context()
		log.Printf("[AGENT CONFIG DEBUG] Creating agent with ServerName: %s, UseCodeExecutionMode: %v, UseToolSearchMode: %v", serverList, useCodeExecutionMode, useToolSearchMode)
		agentConfig := agent.LLMAgentConfig{
			Name:               sessionID,
			ServerName:         serverList, // Use full server list, not just first one
			ConfigPath:         api.mcpConfigPath,
			Provider:           llm.Provider(finalProvider),
			ModelID:            finalModelID,
			Temperature:        req.Temperature,
			MaxTurns:           req.MaxTurns,
			ToolChoice:         "auto",
			StreamingChunkSize: 50,
			Timeout:            2 * time.Minute,
			ToolTimeout: func() time.Duration {
				if envVal := os.Getenv("TOOL_EXECUTION_TIMEOUT"); envVal != "" {
					if timeout, err := time.ParseDuration(envVal); err == nil && timeout > 0 {
						return timeout
					}
				}
				return 5 * time.Minute
			}(),
			SelectedTools: selectedTools, // NEW: Pass selected tools

			// Smart routing disabled - always use all available tools
			EnableSmartRouting: false,

			// Detailed LLM configuration from frontend (unified fallback structure)
			Fallbacks: fallbacks,
			// Code execution mode: When enabled, only virtual tools are added to LLM
			// MCP tools are accessed via generated Go code using discover_code_files and write_code
			UseCodeExecutionMode: useCodeExecutionMode,
			// Tool search mode: When enabled, LLM discovers tools on-demand via search_tools
			UseToolSearchMode: useToolSearchMode,
			// Pre-discovered tools: all virtual/custom tools stay visible in tool search mode
			// Only MCP server tools require discovery via search_tools
			PreDiscoveredTools: func() []string {
				if !useToolSearchMode {
					return nil
				}
				// Collect all virtual tool names so they're never hidden behind search
				preDiscovered := collectVirtualToolNames(
					virtualtools.CreateWorkspaceAdvancedTools(),
					virtualtools.CreateHumanTools(),
				)
				if req.DelegationMode == "plan" {
					// In plan mode, only async tools are registered
					preDiscovered = append(preDiscovered, "delegate", "query_agent", "terminate_agent", "list_agents")
				} else if req.DelegationMode == "spawn" {
					preDiscovered = append(preDiscovered, "delegate")
				}
				if req.EnableBrowserAccess != nil && *req.EnableBrowserAccess {
					preDiscovered = append(preDiscovered, collectVirtualToolNames(virtualtools.CreateWorkspaceBrowserTools())...)
				}
				return preDiscovered
			}(),
			// Convert API keys from request to LLM format (respecting locked providers)
			APIKeys: func() *llm.ProviderAPIKeys {
				// 1. Start with keys from request (if available)
				llmKeys := &llm.ProviderAPIKeys{}
				if req.LLMConfig != nil && req.LLMConfig.APIKeys != nil {
					llmKeys.OpenRouter = req.LLMConfig.APIKeys.OpenRouter
					llmKeys.OpenAI = req.LLMConfig.APIKeys.OpenAI
					llmKeys.Anthropic = req.LLMConfig.APIKeys.Anthropic
					llmKeys.Vertex = req.LLMConfig.APIKeys.Vertex
					llmKeys.GeminiCLI = req.LLMConfig.APIKeys.GeminiCLI

					if req.LLMConfig.APIKeys.Bedrock != nil {
						llmKeys.Bedrock = &llm.BedrockConfig{
							Region: req.LLMConfig.APIKeys.Bedrock.Region,
						}
					}
					if req.LLMConfig.APIKeys.Azure != nil {
						llmKeys.Azure = &llm.AzureAPIConfig{
							Endpoint:   req.LLMConfig.APIKeys.Azure.Endpoint,
							APIKey:     req.LLMConfig.APIKeys.Azure.APIKey,
							APIVersion: req.LLMConfig.APIKeys.Azure.APIVersion,
							Region:     req.LLMConfig.APIKeys.Azure.Region,
						}
					}
				}

				// 2. Get keys from environment
				envKeys := buildProviderAPIKeysFromEnv()
				globalLocked := isGlobalLLMConfigLocked()

				// 3. Override if provider is locked or global lock is on
				if globalLocked || isProviderLocked("openrouter") {
					llmKeys.OpenRouter = envKeys.OpenRouter
				}
				if globalLocked || isProviderLocked("openai") {
					llmKeys.OpenAI = envKeys.OpenAI
				}
				if globalLocked || isProviderLocked("anthropic") {
					llmKeys.Anthropic = envKeys.Anthropic
				}
				if globalLocked || isProviderLocked("vertex") {
					llmKeys.Vertex = envKeys.Vertex
				}
				if globalLocked || isProviderLocked("bedrock") {
					llmKeys.Bedrock = envKeys.Bedrock
				}
				if globalLocked || isProviderLocked("azure") {
					llmKeys.Azure = envKeys.Azure
				}
				if globalLocked || isProviderLocked("gemini-cli") {
					llmKeys.GeminiCLI = envKeys.GeminiCLI
				}

				return llmKeys
			}(),
			// Context summarization configuration
			// Priority: Request > Environment Variable > Default (matches orchestrator defaults)
			EnableContextSummarization: func() bool {
				// Priority: Request > Preset > Environment Variable > Default
				// If explicitly set in request, use that value
				if req.EnableContextSummarization != nil {
					return *req.EnableContextSummarization
				}
				// Check preset LLM config
				if presetLLMConfig != nil && presetLLMConfig.EnableContextSummarization != nil {
					return *presetLLMConfig.EnableContextSummarization
				}
				// Check environment variable - default to enabled (true), can be disabled via "false"
				if envVal := os.Getenv("ENABLE_CONTEXT_SUMMARIZATION"); envVal == "false" {
					return false
				}
				return true // Default to enabled (matches orchestrator)
			}(),
			SummarizeOnTokenThreshold: func() bool {
				// If explicitly set in request, use that value
				if req.SummarizeOnTokenThreshold != nil {
					return *req.SummarizeOnTokenThreshold
				}
				// Check environment variable - default to enabled (true), can be disabled via "false"
				if envVal := os.Getenv("SUMMARIZE_ON_TOKEN_THRESHOLD"); envVal == "false" {
					return false
				}
				return true // Default to enabled (matches orchestrator)
			}(),
			TokenThresholdPercent: func() float64 {
				// Request takes highest priority
				if req.TokenThresholdPercent > 0 {
					return req.TokenThresholdPercent
				}
				// Check environment variable
				if envVal := os.Getenv("TOKEN_THRESHOLD_PERCENT"); envVal != "" {
					if threshold, err := strconv.ParseFloat(envVal, 64); err == nil && threshold > 0 && threshold <= 1.0 {
						return threshold
					}
				}
				// Default to 80% (0.8) - matches orchestrator
				return 0.8
			}(),
			SummarizeOnFixedTokenThreshold: func() bool {
				// If explicitly set in request, use that value
				if req.SummarizeOnFixedTokenThreshold != nil {
					return *req.SummarizeOnFixedTokenThreshold
				}
				// Check environment variable - default to enabled (true), can be disabled via "false"
				if envVal := os.Getenv("SUMMARIZE_ON_FIXED_TOKEN_THRESHOLD"); envVal == "false" {
					return false
				}
				return true // Default to enabled (matches orchestrator)
			}(),
			FixedTokenThreshold: func() int {
				// Request takes highest priority
				if req.FixedTokenThreshold > 0 {
					return req.FixedTokenThreshold
				}
				// Check environment variable
				if envVal := os.Getenv("FIXED_TOKEN_THRESHOLD"); envVal != "" {
					if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
						return threshold
					}
				}
				return 200000 // Default to 200k tokens (matches orchestrator)
			}(),
			SummaryKeepLastMessages: func() int {
				if req.SummaryKeepLastMessages > 0 {
					return req.SummaryKeepLastMessages
				}
				// Check environment variable
				if envVal := os.Getenv("SUMMARY_KEEP_LAST_MESSAGES"); envVal != "" {
					if keepLast, err := strconv.Atoi(envVal); err == nil && keepLast > 0 {
						return keepLast
					}
				}
				return 4 // Default to 4 messages (matches orchestrator)
			}(),
			// Context editing configuration
			// Priority: Request > Environment Variable > Default
			EnableContextEditing: func() bool {
				// Priority: Request > Preset > Environment Variable > Default
				// If explicitly set in request, use that value
				if req.EnableContextEditing != nil {
					return *req.EnableContextEditing
				}
				// Check preset LLM config
				if presetLLMConfig != nil && presetLLMConfig.EnableContextEditing != nil {
					return *presetLLMConfig.EnableContextEditing
				}
				// Check environment variable
				if envVal := os.Getenv("ENABLE_CONTEXT_EDITING"); envVal == "true" {
					return true
				}
				// Default to disabled (false), can be enabled via ENABLE_CONTEXT_EDITING=true
				return os.Getenv("ENABLE_CONTEXT_EDITING") == "true"
			}(),
			ContextEditingThreshold: func() int {
				// Request takes highest priority
				if req.ContextEditingThreshold > 0 {
					return req.ContextEditingThreshold
				}
				// Check environment variable
				if envVal := os.Getenv("CONTEXT_EDITING_THRESHOLD"); envVal != "" {
					if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
						return threshold
					}
				}
				// Default to 0 (use default: 100)
				return 0
			}(),
			ContextEditingTurnThreshold: func() int {
				// Request takes highest priority
				if req.ContextEditingTurnThreshold > 0 {
					return req.ContextEditingTurnThreshold
				}
				// Check environment variable
				if envVal := os.Getenv("CONTEXT_EDITING_TURN_THRESHOLD"); envVal != "" {
					if turnThreshold, err := strconv.Atoi(envVal); err == nil && turnThreshold > 0 {
						return turnThreshold
					}
				}
				// Default to 0 (use default: 5)
				return 0
			}(),
			// Context offloading: large output threshold
			// Tool outputs larger than this threshold (in tokens) are offloaded to filesystem
			LargeOutputThreshold: func() int {
				// Check environment variable
				if envVal := os.Getenv("LARGE_OUTPUT_THRESHOLD"); envVal != "" {
					if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
						return threshold
					}
				}
				// Default to 0 (use library default: 10000 tokens)
				return 0
			}(),
			// Parallel tool execution: enabled by default, can be disabled via ENABLE_PARALLEL_TOOL_EXECUTION=false
			EnableParallelToolExecution: func() bool {
				if envVal := os.Getenv("ENABLE_PARALLEL_TOOL_EXECUTION"); envVal == "false" {
					return false
				}
				return true // Default to enabled
			}(),
			// MCP session ID for connection reuse (e.g., Playwright browser sharing)
			// Use the chat session ID so all agents in the same session share MCP connections
			SessionID: sessionID,
			// User ID for per-user OAuth token isolation
			// This ensures MCP servers with OAuth use user-specific token files
			UserID: currentUserID,
		}

		// Set agent mode based on request
		switch req.AgentMode {
		case "simple":
			agentConfig.AgentMode = mcpagent.SimpleAgent
		case "orchestrator":
			// For orchestrator mode, we'll handle it differently
			agentConfig.AgentMode = mcpagent.SimpleAgent // Use Simple as base for orchestrator
		case "workflow":
			// For workflow mode, we'll handle it differently
			agentConfig.AgentMode = mcpagent.SimpleAgent // Use Simple as base for workflow
		default:
			agentConfig.AgentMode = mcpagent.SimpleAgent // Default to Simple mode
		}
		log.Printf("[AGENT DEBUG] Creating agent with mode: %s, servers: %s", agentConfig.AgentMode, serverList)
		log.Printf("[SMART ROUTING DEBUG] Smart routing enabled - MaxTools: %d, MaxServers: %d (using defaults for temperature/tokens)",
			agentConfig.SmartRoutingMaxTools, agentConfig.SmartRoutingMaxServers)
		log.Printf("[LATENCY_DEBUG] T+%dms | Agent config built, creating agent wrapper | provider=%s model=%s", time.Since(startTime).Milliseconds(), finalProvider, finalModelID)
		// Create LLM agent wrapper with trace using streamCtx
		llmAgent, err := agent.NewLLMAgentWrapperWithTrace(streamCtx, agentConfig, tracer, traceID, api.logger)
		if err != nil {
			log.Printf("[AGENT DEBUG] Failed to create LLM agent wrapper: %v", err)
			sendError(fmt.Sprintf("Failed to create agent: %v", err), true)
			return
		}
		log.Printf("[LATENCY_DEBUG] T+%dms | Agent wrapper created", time.Since(startTime).Milliseconds())

		// Add workspace tools to simple agents (chat mode)
		// This matches how workspace tools are registered in workflow/orchestrator agents
		// This ensures custom tools are available and code generation is triggered
		// Only register workspace tools if workspace access is enabled
		// Note: Frontend always sends enable_workspace_access for chat mode (true/false)
		// Chat mode is detected by: "simple", "" (empty/default), or "chat" agent mode
		// Workflow/orchestrator modes handle workspace tools differently, so exclude them
		isChatMode := req.AgentMode == "simple" || req.AgentMode == "" || req.AgentMode == "chat"

		// Check if skill-creator is in selected skills (mode-agnostic)
		hasSkillCreator := false
		for _, s := range req.SelectedSkills {
			if s == "skill-creator" {
				hasSkillCreator = true
				break
			}
		}

		// Check if subagent-creator is in selected skills
		hasSubAgentCreator := false
		for _, s := range req.SelectedSkills {
			if s == "subagent-creator" || s == "custom/subagent-creator" {
				hasSubAgentCreator = true
				break
			}
		}

		// When skill-creator is selected, ensure it's installed
		if hasSkillCreator {
			workspaceAPIURL := api.GetAPIURL()
			_, err := skills.GetSkill(workspaceAPIURL, "skill-creator")
			if err != nil {
				log.Printf("[SKILL CREATOR] skill-creator not found, attempting import from GitHub...")
				_, err := skills.ImportGitHubSkill(workspaceAPIURL, "https://github.com/anthropics/skills/tree/main/skills/skill-creator", "")
				if err != nil {
					log.Printf("[SKILL CREATOR] Warning: Failed to import skill-creator: %v", err)
				} else {
					log.Printf("[SKILL CREATOR] Successfully imported skill-creator")
				}
			}
		}

		var memoryBgDelegate virtualtools.BackgroundDelegateFunc
		if isChatMode && llmAgent.GetUnderlyingAgent() != nil {
			// Check if workspace access is enabled
			// Default to true for backward compatibility with legacy requests
			// nil = inherit default (true), non-nil = explicit override
			enableWorkspaceAccess := true // Default to enabled for backward compatibility
			if req.EnableWorkspaceAccess != nil {
				enableWorkspaceAccess = *req.EnableWorkspaceAccess
			}
			// Automatically enable workspace access when skills are selected
			// Skills need workspace access to read files and context
			if len(req.SelectedSkills) > 0 {
				enableWorkspaceAccess = true
				log.Printf("[SKILLS] Automatically enabling workspace access (skills selected: %v)", req.SelectedSkills)
			}

			// Auto-enable workspace access for plan delegation mode
			// Plan mode requires workspace tools for shell commands with proper FolderGuard
			if req.DelegationMode == "plan" {
				enableWorkspaceAccess = true
			}

			// Handle browser access: when enabled, auto-enable workspace and add agent-browser skill
			enableBrowserAccess := false
			if req.EnableBrowserAccess != nil && *req.EnableBrowserAccess {
				enableBrowserAccess = true
				enableWorkspaceAccess = true // Browser tool lives in workspace category
				// Auto-add agent-browser skill if not already selected
				hasAgentBrowserSkill := false
				for _, skill := range req.SelectedSkills {
					if skill == "agent-browser" {
						hasAgentBrowserSkill = true
						break
					}
				}
				if !hasAgentBrowserSkill {
					req.SelectedSkills = append(req.SelectedSkills, "agent-browser")
				}
				log.Printf("[BROWSER] Auto-adding agent-browser skill and tool (enable_browser_access: true)")
			}

			if enableWorkspaceAccess {
				// Create Chats/ folder if it doesn't exist
				if err := skills.CreateFolder("Chats"); err != nil {
					log.Printf("[WORKSPACE] Warning: Could not create Chats/ folder: %v", err)
				}

				// Create skills/ folder if it doesn't exist
				if err := skills.CreateFolder("skills"); err != nil {
					log.Printf("[WORKSPACE] Warning: Could not create skills/ folder: %v", err)
				} else {
					// Create skills/custom/ folder for Skill Builder
					if err := skills.CreateFolder("skills/custom"); err != nil {
						log.Printf("[WORKSPACE] Warning: Could not create skills/custom/ folder: %v", err)
					} else {
						log.Printf("[WORKSPACE] Ensured skills/ and skills/custom/ folders exist")
					}
				}

				// Create subagents/ folder if it doesn't exist
				if err := skills.CreateFolder("subagents"); err != nil {
					log.Printf("[WORKSPACE] Warning: Could not create subagents/ folder: %v", err)
				} else {
					if err := skills.CreateFolder("subagents/custom"); err != nil {
						log.Printf("[WORKSPACE] Warning: Could not create subagents/custom/ folder: %v", err)
					}
				}

				// Chat mode: advanced workspace tools (shell, image, web fetch, PDF, diff_patch)
				// Basic tools (list/read/write/search) and git tools are not needed — shell is sufficient
				// These tools will be RESTRICTED to Chats/ folder via wrapExecutorsWithChatModeFolderGuard
				workspaceTools := virtualtools.CreateWorkspaceAdvancedTools()
				workspaceExecutors, _ := virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSession(currentUserID, sessionID)
				log.Printf("[USER_ID_DEBUGGING] Main agent workspace executors: created with explicit userID=%q sessionID=%q", currentUserID, sessionID)
				_, _, toolCategories := createCustomTools(false) // Get toolCategories map (advanced only)

				// Extract @context file paths for additional write access
				fileContextFolders := extractFileContextWriteFolders(req.Query)
				if len(fileContextFolders) > 0 {
					log.Printf("[FILE CONTEXT] Extracted write paths from @context: %v", fileContextFolders)
				}

				// Apply folder guard to restrict writes based on mode
				// Multi-agent (plan) mode: primary write folder is Plans/, Chats/ also writable
				// Chat mode: writes go to Chats/
				if req.DelegationMode == "plan" {
					additionalFolders := []string{"Chats/"}
					if hasSkillCreator {
						additionalFolders = append(additionalFolders, "skills/custom/")
					}
					if hasSubAgentCreator {
						additionalFolders = append(additionalFolders, "subagents/custom/")
					}
					additionalFolders = append(additionalFolders, fileContextFolders...)
					workspaceExecutors = wrapExecutorsWithPlanFolderGuard(workspaceExecutors, "Plans", additionalFolders...)
					log.Printf("[MULTI-AGENT FOLDER GUARD] Applied Plans/ folder restriction (additional: %v)", additionalFolders)
				} else {
					extraFolders := []string{}
					if hasSkillCreator {
						extraFolders = append(extraFolders, "skills/custom/")
					}
					if hasSubAgentCreator {
						extraFolders = append(extraFolders, "subagents/custom/")
					}
					extraFolders = append(extraFolders, fileContextFolders...)
					workspaceExecutors = wrapExecutorsWithChatModeFolderGuard(workspaceExecutors, extraFolders...)
					log.Printf("[CHAT MODE FOLDER GUARD] Applied Chats/ + %v folder restriction", extraFolders)
				}

				// Apply skill folder guard if skills are selected (read-only access to selected skills only)
				if len(req.SelectedSkills) > 0 {
					log.Printf("[SKILL FOLDER GUARD] Applied skill folder restriction - only selected skills accessible: %v", req.SelectedSkills)
				}

				log.Printf("[WORKSPACE TOOLS] Registering %d workspace advanced tools for chat mode (enable_workspace_access: %v)", len(workspaceTools), enableWorkspaceAccess)

				underlyingAgent := llmAgent.GetUnderlyingAgent()
				for _, tool := range workspaceTools {
					if tool.Function == nil {
						log.Printf("[WORKSPACE TOOLS] Warning: Skipping tool with nil Function")
						continue
					}
					toolName := tool.Function.Name
					if executor, exists := workspaceExecutors[toolName]; exists {
						// Enhance tool description based on mode
						var enhancedDescription string
						if req.DelegationMode == "plan" {
							enhancedDescription = enhanceToolDescriptionForPlanMode(toolName, tool.Function.Description)
						} else {
							enhancedDescription = enhanceToolDescriptionForChatMode(toolName, tool.Function.Description)
						}

						// Convert Parameters to map[string]interface{}
						var params map[string]interface{}
						if tool.Function.Parameters != nil {
							paramsBytes, err := json.Marshal(tool.Function.Parameters)
							if err == nil {
								json.Unmarshal(paramsBytes, &params)
							}
						}
						if params == nil {
							log.Printf("[WORKSPACE TOOLS] Warning: Failed to convert parameters for tool %s", toolName)
							continue
						}

						// Get tool category from the category map - REQUIRED
						toolCategory := toolCategories[toolName]
						if toolCategory == "" {
							log.Printf("[WORKSPACE TOOLS ERROR] Tool %s not found in toolCategories map - category is REQUIRED!", toolName)
							sendError(fmt.Sprintf("Failed to register workspace tool %s: category is REQUIRED", toolName), true)
							return
						}

						// Executor is already the correct type (func(ctx, args) (string, error))
						// No type assertion needed unlike workflow where executors are map[string]interface{}
						if err := underlyingAgent.RegisterCustomTool(
							toolName,
							enhancedDescription,
							params,
							executor,
							toolCategory,
						); err != nil {
							log.Printf("[WORKSPACE TOOLS ERROR] Failed to register tool %s: %v", toolName, err)
							sendError(fmt.Sprintf("Failed to register workspace tool %s: %v", toolName, err), true)
							return
						}
						log.Printf("[WORKSPACE TOOLS] Registered workspace tool: %s (category: %s)", toolName, toolCategory)
					}
				}
				log.Printf("[WORKSPACE TOOLS] Successfully registered %d workspace advanced tools for chat mode", len(workspaceTools))

				// Register browser tool if browser access is enabled
				if enableBrowserAccess {
					browserTools := virtualtools.CreateWorkspaceBrowserTools()
					browserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutors(getCdpPort(req))
					browserCategory := virtualtools.GetWorkspaceBrowserToolCategory()

					// Apply same folder guard as workspace tools (reuse fileContextFolders from above)
					if req.DelegationMode == "plan" {
						additionalFolders := []string{"Chats/"}
						if hasSkillCreator {
							additionalFolders = append(additionalFolders, "skills/custom/")
						}
						additionalFolders = append(additionalFolders, fileContextFolders...)
						browserExecutors = wrapExecutorsWithPlanFolderGuard(browserExecutors, "Plans", additionalFolders...)
					} else {
						browserExtraFolders := []string{}
						if hasSkillCreator {
							browserExtraFolders = append(browserExtraFolders, "skills/custom/")
						}
						browserExtraFolders = append(browserExtraFolders, fileContextFolders...)
						browserExecutors = wrapExecutorsWithChatModeFolderGuard(browserExecutors, browserExtraFolders...)
					}
					log.Printf("[BROWSER TOOLS] Applied folder guard to browser tools (delegation_mode: %s)", req.DelegationMode)

					for _, tool := range browserTools {
						if tool.Function == nil {
							continue
						}
						toolName := tool.Function.Name
						if executor, exists := browserExecutors[toolName]; exists {
							var params map[string]interface{}
							if tool.Function.Parameters != nil {
								paramsBytes, err := json.Marshal(tool.Function.Parameters)
								if err == nil {
									json.Unmarshal(paramsBytes, &params)
								}
							}
							if params == nil {
								log.Printf("[BROWSER TOOLS] Warning: Failed to convert parameters for tool %s", toolName)
								continue
							}

							if err := underlyingAgent.RegisterCustomTool(
								toolName,
								tool.Function.Description,
								params,
								executor,
								browserCategory,
							); err != nil {
								log.Printf("[BROWSER TOOLS ERROR] Failed to register tool %s: %v", toolName, err)
								continue
							}
							log.Printf("[BROWSER TOOLS] Registered browser tool: %s (category: %s)", toolName, browserCategory)
						}
					}
					log.Printf("[BROWSER TOOLS] Successfully registered %d browser tools for chat mode", len(browserTools))
				}

			} else {
				log.Printf("[WORKSPACE TOOLS] Skipping workspace tools registration (enable_workspace_access: false)")
			}

			// Register delegation tool if delegation mode is enabled
			// Note: This is outside the workspace access block because delegation should work regardless of workspace access
			delegationMode := req.DelegationMode // "spawn", "plan", or ""

			if delegationMode == "spawn" || delegationMode == "plan" {
				// Build delegation tier config early so we can pass it to tool creation (for dynamic enum)
				tierConfig := resolveDelegationTierConfig(req.DelegationTierConfig)
				delegationTools := virtualtools.CreateDelegationTools(tierConfig, delegationMode == "plan")
				delegationExecutors := virtualtools.CreateDelegationToolExecutors()
				delegationCategory := virtualtools.GetDelegationToolCategory()

				// Get underlying agent for tool registration
				delegationAgent := llmAgent.GetUnderlyingAgent()
				if delegationAgent == nil {
					log.Printf("[DELEGATION TOOLS ERROR] Cannot register delegation tools - underlying agent is nil")
				} else {
					// Create the delegation execution function that will spawn sub-agents
					// This function is injected into the context for the delegate tool to use
					executeDelegatedTask := func(subCtx context.Context, instruction string) (string, error) {
						return api.executeDelegatedTask(subCtx, req, sessionID, instruction)
					}

					// Create workspace client for plan file I/O (Plans/)
					// Include user ID for per-user folder routing (Plans/ is a per-user folder)
					planWorkspaceClient := workspace.NewClient(
						getWorkspaceAPIURL(),
						workspace.WithFolderGuard(&workspace.FolderGuardConfig{
							Enabled:      true,
							WritePaths:   []string{"Plans"},
							BlockedPaths: []string{"_users"},
						}),
						workspace.WithUserID(currentUserID),
					)

					// Build capabilities context for the planner
					caps := buildCapabilitiesContext(req)

					// Get or create session-level plan state (replaces per-message PlanTracker)
					planState := api.getOrCreatePlanSessionState(sessionID)

					// Create background delegate function for plan mode (async delegation)
					var bgDelegateFunc virtualtools.BackgroundDelegateFunc
					var bgQuerier virtualtools.BGAgentQuerier
					if delegationMode == "plan" {
						bgDelegateFunc = func(bgCtx context.Context, name, instruction string) (string, error) {
							return api.executeBackgroundDelegatedTask(bgCtx, req, sessionID, name, instruction)
						}
						memoryBgDelegate = bgDelegateFunc
						bgQuerier = &bgAgentQuerierImpl{registry: api.bgAgentRegistry}
					}

					// Tools allowed in each mode
					planModeTools := map[string]bool{
						"create_delegation_plan": true,
						"confirm_plan_execution": true,
						"delegate":               true,
						"query_agent":            true,
						"terminate_agent":        true,
						"list_agents":            true,
					}

					for _, tool := range delegationTools {
						if tool.Function == nil {
							continue
						}
						toolName := tool.Function.Name

						// In 'spawn' mode, only register 'delegate'
						if delegationMode == "spawn" && toolName != "delegate" {
							continue
						}
						// In 'plan' mode, only register async delegation tools
						if delegationMode == "plan" && !planModeTools[toolName] {
							continue
						}

						if executor, exists := delegationExecutors[toolName]; exists {
							var params map[string]interface{}
							if tool.Function.Parameters != nil {
								paramsBytes, err := json.Marshal(tool.Function.Parameters)
								if err == nil {
									json.Unmarshal(paramsBytes, &params)
								}
							}
							if params == nil {
								log.Printf("[DELEGATION TOOLS] Warning: Failed to convert parameters for tool %s", toolName)
								continue
							}

							// Capture executor for closure
							exec := executor

							// Wrap the executor to inject delegation function, workspace client, tier config, capabilities, and plan tracker
							wrappedExecutor := func(ctx context.Context, args map[string]interface{}) (string, error) {
								ctx = context.WithValue(ctx, virtualtools.ExecuteDelegatedTaskKey, virtualtools.ExecuteDelegatedTaskFunc(executeDelegatedTask))
								ctx = context.WithValue(ctx, virtualtools.WorkspaceClientKey, planWorkspaceClient)
								ctx = context.WithValue(ctx, virtualtools.PlanEventEmitterKey, &planEventEmitter{
									eventStore: api.eventStore,
									sessionID:  sessionID,
									chatDB:     api.chatDB,
								})
								ctx = context.WithValue(ctx, virtualtools.PlanSessionStateKey, planState)
								ctx = context.WithValue(ctx, virtualtools.SessionEventEmitterKey, &sessionEventEmitter{
									eventStore: api.eventStore,
									sessionID:  sessionID,
									chatDB:     api.chatDB,
								})
								if tierConfig != nil {
									ctx = context.WithValue(ctx, virtualtools.DelegationTierConfigKey, tierConfig)
								}
								if caps != nil {
									ctx = context.WithValue(ctx, virtualtools.CapabilitiesContextKey, caps)
								}
								// Inject background delegation and agent querier for plan mode
								if bgDelegateFunc != nil {
									ctx = context.WithValue(ctx, virtualtools.BackgroundDelegateKey, bgDelegateFunc)
								}
								if bgQuerier != nil {
									ctx = context.WithValue(ctx, virtualtools.BGAgentRegistryKey, bgQuerier)
									ctx = context.WithValue(ctx, virtualtools.BGAgentSessionIDKey, sessionID)
								}
								return exec(ctx, args)
							}

							if err := delegationAgent.RegisterCustomToolWithTimeout(
								toolName,
								tool.Function.Description,
								params,
								wrappedExecutor,
								0, // No timeout — delegation tools run indefinitely (controlled by parent context)
								delegationCategory,
							); err != nil {
								log.Printf("[DELEGATION TOOLS ERROR] Failed to register tool %s: %v", toolName, err)
								continue
							}
							log.Printf("[DELEGATION TOOLS] Registered delegation tool: %s (category: %s)", toolName, delegationCategory)
						}
					}
					log.Printf("[DELEGATION TOOLS] Successfully registered %d delegation tools for chat mode", len(delegationTools))
				}
			}
		}

		// Add custom agent instructions based on agent mode
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			// Create custom tools for chat mode (workspace_advanced only: shell, image, web fetch, PDF)
			allTools, allExecutors, toolCategories := createCustomTools(false) // Chat mode: workspace_advanced only

			// In plan delegation mode (multi-agent), also include human tools (human_feedback)
			// createCustomTools(false) only returns workspace_advanced; human tools need to be added explicitly
			if req.DelegationMode == "plan" {
				humanCategory := virtualtools.GetHumanToolCategory()
				for _, tool := range virtualtools.CreateHumanTools() {
					allTools = append(allTools, tool)
					if tool.Function != nil {
						toolCategories[tool.Function.Name] = humanCategory
					}
				}
				for name, executor := range virtualtools.CreateHumanToolExecutors() {
					allExecutors[name] = executor
				}
			}

			// Register each custom tool with the agent
			// This will trigger code generation and update the registry
			// Note: Workspace tools are already registered above, skip them in allTools
			registeredCount := 0
			for _, tool := range allTools {
				if tool.Function != nil {
					toolName := tool.Function.Name

					// Skip workspace tools - already registered above
					if toolCategories[toolName] == virtualtools.GetWorkspaceToolCategory() {
						continue
					}

					// Human tools: skip in regular chat mode, allow in plan delegation mode
					if toolCategories[toolName] == virtualtools.GetHumanToolCategory() {
						if req.DelegationMode != "plan" {
							continue
						}
					}

					if executor, exists := allExecutors[toolName]; exists {
						// Convert executor to the expected function signature
						if execFunc, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
							// Convert Parameters to map[string]interface{} by marshaling/unmarshaling
							var params map[string]interface{}
							if tool.Function.Parameters != nil {
								paramsBytes, err := json.Marshal(tool.Function.Parameters)
								if err == nil {
									json.Unmarshal(paramsBytes, &params)
								}
							}
							if params == nil {
								params = make(map[string]interface{})
							}

							// Get tool category from the category map - REQUIRED
							toolCategory := toolCategories[toolName]
							if toolCategory == "" {
								log.Printf("[CUSTOM TOOLS ERROR] Tool %s not found in toolCategories map - category is REQUIRED!", toolName)
								// Continue to next tool instead of failing entire request
								continue
							}

							// Wrap human tools to inject SessionEventEmitter for blocking events (feedback/questions)
							registrationFunc := execFunc
							if toolCategory == virtualtools.GetHumanToolCategory() {
								originalExec := execFunc
								registrationFunc = func(ctx context.Context, args map[string]interface{}) (string, error) {
									ctx = context.WithValue(ctx, virtualtools.SessionEventEmitterKey, &sessionEventEmitter{
										eventStore: api.eventStore,
										sessionID:  sessionID,
										chatDB:     api.chatDB,
									})
									return originalExec(ctx, args)
								}
							}

							// Register the tool - this triggers code generation
							if err := underlyingAgent.RegisterCustomTool(
								toolName,
								tool.Function.Description,
								params,
								registrationFunc,
								toolCategory,
							); err != nil {
								log.Printf("[CUSTOM TOOLS ERROR] Failed to register tool %s: %v", toolName, err)
								// Continue to next tool instead of failing entire request
								continue
							}
							registeredCount++
							log.Printf("[CUSTOM TOOLS] Registered custom tool: %s (category: %s)", toolName, toolCategory)
						}
					}
				}
			}
			log.Printf("[CUSTOM TOOLS] Registered %d custom tools with agent", registeredCount)

			// Register memory tools in all chat modes.
			// In plan mode, memory tools can spawn background memory agents.
			memoryTools := virtualtools.CreateMemoryTools()
			memoryExecutors := virtualtools.CreateMemoryToolExecutors()
			memoryCategory := virtualtools.GetDelegationToolCategory()
			memoryWorkspaceClient := workspace.NewClient(
				getWorkspaceAPIURL(),
				workspace.WithFolderGuard(&workspace.FolderGuardConfig{
					Enabled:      true,
					WritePaths:   []string{"Plans"},
					BlockedPaths: []string{"_users"},
				}),
				workspace.WithUserID(currentUserID),
			)

			registeredMemoryCount := 0
			for _, tool := range memoryTools {
				if tool.Function == nil {
					continue
				}
				toolName := tool.Function.Name
				exec, exists := memoryExecutors[toolName]
				if !exists {
					continue
				}

				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
				}
				if params == nil {
					params = make(map[string]interface{})
				}

				wrappedMemoryExecutor := func(ctx context.Context, args map[string]interface{}) (string, error) {
					ctx = context.WithValue(ctx, virtualtools.WorkspaceClientKey, memoryWorkspaceClient)
					if memoryBgDelegate != nil {
						ctx = context.WithValue(ctx, virtualtools.BackgroundDelegateKey, memoryBgDelegate)
					}
					return exec(ctx, args)
				}

				if err := underlyingAgent.RegisterCustomToolWithTimeout(
					toolName,
					tool.Function.Description,
					params,
					wrappedMemoryExecutor,
					0, // Memory operations may involve background delegation; do not enforce timeout.
					memoryCategory,
				); err != nil {
					log.Printf("[MEMORY TOOLS ERROR] Failed to register tool %s: %v", toolName, err)
					continue
				}
				registeredMemoryCount++
				log.Printf("[MEMORY TOOLS] Registered memory tool: %s (category: %s)", toolName, memoryCategory)
			}
			log.Printf("[MEMORY TOOLS] Registered %d memory tools with agent", registeredMemoryCount)

			// Update code execution registry to rebuild system prompt with newly registered tools
			// This ensures human_feedback and workspace tools appear in the system prompt
			if err := underlyingAgent.UpdateCodeExecutionRegistry(); err != nil {
				log.Printf("[CUSTOM TOOLS] Warning: Failed to update code execution registry: %v", err)
			}

			// Add base instructions — skip for plan mode (multi-agent) since the
			// main agent is an orchestrator, not a file writer. The Chats/ folder
			// rules don't apply; background sub-agents get their own instructions.
			if req.DelegationMode != "plan" {
				underlyingAgent.AppendSystemPrompt(GetAgentInstructions())
			}

			// Add skill builder instructions when skill-creator is active
			if hasSkillCreator {
				underlyingAgent.AppendSystemPrompt(GetSkillBuilderInstructions())
				log.Printf("[SKILL BUILDER] Added skill builder instructions to system prompt")
			}

			// Add sub-agent builder instructions when subagent-creator is active
			if hasSubAgentCreator {
				underlyingAgent.AppendSystemPrompt(GetSubAgentBuilderInstructions())
				log.Printf("[SUBAGENT BUILDER] Added sub-agent builder instructions to system prompt")
			}

			// Add skill instructions if skills are selected
			if len(req.SelectedSkills) > 0 {
				skillPrompt := buildSkillPrompt(req.SelectedSkills)
				if skillPrompt != "" {
					underlyingAgent.AppendSystemPrompt(skillPrompt)
					log.Printf("[SKILLS] Added skill instructions to system prompt (%d skills)", len(req.SelectedSkills))
				}
			}

			// Add workflow context if workflow paths are selected (via # in chat)
			if len(req.WorkflowContextPaths) > 0 {
				workflowPrompt := buildWorkflowContextPrompt(req.WorkflowContextPaths, getWorkspaceAPIURL())
				if workflowPrompt != "" {
					underlyingAgent.AppendSystemPrompt(workflowPrompt)
					log.Printf("[WORKFLOW-CTX] Added workflow context to system prompt (%d workflows)", len(req.WorkflowContextPaths))
				}
			}

			// Add delegation instructions based on mode
			if req.DelegationMode == "plan" {
				if req.PlanPhase == "execution" {
					// Execution-only mode: skip planning, use spawn-style delegation with exec override
					underlyingAgent.AppendSystemPrompt(virtualtools.GetExecutionOnlyInstructions())
					log.Printf("[DELEGATION] Added execution-only instructions to system prompt (mode: plan, phase: execution)")
				} else {
					// Plan mode: plan→approve→execute with async background agents
					underlyingAgent.AppendSystemPrompt(virtualtools.GetPlanWithBackgroundAgentsInstructions())
					log.Printf("[DELEGATION] Added plan+background agent instructions to system prompt (mode: plan)")
				}
				// Inject custom tier descriptions into system prompt so the manager knows about them
				if delegationTierCfg := resolveDelegationTierConfig(req.DelegationTierConfig); delegationTierCfg != nil {
					if tierSection := virtualtools.BuildCustomTierPromptSection(delegationTierCfg); tierSection != "" {
						underlyingAgent.AppendSystemPrompt(tierSection)
					}
				}
			} else if req.DelegationMode == "spawn" {
				underlyingAgent.AppendSystemPrompt(virtualtools.GetDelegationInstructions())
				// Inject custom tier descriptions into system prompt so the manager knows about them
				if delegationTierCfg := resolveDelegationTierConfig(req.DelegationTierConfig); delegationTierCfg != nil {
					if tierSection := virtualtools.BuildCustomTierPromptSection(delegationTierCfg); tierSection != "" {
						underlyingAgent.AppendSystemPrompt(tierSection)
					}
				}
				log.Printf("[DELEGATION] Added delegation instructions to system prompt (mode: spawn)")
			}

			// Memory tools are available in all chat modes.
			underlyingAgent.AppendSystemPrompt(virtualtools.GetMemoryInstructions())

			// Add CLI-specific human tool override when using CLI-based providers.
			// This covers delegation tools, memory tools, and human interaction tools —
			// all registered under the "human" category and accessible only via HTTP API.
			if req.Provider == "claude-code" {
				underlyingAgent.AppendSystemPrompt(virtualtools.GetClaudeCodeDelegationOverride())
				log.Printf("[CLAUDE CODE] Added human tool HTTP API override instructions")
			}
			if req.Provider == "gemini-cli" {
				underlyingAgent.AppendSystemPrompt(virtualtools.GetClaudeCodeDelegationOverride())
				log.Printf("[GEMINI CLI] Added human tool HTTP API override instructions")
			}
		}

		// Add event observer immediately after agent creation to capture all events
		// ✅ FIX: Always attach EventObserver to agent, even in orchestrator mode
		// The eventbridge.OrchestratorAgentEventBridge handles orchestrator-specific events, but we still need EventObserver for regular agent events
		log.Printf("[DATABASE DEBUG] Starting event observer setup for session %s", sessionID)
		log.Printf("[DATABASE DEBUG] ChatDB available: %v", api.chatDB != nil)

		log.Printf("[DATABASE DEBUG] Creating in-memory event observer for session %s", sessionID)
		// Create in-memory event observer for real-time updates
		eventObserver := events.NewEventObserverWithLogger(api.eventStore, sessionID, api.logger)

		log.Printf("[DATABASE DEBUG] Creating database event observer for session %s", sessionID)
		// Create database event observer to store events in database
		dbEventObserver := database.NewEventDatabaseObserver(api.chatDB)
		log.Printf("[DATABASE DEBUG] Database event observer created successfully for session %s", sessionID)

		// Add event observer directly to the underlying MCP agent since the wrapper's AddEventListener is disabled
		log.Printf("[DATABASE DEBUG] Getting underlying agent for session %s", sessionID)
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			log.Printf("[DATABASE DEBUG] Underlying agent found, adding event observers for session %s", sessionID)
			underlyingAgent.AddEventListener(eventObserver)
			log.Printf("[DATABASE DEBUG] Added in-memory event observer for session %s", sessionID)
			underlyingAgent.AddEventListener(dbEventObserver)
			log.Printf("[DATABASE DEBUG] Added database event observer for session %s", sessionID)
		} else {
			log.Printf("[DATABASE DEBUG] ERROR: Underlying MCP agent is nil for session %s", sessionID)
		}

		// --- BEGIN: Load conversation history and accumulate for streaming ---
		// Load conversation history for this session
		api.conversationMux.RLock()
		history, exists := api.conversationHistory[sessionID]
		api.conversationMux.RUnlock()

		if exists && len(history) > 0 {
			log.Printf("[CONVERSATION DEBUG] Loading %d messages from in-memory conversation history for session %s", len(history), sessionID)
			// Load the conversation history into the agent
			for _, msg := range history {
				llmAgent.AppendMessage(msg)
			}
		} else if api.chatDB != nil {
			// Try to load conversation history from database
			log.Printf("[CONVERSATION DEBUG] No in-memory history found for session %s, attempting to load from database", sessionID)

			// Get events for this session, focusing on conversation_turn events
			dbEvents, err := api.chatDB.GetEventsBySession(r.Context(), sessionID, 1000, 0)
			if err != nil {
				log.Printf("[CONVERSATION DEBUG] Failed to load events from database for session %s: %v", sessionID, err)
			} else {
				// Find the last conversation_turn event which contains full conversation history
				var lastTurnEvent *database.Event
				for i := len(dbEvents) - 1; i >= 0; i-- {
					if dbEvents[i].EventType == string(unifiedevents.ConversationTurn) {
						lastTurnEvent = &dbEvents[i]
						break
					}
				}

				if lastTurnEvent != nil {
					// Parse the event data using typed structures
					// EventData contains the full AgentEvent JSON structure
					// Use a helper struct to unmarshal Data as json.RawMessage first
					type agentEventHelper struct {
						Type unifiedevents.EventType `json:"type"`
						Data json.RawMessage         `json:"data"`
					}

					var agentEvent agentEventHelper
					if err := json.Unmarshal(lastTurnEvent.EventData, &agentEvent); err != nil {
						log.Printf("[CONVERSATION DEBUG] Failed to parse AgentEvent for session %s: %v", sessionID, err)
					} else if agentEvent.Type == unifiedevents.ConversationTurn {
						// Unmarshal Data field to ConversationTurnEvent
						var turnEvent unifiedevents.ConversationTurnEvent
						if err := json.Unmarshal(agentEvent.Data, &turnEvent); err != nil {
							log.Printf("[CONVERSATION DEBUG] Failed to parse ConversationTurnEvent data for session %s: %v", sessionID, err)
						} else if len(turnEvent.Messages) > 0 {
							// Deserialize messages from SerializedMessage format back to llmtypes.MessageContent
							deserializedHistory := make([]llmtypes.MessageContent, 0, len(turnEvent.Messages))
							for _, serializedMsg := range turnEvent.Messages {
								msg := deserializeSerializedMessage(serializedMsg)
								if msg != nil {
									deserializedHistory = append(deserializedHistory, *msg)
								}
							}

							if len(deserializedHistory) > 0 {
								log.Printf("[CONVERSATION DEBUG] Loaded %d messages from database conversation history for session %s", len(deserializedHistory), sessionID)
								// Load the conversation history into the agent
								for _, msg := range deserializedHistory {
									llmAgent.AppendMessage(msg)
								}
								// Cache in memory for future use
								api.conversationMux.Lock()
								api.conversationHistory[sessionID] = deserializedHistory
								api.conversationMux.Unlock()
							} else {
								log.Printf("[CONVERSATION DEBUG] No valid messages found after deserialization for session %s", sessionID)
							}
						} else {
							log.Printf("[CONVERSATION DEBUG] ConversationTurnEvent has no messages for session %s", sessionID)
						}
					} else {
						log.Printf("[CONVERSATION DEBUG] Event type is %s, expected conversation_turn for session %s", agentEvent.Type, sessionID)
					}
				} else {
					log.Printf("[CONVERSATION DEBUG] No conversation_turn event found in database for session %s", sessionID)
				}
			}
		} else {
			log.Printf("[CONVERSATION DEBUG] No conversation history found for session %s, starting fresh", sessionID)
		}

		// Note: User message is added by StreamWithEvents internally, no need to add it here
		// --- END: Load conversation history and accumulate for streaming ---

		log.Printf("[AGENT DEBUG] Starting agent processing for query %s", queryID)

		// Create a cancellable context for agent execution using background context
		// This prevents the agent from being canceled when the HTTP request ends
		agentCtx, agentCancel := context.WithCancel(context.Background())

		// Inject user ID into the agent context for per-user folder isolation
		// This allows workspace tools to route per-user folders correctly
		agentCtx = context.WithValue(agentCtx, common.UserIDKey, currentUserID)
		log.Printf("[USER_ID_DEBUGGING] Main agent: injected UserIDKey=%q into agentCtx", currentUserID)

		// Store the cancel function for potential cancellation
		api.agentCancelMux.Lock()
		api.agentCancelFuncs[sessionID] = agentCancel
		api.agentCancelMux.Unlock()

		// Merge global secrets with user-supplied secrets, then inject into system prompt (not user message)
		chatQuery := req.Query
		allChatSecrets := mergeGlobalSecrets(req.DecryptedSecrets, req.SelectedGlobalSecrets)
		if len(allChatSecrets) > 0 {
			var secretParts []string
			for _, s := range allChatSecrets {
				secretParts = append(secretParts, fmt.Sprintf("### %s\n```\n%s\n```", s.Name, s.Value))
			}
			secretPrompt := "\n## 🔐 Secrets\n\nThe following secrets/credentials have been provided. Use them as needed:\n\n" + strings.Join(secretParts, "\n")
			if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
				underlyingAgent.AppendSystemPrompt(secretPrompt)
				log.Printf("[SECRETS] Injected %d secrets (%d global + %d user) into system prompt", len(allChatSecrets), len(allChatSecrets)-len(req.DecryptedSecrets), len(req.DecryptedSecrets))
			}
		}

		// Restore Claude Code CLI session ID for --resume on subsequent turns
		if ccSID, ok := api.claudeCodeSessionIDs[sessionID]; ok {
			if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
				underlyingAgent.ClaudeCodeSessionID = ccSID
			}
		}

		// Restore Gemini CLI session ID for --resume on subsequent turns
		if gSID, ok := api.geminiSessionIDs[sessionID]; ok {
			if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
				underlyingAgent.GeminiSessionID = gSID
			}
		}

		// Use the enhanced wrapper to get text chunks - events are handled via EventObserver and polling API
		log.Printf("[LATENCY_DEBUG] T+%dms | Starting StreamWithEvents (LLM call) | queryID=%s", time.Since(startTime).Milliseconds(), queryID)
		textChan, err := llmAgent.StreamWithEvents(agentCtx, chatQuery)
		if err != nil {
			log.Printf("[AGENT DEBUG] llmAgent.StreamWithEvents() error: %v", err)
			sendError(fmt.Sprintf("Failed to start streaming: %v", err), true)
			return
		}
		log.Printf("[LATENCY_DEBUG] T+%dms | StreamWithEvents channel opened | queryID=%s", time.Since(startTime).Milliseconds(), queryID)
		log.Printf("[AGENT DEBUG] llmAgent.StreamWithEvents() started successfully for query %s", queryID)

		// Stream response chunks with enhanced error handling
		chunkCount := 0

		log.Printf("[AGENT DEBUG] Entering streaming loop for query %s", queryID)
		for chunk := range textChan {
			log.Printf("[AGENT DEBUG] raw chunk (len=%d): %s", len(chunk), chunk)
			chunkCount++

			// Note: Chunks are processed by the agent internally, no manual accumulation needed

			// Save conversation history incrementally during streaming
			// This ensures we don't lose progress if streaming is stopped mid-way
			api.conversationMux.Lock()
			api.conversationHistory[sessionID] = llmAgent.GetHistory()
			api.conversationMux.Unlock()

			// Check for context cancellation
			select {
			case <-streamCtx.Done():
				tracer.EndTrace(traceID, map[string]interface{}{
					"status":   "timeout",
					"query_id": queryID,
				})

				// Update chat session status to error for timeout
				if chatSession != nil {
					updateReq := &database.UpdateChatSessionRequest{
						Title:     chatSession.Title,     // Preserve existing title
						AgentMode: chatSession.AgentMode, // Preserve existing agent_mode
						Status:    "error",
					}
					_, err := api.chatDB.UpdateChatSession(streamCtx, sessionID, updateReq)
					if err != nil {
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to error (timeout): %v", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to error status (timeout)", sessionID)
					}
				}

				// Update active session status to error
				api.updateSessionStatus(sessionID, "error")

				// Emit server-level timeout completion event
				// Create a timeout completion event using UnifiedCompletionEvent
				timeoutEventData := unifiedevents.NewUnifiedCompletionEventWithError(
					"server",              // agentType
					req.AgentMode,         // agentMode
					req.Query,             // question
					"context timeout",     // error message
					time.Since(startTime), // duration
					0,                     // turns
				)

				agentEvent := unifiedevents.NewAgentEvent(timeoutEventData)
				agentEvent.SessionID = sessionID

				serverTimeoutEvent := events.Event{
					ID:        fmt.Sprintf("server_timeout_%s_%d", queryID, time.Now().UnixNano()),
					Type:      string(unifiedevents.EventTypeUnifiedCompletion),
					Timestamp: time.Now(),
					Data:      agentEvent,
					SessionID: sessionID,
				}
				api.eventStore.AddEvent(sessionID, serverTimeoutEvent)
				log.Printf("[SERVER DEBUG] Emitted server timeout completion event for query %s", queryID)
				return
			default:
			}
		}
		log.Printf("[AGENT DEBUG] Streaming loop exited for query %s", queryID)
		log.Printf("[AGENT DEBUG] After streaming loop, streamCtx.Err(): %v", streamCtx.Err())

		// Final save of conversation history (in case streaming was stopped mid-way)
		// This ensures we capture the final state even if streaming was interrupted
		api.conversationMux.Lock()
		api.conversationHistory[sessionID] = llmAgent.GetHistory()
		api.conversationMux.Unlock()
		log.Printf("[CONVERSATION DEBUG] Final save: %d messages to conversation history for session %s", len(llmAgent.GetHistory()), sessionID)

		// Save Claude Code CLI session ID for --resume on next turn
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			if ccSID := underlyingAgent.ClaudeCodeSessionID; ccSID != "" {
				api.claudeCodeSessionIDs[sessionID] = ccSID
				log.Printf("[CLAUDE CODE] Saved session ID %s for session %s", ccSID, sessionID)
			}
		}

		// Save Gemini CLI session ID for --resume on next turn
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			if gSID := underlyingAgent.GeminiSessionID; gSID != "" {
				api.geminiSessionIDs[sessionID] = gSID
				log.Printf("[GEMINI CLI] Saved session ID %s for session %s", gSID, sessionID)
			}
		}

		// Store agent for reuse by synthetic turns (plan mode only)
		// The stored agent retains all tools, prompts, observers, and conversation history
		if req.DelegationMode == "plan" {
			api.sessionAgentsMux.Lock()
			api.sessionAgents[sessionID] = llmAgent
			api.sessionAgentsMux.Unlock()
			log.Printf("[BG AGENT] Stored agent for session %s for synthetic turn reuse", sessionID)
		}

		// Clean up the agent cancel function when streaming is complete
		api.agentCancelMux.Lock()
		delete(api.agentCancelFuncs, sessionID)
		api.agentCancelMux.Unlock()

		// --- BEGIN: Persist plan state for session resume ---
		if req.DelegationMode == "plan" {
			planState := api.getOrCreatePlanSessionState(sessionID)
			if planState.PlanID != "" {
				// Read existing config, merge plan state, and save
				if chatSession != nil {
					existingConfig := &database.ChatSessionConfig{}
					if cs, err := api.chatDB.GetChatSession(context.Background(), sessionID); err == nil && cs != nil {
						if cfg, err := cs.GetConfig(); err == nil && cfg != nil {
							existingConfig = cfg
						}
					}
					existingConfig.PlanID = planState.PlanID
					existingConfig.PlanFolder = planState.PlanFolder
					existingConfig.PlanPhase = planState.GetPhase()
					if configJSON, err := existingConfig.ToJSON(); err == nil {
						if _, err := api.chatDB.UpdateChatSession(streamCtx, sessionID, &database.UpdateChatSessionRequest{
							Config: configJSON,
						}); err != nil {
							log.Printf("[PLAN STATE] Failed to persist plan state: %v", err)
						} else {
							log.Printf("[PLAN STATE] Saved plan state for session %s: plan=%s phase=%s", sessionID, planState.PlanID, planState.GetPhase())
						}
					}
				}
			}
		}
		// --- END: Persist plan state for session resume ---

		// --- BEGIN: Update chat session status to completed ---
		if chatSession != nil {
			// Update session status to completed with completion timestamp
			completedAt := time.Now()
			updateReq := &database.UpdateChatSessionRequest{
				Status:      "completed",
				CompletedAt: &completedAt,
			}
			_, err := api.chatDB.UpdateChatSession(streamCtx, sessionID, updateReq)
			if err != nil {
				log.Printf("[DATABASE DEBUG] Failed to update chat session status to completed: %v", err)
			} else {
				log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to completed status", sessionID)
			}
		}
		// --- END: Update chat session status to completed ---

		// Update active session status to completed
		log.Printf("[COMPLETION] Updating session %s status to completed", sessionID)
		api.updateSessionStatus(sessionID, "completed")

		// End conversation trace
		tracer.EndTrace(traceID, map[string]interface{}{
			"status": "completed",
		})

		// Note: Completion events are emitted by the underlying agent, no need for server-level events

		log.Printf("[AGENT DEBUG] Query %s completed successfully", queryID)
	}()
}

// Add endpoint to stop/clear a session
func (api *StreamingAPI) handleStopSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	// Verify session ownership for multi-user isolation
	currentUserID := GetUserIDFromContext(r.Context())
	_, err := api.chatDB.GetChatSessionWithUser(r.Context(), sessionID, currentUserID)
	if err != nil {
		http.Error(w, "Session not found or access denied", http.StatusNotFound)
		return
	}

	// Cancel agent execution context if it exists
	api.agentCancelMux.Lock()
	if cancelFunc, exists := api.agentCancelFuncs[sessionID]; exists {
		cancelFunc() // Cancel the agent execution
		delete(api.agentCancelFuncs, sessionID)
		log.Printf("[SESSION DEBUG] Canceled agent execution context for session %s", sessionID)
	}
	api.agentCancelMux.Unlock()

	// Update active session status to stopped
	api.updateSessionStatus(sessionID, "stopped")

	// NOTE: Do NOT clean up sessionAgents or cancel background agents here.
	// handleStopSession is called when the user sends a new message (to stop the current turn)
	// or presses the stop button. Background agents and stored agents must survive across turns
	// so that synthetic turns can fire when background agents complete.
	// Background agents are only canceled explicitly via terminate_agent tool or when the
	// session is fully closed/deleted.

	// Cancel all workflow orchestrator contexts for this session
	// Since we now use queryID as the key, we need to look up all queryIDs for this session
	api.sessionQueryIDMux.Lock()
	queryIDs := api.sessionQueryIDs[sessionID]
	delete(api.sessionQueryIDs, sessionID) // Clear the mapping
	api.sessionQueryIDMux.Unlock()

	if len(queryIDs) > 0 {
		api.workflowOrchestratorContextMux.Lock()
		for _, qid := range queryIDs {
			if cancelFunc, exists := api.workflowOrchestratorContexts[qid]; exists {
				cancelFunc() // Cancel this workflow execution
				delete(api.workflowOrchestratorContexts, qid)
				log.Printf("[SESSION DEBUG] Canceled workflow execution %s for session %s", qid, sessionID)
			}
		}
		api.workflowOrchestratorContextMux.Unlock()
		log.Printf("[SESSION DEBUG] Canceled %d workflow execution(s) for session %s", len(queryIDs), sessionID)
	}

	// Clear workflow objective
	api.workflowObjectiveMux.Lock()
	if _, exists := api.workflowObjectives[sessionID]; exists {
		delete(api.workflowObjectives, sessionID)
		log.Printf("[SESSION DEBUG] Cleared workflow objective for session %s", sessionID)
	}
	api.workflowObjectiveMux.Unlock()

	// Note: Conversation history and orchestrator state are preserved to allow resuming the conversation
	// Use /api/session/clear if you want to clear conversation history

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Session stopped (conversation history and orchestrator state preserved)"))
}

// Add endpoint to clear conversation history for a session
func (api *StreamingAPI) handleClearSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	// Verify session ownership for multi-user isolation
	currentUserID := GetUserIDFromContext(r.Context())
	_, err := api.chatDB.GetChatSessionWithUser(r.Context(), sessionID, currentUserID)
	if err != nil {
		http.Error(w, "Session not found or access denied", http.StatusNotFound)
		return
	}

	// Clear conversation history
	api.conversationMux.Lock()
	if _, exists := api.conversationHistory[sessionID]; exists {
		delete(api.conversationHistory, sessionID)
		log.Printf("[SESSION DEBUG] Cleared conversation history for session %s", sessionID)
	}
	api.conversationMux.Unlock()

	// Clear orchestrator state (removed - now stateless)

	// Clear orchestrator instance (legacy removed)

	// Clear workflow objective
	api.workflowObjectiveMux.Lock()
	if _, exists := api.workflowObjectives[sessionID]; exists {
		delete(api.workflowObjectives, sessionID)
		log.Printf("[SESSION DEBUG] Cleared workflow objective for session %s", sessionID)
	}
	api.workflowObjectiveMux.Unlock()

	// Clear Claude Code CLI session ID
	delete(api.claudeCodeSessionIDs, sessionID)

	// Clear Gemini CLI session ID
	delete(api.geminiSessionIDs, sessionID)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Session cleared (conversation history and orchestrator state removed)"))
}

// State management functions removed - orchestrator is now stateless

// createServerLogger creates a logger instance for the server
// This logger writes to stdout only to avoid duplication with shell redirection
func createServerLogger() loggerv2.Logger {
	// Force stdout logging by passing empty log file and enableStdout=true
	// This prevents the application from writing to the file directly,
	// allowing the shell script's redirection to handle file logging without duplicates.
	logFile := ""

	// Check for log level from environment variable
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	serverLogger, err := logger.CreateLogger(logFile, logLevel, "text", true)
	if err != nil {
		log.Fatalf("Failed to create server logger: %v", err)
	}
	return serverLogger
}

// createLLMLogger creates a separate logger instance for LLM operations
// This logger writes to logs/llm_debug.log to separate LLM logs from server logs
func createLLMLogger() loggerv2.Logger {
	llmLogger, err := logger.CreateLogger("logs/llm_debug.log", "debug", "text", false)
	if err != nil {
		log.Fatalf("Failed to create LLM logger: %v", err)
	}
	return llmLogger
}

// Chat History API Handlers

// createChatSessionHandler creates a new chat session
func createChatSessionHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req database.CreateChatSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Get current user ID for session isolation
		userID := GetUserIDFromContext(r.Context())

		session, err := db.CreateChatSessionWithUser(r.Context(), &req, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(session)
	}
}

// listChatSessionsHandler lists all chat sessions with pagination
func listChatSessionsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")
		presetQueryID := r.URL.Query().Get("preset_query_id")
		agentMode := r.URL.Query().Get("agent_mode")

		limit := 20
		offset := 0

		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}

		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil {
				offset = o
			}
		}

		// Convert preset_query_id to pointer for optional filtering
		var presetQueryIDPtr *string
		if presetQueryID != "" {
			presetQueryIDPtr = &presetQueryID
		}

		// Convert agent_mode to pointer for optional filtering
		var agentModePtr *string
		if agentMode != "" {
			agentModePtr = &agentMode
		}

		// Get current user ID for session isolation
		userID := GetUserIDFromContext(r.Context())

		sessions, total, err := db.ListChatSessionsWithUser(r.Context(), limit, offset, presetQueryIDPtr, agentModePtr, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"sessions": sessions,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// getChatSessionHandler gets a specific chat session
func getChatSessionHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]

		// Get current user ID for session isolation
		userID := GetUserIDFromContext(r.Context())

		session, err := db.GetChatSessionWithUser(r.Context(), sessionID, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session)
	}
}

// updateChatSessionHandler updates a chat session
func updateChatSessionHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]

		// Get current user ID for session isolation - verify ownership first
		userID := GetUserIDFromContext(r.Context())
		_, err := db.GetChatSessionWithUser(r.Context(), sessionID, userID)
		if err != nil {
			http.Error(w, "Session not found or access denied", http.StatusNotFound)
			return
		}

		var req database.UpdateChatSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		session, err := db.UpdateChatSession(r.Context(), sessionID, &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session)
	}
}

// deleteChatSessionHandler deletes a chat session
func deleteChatSessionHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]

		// Get current user ID for session isolation - verify ownership first
		userID := GetUserIDFromContext(r.Context())
		_, err := db.GetChatSessionWithUser(r.Context(), sessionID, userID)
		if err != nil {
			http.Error(w, "Session not found or access denied", http.StatusNotFound)
			return
		}

		err = db.DeleteChatSession(r.Context(), sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Chat session deleted successfully"})
	}
}

// convertDBEventToPollingEvent converts a database.Event to events.Event format
// This is the same conversion used by the polling API to ensure consistency
func convertDBEventToPollingEvent(dbEvent database.Event, sessionID string) (*events.Event, error) {
	// Unmarshal the full AgentEvent - use helper struct to handle EventData interface
	type agentEventWithRawData struct {
		Type           unifiedevents.EventType `json:"type"`
		Timestamp      time.Time               `json:"timestamp"`
		EventIndex     int                     `json:"event_index"`
		TraceID        string                  `json:"trace_id,omitempty"`
		SpanID         string                  `json:"span_id,omitempty"`
		ParentID       string                  `json:"parent_id,omitempty"`
		CorrelationID  string                  `json:"correlation_id,omitempty"`
		HierarchyLevel int                     `json:"hierarchy_level"`
		SessionID      string                  `json:"session_id,omitempty"`
		Component      string                  `json:"component,omitempty"`
		Data           json.RawMessage         `json:"data"`
	}

	var helper agentEventWithRawData
	if err := json.Unmarshal(dbEvent.EventData, &helper); err != nil {
		return nil, fmt.Errorf("failed to parse event: %w", err)
	}

	// Unmarshal Data field into a map to preserve structure
	var dataMap map[string]interface{}
	if err := json.Unmarshal(helper.Data, &dataMap); err != nil {
		return nil, fmt.Errorf("failed to parse event data: %w", err)
	}

	// Extract event-specific fields, excluding BaseEventData fields
	// BaseEventData fields are: timestamp, trace_id, span_id, event_id, parent_id,
	// is_end_event, correlation_id, hierarchy_level, session_id, component, metadata
	baseEventDataFields := map[string]bool{
		"timestamp":       true,
		"trace_id":        true,
		"span_id":         true,
		"event_id":        true,
		"parent_id":       true,
		"is_end_event":    true,
		"correlation_id":  true,
		"hierarchy_level": true,
		"session_id":      true,
		"component":       true,
		"metadata":        true,
	}

	actualEventData := make(map[string]interface{})
	for k, v := range dataMap {
		// Skip BaseEventData fields - they're already in AgentEvent
		if !baseEventDataFields[k] {
			actualEventData[k] = v
		}
	}

	// Create AgentEvent with flatEventData that serializes directly as event-specific fields
	// This ensures event.data.data contains the actual event data (like {content: "..."})
	agentEvent := unifiedevents.AgentEvent{
		Type:           helper.Type,
		Timestamp:      helper.Timestamp,
		EventIndex:     helper.EventIndex,
		TraceID:        helper.TraceID,
		SpanID:         helper.SpanID,
		ParentID:       helper.ParentID,
		CorrelationID:  helper.CorrelationID,
		HierarchyLevel: helper.HierarchyLevel,
		SessionID:      helper.SessionID,
		Component:      helper.Component,
		Data: &flatEventData{
			eventData: actualEventData,
			eventType: helper.Type,
		},
	}

	return &events.Event{
		ID:        dbEvent.ID,
		Type:      dbEvent.EventType,
		Timestamp: dbEvent.Timestamp,
		SessionID: sessionID,
		Data:      &agentEvent,
	}, nil
}

// getSessionEventsHandler gets events for a specific session
// Returns events in the same format as polling API (events.Event[])
func getSessionEventsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]

		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		limit := 100
		offset := 0

		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}

		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil {
				offset = o
			}
		}

		// Cap limit to prevent slow queries and large responses (max 500 events per request)
		const maxLimit = 500
		if limit <= 0 || limit > maxLimit {
			limit = maxLimit
		}

		dbEvents, err := db.GetEventsBySession(r.Context(), sessionID, limit, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[CHAT_HISTORY] Loading events for session %s: found %d events", sessionID, len(dbEvents))

		// Convert database events to polling events format using shared helper
		convertedEvents := make([]events.Event, 0, len(dbEvents))
		parseErrors := 0
		for i, dbEvent := range dbEvents {
			convertedEvent, err := convertDBEventToPollingEvent(dbEvent, sessionID)
			if err != nil {
				parseErrors++
				if i < 3 {
					log.Printf("[CHAT_HISTORY ERROR] Failed to convert event %d for session %s: %v, event_type=%s", i, sessionID, err, dbEvent.EventType)
				}
				continue
			}

			convertedEvents = append(convertedEvents, *convertedEvent)
		}

		log.Printf("[CHAT_HISTORY] Converted %d events: converted=%d, parse_errors=%d", len(dbEvents), len(convertedEvents), parseErrors)

		// Get total count using COUNT(*) - O(1) with index, avoids fetching all events
		total := offset + len(dbEvents)
		if count, err := db.CountEventsBySession(r.Context(), sessionID); err == nil {
			total = count
		}

		response := map[string]interface{}{
			"events": convertedEvents,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// searchEventsHandler searches events with filters
func searchEventsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var filter database.EventFilter

		// Parse query parameters
		if sessionID := r.URL.Query().Get("session_id"); sessionID != "" {
			filter.SessionID = sessionID
		}

		if eventType := r.URL.Query().Get("event_type"); eventType != "" {
			filter.EventType = unifiedevents.EventType(eventType)
		}

		if fromDateStr := r.URL.Query().Get("from_date"); fromDateStr != "" {
			if fromDate, err := time.Parse(time.RFC3339, fromDateStr); err == nil {
				filter.FromDate = fromDate
			}
		}

		if toDateStr := r.URL.Query().Get("to_date"); toDateStr != "" {
			if toDate, err := time.Parse(time.RFC3339, toDateStr); err == nil {
				filter.ToDate = toDate
			}
		}

		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		limit := 100
		offset := 0

		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}

		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil {
				offset = o
			}
		}

		filter.Limit = limit
		filter.Offset = offset

		req := &database.GetChatHistoryRequest{
			SessionID: filter.SessionID,
			EventType: string(filter.EventType),
			FromDate:  filter.FromDate,
			ToDate:    filter.ToDate,
			Limit:     filter.Limit,
			Offset:    filter.Offset,
		}

		response, err := db.GetEvents(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// chatHistoryHealthCheckHandler health check for chat history
func chatHistoryHealthCheckHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "unhealthy",
				"error":  err.Error(),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "healthy",
			"service": "chat-history",
		})
	}
}

// getAllSessionCostsHandler returns aggregate costs across all user's chat sessions
func getAllSessionCostsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
		agentMode := r.URL.Query().Get("agent_mode")
		limitStr := r.URL.Query().Get("limit")

		limit := 100
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}

		var agentModePtr *string
		if agentMode != "" {
			agentModePtr = &agentMode
		}

		sessions, _, err := db.ListChatSessionsWithUser(r.Context(), limit, 0, nil, agentModePtr, userID)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to list sessions: %v", err), http.StatusInternalServerError)
			return
		}

		// Collect all session IDs for batch query
		sessionIDs := make([]string, len(sessions))
		sessionMap := make(map[string]*database.ChatHistorySummary, len(sessions))
		for i, s := range sessions {
			sessionIDs[i] = s.SessionID
			sCopy := s
			sessionMap[s.SessionID] = &sCopy
		}

		// Batch fetch token_usage + delegation_start events for ALL sessions in one query
		allEvents, err := batchGetCostEvents(r.Context(), db, sessionIDs)
		if err != nil {
			log.Printf("[COSTS] Error batch fetching cost events: %v", err)
			// Fall through with empty events
		}

		aggregate := AggregateCosts{
			ByModel: make(map[string]*ChatModelUsage),
			ByAgent: make(map[string]*ChatModelUsage),
		}

		var sessionCosts []SessionCostSummary

		for _, session := range sessions {
			sessEvents := allEvents[session.SessionID]

			// Build delegation name map from delegation_start events
			delegationNameMap := make(map[string]string)
			for _, ev := range sessEvents {
				if ev.eventType == "delegation_start" {
					delegationNameMap[ev.delegationID] = ev.bgAgentID
				}
			}

			// Determine display mode
			displayMode := session.AgentMode
			if session.AgentMode == "simple" {
				if cfg, err := database.ChatSessionConfigFromJSON(session.Config); err == nil && cfg != nil && cfg.DelegationMode == "plan" {
					displayMode = "multi-agent"
				}
			}

			summary := SessionCostSummary{
				SessionID: session.SessionID,
				Title:     session.Title,
				AgentMode: displayMode,
				CreatedAt: session.CreatedAt,
				Status:    session.Status,
				ByModel:   make(map[string]*ChatModelUsage),
				ByAgent:   make(map[string]*ChatModelUsage),
			}

			for _, ev := range sessEvents {
				if ev.eventType != "token_usage" {
					continue
				}
				tud := ev.tokenData

				// Resolve delegation component name
				if tud.correlationID != "" {
					if agentName, ok := delegationNameMap[tud.correlationID]; ok && agentName != "" {
						tud.Component = agentName
					}
				}

				modelUsage := getOrCreateModelUsage(summary.ByModel, tud.ModelID)
				accumulateUsage(modelUsage, &tud)

				if tud.Component != "" {
					agentUsage := getOrCreateModelUsage(summary.ByAgent, tud.Component)
					accumulateUsage(agentUsage, &tud)
				}

				summary.TotalCost += tud.TotalCost
				summary.TotalInput += tud.PromptTokens
				summary.TotalOutput += tud.CompletionTokens
				summary.TotalCalls++
			}

			sessionCosts = append(sessionCosts, summary)

			aggregate.TotalCost += summary.TotalCost
			aggregate.TotalInput += summary.TotalInput
			aggregate.TotalOutput += summary.TotalOutput
			aggregate.TotalCalls += summary.TotalCalls

			for modelID, usage := range summary.ByModel {
				aggModel := getOrCreateModelUsage(aggregate.ByModel, modelID)
				mergeUsage(aggModel, usage)
			}
			for agentName, usage := range summary.ByAgent {
				aggAgent := getOrCreateModelUsage(aggregate.ByAgent, agentName)
				mergeUsage(aggAgent, usage)
			}
		}

		aggregate.TotalSessions = len(sessionCosts)

		if sessionCosts == nil {
			sessionCosts = []SessionCostSummary{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UserCostsResponse{
			Sessions:  sessionCosts,
			Aggregate: aggregate,
		})
	}
}

// getSessionCostsHandler returns detailed cost breakdown for a single session
func getSessionCostsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]
		if sessionID == "" {
			http.Error(w, "session_id is required", http.StatusBadRequest)
			return
		}

		userID := GetUserIDFromContext(r.Context())

		session, err := db.GetChatSessionWithUser(r.Context(), sessionID, userID)
		if err != nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}

		// Use batch query for single session (same optimized path)
		allEvents, err := batchGetCostEvents(r.Context(), db, []string{sessionID})
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to get token events: %v", err), http.StatusInternalServerError)
			return
		}
		sessEvents := allEvents[sessionID]

		// Build delegation name map
		delegationNameMap := make(map[string]string)
		for _, ev := range sessEvents {
			if ev.eventType == "delegation_start" {
				delegationNameMap[ev.delegationID] = ev.bgAgentID
			}
		}

		detail := SessionCostDetail{
			SessionID:       session.SessionID,
			Title:           session.Title,
			CreatedAt:       session.CreatedAt,
			ByModel:         make(map[string]*ChatModelUsage),
			ByTurnAndModel:  make(map[string]map[string]*ChatModelUsage),
			ByAgentAndModel: make(map[string]map[string]*ChatModelUsage),
		}

		for _, ev := range sessEvents {
			if ev.eventType != "token_usage" {
				continue
			}
			tud := ev.tokenData

			// Resolve delegation component name
			if tud.correlationID != "" {
				if agentName, ok := delegationNameMap[tud.correlationID]; ok && agentName != "" {
					tud.Component = agentName
				}
			}

			modelUsage := getOrCreateModelUsage(detail.ByModel, tud.ModelID)
			accumulateUsage(modelUsage, &tud)

			turnKey := fmt.Sprintf("turn-%d", tud.Turn)
			if detail.ByTurnAndModel[turnKey] == nil {
				detail.ByTurnAndModel[turnKey] = make(map[string]*ChatModelUsage)
			}
			turnModelUsage := getOrCreateModelUsage(detail.ByTurnAndModel[turnKey], tud.ModelID)
			accumulateUsage(turnModelUsage, &tud)

			agentKey := tud.Component
			if agentKey == "" {
				agentKey = "main"
			}
			if detail.ByAgentAndModel[agentKey] == nil {
				detail.ByAgentAndModel[agentKey] = make(map[string]*ChatModelUsage)
			}
			agentModelUsage := getOrCreateModelUsage(detail.ByAgentAndModel[agentKey], tud.ModelID)
			accumulateUsage(agentModelUsage, &tud)

			detail.TotalCost += tud.TotalCost
			detail.TotalInput += tud.PromptTokens
			detail.TotalOutput += tud.CompletionTokens
			detail.TotalCalls++
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(detail)
	}
}

// getDelegationLogsHandler returns delegation log entries with costs for a session (mux handler)
func getDelegationLogsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]
		if sessionID == "" {
			http.Error(w, "session_id is required", http.StatusBadRequest)
			return
		}

		// Check session exists
		userID := GetUserIDFromContext(r.Context())
		_, err := db.GetChatSessionWithUser(r.Context(), sessionID, userID)
		if err != nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}

		// Get all events for this session
		dbEvents, err := db.GetEventsBySession(r.Context(), sessionID, 10000, 0)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to get events: %v", err), http.StatusInternalServerError)
			return
		}

		// Also fetch sub-agent session events
		sqlDB := db.GetDB()
		var subSessionEvents []database.Event
		if sqlDB != nil {
			subSessionPrefix := sessionID + "-sub-"
			query := `SELECT session_id FROM chat_sessions WHERE session_id LIKE ?`
			if isPostgresDB(db) {
				query = `SELECT session_id FROM chat_sessions WHERE session_id LIKE $1`
			}
			rows, err := sqlDB.QueryContext(r.Context(),
				query,
				subSessionPrefix+"%",
			)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var subSessionID string
					if err := rows.Scan(&subSessionID); err != nil {
						continue
					}
					subEvents, err := db.GetEventsBySession(r.Context(), subSessionID, 10000, 0)
					if err == nil {
						subSessionEvents = append(subSessionEvents, subEvents...)
					}
				}
			}
		}

		allEvents := append(dbEvents, subSessionEvents...)

		// Build delegation entries from start/end events and aggregate token_usage
		delegationMap := make(map[string]*DelegationLogEntry)
		var delegationOrder []string

		for _, dbEvent := range allEvents {
			var wrapper struct {
				CorrelationID string          `json:"correlation_id"`
				Timestamp     time.Time       `json:"timestamp"`
				Data          json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(dbEvent.EventData, &wrapper); err != nil {
				continue
			}

			switch dbEvent.EventType {
			case "delegation_start":
				var startData struct {
					DelegationID      string   `json:"delegation_id"`
					Depth             int      `json:"depth"`
					Instruction       string   `json:"instruction"`
					ReasoningLevel    string   `json:"reasoning_level"`
					ModelID           string   `json:"model_id"`
					ToolMode          string   `json:"tool_mode"`
					Servers           []string `json:"servers"`
					BackgroundAgentID string   `json:"background_agent_id"`
					Timestamp         string   `json:"timestamp"`
				}
				if err := json.Unmarshal(wrapper.Data, &startData); err != nil {
					continue
				}

				entry := &DelegationLogEntry{
					DelegationID:      startData.DelegationID,
					Instruction:       startData.Instruction,
					ReasoningLevel:    startData.ReasoningLevel,
					ModelID:           startData.ModelID,
					ToolMode:          startData.ToolMode,
					Servers:           startData.Servers,
					BackgroundAgentID: startData.BackgroundAgentID,
					Depth:             startData.Depth,
					Status:            "running",
					StartTime:         startData.Timestamp,
					TokenUsage:        make(map[string]*ChatModelUsage),
				}
				delegationMap[startData.DelegationID] = entry
				delegationOrder = append(delegationOrder, startData.DelegationID)

			case "delegation_end":
				var endData struct {
					DelegationID string `json:"delegation_id"`
					Result       string `json:"result"`
					Error        string `json:"error"`
					Success      bool   `json:"success"`
					Timestamp    string `json:"timestamp"`
					InputTokens  int64  `json:"input_tokens"`
					OutputTokens int64  `json:"output_tokens"`
					ToolCalls    int64  `json:"tool_calls"`
					Duration     string `json:"duration"`
				}
				if err := json.Unmarshal(wrapper.Data, &endData); err != nil {
					continue
				}

				entry, ok := delegationMap[endData.DelegationID]
				if !ok {
					continue
				}

				if endData.Success {
					entry.Status = "completed"
				} else {
					entry.Status = "failed"
				}
				entry.EndTime = endData.Timestamp
				entry.Duration = endData.Duration
				entry.Result = endData.Result
				entry.Error = endData.Error
				entry.InputTokens = endData.InputTokens
				entry.OutputTokens = endData.OutputTokens
				entry.ToolCalls = endData.ToolCalls

			case "token_usage":
				correlationID := wrapper.CorrelationID
				if correlationID == "" {
					continue
				}

				entry, ok := delegationMap[correlationID]
				if !ok {
					continue
				}

				tud, err := parseTokenUsageEvent(dbEvent.EventData)
				if err != nil {
					continue
				}

				modelUsage := getOrCreateModelUsage(entry.TokenUsage, tud.ModelID)
				accumulateUsage(modelUsage, &tud)
				entry.TotalCostUSD += tud.TotalCost
			}
		}

		// Build ordered response
		response := DelegationLogsResponse{
			Delegations: make([]DelegationLogEntry, 0, len(delegationOrder)),
			ByModel:     make(map[string]*ChatModelUsage),
		}

		for _, id := range delegationOrder {
			entry := delegationMap[id]
			response.Delegations = append(response.Delegations, *entry)
			response.TotalCost += entry.TotalCostUSD
			response.TotalInput += entry.InputTokens
			response.TotalOutput += entry.OutputTokens
			response.TotalCalls++

			for modelID, usage := range entry.TokenUsage {
				aggModel := getOrCreateModelUsage(response.ByModel, modelID)
				mergeUsage(aggModel, usage)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// getAllDelegationLogsHandler returns delegation logs grouped by session with main agent + sub-agent breakdown
func getAllDelegationLogsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
		limitStr := r.URL.Query().Get("limit")

		limit := 50
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}

		sessions, _, err := db.ListChatSessionsWithUser(r.Context(), limit, 0, nil, nil, userID)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to list sessions: %v", err), http.StatusInternalServerError)
			return
		}

		// Build session info map and filter to multi-agent sessions
		type sessionInfo struct {
			summary database.ChatHistorySummary
		}
		var multiAgentSessions []sessionInfo
		for _, s := range sessions {
			cfg, err := database.ChatSessionConfigFromJSON(s.Config)
			if err == nil && cfg != nil && cfg.DelegationMode == "plan" {
				multiAgentSessions = append(multiAgentSessions, sessionInfo{summary: s})
			}
		}

		response := AllDelegationLogsResponse{
			Sessions: make([]SessionDelegationLogs, 0),
			ByModel:  make(map[string]*ChatModelUsage),
		}

		for _, si := range multiAgentSessions {
			sid := si.summary.SessionID
			dbEvents, err := db.GetEventsBySession(r.Context(), sid, 10000, 0)
			if err != nil {
				continue
			}

			sessLog := SessionDelegationLogs{
				SessionID:   sid,
				Title:       si.summary.Title,
				CreatedAt:   si.summary.CreatedAt,
				Status:      si.summary.Status,
				Delegations: make([]DelegationLogEntry, 0),
				ByModel:     make(map[string]*ChatModelUsage),
				MainAgent: AgentCostSummary{
					Name:    "Main Agent",
					ByModel: make(map[string]*ChatModelUsage),
				},
			}

			delegationMap := make(map[string]*DelegationLogEntry)
			var delegationOrder []string
			// Track which correlation_ids belong to delegations
			delegationCorrelations := make(map[string]bool)

			for _, dbEvent := range dbEvents {
				var wrapper struct {
					CorrelationID string          `json:"correlation_id"`
					Data          json.RawMessage `json:"data"`
				}
				if err := json.Unmarshal(dbEvent.EventData, &wrapper); err != nil {
					continue
				}

				switch dbEvent.EventType {
				case "delegation_start":
					var startData struct {
						DelegationID      string   `json:"delegation_id"`
						Depth             int      `json:"depth"`
						Instruction       string   `json:"instruction"`
						ReasoningLevel    string   `json:"reasoning_level"`
						ModelID           string   `json:"model_id"`
						ToolMode          string   `json:"tool_mode"`
						Servers           []string `json:"servers"`
						BackgroundAgentID string   `json:"background_agent_id"`
						Timestamp         string   `json:"timestamp"`
					}
					if err := json.Unmarshal(wrapper.Data, &startData); err != nil {
						continue
					}
					entry := &DelegationLogEntry{
						DelegationID:      startData.DelegationID,
						SessionID:         sid,
						Instruction:       startData.Instruction,
						ReasoningLevel:    startData.ReasoningLevel,
						ModelID:           startData.ModelID,
						ToolMode:          startData.ToolMode,
						Servers:           startData.Servers,
						BackgroundAgentID: startData.BackgroundAgentID,
						Depth:             startData.Depth,
						Status:            "running",
						StartTime:         startData.Timestamp,
						TokenUsage:        make(map[string]*ChatModelUsage),
					}
					delegationMap[startData.DelegationID] = entry
					delegationOrder = append(delegationOrder, startData.DelegationID)
					delegationCorrelations[startData.DelegationID] = true

				case "delegation_end":
					var endData struct {
						DelegationID string `json:"delegation_id"`
						Result       string `json:"result"`
						Error        string `json:"error"`
						Success      bool   `json:"success"`
						Timestamp    string `json:"timestamp"`
						InputTokens  int64  `json:"input_tokens"`
						OutputTokens int64  `json:"output_tokens"`
						ToolCalls    int64  `json:"tool_calls"`
						Duration     string `json:"duration"`
					}
					if err := json.Unmarshal(wrapper.Data, &endData); err != nil {
						continue
					}
					entry, ok := delegationMap[endData.DelegationID]
					if !ok {
						continue
					}
					if endData.Success {
						entry.Status = "completed"
					} else {
						entry.Status = "failed"
					}
					entry.EndTime = endData.Timestamp
					entry.Duration = endData.Duration
					entry.Result = endData.Result
					entry.Error = endData.Error
					entry.InputTokens = endData.InputTokens
					entry.OutputTokens = endData.OutputTokens
					entry.ToolCalls = endData.ToolCalls

				case "token_usage":
					tud, err := parseTokenUsageEvent(dbEvent.EventData)
					if err != nil {
						continue
					}

					correlationID := wrapper.CorrelationID
					if correlationID != "" {
						if entry, ok := delegationMap[correlationID]; ok {
							// Sub-agent token usage
							modelUsage := getOrCreateModelUsage(entry.TokenUsage, tud.ModelID)
							accumulateUsage(modelUsage, &tud)
							entry.TotalCostUSD += tud.TotalCost
							continue
						}
					}

					// Main agent token usage (no correlation_id or doesn't match a delegation)
					// NOTE: Main agent emits CUMULATIVE token_usage events (one per user turn).
					// Each event contains the running total, so we REPLACE to avoid double-counting.
					mainModel := getOrCreateModelUsage(sessLog.MainAgent.ByModel, tud.ModelID)
					replaceUsage(mainModel, &tud)
					sessLog.MainAgent.InputTokens = int64(tud.PromptTokens)
					sessLog.MainAgent.OutputTokens = int64(tud.CompletionTokens)
					sessLog.MainAgent.TotalCostUSD = tud.TotalCost
					sessLog.MainAgent.LLMCalls++
				}
			}

			// Build session-level aggregates
			for _, id := range delegationOrder {
				entry := delegationMap[id]
				sessLog.Delegations = append(sessLog.Delegations, *entry)
				for modelID, usage := range entry.TokenUsage {
					aggModel := getOrCreateModelUsage(sessLog.ByModel, modelID)
					mergeUsage(aggModel, usage)
				}
			}

			// Add main agent costs to session totals
			for modelID, usage := range sessLog.MainAgent.ByModel {
				aggModel := getOrCreateModelUsage(sessLog.ByModel, modelID)
				mergeUsage(aggModel, usage)
			}

			// Compute session totals
			for _, usage := range sessLog.ByModel {
				sessLog.TotalCost += usage.TotalCost
				sessLog.TotalInput += int64(usage.InputTokens)
				sessLog.TotalOutput += int64(usage.OutputTokens)
				sessLog.TotalCalls += int64(usage.LLMCallCount)
			}

			// Only include sessions that have delegations or main agent costs
			if len(sessLog.Delegations) > 0 || sessLog.MainAgent.LLMCalls > 0 {
				response.Sessions = append(response.Sessions, sessLog)
				response.TotalCost += sessLog.TotalCost
				response.TotalInput += sessLog.TotalInput
				response.TotalOutput += sessLog.TotalOutput
				response.TotalCalls += sessLog.TotalCalls
				for modelID, usage := range sessLog.ByModel {
					aggModel := getOrCreateModelUsage(response.ByModel, modelID)
					mergeUsage(aggModel, usage)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// getDelegationEventsHandler returns events for a specific delegation (drill-down, mux handler)
func getDelegationEventsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]
		delegationID := vars["delegation_id"]

		if sessionID == "" || delegationID == "" {
			http.Error(w, "session_id and delegation_id are required", http.StatusBadRequest)
			return
		}

		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		limit := 500
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}
		offset := 0
		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
				offset = o
			}
		}

		// Query events with matching correlation_id
		dbEvents, err := db.GetEventsByCorrelationID(r.Context(), sessionID, delegationID, limit, offset)
		if err != nil || len(dbEvents) == 0 {
			// Fallback: try sub-agent session
			subSessionID := sessionID + "-sub-" + delegationID
			dbEvents, err = db.GetEventsBySession(r.Context(), subSessionID, limit, offset)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to get delegation events: %v", err), http.StatusInternalServerError)
				return
			}
		}

		type rawEvent struct {
			ID        string          `json:"id"`
			Type      string          `json:"type"`
			Timestamp time.Time       `json:"timestamp"`
			SessionID string          `json:"session_id"`
			Data      json.RawMessage `json:"data"`
		}

		events := make([]rawEvent, 0, len(dbEvents))
		for _, dbEvent := range dbEvents {
			events = append(events, rawEvent{
				ID:        dbEvent.ID,
				Type:      dbEvent.EventType,
				Timestamp: dbEvent.Timestamp,
				SessionID: sessionID,
				Data:      dbEvent.EventData,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"events": events,
			"total":  len(events),
		})
	}
}

// costEvent is a lightweight struct for batch cost queries (holds either token_usage or delegation_start data)
type costEvent struct {
	eventType    string         // "token_usage" or "delegation_start"
	tokenData    tokenUsageData // populated for token_usage events
	delegationID string         // populated for delegation_start events
	bgAgentID    string         // populated for delegation_start events
}

// batchGetCostEvents fetches token_usage and delegation_start events for multiple sessions in a single query
func batchGetCostEvents(ctx context.Context, db database.Database, sessionIDs []string) (map[string][]costEvent, error) {
	result := make(map[string][]costEvent)
	if len(sessionIDs) == 0 {
		return result, nil
	}

	sqlDB := db.GetDB()
	if sqlDB == nil {
		return result, fmt.Errorf("no database connection")
	}

	// Resolve session UUIDs to internal IDs
	// Build placeholder string for IN clause
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, sid := range sessionIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = sid
	}
	inClause := "(" + strings.Join(placeholders, ",") + ")"

	// Step 1: Resolve session_id UUIDs -> internal hex IDs
	resolveQuery := `SELECT id, session_id FROM chat_sessions WHERE session_id IN ` + inClause
	rows, err := sqlDB.QueryContext(ctx, resolveQuery, args...)
	if err != nil {
		return result, fmt.Errorf("failed to resolve session IDs: %w", err)
	}

	internalToUUID := make(map[string]string) // internal hex ID -> UUID
	internalIDs := make([]string, 0, len(sessionIDs))
	for rows.Next() {
		var internalID, uuid string
		if err := rows.Scan(&internalID, &uuid); err != nil {
			continue
		}
		internalToUUID[internalID] = uuid
		internalIDs = append(internalIDs, internalID)
	}
	rows.Close()

	if len(internalIDs) == 0 {
		return result, nil
	}

	// Step 2: Batch query events filtered by event_type
	placeholders2 := make([]string, len(internalIDs))
	args2 := make([]interface{}, len(internalIDs)+2)
	args2[0] = "token_usage"
	args2[1] = "delegation_start"
	for i, id := range internalIDs {
		placeholders2[i] = fmt.Sprintf("$%d", i+3)
		args2[i+2] = id
	}
	inClause2 := "(" + strings.Join(placeholders2, ",") + ")"

	eventsQuery := `SELECT chat_session_id, event_type, event_data FROM events
		WHERE event_type IN ($1, $2) AND chat_session_id IN ` + inClause2 + `
		ORDER BY timestamp ASC`

	eventRows, err := sqlDB.QueryContext(ctx, eventsQuery, args2...)
	if err != nil {
		return result, fmt.Errorf("failed to batch query events: %w", err)
	}
	defer eventRows.Close()

	for eventRows.Next() {
		var chatSessionID, eventType, eventDataJSON string
		if err := eventRows.Scan(&chatSessionID, &eventType, &eventDataJSON); err != nil {
			continue
		}

		// Map internal ID back to UUID
		uuid, ok := internalToUUID[chatSessionID]
		if !ok {
			continue
		}

		eventData := json.RawMessage(eventDataJSON)

		if eventType == "token_usage" {
			tud, err := parseTokenUsageEvent(eventData)
			if err != nil {
				continue
			}
			result[uuid] = append(result[uuid], costEvent{
				eventType: "token_usage",
				tokenData: tud,
			})
		} else if eventType == "delegation_start" {
			var wrapper struct {
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(eventData, &wrapper); err != nil {
				continue
			}
			var delegData struct {
				DelegationID      string `json:"delegation_id"`
				BackgroundAgentID string `json:"background_agent_id"`
			}
			if err := json.Unmarshal(wrapper.Data, &delegData); err != nil {
				continue
			}
			result[uuid] = append(result[uuid], costEvent{
				eventType:    "delegation_start",
				delegationID: delegData.DelegationID,
				bgAgentID:    delegData.BackgroundAgentID,
			})
		}
	}

	return result, nil
}

// --- ACTIVE SESSION MANAGEMENT ---

// trackActiveSession tracks a new active session
func (api *StreamingAPI) trackActiveSession(sessionID, agentMode, query, userID string) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()

	api.activeSessions[sessionID] = &ActiveSessionInfo{
		SessionID:    sessionID,
		AgentMode:    agentMode,
		Status:       "running",
		LastActivity: time.Now(),
		CreatedAt:    time.Now(),
		Query:        query,
		UserID:       userID,
	}

	log.Printf("[ACTIVE_SESSION] Tracked active session: %s (mode: %s, user: %s)", sessionID, agentMode, userID)
}

// updateSessionActivity updates the LastActivity timestamp for a session when events are added
func (api *StreamingAPI) updateSessionActivity(sessionID string) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()

	if session, exists := api.activeSessions[sessionID]; exists {
		session.LastActivity = time.Now()
		// Don't log every activity update to avoid log spam
	}
}

// updateSessionStatus updates the status of an active session
func (api *StreamingAPI) updateSessionStatus(sessionID, status string) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()

	if session, exists := api.activeSessions[sessionID]; exists {
		session.Status = status
		session.LastActivity = time.Now()
		log.Printf("[ACTIVE_SESSION] Updated session %s status to: %s", sessionID, status)
	} else {
		log.Printf("[ACTIVE_SESSION] Session %s not found in activeSessions, updating database only", sessionID)
	}

	// Always update the database, regardless of whether session is in activeSessions
	go func() {
		ctx := context.Background()
		var completedAt *time.Time
		if status == "completed" {
			now := time.Now()
			completedAt = &now
		}

		log.Printf("[ACTIVE_SESSION] Updating database for session %s status to: %s", sessionID, status)
		_, err := api.chatDB.UpdateChatSession(ctx, sessionID, &database.UpdateChatSessionRequest{
			Status:      status,
			CompletedAt: completedAt,
		})
		if err != nil {
			log.Printf("[ACTIVE_SESSION] Failed to update database for session %s: %v", sessionID, err)
		} else {
			log.Printf("[ACTIVE_SESSION] Successfully updated database for session %s status to: %s", sessionID, status)
		}

		// NOTE: Completed sessions are NOT removed from activeSessions map immediately.
		// They remain in the map so getAllActiveSessions can include them in the 30-minute
		// window for page refresh restoration. The cleanupInactiveSessions goroutine will
		// eventually remove old sessions.
	}()
}

// handleDismissSession marks a session as dismissed so it won't be auto-restored on page refresh.
func (api *StreamingAPI) handleDismissSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	sessionID := vars["session_id"]

	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Update in-memory status
	api.activeSessionsMux.Lock()
	if session, exists := api.activeSessions[sessionID]; exists {
		session.Status = "dismissed"
		session.LastActivity = time.Now()
	}
	api.activeSessionsMux.Unlock()

	// Also update DB so the dismiss survives backend restarts.
	// The DB fallback in handleGetActiveSessions checks status — "dismissed" won't match.
	if api.chatDB != nil {
		if _, err := api.chatDB.UpdateChatSession(r.Context(), sessionID, &database.UpdateChatSessionRequest{
			Status: "dismissed",
		}); err != nil {
			log.Printf("[ACTIVE_SESSION] Failed to dismiss session %s in DB: %v", sessionID, err)
		}
	}

	log.Printf("[ACTIVE_SESSION] Dismissed session %s", sessionID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "dismissed",
		"session": sessionID,
	})
}

// planEventEmitter implements virtualtools.PlanEventEmitter to emit workspace_file_operation events
// when delegation plan files are saved, so the workspace sidebar highlights them.
type planEventEmitter struct {
	eventStore *events.EventStore
	sessionID  string
	chatDB     database.Database
}

func (e *planEventEmitter) EmitFileEvent(filepath string) {
	now := time.Now()
	eventData := unifiedevents.NewWorkspaceFileOperationEvent("update", filepath, virtualtools.PlanFileFolderPath, 0, "delegation")
	event := events.Event{
		ID:        fmt.Sprintf("%s_plan_file_%d", e.sessionID, now.UnixNano()),
		Type:      "workspace_file_operation",
		Timestamp: now,
		SessionID: e.sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:      unifiedevents.WorkspaceFileOperation,
			Timestamp: now,
			SessionID: e.sessionID,
			Component: "delegation",
			Data:      eventData,
		},
	}
	e.eventStore.AddEvent(e.sessionID, event)
	if e.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := e.chatDB.StoreEvent(context.Background(), e.sessionID, event.Data); err != nil {
			log.Printf("[DELEGATION PLAN] Failed to persist event to DB: %v", err)
		}
	}
	log.Printf("[DELEGATION PLAN] Emitted workspace_file_operation event for plan file: %s (session: %s)", filepath, e.sessionID)
}

// sessionEventEmitter implements virtualtools.SessionEventEmitter to emit
// blocking_human_feedback events for the confirm_plan_execution tool.
type sessionEventEmitter struct {
	eventStore *events.EventStore
	sessionID  string
	chatDB     database.Database
}

func (e *sessionEventEmitter) EmitBlockingHumanFeedback(requestID, question, contextText string, yesNoOnly bool, yesLabel, noLabel string, options ...string) {
	now := time.Now()
	eventData := &orchEvents.BlockingHumanFeedbackEvent{
		BaseEventData: unifiedevents.BaseEventData{
			Timestamp: now,
		},
		Question:      question,
		AllowFeedback: !yesNoOnly && len(options) == 0, // Allow text input when not yes/no and no options
		Context:       contextText,
		SessionID:     e.sessionID,
		RequestID:     requestID,
		YesNoOnly:     yesNoOnly,
		YesLabel:      yesLabel,
		NoLabel:       noLabel,
		Options:       options,
	}
	event := events.Event{
		ID:        fmt.Sprintf("%s_plan_approval_%d", e.sessionID, now.UnixNano()),
		Type:      "blocking_human_feedback",
		Timestamp: now,
		SessionID: e.sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:      orchEvents.BlockingHumanFeedback,
			Timestamp: now,
			SessionID: e.sessionID,
			Component: "delegation",
			Data:      eventData,
		},
	}
	e.eventStore.AddEvent(e.sessionID, event)
	if e.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := e.chatDB.StoreEvent(context.Background(), e.sessionID, event.Data); err != nil {
			log.Printf("[PLAN APPROVAL] Failed to persist event to DB: %v", err)
		}
	}
	log.Printf("[PLAN APPROVAL] Emitted blocking_human_feedback event for plan approval (request_id: %s, session: %s)", requestID, e.sessionID)
}

func (e *sessionEventEmitter) EmitPlanApproval(question, contextText, yesLabel string) {
	now := time.Now()
	eventData := &orchEvents.PlanApprovalEvent{
		BaseEventData: unifiedevents.BaseEventData{
			Timestamp: now,
		},
		Question: question,
		Context:  contextText,
		YesLabel: yesLabel,
	}
	event := events.Event{
		ID:        fmt.Sprintf("%s_plan_approval_%d", e.sessionID, now.UnixNano()),
		Type:      "plan_approval",
		Timestamp: now,
		SessionID: e.sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:      orchEvents.PlanApproval,
			Timestamp: now,
			SessionID: e.sessionID,
			Component: "delegation",
			Data:      eventData,
		},
	}
	e.eventStore.AddEvent(e.sessionID, event)
	if e.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := e.chatDB.StoreEvent(context.Background(), e.sessionID, event.Data); err != nil {
			log.Printf("[PLAN APPROVAL] Failed to persist plan_approval to DB: %v", err)
		}
	}
	log.Printf("[PLAN APPROVAL] Emitted plan_approval event (session: %s)", e.sessionID)
}

func (e *sessionEventEmitter) EmitBlockingHumanQuestions(requestID string, questions []map[string]string) {
	now := time.Now()
	// Convert questions to the event struct format
	var eventQuestions []orchEvents.BlockingHumanQuestionsQuestion
	for _, q := range questions {
		eventQuestions = append(eventQuestions, orchEvents.BlockingHumanQuestionsQuestion{
			ID:       q["id"],
			Question: q["question"],
		})
	}
	eventData := &orchEvents.BlockingHumanQuestionsEvent{
		BaseEventData: unifiedevents.BaseEventData{
			Timestamp: now,
		},
		RequestID: requestID,
		Questions: eventQuestions,
		SessionID: e.sessionID,
	}
	event := events.Event{
		ID:        fmt.Sprintf("%s_human_questions_%d", e.sessionID, now.UnixNano()),
		Type:      "blocking_human_questions",
		Timestamp: now,
		SessionID: e.sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:      orchEvents.BlockingHumanQuestions,
			Timestamp: now,
			SessionID: e.sessionID,
			Component: "human",
			Data:      eventData,
		},
	}
	e.eventStore.AddEvent(e.sessionID, event)
	if e.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := e.chatDB.StoreEvent(context.Background(), e.sessionID, event.Data); err != nil {
			log.Printf("[HUMAN QUESTIONS] Failed to persist event to DB: %v", err)
		}
	}
	log.Printf("[HUMAN QUESTIONS] Emitted blocking_human_questions event (request_id: %s, session: %s)", requestID, e.sessionID)
}

// executeDelegatedTask executes a delegated task via a sub-agent.
// onCreated is an optional callback invoked after the sub-agent wrapper is created
// but before Invoke — used by background agents to attach a history func.
func (api *StreamingAPI) executeDelegatedTask(ctx context.Context, parentReq QueryRequest, sessionID string, instruction string, onCreated ...func(wrapper *agent.LLMAgentWrapper)) (string, error) {
	log.Printf("[DELEGATION] Creating sub-agent for delegated task in session %s", sessionID)

	// Check delegation depth from context
	currentDepth := 0
	if depth, ok := ctx.Value(virtualtools.DelegationDepthKey).(int); ok {
		currentDepth = depth
	}

	if currentDepth >= virtualtools.MaxDelegationDepth {
		return "", fmt.Errorf("maximum delegation depth (%d) reached", virtualtools.MaxDelegationDepth)
	}

	// Generate a unique delegation ID for tracking
	delegationID := fmt.Sprintf("delegation-%d-%d", currentDepth, time.Now().UnixNano())

	// Build sub-agent config from parent request
	// Get provider and model from parent request
	provider := llm.Provider(parentReq.Provider)
	if provider == "" {
		provider = llm.Provider("anthropic")
	}
	modelID := parentReq.ModelID
	if modelID == "" {
		modelID = "claude-sonnet-4-20250514"
	}

	// Load sub-agent template if specified
	var loadedTemplate *subagents.SubAgent
	agentTemplateName, _ := ctx.Value(virtualtools.AgentTemplateKey).(string)
	if agentTemplateName != "" {
		workspaceAPIURL := getWorkspaceAPIURL()
		sa, err := subagents.GetSubAgent(workspaceAPIURL, agentTemplateName)
		if err != nil {
			log.Printf("[DELEGATION] Warning: Failed to load sub-agent template %s: %v", agentTemplateName, err)
		} else {
			loadedTemplate = sa
			log.Printf("[DELEGATION] Loaded sub-agent template: %s (%s)", sa.Frontmatter.Name, agentTemplateName)
		}
	}

	// Resolve reasoning level tier to specific provider/model if configured
	reasoningLevel, _ := ctx.Value(virtualtools.ReasoningLevelKey).(string)
	// Apply template defaults if not explicitly set
	if reasoningLevel == "" && loadedTemplate != nil && loadedTemplate.Frontmatter.DefaultReasoningLevel != "" {
		reasoningLevel = loadedTemplate.Frontmatter.DefaultReasoningLevel
		log.Printf("[DELEGATION] Using template default reasoning_level: %s", reasoningLevel)
	}
	if reasoningLevel != "" {
		tierConfig := resolveDelegationTierConfig(parentReq.DelegationTierConfig)
		if tierConfig != nil {
			var tierModel *virtualtools.TierModel
			switch reasoningLevel {
			case "high":
				tierModel = tierConfig.High
			case "medium":
				tierModel = tierConfig.Medium
			case "low":
				tierModel = tierConfig.Low
			default:
				// Custom tier lookup
				if tierConfig.Custom != nil {
					if ct, ok := tierConfig.Custom[reasoningLevel]; ok {
						tierModel = &virtualtools.TierModel{Provider: ct.Provider, ModelID: ct.ModelID}
					}
				}
			}
			if tierModel != nil && tierModel.Provider != "" && tierModel.ModelID != "" {
				provider = llm.Provider(tierModel.Provider)
				modelID = tierModel.ModelID
				log.Printf("[DELEGATION] Using tier %s model: %s/%s", reasoningLevel, tierModel.Provider, tierModel.ModelID)
			}
		}
	}

	// Resolve tool_mode for sub-agent
	toolMode, _ := ctx.Value(virtualtools.ToolModeKey).(string)
	if toolMode == "" && loadedTemplate != nil && loadedTemplate.Frontmatter.DefaultToolMode != "" {
		toolMode = loadedTemplate.Frontmatter.DefaultToolMode
		log.Printf("[DELEGATION] Using template default tool_mode: %s", toolMode)
	}

	// Build server name — use delegation-specific servers if provided, otherwise all parent servers
	var serverName string
	var serversList []string
	if delegationServers, ok := ctx.Value(virtualtools.DelegationServersKey).([]string); ok && len(delegationServers) > 0 {
		serverName = strings.Join(delegationServers, ",")
		serversList = delegationServers
		log.Printf("[DELEGATION] Using sub-agent specific servers: %s", serverName)
	} else if len(parentReq.EnabledServers) > 0 {
		serverName = strings.Join(parentReq.EnabledServers, ",")
		serversList = parentReq.EnabledServers
	} else if len(parentReq.Servers) > 0 {
		serverName = strings.Join(parentReq.Servers, ",")
		serversList = parentReq.Servers
	}

	// Auto-enable tool search mode for sub-agents with many MCP servers (>3)
	// This prevents overwhelming the LLM with too many tool definitions
	useToolSearch := toolMode == "tool_search"
	useCodeExec := toolMode == "code_execution"
	if !useToolSearch && !useCodeExec {
		realServerCount := 0
		for _, s := range serversList {
			if s != "all" && s != mcpclient.NoServers {
				realServerCount++
			}
		}
		if realServerCount > 3 {
			useToolSearch = true
			toolMode = "tool_search"
			log.Printf("[DELEGATION] Auto-enabling tool search mode for sub-agent — %d MCP servers (>3)", realServerCount)
		}
	}

	// Extract background agent ID if this delegation was spawned by a background agent
	backgroundAgentID, _ := ctx.Value(virtualtools.BackgroundAgentIDKey).(string)

	// Emit delegation_start event (after model and server resolution so we can include all info)
	api.emitDelegationStartEvent(sessionID, delegationID, currentDepth, instruction, reasoningLevel, modelID, toolMode, serversList, backgroundAgentID)

	// Convert API keys from parent request to LLM format (respecting locked providers)
	var apiKeys *llm.ProviderAPIKeys = &llm.ProviderAPIKeys{}

	// 1. Start with keys from parent request
	if parentReq.LLMConfig != nil && parentReq.LLMConfig.APIKeys != nil {
		apiKeys.OpenRouter = parentReq.LLMConfig.APIKeys.OpenRouter
		apiKeys.OpenAI = parentReq.LLMConfig.APIKeys.OpenAI
		apiKeys.Anthropic = parentReq.LLMConfig.APIKeys.Anthropic
		apiKeys.Vertex = parentReq.LLMConfig.APIKeys.Vertex
		apiKeys.GeminiCLI = parentReq.LLMConfig.APIKeys.GeminiCLI
		if parentReq.LLMConfig.APIKeys.Bedrock != nil {
			apiKeys.Bedrock = &llm.BedrockConfig{Region: parentReq.LLMConfig.APIKeys.Bedrock.Region}
		}
		if parentReq.LLMConfig.APIKeys.Azure != nil {
			apiKeys.Azure = &llm.AzureAPIConfig{
				Endpoint:   parentReq.LLMConfig.APIKeys.Azure.Endpoint,
				APIKey:     parentReq.LLMConfig.APIKeys.Azure.APIKey,
				APIVersion: parentReq.LLMConfig.APIKeys.Azure.APIVersion,
				Region:     parentReq.LLMConfig.APIKeys.Azure.Region,
			}
		}
	}

	// 2. Override if global lock is on OR specific provider is locked
	envKeys := buildProviderAPIKeysFromEnv()
	globalLocked := isGlobalLLMConfigLocked()

	if globalLocked {
		// Force single provider if global lock is on
		provStr, modelStr := getPrimaryProviderAndModelFromDefaults()
		supported := getSupportedProviders()
		if len(supported) > 0 {
			allowed := make(map[string]bool)
			for _, p := range supported {
				allowed[p] = true
			}
			if !allowed[provStr] {
				provStr = supported[0]
				modelStr = llm.GetDefaultModel(llm.Provider(provStr))
			}
		}
		provider = llm.Provider(provStr)
		modelID = modelStr
		// Use all env keys for simplicity/security in global lock mode
		apiKeys = envKeys
	} else {
		// Partial locking: override keys for locked providers
		if isProviderLocked("openrouter") {
			apiKeys.OpenRouter = envKeys.OpenRouter
		}
		if isProviderLocked("openai") {
			apiKeys.OpenAI = envKeys.OpenAI
		}
		if isProviderLocked("anthropic") {
			apiKeys.Anthropic = envKeys.Anthropic
		}
		if isProviderLocked("vertex") {
			apiKeys.Vertex = envKeys.Vertex
		}
		if isProviderLocked("bedrock") {
			apiKeys.Bedrock = envKeys.Bedrock
		}
		if isProviderLocked("azure") {
			apiKeys.Azure = envKeys.Azure
		}
		if isProviderLocked("gemini-cli") {
			apiKeys.GeminiCLI = envKeys.GeminiCLI
		}
	}

	// Get user ID from context for per-user OAuth token isolation
	subAgentUserID := ""
	if userID, ok := ctx.Value(common.UserIDKey).(string); ok {
		subAgentUserID = userID
	}
	log.Printf("[USER_ID_DEBUGGING] Sub-agent: subAgentUserID=%q (from parent context UserIDKey)", subAgentUserID)

	// Create sub-agent config based on parent request
	subAgentConfig := agent.LLMAgentConfig{
		Name:       fmt.Sprintf("%s-sub-%d-%d", sessionID, currentDepth, time.Now().UnixNano()),
		ServerName: serverName,
		ConfigPath: api.mcpConfigPath,
		Provider:   provider,
		ModelID:    modelID,
		Temperature: func() float64 {
			if parentReq.Temperature > 0 {
				return parentReq.Temperature
			}
			return 0.7
		}(),
		MaxTurns: func() int {
			if parentReq.MaxTurns > 0 {
				return parentReq.MaxTurns
			}
			return 100
		}(),
		ToolChoice:         "", // Empty — let the library decide; Azure/OpenAI reject tool_choice when no tools are present
		StreamingChunkSize: 1,
		// No Timeout set — sub-agent lifetime is controlled by the parent context.
		ToolTimeout: func() time.Duration {
			if envVal := os.Getenv("TOOL_EXECUTION_TIMEOUT"); envVal != "" {
				if timeout, err := time.ParseDuration(envVal); err == nil && timeout > 0 {
					return timeout
				}
			}
			return 5 * time.Minute
		}(),
		// Sub-agent mode uses the resolved values (from delegate call, template default, or auto-enable).
		UseCodeExecutionMode: useCodeExec,
		UseToolSearchMode:    useToolSearch,
		// Pre-discovered tools: all virtual/custom tools stay visible in tool search mode
		// Only MCP server tools require discovery via search_tools
		PreDiscoveredTools: func() []string {
			if !useToolSearch {
				return nil
			}
			preDiscovered := collectVirtualToolNames(
				virtualtools.CreateWorkspaceAdvancedTools(),
				virtualtools.CreateHumanTools(),
			)
			if parentReq.EnableBrowserAccess != nil && *parentReq.EnableBrowserAccess {
				preDiscovered = append(preDiscovered, collectVirtualToolNames(virtualtools.CreateWorkspaceBrowserTools())...)
			}
			return preDiscovered
		}(),
		APIKeys:   apiKeys,
		SessionID: sessionID,      // Reuse parent session's MCP connections via registry
		UserID:    subAgentUserID, // Per-user OAuth token isolation
		// Context offloading: inherit from environment
		LargeOutputThreshold: func() int {
			if envVal := os.Getenv("LARGE_OUTPUT_THRESHOLD"); envVal != "" {
				if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
					return threshold
				}
			}
			return 0
		}(),
		// Context summarization: inherit from parent request > env > defaults
		EnableContextSummarization: func() bool {
			if parentReq.EnableContextSummarization != nil {
				return *parentReq.EnableContextSummarization
			}
			if envVal := os.Getenv("ENABLE_CONTEXT_SUMMARIZATION"); envVal == "false" {
				return false
			}
			return true
		}(),
		SummarizeOnTokenThreshold: func() bool {
			if parentReq.SummarizeOnTokenThreshold != nil {
				return *parentReq.SummarizeOnTokenThreshold
			}
			if envVal := os.Getenv("SUMMARIZE_ON_TOKEN_THRESHOLD"); envVal == "false" {
				return false
			}
			return true
		}(),
		TokenThresholdPercent: func() float64 {
			if parentReq.TokenThresholdPercent > 0 {
				return parentReq.TokenThresholdPercent
			}
			if envVal := os.Getenv("TOKEN_THRESHOLD_PERCENT"); envVal != "" {
				if threshold, err := strconv.ParseFloat(envVal, 64); err == nil && threshold > 0 && threshold <= 1.0 {
					return threshold
				}
			}
			return 0.8
		}(),
		SummarizeOnFixedTokenThreshold: func() bool {
			if parentReq.SummarizeOnFixedTokenThreshold != nil {
				return *parentReq.SummarizeOnFixedTokenThreshold
			}
			if envVal := os.Getenv("SUMMARIZE_ON_FIXED_TOKEN_THRESHOLD"); envVal == "false" {
				return false
			}
			return true
		}(),
		FixedTokenThreshold: func() int {
			if parentReq.FixedTokenThreshold > 0 {
				return parentReq.FixedTokenThreshold
			}
			if envVal := os.Getenv("FIXED_TOKEN_THRESHOLD"); envVal != "" {
				if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
					return threshold
				}
			}
			return 200000
		}(),
		SummaryKeepLastMessages: func() int {
			if parentReq.SummaryKeepLastMessages > 0 {
				return parentReq.SummaryKeepLastMessages
			}
			if envVal := os.Getenv("SUMMARY_KEEP_LAST_MESSAGES"); envVal != "" {
				if keepLast, err := strconv.Atoi(envVal); err == nil && keepLast > 0 {
					return keepLast
				}
			}
			return 4
		}(),
		// Context editing: inherit from parent request > env > defaults
		EnableContextEditing: func() bool {
			if parentReq.EnableContextEditing != nil {
				return *parentReq.EnableContextEditing
			}
			if envVal := os.Getenv("ENABLE_CONTEXT_EDITING"); envVal == "true" {
				return true
			}
			return false
		}(),
		ContextEditingThreshold: func() int {
			if parentReq.ContextEditingThreshold > 0 {
				return parentReq.ContextEditingThreshold
			}
			if envVal := os.Getenv("CONTEXT_EDITING_THRESHOLD"); envVal != "" {
				if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
					return threshold
				}
			}
			return 0
		}(),
		ContextEditingTurnThreshold: func() int {
			if parentReq.ContextEditingTurnThreshold > 0 {
				return parentReq.ContextEditingTurnThreshold
			}
			if envVal := os.Getenv("CONTEXT_EDITING_TURN_THRESHOLD"); envVal != "" {
				if turnThreshold, err := strconv.Atoi(envVal); err == nil && turnThreshold > 0 {
					return turnThreshold
				}
			}
			return 0
		}(),
		// Parallel tool execution: enabled by default, can be disabled via ENABLE_PARALLEL_TOOL_EXECUTION=false
		EnableParallelToolExecution: func() bool {
			if envVal := os.Getenv("ENABLE_PARALLEL_TOOL_EXECUTION"); envVal == "false" {
				return false
			}
			return true
		}(),
	}

	// Create sub-agent using the wrapper (same as parent agent creation)
	subAgent, err := agent.NewLLMAgentWrapper(ctx, subAgentConfig, nil, api.logger)
	if err != nil {
		api.emitDelegationEndEvent(sessionID, delegationID, currentDepth, "", err.Error(), nil)
		return "", fmt.Errorf("failed to create sub-agent: %w", err)
	}

	// Add event observers to sub-agent so its events appear in the UI
	// Events from sub-agent will be tagged with Component field for identification
	if underlyingAgent := subAgent.GetUnderlyingAgent(); underlyingAgent != nil {
		// Create in-memory event observer for real-time updates
		// DelegationEventObserver tags events with correlation_id/parent_id and also persists to DB
		// (replaces the separate EventDatabaseObserver to avoid untagged duplicates)
		subAgentObserver := events.NewDelegationEventObserver(api.eventStore, sessionID, currentDepth, delegationID, api.logger)
		// Wire tool event callback if provided (background agents use this for timing)
		if toolCb, ok := ctx.Value(virtualtools.ToolEventCallbackKey).(events.ToolEventCallback); ok && toolCb != nil {
			subAgentObserver.OnToolEvent = toolCb
		}
		// Wire DB persistence so tagged sub-agent events are stored for shared sessions / restore
		subAgentObserver.DBStore = func(ctx context.Context, sid string, evt *unifiedevents.AgentEvent) error {
			return api.chatDB.StoreEvent(ctx, sid, evt)
		}
		underlyingAgent.AddEventListener(subAgentObserver)
		log.Printf("[DELEGATION] Added event observers for sub-agent at depth %d", currentDepth)

		// Merge template skills/servers into parent request if a template is loaded
		if loadedTemplate != nil {
			templateSkills := subagents.ParseCSV(loadedTemplate.Frontmatter.Skills)
			for _, ts := range templateSkills {
				found := false
				for _, ps := range parentReq.SelectedSkills {
					if ps == ts {
						found = true
						break
					}
				}
				if !found {
					parentReq.SelectedSkills = append(parentReq.SelectedSkills, ts)
				}
			}
			templateServers := subagents.ParseCSV(loadedTemplate.Frontmatter.Servers)
			for _, ts := range templateServers {
				found := false
				for _, ps := range parentReq.EnabledServers {
					if ps == ts {
						found = true
						break
					}
				}
				if !found {
					parentReq.EnabledServers = append(parentReq.EnabledServers, ts)
				}
			}
			if len(templateSkills) > 0 || len(templateServers) > 0 {
				log.Printf("[DELEGATION] Merged template skills=%v servers=%v into sub-agent config", templateSkills, templateServers)
			}
		}

		// Add skill instructions to sub-agent system prompt (mirrors parent agent setup)
		if len(parentReq.SelectedSkills) > 0 {
			skillPrompt := buildSkillPrompt(parentReq.SelectedSkills)
			if skillPrompt != "" {
				underlyingAgent.AppendSystemPrompt(skillPrompt)
				log.Printf("[DELEGATION] Added skill instructions to sub-agent (%d skills)", len(parentReq.SelectedSkills))
			}
		}

		// Add skill builder instructions for sub-agents when skill-creator is active
		hasSkillCreator := false
		for _, s := range parentReq.SelectedSkills {
			if s == "skill-creator" {
				hasSkillCreator = true
				break
			}
		}
		if hasSkillCreator {
			underlyingAgent.AppendSystemPrompt(GetSkillBuilderInstructions())
			log.Printf("[DELEGATION] Added skill builder instructions to sub-agent")
		}

		// Inject sub-agent template instructions into system prompt
		if loadedTemplate != nil {
			templatePrompt := fmt.Sprintf("\n## Sub-Agent Role: %s\n\n%s\n",
				loadedTemplate.Frontmatter.Name, loadedTemplate.Content)
			underlyingAgent.AppendSystemPrompt(templatePrompt)
			log.Printf("[DELEGATION] Injected sub-agent template instructions: %s", loadedTemplate.Frontmatter.Name)
		}

		// Merge global secrets with parent's decrypted secrets, then inject into sub-agent
		allDelegationSecrets := mergeGlobalSecrets(parentReq.DecryptedSecrets, parentReq.SelectedGlobalSecrets)
		if len(allDelegationSecrets) > 0 {
			var secretParts []string
			for _, s := range allDelegationSecrets {
				secretParts = append(secretParts, fmt.Sprintf("### %s\n```\n%s\n```", s.Name, s.Value))
			}
			secretPrompt := "\n## 🔐 Secrets\n\nThe following secrets/credentials have been provided. Use them as needed:\n\n" + strings.Join(secretParts, "\n")
			underlyingAgent.AppendSystemPrompt(secretPrompt)
			log.Printf("[DELEGATION] Injected %d secrets (%d global + %d user) into sub-agent system prompt", len(allDelegationSecrets), len(allDelegationSecrets)-len(parentReq.DecryptedSecrets), len(parentReq.DecryptedSecrets))
		}
	}

	// Register the same workspace tools as parent (if workspace access is enabled)
	if underlyingAgent := subAgent.GetUnderlyingAgent(); underlyingAgent != nil {
		// Check workspace access from parent request
		enableWorkspaceAccess := true
		if parentReq.EnableWorkspaceAccess != nil {
			enableWorkspaceAccess = *parentReq.EnableWorkspaceAccess
		}
		if len(parentReq.SelectedSkills) > 0 {
			enableWorkspaceAccess = true
		}

		if enableWorkspaceAccess {
			// Sub-agents get advanced workspace tools (shell, image, web fetch, PDF, diff_patch)
			workspaceTools := virtualtools.CreateWorkspaceAdvancedTools()
			workspaceExecutors, _ := virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSession(subAgentUserID, sessionID)
			log.Printf("[USER_ID_DEBUGGING] Sub-agent workspace executors: created with explicit userID=%q sessionID=%q", subAgentUserID, sessionID)
			_, _, toolCategories := createCustomTools(false)

			// Check for skill-creator
			hasSkillCreator := false
			for _, s := range parentReq.SelectedSkills {
				if s == "skill-creator" {
					hasSkillCreator = true
					break
				}
			}

			// Check for subagent-creator
			hasSubAgentCreator := false
			for _, s := range parentReq.SelectedSkills {
				if s == "subagent-creator" || s == "custom/subagent-creator" {
					hasSubAgentCreator = true
					break
				}
			}

			// Extract @context file paths from parent query for additional write access
			fileContextFolders := extractFileContextWriteFolders(parentReq.Query)
			if len(fileContextFolders) > 0 {
				log.Printf("[DELEGATION] Extracted write paths from parent @context: %v", fileContextFolders)
			}

			// Apply folder guards
			// If executing a plan task, restrict writes to the plan's specific folder
			planFolder, _ := ctx.Value(virtualtools.PlanFolderKey).(string)
			if planFolder != "" {
				// Tighter restriction: only allow writes to plan folder (e.g. Plans/{planID}/)
				additionalFolders := []string{}
				if hasSkillCreator {
					additionalFolders = append(additionalFolders, "skills/custom/")
				}
				if hasSubAgentCreator {
					additionalFolders = append(additionalFolders, "subagents/custom/")
				}
				additionalFolders = append(additionalFolders, fileContextFolders...)
				workspaceExecutors = wrapExecutorsWithPlanFolderGuard(workspaceExecutors, planFolder, additionalFolders...)
				log.Printf("[DELEGATION] Applied plan folder guard: writes restricted to %s/", planFolder)
			} else {
				extraFolders := []string{}
				if hasSkillCreator {
					extraFolders = append(extraFolders, "skills/custom/")
				}
				if hasSubAgentCreator {
					extraFolders = append(extraFolders, "subagents/custom/")
				}
				extraFolders = append(extraFolders, fileContextFolders...)
				workspaceExecutors = wrapExecutorsWithChatModeFolderGuard(workspaceExecutors, extraFolders...)
			}

			// Register workspace tools
			for _, tool := range workspaceTools {
				if tool.Function == nil {
					continue
				}
				toolName := tool.Function.Name
				if executor, exists := workspaceExecutors[toolName]; exists {
					enhancedDescription := enhanceToolDescriptionForChatMode(toolName, tool.Function.Description)

					var params map[string]interface{}
					if tool.Function.Parameters != nil {
						paramsBytes, err := json.Marshal(tool.Function.Parameters)
						if err == nil {
							json.Unmarshal(paramsBytes, &params)
						}
					}
					if params == nil {
						continue
					}

					toolCategory := toolCategories[toolName]
					if toolCategory == "" {
						continue
					}

					if err := underlyingAgent.RegisterCustomTool(
						toolName,
						enhancedDescription,
						params,
						executor,
						toolCategory,
					); err != nil {
						log.Printf("[DELEGATION] Warning: Failed to register tool %s for sub-agent: %v", toolName, err)
					}
				}
			}

			// Register browser tools if enabled
			if parentReq.EnableBrowserAccess != nil && *parentReq.EnableBrowserAccess {
				browserTools := virtualtools.CreateWorkspaceBrowserTools()
				browserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutors(getCdpPort(parentReq))
				browserCategory := virtualtools.GetWorkspaceBrowserToolCategory()

				browserExtraFolders := []string{}
				if hasSkillCreator {
					browserExtraFolders = append(browserExtraFolders, "skills/custom/")
				}
				browserExtraFolders = append(browserExtraFolders, fileContextFolders...)
				browserExecutors = wrapExecutorsWithChatModeFolderGuard(browserExecutors, browserExtraFolders...)

				for _, tool := range browserTools {
					if tool.Function == nil {
						continue
					}
					toolName := tool.Function.Name
					if executor, exists := browserExecutors[toolName]; exists {
						var params map[string]interface{}
						if tool.Function.Parameters != nil {
							paramsBytes, err := json.Marshal(tool.Function.Parameters)
							if err == nil {
								json.Unmarshal(paramsBytes, &params)
							}
						}
						if params == nil {
							continue
						}

						if err := underlyingAgent.RegisterCustomTool(
							toolName,
							tool.Function.Description,
							params,
							executor,
							browserCategory,
						); err != nil {
							log.Printf("[DELEGATION] Warning: Failed to register browser tool %s for sub-agent: %v", toolName, err)
						}
					}
				}
			}

			// NOTE: Sub-agents do NOT get the delegate tool themselves (v1 design choice)
			// This prevents runaway delegation chains

			// Add plan update instructions to worker sub-agents
			// Workers update plan.md as they work — adding findings, marking progress, noting issues
			if planFolder != "" {
				planUpdatePrompt := fmt.Sprintf(`
## Workspace Rules
Your workspace folder is: %s/
Save ALL output files (scripts, data, reports, etc.) inside this folder. Do NOT write to Chats/ or any other folder.

## Plan Update Protocol
You are working on a task from the plan at %s/plan.md.
After completing your task, you MUST update the plan file:
1. **Mark your task**: Change '[ ]' to '[x]' for your completed task using sed
2. **Add key knowledge**: Append important discoveries to the '## Key Knowledge' section — things that other workers (who start fresh with NO context) need to know. Examples: file paths created/modified, API endpoints discovered, configuration values, naming conventions found, gotchas or constraints discovered.
3. **Add results**: Append a brief summary of what you did to the '## Notes' section
4. **Report issues**: If something failed or was unexpected, note it in '## Notes'

This is critical — the manager reads plan.md after you finish and passes your Key Knowledge to the next worker. Without your updates, the next worker will have no context about what you discovered.

Use execute_shell_command or diff_patch_workspace_file to update the file.
- For appending: execute_shell_command(command: "echo '\n- [task-N result]: Summary of findings' >> %s/plan.md", working_directory: ".")
- For precise edits (marking checkboxes, updating sections): diff_patch_workspace_file with filepath "%s/plan.md"
`, planFolder, planFolder, planFolder, planFolder)
				underlyingAgent.AppendSystemPrompt(planUpdatePrompt)
				log.Printf("[DELEGATION] Added plan update instructions for plan folder: %s", planFolder)
			}
		}
	}

	log.Printf("[DELEGATION] Sub-agent created, executing instruction at depth %d", currentDepth)

	// Notify caller that the sub-agent wrapper is ready (used by background agents)
	if len(onCreated) > 0 && onCreated[0] != nil {
		onCreated[0](subAgent)
	}

	// Run the sub-agent with the instruction
	startTime := time.Now()
	result, err := subAgent.Invoke(ctx, instruction)
	duration := time.Since(startTime)

	// Collect metrics from sub-agent
	metrics := subAgent.GetMetricsSnapshot()
	stats := &delegationEndStats{
		InputTokens:  metrics.InputTokens,
		OutputTokens: metrics.OutputTokens,
		ToolCalls:    metrics.ToolCallsExecuted,
		Duration:     duration.String(),
		TotalCostUSD: metrics.TotalCostUSD,
	}

	if err != nil {
		api.emitDelegationEndEvent(sessionID, delegationID, currentDepth, "", err.Error(), stats)
		return "", fmt.Errorf("sub-agent execution failed: %w", err)
	}

	// Emit delegation_end event with success
	api.emitDelegationEndEvent(sessionID, delegationID, currentDepth, fmt.Sprintf("Completed in %s", duration), "", stats)

	log.Printf("[DELEGATION] Sub-agent completed at depth %d in %s", currentDepth, duration)
	return result, nil
}

// --- Background Agent Infrastructure for Async Delegation ---

// bgAgentQuerierImpl implements virtualtools.BGAgentQuerier using the registry
type bgAgentQuerierImpl struct {
	registry *BackgroundAgentRegistry
}

func (q *bgAgentQuerierImpl) QueryAgent(sessionID, agentID string, last, offset int) (*virtualtools.BGAgentInfo, error) {
	agent := q.registry.Get(sessionID, agentID)
	if agent == nil {
		return nil, fmt.Errorf("agent %s not found", agentID)
	}
	snap := agent.GetSnapshot()
	elapsed := time.Since(snap.CreatedAt)
	if snap.CompletedAt != nil {
		elapsed = snap.CompletedAt.Sub(snap.CreatedAt)
	}
	info := &virtualtools.BGAgentInfo{
		ID:        snap.ID,
		Name:      snap.Name,
		Status:    string(snap.Status),
		Elapsed:   elapsed.Truncate(time.Second).String(),
		CreatedAt: snap.CreatedAt.Format(time.RFC3339),
	}
	if snap.CompletedAt != nil {
		info.CompletedAt = snap.CompletedAt.Format(time.RFC3339)
	}
	if snap.Status == BGAgentCompleted || snap.Status == BGAgentFailed {
		info.Result = truncateForToolResponse(snap.Result, 4000)
		info.Error = snap.Error
	}
	if snap.Status == BGAgentRunning {
		// Return conversation history with pagination (last N entries, skip offset from end)
		agent := q.registry.Get(sessionID, agentID)
		if agent != nil {
			// Get more entries than needed so we can apply offset
			allHistory := agent.GetRecentHistory(last + offset)
			// Apply offset: trim the last `offset` entries
			if offset > 0 && len(allHistory) > offset {
				allHistory = allHistory[:len(allHistory)-offset]
			} else if offset > 0 {
				allHistory = nil // offset exceeds history length
			}
			// Take only the last `last` entries
			if len(allHistory) > last {
				allHistory = allHistory[len(allHistory)-last:]
			}
			for _, h := range allHistory {
				info.RecentHistory = append(info.RecentHistory, virtualtools.BGAgentHistoryEntry{
					Role: h.Role,
					Text: truncateForToolResponse(h.Text, 1000),
				})
			}
		}
		// Include recent tool calls with timing
		if agent != nil {
			toolCalls := agent.GetRecentToolCalls(5)
			for _, tc := range toolCalls {
				dur := ""
				if tc.Status == "running" {
					dur = time.Since(tc.StartedAt).Truncate(time.Second).String()
				} else if tc.Duration > 0 {
					dur = tc.Duration.Truncate(time.Millisecond).String()
				}
				info.RecentToolCalls = append(info.RecentToolCalls, virtualtools.BGAgentToolCall{
					ToolName: tc.ToolName,
					Duration: dur,
					Status:   tc.Status,
				})
			}
		}
	}
	return info, nil
}

func (q *bgAgentQuerierImpl) ListAgents(sessionID string) ([]*virtualtools.BGAgentInfo, error) {
	agents := q.registry.GetAll(sessionID)
	infos := make([]*virtualtools.BGAgentInfo, 0, len(agents))
	for _, agent := range agents {
		snap := agent.GetSnapshot()
		elapsed := time.Since(snap.CreatedAt)
		if snap.CompletedAt != nil {
			elapsed = snap.CompletedAt.Sub(snap.CreatedAt)
		}
		info := &virtualtools.BGAgentInfo{
			ID:      snap.ID,
			Name:    snap.Name,
			Status:  string(snap.Status),
			Elapsed: elapsed.Truncate(time.Second).String(),
		}
		if snap.Status == BGAgentFailed {
			info.Error = snap.Error
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (q *bgAgentQuerierImpl) TerminateAgent(sessionID, agentID string) error {
	return q.registry.CancelAgent(sessionID, agentID)
}

// truncateForToolResponse truncates a string for inclusion in tool responses
func truncateForToolResponse(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}

// executeBackgroundDelegatedTask spawns a background goroutine for async delegation
func (api *StreamingAPI) executeBackgroundDelegatedTask(
	ctx context.Context, parentReq QueryRequest, sessionID, name, instruction string,
) (string, error) {
	agentID := api.bgAgentRegistry.NextID(name)
	bgCtx, bgCancel := context.WithCancel(context.Background())

	// Copy only the context values actually needed by executeDelegatedTask.
	// Note: DelegationDepthKey is NOT copied because background sub-agents don't have
	// the delegate tool, so they can never create further sub-agents.
	if rl, ok := ctx.Value(virtualtools.ReasoningLevelKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.ReasoningLevelKey, rl)
	}
	if pf, ok := ctx.Value(virtualtools.PlanFolderKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.PlanFolderKey, pf)
	}
	if tm, ok := ctx.Value(virtualtools.ToolModeKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.ToolModeKey, tm)
	}
	if at, ok := ctx.Value(virtualtools.AgentTemplateKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.AgentTemplateKey, at)
	}
	if ds, ok := ctx.Value(virtualtools.DelegationServersKey).([]string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.DelegationServersKey, ds)
	}
	// Pass user ID for per-user OAuth
	if userID, ok := ctx.Value(common.UserIDKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, common.UserIDKey, userID)
		log.Printf("[USER_ID_DEBUGGING] Background agent: copied UserIDKey=%q to bgCtx", userID)
	}

	bgAgent := &BackgroundAgent{
		ID:          agentID,
		Name:        name,
		SessionID:   sessionID,
		Instruction: instruction,
		Status:      BGAgentRunning,
		CreatedAt:   time.Now(),
		cancel:      bgCancel,
	}
	api.bgAgentRegistry.Register(sessionID, bgAgent)

	// Inject background agent ID so delegation_start event can link back to this agent
	bgCtx = context.WithValue(bgCtx, virtualtools.BackgroundAgentIDKey, agentID)

	// Inject tool event callback so executeDelegatedTask's observer tracks timing on bgAgent
	bgCtx = context.WithValue(bgCtx, virtualtools.ToolEventCallbackKey, events.ToolEventCallback(
		func(toolCallID, toolName, eventType string, duration time.Duration) {
			switch eventType {
			case "start":
				bgAgent.RecordToolCallStart(toolCallID, toolName)
			case "end":
				bgAgent.RecordToolCallEnd(toolCallID, toolName, duration, false)
			case "error":
				bgAgent.RecordToolCallEnd(toolCallID, toolName, duration, true)
			}
		},
	))

	// Emit background_agent_started event
	api.emitBackgroundAgentEvent(sessionID, agentID, "background_agent_started", map[string]interface{}{
		"agent_id":    agentID,
		"name":        name,
		"instruction": truncateForToolResponse(instruction, 200),
	})

	// Start the background completion loop for this session if not already running
	api.completionLoopStartedMu.Lock()
	if !api.completionLoopStarted[sessionID] {
		api.completionLoopStarted[sessionID] = true
		go api.backgroundCompletionLoop(sessionID)
	}
	api.completionLoopStartedMu.Unlock()

	go func() {
		defer bgCancel()
		result, err := api.executeDelegatedTask(bgCtx, parentReq, sessionID, instruction, func(wrapper *agent.LLMAgentWrapper) {
			// Attach history func so query_agent can read the sub-agent's live conversation
			bgAgent.SetHistoryFunc(func(lastN int) []HistoryEntry {
				history := wrapper.GetHistory()
				start := 0
				if lastN > 0 && len(history) > lastN {
					start = len(history) - lastN
				}
				var entries []HistoryEntry
				for _, msg := range history[start:] {
					role := string(msg.Role)
					var parts []string
					for _, part := range msg.Parts {
						switch p := part.(type) {
						case llmtypes.TextContent:
							if p.Text != "" {
								parts = append(parts, p.Text)
							}
						case llmtypes.ToolCall:
							name := ""
							args := ""
							if p.FunctionCall != nil {
								name = p.FunctionCall.Name
								args = p.FunctionCall.Arguments
							}
							parts = append(parts, fmt.Sprintf("[tool_call: %s(%s)]", name, args))
						case *llmtypes.ToolCall:
							name := ""
							args := ""
							if p != nil && p.FunctionCall != nil {
								name = p.FunctionCall.Name
								args = p.FunctionCall.Arguments
							}
							parts = append(parts, fmt.Sprintf("[tool_call: %s(%s)]", name, args))
						case llmtypes.ToolCallResponse:
							parts = append(parts, fmt.Sprintf("[tool_result: %s] %s", p.Name, p.Content))
						case *llmtypes.ToolCallResponse:
							if p != nil {
								parts = append(parts, fmt.Sprintf("[tool_result: %s] %s", p.Name, p.Content))
							}
						}
					}
					if len(parts) > 0 {
						entries = append(entries, HistoryEntry{
							Role: role,
							Text: strings.Join(parts, "\n"),
						})
					}
				}
				return entries
			})
		})

		now := time.Now()
		duration := now.Sub(bgAgent.CreatedAt)

		if err != nil {
			bgAgent.SetError(err.Error())
			api.emitBackgroundAgentEvent(sessionID, agentID, "background_agent_completed", map[string]interface{}{
				"agent_id": agentID,
				"name":     name,
				"status":   "failed",
				"error":    err.Error(),
				"duration": duration.Truncate(time.Second).String(),
			})
			log.Printf("[BG AGENT] Agent '%s' (ID: %s) failed after %s: %v", name, agentID, duration, err)
		} else {
			bgAgent.SetResult(result)
			api.emitBackgroundAgentEvent(sessionID, agentID, "background_agent_completed", map[string]interface{}{
				"agent_id": agentID,
				"name":     name,
				"status":   "completed",
				"result":   truncateForToolResponse(result, 500),
				"duration": duration.Truncate(time.Second).String(),
			})
			log.Printf("[BG AGENT] Agent '%s' (ID: %s) completed in %s", name, agentID, duration)
		}

		// Signal completion to the notification loop
		api.bgAgentRegistry.NotifyCompletion(sessionID, agentID)
	}()

	return agentID, nil
}

// emitBackgroundAgentEvent emits a background agent event to the event store
func (api *StreamingAPI) emitBackgroundAgentEvent(sessionID, agentID, eventType string, data map[string]interface{}) {
	now := time.Now()
	data["timestamp"] = now.Format(time.RFC3339)

	eventID := fmt.Sprintf("%s_%s_%s", sessionID, eventType, agentID)
	if agentID == "" {
		eventID = fmt.Sprintf("%s_%s_%d", sessionID, eventType, now.UnixNano())
	}

	event := events.Event{
		ID:        eventID,
		Type:      eventType,
		Timestamp: now,
		SessionID: sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:      unifiedevents.EventType(eventType),
			Timestamp: now,
			SessionID: sessionID,
			Component: "background-agent",
			Data:      events.NewGenericEventData(eventType, data),
		},
	}
	api.eventStore.AddEvent(sessionID, event)
	// Also persist to database so shared/restored sessions include background agent events
	if api.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := api.chatDB.StoreEvent(context.Background(), sessionID, event.Data); err != nil {
			log.Printf("[BG_AGENT] Failed to persist %s event to DB: %v", eventType, err)
		}
	}
}

// isSessionBusy returns whether the session is currently processing a user turn
func (api *StreamingAPI) isSessionBusy(sessionID string) bool {
	api.sessionBusyMu.RLock()
	defer api.sessionBusyMu.RUnlock()
	return api.sessionBusy[sessionID]
}

// setSessionBusy sets the busy state for a session
func (api *StreamingAPI) setSessionBusy(sessionID string, busy bool) {
	api.sessionBusyMu.Lock()
	api.sessionBusy[sessionID] = busy
	api.sessionBusyMu.Unlock()
}

// queuePendingCompletion adds a completed agent ID to the pending queue
func (api *StreamingAPI) queuePendingCompletion(sessionID, agentID string) {
	api.pendingMu.Lock()
	defer api.pendingMu.Unlock()
	api.pendingCompletions[sessionID] = append(api.pendingCompletions[sessionID], agentID)
}

// drainPendingCompletions returns and clears all pending completion agent IDs
func (api *StreamingAPI) drainPendingCompletions(sessionID string) []string {
	api.pendingMu.Lock()
	defer api.pendingMu.Unlock()
	pending := api.pendingCompletions[sessionID]
	delete(api.pendingCompletions, sessionID)
	return pending
}

// backgroundCompletionLoop listens for background agent completions and triggers synthetic turns
func (api *StreamingAPI) backgroundCompletionLoop(sessionID string) {
	ch := api.bgAgentRegistry.GetNotificationChannel(sessionID)
	log.Printf("[BG AGENT] Started completion loop for session %s", sessionID)

	for agentID := range ch {
		if api.isSessionBusy(sessionID) {
			// Session is busy — queue the completion for later processing
			api.queuePendingCompletion(sessionID, agentID)
			log.Printf("[BG AGENT] Session %s busy, queued completion for agent %s", sessionID, agentID)
		} else {
			api.processBackgroundAgentCompletion(sessionID, agentID)
		}
	}

	log.Printf("[BG AGENT] Completion loop ended for session %s", sessionID)
}

// processBackgroundAgentCompletion injects a synthetic message and triggers a new main agent turn
func (api *StreamingAPI) processBackgroundAgentCompletion(sessionID, agentID string) {
	agent := api.bgAgentRegistry.Get(sessionID, agentID)
	if agent == nil {
		log.Printf("[BG AGENT] Warning: agent %s not found for completion processing", agentID)
		return
	}

	// Prevent duplicate processing
	agent.mu.Lock()
	if agent.notified {
		agent.mu.Unlock()
		return
	}
	agent.notified = true
	agent.mu.Unlock()

	snap := agent.GetSnapshot()

	var resultText string
	if snap.Status == BGAgentCompleted {
		resultText = truncateForToolResponse(snap.Result, 4000)
	} else if snap.Status == BGAgentFailed {
		resultText = fmt.Sprintf("Error: %s", snap.Error)
	} else {
		resultText = fmt.Sprintf("Status: %s", snap.Status)
	}

	syntheticMsg := fmt.Sprintf(
		"[Background Agent Notification]\nAgent '%s' (ID: %s) completed.\nStatus: %s\nResult:\n%s",
		snap.Name, snap.ID, snap.Status, resultText)

	// NOTE: Don't inject syntheticMsg into conversation history here.
	// handleQuery will add it via StreamWithEvents when the synthetic turn runs.

	// Emit synthetic_turn_ready event so frontend shows amber banner before the turn fires
	statusLabel := "completed"
	if snap.Status == BGAgentFailed {
		statusLabel = "failed"
	}
	api.emitBackgroundAgentEvent(sessionID, agentID, "synthetic_turn_ready", map[string]interface{}{
		"message":  fmt.Sprintf("Background agent '%s' %s. The main agent will process the results.", snap.Name, statusLabel),
		"agent_id": snap.ID,
		"name":     snap.Name,
		"status":   string(snap.Status),
	})

	// Trigger a synthetic turn using the stored QueryRequest
	// Called synchronously so handleQuery sets session busy before returning,
	// preventing concurrent synthetic turns for the same session.
	api.executeSyntheticTurn(sessionID, syntheticMsg)
}

// executeSyntheticTurn drives the stored agent directly with a synthetic message.
// Instead of creating an internal HTTP request and re-building the entire agent/tools/history,
// it reuses the agent stored after the last plan-mode turn via StreamWithEvents().
// This is called synchronously from processBackgroundAgentCompletion — it sets session busy
// before spawning the goroutine, preventing concurrent synthetic turns.
func (api *StreamingAPI) executeSyntheticTurn(sessionID, syntheticMsg string) {
	// Get stored agent for this session
	api.sessionAgentsMux.RLock()
	llmAgent, ok := api.sessionAgents[sessionID]
	api.sessionAgentsMux.RUnlock()

	if !ok || llmAgent == nil {
		log.Printf("[BG AGENT] No stored agent for session %s, cannot trigger synthetic turn", sessionID)
		return
	}

	// Get stored query request for user ID context
	api.lastQueryMu.RLock()
	req, hasReq := api.lastQueryRequests[sessionID]
	api.lastQueryMu.RUnlock()

	// Set session busy synchronously BEFORE spawning goroutine
	// This prevents concurrent synthetic turns from the completion listener
	api.setSessionBusy(sessionID, true)

	// Update session status to running
	api.updateSessionStatus(sessionID, "running")

	// Create cancellable context for this synthetic turn
	agentCtx, agentCancel := context.WithCancel(context.Background())

	// Inject user ID into context for per-user folder isolation
	if hasReq && req.userID != "" {
		agentCtx = context.WithValue(agentCtx, common.UserIDKey, req.userID)
	}

	// Store cancel function so handleStopSession can cancel this turn
	api.agentCancelMux.Lock()
	api.agentCancelFuncs[sessionID] = agentCancel
	api.agentCancelMux.Unlock()

	log.Printf("[BG AGENT] Executing synthetic turn for session %s via stored agent", sessionID)

	go func() {
		defer func() {
			// Clean up cancel function
			api.agentCancelMux.Lock()
			delete(api.agentCancelFuncs, sessionID)
			api.agentCancelMux.Unlock()

			// Clear session busy and drain pending completions
			api.setSessionBusy(sessionID, false)
			pending := api.drainPendingCompletions(sessionID)
			for _, pendingAgentID := range pending {
				go api.processBackgroundAgentCompletion(sessionID, pendingAgentID)
			}
		}()

		// Stream the synthetic message through the stored agent
		// Events flow through already-attached EventObservers (in-memory + DB)
		textChan, err := llmAgent.StreamWithEvents(agentCtx, syntheticMsg)
		if err != nil {
			log.Printf("[BG AGENT] StreamWithEvents error for synthetic turn on session %s: %v", sessionID, err)
			api.updateSessionStatus(sessionID, "error")
			return
		}

		// Consume text chunks and save conversation history incrementally
		for range textChan {
			api.conversationMux.Lock()
			api.conversationHistory[sessionID] = llmAgent.GetHistory()
			api.conversationMux.Unlock()
		}

		// Final save of conversation history
		api.conversationMux.Lock()
		api.conversationHistory[sessionID] = llmAgent.GetHistory()
		api.conversationMux.Unlock()
		log.Printf("[BG AGENT] Synthetic turn completed for session %s, history: %d messages", sessionID, len(llmAgent.GetHistory()))

		// Update stored agent (it now has the latest history from this turn)
		api.sessionAgentsMux.Lock()
		api.sessionAgents[sessionID] = llmAgent
		api.sessionAgentsMux.Unlock()

		// Update session status to completed
		api.updateSessionStatus(sessionID, "completed")
	}()
}

// buildCapabilitiesContext creates a CapabilitiesContext from the chat request
// This is passed to the planner sub-agent so it knows what tools/servers/skills are available
func buildCapabilitiesContext(req QueryRequest) *virtualtools.CapabilitiesContext {
	caps := &virtualtools.CapabilitiesContext{
		EnabledServers: req.EnabledServers,
		SelectedTools:  req.SelectedTools,
		HasWorkspace:   req.EnableWorkspaceAccess == nil || *req.EnableWorkspaceAccess,
		HasBrowser:     req.EnableBrowserAccess != nil && *req.EnableBrowserAccess,
	}

	// Load skill summaries
	workspaceAPIURL := getWorkspaceAPIURL()
	for _, folderName := range req.SelectedSkills {
		skill, err := skills.GetSkill(workspaceAPIURL, folderName)
		if err != nil {
			log.Printf("[CAPABILITIES] Warning: Failed to load skill %s: %v", folderName, err)
			continue
		}
		caps.Skills = append(caps.Skills, virtualtools.SkillSummary{
			Name:        skill.Frontmatter.Name,
			Description: skill.Frontmatter.Description,
			FolderName:  folderName,
		})
	}

	// Load sub-agent template summaries
	for _, folderName := range req.SelectedSubAgents {
		sa, err := subagents.GetSubAgent(workspaceAPIURL, folderName)
		if err != nil {
			log.Printf("[CAPABILITIES] Warning: Failed to load sub-agent template %s: %v", folderName, err)
			continue
		}
		caps.SubAgentTemplates = append(caps.SubAgentTemplates, virtualtools.SubAgentTemplateSummary{
			Name:                  sa.Frontmatter.Name,
			Description:           sa.Frontmatter.Description,
			FolderName:            folderName,
			DefaultReasoningLevel: sa.Frontmatter.DefaultReasoningLevel,
			DefaultToolMode:       sa.Frontmatter.DefaultToolMode,
			Skills:                subagents.ParseCSV(sa.Frontmatter.Skills),
			Servers:               subagents.ParseCSV(sa.Frontmatter.Servers),
		})
	}

	return caps
}

// emitDelegationStartEvent emits an event when delegation starts
// This event serves as the parent for all sub-agent events (via parent_id linking)
func (api *StreamingAPI) emitDelegationStartEvent(sessionID, delegationID string, depth int, instruction, reasoningLevel, modelID, toolMode string, servers []string, backgroundAgentID string) {
	now := time.Now()
	eventID := fmt.Sprintf("%s_delegation_start_%s", sessionID, delegationID)
	eventData := &events.DelegationStartEventData{
		DelegationID:      delegationID,
		Depth:             depth,
		Instruction:       instruction,
		ReasoningLevel:    reasoningLevel,
		ModelID:           modelID,
		ToolMode:          toolMode,
		Servers:           servers,
		BackgroundAgentID: backgroundAgentID,
		Timestamp:         now.Format(time.RFC3339),
	}
	event := events.Event{
		ID:        eventID,
		Type:      "delegation_start",
		Timestamp: now,
		SessionID: sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:           unifiedevents.EventType("delegation_start"),
			Timestamp:      now,
			HierarchyLevel: depth,
			SessionID:      sessionID,
			Component:      fmt.Sprintf("delegation-%d", depth),
			CorrelationID:  delegationID, // Links all delegation events together
			Data:           eventData,
		},
	}
	api.eventStore.AddEvent(sessionID, event)
	// Also persist to database so shared sessions and restored sessions include delegation events
	if api.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := api.chatDB.StoreEvent(context.Background(), sessionID, event.Data); err != nil {
			log.Printf("[DELEGATION] Failed to persist delegation_start to DB: %v", err)
		}
	}
	log.Printf("[DELEGATION] Emitted delegation_start event %s for %s at depth %d", eventID, delegationID, depth)
}

// delegationEndStats holds optional stats for delegation end events
type delegationEndStats struct {
	InputTokens  int64
	OutputTokens int64
	ToolCalls    int64
	Duration     string
	TotalCostUSD float64
}

// emitDelegationEndEvent emits an event when delegation ends
// This event has the same correlation_id as delegation_start for grouping
func (api *StreamingAPI) emitDelegationEndEvent(sessionID, delegationID string, depth int, result, errorMsg string, stats *delegationEndStats) {
	now := time.Now()
	delegationStartEventID := fmt.Sprintf("%s_delegation_start_%s", sessionID, delegationID)
	eventData := &events.DelegationEndEventData{
		DelegationID: delegationID,
		Depth:        depth,
		Result:       result,
		Error:        errorMsg,
		Success:      errorMsg == "",
		Timestamp:    now.Format(time.RFC3339),
	}
	if stats != nil {
		eventData.InputTokens = stats.InputTokens
		eventData.OutputTokens = stats.OutputTokens
		eventData.ToolCalls = stats.ToolCalls
		eventData.Duration = stats.Duration
		eventData.TotalCostUSD = stats.TotalCostUSD
	}
	event := events.Event{
		ID:        fmt.Sprintf("%s_delegation_end_%s", sessionID, delegationID),
		Type:      "delegation_end",
		Timestamp: now,
		SessionID: sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:           unifiedevents.EventType("delegation_end"),
			Timestamp:      now,
			HierarchyLevel: depth,
			SessionID:      sessionID,
			Component:      fmt.Sprintf("delegation-%d", depth),
			CorrelationID:  delegationID,           // Links all delegation events together
			ParentID:       delegationStartEventID, // Makes this a child of delegation_start (for tree display)
			Data:           eventData,
		},
	}
	api.eventStore.AddEvent(sessionID, event)
	// Also persist to database so shared sessions and restored sessions include delegation events
	if api.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := api.chatDB.StoreEvent(context.Background(), sessionID, event.Data); err != nil {
			log.Printf("[DELEGATION] Failed to persist delegation_end to DB: %v", err)
		}
	}
	log.Printf("[DELEGATION] Emitted delegation_end event for %s at depth %d (success: %v)", delegationID, depth, errorMsg == "")
}

// resolveDelegationTierConfig builds a DelegationTierConfig by merging:
// 1. Frontend config (from QueryRequest) - highest priority
// 2. Environment variables (DELEGATION_TIER_*) - fallback
// Returns nil if no tier config is available at all
func resolveDelegationTierConfig(frontendConfig *virtualtools.DelegationTierConfig) *virtualtools.DelegationTierConfig {
	result := &virtualtools.DelegationTierConfig{}
	hasAny := false

	// Start with env var defaults
	if p, m := os.Getenv("DELEGATION_TIER_HIGH_PROVIDER"), os.Getenv("DELEGATION_TIER_HIGH_MODEL"); p != "" && m != "" {
		result.High = &virtualtools.TierModel{Provider: p, ModelID: m}
		hasAny = true
	}
	if p, m := os.Getenv("DELEGATION_TIER_MEDIUM_PROVIDER"), os.Getenv("DELEGATION_TIER_MEDIUM_MODEL"); p != "" && m != "" {
		result.Medium = &virtualtools.TierModel{Provider: p, ModelID: m}
		hasAny = true
	}
	if p, m := os.Getenv("DELEGATION_TIER_LOW_PROVIDER"), os.Getenv("DELEGATION_TIER_LOW_MODEL"); p != "" && m != "" {
		result.Low = &virtualtools.TierModel{Provider: p, ModelID: m}
		hasAny = true
	}

	// Override with frontend config (higher priority)
	if frontendConfig != nil {
		if frontendConfig.High != nil && frontendConfig.High.Provider != "" && frontendConfig.High.ModelID != "" {
			result.High = frontendConfig.High
			hasAny = true
		}
		if frontendConfig.Medium != nil && frontendConfig.Medium.Provider != "" && frontendConfig.Medium.ModelID != "" {
			result.Medium = frontendConfig.Medium
			hasAny = true
		}
		if frontendConfig.Low != nil && frontendConfig.Low.Provider != "" && frontendConfig.Low.ModelID != "" {
			result.Low = frontendConfig.Low
			hasAny = true
		}
		// Pass through custom tiers from frontend (no env var equivalent)
		if len(frontendConfig.Custom) > 0 {
			result.Custom = frontendConfig.Custom
			hasAny = true
		}
	}

	if !hasAny {
		return nil
	}
	return result
}

// handleGetDelegationTierDefaults returns the env var default values for delegation tier config
func (api *StreamingAPI) handleGetDelegationTierDefaults(w http.ResponseWriter, r *http.Request) {
	tierConfig := resolveDelegationTierConfig(nil) // env vars only
	if tierConfig == nil {
		tierConfig = &virtualtools.DelegationTierConfig{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tierConfig)
}

// getActiveSession retrieves an active session by ID
// truncateForLog truncates a string for logging purposes
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (api *StreamingAPI) getActiveSession(sessionID string) (*ActiveSessionInfo, bool) {
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()

	session, exists := api.activeSessions[sessionID]
	return session, exists
}

// getAllActiveSessions returns all active sessions (filtered by activity - 10 minute timeout)
func (api *StreamingAPI) getAllActiveSessions() []*ActiveSessionInfo {
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()

	now := time.Now()
	inactivityTimeout := 10 * time.Minute
	recentCompletionWindow := 30 * time.Minute
	sessions := make([]*ActiveSessionInfo, 0, len(api.activeSessions))

	for _, session := range api.activeSessions {
		// Include running sessions that have been active within the last 10 minutes
		if session.Status == "running" && now.Sub(session.LastActivity) < inactivityTimeout {
			sessions = append(sessions, session)
		} else if session.Status == "completed" && now.Sub(session.LastActivity) < recentCompletionWindow {
			// Also include recently completed sessions (within 30 minutes) for page refresh restore
			sessions = append(sessions, session)
		}
	}

	return sessions
}

// cleanupInactiveSessions runs periodically to:
// 1. Mark running sessions as inactive if no events for 10 minutes
// 2. Remove completed/inactive sessions from map after 30 minutes (to prevent memory leaks)
func (api *StreamingAPI) cleanupInactiveSessions() {
	ticker := time.NewTicker(2 * time.Minute) // Check every 2 minutes
	defer ticker.Stop()

	for range ticker.C {
		api.activeSessionsMux.Lock()
		now := time.Now()
		inactivityTimeout := 10 * time.Minute
		completedSessionRetention := 30 * time.Minute
		sessionsToMarkInactive := make([]string, 0)
		sessionsToDelete := make([]string, 0)

		for sessionID, session := range api.activeSessions {
			// Mark as inactive if no activity for 10 minutes and status is still "running"
			if session.Status == "running" && now.Sub(session.LastActivity) >= inactivityTimeout {
				sessionsToMarkInactive = append(sessionsToMarkInactive, sessionID)
			}
			// Delete completed/inactive sessions after 30 minutes to prevent memory leaks
			// These sessions have already been saved to the database
			if (session.Status == "completed" || session.Status == "inactive") && now.Sub(session.LastActivity) >= completedSessionRetention {
				sessionsToDelete = append(sessionsToDelete, sessionID)
			}
		}

		// Delete old sessions within the lock
		for _, sessionID := range sessionsToDelete {
			if session, exists := api.activeSessions[sessionID]; exists {
				status := session.Status
				delete(api.activeSessions, sessionID)
				log.Printf("[ACTIVE_SESSION] Cleanup: Removed old %s session %s from memory (>30 min old)", status, sessionID)
			}
		}

		api.activeSessionsMux.Unlock()

		// Mark sessions as inactive (outside lock to avoid deadlock)
		for _, sessionID := range sessionsToMarkInactive {
			log.Printf("[ACTIVE_SESSION] Marking session %s as inactive (no activity for 10+ minutes)", sessionID)
			api.updateSessionStatus(sessionID, "inactive")
		}
	}
}

// storeWorkflowOrchestrator stores a workflow orchestrator for a session
func (api *StreamingAPI) storeWorkflowOrchestrator(sessionID string, orchestrator orchestrator.Orchestrator) {
	api.workflowOrchestratorsMux.Lock()
	defer api.workflowOrchestratorsMux.Unlock()
	api.workflowOrchestrators[sessionID] = orchestrator
	log.Printf("[ORCHESTRATOR] Stored workflow orchestrator for session %s", sessionID)
}

// --- LLM GUIDANCE API HANDLERS ---

// deserializeSerializedMessage converts a SerializedMessage (typed) back to llmtypes.MessageContent
func deserializeSerializedMessage(serialized unifiedevents.SerializedMessage) *llmtypes.MessageContent {
	var role llmtypes.ChatMessageType
	switch serialized.Role {
	case "human": // Standard value from llmtypes
		role = llmtypes.ChatMessageTypeHuman
	case "ai": // Standard value from llmtypes
		role = llmtypes.ChatMessageTypeAI
	case "tool": // Standard value from llmtypes
		role = llmtypes.ChatMessageTypeTool
	case "system": // Standard value from llmtypes
		role = llmtypes.ChatMessageTypeSystem
	case "user", "assistant": // Fallback for compatibility (shouldn't happen but handle gracefully)
		if serialized.Role == "user" {
			role = llmtypes.ChatMessageTypeHuman
		} else {
			role = llmtypes.ChatMessageTypeAI
		}
	default:
		// Default to human if unknown role
		log.Printf("[DESERIALIZE] Unknown role '%s', defaulting to human", serialized.Role)
		role = llmtypes.ChatMessageTypeHuman
	}

	msg := &llmtypes.MessageContent{
		Role:  role,
		Parts: []llmtypes.ContentPart{},
	}

	for _, part := range serialized.Parts {
		switch part.Type {
		case "text":
			if content, ok := part.Content.(string); ok {
				msg.Parts = append(msg.Parts, llmtypes.TextContent{Text: content})
			}
		case "tool_call":
			if contentMap, ok := part.Content.(map[string]interface{}); ok {
				toolCall := llmtypes.ToolCall{
					FunctionCall: &llmtypes.FunctionCall{}, // Initialize pointer
				}
				if id, ok := contentMap["id"].(string); ok {
					toolCall.ID = id
				}
				if fnName, ok := contentMap["function_name"].(string); ok {
					toolCall.FunctionCall.Name = fnName
				}
				if fnArgs, ok := contentMap["function_args"].(string); ok {
					toolCall.FunctionCall.Arguments = fnArgs
				}
				msg.Parts = append(msg.Parts, toolCall)
			}
		case "tool_response":
			if contentMap, ok := part.Content.(map[string]interface{}); ok {
				toolResp := llmtypes.ToolCallResponse{}
				if toolCallID, ok := contentMap["tool_call_id"].(string); ok {
					toolResp.ToolCallID = toolCallID
				}
				if content, ok := contentMap["content"].(string); ok {
					toolResp.Content = content
				}
				msg.Parts = append(msg.Parts, toolResp)
			}
		}
	}

	return msg
}

// handleSetLLMGuidance sets LLM guidance for a session
func (api *StreamingAPI) handleSetLLMGuidance(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["session_id"]
	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	var req LLMGuidanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate session exists
	api.activeSessionsMux.RLock()
	session, exists := api.activeSessions[sessionID]
	api.activeSessionsMux.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Update guidance in activeSessions
	api.activeSessionsMux.Lock()
	session.LLMGuidance = req.Guidance
	session.LastActivity = time.Now()
	api.activeSessionsMux.Unlock()

	log.Printf("[LLM_GUIDANCE] Set guidance for session %s: %s", sessionID, req.Guidance)

	response := LLMGuidanceResponse{
		SessionID: sessionID,
		Status:    "success",
		Message:   "LLM guidance updated successfully",
		Guidance:  req.Guidance,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleSubmitHumanFeedback handles human feedback submission
func (api *StreamingAPI) handleSubmitHumanFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req HumanFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.UniqueID == "" {
		http.Error(w, "unique_id is required", http.StatusBadRequest)
		return
	}

	if req.Response == "" {
		http.Error(w, "response is required", http.StatusBadRequest)
		return
	}

	// Get human feedback store and submit response
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	if err := feedbackStore.SubmitResponse(req.UniqueID, req.Response); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("[HUMAN_FEEDBACK] Submitted response for unique_id %s: %s", req.UniqueID, req.Response)

	response := HumanFeedbackResponse{
		UniqueID: req.UniqueID,
		Status:   "success",
		Message:  "Human feedback submitted successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
