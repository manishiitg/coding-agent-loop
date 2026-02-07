package agents

import (
	"context"
	"fmt"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	internalLLM "github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

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
	TodoPlannerAnonymizationAgentType        AgentType = "todo_planner_anonymization"          // Anonymizes learnings by replacing values with variables
	TodoPlannerPlanImprovementAgentType      AgentType = "todo_planner_plan_improvement"       // Analyzes execution and provides plan improvement feedback
	TodoPlannerEvaluationDebuggerAgentType   AgentType = "todo_planner_evaluation_debugger"    // Analyzes evaluation execution and provides feedback for evaluation plan improvement
	TodoPlannerCodeExecDebuggingAgentType    AgentType = "todo_planner_code_exec_debugging"    // Analyzes execution logs for code execution errors
	TodoPlannerPlanningAgentType             AgentType = "todo_planner_planning"               // Creates step-wise plan from objective
	TodoPlannerExecutionAgentType            AgentType = "todo_planner_execution"              // Executes first step of plan
	TodoPlannerValidationAgentType           AgentType = "todo_planner_validation"             // Validates execution results
	TodoPlannerSuccessLearningAgentType      AgentType = "todo_planner_success_learning"       // Analyzes successful executions to capture best practices
	TodoPlannerLearningDetectionAgentType    AgentType = "todo_planner_learning_detection"     // Detects if new learnings were generated after learning phase
	TodoPlannerPlanToolOptimizationAgentType AgentType = "todo_planner_plan_tool_optimization" // Optimizes tool selections in step_config.json based on learnings
	ConditionalAgentType                     AgentType = "conditional"                         // Conditional decision agent for evaluating step conditions
	OrchestrationAgentType                   AgentType = "orchestration"                       // DEPRECATED: Legacy orchestration agent type (no longer used). Current orchestration uses OrchestrationOrchestratorAgent which handles execution, evaluation, and routing in one step.
	EvaluationScoringAgentType               AgentType = "evaluation_scoring"                  // Calculates scores for evaluation steps based on success criteria
	TodoTaskOrchestratorAgentType            AgentType = "todo_task_orchestrator"              // TodoTask orchestrator agent that manages todo lists and delegates to sub-agents
	GenericExecutionAgentType                AgentType = "generic_execution"                   // Generic execution agent for todo task steps (no learning, no prevalidation)
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
	useToolSearchMode bool, // Enable tool search mode
	preDiscoveredTools []string, // Tools always available without searching
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
	enableContextOffloading *bool, // Context offloading configuration
	largeOutputThreshold int, // Token threshold for context offloading (0 = use default: 10000)
	enableContextSummarization bool, // Context summarization configuration
	summarizeOnTokenThreshold bool, // Enable token-based summarization trigger
	tokenThresholdPercent float64, // Percentage of context window to trigger summarization
	summarizeOnFixedTokenThreshold bool, // Enable fixed token-based summarization trigger
	fixedTokenThreshold int, // Fixed token threshold to trigger summarization
	summaryKeepLastMessages int, // Number of recent messages to keep when summarizing
	enableContextEditing bool, // Context editing configuration
	contextEditingThreshold int, // Token threshold for context editing (0 = use default)
	contextEditingTurnThreshold int, // Turn age threshold for context editing (0 = use default)
	enableParallelToolExecution bool, // Parallel tool execution configuration
	llmConfig *LLMConfig, // NEW: Full LLM configuration
	apiKeys *AgentAPIKeys, // API keys for providers
	mcpSessionID string, // MCP session ID for connection sharing across agents
	runtimeOverrides mcpclient.RuntimeOverrides, // Runtime config overrides for MCP servers (e.g., output directories)
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

	// Add LLM config if provided
	if llmConfig != nil {
		// Convert orchestrator LLMConfig to mcpagent AgentLLMConfiguration
		mcpConfig := mcpagent.AgentLLMConfiguration{
			Primary: mcpagent.LLMModel{
				Provider: llmConfig.Primary.Provider,
				ModelID:  llmConfig.Primary.ModelID,
				APIKey:   llmConfig.Primary.APIKey,
				Region:   llmConfig.Primary.Region,
			},
			Fallbacks: make([]mcpagent.LLMModel, len(llmConfig.Fallbacks)),
		}
		for i, fb := range llmConfig.Fallbacks {
			mcpConfig.Fallbacks[i] = mcpagent.LLMModel{
				Provider: fb.Provider,
				ModelID:  fb.ModelID,
				APIKey:   fb.APIKey,
				Region:   fb.Region,
			}
		}
		agentOptions = append(agentOptions, mcpagent.WithLLMConfig(mcpConfig))
	}

	// Note: API keys are now extracted directly from the LLM instance
	// via extractAPIKeysFromLLM() in mcpagent, so no need to pass them explicitly

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

	// Add tool search mode if enabled
	if useToolSearchMode {
		agentOptions = append(agentOptions, mcpagent.WithToolSearchMode(true))
	}
	if len(preDiscoveredTools) > 0 {
		agentOptions = append(agentOptions, mcpagent.WithPreDiscoveredTools(preDiscoveredTools))
	}

	// Enable smart routing for all agents
	// Smart routing helps filter tools based on relevance to the task
	agentOptions = append(agentOptions,
		mcpagent.WithSmartRouting(true),
		mcpagent.WithSmartRoutingThresholds(20, 4), // 20 tools, 4 servers threshold for all agents
	)

	// Add context offloading option if specified
	// Default to true if nil (backward compatible)
	contextOffloadingEnabled := true
	if enableContextOffloading != nil {
		contextOffloadingEnabled = *enableContextOffloading
	}
	agentOptions = append(agentOptions, mcpagent.WithContextOffloading(contextOffloadingEnabled))

	// Add large output threshold if specified (0 = use default: 10000 tokens)
	if largeOutputThreshold > 0 {
		agentOptions = append(agentOptions, mcpagent.WithLargeOutputThreshold(largeOutputThreshold))
	}

	// Add context summarization configuration
	if enableContextSummarization {
		agentOptions = append(agentOptions, mcpagent.WithContextSummarization(true))
		if summarizeOnTokenThreshold {
			agentOptions = append(agentOptions, mcpagent.WithSummarizeOnTokenThreshold(true, tokenThresholdPercent))
		}
		if summarizeOnFixedTokenThreshold && fixedTokenThreshold > 0 {
			agentOptions = append(agentOptions, mcpagent.WithSummarizeOnFixedTokenThreshold(true, fixedTokenThreshold))
		}
		if summaryKeepLastMessages > 0 {
			agentOptions = append(agentOptions, mcpagent.WithSummaryKeepLastMessages(summaryKeepLastMessages))
		}
	}

	// Add context editing configuration
	if enableContextEditing {
		agentOptions = append(agentOptions, mcpagent.WithContextEditing(true))
		if contextEditingThreshold > 0 {
			agentOptions = append(agentOptions, mcpagent.WithContextEditingThreshold(contextEditingThreshold))
		}
		if contextEditingTurnThreshold > 0 {
			agentOptions = append(agentOptions, mcpagent.WithContextEditingTurnThreshold(contextEditingTurnThreshold))
		}
	}

	// Add parallel tool execution if enabled
	if enableParallelToolExecution {
		agentOptions = append(agentOptions, mcpagent.WithParallelToolExecution(true))
	}

	// Removed verbose logging

	// Use logger directly (already loggerv2.Logger)
	v2Logger := logger

	// Determine server name (join multiple servers with comma, or use first server, or AllServers)
	// NewAgentConnection supports comma-separated server names to connect to multiple servers
	serverName := mcpclient.AllServers
	if len(serverNames) > 0 {
		if len(serverNames) == 1 {
			serverName = serverNames[0]
		} else {
			// Multiple servers: join with comma for NewAgentConnection
			serverName = strings.Join(serverNames, ",")
		}
	}

	// Build options from parameters
	options := agentOptions
	if serverName != "" && serverName != mcpclient.AllServers {
		options = append(options, mcpagent.WithServerName(serverName))
	}
	if tracer != nil {
		options = append(options, mcpagent.WithTracer(tracer))
	}
	if traceID != "" {
		options = append(options, mcpagent.WithTraceID(traceID))
	}
	if v2Logger != nil {
		options = append(options, mcpagent.WithLogger(v2Logger))
	}

	// Add MCP session ID for connection sharing across agents in the same workflow
	// When set, connections are stored in a session registry and reused
	if mcpSessionID != "" {
		options = append(options, mcpagent.WithSessionID(mcpSessionID))
		logger.Info("🔗 Using MCP session for connection sharing",
			loggerv2.String("session_id", mcpSessionID),
			loggerv2.String("agent_name", name))
	}

	// Add runtime overrides for workflow-specific MCP server configuration
	// e.g., setting unique output directories per workflow run
	if runtimeOverrides != nil {
		options = append(options, mcpagent.WithRuntimeOverrides(runtimeOverrides))
		logger.Info("🔧 Using runtime overrides for MCP servers",
			loggerv2.String("agent_name", name),
			loggerv2.Int("overrides_count", len(runtimeOverrides)))
	}

	// Create agent with all options
	// modelID is automatically extracted from llm
	agent, err := mcpagent.NewAgent(ctx, llm, configPath, options...)
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
