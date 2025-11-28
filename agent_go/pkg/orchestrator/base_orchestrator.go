package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/events"
	"mcpagent/observability"

	"llm-providers/llmtypes"
)

// Orchestrator defines the common interface for all orchestrators
type Orchestrator interface {
	// Execute performs the orchestration logic
	Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error)

	// GetType returns the orchestrator type
	GetType() string
}

// LLMConfig represents the LLM configuration from frontend
type LLMConfig struct {
	Provider              string                        `json:"provider"`
	ModelID               string                        `json:"model_id"`
	FallbackModels        []string                      `json:"fallback_models"`
	CrossProviderFallback *agents.CrossProviderFallback `json:"cross_provider_fallback,omitempty"`
	APIKeys               *APIKeys                      `json:"api_keys,omitempty"`
}

// APIKeys represents API keys for different providers
type APIKeys struct {
	OpenRouter *string     `json:"openrouter,omitempty"`
	OpenAI     *string     `json:"openai,omitempty"`
	Anthropic  *string     `json:"anthropic,omitempty"`
	Vertex     *string     `json:"vertex,omitempty"`
	Bedrock    *BedrockKey `json:"bedrock,omitempty"`
}

// BedrockKey represents Bedrock configuration
type BedrockKey struct {
	Region string `json:"region"`
}

// OrchestratorType represents the type of orchestrator
type OrchestratorType string

const (
	OrchestratorTypePlanner  OrchestratorType = "planner"
	OrchestratorTypeWorkflow OrchestratorType = "workflow"
)

// StepTokenUsage represents accumulated token usage for a workflow step
type StepTokenUsage struct {
	PromptTokens          int
	CompletionTokens      int
	TotalTokens           int
	CacheTokens           int
	ReasoningTokens       int
	LLMCallCount          int
	CacheEnabledCallCount int
	CacheDiscountSum      float64 // Sum of cache discounts for averaging
}

// BaseOrchestrator provides unified base functionality for all orchestrators
type BaseOrchestrator struct {
	// Context-aware event bridge for orchestrator-level events
	contextAwareBridge mcpagent.AgentEventListener

	// Logger for the orchestrator
	logger utils.ExtendedLogger

	// Workspace tools for file operations
	WorkspaceTools         []llmtypes.Tool
	WorkspaceToolExecutors map[string]interface{}
	ToolCategories         map[string]string // Tool name to category mapping

	// Orchestrator type and configuration
	orchestratorType OrchestratorType
	startTime        time.Time

	// Common configuration shared between orchestrators
	provider             string
	model                string
	mcpConfigPath        string
	temperature          float64
	agentMode            string
	selectedServers      []string
	selectedTools        []string   // Selected tools in "server:tool" format
	useCodeExecutionMode bool       // MCP code execution mode
	llmConfig            *LLMConfig // LLM configuration
	maxTurns             int        // Maximum turns for the orchestrator

	// Optional simple state (for workflow orchestrators)
	objective     string
	workspacePath string

	// Folder guard paths for fine-grained access control
	folderGuardReadPaths  []string
	folderGuardWritePaths []string

	// Step token tracking
	stepTokenAccumulator map[string]*StepTokenUsage // key format: "phase:step"
	stepTokenMutex       sync.RWMutex
}

// NewBaseOrchestrator creates a new unified base orchestrator
func NewBaseOrchestrator(
	logger utils.ExtendedLogger,
	eventBridge mcpagent.AgentEventListener,
	orchestratorType OrchestratorType,
	provider string,
	model string,
	mcpConfigPath string,
	temperature float64,
	agentMode string,
	selectedServers []string,
	selectedTools []string, // NEW parameter
	useCodeExecutionMode bool, // NEW parameter
	llmConfig *LLMConfig,
	maxTurns int,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
	toolCategories map[string]string, // NEW: tool category map
) (*BaseOrchestrator, error) {

	// Create context-aware event bridge that wraps the main event bridge
	contextAwareBridge := NewContextAwareEventBridge(eventBridge, logger)

	// Create orchestrator instance
	orchestrator := &BaseOrchestrator{
		contextAwareBridge:     contextAwareBridge,
		logger:                 logger,
		WorkspaceTools:         customTools,
		WorkspaceToolExecutors: customToolExecutors,
		ToolCategories:         toolCategories, // NEW: store category map
		orchestratorType:       orchestratorType,
		startTime:              time.Now(),
		// Common configuration
		provider:             provider,
		model:                model,
		mcpConfigPath:        mcpConfigPath,
		temperature:          temperature,
		agentMode:            agentMode,
		selectedServers:      selectedServers,
		selectedTools:        selectedTools,        // NEW field
		useCodeExecutionMode: useCodeExecutionMode, // NEW field
		llmConfig:            llmConfig,
		maxTurns:             maxTurns,
		// Initialize step token tracking
		stepTokenAccumulator: make(map[string]*StepTokenUsage),
	}

	// Set token accumulator on bridge for step token tracking
	contextAwareBridge.SetTokenAccumulator(orchestrator)

	return orchestrator, nil
}

// getMapKeys returns all keys from a map as a slice (helper for logging)
func getMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// GetLogger returns the orchestrator's logger
func (bo *BaseOrchestrator) GetLogger() utils.ExtendedLogger {
	return bo.logger
}

// emitEvent emits an event through the event bridge
func (bo *BaseOrchestrator) emitEvent(ctx context.Context, eventType events.EventType, data events.EventData) {
	// Create agent event
	agentEvent := &events.AgentEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	// Emit through event bridge
	if err := bo.contextAwareBridge.HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to emit event %s: %w", eventType, err)
	}
}

// EmitOrchestratorStart emits an orchestrator start event
func (bo *BaseOrchestrator) EmitOrchestratorStart(ctx context.Context, objective string, agentsCount int, executionMode string) {
	bo.GetLogger().Infof("📤 Emitting orchestrator start event")

	eventData := &events.OrchestratorStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Objective:        objective,
		AgentsCount:      agentsCount,
		ServersCount:     len(bo.selectedServers),
		OrchestratorType: bo.GetType(),
		ExecutionMode:    executionMode,
	}

	bo.emitEvent(ctx, events.OrchestratorStart, eventData)
}

// EmitOrchestratorEnd emits an orchestrator end event
func (bo *BaseOrchestrator) EmitOrchestratorEnd(ctx context.Context, objective, result, status, message string, executionMode string) {
	bo.GetLogger().Infof("📤 Emitting orchestrator end event: %s", status)

	duration := time.Since(bo.startTime)
	eventData := &events.OrchestratorEndEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Objective:        objective,
		Result:           result,
		Status:           status,
		Duration:         duration,
		OrchestratorType: bo.GetType(),
		ExecutionMode:    executionMode,
	}

	bo.emitEvent(ctx, events.OrchestratorEnd, eventData)
}

// EmitUnifiedCompletionEvent emits a unified completion event
func (bo *BaseOrchestrator) EmitUnifiedCompletionEvent(ctx context.Context, agentType, agentMode, question, finalResult, status string, turns int) {
	bo.GetLogger().Infof("📤 Emitting unified completion event: %s", status)

	duration := time.Since(bo.startTime)
	completionEventData := events.NewUnifiedCompletionEvent(
		agentType,
		agentMode,
		question,
		finalResult,
		status,
		duration,
		turns,
	)

	agentEvent := events.NewAgentEvent(completionEventData)

	// Emit through event bridge directly
	if err := bo.contextAwareBridge.HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to emit unified completion event: %w", err)
	}
}

// ConnectAgentToEventBridge connects a sub-agent to the event bridge for proper event forwarding
// ConnectAgentToEventBridge removed: logic now inlined in CreateAndSetupStandardAgent

// GetStartTime returns the start time
func (bo *BaseOrchestrator) GetStartTime() time.Time {
	return bo.startTime
}

// GetOrchestratorType returns the orchestrator type
func (bo *BaseOrchestrator) GetOrchestratorType() OrchestratorType {
	return bo.orchestratorType
}

// Workflow-specific methods (only available for workflow orchestrators)
// GetObjective returns the current objective
func (bo *BaseOrchestrator) GetObjective() string {
	return bo.objective
}

// SetObjective sets the objective
func (bo *BaseOrchestrator) SetObjective(objective string) {
	bo.objective = objective
}

// GetWorkspacePath returns the current workspace path
func (bo *BaseOrchestrator) GetWorkspacePath() string {
	return bo.workspacePath
}

// SetWorkspacePath sets the workspace path
func (bo *BaseOrchestrator) SetWorkspacePath(workspacePath string) {
	bo.workspacePath = workspacePath
}

// SetWorkspacePathForFolderGuard sets separate read and write paths for folder guard validation
// If both arrays are empty, folder guard validation is disabled (allows all paths)
func (bo *BaseOrchestrator) SetWorkspacePathForFolderGuard(readPaths []string, writePaths []string) {
	if len(readPaths) == 0 && len(writePaths) == 0 {
		// Empty arrays disable folder guard
		bo.folderGuardReadPaths = nil
		bo.folderGuardWritePaths = nil
		bo.GetLogger().Infof("🔓 Folder guard disabled (empty read/write paths)")
	} else {
		bo.folderGuardReadPaths = readPaths
		bo.folderGuardWritePaths = writePaths
		bo.GetLogger().Infof("🔒 Folder guard enabled - Read paths: %v, Write paths: %v", readPaths, writePaths)
	}
}

// GetFolderGuardPaths returns the current folder guard read and write paths
func (bo *BaseOrchestrator) GetFolderGuardPaths() (readPaths []string, writePaths []string) {
	return bo.folderGuardReadPaths, bo.folderGuardWritePaths
}

// GetContextAwareBridge returns the context-aware event bridge
func (bo *BaseOrchestrator) GetContextAwareBridge() mcpagent.AgentEventListener {
	return bo.contextAwareBridge
}

// GetProvider returns the LLM provider
func (bo *BaseOrchestrator) GetProvider() string {
	return bo.provider
}

