package external

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/logger"
	"mcp-agent/agent_go/pkg/mcpagent"

	"sync"
)

// AgentMode defines the type of agent behavior and reasoning approach.
//
// The agent mode determines how the agent processes user queries and generates responses.
// Different modes use different reasoning patterns and conversation end detection strategies.
type AgentMode string

const (
	// SimpleAgent is the standard tool-using agent without explicit reasoning.
	//
	// This mode provides direct tool usage without step-by-step thinking processes.
	// It's faster and more efficient for straightforward queries that don't require
	// complex reasoning chains.
	SimpleAgent AgentMode = "simple"

	// ReActAgent is the reasoning and acting agent with explicit thought processes.
	//
	// This mode uses the ReAct (Reasoning + Acting) pattern to break down complex
	// problems into logical steps. It provides detailed reasoning for each action
	// and is better suited for complex, multi-step queries.
	ReActAgent AgentMode = "react"
)

// PerformanceMetrics represents comprehensive agent performance data and statistics.
//
// This struct tracks various metrics about agent usage including request counts,
// success/failure rates, latency measurements, token usage, and timing information.
// It's useful for monitoring agent health, performance optimization, and usage analytics.
type PerformanceMetrics struct {
	TotalRequests      int64         `json:"total_requests"`
	SuccessfulRequests int64         `json:"successful_requests"`
	FailedRequests     int64         `json:"failed_requests"`
	AverageLatency     time.Duration `json:"average_latency"`
	TotalTokens        int64         `json:"total_tokens"`
	LastRequestTime    time.Time     `json:"last_request_time"`
}

// AgentCore provides the core functionality for agent invocation and conversation management.
//
// This interface defines the fundamental operations that all agents must support:
// sending prompts, processing responses, and managing conversation history.
// It's the primary interface for basic agent interactions.
type AgentCore interface {
	// Invoke sends a prompt to the agent and returns the complete response.
	//
	// This method processes a single user prompt and returns the agent's response.
	// The context can be used for cancellation, timeouts, and request tracing.
	// The response includes any tool calls, reasoning, and final answers.
	//
	// Parameters:
	//   - ctx: Context for cancellation, timeouts, and tracing
	//   - prompt: The user's question or instruction
	//
	// Returns:
	//   - The complete agent response as a string
	//   - Any error that occurred during processing
	Invoke(ctx context.Context, prompt string) (string, error)

	// InvokeWithHistory sends a conversation with history and returns the answer.
	//
	// This method processes a conversation that includes previous message history.
	// It's useful for maintaining context across multiple interactions and
	// building upon previous conversations.
	//
	// Parameters:
	//   - ctx: Context for cancellation, timeouts, and tracing
	//   - messages: Array of message content including conversation history
	//
	// Returns:
	//   - The agent's response to the current conversation
	//   - Updated message history that can be used for subsequent calls
	//   - Any error that occurred during processing
	InvokeWithHistory(ctx context.Context, messages []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error)
}

// AgentConfig provides configuration management and customization capabilities.
//
// This interface allows runtime modification of agent behavior including
// custom instructions, system prompts, and configuration updates.
// It's useful for adapting agent behavior without recreating the agent instance.
type AgentConfig interface {
	// SetCustomInstructions sets custom instructions for the agent.
	//
	// This method allows runtime modification of the agent's behavior by
	// adding custom instructions to the system prompt. The instructions
	// are appended to the existing system prompt and take effect immediately.
	//
	// Parameters:
	//   - instructions: Custom instructions to add to the agent's system prompt
	//
	// Example:
	//   agent.SetCustomInstructions("Always provide step-by-step explanations")
	SetCustomInstructions(instructions string)

	// GetCustomInstructions returns the current custom instructions.
	//
	// This method retrieves the custom instructions that have been set
	// for the agent. It's useful for checking what customizations
	// are currently active.
	//
	// Returns:
	//   - The current custom instructions string, or empty string if none set
	GetCustomInstructions() string

	// GetConfig returns the current agent configuration.
	//
	// This method provides access to the complete agent configuration
	// including provider settings, model parameters, and system prompt
	// configuration. It's useful for inspection and debugging.
	//
	// Returns:
	//   - The complete agent configuration struct
	GetConfig() Config

	// UpdateConfig updates the agent configuration.
	//
	// This method allows runtime modification of the agent's configuration.
	// Note: Some configuration changes may require agent reinitialization
	// to take full effect.
	//
	// Parameters:
	//   - config: The new configuration to apply
	//
	// Returns:
	//   - Any error that occurred during configuration update
	UpdateConfig(config Config) error
}

