package orchestrator

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetDefaultMaxTurnsFromEnv returns the default max turns from environment variable
// Checks MAX_TURNS and ORCHESTRATOR_MAX_TURNS (in that order)
// Returns 50 if neither is set or invalid
func GetDefaultMaxTurnsFromEnv() int {
	// Check MAX_TURNS first (more general)
	if envVal := os.Getenv("MAX_TURNS"); envVal != "" {
		if maxTurns, err := strconv.Atoi(envVal); err == nil && maxTurns > 0 {
			return maxTurns
		}
	}
	// Fall back to ORCHESTRATOR_MAX_TURNS (orchestrator-specific)
	if envVal := os.Getenv("ORCHESTRATOR_MAX_TURNS"); envVal != "" {
		if maxTurns, err := strconv.Atoi(envVal); err == nil && maxTurns > 0 {
			return maxTurns
		}
	}
	// Default to 50 if neither is set or invalid
	return 50
}

// SecretEntry represents a decrypted secret (name + value, where value can be any text)
type SecretEntry struct {
	Name  string
	Value string
}

// Orchestrator defines the common interface for all orchestrators
type Orchestrator interface {
	// Execute performs the orchestration logic
	Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error)

	// GetType returns the orchestrator type
	GetType() string
}

// BaseOrchestrator provides unified base functionality for all orchestrators
type BaseOrchestrator struct {
	// Context-aware event bridge for orchestrator-level events
	contextAwareBridge mcpagent.AgentEventListener

	// Logger for the orchestrator
	logger loggerv2.Logger

	// Workspace tools for file operations
	WorkspaceTools         []llmtypes.Tool
	WorkspaceToolExecutors map[string]interface{}
	ToolCategories         map[string]string // Tool name to category mapping

	// Orchestrator type and configuration
	orchestratorType OrchestratorType
	startTime        time.Time

	// Common configuration shared between orchestrators
	mcpConfigPath        string
	temperature          float64
	agentMode            string
	selectedServers      []string
	selectedTools        []string   // Selected tools in "server:tool" format
	selectedSkills       []string       // Selected skill folder names for workflow
	secrets              []SecretEntry  // Decrypted secrets to inject into agents
	useCodeExecutionMode bool           // MCP code execution mode
	llmConfig            *LLMConfig // LLM configuration
	maxTurns             int        // Maximum turns for the orchestrator

	// Optional simple state (for workflow orchestrators)
	objective     string
	workspacePath string

	// Folder guard paths for fine-grained access control
	folderGuardReadPaths  []string
	folderGuardWritePaths []string

	// Iteration folder for token persistence (workflow-specific)
	iterationFolder string

	// Context summarization configuration
	enableContextSummarization     bool
	summarizeOnTokenThreshold      bool
	tokenThresholdPercent          float64
	summarizeOnFixedTokenThreshold bool
	fixedTokenThreshold            int
	summaryKeepLastMessages        int

	// Context editing configuration
	enableContextEditing        bool // Enable context editing (dynamic context reduction)
	contextEditingThreshold     int  // Token threshold for context editing
	contextEditingTurnThreshold int  // Turn age threshold for context editing

	// Context offloading configuration
	largeOutputThreshold int // Token threshold for context offloading (0 = use default: 10000)

	// MCP session ID for connection sharing across agents
	// When set, all agents created by this orchestrator share MCP connections
	mcpSessionID string

	// Reference to the workspace executor env map (from CreateWorkspaceAdvancedToolExecutorsWithSession*)
	// When the MCP session ID changes (e.g., per-group in batch execution), we update
	// MCP_API_URL and MCP_SESSION_ID in this map so that code execution mode shell commands
	// use the correct session-scoped URL (preventing new browser tabs per Playwright call).
	workspaceEnvRef map[string]string
	// Mutex protecting concurrent writes to workspaceEnvRef (parallel sub-agents)
	workspaceEnvMu sync.Mutex

	// Browser downloads path (relative to workspace, e.g., "runs/iteration-2/xspaces/execution/Downloads")
	// Set by setupBrowserDownloadsPathOverride when agent-browser is detected.
	// Injected into context via BrowserDownloadsPathKey so the browser executor uses it as working directory.
	browserDownloadsPath string
}

