package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	mcpagent "mcp-agent/agent_go/pkg/mcpagent"
)

// LLMAgentWrapper wraps the complex MCP Agent to provide a simple LLM-like interface
type LLMAgentWrapper struct {
	agent   *mcpagent.Agent
	name    string
	mu      sync.RWMutex
	closed  bool
	config  LLMAgentConfig
	metrics *agentMetricsImpl
	tracer  observability.Tracer
	traceID observability.TraceID
	logger  utils.ExtendedLogger

	// In-memory conversation history for multi-turn state
	history []llmtypes.MessageContent
}

// LLMAgentConfig holds configuration for the LLM agent wrapper
type LLMAgentConfig struct {
	Name               string
	ServerName         string
	ConfigPath         string
	Provider           llm.Provider // LLM provider (bedrock, openai, anthropic, openrouter)
	ModelID            string
	Temperature        float64
	ToolChoice         string
	MaxTurns           int
	StreamingChunkSize int
	Timeout            time.Duration
	ToolTimeout        time.Duration      // Tool execution timeout (default: 5 minutes)
	AgentMode          mcpagent.AgentMode // Agent mode (Simple or ReAct)
	CacheOnly          bool               // If true, only use cached servers (skip servers without cache)
	SelectedTools      []string           // Selected tools in "server:tool" format

	// Smart routing configuration
	EnableSmartRouting     bool // Enable smart routing for tool filtering
	SmartRoutingMaxTools   int  // Threshold for max tools before enabling smart routing
	SmartRoutingMaxServers int  // Threshold for max servers before enabling smart routing

	// Detailed LLM configuration from frontend
	FallbackModels        []string               // Custom fallback models from frontend
	CrossProviderFallback *CrossProviderFallback // Cross-provider fallback configuration
}

// CrossProviderFallback represents cross-provider fallback configuration
type CrossProviderFallback struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

// agentMetricsImpl is the concrete implementation of AgentMetrics interface
type agentMetricsImpl struct {
	mu sync.RWMutex

	// Request metrics
	TotalRequests      int64
	SuccessfulRequests int64
	FailedRequests     int64

	// Timing metrics
	TotalLatency   time.Duration
	MinLatency     time.Duration
	MaxLatency     time.Duration
	AverageLatency time.Duration

	// Token metrics
	TotalTokensUsed int64
	InputTokens     int64
	OutputTokens    int64

	// Tool metrics
	ToolCallsExecuted  int64
	ToolCallsSucceeded int64
	ToolCallsFailed    int64

	// Stream metrics
	StreamsStarted   int64
	StreamsCompleted int64
	StreamsFailed    int64

	// Status tracking
	IsHealthy       bool
	LastRequestTime time.Time
	LastSuccessTime time.Time
	LastErrorTime   time.Time
	LastError       error
}

// NewLLMAgentWrapper creates a new LLM agent wrapper
func NewLLMAgentWrapper(ctx context.Context, config LLMAgentConfig, tracer observability.Tracer, logger utils.ExtendedLogger) (*LLMAgentWrapper, error) {
	// If no tracer is provided, automatically get one based on environment configuration
	if tracer == nil {
		tracer = observability.GetTracer("noop")
	}
	return NewLLMAgentWrapperWithTrace(ctx, config, tracer, "", logger)
}