// GetModel returns the LLM model
func (bo *BaseOrchestrator) GetModel() string {
	return bo.model
}

// GetMCPConfigPath returns the MCP configuration path
func (bo *BaseOrchestrator) GetMCPConfigPath() string {
	return bo.mcpConfigPath
}

// GetTemperature returns the temperature setting
func (bo *BaseOrchestrator) GetTemperature() float64 {
	return bo.temperature
}

// GetAgentMode returns the agent mode
func (bo *BaseOrchestrator) GetAgentMode() string {
	return bo.agentMode
}

// GetSelectedServers returns the selected servers
func (bo *BaseOrchestrator) GetSelectedServers() []string {
	return bo.selectedServers
}

// GetSelectedTools returns the selected tools
func (bo *BaseOrchestrator) GetSelectedTools() []string {
	return bo.selectedTools
}

// GetUseCodeExecutionMode returns the code execution mode setting
func (bo *BaseOrchestrator) GetUseCodeExecutionMode() bool {
	return bo.useCodeExecutionMode
}

// GetLLMConfig returns the LLM configuration
func (bo *BaseOrchestrator) GetLLMConfig() *LLMConfig {
	return bo.llmConfig
}

// GetTracer returns the tracer (not implemented - orchestrator doesn't have its own tracer)
func (bo *BaseOrchestrator) GetTracer() observability.Tracer {
	// Orchestrators don't have their own tracer - they coordinate agents that have tracers
	return nil
}

// GetMaxTurns returns the maximum turns for the orchestrator
func (bo *BaseOrchestrator) GetMaxTurns() int {
	return bo.maxTurns
}

// GetType returns the orchestrator type
func (bo *BaseOrchestrator) GetType() string {
	return string(bo.orchestratorType)
}

// validatePathInWorkspace validates that the input path is within the workspace boundary
func validatePathInWorkspace(workspacePath, inputPath string) error {
	if workspacePath == "" {
		return nil // No validation if workspacePath is not set
	}

	// Normalize workspace path
	workspaceAbs, err := filepath.Abs(workspacePath)
	if err != nil {
		return fmt.Errorf("failed to resolve workspace path: %w", err)
	}
	workspaceAbs = filepath.Clean(workspaceAbs)

	// Resolve input path relative to workspace if it's relative
	var inputAbs string
	if filepath.IsAbs(inputPath) {
		inputAbs, err = filepath.Abs(inputPath)
		if err != nil {
			return fmt.Errorf("failed to resolve input path: %w", err)
		}
	} else {
		// Relative path - check both workspace-relative and CWD-relative resolutions
		// First, resolve relative to workspace (standard behavior)
		inputAbsFromWorkspace := filepath.Join(workspaceAbs, inputPath)
		inputAbsFromWorkspace = filepath.Clean(inputAbsFromWorkspace)

		// Also check what it resolves to from current working directory
		// This catches cases like "Workflow/HRMS PR Review/summary.md" which is a sibling, not a child
		inputAbsFromCWD, err := filepath.Abs(inputPath)
		if err == nil {
			inputAbsFromCWD = filepath.Clean(inputAbsFromCWD)
			// Check if CWD-resolved path is outside workspace
			cwdRel, relErr := filepath.Rel(workspaceAbs, inputAbsFromCWD)
			if relErr == nil && (strings.HasPrefix(cwdRel, "..") || cwdRel == "..") {
				// CWD path is outside workspace - this is the real intent, so use CWD resolution and block it
				inputAbs = inputAbsFromCWD
			} else {
				// CWD path is inside workspace - use workspace-relative resolution
				inputAbs = inputAbsFromWorkspace
			}
		} else {
			// Fallback to workspace-relative if CWD resolution fails
			inputAbs = inputAbsFromWorkspace
		}
	}
	inputAbs = filepath.Clean(inputAbs)

	// Special exception: Allow Downloads directory to bypass workspace boundary check
	// Downloads is a common directory that should be accessible regardless of workspace restrictions
	// Check if the path contains "Downloads" as a directory component (not just as part of a filename)
	inputAbsSlash := filepath.ToSlash(inputAbs)
	inputPathSlash := filepath.ToSlash(inputPath)

	// Check if path is in Downloads directory (allow "Downloads", "Downloads/...", or paths containing "/Downloads/")
	// But still prevent directory traversal attacks like "../../Downloads"
	isDownloadsPath := false
	if strings.HasPrefix(inputPathSlash, "Downloads/") || inputPathSlash == "Downloads" {
		// Direct Downloads path - allow it (no directory traversal)
		isDownloadsPath = true
	} else if strings.Contains(inputAbsSlash, "/Downloads/") || strings.HasSuffix(inputAbsSlash, "/Downloads") {
		// Path contains Downloads directory - check it's not a directory traversal attack
		if !strings.Contains(inputPathSlash, "../") && !strings.Contains(inputPathSlash, "..\\") {
			isDownloadsPath = true
		}
	}

	// If it's a Downloads path, skip workspace boundary validation
	if isDownloadsPath {
		// Final safety check: prevent any directory traversal attempts
		if strings.Contains(inputPathSlash, "../") || strings.Contains(inputPathSlash, "..\\") {
			return fmt.Errorf("path '%s' contains directory traversal and cannot be used even for Downloads directory", inputPath)
		}
		// Allow Downloads paths
		return nil
	}

	// Check if input path is within workspace boundary
	// First, verify that inputAbs actually has workspaceAbs as a prefix with proper path separator
	// This ensures we're checking directory boundaries, not just string prefixes
	workspaceAbsSlash := filepath.ToSlash(workspaceAbs) + "/"

	// Check if paths are equal (same directory) or input is a subdirectory
	if inputAbsSlash != filepath.ToSlash(workspaceAbs) && !strings.HasPrefix(inputAbsSlash, workspaceAbsSlash) {
		return fmt.Errorf("path '%s' (resolved to '%s') is outside workspace boundary '%s'. All file operations must be within the configured workspace", inputPath, inputAbs, workspacePath)
	}

	// Additional check using relative path (catches edge cases)
	rel, err := filepath.Rel(workspaceAbs, inputAbs)
	if err != nil {
		return fmt.Errorf("path validation error: %w", err)
	}

	// Check if path escapes workspace (contains ".." or is absolute)
	if strings.HasPrefix(rel, "..") || rel == ".." {
		return fmt.Errorf("path '%s' (resolved to '%s', relative: '%s') is outside workspace boundary '%s'. All file operations must be within the configured workspace", inputPath, inputAbs, rel, workspacePath)
	}

	return nil
}

// validatePathInAllowedPaths validates that the input path is within any of the allowed paths
// If allowedPaths is empty/nil, returns nil (allows all paths)
func validatePathInAllowedPaths(allowedPaths []string, inputPath string) error {
	// Empty array means disable folder guard - allow all paths
	if len(allowedPaths) == 0 {
		return nil
	}

	// Check against each allowed path
	for _, allowedPath := range allowedPaths {
		if err := validatePathInWorkspace(allowedPath, inputPath); err == nil {
			// Path is valid within this allowed path
			return nil
		}
	}

	// Path is not valid within any allowed path
	return fmt.Errorf("path '%s' is not within any of the allowed paths: %v", inputPath, allowedPaths)
}

// normalizePathForAllowedPaths normalizes a path relative to the first matching allowed path
// Returns the normalized path and the matching allowed path index
func normalizePathForAllowedPaths(allowedPaths []string, inputPath string) (string, int, error) {
	// Empty array means disable folder guard - return path as-is
	if len(allowedPaths) == 0 {
		return inputPath, -1, nil
	}

	// Find first matching allowed path
	for i, allowedPath := range allowedPaths {
		if err := validatePathInWorkspace(allowedPath, inputPath); err == nil {
			// Path matches this allowed path - normalize relative to it
			normalizedPath, err := normalizePathForWorkspace(allowedPath, inputPath)
			if err != nil {
				return "", -1, err
			}
			return normalizedPath, i, nil
		}
	}

	// No match found - use first allowed path as base for normalization
	normalizedPath, err := normalizePathForWorkspace(allowedPaths[0], inputPath)
	if err != nil {
		return "", -1, err
	}
	return normalizedPath, 0, nil
}

