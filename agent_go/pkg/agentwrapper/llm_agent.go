package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	agentlogger "mcp-agent-builder-go/agent_go/pkg/logger"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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
	logger  loggerv2.Logger

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
	SelectedTools      []string           // Selected tools in "server:tool" format

	// Smart routing configuration
	EnableSmartRouting     bool // Enable smart routing for tool filtering
	SmartRoutingMaxTools   int  // Threshold for max tools before enabling smart routing
	SmartRoutingMaxServers int  // Threshold for max servers before enabling smart routing

	// Unified fallback configuration (replaces FallbackModels and CrossProviderFallback)
	Fallbacks []FallbackModel // Fallback models with optional provider override
	// Code execution mode: When enabled, only virtual tools are added to LLM
	// MCP tools are accessed via generated Go code using discover_code_files and write_code
	UseCodeExecutionMode bool
	// Tool search mode: When enabled, LLM discovers tools on-demand via search_tools
	UseToolSearchMode bool
	// Pre-discovered tools: Tools always available in tool search mode (without needing to search)
	PreDiscoveredTools []string
	APIKeys            *llm.ProviderAPIKeys // API keys for providers

	// Context summarization configuration
	EnableContextSummarization     bool    // Enable context summarization feature
	SummarizeOnTokenThreshold      bool    // Enable token-based summarization trigger (percentage-based)
	TokenThresholdPercent          float64 // Percentage of context window to trigger summarization (0.0-1.0, default: 0.8 = 80%)
	SummarizeOnFixedTokenThreshold bool    // Enable fixed token-based summarization trigger
	FixedTokenThreshold            int     // Fixed token threshold to trigger summarization (e.g., 100000 = 100k tokens, default: 100k)
	SummaryKeepLastMessages        int     // Number of recent messages to keep when summarizing (0 = use default: 4)

	// Context editing configuration
	EnableContextEditing        bool // Enable context editing (dynamic context reduction)
	ContextEditingThreshold     int  // Token threshold for context editing (0 = use default: 100)
	ContextEditingTurnThreshold int  // Turn age threshold for context editing (0 = use default: 5)

	// Context offloading configuration
	LargeOutputThreshold int // Token threshold for context offloading (0 = use default: 10000)

	// Parallel tool execution: When enabled, multiple tool calls in a single LLM response
	// are executed concurrently using a fork-join pattern instead of sequentially
	EnableParallelToolExecution bool

	// MCP session management for connection reuse
	// When set, MCP connections are shared via session registry instead of creating new connections
	// This enables browser reuse in Playwright and other stateful MCP servers
	SessionID string

	// User ID for per-user OAuth token isolation
	// When set, OAuth tokens for MCP servers are stored at user-specific paths
	// This enables multi-user deployments where each user's OAuth credentials are isolated
	UserID string
}