// AgentLifecycle provides lifecycle management and resource control.
//
// This interface handles agent initialization, cleanup, and context management.
// It ensures proper resource allocation and cleanup for long-running agents.
type AgentLifecycle interface {
	// Initialize prepares the agent for use (e.g., connects to MCP servers).
	//
	// This method sets up the agent's internal state, establishes connections
	// to MCP servers, and prepares it for processing requests. It should be
	// idempotent (safe to call multiple times).
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//
	// Returns:
	//   - Any error that occurred during initialization
	Initialize(ctx context.Context) error

	// Close releases resources (e.g., closes connections).
	//
	// This method performs cleanup operations including closing MCP server
	// connections and releasing any allocated resources. It should be safe
	// to call on an uninitialized agent.
	//
	// Returns:
	//   - Any error that occurred during cleanup
	Close() error

	// GetContext returns the agent's context for cancellation and lifecycle management.
	//
	// This method provides access to the agent's internal context, which can be
	// used for cancellation, timeout control, and tracing. It's useful for
	// integrating the agent with external cancellation mechanisms.
	//
	// Returns:
	//   - The agent's internal context
	GetContext() context.Context
}

// AgentMonitoring provides monitoring, health checks, and operational insights.
//
// This interface offers comprehensive monitoring capabilities including connection
// health, performance statistics, and internal logging access. It's essential
// for production deployments and debugging.
type AgentMonitoring interface {
	// CheckHealth performs health checks on all MCP connections.
	//
	// This method verifies the health and connectivity of all connected MCP servers.
	// It returns a map where keys are server names and values are any errors
	// encountered during the health check.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//
	// Returns:
	//   - Map of server names to error status (nil error means healthy)
	CheckHealth(ctx context.Context) map[string]error

	// GetStats returns statistics about all MCP connections.
	//
	// This method provides detailed operational statistics including connection
	// counts, response times, error rates, and other performance metrics.
	// It's useful for monitoring and debugging agent performance.
	//
	// Returns:
	//   - Map containing various connection and performance statistics
	GetStats() map[string]interface{}

	// GetServerNames returns the names of all connected servers.
	//
	// This method provides a list of all MCP server names that are currently
	// connected and available to the agent. It's useful for understanding
	// the agent's capabilities and available tools.
	//
	// Returns:
	//   - Slice of connected server names
	GetServerNames() []string

	// GetInternalLogger returns the underlying MCP agent's logger for debugging and monitoring.
	//
	// This method provides access to the internal MCP agent's logger, giving you
	// visibility into ALL internal operations including:
	//   - Tool discovery and calls
	//   - MCP server connections and health
	//   - LLM generation and token usage
	//   - Event emission and tracing
	//   - Error handling and debugging
	//
	// This is particularly useful for production debugging and detailed monitoring
	// of agent behavior.
	//
	// Returns:
	//   - The internal MCP agent's logger instance
	GetInternalLogger() utils.ExtendedLogger
}

// AgentCapabilities provides capability discovery and tool information.
//
// This interface offers methods to discover and understand what the agent
// can do, including available tools, connected servers, and general capabilities.
// It's useful for building user interfaces and understanding agent scope.
type AgentCapabilities interface {
	// GetCapabilities returns a human-readable summary of available tools/capabilities.
	//
	// This method provides a comprehensive overview of what the agent can do,
	// including its mode, provider, model, and general capabilities. It's
	// useful for user interfaces and understanding agent scope.
	//
	// Returns:
	//   - Human-readable string describing agent capabilities
	GetCapabilities() string

	// GetServerNames returns the list of connected server names
	GetServerNames() []string

	// GetToolNames returns the list of available tool names.
	//
	// This method provides a list of all tool names that the agent can use.
	// It's useful for building tool selection interfaces and understanding
	// the agent's available capabilities.
	//
	// Returns:
	//   - Slice of available tool names
	GetToolNames() []string

	// RegisterCustomTool registers a custom tool with both schema and execution function
	// This maintains proper encapsulation while allowing custom tool registration
	RegisterCustomTool(name string, description string, parameters map[string]interface{}, executionFunc func(ctx context.Context, args map[string]interface{}) (string, error))
}

// AgentEventListener defines the interface for event listeners that can receive agent events.
//
// This interface allows external systems to subscribe to agent events for monitoring,
// logging, analytics, or integration purposes. Events include tool calls, LLM operations,
// conversation state changes, and more.
type AgentEventListener interface {
	// HandleEvent processes an agent event when it occurs.
	//
	// This method is called by the agent whenever an event is emitted.
	// The event contains typed data specific to the event type, allowing
	// listeners to process different event types appropriately.
	//
	// Parameters:
	//   - ctx: Context for the event handling operation
	//   - event: The agent event with typed data
	//
	// Returns:
	//   - Any error that occurred during event processing
	HandleEvent(ctx context.Context, event *AgentEvent) error

	// Name returns a unique identifier for this event listener.
	//
	// This method provides a human-readable name for the listener, which is
	// useful for debugging, logging, and listener management.
	//
	// Returns:
	//   - A unique name identifying this listener
	Name() string
}