// NewBaseOrchestrator creates a new unified base orchestrator
// Note: provider and model parameters removed - LLM selection uses temp override → step config → preset LLM priority
func NewBaseOrchestrator(
	logger loggerv2.Logger,
	eventBridge mcpagent.AgentEventListener,
	orchestratorType OrchestratorType,
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
	toolCategories map[string]string, // Tool name to category mapping
) (*BaseOrchestrator, error) {

	// Create context-aware event bridge that wraps the main event bridge
	contextAwareBridge := NewContextAwareEventBridge(eventBridge, logger)

	// Load context summarization configuration from environment variables
	// Default to enabled (true), can be disabled via ENABLE_CONTEXT_SUMMARIZATION=false
	enableContextSummarization := os.Getenv("ENABLE_CONTEXT_SUMMARIZATION") != "false"
	// Default to enabled (true), can be disabled via SUMMARIZE_ON_TOKEN_THRESHOLD=false
	summarizeOnTokenThreshold := os.Getenv("SUMMARIZE_ON_TOKEN_THRESHOLD") != "false"
	tokenThresholdPercent := 0.8 // Default to 80%
	if envVal := os.Getenv("TOKEN_THRESHOLD_PERCENT"); envVal != "" {
		if threshold, err := strconv.ParseFloat(envVal, 64); err == nil && threshold > 0 && threshold <= 1.0 {
			tokenThresholdPercent = threshold
		}
	}
	summaryKeepLastMessages := 4 // Default to 4 messages (roughly 2 turns)
	if envVal := os.Getenv("SUMMARY_KEEP_LAST_MESSAGES"); envVal != "" {
		if keepLast, err := strconv.Atoi(envVal); err == nil && keepLast > 0 {
			summaryKeepLastMessages = keepLast
		}
	}
	summarizeOnFixedTokenThreshold := true // Default to enabled with 80k token threshold
	if envVal := os.Getenv("SUMMARIZE_ON_FIXED_TOKEN_THRESHOLD"); envVal == "false" {
		summarizeOnFixedTokenThreshold = false
	}
	fixedTokenThreshold := 80000 // Default to 80k tokens (triggers before 100k max limit)
	if envVal := os.Getenv("FIXED_TOKEN_THRESHOLD"); envVal != "" {
		if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
			fixedTokenThreshold = threshold
		}
	}

	// Load context editing configuration from environment variables
	// Default to disabled (false), can be enabled via ENABLE_CONTEXT_EDITING=true
	enableContextEditing := os.Getenv("ENABLE_CONTEXT_EDITING") == "true"
	contextEditingThreshold := 10000 // Default to 10k tokens - compact outputs larger than this (matches library default)
	if envVal := os.Getenv("CONTEXT_EDITING_THRESHOLD"); envVal != "" {
		if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
			contextEditingThreshold = threshold
		}
	}
	contextEditingTurnThreshold := 20 // Default to 20 turns - compact outputs older than this
	if envVal := os.Getenv("CONTEXT_EDITING_TURN_THRESHOLD"); envVal != "" {
		if turnThreshold, err := strconv.Atoi(envVal); err == nil && turnThreshold > 0 {
			contextEditingTurnThreshold = turnThreshold
		}
	}

	// Load large output threshold for context offloading from environment
	// Default to 0 which means use library default (10000 tokens)
	largeOutputThreshold := 0
	if envVal := os.Getenv("LARGE_OUTPUT_THRESHOLD"); envVal != "" {
		if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
			largeOutputThreshold = threshold
			logger.Info(fmt.Sprintf("🔧 Large output threshold set to %d tokens from env", largeOutputThreshold))
		}
	}

	// Default maxTurns from environment variable or 50 if not provided or 0
	if maxTurns <= 0 {
		maxTurns = GetDefaultMaxTurnsFromEnv()
		logger.Info(fmt.Sprintf("🔧 MaxTurns not provided or 0, defaulting to %d (from env or 50)", maxTurns))
	}

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
		mcpConfigPath:        mcpConfigPath,
		temperature:          temperature,
		agentMode:            agentMode,
		selectedServers:      selectedServers,
		selectedTools:        selectedTools,        // NEW field
		useCodeExecutionMode: useCodeExecutionMode, // NEW field
		llmConfig:            llmConfig,
		maxTurns:             maxTurns,
		// Context summarization configuration
		enableContextSummarization:     enableContextSummarization,
		summarizeOnTokenThreshold:      summarizeOnTokenThreshold,
		tokenThresholdPercent:          tokenThresholdPercent,
		summarizeOnFixedTokenThreshold: summarizeOnFixedTokenThreshold,
		fixedTokenThreshold:            fixedTokenThreshold,
		summaryKeepLastMessages:        summaryKeepLastMessages,
		// Context editing configuration
		enableContextEditing:        enableContextEditing,
		contextEditingThreshold:     contextEditingThreshold,
		contextEditingTurnThreshold: contextEditingTurnThreshold,
		// Context offloading configuration
		largeOutputThreshold: largeOutputThreshold,
	}

	// Note: No fallback to orchestrator default provider/model - LLM selection uses temp override → step config → preset LLM priority
	// llmConfig.Primary should be populated by the caller if needed

	// Set token persister on bridge (no longer using accumulators)
	contextAwareBridge.SetTokenPersister(orchestrator)

	return orchestrator, nil
}