// FallbackModel represents a fallback model configuration
// If Provider is empty, it uses the same provider as the primary model
type FallbackModel struct {
	Provider string `json:"provider,omitempty"` // Optional: override provider for cross-provider fallback
	ModelID  string `json:"model_id"`
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
func NewLLMAgentWrapper(ctx context.Context, config LLMAgentConfig, tracer observability.Tracer, logger loggerv2.Logger) (*LLMAgentWrapper, error) {
	// If no tracer is provided, automatically get one based on environment configuration
	if tracer == nil {
		tracer = observability.GetTracer("noop")
	}
	return NewLLMAgentWrapperWithTrace(ctx, config, tracer, "", logger)
}

// NewLLMAgentWrapperWithTrace creates a new LLM agent wrapper with hierarchical tracing support
func NewLLMAgentWrapperWithTrace(ctx context.Context, config LLMAgentConfig, tracer observability.Tracer, mainTraceID observability.TraceID, logger loggerv2.Logger) (*LLMAgentWrapper, error) {
	logger.Info(fmt.Sprintf("NewLLMAgentWrapper received config: %+v", config))
	logger.Info(fmt.Sprintf("Creating agent with config path: %s", config.ConfigPath))
	if config.Name == "" {
		config.Name = "mcp-agent"
	}

	// Set default tool timeout if not specified
	if config.ToolTimeout == 0 {
		config.ToolTimeout = 5 * time.Minute
		logger.Info(fmt.Sprintf("Setting default tool timeout to %v", config.ToolTimeout))
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
	logger.Info(fmt.Sprintf("NewLLMAgentWrapper initializing LLM with provider: %s, model_id: %s", config.Provider, config.ModelID))
	llm, err := initializeLLMWithConfig(config, logger, traceID)
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
		mcpagent.WithMaxTurns(config.MaxTurns),
		mcpagent.WithToolTimeout(config.ToolTimeout),
	}
	// Only set tool_choice when non-empty — Azure/OpenAI reject tool_choice when no tools are present
	if config.ToolChoice != "" {
		agentOptions = append(agentOptions, mcpagent.WithToolChoice(config.ToolChoice))
	}

	// Add cross-provider fallback configuration if any fallbacks have a different provider
	var crossProviderModels []string
	var crossProviderName string
	for _, fb := range config.Fallbacks {
		if fb.Provider != "" && fb.Provider != string(config.Provider) {
			crossProviderModels = append(crossProviderModels, fb.ModelID)
			if crossProviderName == "" {
				crossProviderName = fb.Provider
			}
		}
	}
	if len(crossProviderModels) > 0 {
		crossProviderFallback := &mcpagent.CrossProviderFallback{
			Provider: crossProviderName,
			Models:   crossProviderModels,
		}
		agentOptions = append(agentOptions, mcpagent.WithCrossProviderFallback(crossProviderFallback))
		logger.Info(fmt.Sprintf("🔄 Cross-provider fallback configured - Provider: %s, Models: %v",
			crossProviderName, crossProviderModels))
	}

	// Add selected servers for tool filtering
	// Parse ServerName (comma-separated string) into array for WithSelectedServers
	if config.ServerName != "" && config.ServerName != "all" {
		// Split comma-separated server names and trim whitespace
		serverNames := strings.Split(config.ServerName, ",")
		trimmedServers := make([]string, 0, len(serverNames))
		for _, name := range serverNames {
			trimmed := strings.TrimSpace(name)
			if trimmed != "" {
				trimmedServers = append(trimmedServers, trimmed)
			}
		}
		if len(trimmedServers) > 0 {
			agentOptions = append(agentOptions, mcpagent.WithSelectedServers(trimmedServers))
			logger.Info(fmt.Sprintf("🔧 Selected servers configured: %v", trimmedServers))
		}
	}

	// Add selected tools if provided
	if len(config.SelectedTools) > 0 {
		agentOptions = append(agentOptions, mcpagent.WithSelectedTools(config.SelectedTools))
		logger.Info(fmt.Sprintf("🔧 Selected tools configured: %d tools", len(config.SelectedTools)))
	}

	// Add code execution mode if enabled
	if config.UseCodeExecutionMode {
		agentOptions = append(agentOptions, mcpagent.WithCodeExecutionMode(true))
		logger.Info("🔧 Code execution mode enabled - MCP tools will be accessed via generated Go code")
	}

	// Add tool search mode if enabled
	if config.UseToolSearchMode {
		agentOptions = append(agentOptions, mcpagent.WithToolSearchMode(true))
		logger.Info("🔍 Tool search mode enabled - LLM will discover tools on-demand via search_tools and add them via add_tool")
	}

	// Add pre-discovered tools if specified (tools available without searching in tool search mode)
	if len(config.PreDiscoveredTools) > 0 {
		agentOptions = append(agentOptions, mcpagent.WithPreDiscoveredTools(config.PreDiscoveredTools))
		logger.Info(fmt.Sprintf("🔧 Pre-discovered tools configured: %v", config.PreDiscoveredTools))
	}

	// Add session ID for MCP connection reuse (e.g., Playwright browser sharing)
	if config.SessionID != "" {
		agentOptions = append(agentOptions, mcpagent.WithSessionID(config.SessionID))
		logger.Info(fmt.Sprintf("🔗 MCP session ID configured for connection reuse: %s", config.SessionID))
	}

	// Add user ID for per-user OAuth token isolation
	if config.UserID != "" {
		agentOptions = append(agentOptions, mcpagent.WithUserID(config.UserID))
		logger.Info(fmt.Sprintf("👤 User ID configured for per-user OAuth isolation: %s", config.UserID))
	}

	// Add parallel tool execution if enabled
	if config.EnableParallelToolExecution {
		agentOptions = append(agentOptions, mcpagent.WithParallelToolExecution(true))
		logger.Info("⚡ Parallel tool execution enabled - multiple tool calls will run concurrently")
	}

	// Add context summarization options if enabled
	if config.EnableContextSummarization {
		agentOptions = append(agentOptions, mcpagent.WithContextSummarization(true))
		if config.SummarizeOnTokenThreshold {
			thresholdPercent := config.TokenThresholdPercent
			if thresholdPercent <= 0 || thresholdPercent > 1.0 {
				thresholdPercent = 0.8 // Default to 80%
			}
			agentOptions = append(agentOptions, mcpagent.WithSummarizeOnTokenThreshold(true, thresholdPercent))
		}
		if config.SummarizeOnFixedTokenThreshold && config.FixedTokenThreshold > 0 {
			agentOptions = append(agentOptions, mcpagent.WithSummarizeOnFixedTokenThreshold(true, config.FixedTokenThreshold))
		}
		if config.SummaryKeepLastMessages > 0 {
			agentOptions = append(agentOptions, mcpagent.WithSummaryKeepLastMessages(config.SummaryKeepLastMessages))
		}
		logger.Info(fmt.Sprintf("📝 Context summarization enabled - Token threshold: %v (%.0f%%), Fixed threshold: %v (%d tokens), Keep last messages: %d",
			config.SummarizeOnTokenThreshold, config.TokenThresholdPercent*100, config.SummarizeOnFixedTokenThreshold, config.FixedTokenThreshold, config.SummaryKeepLastMessages))
	}

	// Add context editing options if enabled
	if config.EnableContextEditing {
		agentOptions = append(agentOptions, mcpagent.WithContextEditing(true))
		if config.ContextEditingThreshold > 0 {
			agentOptions = append(agentOptions, mcpagent.WithContextEditingThreshold(config.ContextEditingThreshold))
		}
		if config.ContextEditingTurnThreshold > 0 {
			agentOptions = append(agentOptions, mcpagent.WithContextEditingTurnThreshold(config.ContextEditingTurnThreshold))
		}
		logger.Info(fmt.Sprintf("✂️ Context editing enabled - Token threshold: %d, Turn threshold: %d",
			config.ContextEditingThreshold, config.ContextEditingTurnThreshold))
	}

	// Add large output threshold for context offloading if specified
	if config.LargeOutputThreshold > 0 {
		agentOptions = append(agentOptions, mcpagent.WithLargeOutputThreshold(config.LargeOutputThreshold))
		logger.Info(fmt.Sprintf("📦 Large output threshold set to %d tokens", config.LargeOutputThreshold))
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

		logger.Info(fmt.Sprintf("🎯 Smart routing enabled - MaxTools: %d, MaxServers: %d (using defaults for temperature/tokens)",
			maxTools, maxServers))
	} else {
		logger.Info("🔧 Smart routing disabled - using all available tools")
	}

	// Use logger directly (already loggerv2.Logger)
	var v2Logger loggerv2.Logger
	if logger != nil {
		v2Logger = logger
	} else {
		v2Logger = loggerv2.NewDefault()
	}

	// Build options from parameters
	options := agentOptions
	if config.ServerName != "" && config.ServerName != "all" {
		options = append(options, mcpagent.WithServerName(config.ServerName))
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

	// Enable streaming for LLM text responses (emits streaming_start, streaming_chunk, streaming_end events)
	options = append(options, mcpagent.WithStreaming(true))

	if config.AgentMode == mcpagent.SimpleAgent {
		// Create Simple agent
		// modelID is automatically extracted from llm
		agent, err = mcpagent.NewSimpleAgent(
			ctx,
			llm,
			config.ConfigPath,
			options...,
		)
	} else {
		// Create Simple agent (default)
		// modelID is automatically extracted from llm
		agent, err = mcpagent.NewSimpleAgent(
			ctx,
			llm,
			config.ConfigPath,
			options...,
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

	// Set the agent's API keys for fallback LLM creation
	if config.APIKeys != nil {
		// Convert from wrapper API keys to agent API keys
		agentAPIKeys := &mcpagent.AgentAPIKeys{
			OpenRouter: config.APIKeys.OpenRouter,
			OpenAI:     config.APIKeys.OpenAI,
			Anthropic:  config.APIKeys.Anthropic,
			Vertex:     config.APIKeys.Vertex,
		}
		if config.APIKeys.Bedrock != nil {
			agentAPIKeys.Bedrock = &mcpagent.AgentBedrockConfig{
				Region: config.APIKeys.Bedrock.Region,
			}
		}
		if config.APIKeys.Azure != nil {
			agentAPIKeys.Azure = &mcpagent.AgentAzureConfig{
				Endpoint:   config.APIKeys.Azure.Endpoint,
				APIKey:     config.APIKeys.Azure.APIKey,
				APIVersion: config.APIKeys.Azure.APIVersion,
				Region:     config.APIKeys.Azure.Region,
			}
		}
		agent.APIKeys = agentAPIKeys
		logger.Info("🔑 API keys configured for agent fallback LLM creation")
	}

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
		logger.Info(fmt.Sprintf("Created agent trace for conversation: %s", traceID))
	} else {
		// For hierarchical tracing, don't end the main trace - let the parent handle it
		if tracer != nil {
			// Just log that we're using hierarchical tracing
			logger.Info(fmt.Sprintf("Using hierarchical tracing, main_trace_id: %s", mainTraceID))
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

		// Debug logging removed - excessive verbosity

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
		w.logger.Info(fmt.Sprintf("Context canceled before agent execution: %s", ctx.Err().Error()))
		return "", fmt.Errorf("agent execution canceled: %w", ctx.Err())
	}

	// Execute the request with message history
	response, updatedMessages, err := w.agent.AskWithHistory(timeoutCtx, messages)
	duration := time.Since(startTime)

	// End the trace after conversation completion
	if w.traceID != "" && w.tracer != nil {
		w.logger.Info(fmt.Sprintf("Ending agent trace - trace_id: %s, response_length: %d, duration_ms: %d",
			w.traceID, len(response), duration.Milliseconds()))

		// Agent end event removed - no longer needed
	} else {
		w.logger.Info(fmt.Sprintf("Not ending trace - trace_id: %s, tracer: %v", w.traceID, w.tracer != nil))
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

// GetUnderlyingAgent returns the underlying MCP agent for direct access
func (w *LLMAgentWrapper) GetUnderlyingAgent() *mcpagent.Agent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.agent
}

// AgentMetricsSnapshot is a read-only snapshot of agent metrics
type AgentMetricsSnapshot struct {
	InputTokens       int64
	OutputTokens      int64
	ToolCallsExecuted int64
	TotalCostUSD      float64
}

// GetMetricsSnapshot returns a snapshot of the agent's current metrics
func (w *LLMAgentWrapper) GetMetricsSnapshot() AgentMetricsSnapshot {
	w.metrics.mu.RLock()
	defer w.metrics.mu.RUnlock()
	snapshot := AgentMetricsSnapshot{
		InputTokens:       w.metrics.InputTokens,
		OutputTokens:      w.metrics.OutputTokens,
		ToolCallsExecuted: w.metrics.ToolCallsExecuted,
	}
	// Get total cost from the underlying agent (includes provider-reported costs)
	if w.agent != nil {
		_, _, _, _, _, _, _, _, _, totalCost, _, _, _ := w.agent.GetTokenUsageWithPricing()
		snapshot.TotalCostUSD = totalCost
	}
	return snapshot
}

// GetName implements the AgentCapabilities interface
func (w *LLMAgentWrapper) GetName() string {
	return w.name
}

// GetHistory returns a copy of the current conversation history
func (w *LLMAgentWrapper) GetHistory() []llmtypes.MessageContent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	h := make([]llmtypes.MessageContent, len(w.history))
	copy(h, w.history)
	return h
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

// AppendMessage adds a message to the conversation history
func (w *LLMAgentWrapper) AppendMessage(msg llmtypes.MessageContent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.history = append(w.history, msg)
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

// initializeLLMWithConfig initializes an LLM using detailed configuration from frontend
func initializeLLMWithConfig(config LLMAgentConfig, logger loggerv2.Logger, traceID observability.TraceID) (llmtypes.Model, error) {
	// Validate and convert provider string to llm.Provider type
	llmProvider, err := llm.ValidateProvider(string(config.Provider))
	if err != nil {
		return nil, fmt.Errorf("invalid LLM provider '%s': %w", config.Provider, err)
	}

	// Build fallback models list from unified Fallbacks structure
	var fallbackModels []string

	// Add custom fallback models from config if provided
	if len(config.Fallbacks) > 0 {
		for _, fb := range config.Fallbacks {
			// Format: provider/model for cross-provider fallbacks, or just model for same-provider
			if fb.Provider != "" && fb.Provider != string(config.Provider) {
				fallbackModels = append(fallbackModels, fmt.Sprintf("%s/%s", fb.Provider, fb.ModelID))
			} else {
				fallbackModels = append(fallbackModels, fb.ModelID)
			}
		}
		logger.Info(fmt.Sprintf("Using custom fallback models from config: %v", fallbackModels))
	} else {
		// Use default fallback models for the provider
		fallbackModels = append(fallbackModels, llm.GetDefaultFallbackModels(llmProvider)...)
		// Also add default cross-provider fallbacks
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llmProvider)
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		logger.Info(fmt.Sprintf("Using default fallback models for provider %s: %v", config.Provider, fallbackModels))
	}

	// Create a separate LLM logger that writes to llm_debug.log
	// This separates LLM logs (including [GEMINI] logs from multi-llm-provider-go) from server logs
	var v2LoggerForLLM loggerv2.Logger
	llmLogger, err := agentlogger.CreateLogger("logs/llm_debug.log", "info", "text", true)
	if err != nil {
		// Fallback to the provided logger if LLM logger creation fails
		if logger != nil {
			v2LoggerForLLM = logger
		} else {
			v2LoggerForLLM = loggerv2.NewDefault()
		}
	} else {
		v2LoggerForLLM = llmLogger
	}

	// Use the existing LLM provider system with detailed fallback models
	llmConfig := llm.Config{
		Provider:       llmProvider,
		ModelID:        config.ModelID,
		Temperature:    config.Temperature,
		TraceID:        traceID, // Pass the trace ID for proper span hierarchy
		FallbackModels: fallbackModels,
		MaxRetries:     3,
		Logger:         v2LoggerForLLM,
		APIKeys:        config.APIKeys, // Use API keys directly from config
	}

	// Initialize the LLM using the factory with detailed fallback support
	return llm.InitializeLLM(llmConfig)
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
		w.mu.RUnlock()
		return nil, errors.New("agent is closed")
	}
	w.mu.RUnlock()

	// Create channel for text chunks only
	textChan := make(chan string, 50)

	// Start streaming in a goroutine
	go func() {
		defer close(textChan)

		// Set up real-time streaming callback to forward content chunks as they arrive.
		// This is critical for CLI providers (Gemini CLI, Claude Code) where the entire
		// agentic loop runs inside the CLI process — without this, the user sees nothing
		// until the full response is ready.
		streamedAny := false
		streamedChunks := 0
		w.mu.Lock()
		prevCallback := w.agent.StreamingCallback
		w.agent.StreamingCallback = func(chunk llmtypes.StreamChunk) {
			if chunk.Type == llmtypes.StreamChunkTypeContent && chunk.Content != "" {
				if !streamedAny {
					w.logger.Info(fmt.Sprintf("[STREAMING] First real-time chunk received (len=%d), streaming callback active", len(chunk.Content)))
				}
				streamedAny = true
				streamedChunks++
				select {
				case <-ctx.Done():
				case textChan <- chunk.Content:
				}
			}
			// Chain to previous callback if any
			if prevCallback != nil {
				prevCallback(chunk)
			}
		}
		w.mu.Unlock()
		w.logger.Info("[STREAMING] Real-time streaming callback installed")

		// Restore previous callback on exit
		defer func() {
			w.mu.Lock()
			w.agent.StreamingCallback = prevCallback
			w.mu.Unlock()
			if streamedAny {
				w.logger.Info(fmt.Sprintf("[STREAMING] Streamed %d chunks in real-time", streamedChunks))
			} else {
				w.logger.Info("[STREAMING] No real-time chunks received, will send full response")
			}
		}()

		// Add user message to history
		w.AppendUserMessage(prompt)

		// Get conversation history and execute
		messages := w.GetHistory()

		// Execute the request with the agent
		response, updatedMessages, err := w.agent.AskWithHistory(ctx, messages)

		if err != nil {
			w.logger.Error("AskWithHistory failed", err)
			// Surface a user-visible error message so the frontend doesn't just silently hang.
			errMsg := "⚠️ An error occurred while generating a response. Please try again."
			errStr := err.Error()
			if strings.Contains(errStr, "gemini cli overloaded") ||
				strings.Contains(errStr, "high demand") ||
				strings.Contains(errStr, "signal: killed") ||
				strings.Contains(errStr, "gemini cli execution failed") {
				errMsg = "⚠️ Gemini API is currently overloaded — no response received. Please try again in a moment."
			} else if strings.Contains(errStr, "context deadline exceeded") || strings.Contains(errStr, "context canceled") {
				errMsg = "⚠️ Request timed out. Please try again."
			}
			select {
			case <-ctx.Done():
			case textChan <- errMsg:
			}
			return
		}

		// Update the agent's history with the updated messages from the conversation
		// Always update - messages may have been summarized (fewer) or expanded (more)
		w.mu.Lock()
		w.history = updatedMessages
		w.mu.Unlock()

		// Only send the full response if we didn't already stream it via callback.
		// For non-streaming providers (standard API), no callback fires and we send the full text.
		// For CLI providers with streaming, chunks were already sent incrementally.
		if !streamedAny && response != "" {
			select {
			case <-ctx.Done():
				return
			case textChan <- response:
			}
		}
	}()

	return textChan, nil
}