// AgentEvent represents an agent event with typed data and metadata.
//
// This struct encapsulates all information about an agent event including
// the event type, associated data, and timestamp. The data field contains
// typed event information specific to the event type.
type AgentEvent struct {
	Type          AgentEventType
	Data          events.EventData // Use unified events directly
	Timestamp     time.Time
	TraceID       string // For hierarchical tracing
	ParentID      string // For hierarchical tracing
	CorrelationID string // For linking start/end events
}

// GetTypedData returns the event data as unified events EventData.
//
// This method provides access to the typed event data, which contains
// specific information based on the event type. The returned data
// can be type-asserted to access event-specific fields.
//
// Returns:
//   - The typed event data for this event
func (e *AgentEvent) GetTypedData() events.EventData {
	return e.Data
}

// GetTraceID returns the trace ID for hierarchical tracing
func (e *AgentEvent) GetTraceID() string {
	return e.TraceID
}

// GetParentID returns the parent ID for hierarchical tracing
func (e *AgentEvent) GetParentID() string {
	return e.ParentID
}

// GetCorrelationID returns the correlation ID for linking start/end events
func (e *AgentEvent) GetCorrelationID() string {
	return e.CorrelationID
}

// AgentEventType represents the type of agent event
// We now use the unified events package EventType
type AgentEventType = events.EventType

// Note: All event constants are now imported directly from the MCP agent
// No need to redefine them here since the MCP agent already provides
// all the event types we need.

// AgentEvents provides event system management and event emission capabilities.
//
// This interface allows external systems to subscribe to agent events and
// provides methods for emitting custom events. It's the primary mechanism
// for monitoring agent behavior and integrating with external systems.
type AgentEvents interface {
	// AddEventListener adds an event listener for agent events.
	//
	// This method registers an event listener that will receive all agent events.
	// The listener is called for every event emitted by the agent, allowing
	// external systems to monitor agent behavior in real-time.
	//
	// Parameters:
	//   - listener: The event listener to register
	//
	// Note: The same listener can be added multiple times, but this is not recommended.
	AddEventListener(listener AgentEventListener)

	// RemoveEventListener removes an event listener.
	//
	// This method unregisters an event listener, stopping it from receiving
	// future agent events. It's important to remove listeners when they're
	// no longer needed to prevent memory leaks.
	//
	// Parameters:
	//   - listener: The event listener to unregister
	//
	// Note: If the listener was added multiple times, only one instance is removed.
	RemoveEventListener(listener AgentEventListener)

	// EmitEvent emits a typed event to all listeners.
	//
	// This method allows external systems to emit custom events that will be
	// received by all registered event listeners. It's useful for custom
	// event types and external integrations.
	//
	// Parameters:
	//   - ctx: Context for the event emission
	//   - eventType: The type of event to emit
	//   - data: The typed data associated with the event
	EmitEvent(ctx context.Context, eventType AgentEventType, data events.EventData)

	// EmitTypedEvent emits a typed event to all listeners.
	//
	// This method is similar to EmitEvent but takes the event data directly.
	// It's useful when you already have a TypedEventData instance and want
	// to emit it without specifying the event type separately.
	//
	// Parameters:
	//   - ctx: Context for the event emission
	//   - eventData: The typed event data to emit
	EmitTypedEvent(ctx context.Context, eventData events.EventData)
}

// Agent is the composed interface that embeds all agent capabilities.
//
// This interface combines all the individual agent interfaces into a single
// comprehensive interface. Concrete implementations should satisfy this interface
// to provide full agent functionality. Parent systems can type-assert to specific
// interfaces if they only need certain capabilities.
//
// The interface includes:
//   - Core functionality (Invoke, InvokeWithHistory)
//   - Configuration management (custom instructions, config updates)
//   - Lifecycle management (initialization, cleanup)
//   - Monitoring and health checks
//   - Capability discovery
//   - Event system management
type Agent interface {
	AgentCore
	AgentConfig
	AgentLifecycle
	AgentMonitoring
	AgentCapabilities
	AgentEvents
}

