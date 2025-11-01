package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	internalLLM "mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const orchestratorIDKey contextKey = "orchestrator_id"

// AgentMode represents the mode of operation for an agent
type AgentMode string

const (
	SimpleAgent AgentMode = "simple"
	ReActAgent  AgentMode = "react"
)

// AgentType represents the type of agent
type AgentType string

const (
	PlanningAgentType          AgentType = "planning"
	ExecutionAgentType         AgentType = "execution"
	ParallelExecutionAgentType AgentType = "parallel_execution" // Executes specific step in parallel
	ValidationAgentType        AgentType = "validation"
	PlanOrganizerAgentType     AgentType = "plan_organizer"
	PlanBreakdownAgentType     AgentType = "plan_breakdown" // Analyzes dependencies and creates independent steps
	PlanReaderAgentType        AgentType = "plan_reader"    // Reads plan markdown and returns structured JSON (read-only)

	// Orchestrator types
	PlannerOrchestratorAgentType  AgentType = "planner_orchestrator"  // AI-controlled planner orchestrator
	WorkflowOrchestratorAgentType AgentType = "workflow_orchestrator" // AI-controlled workflow orchestrator

	// 🆕 NEW: Workflow-specific types
	TodoPlannerAgentType       AgentType = "todo_planner"        // Creates todo list once
	TodoExecutionAgentType     AgentType = "todo_execution"      // Executes one todo at a time
	TodoValidationAgentType    AgentType = "todo_validation"     // Validates todo completion
	WorkspaceUpdateAgentType   AgentType = "workspace_update"    // Updates Tasks/ folder
	TodoRefinePlannerAgentType AgentType = "todo_refine_planner" // Refines todo list based on execution history
	DataCritiqueAgentType      AgentType = "data_critique"       // Critiques any input data for factual accuracy and analytical quality
	ReportGenerationAgentType  AgentType = "report_generation"   // Generates comprehensive reports from workflow execution
	TodoOptimizationAgentType  AgentType = "todo_optimization"   // Orchestrates optimization processes (refinement, critique, reports)

	// 🆕 NEW: Multi-agent TodoPlanner sub-agents
	VariableExtractionAgentType         AgentType = "variable_extraction"           // Extracts variables from objective
	TodoPlannerPlanningAgentType        AgentType = "todo_planner_planning"         // Creates step-wise plan from objective
	TodoPlannerExecutionAgentType       AgentType = "todo_planner_execution"        // Executes first step of plan
	TodoPlannerValidationAgentType      AgentType = "todo_planner_validation"       // Validates execution results
	TodoPlannerWriterAgentType          AgentType = "todo_planner_writer"           // Creates optimal todo list
	TodoPlannerCleanupAgentType         AgentType = "todo_planner_cleanup"          // Manages workspace cleanup
	TodoPlannerCritiqueAgentType        AgentType = "todo_planner_critique"         // Critiques execution/validation data for planning
	TodoPlannerSuccessLearningAgentType AgentType = "todo_planner_success_learning" // Analyzes successful executions to capture best practices
	ConditionalLLMAgentType             AgentType = "conditional_llm"               // Makes conditional decisions
)

// BaseAgentInterface defines the interface for base agent operations
type BaseAgentInterface interface {
	// Core execution
	Execute(ctx context.Context, userMessage string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error)

	// Agent information
	GetType() AgentType
	GetName() string
	GetInstructions() string
	GetMode() AgentMode
	GetServerNames() []string

	// Resource management
	Close() error

	// Event system - now handled by unified events system

	// Workflow support
	GetWorkflowContext() map[string]interface{}
	SetWorkflowContext(context map[string]interface{})
	GetPreviousAgentOutput() string
	SetPreviousAgentOutput(output string)

	// MCP agent access
	Agent() *mcpagent.Agent
}