// normalizePathForWorkspace normalizes a path to be workspace-relative
// Returns a workspace-relative path (e.g., "." becomes "", "subfolder" stays "subfolder", absolute paths become relative)
func normalizePathForWorkspace(workspacePath, inputPath string) (string, error) {
	if workspacePath == "" {
		// No workspace - return path as-is
		return inputPath, nil
	}

	// Handle empty string or "." - normalize to "" which represents workspace root
	if inputPath == "" || inputPath == "." {
		return "", nil
	}

	// Normalize workspace path
	workspaceAbs, err := filepath.Abs(workspacePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace path: %w", err)
	}
	workspaceAbs = filepath.Clean(workspaceAbs)

	// Resolve input path relative to workspace if it's relative
	var inputAbs string
	if filepath.IsAbs(inputPath) {
		inputAbs, err = filepath.Abs(inputPath)
		if err != nil {
			return "", fmt.Errorf("failed to resolve input path: %w", err)
		}
	} else {
		// Relative path - resolve relative to workspace
		inputAbs = filepath.Join(workspaceAbs, inputPath)
	}
	inputAbs = filepath.Clean(inputAbs)

	// Convert to workspace-relative path
	rel, err := filepath.Rel(workspaceAbs, inputAbs)
	if err != nil {
		return "", fmt.Errorf("path normalization error: %w", err)
	}

	// Handle edge cases
	if rel == "." || rel == "" {
		return "", nil // Workspace root
	}

	return rel, nil
}

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
			var pathType string

			if useFolderGuardPaths {
				if isWriteCopy {
					// Write operations use writePaths only
					allowedPaths = bo.folderGuardWritePaths
					pathType = "write"
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
					pathType = "read"
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
					pathType = "read"
				}
			} else {
				// Fallback to workspacePath (single path mode)
				if workspacePath != "" {
					allowedPaths = []string{workspacePath}
					pathType = "workspace"
				}
			}

			// Validate and normalize all path parameters
			for _, paramName := range paramsToValidateCopy {
				if paramValue, exists := args[paramName]; exists {
					if pathStr, ok := paramValue.(string); ok {
						// Empty string or "." means workspace root - normalize to ""
						if pathStr == "" || pathStr == "." {
							args[paramName] = ""
							bo.GetLogger().Infof("📁 Normalized path for tool %s, parameter %s: '%s' -> '' (workspace root)", toolNameCopy, paramName, pathStr)
							continue
						}

						// Validate the path against allowed paths
						bo.GetLogger().Infof("🔒 Validating path for tool %s, parameter %s: type=%s, paths=%v, input='%s'", toolNameCopy, paramName, pathType, allowedPaths, pathStr)
						if err := validatePathInAllowedPaths(allowedPaths, pathStr); err != nil {
							bo.GetLogger().Warnf("⚠️ Path validation failed for tool %s, parameter %s: %v", toolNameCopy, paramName, err)
							return "", err
						}
						bo.GetLogger().Infof("✅ Path validation passed for tool %s, parameter %s (type: %s)", toolNameCopy, paramName, pathType)

						// Normalize the path
						normalizedPath, matchedIndex, err := normalizePathForAllowedPaths(allowedPaths, pathStr)
						if err != nil {
							bo.GetLogger().Warnf("⚠️ Path normalization failed for tool %s, parameter %s: %v", toolNameCopy, paramName, err)
							return "", err
						}
						if matchedIndex >= 0 {
							bo.GetLogger().Infof("📁 Normalized path for tool %s, parameter %s: '%s' -> '%s' (matched path index: %d)", toolNameCopy, paramName, pathStr, normalizedPath, matchedIndex)
						} else {
							bo.GetLogger().Infof("📁 Normalized path for tool %s, parameter %s: '%s' -> '%s' (no folder guard)", toolNameCopy, paramName, pathStr, normalizedPath)
						}
						// Update the args with normalized path
						args[paramName] = normalizedPath
					}
				}
			}

			// All validations passed and paths normalized - call original executor
			return originalExecutor(ctx, args)
		}

		wrappedExecutors[toolName] = wrappedExecutor
	}

	return wrappedExecutors
}

// CreateStandardAgentConfig creates a standardized agent configuration
// use CreateAndSetupStandardAgent instead which combines configuration and setup.
func (bo *BaseOrchestrator) CreateStandardAgentConfig(agentName string, maxTurns int, outputFormat agents.OutputFormat) *agents.OrchestratorAgentConfig {
	return bo.createAgentConfigWithLLM(agentName, maxTurns, outputFormat, bo.GetLLMConfig())
}

// CreateStandardAgentConfigWithCustomServers creates a standardized agent configuration with custom MCP servers
// This allows specific agents to override the default MCP server list
func (bo *BaseOrchestrator) CreateStandardAgentConfigWithCustomServers(agentName string, maxTurns int, outputFormat agents.OutputFormat, customServers []string) *agents.OrchestratorAgentConfig {
	config := bo.createAgentConfigWithLLM(agentName, maxTurns, outputFormat, bo.GetLLMConfig())

	// Override the server names with custom servers
	config.ServerNames = customServers

	bo.GetLogger().Infof("🔧 Created agent config for %s with custom MCP servers: %v", agentName, customServers)
	return config
}

// CreateStandardAgentConfigWithLLM creates a standardized agent configuration with custom LLM config
// This allows specific agents to override the default LLM configuration
func (bo *BaseOrchestrator) CreateStandardAgentConfigWithLLM(agentName string, maxTurns int, outputFormat agents.OutputFormat, llmConfig *LLMConfig) *agents.OrchestratorAgentConfig {
	return bo.createAgentConfigWithLLM(agentName, maxTurns, outputFormat, llmConfig)
}

// createAgentConfigWithLLM creates a generic agent configuration with detailed LLM config
func (bo *BaseOrchestrator) createAgentConfigWithLLM(agentName string, maxTurns int, outputFormat agents.OutputFormat, llmConfig *LLMConfig) *agents.OrchestratorAgentConfig {
	config := agents.NewOrchestratorAgentConfig(agentName)

	// Store the unique agent name for use in agent initialization
	config.AgentName = agentName

	// Use detailed LLM configuration from frontend if available
	llmProvider := bo.GetProvider()
	llmModel := bo.GetModel()
	// Use orchestrator-configured temperature unless an agent must override explicitly
	llmTemp := bo.GetTemperature()

	if llmConfig != nil {
		llmProvider = llmConfig.Provider
		llmModel = llmConfig.ModelID
		bo.GetLogger().Infof("🔧 Using detailed LLM config for %s agent - Provider: %s, Model: %s",
			agentName, llmProvider, llmModel)
	}

	config.Provider = llmProvider
	config.Model = llmModel
	config.Temperature = llmTemp // Uses orchestrator-configured temperature
	config.MCPConfigPath = bo.GetMCPConfigPath()
	config.MaxTurns = maxTurns
	config.ToolChoice = "auto"
	config.ServerNames = bo.GetSelectedServers()
	config.SelectedTools = bo.GetSelectedTools()               // NEW field
	config.UseCodeExecutionMode = bo.GetUseCodeExecutionMode() // NEW field
	config.Mode = agents.AgentMode(bo.GetAgentMode())
	config.OutputFormat = outputFormat
	config.MaxRetries = 3
	config.Timeout = 300 // Same timeout for all agents
	config.RateLimit = 60

	// Detailed LLM configuration from frontend
	if llmConfig != nil {
		config.FallbackModels = llmConfig.FallbackModels
		config.CrossProviderFallback = llmConfig.CrossProviderFallback
		// Convert API keys from orchestrator format to agent format
		if llmConfig.APIKeys != nil {
			config.APIKeys = &agents.AgentAPIKeys{
				OpenRouter: llmConfig.APIKeys.OpenRouter,
				OpenAI:     llmConfig.APIKeys.OpenAI,
				Anthropic:  llmConfig.APIKeys.Anthropic,
				Vertex:     llmConfig.APIKeys.Vertex,
			}
			if llmConfig.APIKeys.Bedrock != nil {
				config.APIKeys.Bedrock = &agents.BedrockAgentConfig{
					Region: llmConfig.APIKeys.Bedrock.Region,
				}
			}
		}
	}

	return config
}

// CreateAndSetupStandardAgent creates and sets up an agent with standardized configuration
func (bo *BaseOrchestrator) CreateAndSetupStandardAgent(
	ctx context.Context,
	agentName string,
	phase string,
	step, iteration int,
	maxTurns int,
	outputFormat agents.OutputFormat,
	createAgentFunc func(*agents.OrchestratorAgentConfig, utils.ExtendedLogger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
) (agents.OrchestratorAgent, error) {
	// Create standardized agent configuration using agentName as agentType
	config := bo.CreateStandardAgentConfig(agentName, maxTurns, outputFormat)

	// Create agent using provided factory function
	agent := createAgentFunc(config, bo.GetLogger(), bo.GetTracer(), bo.GetContextAwareBridge())

	// Initialize and setup agent (inlined from setupAgent)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize %s: %w", agentName, err)
	}

	// Validate essentials and connect event bridge
	eventBridge := bo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	bo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// 🔗 Connect agent to orchestrator's main event bridge using existing bridge (reuse)
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		bo.GetLogger().Infof("🔗 Reused context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
		bo.GetLogger().Infof("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
	}

	// Register custom tools
	if customTools != nil && customToolExecutors != nil {
		// Filter out write tools if there's no write access
		filteredTools := make([]llmtypes.Tool, 0, len(customTools))
		for _, tool := range customTools {
			if tool.Function != nil && !bo.ShouldFilterWriteTool(tool.Function.Name) {
				filteredTools = append(filteredTools, tool)
			} else if tool.Function != nil && bo.ShouldFilterWriteTool(tool.Function.Name) {
				bo.GetLogger().Infof("🚫 Filtering out write tool %s (no write access)", tool.Function.Name)
			}
		}

		// Wrap executors with folder guard if workspacePath is set
		wrappedExecutors := bo.WrapWorkspaceToolsWithFolderGuard(customToolExecutors)

		// Enhance tool descriptions with folder guard information automatically
		for i := range filteredTools {
			if filteredTools[i].Function != nil {
				filteredTools[i].Function.Description = bo.EnhanceToolDescriptionWithFolderGuard(
					filteredTools[i].Function.Name,
					filteredTools[i].Function.Description,
				)
			}
		}

		bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(customTools), agentName, baseAgent.GetMode())
		if bo.ToolCategories != nil {
			bo.GetLogger().Infof("🔍 [DISCOVERY] ToolCategories map has %d entries", len(bo.ToolCategories))
			// Log ALL entries for debugging (not just first 10)
			for toolName, category := range bo.ToolCategories {
				bo.GetLogger().Infof("🔍 [DISCOVERY]   - %s -> %s", toolName, category)
			}
		} else {
			bo.GetLogger().Warnf("🔍 [DISCOVERY] ToolCategories map is nil - all tools will default to 'custom' category")
		}

		// Also log all tool names being registered for comparison
		bo.GetLogger().Infof("🔍 [DISCOVERY] Tools being registered (count: %d):", len(customTools))
		for _, tool := range customTools {
			if tool.Function != nil {
				bo.GetLogger().Infof("🔍 [DISCOVERY]   - Tool name: %s", tool.Function.Name)
			}
		}

		bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode) (filtered from %d)", len(filteredTools), agentName, baseAgent.GetMode(), len(customTools))

		for _, tool := range filteredTools {
			if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
				// Convert Parameters to map[string]interface{}
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						if err := json.Unmarshal(paramsBytes, &params); err != nil {
							bo.GetLogger().Warnf("Warning: Failed to unmarshal parameters for tool %s: %v", tool.Function.Name, err)
							params = nil
						}
					}
				}
				if params == nil {
					bo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				// Type assert executor to function type
				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from stored map - REQUIRED, no default
					// All tools must have a category from ToolCategories map
					var toolCategory string
					if bo.ToolCategories != nil {
						if cat, exists := bo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
							bo.GetLogger().Infof("🔍 [DISCOVERY] Tool %s assigned category: %s", tool.Function.Name, toolCategory)
						} else {
							// Tool not found in map - throw error
							bo.GetLogger().Errorf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name)
							bo.GetLogger().Errorf("❌ [DISCOVERY] Available keys in ToolCategories map: %v", getMapKeys(bo.ToolCategories))
							bo.GetLogger().Errorf("❌ [DISCOVERY] Tool name being looked up: '%s' (len=%d)", tool.Function.Name, len(tool.Function.Name))
							return nil, fmt.Errorf("tool %s not found in ToolCategories map - category is REQUIRED", tool.Function.Name)
						}
					} else {
						bo.GetLogger().Errorf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name)
						return nil, fmt.Errorf("ToolCategories map is nil - category is REQUIRED for tool %s", tool.Function.Name)
					}

					// Validate category is not empty
					if toolCategory == "" {
						return nil, fmt.Errorf("tool %s has empty category - category is REQUIRED", tool.Function.Name)
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						return nil, fmt.Errorf("failed to register tool %s: %w", tool.Function.Name, err)
					}
				} else {
					bo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		// Log summary of category assignments
		categorySummary := make(map[string]int)
		for _, tool := range customTools {
			if tool.Function != nil {
				toolName := tool.Function.Name
				category := "custom"
				if bo.ToolCategories != nil {
					if cat, exists := bo.ToolCategories[toolName]; exists {
						category = cat
					}
				}
				categorySummary[category]++
			}
		}
		bo.GetLogger().Infof("🔍 [DISCOVERY] Category assignment summary:")
		for category, count := range categorySummary {
			bo.GetLogger().Infof("🔍 [DISCOVERY]   - %s: %d tools", category, count)
		}

		bo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())

		// 🔧 CRITICAL FIX: Explicitly update code execution registry after all tools are registered
		// This ensures workspace and human tools are available in code execution mode
		if bo.GetUseCodeExecutionMode() {
			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				bo.GetLogger().Warnf("⚠️ Failed to update code execution registry for %s: %v", agentName, err)
				// Don't fail agent creation if registry update fails, but log the warning
			} else {
				bo.GetLogger().Infof("✅ [CODE_EXECUTION] Registry updated for %s agent - workspace and human tools are now available", agentName)
			}
		}
	}

	return agent, nil
}