// agentImpl is the concrete implementation of the Agent interface
type agentImpl struct {
	agent              *mcpagent.Agent
	customInstructions string
	config             Config
	listeners          []AgentEventListener
	mu                 *sync.RWMutex
	logger             utils.ExtendedLogger // Added logger field

	// 🆕 NEW: Streaming tracer management
	streamingSubscription func() // Function to unsubscribe from streaming
	streamingMu           sync.RWMutex

	// 🆕 NEW: Trace management for proper cleanup
	traceID observability.TraceID // Store the trace ID for cleanup
	tracer  observability.Tracer  // Store the tracer for cleanup
}

// NewAgent creates a new agent with the given configuration.
//
// This function is the primary constructor for creating new agent instances.
// It initializes the underlying MCP agent, sets up connections, configures
// the LLM provider, and prepares the agent for use.
//
// Parameters:
//   - ctx: Context for initialization and cancellation
//   - config: Complete agent configuration including provider, model, and settings
//
// Returns:
//   - A fully configured Agent instance ready for use
//   - Any error that occurred during initialization
//
// The function automatically:
//   - Sets up the LLM provider (OpenAI, Bedrock, etc.)
//   - Connects to MCP servers based on configuration
//   - Applies custom system prompts if configured
//   - Sets up tool output handling
//
// Deprecated: Use NewAgentBuilder().With*() methods instead for better readability and immutability.
// This function will be removed in a future version.
func NewAgent(ctx context.Context, config Config) (Agent, error) {
	// Ensure tool choice is always set to prevent OpenAI API errors
	if config.ToolChoice == "" {
		config.ToolChoice = "auto"
	}

	// Validate system prompt configuration
	if err := ValidateSystemPromptConfig(config.SystemPrompt); err != nil {
		return nil, fmt.Errorf("invalid system prompt configuration: %w", err)
	}

	// Initialize tracer based on configuration
	var tracer observability.Tracer
	if config.Tracer != nil {
		tracer = config.Tracer
	} else {
		tracer = observability.GetTracer(config.TraceProvider)
	}

	// Initialize LLM
	llm, err := initializeLLM(config.Provider, config.ModelID, config.Temperature)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize LLM: %w", err)
	}

	// Note: Custom logger from config is not currently used

	// Create the underlying agent using the new functional options pattern
	var agent *mcpagent.Agent

	// Determine agent mode
	var agentMode mcpagent.AgentMode
	if config.AgentMode == ReActAgent {
		agentMode = mcpagent.ReActAgent
	} else {
		agentMode = mcpagent.SimpleAgent
	}

	// Create agent with functional options
	// Use custom logger if provided, otherwise create a default logger with file and console output
	var agentLogger utils.ExtendedLogger
	if config.Logger != nil {
		// Use our extended logger interface directly - no more two-stage approach!
		agentLogger = utils.AdaptLogger(config.Logger)
	} else {
		// Create a default logger with both file and console output
		defaultLogger, err := createDefaultLogger()
		if err != nil {
			return nil, fmt.Errorf("failed to create default logger: %w", err)
		}
		agentLogger = defaultLogger
	}

	// Now that we have a logger, check if we need to create a Langfuse tracer
	if config.Tracer == nil && config.TraceProvider == "langfuse" {
		// Create Langfuse tracer when TraceProvider is set to "langfuse"
		langfuseTracer, err := observability.NewLangfuseTracerWithLogger(agentLogger)
		if err != nil {
			agentLogger.Warnf("Failed to create Langfuse tracer: %v, falling back to noop tracer", err)
			tracer = observability.NoopTracer{}
		} else {
			tracer = langfuseTracer
			agentLogger.Infof("✅ Langfuse tracer created successfully for host: %s", config.LangfuseHost)
		}
	}

	// Generate a correlation ID - let internal agent handle trace creation
	var traceID observability.TraceID
	traceID = observability.TraceID(fmt.Sprintf("external_agent_%d", time.Now().UnixNano()))
	agentLogger.Infof("🔍 Generated correlation ID: %s", traceID)

	// Log the mode conversion using custom logger
	agentLogger.Info("🔧 Agent mode conversion", map[string]interface{}{
		"external_mode":  config.AgentMode,
		"mcp_mode":       agentMode,
		"mcp_mode_str":   string(agentMode),
		"trace_id":       string(traceID),
		"trace_provider": config.TraceProvider,
	})

	// Build agent options
	agentOptions := []mcpagent.AgentOption{
		mcpagent.WithMode(agentMode),
		mcpagent.WithTemperature(config.Temperature),
		mcpagent.WithToolChoice(config.ToolChoice),
		mcpagent.WithMaxTurns(config.MaxTurns),
		mcpagent.WithToolTimeout(config.ToolTimeout),
		// Enable smart routing for external agent (used by main streaming server)
		// This helps reduce tool overload and improve LLM performance
		mcpagent.WithSmartRouting(true),
		mcpagent.WithSmartRoutingThresholds(20, 4), // 20 tools, 4 servers threshold
	}

	agent, err = mcpagent.NewAgent(
		ctx,
		llm,
		config.ServerName,
		config.ConfigPath,
		config.ModelID,
		tracer,
		traceID,
		agentLogger,
		agentOptions...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// Add custom tools if specified in config (moved here after agent creation)
	// Note: Custom tools are now registered directly with the MCP agent using RegisterCustomTool
	// This provides a cleaner interface for tool registration

	// Log the created agent details using custom logger
	agentLogger.Info("🔧 Agent created successfully", map[string]interface{}{
		"agent_mode":        agent.AgentMode,
		"agent_mode_string": string(agent.AgentMode),
		"max_turns":         agent.MaxTurns,
		"temperature":       agent.Temperature,
		"tool_choice":       agent.ToolChoice,
		"smart_routing":     agent.EnableSmartRouting,
		"smart_routing_thresholds": map[string]interface{}{
			"max_tools":   agent.SmartRoutingThreshold.MaxTools,
			"max_servers": agent.SmartRoutingThreshold.MaxServers,
		},
	})

	// Log smart routing configuration
	agentLogger.Infof("🎯 Smart routing enabled for external agent - MaxTools: %d, MaxServers: %d",
		agent.SmartRoutingThreshold.MaxTools, agent.SmartRoutingThreshold.MaxServers)

	// Ensure internal agent's provider field reflects external config for accurate logging
	agent.SetProvider(config.Provider)

	// Configure ToolOutputHandler with correct output folder from environment
	if toolOutputHandler := agent.GetToolOutputHandler(); toolOutputHandler != nil {
		// Check for environment variable override
		if outputFolder := os.Getenv("TOOL_OUTPUT_FOLDER"); outputFolder != "" {
			toolOutputHandler.SetOutputFolder(outputFolder)
		}

		// Check for threshold override
		if thresholdStr := os.Getenv("TOOL_OUTPUT_THRESHOLD"); thresholdStr != "" {
			if threshold, err := strconv.Atoi(thresholdStr); err == nil {
				toolOutputHandler.SetThreshold(threshold)
			}
		}
	}

	// Apply custom system prompt if configured
	if config.SystemPrompt.Mode == "custom" && config.SystemPrompt.CustomTemplate != "" {
		// Build custom system prompt with the agent's tools and resources
		customPrompt := BuildSystemPrompt(
			config.SystemPrompt,
			buildToolsSectionFromAgent(agent),
			buildPromptsSection(agent.GetPrompts()),
			buildResourcesSection(agent.GetResources()),
			buildVirtualToolsSection(),
		)
		// Use the WithSystemPrompt option to properly set the custom prompt
		// This will set the hasCustomSystemPrompt flag to prevent overwriting
		agent.SetSystemPrompt(customPrompt)
	} else if config.SystemPrompt.AdditionalInstructions != "" {
		// Add additional instructions to the existing system prompt
		agent.SystemPrompt += "\n\n" + config.SystemPrompt.AdditionalInstructions
	}

	return &agentImpl{
		agent:   agent,
		config:  config,
		mu:      &sync.RWMutex{},
		logger:  agentLogger, // Initialize logger field
		traceID: traceID,     // 🆕 NEW: Store trace ID for cleanup
		tracer:  tracer,      // 🆕 NEW: Store tracer for cleanup
	}, nil
}