// NewLLMAgentWrapperWithTrace creates a new LLM agent wrapper with hierarchical tracing support
func NewLLMAgentWrapperWithTrace(ctx context.Context, config LLMAgentConfig, tracer observability.Tracer, mainTraceID observability.TraceID, logger utils.ExtendedLogger) (*LLMAgentWrapper, error) {
	logger.Infof("NewLLMAgentWrapper received config: %+v", config)
	logger.Infof("Creating agent with config path: %s", config.ConfigPath)
	if config.Name == "" {
		config.Name = "mcp-agent"
	}

	// Set default tool timeout if not specified
	if config.ToolTimeout == 0 {
		config.ToolTimeout = 5 * time.Minute
		logger.Infof("Setting default tool timeout to %v", config.ToolTimeout)
	}

	// Create trace ID for agent initialization
	var traceID observability.TraceID
	if mainTraceID != "" {
		// Use the main trace ID for hierarchical tracing
		traceID = mainTraceID
	} else {
		// Create a new trace ID for this agent
		traceID = observability.TraceID(fmt.Sprintf("agent-init-%s-%d", config.Name, time.Now().UnixNano()))
	}

	// Initialize the LLM externally (using Bedrock as default)
	logger.Infof("NewLLMAgentWrapper initializing LLM with provider: %s, model_id: %s", config.Provider, config.ModelID)
	llm, err := initializeLLMWithConfig(config, logger, tracer, traceID)
	if err != nil {
		// Emit error event instead of ending trace
		if tracer != nil && mainTraceID == "" {
			// Create error event for standalone agent
			errorEvent := &events.AgentErrorEvent{
				BaseEventData: events.BaseEventData{
					TraceID: string(traceID),
				},
				Error:    "failed to initialize LLM: " + err.Error(),
				Turn:     0,
				Context:  "agent_initialization",
				Duration: 0,
			}
			// Convert to AgentEvent and emit
			agentEvent := events.NewAgentEvent(errorEvent)
			agentEvent.TraceID = string(traceID)
			tracer.EmitEvent(agentEvent)
		}
		return nil, fmt.Errorf("failed to initialize LLM: %w", err)
	}

	// Initialize the underlying MCP agent with the new API
	var agent *mcpagent.Agent

	// Build agent options with smart routing configuration
	agentOptions := []mcpagent.AgentOption{
		mcpagent.WithTemperature(config.Temperature),
		mcpagent.WithToolChoice(config.ToolChoice),
		mcpagent.WithMaxTurns(config.MaxTurns),
		mcpagent.WithToolTimeout(config.ToolTimeout),
		mcpagent.WithCacheOnly(config.CacheOnly),
	}

	// Add cross-provider fallback configuration if provided
	if config.CrossProviderFallback != nil {
		// Convert from agentwrapper.CrossProviderFallback to mcpagent.CrossProviderFallback
		crossProviderFallback := &mcpagent.CrossProviderFallback{
			Provider: config.CrossProviderFallback.Provider,
			Models:   config.CrossProviderFallback.Models,
		}
		agentOptions = append(agentOptions, mcpagent.WithCrossProviderFallback(crossProviderFallback))
		logger.Infof("🔄 Cross-provider fallback configured - Provider: %s, Models: %v",
			crossProviderFallback.Provider, crossProviderFallback.Models)
	}

	// Add selected tools if provided
	if len(config.SelectedTools) > 0 {
		agentOptions = append(agentOptions, mcpagent.WithSelectedTools(config.SelectedTools))
		logger.Infof("🔧 Selected tools configured: %d tools", len(config.SelectedTools))
	}

	// Add smart routing options if enabled
	if config.EnableSmartRouting {
		// Set smart routing thresholds (use defaults if not specified)
		maxTools := config.SmartRoutingMaxTools
		if maxTools == 0 {
			maxTools = 20 // Default threshold
		}
		maxServers := config.SmartRoutingMaxServers
		if maxServers == 0 {
			maxServers = 4 // Default threshold
		}

		agentOptions = append(agentOptions,
			mcpagent.WithSmartRouting(true),
			mcpagent.WithSmartRoutingThresholds(maxTools, maxServers),
			// Use default smart routing config (temperature: 0.1, maxTokens: 5000, etc.)
			mcpagent.WithSmartRoutingConfig(0.1, 5000, 8, 200, 300),
		)

		logger.Infof("🎯 Smart routing enabled - MaxTools: %d, MaxServers: %d (using defaults for temperature/tokens)",
			maxTools, maxServers)
	} else {
		logger.Infof("🔧 Smart routing disabled - using all available tools")
	}

	if config.AgentMode == mcpagent.ReActAgent {
		// Create ReAct agent
		agent, err = mcpagent.NewReActAgent(
			ctx,
			llm,
			config.ServerName,
			config.ConfigPath,
			config.ModelID,
			tracer,
			traceID,
			logger, // Pass the logger parameter directly
			agentOptions...,
		)
	} else {
		// Create Simple agent (default)
		agent, err = mcpagent.NewSimpleAgent(
			ctx,
			llm,
			config.ServerName,
			config.ConfigPath,
			config.ModelID,
			tracer,
			traceID,
			logger, // Pass the logger parameter directly
			agentOptions...,
		)
	}
	if err != nil {
		// Emit error event instead of ending trace
		if tracer != nil && mainTraceID == "" {
			// Create error event for standalone agent
			errorEvent := &events.AgentErrorEvent{
				BaseEventData: events.BaseEventData{
					TraceID: string(traceID),
				},
				Error:    err.Error(),
				Turn:     0,
				Context:  "agent_creation",
				Duration: 0,
			}
			// Convert to AgentEvent and emit
			agentEvent := events.NewAgentEvent(errorEvent)
			agentEvent.TraceID = string(traceID)
			tracer.EmitEvent(agentEvent)
		}
		return nil, fmt.Errorf("failed to create MCP agent: %w", err)
	}

	// Set the agent's provider field
	agent.SetProvider(config.Provider)

	// Note: Event bridge integration will be added later to avoid import cycles
	// For now, the agent will use its own event system which is compatible with Langfuse

	// Initialize metrics
	metrics := &agentMetricsImpl{
		MinLatency:      time.Duration(^uint64(0) >> 1), // Max duration value
		IsHealthy:       true,
		LastRequestTime: time.Now(),
	}

	wrapper := &LLMAgentWrapper{
		agent:   agent,
		name:    config.Name,
		config:  config,
		metrics: metrics,
		tracer:  tracer,
		traceID: traceID,
		logger:  logger,
	}

	// Don't end the trace immediately - let it be ended after conversation completion
	if mainTraceID == "" {
		// For standalone agent traces, we'll end them after conversation completion
		logger.Infof("Created agent trace for conversation: %s", traceID)
	} else {
		// For hierarchical tracing, don't end the main trace - let the parent handle it
		if tracer != nil {
			// Just log that we're using hierarchical tracing
			logger.Infof("Using hierarchical tracing, main_trace_id: %s", mainTraceID)
		}
	}

	return wrapper, nil
}

