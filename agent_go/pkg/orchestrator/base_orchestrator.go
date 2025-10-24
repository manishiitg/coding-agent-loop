package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
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
}

// OrchestratorType represents the type of orchestrator
type OrchestratorType string

const (
	OrchestratorTypePlanner  OrchestratorType = "planner"
	OrchestratorTypeWorkflow OrchestratorType = "workflow"
)

// BaseOrchestrator provides unified base functionality for all orchestrators
type BaseOrchestrator struct {
	// Context-aware event bridge for orchestrator-level events
	contextAwareBridge mcpagent.AgentEventListener

	// Logger for the orchestrator
	logger utils.ExtendedLogger

	// Workspace tools for file operations
	WorkspaceTools         []llms.Tool
	WorkspaceToolExecutors map[string]interface{}

	// Orchestrator type and configuration
	orchestratorType OrchestratorType
	startTime        time.Time

	// Common configuration shared between orchestrators
	provider        string
	model           string
	mcpConfigPath   string
	temperature     float64
	agentMode       string
	selectedServers []string
	llmConfig       *LLMConfig // LLM configuration
	maxTurns        int        // Maximum turns for the orchestrator

	// Optional simple state (for workflow orchestrators)
	objective     string
	workspacePath string
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
	llmConfig *LLMConfig,
	maxTurns int,
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
) (*BaseOrchestrator, error) {

	// Create context-aware event bridge that wraps the main event bridge
	contextAwareBridge := NewContextAwareEventBridge(eventBridge, logger)

	return &BaseOrchestrator{
		contextAwareBridge:     contextAwareBridge,
		logger:                 logger,
		WorkspaceTools:         customTools,
		WorkspaceToolExecutors: customToolExecutors,
		orchestratorType:       orchestratorType,
		startTime:              time.Now(),
		// Common configuration
		provider:        provider,
		model:           model,
		mcpConfigPath:   mcpConfigPath,
		temperature:     temperature,
		agentMode:       agentMode,
		selectedServers: selectedServers,
		llmConfig:       llmConfig,
		maxTurns:        maxTurns,
	}, nil
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
		bo.GetLogger().Warnf("⚠️ Failed to emit event %s: %v", eventType, err)
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
		bo.GetLogger().Warnf("⚠️ Failed to emit unified completion event: %v", err)
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

// createAgentConfigWithLLM creates a generic agent configuration with detailed LLM config
func (bo *BaseOrchestrator) createAgentConfigWithLLM(agentName string, maxTurns int, outputFormat agents.OutputFormat, llmConfig *LLMConfig) *agents.OrchestratorAgentConfig {
	config := agents.NewOrchestratorAgentConfig(agentName)

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
	config.CacheOnly = false // Allow fresh connections when cache is not available
	config.ServerNames = bo.GetSelectedServers()
	config.Mode = agents.AgentMode(bo.GetAgentMode())
	config.OutputFormat = outputFormat
	config.MaxRetries = 3
	config.Timeout = 300 // Same timeout for all agents
	config.RateLimit = 60

	// Detailed LLM configuration from frontend
	if llmConfig != nil {
		config.FallbackModels = llmConfig.FallbackModels
		config.CrossProviderFallback = llmConfig.CrossProviderFallback
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
	customTools []llms.Tool,
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
		bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(customTools), agentName, baseAgent.GetMode())

		for _, tool := range customTools {
			if executor, exists := customToolExecutors[tool.Function.Name]; exists {
				// Type assert parameters to map[string]interface{}
				params, ok := tool.Function.Parameters.(map[string]interface{})
				if !ok {
					bo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				// Type assert executor to function type
				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
					)
				} else {
					bo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		bo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())
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
	customTools []llms.Tool,
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
		bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(customTools), agentName, baseAgent.GetMode())

		for _, tool := range customTools {
			if executor, exists := customToolExecutors[tool.Function.Name]; exists {
				// Type assert parameters to map[string]interface{}
				params, ok := tool.Function.Parameters.(map[string]interface{})
				if !ok {
					bo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				// Type assert executor to function type
				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
					)
				} else {
					bo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		bo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())
	}

	return agent, nil
}

// SetupStandardAgent removed: setup is now performed inline in CreateAndSetupStandardAgent

// setupAgent removed: logic is now inlined in CreateAndSetupStandardAgent

// ReadWorkspaceFile reads a file from the workspace and returns its content
// Emits tool call events for proper observability
func (bo *BaseOrchestrator) ReadWorkspaceFile(ctx context.Context, filePath string) (string, error) {
	bo.GetLogger().Infof("📖 Reading workspace file: %s", filePath)

	// Prepare tool call parameters
	readArgs := map[string]interface{}{
		"filepath": filePath,
	}

	// Convert args to JSON string for event
	argsJSON, _ := json.Marshal(readArgs)

	// Emit tool call start event
	toolCallStartEvent := &events.ToolCallStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:     0, // Orchestrator-level call
		ToolName: "read_workspace_file",
		ToolParams: events.ToolParams{
			Arguments: string(argsJSON),
		},
		ServerName: "workspace", // Internal workspace tool
	}

	bo.emitEvent(ctx, events.ToolCallStart, toolCallStartEvent)

	// Get the tool executor
	readExecutorInterface, exists := bo.WorkspaceToolExecutors["read_workspace_file"]
	if !exists {
		// Emit tool call error event
		toolCallErrorEvent := &events.ToolCallErrorEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Turn:       0,
			ToolName:   "read_workspace_file",
			Error:      "read_workspace_file tool executor not found",
			ServerName: "workspace",
			Duration:   0,
		}
		bo.emitEvent(ctx, events.ToolCallError, toolCallErrorEvent)
		return "", fmt.Errorf("read_workspace_file tool executor not found")
	}

	readExecutor, ok := readExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		// Emit tool call error event
		toolCallErrorEvent := &events.ToolCallErrorEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Turn:       0,
			ToolName:   "read_workspace_file",
			Error:      "read_workspace_file tool executor has wrong type",
			ServerName: "workspace",
			Duration:   0,
		}
		bo.emitEvent(ctx, events.ToolCallError, toolCallErrorEvent)
		return "", fmt.Errorf("read_workspace_file tool executor has wrong type")
	}

	// Execute the tool call using existing workspace tool logic
	startTime := time.Now()
	readResult, err := readExecutor(ctx, readArgs)
	duration := time.Since(startTime)

	if err != nil {
		// Emit tool call error event for read failure
		toolCallErrorEvent := &events.ToolCallErrorEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Turn:       0,
			ToolName:   "read_workspace_file",
			Error:      fmt.Sprintf("Failed to read file: %v", err),
			ServerName: "workspace",
			Duration:   duration,
		}
		bo.emitEvent(ctx, events.ToolCallError, toolCallErrorEvent)
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Parse the response - handleReadWorkspaceFile returns only the Data field from API response
	var fileData struct {
		Filepath string `json:"filepath"`
		Content  string `json:"content"`
	}

	if err := json.Unmarshal([]byte(readResult), &fileData); err != nil {
		// Emit tool call error event for parsing failure
		toolCallErrorEvent := &events.ToolCallErrorEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Turn:       0,
			ToolName:   "read_workspace_file",
			Error:      fmt.Sprintf("Failed to parse workspace response: %v", err),
			ServerName: "workspace",
			Duration:   duration,
		}
		bo.emitEvent(ctx, events.ToolCallError, toolCallErrorEvent)
		return "", fmt.Errorf("failed to parse workspace response: %w", err)
	}

	// Extract content directly from the parsed data
	fileContent := fileData.Content

	if fileContent == "" {
		// Emit tool call error event for missing content
		toolCallErrorEvent := &events.ToolCallErrorEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Turn:       0,
			ToolName:   "read_workspace_file",
			Error:      "No content found in workspace response",
			ServerName: "workspace",
			Duration:   duration,
		}
		bo.emitEvent(ctx, events.ToolCallError, toolCallErrorEvent)
		return "", fmt.Errorf("no content found in workspace response")
	}

	// Emit successful tool call end event
	toolCallEndEvent := &events.ToolCallEndEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:       0,
		ToolName:   "read_workspace_file",
		Result:     fmt.Sprintf("Successfully read file (%d characters)", len(fileContent)),
		Duration:   duration,
		ServerName: "workspace",
	}
	bo.emitEvent(ctx, events.ToolCallEnd, toolCallEndEvent)

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
	if err := bo.GetContextAwareBridge().HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to emit human feedback event: %v", err)
	}

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