// NewAgentWithMCPServers creates a new agent with direct MCP server configuration.
//
// This function allows you to create an agent with MCP server configurations
// passed directly as structs, bypassing the need for config files.
//
// Parameters:
//   - ctx: Context for initialization and cancellation
//   - config: Agent configuration (can omit ServerName and ConfigPath)
//   - mcpServers: Map of server names to MCP server configurations
//
// Returns:
//   - A fully configured Agent instance ready for use
//   - Any error that occurred during initialization
//
// Example:
//
//	mcpServers := map[string]MCPServerConfig{
//	  "exa": {
//	    Description: "Exa search server",
//	    URL: "https://mcp.exa.ai/mcp?exaApiKey=your-key",
//	  },
//	}
//	agent, err := NewAgentWithMCPServers(ctx, config, mcpServers)
//
// Deprecated: Use NewAgentBuilder().WithMCPServers() instead for better readability and immutability.
// This function will be removed in a future version.
func NewAgentWithMCPServers(ctx context.Context, config Config, mcpServers map[string]MCPServerConfig) (Agent, error) {
	// Set the MCP servers in the config
	config.MCPServers = mcpServers

	// Create a temporary config file with the MCP server configurations
	tempConfigPath, err := createTempMCPServerConfig(mcpServers)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary MCP server config: %w", err)
	}

	// Update the config to use the temporary file
	config.ConfigPath = tempConfigPath
	config.ServerName = "temp-mcp-servers"

	// Create the agent using the standard constructor
	return NewAgent(ctx, config)
}