// Invoke implements the LLMAgent interface - simple prompt-in, response-out
func (w *LLMAgentWrapper) Invoke(ctx context.Context, prompt string) (string, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return "", errors.New("agent is closed")
	}

	// Add user message to wrapper history for tracking
	w.history = append(w.history, llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: prompt}},
	})
	w.mu.Unlock()

	// Use InvokeWithHistory to maintain proper conversation state
	return w.InvokeWithHistory(ctx, w.GetHistory())
}

// InvokeWithHistory allows multi-turn conversation by passing a full message history.
func (w *LLMAgentWrapper) InvokeWithHistory(ctx context.Context, messages []llmtypes.MessageContent) (string, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return "", errors.New("agent is closed")
	}
	// Use the passed messages directly, don't overwrite internal history
	w.mu.Unlock()

	// Create timeout context
	timeoutCtx := ctx
	if w.config.Timeout > 0 {
		var cancel context.CancelFunc
		timeoutCtx, cancel = context.WithTimeout(ctx, w.config.Timeout)
		defer cancel()
	}

	// Start tracking metrics
	startTime := time.Now()
	w.updateRequestMetrics()

	// Emit server selection event
	if w.agent != nil {
		// Get the list of connected servers
		serverNames := w.agent.GetServerNames()
		totalServers := len(serverNames)

		// Determine source based on configuration
		source := "manual"
		if w.config.ServerName == "all" || len(serverNames) == 0 {
			source = "all"
		}

		// Debug logging
		w.logger.Infof("Server selection event - server_names: %v, total_servers: %d, source: %s, config_server_name: %s",
			serverNames, totalServers, source, w.config.ServerName)

		// Create server selection event
		serverSelectionEvent := events.NewMCPServerSelectionEvent(
			1, // turn 1 for initial query
			serverNames,
			totalServers,
			source,
			"", // query will be extracted from messages if needed
		)

		// Emit the event
		w.agent.EmitTypedEvent(ctx, serverSelectionEvent)
	}

	// Check for context cancellation before executing the request
	if ctx.Err() != nil {
		w.logger.Infof("Context cancelled before agent execution: %s", ctx.Err().Error())
		return "", fmt.Errorf("agent execution cancelled: %w", ctx.Err())
	}

	// Execute the request with message history
	response, updatedMessages, err := w.agent.AskWithHistory(timeoutCtx, messages)
	duration := time.Since(startTime)

	// End the trace after conversation completion
	if w.traceID != "" && w.tracer != nil {
		w.logger.Infof("Ending agent trace - trace_id: %s, response_length: %d, duration_ms: %d",
			w.traceID, len(response), duration.Milliseconds())

		// Agent end event removed - no longer needed
	} else {
		w.logger.Infof("Not ending trace - trace_id: %s, tracer: %v", w.traceID, w.tracer != nil)
	}

	// Update metrics based on result
	if err != nil {
		w.updateFailureMetrics(duration, err)
		return response, fmt.Errorf("agent request failed: %w", err)
	}

	w.updateSuccessMetrics(duration, response)

	// Add assistant message to history
	w.mu.Lock()
	w.history = updatedMessages
	w.mu.Unlock()

	return response, nil
}

