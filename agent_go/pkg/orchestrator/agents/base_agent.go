package agents

import (
	"context"
	"fmt"
	loggerv2 "mcpagent/logger/v2"
	"strings"
	"time"

	mcpagent "mcpagent/agent"
	internalLLM "mcpagent/llm"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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
	VariableExtractionAgentType               AgentType = "variable_extraction"                 // Extracts variables from objective
	TodoPlannerAnonymizationAgentType         AgentType = "todo_planner_anonymization"          // Anonymizes learnings by replacing values with variables
	TodoPlannerPlanImprovementAgentType       AgentType = "todo_planner_plan_improvement"       // Analyzes execution and provides plan improvement feedback
	TodoPlannerPlanningAgentType              AgentType = "todo_planner_planning"               // Creates step-wise plan from objective
	TodoPlannerExecutionAgentType             AgentType = "todo_planner_execution"              // Executes first step of plan
	TodoPlannerValidationAgentType            AgentType = "todo_planner_validation"             // Validates execution results
	TodoPlannerPrerequisiteDetectionAgentType AgentType = "todo_planner_prerequisite_detection" // Detects if validation failure is due to missing prerequisites
	TodoPlannerSuccessLearningAgentType       AgentType = "todo_planner_success_learning"       // Analyzes successful executions to capture best practices
	TodoPlannerPlanToolOptimizationAgentType  AgentType = "todo_planner_plan_tool_optimization" // Optimizes tool selections in step_config.json based on learnings
	ConditionalAgentType                      AgentType = "conditional"                         // Conditional decision agent for evaluating step conditions
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
	logger  loggerv2.Logger

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
	logger loggerv2.Logger,
	cacheOnly bool,
	enableLargeOutputVirtualTools *bool, // NEW parameter
) (*BaseAgent, error) {
	// Convert AgentMode to mcpagent.AgentMode
	// All agents use Simple mode
	var mcpMode mcpagent.AgentMode = mcpagent.SimpleAgent

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
		// Removed verbose logging
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

	// Removed verbose logging

	// Use logger directly (already loggerv2.Logger)
	v2Logger := logger

	// Determine server name (join multiple servers with comma, or use first server, or empty string)
	// NewAgentConnection supports comma-separated server names to connect to multiple servers
	serverName := ""
	if len(serverNames) > 0 {
		if len(serverNames) == 1 {
			serverName = serverNames[0]
		} else {
			// Multiple servers: join with comma for NewAgentConnection
			serverName = strings.Join(serverNames, ",")
		}
	}

	// Create agent with all options
	agent, err := mcpagent.NewAgent(ctx, llm, serverName, configPath, modelID, tracer, traceID, v2Logger, agentOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	return &BaseAgent{
		agent:        agent,
		name:         name,
		agentType:    agentType,
		logger:       logger,
		tracer:       tracer,
		traceID:      traceID,
		instructions: instructions,
		mode:         mode,
		serverNames:  serverNames,
		llm:          llm,
		configPath:   configPath,
		modelID:      modelID,
		temperature:  temperature,
		toolChoice:   toolChoice,
		maxTurns:     maxTurns,
		provider:     provider,
	}, nil
}

// Execute executes the agent with user message and conversation history
func (ba *BaseAgent) Execute(ctx context.Context, userMessage string, conversationHistory []llmtypes.MessageContent, systemPrompt string, overwriteSystemPrompt bool) (string, []llmtypes.MessageContent, error) {
	// Removed verbose logging

	// Set or append system prompt if provided
	if systemPrompt != "" {
		if overwriteSystemPrompt {
			ba.agent.SetSystemPrompt(systemPrompt)
		} else {
			ba.agent.AppendSystemPrompt(systemPrompt)
		}
	}

	startTime := time.Now()

	// Prepare messages: always append userMessage to conversation history
	messages := make([]llmtypes.MessageContent, len(conversationHistory))
	copy(messages, conversationHistory)

	// Always append the user message
	userMessageContent := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: userMessage}},
	}
	messages = append(messages, userMessageContent)

	// Execute the agent with orchestrator context and conversation history
	orchestratorCtx := context.WithValue(ctx, orchestratorIDKey, fmt.Sprintf("%s_%s_%d", ba.agentType, ba.name, time.Now().UnixNano()))
	answer, updatedConversationHistory, err := ba.agent.AskWithHistory(orchestratorCtx, messages)

	executionTime := time.Since(startTime)

	if err != nil {
		return "", nil, fmt.Errorf("agent execution failed: %w", err)
	}

	// Removed verbose logging
	_ = executionTime

	return answer, updatedConversationHistory, nil
}

// Agent returns the underlying MCP agent
func (ba *BaseAgent) Agent() *mcpagent.Agent {
	return ba.agent
}

// GetName returns the agent name
func (ba *BaseAgent) GetName() string {
	return ba.name
}

// GetType returns the agent type
func (ba *BaseAgent) GetType() AgentType {
	return ba.agentType
}

// GetInstructions returns the agent instructions
func (ba *BaseAgent) GetInstructions() string {
	return ba.instructions
}

// GetMode returns the agent mode
func (ba *BaseAgent) GetMode() AgentMode {
	return ba.mode
}

// GetServerNames returns the server names
func (ba *BaseAgent) GetServerNames() []string {
	return ba.serverNames
}

// Close closes the agent
func (ba *BaseAgent) Close() error {
	if ba.agent != nil {
		ba.agent.Close()
	}
	return nil
}

// GetWorkflowContext returns the workflow context
func (ba *BaseAgent) GetWorkflowContext() map[string]interface{} {
	return ba.workflowContext
}

// SetWorkflowContext sets the workflow context
func (ba *BaseAgent) SetWorkflowContext(context map[string]interface{}) {
	ba.workflowContext = context
}

// GetPreviousAgentOutput returns the previous agent output
func (ba *BaseAgent) GetPreviousAgentOutput() string {
	return ba.previousAgentOutput
}

// SetPreviousAgentOutput sets the previous agent output
func (ba *BaseAgent) SetPreviousAgentOutput(output string) {
	ba.previousAgentOutput = output
}