// createTempMCPServerConfig creates a temporary JSON config file with MCP server configurations
func createTempMCPServerConfig(mcpServers map[string]MCPServerConfig) (string, error) {
	// Create a temporary file
	tempFile, err := os.CreateTemp("", "mcp-servers-*.json")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer tempFile.Close()

	// Convert MCPServerConfig to the format expected by the MCP agent
	serverConfigs := make(map[string]interface{})
	for _, config := range mcpServers {
		serverConfig := map[string]interface{}{
			"description": config.Description,
		}

		// Set connection details based on what's provided
		if config.URL != "" {
			serverConfig["url"] = config.URL
		}
		if config.Command != "" {
			serverConfig["command"] = config.Command
			if len(config.Args) > 0 {
				serverConfig["args"] = config.Args
			}
		}
		if config.Protocol != "" {
			serverConfig["protocol"] = config.Protocol
		}
		if len(config.Env) > 0 {
			serverConfig["env"] = config.Env
		}
		if len(config.Headers) > 0 {
			serverConfig["headers"] = config.Headers
		}

		// Use "temp-mcp-servers" as the server name to match what the MCP agent expects
		// This is set in the config.ServerName field above
		serverConfigs["temp-mcp-servers"] = serverConfig
	}

	// Create the full config structure
	fullConfig := map[string]interface{}{
		"mcpServers": serverConfigs,
	}

	// Encode to JSON
	encoder := json.NewEncoder(tempFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(fullConfig); err != nil {
		return "", fmt.Errorf("failed to encode config to JSON: %w", err)
	}

	return tempFile.Name(), nil
}

// AgentCore implementation
func (a *agentImpl) Invoke(ctx context.Context, prompt string) (string, error) {
	// Check for context cancellation before invoking
	if ctx.Err() != nil {
		return "", fmt.Errorf("context cancelled before invoking: %w", ctx.Err())
	}
	return a.agent.Ask(ctx, prompt)
}

func (a *agentImpl) InvokeWithHistory(ctx context.Context, messages []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Check for context cancellation before invoking with history
	if ctx.Err() != nil {
		return "", nil, fmt.Errorf("context cancelled before invoking with history: %w", ctx.Err())
	}
	return a.agent.AskWithHistory(ctx, messages)
}

// Structured output functions for external agent
// AskStructured runs a single-question interaction and converts the result to structured output
func AskStructured[T any](a Agent, ctx context.Context, question string, schema T, schemaString string) (T, error) {
	// Check for context cancellation before invoking
	if ctx.Err() != nil {
		var zero T
		return zero, fmt.Errorf("context cancelled before invoking: %w", ctx.Err())
	}

	// Get the underlying events.Agent from the external agent
	agentImpl, ok := a.(*agentImpl)
	if !ok {
		var zero T
		return zero, fmt.Errorf("failed to get underlying agent implementation")
	}

	// Use the mcpagent structured output function
	return mcpagent.AskStructured(agentImpl.agent, ctx, question, schema, schemaString)
}

// AskWithHistoryStructured runs an interaction using message history and converts the result to structured output
func AskWithHistoryStructured[T any](a Agent, ctx context.Context, messages []llmtypes.MessageContent, schema T, schemaString string) (T, []llmtypes.MessageContent, error) {
	// Check for context cancellation before invoking with history
	if ctx.Err() != nil {
		var zero T
		return zero, nil, fmt.Errorf("context cancelled before invoking with history: %w", ctx.Err())
	}

	// Get the underlying events.Agent from the external agent
	agentImpl, ok := a.(*agentImpl)
	if !ok {
		var zero T
		return zero, nil, fmt.Errorf("failed to get underlying agent implementation")
	}

	// Use the mcpagent structured output function
	return mcpagent.AskWithHistoryStructured(agentImpl.agent, ctx, messages, schema, schemaString)
}

// AgentConfig implementation
func (a *agentImpl) SetCustomInstructions(instructions string) {
	a.customInstructions = instructions

	// Update the underlying agent's system prompt to include custom instructions
	if instructions != "" {
		// Append custom instructions to the existing system prompt
		updatedSystemPrompt := a.agent.SystemPrompt + "\n\n" + instructions
		a.agent.SystemPrompt = updatedSystemPrompt
	}
}

func (a *agentImpl) GetCustomInstructions() string {
	return a.customInstructions
}

func (a *agentImpl) GetConfig() Config {
	return a.config
}

func (a *agentImpl) UpdateConfig(config Config) error {
	// TODO: Implement config update logic
	a.config = config
	return nil
}

// AgentLifecycle implementation
func (a *agentImpl) Initialize(ctx context.Context) error {
	// The agent is already initialized in NewAgent
	return nil
}

func (a *agentImpl) Close() error {
	// Clean up streaming subscription if active
	a.streamingMu.Lock()
	if a.streamingSubscription != nil {
		a.streamingSubscription()
		a.streamingSubscription = nil
	}
	a.streamingMu.Unlock()

	// 🆕 NEW: End the trace if it's a Langfuse trace
	if a.traceID != "" && a.tracer != nil {
		// End the trace with session summary
		a.tracer.EndTrace(a.traceID, map[string]interface{}{
			"session_type":   "external_agent",
			"provider":       a.config.Provider,
			"model":          a.config.ModelID,
			"mode":           string(a.config.AgentMode),
			"ended_at":       time.Now().Format(time.RFC3339),
			"session_status": "completed",
		})
		a.logger.Infof("🔍 Langfuse: Ended trace %s", a.traceID)
	}

	if a.agent != nil {
		a.agent.Close()
	}
	return nil
}

func (a *agentImpl) GetContext() context.Context {
	return a.agent.GetContext()
}

// AgentMonitoring implementation
func (a *agentImpl) CheckHealth(ctx context.Context) map[string]error {
	// Check for context cancellation before health check
	if ctx.Err() != nil {
		return map[string]error{"context": fmt.Errorf("context cancelled before health check: %w", ctx.Err())}
	}
	return a.agent.CheckConnectionHealth(ctx)
}

func (a *agentImpl) GetStats() map[string]interface{} {
	return a.agent.GetConnectionStats()
}

func (a *agentImpl) GetServerNames() []string {
	return a.agent.GetServerNames()
}

// 🆕 NEW: Internal Logging Access
func (a *agentImpl) GetInternalLogger() utils.ExtendedLogger {
	// Return the underlying MCP agent's logger if available
	if a.agent != nil && a.agent.Logger != nil {
		return a.agent.Logger
	}

	// Fallback to default logger if no custom logger is set
	defaultLogger, err := createDefaultLogger()
	if err != nil {
		// If we can't create a default logger, return nil
		return nil
	}
	return defaultLogger
}

// AgentCapabilities implementation
func (a *agentImpl) GetCapabilities() string {
	return fmt.Sprintf("External agent with %s mode, %s provider, %s model",
		a.config.AgentMode, a.config.Provider, a.config.ModelID)
}

func (a *agentImpl) GetToolNames() []string {
	// TODO: Implement tool name collection
	return []string{}
}

// RegisterCustomTool registers a custom tool with both schema and execution function
// This maintains proper encapsulation while allowing custom tool registration
func (a *agentImpl) RegisterCustomTool(name string, description string, parameters map[string]interface{}, executionFunc func(ctx context.Context, args map[string]interface{}) (string, error)) {
	// Delegate to the underlying MCP agent
	if a.agent != nil {
		a.agent.RegisterCustomTool(name, description, parameters, executionFunc)
	}
}

// AgentEvents implementation
func (a *agentImpl) AddEventListener(listener AgentEventListener) {
	// Add the listener to our local list
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.listeners == nil {
		a.listeners = make([]AgentEventListener, 0)
	}
	a.listeners = append(a.listeners, listener)

	// 🆕 NEW: Use streaming tracer internally instead of event listeners
	// Check if the underlying agent supports streaming
	if a.agent.HasStreamingCapability() {
		// Use streaming tracer for more efficient event handling
		a.logger.Infof("🔍 Using streaming tracer for event handling (external package)")

		// Subscribe to agent's event stream
		eventChan, unsubscribe, hasStreaming := a.agent.SubscribeToEvents(context.Background())
		if hasStreaming {
			// Store the unsubscribe function for cleanup
			a.streamingMu.Lock()
			a.streamingSubscription = unsubscribe
			a.streamingMu.Unlock()

			// Start event forwarding from streaming tracer to external listeners
			go a.forwardEventsFromStream(eventChan, unsubscribe)
		} else {
			// Fall back to old event listener method
			a.logger.Warnf("Streaming subscription failed, falling back to event listeners")
			a.setupEventListenerAdapter()
		}
	} else {
		// Fall back to old event listener method
		a.logger.Infof("Agent doesn't support streaming, using event listeners")
		a.setupEventListenerAdapter()
	}
}

// setupEventListenerAdapter sets up the old event listener adapter for backward compatibility
func (a *agentImpl) setupEventListenerAdapter() {
	// Create an event listener adapter that converts MCP agent events to external format
	// and forwards them to our external listeners
	adapter := &eventListenerAdapter{
		externalListeners: a.listeners,
		mu:                a.mu,
	}

	// Add the adapter to the MCP agent to capture ALL events from now on
	a.agent.AddEventListener(adapter)
}

// forwardEventsFromStream forwards events from the streaming tracer to external listeners
func (a *agentImpl) forwardEventsFromStream(eventChan <-chan *events.AgentEvent, unsubscribe func()) {
	defer unsubscribe() // Ensure cleanup

	for event := range eventChan {
		// Convert MCP agent event to external format
		externalEvent := &AgentEvent{
			Type:          AgentEventType(event.Type),
			Timestamp:     event.Timestamp,
			Data:          event.Data,
			TraceID:       event.TraceID,
			ParentID:      event.ParentID,
			CorrelationID: event.CorrelationID,
		}

		// Forward to all external listeners
		a.mu.RLock()
		listeners := make([]AgentEventListener, len(a.listeners))
		copy(listeners, a.listeners)
		a.mu.RUnlock()

		for _, listener := range listeners {
			if err := listener.HandleEvent(context.Background(), externalEvent); err != nil {
				// Log error but continue with other listeners
				a.logger.Warnf("Error in external event listener %s: %v", listener.Name(), err)
			}
		}
	}
}

func (a *agentImpl) RemoveEventListener(listener AgentEventListener) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Remove from our local list
	for i, l := range a.listeners {
		if l == listener {
			a.listeners = append(a.listeners[:i], a.listeners[i+1:]...)
			break
		}
	}

	// Note: When using streaming tracer, we don't need to remove from the MCP agent
	// because we're using the streaming subscription, not the event listener adapter.
	// The streaming subscription is managed separately and will be cleaned up
	// when the external agent is closed.
}