// GetCapabilities implements the AgentCapabilities interface
func (w *LLMAgentWrapper) GetCapabilities() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.closed {
		return map[string]interface{}{"error": "Agent is closed"}
	}

	toolNames := make([]string, 0, len(w.agent.Tools))
	for _, tool := range w.agent.Tools {
		toolNames = append(toolNames, tool.Function.Name)
	}

	return map[string]interface{}{
		"name":      w.name,
		"model":     w.config.ModelID,
		"server":    w.config.ServerName,
		"tools":     toolNames,
		"max_turns": w.config.MaxTurns,
	}
}

// GetUnderlyingAgent returns the underlying MCP agent for direct access
func (w *LLMAgentWrapper) GetUnderlyingAgent() *mcpagent.Agent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.agent
}

// GetName implements the AgentCapabilities interface
func (w *LLMAgentWrapper) GetName() string {
	return w.name
}

// GetVersion implements the AgentCapabilities interface
func (w *LLMAgentWrapper) GetVersion() string {
	return "1.0.0"
}

// Start implements the AgentLifecycle interface
func (w *LLMAgentWrapper) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return errors.New("cannot start closed agent")
	}

	// Agent is already started during initialization
	w.metrics.IsHealthy = true
	w.metrics.LastSuccessTime = time.Now()

	return nil
}

// Stop implements the AgentLifecycle interface
func (w *LLMAgentWrapper) Stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil // Already stopped
	}

	w.closed = true
	w.metrics.IsHealthy = false

	// Close the underlying agent
	if w.agent != nil {
		w.agent.Close()
	}

	return nil
}

// IsHealthy implements the AgentLifecycle interface
func (w *LLMAgentWrapper) IsHealthy() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return !w.closed && w.metrics.IsHealthy
}

// GetMetrics implements the AgentMetrics interface
func (w *LLMAgentWrapper) GetMetrics() map[string]interface{} {
	w.metrics.mu.RLock()
	defer w.metrics.mu.RUnlock()

	return map[string]interface{}{
		"total_requests":       w.metrics.TotalRequests,
		"successful_requests":  w.metrics.SuccessfulRequests,
		"failed_requests":      w.metrics.FailedRequests,
		"total_latency_ms":     w.metrics.TotalLatency.Milliseconds(),
		"min_latency_ms":       w.metrics.MinLatency.Milliseconds(),
		"max_latency_ms":       w.metrics.MaxLatency.Milliseconds(),
		"average_latency_ms":   w.metrics.AverageLatency.Milliseconds(),
		"total_tokens_used":    w.metrics.TotalTokensUsed,
		"input_tokens":         w.metrics.InputTokens,
		"output_tokens":        w.metrics.OutputTokens,
		"tool_calls_executed":  w.metrics.ToolCallsExecuted,
		"tool_calls_succeeded": w.metrics.ToolCallsSucceeded,
		"tool_calls_failed":    w.metrics.ToolCallsFailed,
		"streams_started":      w.metrics.StreamsStarted,
		"streams_completed":    w.metrics.StreamsCompleted,
		"streams_failed":       w.metrics.StreamsFailed,
		"is_healthy":           w.metrics.IsHealthy,
		"last_request_time":    w.metrics.LastRequestTime,
		"last_success_time":    w.metrics.LastSuccessTime,
		"last_error_time":      w.metrics.LastErrorTime,
		"last_error":           w.getLastErrorString(),
	}
}

// GetToolDefinitions returns the full tool definitions (name, description, parameters) for advanced UI display
func (w *LLMAgentWrapper) GetToolDefinitions() []map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.closed || w.agent == nil {
		return nil
	}

	tools := w.agent.Tools
	result := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		if t.Function == nil {
			continue
		}
		entry := map[string]interface{}{
			"name":        t.Function.Name,
			"description": t.Function.Description,
			"parameters":  t.Function.Parameters, // Already JSON-schema
		}
		result = append(result, entry)
	}
	return result
}

// GetHistory returns a copy of the current conversation history
func (w *LLMAgentWrapper) GetHistory() []llmtypes.MessageContent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	h := make([]llmtypes.MessageContent, len(w.history))
	copy(h, w.history)
	return h
}