// CreateAndSetupStandardAgentWithCustomServers creates and sets up an agent with custom MCP servers
// This allows specific agents to override the default MCP server list
func (bo *BaseOrchestrator) CreateAndSetupStandardAgentWithCustomServers(
	ctx context.Context,
	agentName string,
	phase string,
	step, iteration int,
	maxTurns int,
	outputFormat agents.OutputFormat,
	customServers []string,
	createAgentFunc func(*agents.OrchestratorAgentConfig, utils.ExtendedLogger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
) (agents.OrchestratorAgent, error) {
	// Create standardized agent configuration with custom servers
	config := bo.CreateStandardAgentConfigWithCustomServers(agentName, maxTurns, outputFormat, customServers)

	// Create agent using provided factory function
	agent := createAgentFunc(config, bo.GetLogger(), bo.GetTracer(), bo.GetContextAwareBridge())

	// Initialize and setup agent (inlined from setupAgent)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize %s: %w", agentName, err)
	}

	// Validate essentials and connect event bridge
	eventBridge := bo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	bo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// 🔗 Connect agent to orchestrator's main event bridge using existing bridge (reuse)
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(interface {
		SetOrchestratorContext(phase string, step, iteration int, agentName string)
	}); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(eventBridge)
		bo.GetLogger().Infof("🔗 Reused context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
		bo.GetLogger().Infof("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
	}

	// Register custom tools
	if customTools != nil && customToolExecutors != nil {
		// Filter out write tools if there's no write access
		filteredTools := make([]llmtypes.Tool, 0, len(customTools))
		for _, tool := range customTools {
			if tool.Function != nil && !bo.ShouldFilterWriteTool(tool.Function.Name) {
				filteredTools = append(filteredTools, tool)
			} else if tool.Function != nil && bo.ShouldFilterWriteTool(tool.Function.Name) {
				bo.GetLogger().Infof("🚫 Filtering out write tool %s (no write access)", tool.Function.Name)
			}
		}

		// Wrap executors with folder guard if workspacePath is set
		wrappedExecutors := bo.WrapWorkspaceToolsWithFolderGuard(customToolExecutors)

		// Enhance tool descriptions with folder guard information automatically
		for i := range filteredTools {
			if filteredTools[i].Function != nil {
				filteredTools[i].Function.Description = bo.EnhanceToolDescriptionWithFolderGuard(
					filteredTools[i].Function.Name,
					filteredTools[i].Function.Description,
				)
			}
		}

		bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(customTools), agentName, baseAgent.GetMode())
		if bo.ToolCategories != nil {
			bo.GetLogger().Infof("🔍 [DISCOVERY] ToolCategories map has %d entries", len(bo.ToolCategories))
			// Log ALL entries for debugging (not just first 10)
			for toolName, category := range bo.ToolCategories {
				bo.GetLogger().Infof("🔍 [DISCOVERY]   - %s -> %s", toolName, category)
			}
		} else {
			bo.GetLogger().Warnf("🔍 [DISCOVERY] ToolCategories map is nil - all tools will default to 'custom' category")
		}

		// Also log all tool names being registered for comparison
		bo.GetLogger().Infof("🔍 [DISCOVERY] Tools being registered (count: %d):", len(customTools))
		for _, tool := range customTools {
			if tool.Function != nil {
				bo.GetLogger().Infof("🔍 [DISCOVERY]   - Tool name: %s", tool.Function.Name)
			}
		}

		bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode) (filtered from %d)", len(filteredTools), agentName, baseAgent.GetMode(), len(customTools))

		for _, tool := range filteredTools {
			if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
				// Convert Parameters to map[string]interface{}
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						if err := json.Unmarshal(paramsBytes, &params); err != nil {
							bo.GetLogger().Warnf("Warning: Failed to unmarshal parameters for tool %s: %v", tool.Function.Name, err)
							params = nil
						}
					}
				}
				if params == nil {
					bo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				// Type assert executor to function type
				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from stored map - REQUIRED, no default
					// All tools must have a category from ToolCategories map
					var toolCategory string
					if bo.ToolCategories != nil {
						if cat, exists := bo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
							bo.GetLogger().Infof("🔍 [DISCOVERY] Tool %s assigned category: %s", tool.Function.Name, toolCategory)
						} else {
							// Tool not found in map - throw error
							bo.GetLogger().Errorf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name)
							bo.GetLogger().Errorf("❌ [DISCOVERY] Available keys in ToolCategories map: %v", getMapKeys(bo.ToolCategories))
							bo.GetLogger().Errorf("❌ [DISCOVERY] Tool name being looked up: '%s' (len=%d)", tool.Function.Name, len(tool.Function.Name))
							return nil, fmt.Errorf("tool %s not found in ToolCategories map - category is REQUIRED", tool.Function.Name)
						}
					} else {
						bo.GetLogger().Errorf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name)
						return nil, fmt.Errorf("ToolCategories map is nil - category is REQUIRED for tool %s", tool.Function.Name)
					}

					// Validate category is not empty
					if toolCategory == "" {
						return nil, fmt.Errorf("tool %s has empty category - category is REQUIRED", tool.Function.Name)
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						return nil, fmt.Errorf("failed to register tool %s: %w", tool.Function.Name, err)
					}
				} else {
					bo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		// Log summary of category assignments
		categorySummary := make(map[string]int)
		for _, tool := range customTools {
			if tool.Function != nil {
				toolName := tool.Function.Name
				category := "custom"
				if bo.ToolCategories != nil {
					if cat, exists := bo.ToolCategories[toolName]; exists {
						category = cat
					}
				}
				categorySummary[category]++
			}
		}
		bo.GetLogger().Infof("🔍 [DISCOVERY] Category assignment summary:")
		for category, count := range categorySummary {
			bo.GetLogger().Infof("🔍 [DISCOVERY]   - %s: %d tools", category, count)
		}

		bo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())

		// 🔧 CRITICAL FIX: Explicitly update code execution registry after all tools are registered
		// This ensures workspace and human tools are available in code execution mode
		if bo.GetUseCodeExecutionMode() {
			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				bo.GetLogger().Warnf("⚠️ Failed to update code execution registry for %s: %v", agentName, err)
				// Don't fail agent creation if registry update fails, but log the warning
			} else {
				bo.GetLogger().Infof("✅ [CODE_EXECUTION] Registry updated for %s agent - workspace and human tools are now available", agentName)
			}
		}
	}

	return agent, nil
}