func (a *agentImpl) EmitEvent(ctx context.Context, eventType AgentEventType, data events.EventData) {
	// Emit the event to all external listeners
	a.mu.RLock()
	defer a.mu.RUnlock()

	externalEvent := &AgentEvent{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
	}

	for _, listener := range a.listeners {
		if err := listener.HandleEvent(ctx, externalEvent); err != nil {
			// Log error but continue with other listeners
			fmt.Printf("Error in external event listener %s: %v\n", listener.Name(), err)
		}
	}
}

func (a *agentImpl) EmitTypedEvent(ctx context.Context, eventData events.EventData) {
	// Emit the typed event to all external listeners
	a.mu.RLock()
	defer a.mu.RUnlock()

	externalEvent := &AgentEvent{
		Type:      AgentEventType(eventData.GetEventType()),
		Data:      eventData,
		Timestamp: time.Now(),
		// Note: Correlation fields (TraceID, ParentID, CorrelationID) are not available
		// in this context since this is called directly with TypedEventData
		// Correlation data is only available when events come through the adapter
	}

	for _, listener := range a.listeners {
		if err := listener.HandleEvent(ctx, externalEvent); err != nil {
			// Log error but continue with other listeners
			fmt.Printf("Error in external event listener %s: %v\n", listener.Name(), err)
		}
	}
}