// ClearHistory resets the in-memory conversation history
func (w *LLMAgentWrapper) ClearHistory() {
	w.mu.Lock()
	w.history = nil
	w.mu.Unlock()
}

// AppendUserMessage adds a user message to the agent's history
func (w *LLMAgentWrapper) AppendUserMessage(text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	// Let the agent handle everything - just add user message to wrapper history for tracking
	w.history = append(w.history, llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: text}},
	})
}

// AppendAssistantMessage adds an assistant message to the agent's history
func (w *LLMAgentWrapper) AppendAssistantMessage(text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.history = append(w.history, llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeAI,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: text}},
	})
}

// AppendMessage adds a message to the conversation history
func (w *LLMAgentWrapper) AppendMessage(msg llmtypes.MessageContent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.history = append(w.history, msg)
}

// ExecuteTask implements the Agent interface for task execution compatibility.
func (w *LLMAgentWrapper) ExecuteTask(ctx context.Context, description string) (string, error) {
	w.logger.Infof("Entered LLMAgentWrapper.ExecuteTask: %s", description)
	return w.Invoke(ctx, description)
}

// Helper methods for metrics tracking

func (w *LLMAgentWrapper) updateRequestMetrics() {
	w.metrics.mu.Lock()
	defer w.metrics.mu.Unlock()

	w.metrics.TotalRequests++
	w.metrics.LastRequestTime = time.Now()
}

func (w *LLMAgentWrapper) updateSuccessMetrics(duration time.Duration, response string) {
	w.metrics.mu.Lock()
	defer w.metrics.mu.Unlock()

	w.metrics.SuccessfulRequests++
	w.metrics.LastSuccessTime = time.Now()
	w.metrics.IsHealthy = true

	// Update latency metrics
	w.metrics.TotalLatency += duration
	if duration < w.metrics.MinLatency {
		w.metrics.MinLatency = duration
	}
	if duration > w.metrics.MaxLatency {
		w.metrics.MaxLatency = duration
	}
	if w.metrics.TotalRequests > 0 {
		w.metrics.AverageLatency = w.metrics.TotalLatency / time.Duration(w.metrics.TotalRequests)
	}

	// Estimate token usage (simplified)
	w.metrics.OutputTokens += int64(len(response) / 4) // Rough estimation
}

func (w *LLMAgentWrapper) updateFailureMetrics(duration time.Duration, err error) {
	w.metrics.mu.Lock()
	defer w.metrics.mu.Unlock()

	w.metrics.FailedRequests++
	w.metrics.LastErrorTime = time.Now()
	w.metrics.LastError = err

	// Update latency metrics even for failures
	w.metrics.TotalLatency += duration
	if duration < w.metrics.MinLatency {
		w.metrics.MinLatency = duration
	}
	if duration > w.metrics.MaxLatency {
		w.metrics.MaxLatency = duration
	}
	if w.metrics.TotalRequests > 0 {
		w.metrics.AverageLatency = w.metrics.TotalLatency / time.Duration(w.metrics.TotalRequests)
	}
}

func (w *LLMAgentWrapper) getLastErrorString() string {
	if w.metrics.LastError == nil {
		return ""
	}
	return w.metrics.LastError.Error()
}

// initializeLLMWithConfig initializes an LLM using detailed configuration from frontend
func initializeLLMWithConfig(config LLMAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, traceID observability.TraceID) (llmtypes.Model, error) {
	// Validate and convert provider string to llm.Provider type
	llmProvider, err := llm.ValidateProvider(string(config.Provider))
	if err != nil {
		return nil, fmt.Errorf("invalid LLM provider '%s': %w", config.Provider, err)
	}

	// Build fallback models list
	var fallbackModels []string

	// Add custom fallback models from frontend if provided
	if len(config.FallbackModels) > 0 {
		fallbackModels = append(fallbackModels, config.FallbackModels...)
		logger.Infof("Using custom fallback models from frontend: %v", config.FallbackModels)
	} else {
		// Use default fallback models for the provider
		fallbackModels = append(fallbackModels, llm.GetDefaultFallbackModels(llmProvider)...)
		logger.Infof("Using default fallback models for provider %s: %v", config.Provider, fallbackModels)
	}

	// Add cross-provider fallback models if configured
	if config.CrossProviderFallback != nil && len(config.CrossProviderFallback.Models) > 0 {
		fallbackModels = append(fallbackModels, config.CrossProviderFallback.Models...)
		logger.Infof("Added cross-provider fallback models for %s: %v", config.CrossProviderFallback.Provider, config.CrossProviderFallback.Models)
	} else {
		// Add default cross-provider fallbacks
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llmProvider)
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		logger.Infof("Added default cross-provider fallback models: %v", crossProviderFallbacks)
	}

	// Use the existing LLM provider system with detailed fallback models
	llmConfig := llm.Config{
		Provider:       llmProvider,
		ModelID:        config.ModelID,
		Temperature:    config.Temperature,
		TraceID:        traceID, // Pass the trace ID for proper span hierarchy
		FallbackModels: fallbackModels,
		MaxRetries:     3,
		Logger:         logger,
	}

	// Initialize the LLM using the factory with detailed fallback support
	return llm.InitializeLLM(llmConfig)
}