// BaseAgent provides comprehensive functionality for all orchestrator agents
type BaseAgent struct {
	// Core identity
	agentType AgentType
	name      string

	// Core functionality
	agent        *mcpagent.Agent
	instructions string
	mode         AgentMode
	serverNames  []string
	llm          llmtypes.Model

	// Observability
	tracer  observability.Tracer
	traceID observability.TraceID
	logger  utils.ExtendedLogger

	// Event system - now handled by unified events system

	// Workflow context
	workflowContext     map[string]interface{}
	previousAgentOutput string

	// Configuration
	configPath  string
	modelID     string
	temperature float64
	toolChoice  string
	maxTurns    int
	provider    string
}

// NewBaseAgent creates a new BaseAgent instance with comprehensive functionality
func NewBaseAgent(
	ctx context.Context,
	agentType AgentType,
	name string,
	llm llmtypes.Model,
	instructions string,
	serverNames []string,
	selectedTools []string, // NEW parameter
	mode AgentMode,
	tracer observability.Tracer,
	traceID observability.TraceID,
	configPath string,
	modelID string,
	temperature float64,
	toolChoice string,
	maxTurns int,
	provider string,
	logger utils.ExtendedLogger,
	cacheOnly bool,
) (*BaseAgent, error) {
	// Convert AgentMode to mcpagent.AgentMode
	var mcpMode mcpagent.AgentMode
	switch mode {
	case SimpleAgent:
		mcpMode = mcpagent.SimpleAgent
	case ReActAgent:
		mcpMode = mcpagent.ReActAgent
	default:
		mcpMode = mcpagent.SimpleAgent
	}

	// Create the underlying MCP agent
	serverNameStr := strings.Join(serverNames, ",")

	// Prepare agent options
	agentOptions := []mcpagent.AgentOption{
		mcpagent.WithMode(mcpMode),
		mcpagent.WithTemperature(temperature),
		mcpagent.WithToolChoice(toolChoice),
		mcpagent.WithMaxTurns(maxTurns),
		mcpagent.WithProvider(internalLLM.Provider(provider)),
		mcpagent.WithCacheOnly(cacheOnly),
	}

	// Add selected servers for "all tools" mode determination
	if len(serverNames) > 0 {
		agentOptions = append(agentOptions, mcpagent.WithSelectedServers(serverNames))
	}

	// Add selected tools if provided
	if len(selectedTools) > 0 {
		agentOptions = append(agentOptions, mcpagent.WithSelectedTools(selectedTools))
	}

	// Enable smart routing for all agents
	// Smart routing helps filter tools based on relevance to the task
	agentOptions = append(agentOptions,
		mcpagent.WithSmartRouting(true),
		mcpagent.WithSmartRoutingThresholds(20, 4), // 20 tools, 4 servers threshold for all agents
	)
	logger.Infof("🎯 Smart routing enabled for %s agent - MaxTools: 20, MaxServers: 4", agentType)

	agent, err := mcpagent.NewAgent(
		ctx,
		llm,
		serverNameStr,
		configPath,
		modelID,
		tracer,
		traceID,
		logger,
		agentOptions...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP agent: %w", err)
	}

	baseAgent := &BaseAgent{
		agentType:       agentType,
		name:            name,
		agent:           agent,
		instructions:    instructions,
		mode:            mode,
		serverNames:     serverNames,
		llm:             llm,
		tracer:          tracer,
		traceID:         traceID,
		logger:          logger,
		workflowContext: make(map[string]interface{}),
		configPath:      configPath,
		modelID:         modelID,
		temperature:     temperature,
		toolChoice:      toolChoice,
		maxTurns:        maxTurns,
		provider:        provider,
	}

	return baseAgent, nil
}