// CreateAndSetupStandardAgentWithConfig creates and sets up an agent with a pre-created configuration
// This allows agents to have full control over config (custom LLM, servers, EnableLargeOutputVirtualTools, etc.)
// while still using the standard setup logic (initialization, event bridge connection, tool registration)
func (bo *BaseOrchestrator) CreateAndSetupStandardAgentWithConfig(
	ctx context.Context,
	config *agents.OrchestratorAgentConfig,
	phase string,
	step, iteration int,
	createAgentFunc func(*agents.OrchestratorAgentConfig, utils.ExtendedLogger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
	overwriteSystemPrompt bool,
) (agents.OrchestratorAgent, error) {
	// Apply overwriteSystemPrompt parameter to config so callers can override default system prompt behavior
	config.OverwriteSystemPrompt = &overwriteSystemPrompt

	// Create agent using provided factory function with pre-created config
	agent := createAgentFunc(config, bo.GetLogger(), bo.GetTracer(), bo.GetContextAwareBridge())

	// Initialize and setup agent (inlined from setupAgent)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize %s: %w", config.AgentName, err)
	}

	// Validate essentials and connect event bridge
	eventBridge := bo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", config.AgentName)
	}

	bo.GetLogger().Infof("🔍 Checking agent structure for %s", config.AgentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", config.AgentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", config.AgentName)
	}

	// 🔗 Connect agent to orchestrator's main event bridge using existing bridge (reuse)
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(interface {
		SetOrchestratorContext(phase string, step, iteration int, agentName string)
	}); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(eventBridge)
		bo.GetLogger().Infof("🔗 Reused context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
		bo.GetLogger().Infof("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", config.AgentName)
	}

	// Register custom tools
	if customTools != nil && customToolExecutors != nil {
		// Filter out write tools if there's no write access
		filteredTools := make([]llmtypes.Tool, 0, len(customTools))
		for _, tool := range customTools {
			if tool.Function != nil && !bo.ShouldFilterWriteTool(tool.Function.Name) {
				filteredTools = append(filteredTools, tool)
			} else if tool.Function != nil && bo.ShouldFilterWriteTool(tool.Function.Name) {
				bo.GetLogger().Infof("🚫 Filtering out write tool %s (no write access)", tool.Function.Name)
			}
		}

		// Wrap executors with folder guard if workspacePath is set
		wrappedExecutors := bo.WrapWorkspaceToolsWithFolderGuard(customToolExecutors)

		// Enhance tool descriptions with folder guard information automatically
		for i := range filteredTools {
			if filteredTools[i].Function != nil {
				filteredTools[i].Function.Description = bo.EnhanceToolDescriptionWithFolderGuard(
					filteredTools[i].Function.Name,
					filteredTools[i].Function.Description,
				)
			}
		}

		bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode) (filtered from %d)", len(filteredTools), config.AgentName, baseAgent.GetMode(), len(customTools))

		for _, tool := range filteredTools {
			if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
				// Convert Parameters to map[string]interface{}
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						if err := json.Unmarshal(paramsBytes, &params); err != nil {
							bo.GetLogger().Warnf("Warning: Failed to unmarshal parameters for tool %s: %v", tool.Function.Name, err)
							params = nil
						}
					}
				}
				if params == nil {
					bo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				// Type assert executor to function type
				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from stored map - REQUIRED, no default
					// All tools must have a category from ToolCategories map
					var toolCategory string
					if bo.ToolCategories != nil {
						if cat, exists := bo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
							bo.GetLogger().Infof("🔍 [DISCOVERY] Tool %s assigned category: %s", tool.Function.Name, toolCategory)
						} else {
							// Tool not found in map - throw error
							bo.GetLogger().Errorf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name)
							bo.GetLogger().Errorf("❌ [DISCOVERY] Available keys in ToolCategories map: %v", getMapKeys(bo.ToolCategories))
							bo.GetLogger().Errorf("❌ [DISCOVERY] Tool name being looked up: '%s' (len=%d)", tool.Function.Name, len(tool.Function.Name))
							return nil, fmt.Errorf("tool %s not found in ToolCategories map - category is REQUIRED", tool.Function.Name)
						}
					} else {
						bo.GetLogger().Errorf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name)
						return nil, fmt.Errorf("ToolCategories map is nil - category is REQUIRED for tool %s", tool.Function.Name)
					}

					// Validate category is not empty
					if toolCategory == "" {
						return nil, fmt.Errorf("tool %s has empty category - category is REQUIRED", tool.Function.Name)
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						return nil, fmt.Errorf("failed to register tool %s: %w", tool.Function.Name, err)
					}
				} else {
					bo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		bo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", config.AgentName, baseAgent.GetMode())
	}

	return agent, nil
}

// CreateAndSetupStandardAgentWithSystemPrompt creates and sets up an agent with system prompt and user message processors
// This allows agents to have detailed system prompts while keeping user messages simple
func (bo *BaseOrchestrator) CreateAndSetupStandardAgentWithSystemPrompt(
	ctx context.Context,
	agentName string,
	phase string,
	step, iteration int,
	maxTurns int,
	outputFormat agents.OutputFormat,
	systemPromptProcessor func(map[string]string) string,
	userMessageProcessor func(map[string]string) string,
	createAgentFunc func(*agents.OrchestratorAgentConfig, utils.ExtendedLogger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
) (agents.OrchestratorAgent, error) {
	// Create standardized agent configuration using agentName as agentType
	config := bo.CreateStandardAgentConfig(agentName, maxTurns, outputFormat)

	// Create agent using provided factory function
	agent := createAgentFunc(config, bo.GetLogger(), bo.GetTracer(), bo.GetContextAwareBridge())

	// Initialize and setup agent
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize %s: %w", agentName, err)
	}

	// Set user message processor if provided
	// Since agents embed *BaseOrchestratorAgent, methods are promoted
	// Note: systemPromptProcessor is now passed as parameter to Execute methods, not set here
	if userMessageProcessor != nil {
		if settable, ok := agent.(agents.UserMessageProcessorSetter); ok {
			settable.SetUserMessageProcessor(userMessageProcessor)
			bo.GetLogger().Infof("✅ User message processor set for %s", agentName)
		} else {
			bo.GetLogger().Warnf("⚠️ Could not set user message processor for %s - agent does not implement UserMessageProcessorSetter", agentName)
		}
	}

	// Validate essentials and connect event bridge
	eventBridge := bo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	bo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// 🔗 Connect agent to orchestrator's main event bridge using existing bridge (reuse)
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		bo.GetLogger().Infof("🔗 Reused context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
		bo.GetLogger().Infof("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
	}

	// Register custom tools
	if customTools != nil && customToolExecutors != nil {
		// Filter out write tools if there's no write access
		filteredTools := make([]llmtypes.Tool, 0, len(customTools))
		for _, tool := range customTools {
			if tool.Function != nil && !bo.ShouldFilterWriteTool(tool.Function.Name) {
				filteredTools = append(filteredTools, tool)
			} else if tool.Function != nil && bo.ShouldFilterWriteTool(tool.Function.Name) {
				bo.GetLogger().Infof("🚫 Filtering out write tool %s (no write access)", tool.Function.Name)
			}
		}

		// Wrap executors with folder guard if workspacePath is set
		wrappedExecutors := bo.WrapWorkspaceToolsWithFolderGuard(customToolExecutors)

		// Enhance tool descriptions with folder guard information automatically
		for i := range filteredTools {
			if filteredTools[i].Function != nil {
				filteredTools[i].Function.Description = bo.EnhanceToolDescriptionWithFolderGuard(
					filteredTools[i].Function.Name,
					filteredTools[i].Function.Description,
				)
			}
		}

		bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(customTools), agentName, baseAgent.GetMode())
		if bo.ToolCategories != nil {
			bo.GetLogger().Infof("🔍 [DISCOVERY] ToolCategories map has %d entries", len(bo.ToolCategories))
			// Log ALL entries for debugging (not just first 10)
			for toolName, category := range bo.ToolCategories {
				bo.GetLogger().Infof("🔍 [DISCOVERY]   - %s -> %s", toolName, category)
			}
		} else {
			bo.GetLogger().Warnf("🔍 [DISCOVERY] ToolCategories map is nil - all tools will default to 'custom' category")
		}

		// Also log all tool names being registered for comparison
		bo.GetLogger().Infof("🔍 [DISCOVERY] Tools being registered (count: %d):", len(customTools))
		for _, tool := range customTools {
			if tool.Function != nil {
				bo.GetLogger().Infof("🔍 [DISCOVERY]   - Tool name: %s", tool.Function.Name)
			}
		}

		bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode) (filtered from %d)", len(filteredTools), agentName, baseAgent.GetMode(), len(customTools))

		for _, tool := range filteredTools {
			if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
				// Convert Parameters to map[string]interface{}
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						if err := json.Unmarshal(paramsBytes, &params); err != nil {
							bo.GetLogger().Warnf("Warning: Failed to unmarshal parameters for tool %s: %v", tool.Function.Name, err)
							params = nil
						}
					}
				}
				if params == nil {
					bo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				// Type assert executor to function type
				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from stored map - REQUIRED, no default
					// All tools must have a category from ToolCategories map
					var toolCategory string
					if bo.ToolCategories != nil {
						if cat, exists := bo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
							bo.GetLogger().Infof("🔍 [DISCOVERY] Tool %s assigned category: %s", tool.Function.Name, toolCategory)
						} else {
							// Tool not found in map - throw error
							bo.GetLogger().Errorf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name)
							bo.GetLogger().Errorf("❌ [DISCOVERY] Available keys in ToolCategories map: %v", getMapKeys(bo.ToolCategories))
							bo.GetLogger().Errorf("❌ [DISCOVERY] Tool name being looked up: '%s' (len=%d)", tool.Function.Name, len(tool.Function.Name))
							return nil, fmt.Errorf("tool %s not found in ToolCategories map - category is REQUIRED", tool.Function.Name)
						}
					} else {
						bo.GetLogger().Errorf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name)
						return nil, fmt.Errorf("ToolCategories map is nil - category is REQUIRED for tool %s", tool.Function.Name)
					}

					// Validate category is not empty
					if toolCategory == "" {
						return nil, fmt.Errorf("tool %s has empty category - category is REQUIRED", tool.Function.Name)
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						return nil, fmt.Errorf("failed to register tool %s: %w", tool.Function.Name, err)
					}
				} else {
					bo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		// Log summary of category assignments
		categorySummary := make(map[string]int)
		for _, tool := range customTools {
			if tool.Function != nil {
				toolName := tool.Function.Name
				category := "custom"
				if bo.ToolCategories != nil {
					if cat, exists := bo.ToolCategories[toolName]; exists {
						category = cat
					}
				}
				categorySummary[category]++
			}
		}
		bo.GetLogger().Infof("🔍 [DISCOVERY] Category assignment summary:")
		for category, count := range categorySummary {
			bo.GetLogger().Infof("🔍 [DISCOVERY]   - %s: %d tools", category, count)
		}

		bo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())

		// 🔧 CRITICAL FIX: Explicitly update code execution registry after all tools are registered
		// This ensures workspace and human tools are available in code execution mode
		if bo.GetUseCodeExecutionMode() {
			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				bo.GetLogger().Warnf("⚠️ Failed to update code execution registry for %s: %v", agentName, err)
				// Don't fail agent creation if registry update fails, but log the warning
			} else {
				bo.GetLogger().Infof("✅ [CODE_EXECUTION] Registry updated for %s agent - workspace and human tools are now available", agentName)
			}
		}
	}

	// Processors are now stored in BaseOrchestratorAgent, agent can use them directly
	return agent, nil
}