// eventListenerAdapter converts MCP agent events to external format and forwards them
type eventListenerAdapter struct {
	externalListeners []AgentEventListener
	mu                *sync.RWMutex
}

func (a *eventListenerAdapter) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	// Convert MCP agent event to external format
	externalEvent := &AgentEvent{
		Type:          AgentEventType(event.Type),
		Timestamp:     event.Timestamp,
		Data:          event.Data,
		TraceID:       event.TraceID,
		ParentID:      event.ParentID,
		CorrelationID: event.CorrelationID,
	}

	// Forward to all external listeners
	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, listener := range a.externalListeners {
		if err := listener.HandleEvent(ctx, externalEvent); err != nil {
			// Log error but continue with other listeners
			fmt.Printf("Error in external event listener %s: %v\n", listener.Name(), err)
		}
	}

	return nil
}

func (a *eventListenerAdapter) Name() string {
	return "external-event-adapter"
}

// No conversion function needed - using unified events directly

// createDefaultLogger creates a default logger with both file and console output
func createDefaultLogger() (utils.ExtendedLogger, error) {
	// Generate default filename with current date and time
	now := time.Now()
	filename := fmt.Sprintf("external-file-%s-%s.log",
		now.Format("2006-01-02"),
		now.Format("15-04-05"))

	// Create a logger with default settings:
	// - Custom filename with date and time
	// - Info level logging
	// - Text format
	// - Console output enabled
	return logger.CreateLogger(filename, "info", "text", true)
}