// Execute executes the agent with user message and conversation history
func (ba *BaseAgent) Execute(ctx context.Context, userMessage string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	ba.logger.Infof("🚀 Executing %s agent: %s", ba.agentType, ba.name)

	// Event emission now handled by unified events system

	startTime := time.Now()

	// Note: Conversation history is handled by AskWithHistory method
	// The history will be passed directly to AskWithHistory below

	// ✅ HIERARCHY FIX: Add orchestrator_id to context for proper hierarchy detection
	orchestratorCtx := context.WithValue(ctx, orchestratorIDKey, fmt.Sprintf("%s_%s_%d", ba.agentType, ba.name, time.Now().UnixNano()))
	// Added orchestrator_id to context for hierarchy detection

	// Prepare messages: add userMessage (instructions) ONLY on first turn
	// On subsequent turns, conversationHistory already contains the full conversation context
	var messages []llmtypes.MessageContent

	// Copy existing conversation history if present
	if len(conversationHistory) > 0 {
		// Continuing conversation - use history as-is, don't add instructions again
		// IMPORTANT: Do NOT append userMessage here because:
		// 1. Instructions are already in history from iteration 1
		// 2. Adding instructions again would create duplicate instructions
		// 3. Human feedback needs to be the last message, not instructions
		messages = make([]llmtypes.MessageContent, len(conversationHistory))
		copy(messages, conversationHistory)
		ba.logger.Infof("📝 Continuing existing conversation with %d messages (instructions already in history)", len(conversationHistory))
	} else {
		// First turn - add instructions as initial user message
		// This is the ONLY place we add instructions to the conversation
		ba.logger.Infof("📝 Starting new conversation with template message")
		userMessageContent := llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: userMessage}},
		}
		messages = append(messages, userMessageContent)
	}

	// Execute the agent with orchestrator context and conversation history
	answer, updatedConversationHistory, err := ba.agent.AskWithHistory(orchestratorCtx, messages)

	executionTime := time.Since(startTime)

	if err != nil {
		// Event emission now handled by unified events system

		return "", nil, fmt.Errorf("agent execution failed: %w", err)
	}

	// Event emission now handled by unified events system

	ba.logger.Infof("✅ %s agent execution completed: %s (duration: %s)", ba.agentType, ba.name, executionTime)
	return answer, updatedConversationHistory, nil
}

// GetType returns the agent type
func (ba *BaseAgent) GetType() AgentType {
	return ba.agentType
}

// GetName returns the agent name
func (ba *BaseAgent) GetName() string {
	return ba.name
}

// GetServerNames returns the list of server names this agent can access
func (ba *BaseAgent) GetServerNames() []string {
	return ba.serverNames
}

// Agent returns the underlying MCP agent for direct access
func (ba *BaseAgent) Agent() *mcpagent.Agent {
	return ba.agent
}

// AskStructured runs a single-question interaction and converts the result to structured output
// This provides a convenient way for orchestrator agents to get structured responses
func (ba *BaseAgent) AskStructured(ctx context.Context, question string, result interface{}, schema string) error {
	if ba.agent == nil {
		return fmt.Errorf("underlying agent not initialized")
	}

	// ✅ HIERARCHY FIX: Add orchestrator_id to context for proper hierarchy detection
	orchestratorCtx := context.WithValue(ctx, orchestratorIDKey, fmt.Sprintf("%s_%s_%d", ba.agentType, ba.name, time.Now().UnixNano()))
	// Added orchestrator_id to context for hierarchy detection

	// Use the underlying MCP agent's AskStructured method
	// The MCP agent's AskStructured expects: (agent, ctx, question, schema, schemaString)
	// where schema is the type, not the result variable
	// We pass the result variable directly and let Go's type system handle it
	_, err := mcpagent.AskStructured(ba.agent, orchestratorCtx, question, result, schema)
	return err
}

// Ask runs a single-question interaction and returns the raw text response
func (ba *BaseAgent) Ask(ctx context.Context, question string) (string, error) {
	if ba.agent == nil {
		return "", fmt.Errorf("underlying agent not initialized")
	}

	// ✅ HIERARCHY FIX: Add orchestrator_id to context for proper hierarchy detection
	orchestratorCtx := context.WithValue(ctx, orchestratorIDKey, fmt.Sprintf("%s_%s_%d", ba.agentType, ba.name, time.Now().UnixNano()))
	// Added orchestrator_id to context for hierarchy detection

	return ba.agent.Ask(orchestratorCtx, question)
}

// GetInstructions returns the agent's instructions
func (ba *BaseAgent) GetInstructions() string {
	return ba.instructions
}

