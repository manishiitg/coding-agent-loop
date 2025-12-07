package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"

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
	logger loggerv2.Logger

	// Workspace tools for file operations
	WorkspaceTools         []llmtypes.Tool
	WorkspaceToolExecutors map[string]interface{}
	ToolCategories         map[string]string // Tool name to category mapping

	// Orchestrator type and configuration
	orchestratorType OrchestratorType
	startTime        time.Time

	// Common configuration shared between orchestrators
	provider                 string
	model                    string
	mcpConfigPath            string
	temperature              float64
	agentMode                string
	selectedServers          []string
	selectedTools            []string   // Selected tools in "server:tool" format
	useCodeExecutionMode     bool       // MCP code execution mode
	useStepSpecificLearnings bool       // Store learnings in step-specific folders (execution/learnings/step-{X}/)
	llmConfig                *LLMConfig // LLM configuration
	maxTurns                 int        // Maximum turns for the orchestrator

	// Optional simple state (for workflow orchestrators)
	objective     string
	workspacePath string

	// Folder guard paths for fine-grained access control
	folderGuardReadPaths  []string
	folderGuardWritePaths []string

	// Step token tracking
	stepTokenAccumulator map[string]*StepTokenUsage // key format: "phase:step"
	stepTokenTitles      map[string]string          // key format: "phase:step" -> step title
	stepTokenMutex       sync.RWMutex

	// Model token tracking (internal accumulation uses raw integers)
	modelTokenAccumulator map[string]*ModelTokenUsageInternal // key: modelID
	modelTokenMutex       sync.RWMutex

	// Iteration folder for token persistence (workflow-specific)
	iterationFolder string
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
		provider:                 provider,
		model:                    model,
		mcpConfigPath:            mcpConfigPath,
		temperature:              temperature,
		agentMode:                agentMode,
		selectedServers:          selectedServers,
		selectedTools:            selectedTools,        // NEW field
		useCodeExecutionMode:     useCodeExecutionMode, // NEW field
		useStepSpecificLearnings: true,                 // Default to true: store learnings in step-specific folders
		llmConfig:                llmConfig,
		maxTurns:                 maxTurns,
		// Initialize step token tracking
		stepTokenAccumulator: make(map[string]*StepTokenUsage),
		stepTokenTitles:      make(map[string]string),
		// Initialize model token tracking
		modelTokenAccumulator: make(map[string]*ModelTokenUsageInternal),
	}

	// Set token accumulator on bridge for step token tracking
	contextAwareBridge.SetTokenAccumulator(orchestrator)

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