// SetupStandardAgent removed: setup is now performed inline in CreateAndSetupStandardAgent

// setupAgent removed: logic is now inlined in CreateAndSetupStandardAgent

// ReadWorkspaceFile reads a file from the workspace and returns its content
func (bo *BaseOrchestrator) ReadWorkspaceFile(ctx context.Context, filePath string) (string, error) {
	bo.GetLogger().Infof("📖 Reading workspace file: %s", filePath)

	// Prepare tool call parameters
	readArgs := map[string]interface{}{
		"filepath": filePath,
	}

	// Get the tool executor
	readExecutorInterface, exists := bo.WorkspaceToolExecutors["read_workspace_file"]
	if !exists {
		return "", fmt.Errorf("read_workspace_file tool executor not found")
	}

	readExecutor, ok := readExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		return "", fmt.Errorf("read_workspace_file tool executor has wrong type")
	}

	// Execute the tool call using existing workspace tool logic
	readResult, err := readExecutor(ctx, readArgs)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Parse the response - handleReadWorkspaceFile returns only the Data field from API response
	var fileData struct {
		Filepath string `json:"filepath"`
		Content  string `json:"content"`
	}

	if err := json.Unmarshal([]byte(readResult), &fileData); err != nil {
		return "", fmt.Errorf("failed to parse workspace response: %w", err)
	}

	// Extract content directly from the parsed data
	fileContent := fileData.Content

	if fileContent == "" {
		return "", fmt.Errorf("no content found in workspace response")
	}

	bo.GetLogger().Infof("✅ Successfully read file: %s (%d characters)", filePath, len(fileContent))
	return fileContent, nil
}

// CheckWorkspaceFileExists checks if a file exists in the workspace
// Uses ReadWorkspaceFile internally but returns a boolean instead of content
func (bo *BaseOrchestrator) CheckWorkspaceFileExists(ctx context.Context, filePath string) (bool, error) {
	bo.GetLogger().Infof("🔍 Checking if workspace file exists: %s", filePath)

	_, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			bo.GetLogger().Infof("📋 File does not exist: %s", filePath)
			return false, nil
		}
		// Other errors should be returned
		return false, err
	}

	bo.GetLogger().Infof("✅ File exists: %s", filePath)
	return true, nil
}

// RequestHumanFeedback is a common function for requesting human feedback with blocking behavior
// Returns: (approved bool, feedback string, error)
func (bo *BaseOrchestrator) RequestHumanFeedback(
	ctx context.Context,
	requestID string,
	question string,
	context string,
	sessionID string,
	workflowID string,
) (bool, string, error) {
	bo.GetLogger().Infof("🤔 Requesting human feedback: %s", question)

	// Emit human feedback request event
	feedbackEvent := &events.BlockingHumanFeedbackEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Question:      question,
		AllowFeedback: true,
		Context:       context,
		SessionID:     sessionID,
		WorkflowID:    workflowID,
		RequestID:     requestID,
	}

	// Emit the event using the public method
	agentEvent := &events.AgentEvent{
		Type:      events.BlockingHumanFeedback,
		Timestamp: time.Now(),
		Data:      feedbackEvent,
	}

	// Use the context-aware bridge to emit the event
	bridge := bo.GetContextAwareBridge()
	if bridge == nil {
		bo.GetLogger().Errorf("❌ Context-aware bridge is nil, cannot emit blocking human feedback event")
		return false, "", fmt.Errorf("context-aware bridge is nil")
	}
	bo.GetLogger().Infof("📤 Attempting to emit blocking_human_feedback event via context-aware bridge")
	if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Errorf("❌ Failed to emit human feedback event: %w", err)
		return false, "", fmt.Errorf("failed to emit event: %w", err)
	}
	bo.GetLogger().Infof("✅ Successfully emitted blocking_human_feedback event: requestID=%s", requestID)

	// Use HumanFeedbackStore to wait for response
	feedbackStore := virtualtools.GetHumanFeedbackStore()

	// Create feedback request (this registers it in the store)
	if err := feedbackStore.CreateRequest(requestID, question); err != nil {
		return false, "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	bo.GetLogger().Infof("⏸️ Orchestrator paused, waiting for human response (timeout: 10 minutes)...")

	// BLOCKING CALL - waits here until response or timeout
	response, err := feedbackStore.WaitForResponse(requestID, 10*time.Minute)
	if err != nil {
		return false, "", fmt.Errorf("timeout waiting for human feedback: %w", err)
	}

	bo.GetLogger().Infof("▶️ Orchestrator resumed with human response: %s", response)

	// Parse response
	// Expected format: "Approve" or feedback text for revision
	if strings.TrimSpace(response) == "Approve" {
		bo.GetLogger().Infof("✅ User approved via button, continuing")
		return true, "", nil
	}

	// Default: treat as feedback for revision
	bo.GetLogger().Infof("🔄 User provided feedback: %s", response)
	return false, response, nil
}

// RequestYesNoFeedback requests simple yes/no feedback from user with Approve/Reject buttons
// Returns: (approved bool, error)
func (bo *BaseOrchestrator) RequestYesNoFeedback(
	ctx context.Context,
	requestID string,
	question string,
	yesLabel string,
	noLabel string,
	context string,
	sessionID string,
	workflowID string,
) (bool, error) {
	bo.GetLogger().Infof("🤔 Requesting yes/no feedback: %s", question)

	// Set default labels if not provided
	if yesLabel == "" {
		yesLabel = "Approve"
	}
	if noLabel == "" {
		noLabel = "Reject"
	}

	// Emit human feedback request event with yes/no only mode
	feedbackEvent := &events.BlockingHumanFeedbackEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Question:      question,
		AllowFeedback: false, // No textarea in yes/no mode
		YesNoOnly:     true,  // Enable yes/no only mode
		YesLabel:      yesLabel,
		NoLabel:       noLabel,
		Context:       context,
		SessionID:     sessionID,
		WorkflowID:    workflowID,
		RequestID:     requestID,
	}

	// Emit the event
	agentEvent := &events.AgentEvent{
		Type:      events.BlockingHumanFeedback,
		Timestamp: time.Now(),
		Data:      feedbackEvent,
	}

	if err := bo.GetContextAwareBridge().HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to emit yes/no feedback event: %w", err)
	}

	// Wait for response
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	if err := feedbackStore.CreateRequest(requestID, question); err != nil {
		return false, fmt.Errorf("failed to create feedback request: %w", err)
	}

	bo.GetLogger().Infof("⏸️ Orchestrator paused, waiting for yes/no response...")

	response, err := feedbackStore.WaitForResponse(requestID, 10*time.Minute)
	if err != nil {
		return false, fmt.Errorf("timeout waiting for feedback: %w", err)
	}

	bo.GetLogger().Infof("▶️ Orchestrator resumed with response: %s", response)

	// Parse response: "Approve" means Yes, anything else means No
	if strings.TrimSpace(response) == "Approve" {
		bo.GetLogger().Infof("✅ User selected Yes (Approve)")
		return true, nil
	}

	bo.GetLogger().Infof("❌ User selected No (Reject)")
	return false, nil
}

// RequestMultipleChoiceFeedback requests multiple-choice feedback from user
// Returns: (choice string, error) where choice is "option0", "option1", "option2", etc. (0-based index)
func (bo *BaseOrchestrator) RequestMultipleChoiceFeedback(
	ctx context.Context,
	requestID string,
	question string,
	options []string,
	context string,
	sessionID string,
	workflowID string,
) (string, error) {
	bo.GetLogger().Infof("🤔 Requesting multiple-choice feedback: %s (%d options)", question, len(options))

	if len(options) == 0 {
		return "", fmt.Errorf("at least one option is required")
	}

	// Emit human feedback request event with multiple-choice mode
	feedbackEvent := &events.BlockingHumanFeedbackEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Question:      question,
		AllowFeedback: false, // No textarea in multiple-choice mode
		Options:       options,
		Context:       context,
		SessionID:     sessionID,
		WorkflowID:    workflowID,
		RequestID:     requestID,
	}

	// Emit the event
	agentEvent := &events.AgentEvent{
		Type:      events.BlockingHumanFeedback,
		Timestamp: time.Now(),
		Data:      feedbackEvent,
	}

	if err := bo.GetContextAwareBridge().HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to emit multiple-choice feedback event: %w", err)
	}

	// Wait for response
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	if err := feedbackStore.CreateRequest(requestID, question); err != nil {
		return "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	bo.GetLogger().Infof("⏸️ Orchestrator paused, waiting for multiple-choice response...")

	response, err := feedbackStore.WaitForResponse(requestID, 10*time.Minute)
	if err != nil {
		return "", fmt.Errorf("timeout waiting for feedback: %w", err)
	}

	bo.GetLogger().Infof("▶️ Orchestrator resumed with response: %s", response)

	// Parse response: should be "option0", "option1", "option2", etc. (0-based)
	response = strings.TrimSpace(response)

	// Validate response format (option0, option1, option2, etc.)
	if strings.HasPrefix(response, "option") {
		// Extract index from "option0", "option1", etc.
		indexStr := strings.TrimPrefix(response, "option")
		index, err := strconv.Atoi(indexStr)
		if err == nil && index >= 0 && index < len(options) {
			bo.GetLogger().Infof("✅ User selected: %s (option %d: %s)", response, index, options[index])
			return response, nil
		}
	}

	// Default to option0 if response is unclear
	bo.GetLogger().Warnf("⚠️ Unexpected response format: %s, defaulting to option0", response)
	return "option0", nil
}