// GetMode returns the agent's mode
func (ba *BaseAgent) GetMode() AgentMode {
	return ba.mode
}

// Close closes the underlying agent and cleans up resources
func (ba *BaseAgent) Close() error {
	if ba.agent != nil {

		ba.agent.Close()
	}
	return nil
}

// Event system - now handled by unified events system

// Old event emission methods removed - now handled by unified events system

// GetWorkflowContext returns the current workflow context
func (ba *BaseAgent) GetWorkflowContext() map[string]interface{} {
	return ba.workflowContext
}

// SetWorkflowContext sets the workflow context
func (ba *BaseAgent) SetWorkflowContext(context map[string]interface{}) {
	ba.workflowContext = context
}

// GetPreviousAgentOutput returns the output from the previous agent
func (ba *BaseAgent) GetPreviousAgentOutput() string {
	return ba.previousAgentOutput
}

// SetPreviousAgentOutput sets the output from the previous agent
func (ba *BaseAgent) SetPreviousAgentOutput(output string) {
	ba.previousAgentOutput = output
}

// ValidateConfiguration validates the agent configuration
func (ba *BaseAgent) ValidateConfiguration() error {
	if ba.name == "" {
		return fmt.Errorf("agent name cannot be empty")
	}
	if len(ba.serverNames) == 0 {
		return fmt.Errorf("agent must have at least one server assigned")
	}
	if ba.llm == nil {
		return fmt.Errorf("agent must have a valid LLM instance")
	}
	return nil
}

// GetConfigurationSummary returns a summary of the agent configuration
func (ba *BaseAgent) GetConfigurationSummary() map[string]interface{} {
	return map[string]interface{}{
		"agent_type":  string(ba.agentType),
		"agent_name":  ba.name,
		"mode":        string(ba.mode),
		"servers":     ba.serverNames,
		"provider":    ba.provider,
		"model":       ba.modelID,
		"temperature": ba.temperature,
		"max_turns":   ba.maxTurns,
		"tool_choice": ba.toolChoice,
		"config_path": ba.configPath,
		"trace_id":    string(ba.traceID),
	}
}

// AskStructuredTyped is a standalone generic function that provides type-safe structured output
// This gives us the clean generic API without needing to modify the BaseAgent struct
func AskStructuredTyped[T any](ba *BaseAgent, ctx context.Context, question string, schema string, conversationHistory []llmtypes.MessageContent) (T, error) {
	// Check if ba is nil
	if ba == nil {
		var zero T
		return zero, fmt.Errorf("BaseAgent is nil - Initialize() must be called before using the agent")
	}

	if ba.agent == nil {
		var zero T
		return zero, fmt.Errorf("underlying agent not initialized")
	}

	// ✅ HIERARCHY FIX: Add orchestrator_id to context for proper hierarchy detection
	orchestratorCtx := context.WithValue(ctx, orchestratorIDKey, fmt.Sprintf("%s_%s_%d", ba.agentType, ba.name, time.Now().UnixNano()))
	// Added orchestrator_id to context for hierarchy detection

	// Prepare messages: add question ONLY on first turn (when history is empty)
	var messages []llmtypes.MessageContent

	if len(conversationHistory) > 0 {
		// Continuing conversation - use history as-is, don't add question again
		// IMPORTANT: Do NOT append question here - it would create duplicate messages
		// Instructions are already in history from iteration 1
		messages = make([]llmtypes.MessageContent, len(conversationHistory))
		copy(messages, conversationHistory)
	} else {
		// First turn - add question as initial user message
		userMessage := llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: question}},
		}
		messages = append(messages, userMessage)
	}

	// The MCP agent's AskWithHistoryStructured expects: (agent, ctx, messages, schema, schemaString)
	// where schema is the type, not the result variable
	// We create a zero value of type T to pass as the schema parameter
	var schemaType T

	// Call the MCP agent's generic AskWithHistoryStructured function
	result, _, err := mcpagent.AskWithHistoryStructured(ba.agent, orchestratorCtx, messages, schemaType, schema)
	return result, err
}
