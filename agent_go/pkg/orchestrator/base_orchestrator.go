package orchestrator

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetDefaultMaxTurnsFromEnv returns the default max turns from environment variable
// Checks MAX_TURNS and ORCHESTRATOR_MAX_TURNS (in that order)
// Returns 100 if neither is set or invalid
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
	// Default to 100 if neither is set or invalid
	return 100
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

	// Iteration folder for token persistence (workflow-specific)
	iterationFolder string

	// Context summarization configuration
	enableContextSummarization bool
	summarizeOnTokenThreshold  bool
	tokenThresholdPercent      float64
	summaryKeepLastMessages    int
}

// NewBaseOrchestrator creates a new unified base orchestrator
func NewBaseOrchestrator(
	logger loggerv2.Logger,
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
	summaryKeepLastMessages := 8 // Default to 8 messages
	if envVal := os.Getenv("SUMMARY_KEEP_LAST_MESSAGES"); envVal != "" {
		if keepLast, err := strconv.Atoi(envVal); err == nil && keepLast > 0 {
			summaryKeepLastMessages = keepLast
		}
	}

	// Default maxTurns from environment variable or 100 if not provided or 0
	if maxTurns <= 0 {
		maxTurns = GetDefaultMaxTurnsFromEnv()
		logger.Info(fmt.Sprintf("🔧 MaxTurns not provided or 0, defaulting to %d (from env or 100)", maxTurns))
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
		// Context summarization configuration
		enableContextSummarization: enableContextSummarization,
		summarizeOnTokenThreshold:  summarizeOnTokenThreshold,
		tokenThresholdPercent:      tokenThresholdPercent,
		summaryKeepLastMessages:    summaryKeepLastMessages,
	}

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
