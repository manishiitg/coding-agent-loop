package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"llm-providers/llmtypes"
	internalLLM "mcp-agent/agent_go/internal/llm"
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
)

// AgentType represents the type of agent
type AgentType string

const (
	// Multi-agent TodoPlanner sub-agents (actively used)
	VariableExtractionAgentType              AgentType = "variable_extraction"                 // Extracts variables from objective
	TodoPlannerAnonymizationAgentType        AgentType = "todo_planner_anonymization"          // Anonymizes learnings by replacing values with variables
	TodoPlannerPlanImprovementAgentType      AgentType = "todo_planner_plan_improvement"       // Analyzes execution and provides plan improvement feedback
	TodoPlannerPlanningAgentType             AgentType = "todo_planner_planning"               // Creates step-wise plan from objective
	TodoPlannerExecutionAgentType            AgentType = "todo_planner_execution"              // Executes first step of plan
	TodoPlannerValidationAgentType           AgentType = "todo_planner_validation"             // Validates execution results
	TodoPlannerSuccessLearningAgentType      AgentType = "todo_planner_success_learning"       // Analyzes successful executions to capture best practices
	TodoPlannerPlanToolOptimizationAgentType AgentType = "todo_planner_plan_tool_optimization" // Optimizes tool selections in step_config.json based on learnings
)

// BaseAgentInterface defines the interface for base agent operations
type BaseAgentInterface interface {
	// Core execution
	Execute(ctx context.Context, userMessage string, conversationHistory []llmtypes.MessageContent, systemPrompt string, overwriteSystemPrompt bool) (string, []llmtypes.MessageContent, error)

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
	useCodeExecutionMode bool, // NEW parameter
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
	enableLargeOutputVirtualTools *bool, // NEW parameter
) (*BaseAgent, error) {
	// Convert AgentMode to mcpagent.AgentMode
	// All agents use Simple mode
	var mcpMode mcpagent.AgentMode = mcpagent.SimpleAgent

	// Create the underlying MCP agent
	serverNameStr := strings.Join(serverNames, ",")

	// Prepare agent options
	agentOptions := []mcpagent.AgentOption{
		mcpagent.WithMode(mcpMode),
		mcpagent.WithTemperature(temperature),
		mcpagent.WithToolChoice(toolChoice),
		mcpagent.WithMaxTurns(maxTurns),
		mcpagent.WithProvider(internalLLM.Provider(provider)),
	}

	// Add selected servers for "all tools" mode determination
	if len(serverNames) > 0 {
		agentOptions = append(agentOptions, mcpagent.WithSelectedServers(serverNames))
	}

	// Add selected tools if provided
	if len(selectedTools) > 0 {
		agentOptions = append(agentOptions, mcpagent.WithSelectedTools(selectedTools))
	}

	// Add code execution mode if enabled
	if useCodeExecutionMode {
		agentOptions = append(agentOptions, mcpagent.WithCodeExecutionMode(true))
		logger.Infof("🔧 Code execution mode enabled for %s agent - MCP tools will be accessed via generated Go code", agentType)
	}

	// Enable smart routing for all agents
	// Smart routing helps filter tools based on relevance to the task
	agentOptions = append(agentOptions,
		mcpagent.WithSmartRouting(true),
		mcpagent.WithSmartRoutingThresholds(20, 4), // 20 tools, 4 servers threshold for all agents
	)

	// Add large output virtual tools option if specified
	// Default to true if nil (backward compatible)
	largeOutputEnabled := true
	if enableLargeOutputVirtualTools != nil {
		largeOutputEnabled = *enableLargeOutputVirtualTools
	}
	agentOptions = append(agentOptions, mcpagent.WithLargeOutputVirtualTools(largeOutputEnabled))

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
func (ba *BaseAgent) Execute(ctx context.Context, userMessage string, conversationHistory []llmtypes.MessageContent, systemPrompt string, overwriteSystemPrompt bool) (string, []llmtypes.MessageContent, error) {
	ba.logger.Infof("🚀 Executing %s agent: %s", ba.agentType, ba.name)

	// Set or append system prompt if provided
	if systemPrompt != "" {
		if overwriteSystemPrompt {
			ba.agent.SetSystemPrompt(systemPrompt)
			ba.logger.Infof("✅ System prompt overwritten (length: %d chars)", len(systemPrompt))
		} else {
			ba.agent.AppendSystemPrompt(systemPrompt)
			ba.logger.Infof("✅ System prompt appended to agent (length: %d chars)", len(systemPrompt))
		}
	}

	// Event emission now handled by unified events system

	startTime := time.Now()

	// Note: Conversation history is handled by AskWithHistory method
	// The history will be passed directly to AskWithHistory below

	// ✅ HIERARCHY FIX: Add orchestrator_id to context for proper hierarchy detection
	orchestratorCtx := context.WithValue(ctx, orchestratorIDKey, fmt.Sprintf("%s_%s_%d", ba.agentType, ba.name, time.Now().UnixNano()))
	// Added orchestrator_id to context for hierarchy detection

	// Prepare messages: always append userMessage to conversation history
	messages := make([]llmtypes.MessageContent, len(conversationHistory))
	copy(messages, conversationHistory)

	// Always append the user message
	userMessageContent := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: userMessage}},
	}
	messages = append(messages, userMessageContent)
	ba.logger.Infof("📝 Added user message to conversation (total messages: %d)", len(messages))

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
func AskStructuredTyped[T any](ba *BaseAgent, ctx context.Context, question string, schema string, conversationHistory []llmtypes.MessageContent, systemPrompt string, overwriteSystemPrompt bool) (T, []llmtypes.MessageContent, error) {
	// Check if ba is nil
	if ba == nil {
		var zero T
		return zero, nil, fmt.Errorf("BaseAgent is nil - Initialize() must be called before using the agent")
	}

	if ba.agent == nil {
		var zero T
		return zero, nil, fmt.Errorf("underlying agent not initialized")
	}

	// Set or append system prompt if provided
	if systemPrompt != "" {
		if overwriteSystemPrompt {
			ba.agent.SetSystemPrompt(systemPrompt)
			ba.logger.Infof("✅ System prompt overwritten (length: %d chars)", len(systemPrompt))
		} else {
			ba.agent.AppendSystemPrompt(systemPrompt)
			ba.logger.Infof("✅ System prompt appended to agent (length: %d chars)", len(systemPrompt))
		}
	}

	// ✅ HIERARCHY FIX: Add orchestrator_id to context for proper hierarchy detection
	orchestratorCtx := context.WithValue(ctx, orchestratorIDKey, fmt.Sprintf("%s_%s_%d", ba.agentType, ba.name, time.Now().UnixNano()))
	// Added orchestrator_id to context for hierarchy detection

	// Prepare messages: always append question to conversation history
	messages := make([]llmtypes.MessageContent, len(conversationHistory))
	copy(messages, conversationHistory)

	// Always append the question
	userMessage := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: question}},
	}
	messages = append(messages, userMessage)
	ba.logger.Infof("📝 Added question to conversation (total messages: %d)", len(messages))

	// The MCP agent's AskWithHistoryStructured expects: (agent, ctx, messages, schema, schemaString)
	// where schema is the type, not the result variable
	// We create a zero value of type T to pass as the schema parameter
	var schemaType T

	// Call the MCP agent's generic AskWithHistoryStructured function
	// Capture updated conversation history for proper conversation maintenance
	result, updatedHistory, err := mcpagent.AskWithHistoryStructured(ba.agent, orchestratorCtx, messages, schemaType, schema)
	return result, updatedHistory, err
}