// WriteWorkspaceFile writes content to a file in the workspace using MCP tools
func (bo *BaseOrchestrator) WriteWorkspaceFile(ctx context.Context, filePath string, content string) error {
	bo.GetLogger().Infof("📝 Writing workspace file: %s (%d characters)", filePath, len(content))

	// Prepare tool call parameters
	writeArgs := map[string]interface{}{
		"filepath": filePath,
		"content":  content,
	}

	// Get the tool executor
	writeExecutorInterface, exists := bo.WorkspaceToolExecutors["update_workspace_file"]
	if !exists {
		return fmt.Errorf("update_workspace_file tool executor not found")
	}

	writeExecutor, ok := writeExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		return fmt.Errorf("update_workspace_file tool executor has wrong type")
	}

	// Execute the tool call using existing workspace tool logic
	_, err := writeExecutor(ctx, writeArgs)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	bo.GetLogger().Infof("✅ Successfully wrote file: %s (%d characters)", filePath, len(content))
	return nil
}

// DeleteWorkspaceFile deletes a file from the workspace using MCP tools
func (bo *BaseOrchestrator) DeleteWorkspaceFile(ctx context.Context, filePath string) error {
	bo.GetLogger().Infof("🗑️ Deleting workspace file: %s", filePath)

	// Prepare tool call parameters
	deleteArgs := map[string]interface{}{
		"filepath": filePath,
	}

	// Get the tool executor
	deleteExecutorInterface, exists := bo.WorkspaceToolExecutors["delete_workspace_file"]
	if !exists {
		return fmt.Errorf("delete_workspace_file tool executor not found")
	}

	deleteExecutor, ok := deleteExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		return fmt.Errorf("delete_workspace_file tool executor has wrong type")
	}

	// Execute the tool call using existing workspace tool logic
	_, err := deleteExecutor(ctx, deleteArgs)
	if err != nil {
		return fmt.Errorf("failed to delete file %s: %w", filePath, err)
	}

	bo.GetLogger().Infof("✅ Successfully deleted file: %s", filePath)
	return nil
}

// CleanupDirectory recursively deletes all files and directories in a directory using list_workspace_files
// to enumerate files recursively, then deletes all files first, then directories (deepest first)
func (bo *BaseOrchestrator) CleanupDirectory(ctx context.Context, dirPath string, dirName string) error {
	bo.GetLogger().Infof("🧹 Cleaning up %s directory recursively: %s", dirName, dirPath)

	// Use list_workspace_files to enumerate all files in the directory recursively, then delete them
	listExecutorInterface, exists := bo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor not found, skipping directory cleanup")
		return nil
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor has wrong type, skipping directory cleanup")
		return nil
	}

	// Call list_workspace_files to get all files recursively (use high max_depth for recursive listing)
	listArgs := map[string]interface{}{
		"folder":    dirPath,
		"max_depth": 100, // High depth to list all files and directories recursively
	}

	fileListJSON, err := listExecutor(ctx, listArgs)
	if err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to list files in %s directory: %v (directory may not exist or be empty)", dirPath, err)
		return nil // Don't fail - directory may be empty or not exist
	}

	// Parse the JSON response to extract file paths
	var filesList []map[string]interface{}
	if err := json.Unmarshal([]byte(fileListJSON), &filesList); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to parse file list JSON from %s directory: %v", dirPath, err)
		// Try alternative format - might be a single object with a "files" array
		var altFormat map[string]interface{}
		if err2 := json.Unmarshal([]byte(fileListJSON), &altFormat); err2 == nil {
			if filesArray, ok := altFormat["files"].([]interface{}); ok {
				for _, fileInterface := range filesArray {
					if fileMap, ok := fileInterface.(map[string]interface{}); ok {
						filesList = append(filesList, fileMap)
					}
				}
			}
		}
		if len(filesList) == 0 {
			bo.GetLogger().Infof("ℹ️ No files found in %s directory (may be empty)", dirName)
			return nil
		}
	}

	// Separate files and directories for proper deletion order
	var filesToDelete []string
	var dirsToDelete []string

	for _, fileInfo := range filesList {
		filepath, ok := fileInfo["filepath"].(string)
		if !ok || filepath == "" {
			continue
		}

		// Skip the root directory itself
		if filepath == dirPath {
			continue
		}

		// Check if it's a directory
		if isDirectory, ok := fileInfo["is_directory"].(bool); ok && isDirectory {
			dirsToDelete = append(dirsToDelete, filepath)
		} else {
			filesToDelete = append(filesToDelete, filepath)
		}
	}

	// Delete all files first
	deletedFileCount := 0
	for _, filepath := range filesToDelete {
		if err := bo.DeleteWorkspaceFile(ctx, filepath); err == nil {
			deletedFileCount++
			bo.GetLogger().Infof("🗑️ Deleted file: %s", filepath)
		} else {
			// Log but don't fail - some files might already be deleted or have other issues
			bo.GetLogger().Warnf("⚠️ Failed to delete file %s: %v", filepath, err)
		}
	}

	// Delete directories (deepest first - sort by path length descending)
	// This ensures child directories are deleted before parent directories
	sortKey := func(path string) int {
		// Count path separators to determine depth
		count := 0
		for _, char := range path {
			if char == '/' || char == '\\' {
				count++
			}
		}
		return count
	}

	// Sort directories by depth (deepest first)
	for i := 0; i < len(dirsToDelete)-1; i++ {
		for j := i + 1; j < len(dirsToDelete); j++ {
			if sortKey(dirsToDelete[i]) < sortKey(dirsToDelete[j]) {
				dirsToDelete[i], dirsToDelete[j] = dirsToDelete[j], dirsToDelete[i]
			}
		}
	}

	deletedDirCount := 0
	for _, dirpath := range dirsToDelete {
		// Delete directory using DeleteWorkspaceFile (workspace tool should handle directories)
		if err := bo.DeleteWorkspaceFile(ctx, dirpath); err == nil {
			deletedDirCount++
			bo.GetLogger().Infof("🗑️ Deleted directory: %s", dirpath)
		} else {
			// Log but don't fail - some directories might already be deleted or have other issues
			bo.GetLogger().Warnf("⚠️ Failed to delete directory %s: %v", dirpath, err)
		}
	}

	totalDeleted := deletedFileCount + deletedDirCount
	if totalDeleted > 0 {
		bo.GetLogger().Infof("✅ Cleaned up %d files and %d directories from %s directory (total: %d)", deletedFileCount, deletedDirCount, dirName, totalDeleted)
	} else {
		bo.GetLogger().Infof("ℹ️ No files or directories found to delete in %s directory (may have been empty)", dirName)
	}

	return nil
}

// ListWorkspaceDirectories lists all directories in a given path
// Returns a slice of directory names (not full paths)
func (bo *BaseOrchestrator) ListWorkspaceDirectories(ctx context.Context, dirPath string) ([]string, error) {
	bo.GetLogger().Infof("📁 Listing directories in: %s", dirPath)

	// Use list_workspace_files to enumerate directories
	listExecutorInterface, exists := bo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor not found, returning empty list")
		return []string{}, nil
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor has wrong type, returning empty list")
		return []string{}, nil
	}

	// Call list_workspace_files with max_depth: 1 to only get immediate children
	listArgs := map[string]interface{}{
		"folder":    dirPath,
		"max_depth": 1, // Only list immediate children (directories)
	}

	bo.GetLogger().Infof("🔍 DEBUG ListWorkspaceDirectories: Calling list_workspace_files with folder=%s, max_depth=1", dirPath)
	fileListJSON, err := listExecutor(ctx, listArgs)
	bo.GetLogger().Infof("🔍 DEBUG ListWorkspaceDirectories: list_workspace_files returned, error=%v, response_length=%d", err, len(fileListJSON))
	if err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to list files in %s directory: %v (directory may not exist or be empty)", dirPath, err)
		return []string{}, nil // Don't fail - directory may be empty or not exist
	}

	// Parse the JSON response to extract file paths
	var filesList []map[string]interface{}
	if err := json.Unmarshal([]byte(fileListJSON), &filesList); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to parse file list JSON from %s directory: %v", dirPath, err)
		// Try alternative format - might be a single object with a "files" array
		var altFormat map[string]interface{}
		if err2 := json.Unmarshal([]byte(fileListJSON), &altFormat); err2 == nil {
			if filesArray, ok := altFormat["files"].([]interface{}); ok {
				for _, fileInterface := range filesArray {
					if fileMap, ok := fileInterface.(map[string]interface{}); ok {
						filesList = append(filesList, fileMap)
					}
				}
			}
		}
		if len(filesList) == 0 {
			bo.GetLogger().Infof("ℹ️ No files found in %s directory (may be empty)", dirPath)
			return []string{}, nil
		}
	}

	// Extract only directories (folders) from the list
	var directoryNames []string
	for _, fileInfo := range filesList {
		filepath, ok := fileInfo["filepath"].(string)
		if !ok || filepath == "" {
			continue
		}

		// Check if it's a directory
		isDirectory, ok := fileInfo["is_directory"].(bool)
		if !ok || !isDirectory {
			continue
		}

		// Skip the directory itself (if filepath equals dirPath)
		if filepath == dirPath {
			continue
		}

		// Extract directory name (last part of path)
		// filepath will be like "workspace/runs/initial" or "runs/initial"
		// We want just "initial"
		dirName := filepath
		if strings.Contains(dirName, "/") {
			parts := strings.Split(dirName, "/")
			dirName = parts[len(parts)-1]
		}

		// Skip if it's empty
		if dirName != "" {
			directoryNames = append(directoryNames, dirName)
		}
	}

	bo.GetLogger().Infof("📁 Found %d directories: %v", len(directoryNames), directoryNames)
	return directoryNames, nil
}

