package orchestrator

import (
	"context"
	"sync"
	"time"

	"mcp-agent/agent_go/internal/utils"
	mcpagent "mcpagent/agent"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

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