// applyIterationFolderToBridge applies the stored iteration folder to the context-aware bridge
// This ensures all agents created by this orchestrator automatically get the iteration folder
func (bo *BaseOrchestrator) applyIterationFolderToBridge() {
	if bo.iterationFolder != "" {
		if bridge, ok := bo.contextAwareBridge.(*ContextAwareEventBridge); ok {
			bridge.SetIterationFolder(bo.iterationFolder)
			bo.GetLogger().Debug(fmt.Sprintf("📁 Applied iteration folder to bridge: %s", bo.iterationFolder))
		}
	}
}

// getMapKeys returns all keys from a map as a slice (helper for logging)
func getMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// SetMCPSessionID sets the MCP session ID for connection sharing across agents
// When set, all agents created by this orchestrator share MCP connections
// Connections persist until CloseSession() is called (not when agents close)
//
// Also updates MCP_API_URL and MCP_SESSION_ID in the workspace executor env map
// (if SetWorkspaceEnvRef was called). This ensures code execution mode shell commands
// use the correct session-scoped URL, preventing session registry misses that cause
// new browser instances per Playwright call.
func (bo *BaseOrchestrator) SetMCPSessionID(sessionID string) {
	previousSessionID := bo.mcpSessionID
	bo.mcpSessionID = sessionID
	bo.logger.Info(fmt.Sprintf("🔗 Set MCP session ID for connection sharing: %s (previous: %s)", sessionID, previousSessionID))

	// Propagate session change to workspace executor env map for code execution mode
	if bo.workspaceEnvRef != nil && sessionID != "" {
		if baseURL := os.Getenv("MCP_API_URL"); baseURL != "" {
			bo.workspaceEnvMu.Lock()
			oldURL := bo.workspaceEnvRef["MCP_API_URL"]
			newURL := baseURL + "/s/" + sessionID
			bo.workspaceEnvRef["MCP_API_URL"] = newURL
			bo.workspaceEnvRef["MCP_SESSION_ID"] = sessionID
			bo.workspaceEnvMu.Unlock()
			bo.logger.Info(fmt.Sprintf("🔗 Updated workspace env MCP_API_URL: %s → %s", oldURL, newURL))
			bo.logger.Info(fmt.Sprintf("🔗 Updated workspace env MCP_SESSION_ID: %s", sessionID))
		} else {
			bo.logger.Debug("🔗 MCP_API_URL env not set, skipping workspace env update")
		}
	} else if bo.workspaceEnvRef == nil {
		bo.logger.Debug("🔗 No workspace env ref set, skipping workspace env update (workspaceEnvRef is nil)")
	}
}

// GetMCPSessionID returns the MCP session ID
func (bo *BaseOrchestrator) GetMCPSessionID() string {
	return bo.mcpSessionID
}

// SetWorkspaceEnvRef stores a reference to the workspace executor env map.
// This allows SetMCPSessionID to update MCP_API_URL and MCP_SESSION_ID in-place
// so that code execution mode shell commands automatically use the correct session URL.
// The env map is the same reference used by workspace.Client.ExtraEnv (Go maps are reference types).
func (bo *BaseOrchestrator) SetWorkspaceEnvRef(env map[string]string) {
	bo.workspaceEnvRef = env
	if env != nil {
		bo.logger.Info(fmt.Sprintf("🔗 Stored workspace env ref (keys: %v, MCP_API_URL=%s, MCP_SESSION_ID=%s)",
			getMapKeys(env), env["MCP_API_URL"], env["MCP_SESSION_ID"]))
	}
}

// GetWorkspaceEnvRef returns the workspace executor env map reference.
// Used to propagate the env ref from parent orchestrators to child orchestrators
// so that MCP_API_URL updates flow through when the session ID changes.
func (bo *BaseOrchestrator) GetWorkspaceEnvRef() map[string]string {
	return bo.workspaceEnvRef
}

// LockWorkspaceEnv locks the workspace env mutex.
// Must be called before writing to the map returned by GetWorkspaceEnvRef
// when concurrent access is possible (e.g. parallel sub-agent execution).
func (bo *BaseOrchestrator) LockWorkspaceEnv() {
	bo.workspaceEnvMu.Lock()
}

// UnlockWorkspaceEnv unlocks the workspace env mutex.
func (bo *BaseOrchestrator) UnlockWorkspaceEnv() {
	bo.workspaceEnvMu.Unlock()
}