// ListWorkspaceFiles lists all files and directories in a given path
// Returns a slice of file/directory names (not full paths)
func (bo *BaseOrchestrator) ListWorkspaceFiles(ctx context.Context, dirPath string) ([]string, error) {
	bo.GetLogger().Infof("📁 Listing files and directories in: %s", dirPath)

	// Use list_workspace_files to enumerate files and directories
	listExecutorInterface, exists := bo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor not found, returning empty list")
		return []string{}, nil
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor has wrong type, returning empty list")
		return []string{}, nil
	}

	// Call list_workspace_files with max_depth: 1 to only get immediate children
	listArgs := map[string]interface{}{
		"folder":    dirPath,
		"max_depth": 1, // Only list immediate children
	}

	fileListJSON, err := listExecutor(ctx, listArgs)
	if err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to list files in %s directory: %v (directory may not exist or be empty)", dirPath, err)
		return []string{}, nil // Don't fail - directory may be empty or not exist
	}

	// Parse the JSON response to extract file paths
	var filesList []map[string]interface{}
	if err := json.Unmarshal([]byte(fileListJSON), &filesList); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to parse file list JSON from %s directory: %v", dirPath, err)
		// Try alternative format - might be a single object with a "files" array
		var altFormat map[string]interface{}
		if err2 := json.Unmarshal([]byte(fileListJSON), &altFormat); err2 == nil {
			if filesArray, ok := altFormat["files"].([]interface{}); ok {
				for _, fileInterface := range filesArray {
					if fileMap, ok := fileInterface.(map[string]interface{}); ok {
						filesList = append(filesList, fileMap)
					}
				}
			}
		}
		if len(filesList) == 0 {
			bo.GetLogger().Infof("ℹ️ No files found in %s directory (may be empty)", dirPath)
			return []string{}, nil
		}
	}

	// Extract file and directory names (last part of path)
	var names []string
	for _, fileInfo := range filesList {
		filepath, ok := fileInfo["filepath"].(string)
		if !ok || filepath == "" {
			continue
		}

		// Skip the directory itself (if filepath equals dirPath)
		if filepath == dirPath {
			continue
		}

		// Extract name (last part of path)
		name := filepath
		if strings.Contains(name, "/") {
			parts := strings.Split(name, "/")
			name = parts[len(parts)-1]
		}

		// Skip if it's empty
		if name != "" {
			names = append(names, name)
		}
	}

	bo.GetLogger().Infof("📁 Found %d files/directories: %v", len(names), names)
	return names, nil
}

// getToolNamesByCategory returns a set of tool names for a given category
// This uses the actual tool creation functions as the source of truth
func getToolNamesByCategory(category string) map[string]bool {
	toolNames := make(map[string]bool)

	switch category {
	case "workspace_tools":
		// Get tool names from workspace tool executors (source of truth)
		executors := virtualtools.CreateWorkspaceToolExecutors()
		for toolName := range executors {
			toolNames[toolName] = true
		}
	case "human_tools":
		// Get tool names from human tool executors (source of truth)
		executors := virtualtools.CreateHumanToolExecutors()
		for toolName := range executors {
			toolNames[toolName] = true
		}
		// Future categories can be added here:
		// case "memory_tools":
		//     executors := virtualtools.CreateMemoryToolExecutors()
		//     for toolName := range executors {
		//         toolNames[toolName] = true
		//     }
	}

	return toolNames
}

// ConvertOldFormatToNewFormat converts old format (categories + tools) to new unified format
// Old: enabledCategories=["workspace_tools"], enabledTools=["read_workspace_file"]
// New: ["workspace_tools:*", "workspace_tools:read_workspace_file"]
//
// If enabledTools already contains entries with ":" (new format), returns them as-is
func ConvertOldFormatToNewFormat(enabledCategories []string, enabledTools []string) []string {
	// Check if enabledTools is already in new format (contains ":")
	if len(enabledTools) > 0 {
		firstEntry := enabledTools[0]
		if strings.Contains(firstEntry, ":") {
			// Already in new format, return as-is (ignore enabledCategories)
			return enabledTools
		}
	}

	// Old format - convert it
	result := make([]string, 0)

	// Convert categories to "category:*" format
	for _, category := range enabledCategories {
		result = append(result, category+":*")
	}

	// Convert specific tools - need to determine category for each tool
	allCategoryTools := make(map[string]string) // toolName -> category
	for _, category := range []string{"workspace_tools", "human_tools"} {
		categoryToolNames := getToolNamesByCategory(category)
		for toolName := range categoryToolNames {
			allCategoryTools[toolName] = category
		}
	}

	// Add specific tools with their category prefix
	for _, toolName := range enabledTools {
		if category, exists := allCategoryTools[toolName]; exists {
			result = append(result, category+":"+toolName)
		} else {
			// Unknown tool, add without category (will be skipped in parsing)
			result = append(result, "unknown:"+toolName)
		}
	}

	return result
}

// FilterCustomToolsByCategory filters custom tools and executors based on enabled tools
// Format: single array with entries like "category:tool" or "category:*"
//   - "workspace_tools:*" → all tools from CreateWorkspaceToolExecutors()
//   - "workspace_tools:read_workspace_file" → specific tool
//   - "human_tools:*" → all tools from CreateHumanToolExecutors()
//   - "human_tools:human_feedback" → specific tool
//
// Category identification uses the actual tool creation functions as the source of truth
// If enabledTools is empty, return all tools (backward compatible - default behavior)
func FilterCustomToolsByCategory(
	allTools []llmtypes.Tool,
	allExecutors map[string]interface{},
	enabledTools []string, // format: "category:tool" or "category:*"
) ([]llmtypes.Tool, map[string]interface{}) {
	// Build a set of enabled tool names
	enabledToolNames := make(map[string]bool)

	// Parse enabled tools array
	for _, entry := range enabledTools {
		// Format: "category:tool" or "category:*"
		// Use SplitN to handle tool names that might contain colons (split only on first colon)
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			// Invalid format, skip
			continue
		}

		category := parts[0]
		toolSpec := parts[1]

		if toolSpec == "*" {
			// Enable all tools from this category
			categoryToolNames := getToolNamesByCategory(category)
			for toolName := range categoryToolNames {
				enabledToolNames[toolName] = true
			}
		} else {
			// Enable specific tool
			enabledToolNames[toolSpec] = true
		}
	}

	// If nothing is specified, return all tools (backward compatible)
	if len(enabledTools) == 0 {
		return allTools, allExecutors
	}

	// Filter tools based on enabled tool names
	var filteredTools []llmtypes.Tool
	filteredExecutors := make(map[string]interface{})

	for _, tool := range allTools {
		toolName := tool.Function.Name

		// Check if tool is in the enabled set
		if enabledToolNames[toolName] {
			filteredTools = append(filteredTools, tool)
			// Include corresponding executor if it exists
			if executor, exists := allExecutors[toolName]; exists {
				filteredExecutors[toolName] = executor
			}
		}
	}

	return filteredTools, filteredExecutors
}

// AccumulateStepTokens accumulates token usage for a specific step
func (bo *BaseOrchestrator) AccumulateStepTokens(phase string, step int, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens int, llmCallCount int, cacheDiscount float64) {
	bo.stepTokenMutex.Lock()
	defer bo.stepTokenMutex.Unlock()

	key := fmt.Sprintf("%s:%d", phase, step)
	usage, exists := bo.stepTokenAccumulator[key]
	if !exists {
		usage = &StepTokenUsage{}
		bo.stepTokenAccumulator[key] = usage
	}

	usage.PromptTokens += promptTokens
	usage.CompletionTokens += completionTokens
	usage.TotalTokens += totalTokens
	usage.CacheTokens += cacheTokens
	usage.ReasoningTokens += reasoningTokens
	usage.LLMCallCount += llmCallCount
	if cacheTokens > 0 {
		usage.CacheEnabledCallCount++
	}
	usage.CacheDiscountSum += cacheDiscount
}

// GetStepTokenUsage retrieves accumulated token usage for a specific step
func (bo *BaseOrchestrator) GetStepTokenUsage(phase string, step int) *StepTokenUsage {
	bo.stepTokenMutex.RLock()
	defer bo.stepTokenMutex.RUnlock()

	key := fmt.Sprintf("%s:%d", phase, step)
	usage, exists := bo.stepTokenAccumulator[key]
	if !exists {
		return &StepTokenUsage{} // Return zero values if step not found
	}

	// Return a copy to avoid race conditions
	return &StepTokenUsage{
		PromptTokens:          usage.PromptTokens,
		CompletionTokens:      usage.CompletionTokens,
		TotalTokens:           usage.TotalTokens,
		CacheTokens:           usage.CacheTokens,
		ReasoningTokens:       usage.ReasoningTokens,
		LLMCallCount:          usage.LLMCallCount,
		CacheEnabledCallCount: usage.CacheEnabledCallCount,
		CacheDiscountSum:      usage.CacheDiscountSum,
	}
}

// EmitStepTokenUsage emits a step token usage summary event and optionally clears the accumulated data
func (bo *BaseOrchestrator) EmitStepTokenUsage(ctx context.Context, phase string, step int, stepTitle string, clearAfterEmit bool) {
	bo.stepTokenMutex.Lock()
	defer bo.stepTokenMutex.Unlock()

	key := fmt.Sprintf("%s:%d", phase, step)
	usage, exists := bo.stepTokenAccumulator[key]
	if !exists {
		bo.GetLogger().Warnf("⚠️ No token usage data found for step %s:%d", phase, step)
		return
	}

	// Create and emit step token usage event
	stepTokenEvent := events.NewStepTokenUsageEvent(
		phase,
		step,
		stepTitle,
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
		usage.CacheTokens,
		usage.ReasoningTokens,
		usage.LLMCallCount,
		usage.CacheEnabledCallCount,
	)

	bo.emitEvent(ctx, events.StepTokenUsage, stepTokenEvent)

	bo.GetLogger().Infof("📊 Emitted step token usage for %s:%d - Total: %d tokens (Prompt: %d, Completion: %d, Cache: %d, Reasoning: %d, Calls: %d)",
		phase, step, usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens, usage.CacheTokens, usage.ReasoningTokens, usage.LLMCallCount)

	// Clear accumulated data if requested
	if clearAfterEmit {
		delete(bo.stepTokenAccumulator, key)
	}
}