// initializeLLM initializes an LLM based on the provider, model ID and temperature (legacy function)
func initializeLLM(provider, modelID string, temperature float64, logger utils.ExtendedLogger, tracer observability.Tracer, traceID observability.TraceID) (llmtypes.Model, error) {
	// Create a basic config and use the new function
	config := LLMAgentConfig{
		Provider:    llm.Provider(provider),
		ModelID:     modelID,
		Temperature: temperature,
	}
	return initializeLLMWithConfig(config, logger, tracer, traceID)
}

// GetEventDispatcher returns the agent's event dispatcher for direct event access
func (w *LLMAgentWrapper) GetEventDispatcher() interface{} {
	// Event dispatcher was removed in simplified architecture
	// Events now go directly to tracers via EmitTypedEvent
	w.logger.Infof("🔍 DEBUG: EventDispatcher removed - use EmitTypedEvent instead")
	return nil
}

// AddEventListener adds an event listener to the agent's event dispatcher
func (w *LLMAgentWrapper) AddEventListener(listener interface{}) {
	// Event listeners were removed in simplified architecture
	// Events now go directly to tracers via EmitTypedEvent
	w.logger.Infof("🔍 DEBUG: AddEventListener removed - use EmitTypedEvent instead")
}

// EmitEvent emits an event through the agent's event dispatcher
func (w *LLMAgentWrapper) EmitEvent(ctx context.Context, eventType events.AgentEventType, data map[string]interface{}) {
	// EmitEvent was removed in simplified architecture
	// Use EmitTypedEvent instead for typed events
	w.logger.Infof("🔍 DEBUG: EmitEvent removed - use EmitTypedEvent instead")
}

// EmitTypedEvent emits a typed event through the agent's event dispatcher
func (w *LLMAgentWrapper) EmitTypedEvent(ctx context.Context, eventData events.EventData) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed || w.agent == nil {
		return
	}
	w.agent.EmitTypedEvent(ctx, eventData)
}

// StreamWithEvents streams text chunks from the agent during execution
// Events are handled separately via the EventObserver and polling API
func (w *LLMAgentWrapper) StreamWithEvents(ctx context.Context, prompt string) (<-chan string, error) {
	w.mu.RLock()
	if w.closed {
		w.mu.Unlock()
		return nil, errors.New("agent is closed")
	}
	w.mu.RUnlock()

	// Create channel for text chunks only
	textChan := make(chan string, 50)

	// Start streaming in a goroutine
	go func() {
		defer close(textChan)

		// Add user message to history
		w.AppendUserMessage(prompt)

		// Get conversation history and execute
		messages := w.GetHistory()

		// Execute the request with the agent
		response, updatedMessages, err := w.agent.AskWithHistory(ctx, messages)

		if err != nil {
			// Send error event via the existing EventObserver (no duplicate listener needed)
			return
		}

		// Update the agent's history with the updated messages from the conversation
		if len(updatedMessages) > len(messages) {
			w.mu.Lock()
			w.history = updatedMessages
			w.mu.Unlock()
		}

		// Send the full response as a single chunk
		if response != "" {
			select {
			case <-ctx.Done():
				return
			case textChan <- response:
				// Full response sent successfully
			}
		}
	}()

	return textChan, nil
}

// RemoveEventListener removes an event listener from the agent's event dispatcher
func (w *LLMAgentWrapper) RemoveEventListener(listener interface{}) {
	// Event listeners were removed in simplified architecture
	w.logger.Infof("🔍 DEBUG: RemoveEventListener removed - event system simplified")
}