// AskStructuredTypedViaTool is similar to AskStructuredTyped but uses tool calls instead of two-phase conversion
func AskStructuredTypedViaTool[T any](ba *BaseAgent, ctx context.Context, question string, schema string, conversationHistory []llmtypes.MessageContent, systemPrompt string, overwriteSystemPrompt bool, toolName string, toolDescription string) (mcpagent.StructuredOutputResult[T], []llmtypes.MessageContent, error) {
	// Check if ba is nil
	if ba == nil {
		var zero mcpagent.StructuredOutputResult[T]
		return zero, nil, fmt.Errorf("BaseAgent is nil - Initialize() must be called before using the agent")
	}

	if ba.agent == nil {
		var zero mcpagent.StructuredOutputResult[T]
		return zero, nil, fmt.Errorf("underlying agent not initialized")
	}

	// Set or append system prompt if provided
	if systemPrompt != "" {
		if overwriteSystemPrompt {
			ba.agent.SetSystemPrompt(systemPrompt)
			ba.logger.Infof("✅ System prompt overwritten (length: %d chars)", len(systemPrompt))
		} else {
			ba.agent.AppendSystemPrompt(systemPrompt)
			ba.logger.Infof("✅ System prompt appended to agent (length: %d chars)", len(systemPrompt))
		}
	}

	// ✅ HIERARCHY FIX: Add orchestrator_id to context for proper hierarchy detection
	orchestratorCtx := context.WithValue(ctx, orchestratorIDKey, fmt.Sprintf("%s_%s_%d", ba.agentType, ba.name, time.Now().UnixNano()))
	// Added orchestrator_id to context for hierarchy detection

	// Prepare messages: always append question to conversation history
	messages := make([]llmtypes.MessageContent, len(conversationHistory))
	copy(messages, conversationHistory)

	// Always append the question
	userMessage := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: question}},
	}
	messages = append(messages, userMessage)
	ba.logger.Infof("📝 Added question to conversation (total messages: %d)", len(messages))

	// Call the MCP agent's AskWithHistoryStructuredViaTool function
	result, err := mcpagent.AskWithHistoryStructuredViaTool[T](ba.agent, orchestratorCtx, messages, toolName, toolDescription, schema)
	return result, result.Messages, err
}
