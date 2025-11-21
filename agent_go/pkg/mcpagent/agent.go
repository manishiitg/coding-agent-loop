package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"llm-providers/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent/prompt"
	"mcp-agent/agent_go/pkg/mcpcache"
	"mcp-agent/agent_go/pkg/mcpclient"
)

// CustomTool represents a custom tool with its definition and execution function
type CustomTool struct {
	Definition llmtypes.Tool
	Execution  func(ctx context.Context, args map[string]interface{}) (string, error)
}

// AgentEventListener defines the interface for event listeners
type AgentEventListener interface {
	HandleEvent(ctx context.Context, event *events.AgentEvent) error
	Name() string
}

// AgentMode defines the type of agent behavior
type AgentMode string

const (
	// SimpleAgent is the standard tool-using agent without explicit reasoning
	SimpleAgent AgentMode = "simple"
)

// AgentOption defines a functional option for configuring an Agent
type AgentOption func(*Agent)

// WithMode sets the agent mode
func WithMode(mode AgentMode) AgentOption {
	return func(a *Agent) {
		a.AgentMode = mode
	}
}

// WithLogger sets a custom logger
func WithLogger(logger utils.ExtendedLogger) AgentOption {
	return func(a *Agent) {
		a.Logger = logger
	}
}

// WithProvider sets the LLM provider
func WithProvider(provider llm.Provider) AgentOption {
	return func(a *Agent) {
		a.provider = provider
	}
}

// WithMaxTurns sets the maximum conversation turns
func WithMaxTurns(maxTurns int) AgentOption {
	return func(a *Agent) {
		a.MaxTurns = maxTurns
	}
}

// WithTemperature sets the LLM temperature
func WithTemperature(temperature float64) AgentOption {
	return func(a *Agent) {
		a.Temperature = temperature
	}
}

// WithToolChoice sets the tool choice strategy
func WithToolChoice(toolChoice string) AgentOption {
	return func(a *Agent) {
		a.ToolChoice = toolChoice
	}
}

// WithLargeOutputVirtualTools enables/disables large output virtual tools
func WithLargeOutputVirtualTools(enabled bool) AgentOption {
	return func(a *Agent) {
		a.EnableLargeOutputVirtualTools = enabled
	}
}

// WithToolTimeout sets the tool execution timeout
func WithToolTimeout(timeout time.Duration) AgentOption {
	return func(a *Agent) {
		a.ToolTimeout = timeout
	}
}

// WithCustomTools adds custom tools to the agent during creation
func WithCustomTools(tools []llmtypes.Tool) AgentOption {
	return func(a *Agent) {
		a.Tools = append(a.Tools, tools...)
	}
}

// WithSmartRouting enables/disables smart routing for tool filtering
func WithSmartRouting(enabled bool) AgentOption {
	return func(a *Agent) {
		a.EnableSmartRouting = enabled
	}
}

// WithSmartRoutingThresholds sets custom thresholds for smart routing
func WithSmartRoutingThresholds(maxTools, maxServers int) AgentOption {
	return func(a *Agent) {
		a.SmartRoutingThreshold.MaxTools = maxTools
		a.SmartRoutingThreshold.MaxServers = maxServers
	}
}

// WithSmartRoutingConfig sets additional smart routing configuration
func WithSmartRoutingConfig(temperature float64, maxTokens, maxMessages, userMsgLimit, assistantMsgLimit int) AgentOption {
	return func(a *Agent) {
		a.SmartRoutingConfig.Temperature = temperature
		a.SmartRoutingConfig.MaxTokens = maxTokens
		a.SmartRoutingConfig.MaxMessages = maxMessages
		a.SmartRoutingConfig.UserMsgLimit = userMsgLimit
		a.SmartRoutingConfig.AssistantMsgLimit = assistantMsgLimit
	}
}

// WithCacheOnly sets whether to use only cached servers (skip servers without cache)
func WithCacheOnly(cacheOnly bool) AgentOption {
	return func(a *Agent) {
		a.CacheOnly = cacheOnly
	}
}

// WithSystemPrompt sets a custom system prompt
func WithSystemPrompt(systemPrompt string) AgentOption {
	return func(a *Agent) {
		a.SystemPrompt = systemPrompt
		a.hasCustomSystemPrompt = true
	}
}

// WithDiscoverResource enables/disables resource discovery in system prompt
func WithDiscoverResource(enabled bool) AgentOption {
	return func(a *Agent) {
		a.DiscoverResource = enabled
	}
}

// WithDiscoverPrompt enables/disables prompt discovery in system prompt
func WithDiscoverPrompt(enabled bool) AgentOption {
	return func(a *Agent) {
		a.DiscoverPrompt = enabled
	}
}

// WithCrossProviderFallback sets the cross-provider fallback configuration
func WithCrossProviderFallback(crossProviderFallback *CrossProviderFallback) AgentOption {
	return func(a *Agent) {
		a.CrossProviderFallback = crossProviderFallback
	}
}

// WithSelectedTools sets specific tools to use (format: "server:tool")
func WithSelectedTools(tools []string) AgentOption {
	return func(a *Agent) {
		a.selectedTools = tools
	}
}

// WithSelectedServers sets the selected servers list
func WithSelectedServers(servers []string) AgentOption {
	return func(a *Agent) {
		// Store selected servers for tool filtering logic
		// This is used to determine which servers should use "all tools" mode
		a.selectedServers = servers
	}
}

// Agent wraps MCP clients, an LLM, and an observability tracer to answer questions using tool calls.
// It is generic enough to be reused by CLI commands, services, or tests.
type Agent struct {
	// Context for cancellation and lifecycle management
	ctx context.Context

	// Legacy single client (first in the list) kept for backward compatibility
	Client mcpclient.ClientInterface

	// NEW: multiple clients keyed by server name
	Clients map[string]mcpclient.ClientInterface

	// Map tool name → server name (quick dispatch)
	toolToServer map[string]string

	LLM     llmtypes.Model
	Tracers []observability.Tracer // Support multiple tracers
	Tools   []llmtypes.Tool

	// Configuration knobs
	MaxTurns        int
	Temperature     float64
	ToolChoice      string
	ModelID         string
	AgentMode       AgentMode     // NEW: Agent mode (Simple or ReAct)
	ToolTimeout     time.Duration // Tool execution timeout (default: 5 minutes)
	selectedTools   []string      // Selected tools in "server:tool" format
	selectedServers []string      // Selected servers list for "all tools" mode determination

	// Enhanced tracking info
	SystemPrompt string
	TraceID      observability.TraceID
	configPath   string // Path to MCP config file for on-demand connections

	// cached list of server names (for metadata convenience)
	servers []string

	// Event system for observability - REMOVED: No longer using event dispatchers

	// Provider information
	provider llm.Provider

	// Large tool output handling
	toolOutputHandler *utils.ToolOutputHandler

	// Large output virtual tools configuration
	EnableLargeOutputVirtualTools bool

	// Store prompts and resources for system prompt rebuilding
	prompts   map[string][]mcp.Prompt
	resources map[string][]mcp.Resource

	// Flag to track if a custom system prompt was provided
	hasCustomSystemPrompt bool

	// Custom tools that are handled as virtual tools
	customTools map[string]CustomTool

	// Custom logger (optional) - uses our ExtendedLogger interface for consistency
	Logger utils.ExtendedLogger

	// Listeners for typed events
	listeners []AgentEventListener
	mu        sync.RWMutex

	// Smart routing configuration with defaults
	EnableSmartRouting    bool
	SmartRoutingThreshold struct {
		MaxTools   int
		MaxServers int
	}

	// Smart routing configuration for additional parameters
	SmartRoutingConfig struct {
		Temperature       float64
		MaxTokens         int
		MaxMessages       int
		UserMsgLimit      int
		AssistantMsgLimit int
	}

	// Pre-filtered tools for smart routing (determined once at conversation start)
	filteredTools []llmtypes.Tool

	// NEW: Track appended system prompts separately for smart routing
	AppendedSystemPrompts []string // Track each appended prompt
	OriginalSystemPrompt  string   // Keep original system prompt
	HasAppendedPrompts    bool     // Flag to indicate if any prompts were appended

	// Hierarchy tracking fields for event tree structure
	currentParentEventID  string // Track current parent event ID
	currentHierarchyLevel int    // Track current hierarchy level (0=root, 1=child, etc.)

	// Cache behavior configuration
	CacheOnly bool // If true, only use cached servers (skip servers without cache)

	// Resource discovery configuration
	DiscoverResource bool // If true, include resource details in system prompt (default: true)

	// Prompt discovery configuration
	DiscoverPrompt bool // If true, include prompt details in system prompt (default: true)

	// Cross-provider fallback configuration
	CrossProviderFallback *CrossProviderFallback // Cross-provider fallback configuration from frontend

	// Cumulative token tracking for entire conversation
	cumulativePromptTokens     int          // Cumulative prompt/input tokens
	cumulativeCompletionTokens int          // Cumulative completion/output tokens
	cumulativeTotalTokens      int          // Cumulative total tokens
	cumulativeCacheTokens      int          // Cumulative cache tokens (sum of all cache-related tokens)
	cumulativeReasoningTokens  int          // Cumulative reasoning tokens (for models like o3)
	cumulativeCacheDiscount    float64      // Sum of cache discounts (for averaging)
	llmCallCount               int          // Number of LLM calls made
	cacheEnabledCallCount      int          // Number of calls with cache tokens > 0
	tokenTrackingMutex         sync.RWMutex // Mutex for thread-safe token accumulation
}

// CrossProviderFallback represents cross-provider fallback configuration
type CrossProviderFallback struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

// GetProvider returns the provider
func (a *Agent) GetProvider() llm.Provider {
	return a.provider
}

// GetToolOutputHandler returns the tool output handler
func (a *Agent) GetToolOutputHandler() *utils.ToolOutputHandler {
	return a.toolOutputHandler
}

// GetPrompts returns the prompts map
func (a *Agent) GetPrompts() map[string][]mcp.Prompt {
	return a.prompts
}

// GetResources returns the resources map
func (a *Agent) GetResources() map[string][]mcp.Resource {
	return a.resources
}

// GetToolToServer returns the tool to server mapping
func (a *Agent) GetToolToServer() map[string]string {
	return a.toolToServer
}

// SetProvider sets the provider
func (a *Agent) SetProvider(provider llm.Provider) {
	a.provider = provider
}

// SetToolOutputHandler sets the tool output handler
func (a *Agent) SetToolOutputHandler(handler *utils.ToolOutputHandler) {
	a.toolOutputHandler = handler
}

// NewAgent creates a new Agent with the given options
func NewAgent(ctx context.Context, llm llmtypes.Model, serverName, configPath, modelID string, tracer observability.Tracer, traceID observability.TraceID, logger utils.ExtendedLogger, options ...AgentOption) (*Agent, error) {

	logger.Info("🔍 NewAgent started", map[string]interface{}{"config_path": configPath})

	// Load merged MCP servers configuration (base + user)
	config, err := mcpclient.LoadMergedConfig(configPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load merged MCP config: %w", err)
	}

	logger.Info("Merged config contains servers", map[string]interface{}{"server_count": len(config.MCPServers)})
	for name := range config.MCPServers {
		logger.Info("Server found", map[string]interface{}{"server_name": name})
	}

	if tracer == nil {
		tracer = observability.GetTracer("noop")
	}

	// Create streaming tracer that wraps the base tracer
	streamingTracer := NewStreamingTracer(tracer, 100)

	// Create tracers array with streaming tracer
	tracers := []observability.Tracer{streamingTracer}

	if llm == nil {
		return nil, fmt.Errorf("LLM cannot be nil")
	}

	// Create agent with default values first to get CacheOnly setting
	ag := &Agent{
		ctx:                           ctx,
		LLM:                           llm,
		Tracers:                       tracers,
		MaxTurns:                      GetDefaultMaxTurns(SimpleAgent), // Default to simple mode
		Temperature:                   0.2,                             // Default temperature
		ToolChoice:                    "auto",                          // Default tool choice
		ModelID:                       modelID,
		AgentMode:                     SimpleAgent, // Default to simple mode
		TraceID:                       traceID,
		provider:                      "",                          // Will be set by caller
		EnableLargeOutputVirtualTools: true,                        // Default to enabled
		Logger:                        logger,                      // Use the passed logger parameter
		customTools:                   make(map[string]CustomTool), // Initialize custom tools map

		// Smart routing configuration with defaults
		EnableSmartRouting: false, // Default to disabled for now
		SmartRoutingThreshold: struct {
			MaxTools   int
			MaxServers int
		}{
			MaxTools:   30, // Default threshold
			MaxServers: 4,  // Default threshold
		},
		// Smart routing configuration for additional parameters
		SmartRoutingConfig: struct {
			Temperature       float64
			MaxTokens         int
			MaxMessages       int
			UserMsgLimit      int
			AssistantMsgLimit int
		}{
			Temperature:       0.1,  // Default temperature for routing
			MaxTokens:         5000, // Default max tokens for routing
			MaxMessages:       8,    // Default max conversation messages
			UserMsgLimit:      200,  // Default user message character limit
			AssistantMsgLimit: 300,  // Default assistant message character limit
		},

		// Initialize hierarchy tracking fields
		currentParentEventID:  "", // Start with no parent
		currentHierarchyLevel: 0,  // Start at root level

		// Initialize cache behavior (default: false - connect to all servers)
		CacheOnly: false,

		// Initialize resource discovery (default: true - include resources in system prompt)
		DiscoverResource: true,

		// Initialize prompt discovery (default: true - include prompts in system prompt)
		DiscoverPrompt: true,
	}

	// Apply all options to get the final CacheOnly setting
	for _, option := range options {
		option(ag)
	}

	// 🆕 DETAILED AGENT CONNECTION DEBUG LOGGING
	logger.Infof("🤖 [DEBUG] About to call NewAgentConnection - Time: %v", time.Now())
	logger.Infof("🤖 [DEBUG] NewAgentConnection params - ServerName: %s, ConfigPath: %s, CacheOnly: %v", serverName, configPath, ag.CacheOnly)
	logger.Infof("🤖 [DEBUG] LLM details - Provider: %T, Model: %v", llm, llm != nil)

	clients, toolToServer, allLLMTools, servers, prompts, resources, systemPrompt, err := NewAgentConnection(ctx, llm, serverName, configPath, string(traceID), tracers, logger, ag.CacheOnly)

	// 🆕 POST-CONNECTION DEBUG LOGGING
	logger.Infof("🤖 [DEBUG] NewAgentConnection completed - Time: %v", time.Now())
	logger.Infof("🤖 [DEBUG] Connection results - Clients: %d, Tools: %d, Servers: %d, Error: %v", len(clients), len(allLLMTools), len(servers), err != nil)

	if err != nil {
		logger.Errorf("🤖 [DEBUG] NewAgentConnection failed - Error: %v, Error type: %T", err, err)
		return nil, err
	}

	// Use first client for legacy compatibility
	var firstClient mcpclient.ClientInterface
	if len(clients) > 0 {
		for _, c := range clients {
			firstClient = c
			break
		}
	}

	// Initialize tool output handler
	toolOutputHandler := utils.NewToolOutputHandler()

	// Large output handling is now done via virtual tools, not MCP server
	// Virtual tools are enabled by default and handle file operations directly
	toolOutputHandler.SetServerAvailable(true) // Always available with virtual tools

	// Set session ID for organizing files by conversation
	toolOutputHandler.SetSessionID(string(traceID))

	// Update the existing agent with connection data
	ag.Client = firstClient
	ag.Clients = clients
	ag.toolToServer = toolToServer
	ag.Tools = allLLMTools
	ag.SystemPrompt = systemPrompt
	ag.servers = servers
	ag.toolOutputHandler = toolOutputHandler
	ag.prompts = prompts
	ag.resources = resources
	ag.filteredTools = allLLMTools
	ag.configPath = configPath

	// Apply selected tools filter if specified
	// Empty selectedTools array means "use all tools" (no filtering)
	// Non-empty selectedTools array means "use only these specific tools"
	// IMPORTANT: If a server is in selectedServers but has NO tools in selectedTools,
	// it means "use ALL tools from that server" (all tools mode for that server)
	// Also supports "server:*" pattern to explicitly request all tools from a server
	if len(ag.selectedTools) > 0 {
		logger.Infof("🔧 Tool filtering active: %d specific tools selected", len(ag.selectedTools))

		// Create set for fast lookup of specific tools
		selectedToolSet := make(map[string]bool)
		for _, fullName := range ag.selectedTools {
			selectedToolSet[fullName] = true
		}

		// Build map of servers that have "all tools" pattern (server:*)
		serversWithAllTools := make(map[string]bool)
		// Build map of which servers have specific tools (not "all tools")
		serversWithSpecificTools := make(map[string]bool)
		for _, fullName := range ag.selectedTools {
			// Parse "server:tool" or "server:*" format
			parts := strings.SplitN(fullName, ":", 2)
			if len(parts) == 2 {
				serverName := parts[0]
				toolName := parts[1]
				if toolName == "*" {
					// "server:*" means all tools from this server
					serversWithAllTools[serverName] = true
				} else {
					// Specific tool selected
					serversWithSpecificTools[serverName] = true
				}
			}
		}

		// Filter tools: include specific tools OR all tools from servers with "*" pattern
		var filteredTools []llmtypes.Tool
		for _, tool := range allLLMTools {
			// Get server name for this tool
			serverName, exists := toolToServer[tool.Function.Name]
			if !exists {
				// Custom/virtual tool - always include
				filteredTools = append(filteredTools, tool)
				continue
			}

			// Check if this server has "all tools" pattern
			if serversWithAllTools[serverName] {
				// Server has "server:*" pattern - include ALL tools from this server
				filteredTools = append(filteredTools, tool)
			} else if serversWithSpecificTools[serverName] {
				// Server has specific tools - check if this tool is selected
				fullName := fmt.Sprintf("%s:%s", serverName, tool.Function.Name)
				if selectedToolSet[fullName] {
					filteredTools = append(filteredTools, tool)
				}
			} else {
				// Server has no tools in selectedTools - include ALL tools from this server
				// (this is "all tools" mode for this server when it's in selectedServers)
				filteredTools = append(filteredTools, tool)
			}
		}

		logger.Infof("🔧 Tool filtering complete: %d tools selected from %d total", len(filteredTools), len(allLLMTools))
		ag.Tools = filteredTools
		ag.filteredTools = filteredTools
	} else {
		// No specific tools selected - use all available tools
		logger.Infof("🔧 Using all available tools: %d tools (no filtering applied)", len(allLLMTools))
		ag.Tools = allLLMTools
		ag.filteredTools = allLLMTools
	}

	// Always rebuild system prompt with the correct agent mode
	// This ensures Simple agents get Simple prompts and ReAct agents get ReAct prompts
	if !ag.hasCustomSystemPrompt {
		ag.SystemPrompt = prompt.BuildSystemPromptWithoutTools(ag.prompts, ag.resources, string(ag.AgentMode), ag.DiscoverResource, ag.DiscoverPrompt, ag.Logger)
	}

	// Add virtual tools to the LLM tools list
	virtualTools := ag.CreateVirtualTools()
	ag.Tools = append(ag.Tools, virtualTools...)

	// 🎯 SMART ROUTING INITIALIZATION - Run AFTER all tools are loaded (including virtual tools)
	// This ensures we have the complete tool count for accurate smart routing decisions
	logger.Infof("🎯 [DEBUG] Smart routing check - EnableSmartRouting: %v, shouldUseSmartRouting: %v", ag.EnableSmartRouting, ag.shouldUseSmartRouting())
	logger.Infof("🎯 [DEBUG] Smart routing context - Time: %v", time.Now())

	if ag.shouldUseSmartRouting() {
		// Get server count for logging (cached vs active)
		var serverCount int
		var serverType string
		if ag.CacheOnly {
			// Count unique servers from tool-to-server mapping
			serverSet := make(map[string]bool)
			for _, serverName := range ag.toolToServer {
				serverSet[serverName] = true
			}
			serverCount = len(serverSet)
			serverType = "cached"
		} else {
			serverCount = len(ag.Clients)
			serverType = "active"
		}

		logger.Infof("🎯 Smart routing enabled - determining relevant tools after full initialization")
		logger.Infof("🎯 Total tools loaded: %d, %s servers: %d (thresholds: tools>%d, servers>%d)",
			len(ag.Tools), serverType, serverCount, ag.SmartRoutingThreshold.MaxTools, ag.SmartRoutingThreshold.MaxServers)

		// For now, use all tools since we don't have conversation context yet
		// Smart routing will be re-evaluated in AskWithHistory with full conversation context
		ag.filteredTools = ag.Tools
		logger.Infof("🎯 Smart routing will be applied during conversation with full context")
	} else {
		// Get server count for logging (cached vs active)
		var serverCount int
		var serverType string
		if ag.CacheOnly {
			// Count unique servers from tool-to-server mapping
			serverSet := make(map[string]bool)
			for _, serverName := range ag.toolToServer {
				serverSet[serverName] = true
			}
			serverCount = len(serverSet)
			serverType = "cached"
			logger.Infof("🔧 DEBUG: Cache-only mode - toolToServer map has %d entries, unique servers: %d", len(ag.toolToServer), serverCount)
			// Extract server names for debugging
			serverNames := make([]string, 0, len(serverSet))
			for serverName := range serverSet {
				serverNames = append(serverNames, serverName)
			}
			logger.Infof("🔧 DEBUG: Server names in toolToServer: %v", serverNames)
		} else {
			serverCount = len(ag.Clients)
			serverType = "active"
			logger.Infof("🔧 DEBUG: Active mode - Clients map has %d entries", serverCount)
		}

		// No smart routing - use all tools
		ag.filteredTools = ag.Tools
		logger.Infof("🔧 Smart routing disabled - using all %d tools (%s servers: %d, thresholds: tools>%d, servers>%d)",
			len(ag.Tools), serverType, serverCount, ag.SmartRoutingThreshold.MaxTools, ag.SmartRoutingThreshold.MaxServers)
	}

	// No more event listeners - events go directly to tracer
	// Langfuse tracing is handled by the tracer itself

	// Agent initialization complete

	return ag, nil
}

// SetCurrentQuery sets the current query for hierarchy tracking
func (a *Agent) SetCurrentQuery(query string) {
	// This method is no longer needed as hierarchy is removed
}

// createOnDemandConnection creates a connection to a specific server when needed in cache-only mode
func (a *Agent) createOnDemandConnection(ctx context.Context, serverName string) (mcpclient.ClientInterface, error) {
	logger := getLogger(a)
	startTime := time.Now()
	logger.Infof("[ON-DEMAND CONNECTION] Creating connection for server: %s", serverName)

	// Add a shorter timeout for on-demand connections (3 minutes instead of 10)
	// This prevents hanging for too long and provides faster feedback
	connectTimeout := 3 * time.Minute
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	// Start a goroutine to log progress and timeout warnings
	progressDone := make(chan bool, 1)
	go func() {
		ticker := time.NewTicker(30 * time.Second) // Log every 30 seconds
		defer ticker.Stop()

		elapsed := time.Since(startTime)
		for {
			select {
			case <-ticker.C:
				elapsed = time.Since(startTime)
				remaining := connectTimeout - elapsed
				if remaining > 0 {
					logger.Infof("[ON-DEMAND CONNECTION] Still connecting to %s... (elapsed: %v, remaining: %v)",
						serverName, elapsed.Round(time.Second), remaining.Round(time.Second))
				} else {
					logger.Warnf("[ON-DEMAND CONNECTION] Connection to %s has exceeded timeout (%v)", serverName, connectTimeout)
				}
			case <-connectCtx.Done():
				return
			case <-progressDone:
				return
			}
		}
	}()

	// Load the merged config to get server details
	logger.Infof("[ON-DEMAND CONNECTION] Loading config for server: %s", serverName)
	config, err := mcpclient.LoadMergedConfig(a.configPath, logger)
	if err != nil {
		progressDone <- true
		return nil, fmt.Errorf("failed to load merged config for on-demand connection: %w", err)
	}

	serverConfig, exists := config.MCPServers[serverName]
	if !exists {
		progressDone <- true
		return nil, fmt.Errorf("server %s not found in config", serverName)
	}

	logger.Infof("[ON-DEMAND CONNECTION] Server config loaded: command=%s, args=%v, protocol=%s",
		serverConfig.Command, serverConfig.Args, serverConfig.Protocol)

	// Create a new client for this specific server
	logger.Infof("[ON-DEMAND CONNECTION] Creating MCP client for server: %s", serverName)
	client := mcpclient.New(mcpclient.MCPServerConfig{
		Command:  serverConfig.Command,
		Args:     serverConfig.Args,
		URL:      serverConfig.URL,
		Protocol: serverConfig.Protocol,
		Env:      serverConfig.Env, // Include environment variables
	}, logger)

	// Connect to the server with timeout context
	logger.Infof("[ON-DEMAND CONNECTION] Attempting to connect to server: %s (timeout: %v)", serverName, connectTimeout)
	connectStartTime := time.Now()
	if err := client.Connect(connectCtx); err != nil {
		progressDone <- true
		connectDuration := time.Since(connectStartTime)

		// Check if it was a timeout
		if connectCtx.Err() == context.DeadlineExceeded {
			logger.Errorf("[ON-DEMAND CONNECTION] Connection to %s timed out after %v: %v",
				serverName, connectDuration, err)
			return nil, fmt.Errorf("connection to server %s timed out after %v: %w",
				serverName, connectTimeout, err)
		}

		logger.Errorf("[ON-DEMAND CONNECTION] Failed to connect to server %s after %v: %v",
			serverName, connectDuration, err)
		return nil, fmt.Errorf("failed to connect to server %s: %w", serverName, err)
	}

	progressDone <- true
	connectDuration := time.Since(connectStartTime)
	totalDuration := time.Since(startTime)
	logger.Infof("[ON-DEMAND CONNECTION] Successfully connected to server: %s (connect_time: %v, total_time: %v)",
		serverName, connectDuration.Round(time.Millisecond), totalDuration.Round(time.Millisecond))
	return client, nil
}

// StartAgentSession creates a new agent-level event tree
func (a *Agent) StartAgentSession(ctx context.Context) {
	// Emit agent start event to create hierarchy
	agentStartEvent := events.NewAgentStartEvent(string(a.AgentMode), a.ModelID, string(a.provider))
	a.EmitTypedEvent(ctx, agentStartEvent)
}

// StartTurn creates a new turn-level event tree
func (a *Agent) StartTurn(ctx context.Context, turn int) {
	// Emit conversation turn event (this is already being emitted in conversation.go)
	// This method is kept for consistency but the actual turn event is emitted in AskWithHistory
}

// StartLLMGeneration creates a new LLM-level event tree
func (a *Agent) StartLLMGeneration(ctx context.Context) {
	// Emit LLM generation start event to create hierarchy
	llmStartEvent := events.NewLLMGenerationStartEvent(0, a.ModelID, a.Temperature, len(a.filteredTools), 0)
	a.EmitTypedEvent(ctx, llmStartEvent)
}

// extractCacheTokens extracts all cache-related tokens from GenerationInfo
// Supports multiple providers: OpenAI (CachedContentTokens), Anthropic (CacheReadInputTokens, CacheCreationInputTokens)
func extractCacheTokens(generationInfo *llmtypes.GenerationInfo) int {
	if generationInfo == nil {
		return 0
	}

	totalCacheTokens := 0

	// Check CachedContentTokens (OpenAI, Gemini)
	if generationInfo.CachedContentTokens != nil {
		totalCacheTokens += *generationInfo.CachedContentTokens
	}

	// Check Additional map for Anthropic cache tokens
	if generationInfo.Additional != nil {
		// CacheReadInputTokens (tokens read from cache)
		if cacheRead, ok := generationInfo.Additional["CacheReadInputTokens"]; ok {
			if cacheReadInt, ok := cacheRead.(int); ok {
				totalCacheTokens += cacheReadInt
			} else if cacheReadFloat, ok := cacheRead.(float64); ok {
				totalCacheTokens += int(cacheReadFloat)
			}
		}
		// Also check lowercase variant
		if cacheRead, ok := generationInfo.Additional["cache_read_input_tokens"]; ok {
			if cacheReadInt, ok := cacheRead.(int); ok {
				totalCacheTokens += cacheReadInt
			} else if cacheReadFloat, ok := cacheRead.(float64); ok {
				totalCacheTokens += int(cacheReadFloat)
			}
		}

		// CacheCreationInputTokens (tokens used to create cache)
		if cacheCreate, ok := generationInfo.Additional["CacheCreationInputTokens"]; ok {
			if cacheCreateInt, ok := cacheCreate.(int); ok {
				totalCacheTokens += cacheCreateInt
			} else if cacheCreateFloat, ok := cacheCreate.(float64); ok {
				totalCacheTokens += int(cacheCreateFloat)
			}
		}
		// Also check lowercase variant
		if cacheCreate, ok := generationInfo.Additional["cache_creation_input_tokens"]; ok {
			if cacheCreateInt, ok := cacheCreate.(int); ok {
				totalCacheTokens += cacheCreateInt
			} else if cacheCreateFloat, ok := cacheCreate.(float64); ok {
				totalCacheTokens += int(cacheCreateFloat)
			}
		}
	}

	return totalCacheTokens
}

// accumulateTokenUsage accumulates token usage from an LLM call
func (a *Agent) accumulateTokenUsage(ctx context.Context, usageMetrics events.UsageMetrics, generationInfo *llmtypes.GenerationInfo, turn int) {
	a.tokenTrackingMutex.Lock()
	defer a.tokenTrackingMutex.Unlock()

	// Extract cache tokens
	cacheTokens := extractCacheTokens(generationInfo)

	// Extract reasoning tokens
	reasoningTokens := 0
	if generationInfo != nil && generationInfo.ReasoningTokens != nil {
		reasoningTokens = *generationInfo.ReasoningTokens
	}

	// Extract cache discount
	cacheDiscount := 0.0
	if generationInfo != nil && generationInfo.CacheDiscount != nil {
		cacheDiscount = *generationInfo.CacheDiscount
	}

	// Accumulate tokens
	a.cumulativePromptTokens += usageMetrics.PromptTokens
	a.cumulativeCompletionTokens += usageMetrics.CompletionTokens
	a.cumulativeTotalTokens += usageMetrics.TotalTokens
	a.cumulativeCacheTokens += cacheTokens
	a.cumulativeReasoningTokens += reasoningTokens
	a.cumulativeCacheDiscount += cacheDiscount
	a.llmCallCount++

	if cacheTokens > 0 {
		a.cacheEnabledCallCount++
	}

	// Detailed logging of tokens received from LLM provider
	logger := getLogger(a)
	logger.Infof("📊 [TOKEN TRACKING] Turn %d - Tokens from LLM Provider:", turn)
	logger.Infof("   Prompt/Input: %d, Completion/Output: %d, Total: %d",
		usageMetrics.PromptTokens, usageMetrics.CompletionTokens, usageMetrics.TotalTokens)

	if cacheTokens > 0 {
		logger.Infof("   Cache Tokens: %d", cacheTokens)
		// Log breakdown of cache tokens
		if generationInfo != nil {
			if generationInfo.CachedContentTokens != nil {
				logger.Infof("      - CachedContentTokens: %d", *generationInfo.CachedContentTokens)
			}
			if generationInfo.Additional != nil {
				if cacheRead, ok := generationInfo.Additional["CacheReadInputTokens"]; ok {
					logger.Infof("      - CacheReadInputTokens: %v", cacheRead)
				}
				if cacheCreate, ok := generationInfo.Additional["CacheCreationInputTokens"]; ok {
					logger.Infof("      - CacheCreationInputTokens: %v", cacheCreate)
				}
			}
		}
	}

	if reasoningTokens > 0 {
		logger.Infof("   Reasoning Tokens: %d", reasoningTokens)
	}

	if cacheDiscount > 0 {
		logger.Infof("   Cache Discount: %.2f%%", cacheDiscount*100)
	}

	// Log cumulative totals
	logger.Infof("📊 [TOKEN TRACKING] Cumulative Totals (after turn %d):", turn)
	logger.Infof("   Prompt: %d, Completion: %d, Total: %d",
		a.cumulativePromptTokens, a.cumulativeCompletionTokens, a.cumulativeTotalTokens)
	logger.Infof("   Cache: %d, Reasoning: %d, LLM Calls: %d, Cache-Enabled Calls: %d",
		a.cumulativeCacheTokens, a.cumulativeReasoningTokens, a.llmCallCount, a.cacheEnabledCallCount)
}

// EndLLMGeneration ends the current LLM generation
func (a *Agent) EndLLMGeneration(ctx context.Context, result string, turn int, toolCalls int, duration time.Duration, usageMetrics events.UsageMetrics, generationInfo *llmtypes.GenerationInfo) {
	// Accumulate token usage (including cache tokens)
	a.accumulateTokenUsage(ctx, usageMetrics, generationInfo, turn)

	// Extract cache and reasoning tokens to include in UsageMetrics
	cacheTokens := extractCacheTokens(generationInfo)
	reasoningTokens := 0
	if generationInfo != nil && generationInfo.ReasoningTokens != nil {
		reasoningTokens = *generationInfo.ReasoningTokens
	}

	// Add cache and reasoning tokens to usage metrics
	usageMetrics.CacheTokens = cacheTokens
	usageMetrics.ReasoningTokens = reasoningTokens

	// Emit LLM generation end event with complete token information
	llmEndEvent := events.NewLLMGenerationEndEvent(turn, result, toolCalls, duration, usageMetrics)
	a.EmitTypedEvent(ctx, llmEndEvent)
}

// EndTurn ends the current turn
func (a *Agent) EndTurn(ctx context.Context) {
	// This method is no longer needed as hierarchy is removed
}

// emitTotalTokenUsageEvent emits a total token usage event with all cumulative metrics
func (a *Agent) emitTotalTokenUsageEvent(ctx context.Context, conversationDuration time.Duration) {
	a.tokenTrackingMutex.RLock()
	defer a.tokenTrackingMutex.RUnlock()

	// Create generation info map with cumulative cache information
	generationInfo := make(map[string]interface{})
	generationInfo["cumulative_prompt_tokens"] = a.cumulativePromptTokens
	generationInfo["cumulative_completion_tokens"] = a.cumulativeCompletionTokens
	generationInfo["cumulative_total_tokens"] = a.cumulativeTotalTokens
	generationInfo["cumulative_cache_tokens"] = a.cumulativeCacheTokens
	generationInfo["cumulative_reasoning_tokens"] = a.cumulativeReasoningTokens
	generationInfo["llm_call_count"] = a.llmCallCount
	generationInfo["cache_enabled_call_count"] = a.cacheEnabledCallCount

	// Emit total token usage event
	totalTokenEvent := events.NewTokenUsageEventWithCache(
		0, // turn (this is a summary event, not tied to a specific turn)
		"conversation_total",
		a.ModelID,
		string(a.provider),
		a.cumulativePromptTokens,
		a.cumulativeCompletionTokens,
		a.cumulativeTotalTokens,
		conversationDuration,
		"conversation_total",
		0.0, // cache discount removed
		a.cumulativeReasoningTokens,
		generationInfo,
	)

	a.EmitTypedEvent(ctx, totalTokenEvent)

	// Log total token usage summary
	logger := getLogger(a)
	logger.Infof("📊 [TOKEN TRACKING] ===== CONVERSATION TOTAL TOKEN USAGE =====")
	logger.Infof("   Prompt/Input Tokens: %d", a.cumulativePromptTokens)
	logger.Infof("   Completion/Output Tokens: %d", a.cumulativeCompletionTokens)
	logger.Infof("   Total Tokens: %d", a.cumulativeTotalTokens)
	logger.Infof("   Cache Tokens: %d", a.cumulativeCacheTokens)
	logger.Infof("   Reasoning Tokens: %d", a.cumulativeReasoningTokens)
	logger.Infof("   LLM Calls: %d", a.llmCallCount)
	logger.Infof("   Cache-Enabled Calls: %d", a.cacheEnabledCallCount)
	logger.Infof("   Conversation Duration: %v", conversationDuration)
	logger.Infof("============================================================")
}

// GetTokenUsage returns the current cumulative token usage metrics
func (a *Agent) GetTokenUsage() (promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount int) {
	a.tokenTrackingMutex.RLock()
	defer a.tokenTrackingMutex.RUnlock()

	promptTokens = a.cumulativePromptTokens
	completionTokens = a.cumulativeCompletionTokens
	totalTokens = a.cumulativeTotalTokens
	cacheTokens = a.cumulativeCacheTokens
	reasoningTokens = a.cumulativeReasoningTokens
	llmCallCount = a.llmCallCount
	cacheEnabledCallCount = a.cacheEnabledCallCount
	return
}

// EndAgentSession ends the current agent session
func (a *Agent) EndAgentSession(ctx context.Context, conversationDuration time.Duration) {
	// Emit total token usage event before agent end event
	a.emitTotalTokenUsageEvent(ctx, conversationDuration)

	// Read cumulative token metrics for agent_end event
	promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount := a.GetTokenUsage()

	// Emit agent end event with token usage information
	agentEndEvent := events.NewAgentEndEventWithTokens(
		string(a.AgentMode),
		true,
		"",
		promptTokens,
		completionTokens,
		totalTokens,
		cacheTokens,
		reasoningTokens,
		llmCallCount,
		cacheEnabledCallCount,
	)
	a.EmitTypedEvent(ctx, agentEndEvent)
}

// RebuildSystemPromptWithFilteredServers rebuilds the system prompt with only prompts/resources from relevant servers
func (a *Agent) RebuildSystemPromptWithFilteredServers(ctx context.Context, relevantServers []string) error {
	logger := a.Logger
	logger.Info("🔄 Rebuilding system prompt with filtered servers", map[string]interface{}{
		"relevant_servers": relevantServers,
		"total_servers":    len(a.Clients),
	})

	// Get fresh prompts and resources from unified cache using simple server names
	filteredPrompts := make(map[string][]mcp.Prompt)
	filteredResources := make(map[string][]mcp.Resource)

	// Load MCP configuration to get server configs for cache keys
	config, err := mcpclient.LoadMergedConfig(a.configPath, logger)
	if err != nil {
		logger.Warnf("Failed to load MCP config for cache lookup: %w", err)
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// Get cache manager
	cacheManager := mcpcache.GetCacheManager(logger)

	for _, serverName := range relevantServers {
		// Get server configuration for this server
		serverConfig, exists := config.MCPServers[serverName]
		if !exists {
			logger.Warnf("Server configuration not found for %s, skipping cache lookup", serverName)
			continue
		}

		// Generate configuration-aware cache key
		cacheKey := mcpcache.GenerateUnifiedCacheKey(serverName, serverConfig)

		// Try to get cached data
		cachedEntry, found := cacheManager.Get(cacheKey)
		if !found {
			logger.Debugf("Cache miss for server %s", serverName)
			continue
		}

		if cachedEntry != nil && cachedEntry.IsValid {
			logger.Infof("✅ Cache hit for server %s - using cached prompts and resources", serverName)

			// Add cached prompts and resources to filtered collections
			if len(cachedEntry.Prompts) > 0 {
				filteredPrompts[serverName] = cachedEntry.Prompts
			}
			if len(cachedEntry.Resources) > 0 {
				filteredResources[serverName] = cachedEntry.Resources
			}
		} else {
			logger.Debugf("Cache miss or invalid entry for server %s", serverName)
		}
	}

	// Rebuild system prompt with filtered data
	newSystemPrompt := prompt.BuildSystemPromptWithoutTools(
		filteredPrompts,
		filteredResources,
		string(a.AgentMode),
		a.DiscoverResource,
		a.DiscoverPrompt,
		a.Logger,
	)

	// Update the agent's system prompt
	a.SystemPrompt = newSystemPrompt

	logger.Info("✅ System prompt rebuilt with filtered servers", map[string]interface{}{
		"filtered_prompts_count":   len(filteredPrompts),
		"filtered_resources_count": len(filteredResources),
		"new_prompt_length":        len(newSystemPrompt),
	})

	return nil
}

// NewAgentWithObservability creates a new Agent with observability configuration
func NewAgentWithObservability(ctx context.Context, llm llmtypes.Model, serverName, configPath, modelID string, logger utils.ExtendedLogger, options ...AgentOption) (*Agent, error) {
	logger.Info("[MCP AGENT DEBUG] Reading merged config from", map[string]interface{}{"config_path": configPath})

	// Load merged MCP servers configuration (base + user)
	config, err := mcpclient.LoadMergedConfig(configPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load merged MCP config: %w", err)
	}

	logger.Info("[MCP AGENT DEBUG] Merged config contains servers", map[string]interface{}{"server_count": len(config.MCPServers)})
	for name := range config.MCPServers {
		logger.Info("[MCP AGENT DEBUG] Server found", map[string]interface{}{"server_name": name})
	}

	if llm == nil {
		return nil, fmt.Errorf("LLM cannot be nil")
	}

	// Create tracers - we always get at least a noop tracer
	baseTracer := observability.GetTracerWithLogger("noop", logger)

	// Create streaming tracer that wraps the base tracer
	streamingTracer := NewStreamingTracer(baseTracer, 100)

	// Create tracers array with streaming tracer
	tracers := []observability.Tracer{streamingTracer}

	// Generate a simple trace ID for this agent session
	traceID := observability.TraceID(fmt.Sprintf("agent-session-%s-%d", modelID, time.Now().UnixNano()))

	clients, toolToServer, allLLMTools, servers, prompts, resources, systemPrompt, err := NewAgentConnection(ctx, llm, serverName, configPath, string(traceID), tracers, logger, false) // Default CacheOnly = false for observability version
	if err != nil {
		return nil, err
	}

	// Use first client for legacy compatibility
	var firstClient mcpclient.ClientInterface
	if len(clients) > 0 {
		for _, c := range clients {
			firstClient = c
			break
		}
	}

	// Initialize tool output handler
	toolOutputHandler := utils.NewToolOutputHandler()

	// Large output handling is now done via virtual tools, not MCP server
	// Virtual tools are enabled by default and handle file operations directly
	toolOutputHandler.SetServerAvailable(true) // Always available with virtual tools

	// Set session ID for organizing files by conversation
	toolOutputHandler.SetSessionID(string(traceID))

	// Debug logging for virtual tools availability (observability version)
	// Use the logger we created earlier
	logger.Infof("🔍 Large output handling via virtual tools (observability) - virtual_tools_enabled: %v, total_clients: %d, client_names: %v", true, len(clients), getClientNames(clients))

	ag := &Agent{
		Client:                        firstClient,
		Clients:                       clients,
		toolToServer:                  toolToServer,
		LLM:                           llm,
		Tracers:                       tracers, // Support multiple tracers
		Tools:                         allLLMTools,
		MaxTurns:                      GetDefaultMaxTurns(SimpleAgent), // Default to simple mode
		Temperature:                   0.2,                             // Default temperature
		ToolChoice:                    "auto",                          // Default tool choice
		ModelID:                       modelID,
		SystemPrompt:                  systemPrompt,
		TraceID:                       traceID,
		servers:                       servers,
		provider:                      "", // Will be set by caller
		toolOutputHandler:             toolOutputHandler,
		EnableLargeOutputVirtualTools: true, // Default to enabled
		prompts:                       prompts,
		resources:                     resources,
		Logger:                        logger,                      // Set the logger on the agent
		customTools:                   make(map[string]CustomTool), // Initialize custom tools map
	}

	// Apply all options
	for _, option := range options {
		option(ag)
	}

	// No more event listeners - events go directly to tracer
	// Tracing is handled by the tracer itself based on TRACING_PROVIDER

	// Agent initialization complete

	return ag, nil
}

// Convenience constructors for common use cases
func NewSimpleAgent(ctx context.Context, llm llmtypes.Model, serverName, configPath, modelID string, tracer observability.Tracer, traceID observability.TraceID, logger utils.ExtendedLogger, options ...AgentOption) (*Agent, error) {
	return NewAgent(ctx, llm, serverName, configPath, modelID, tracer, traceID, logger, append(options, WithMode(SimpleAgent))...)
}

// Legacy constructors have been removed to enforce proper logger usage
// Use NewAgent or NewSimpleAgent with functional options instead

// AddEventListener and EmitEvent methods have been removed - events now go directly to tracers

// AddEventListener adds an event listener to the agent
func (a *Agent) AddEventListener(listener AgentEventListener) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.listeners == nil {
		a.listeners = make([]AgentEventListener, 0)
	}
	a.listeners = append(a.listeners, listener)

	// 🆕 NEW: Enable streaming tracer when event listeners are added
	// This provides streaming capabilities to external systems
	if _, hasStreaming := a.GetStreamingTracer(); hasStreaming {
		a.Logger.Infof("🔍 Streaming tracer enabled for event listener: %s", listener.Name())

		// The streaming tracer is already active and will forward events to all listeners
		// No additional setup needed - events automatically flow through the streaming system
	} else {
		a.Logger.Warnf("Streaming tracer not available, using traditional event listener system")
	}
}

// RemoveEventListener removes an event listener from the agent
func (a *Agent) RemoveEventListener(listener AgentEventListener) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for i, l := range a.listeners {
		if l == listener {
			a.listeners = append(a.listeners[:i], a.listeners[i+1:]...)
			break
		}
	}
}

// initializeHierarchyForContext sets the initial hierarchy level based on calling context
func (a *Agent) initializeHierarchyForContext(ctx context.Context) {
	// ✅ SIMPLIFIED APPROACH: Detect context by checking stack trace or other indicators

	// Check if we're in orchestrator context by looking for orchestrator-related context values
	if orchestratorID := ctx.Value("orchestrator_id"); orchestratorID != nil {
		// Orchestrator context: Start at level 2 (orchestrator_start -> orchestrator_agent_start -> system_prompt)
		a.currentHierarchyLevel = 2
		a.currentParentEventID = fmt.Sprintf("orchestrator_agent_start_%d", time.Now().UnixNano())
		return
	}

	// Check if we're in server context (HTTP API call) by looking for session-related context values
	if sessionID := ctx.Value("session_id"); sessionID != nil {
		// Server context: Start at level 0 (system_prompt is root)
		a.currentHierarchyLevel = 0
		a.currentParentEventID = ""
		return
	}

	// ✅ FALLBACK: Always start at level 0 for now
	// This ensures consistent behavior until we implement proper context detection
	a.currentHierarchyLevel = 0
	a.currentParentEventID = ""
}

// EmitTypedEvent sends a typed event to all tracers AND all listeners
func (a *Agent) EmitTypedEvent(ctx context.Context, eventData events.EventData) {

	// ✅ SET HIERARCHY FIELDS ON EVENT DATA FIRST (SINGLE SOURCE OF TRUTH)
	// Use interface-based approach - works for ALL event types that embed BaseEventData
	if baseEventData, ok := eventData.(interface {
		SetHierarchyFields(string, int, string, string)
	}); ok {
		baseEventData.SetHierarchyFields(a.currentParentEventID, a.currentHierarchyLevel, string(a.TraceID), events.GetComponentFromEventType(eventData.GetEventType()))
	}

	// Create event with correlation ID for start/end event pairs
	event := events.NewAgentEvent(eventData)
	event.TraceID = string(a.TraceID)

	// Generate a unique SpanID for this event
	event.SpanID = fmt.Sprintf("span_%s_%d", string(eventData.GetEventType()), time.Now().UnixNano())

	// ✅ COPY HIERARCHY FIELDS FROM EVENT DATA TO WRAPPER (SINGLE SOURCE OF TRUTH)
	// Get hierarchy fields from the event data (which we just set above)
	// Use interface to access BaseEventData fields from any event type
	if baseEventData, ok := eventData.(interface{ GetBaseEventData() *events.BaseEventData }); ok {
		baseData := baseEventData.GetBaseEventData()
		event.ParentID = baseData.ParentID
		event.HierarchyLevel = baseData.HierarchyLevel
		event.SessionID = baseData.SessionID
		event.Component = baseData.Component
	}

	// Update hierarchy for next event based on event type
	eventType := events.EventType(eventData.GetEventType())

	if events.IsStartEvent(eventType) {
		// ✅ SPECIAL HANDLING: conversation_turn should reset to level 2 (child of conversation_start)
		if eventType == events.ConversationTurn {
			a.currentHierarchyLevel = 2 // Reset to level 2 for new conversation turn
			a.currentParentEventID = event.SpanID
		} else if eventType == events.ToolCallStart {
			// ✅ SPECIAL HANDLING: tool_call_start should be sibling of llm_generation_end
			// Don't increment level - use current level (same as llm_generation_end)
			a.currentParentEventID = event.SpanID
		} else {
			// ✅ FIX: Increment level FIRST, then use it for next event
			a.currentHierarchyLevel++
			a.currentParentEventID = event.SpanID
		}
	} else if events.IsEndEvent(eventType) {
		if eventType == events.ToolCallEnd {
			// ✅ SPECIAL HANDLING: tool_call_end should be sibling of tool_call_start
			// Don't change level - use same level as tool_call_start
			// Level remains unchanged
		} else {
			// ✅ FIX: Don't decrement level immediately - let the next start event handle it
			// This allows token_usage and tool_call_start to be siblings of llm_generation_end
			// Level remains unchanged
		}
	}

	// Add correlation ID for start/end event pairs
	if isStartOrEndEvent(events.EventType(eventData.GetEventType())) {
		event.CorrelationID = fmt.Sprintf("%s_%d", string(eventData.GetEventType()), time.Now().UnixNano())
	}

	// Send to all tracers (multiple tracer support)
	// The streaming tracer will automatically forward events to subscribers
	for _, tracer := range a.Tracers {
		if err := tracer.EmitEvent(event); err != nil {
			a.Logger.Warnf("Failed to emit event to tracer %T: %v", tracer, err)
		}
	}

	// ALSO send to all event listeners for backward compatibility
	// This ensures existing code continues to work while streaming is available
	a.mu.RLock()
	listeners := make([]AgentEventListener, len(a.listeners))
	copy(listeners, a.listeners)
	a.mu.RUnlock()

	for _, listener := range listeners {
		if err := listener.HandleEvent(ctx, event); err != nil {
			a.Logger.Warnf("Failed to emit event to listener %T: %v", listener, err)
		}
	}
}

// isStartOrEndEvent checks if an event type is a start or end event that needs correlation ID
func isStartOrEndEvent(eventType events.EventType) bool {
	return eventType == events.ConversationStart || eventType == events.ConversationEnd ||
		eventType == events.LLMGenerationStart || eventType == events.LLMGenerationEnd ||
		eventType == events.ToolCallStart || eventType == events.ToolCallEnd
}

// GetPrimaryTracer returns the first tracer for backward compatibility
func (a *Agent) GetPrimaryTracer() observability.Tracer {
	if len(a.Tracers) > 0 {
		return a.Tracers[0]
	}
	return observability.NoopTracer{}
}

// GetStreamingTracer returns the streaming tracer if available
func (a *Agent) GetStreamingTracer() (StreamingTracer, bool) {
	if len(a.Tracers) > 0 {
		if streamingTracer, ok := a.Tracers[0].(StreamingTracer); ok {
			return streamingTracer, true
		}
	}
	return nil, false
}

// HasStreamingCapability returns true if the agent supports event streaming
func (a *Agent) HasStreamingCapability() bool {
	_, hasStreaming := a.GetStreamingTracer()
	return hasStreaming
}

// GetEventStream returns the event stream channel if streaming is available
func (a *Agent) GetEventStream() (<-chan *events.AgentEvent, bool) {
	if streamingTracer, hasStreaming := a.GetStreamingTracer(); hasStreaming {
		return streamingTracer.GetEventStream(), true
	}
	return nil, false
}

// SubscribeToEvents allows external systems to subscribe to agent events
func (a *Agent) SubscribeToEvents(ctx context.Context) (<-chan *events.AgentEvent, func(), bool) {
	if streamingTracer, hasStreaming := a.GetStreamingTracer(); hasStreaming {
		eventChan, unsubscribe := streamingTracer.SubscribeToEvents(ctx)
		return eventChan, unsubscribe, true
	}
	return nil, func() {}, false
}

// getClientNames returns a list of client names for debugging
func getClientNames(clients map[string]mcpclient.ClientInterface) []string {
	names := make([]string, 0, len(clients))
	for name := range clients {
		names = append(names, name)
	}
	return names
}

// Close closes all underlying MCP client connections.
func (a *Agent) Close() {
	// Close all clients in the map
	for serverName, client := range a.Clients {
		if client != nil {
			a.Logger.Info("🔌 Closing connection to %s", map[string]interface{}{"server_name": serverName})
			client.Close()
		}
	}

	// Legacy single client cleanup (may be redundant but safe)
	if a.Client != nil {
		a.Client.Close()
	}
}

// CheckConnectionHealth performs health checks on all MCP connections
func (a *Agent) CheckConnectionHealth(ctx context.Context) map[string]error {
	healthResults := make(map[string]error)

	for serverName, client := range a.Clients {
		if client == nil {
			healthResults[serverName] = fmt.Errorf("client is nil")
			continue
		}

		// Check if connection is active by trying to list tools
		_, err := client.ListTools(ctx)
		if err != nil {
			healthResults[serverName] = fmt.Errorf("connection health check failed: %w", err)
		}
	}

	return healthResults
}

// GetConnectionStats returns statistics about all MCP connections
func (a *Agent) GetConnectionStats() map[string]interface{} {
	stats := make(map[string]interface{})

	totalConnections := 0
	healthyConnections := 0
	activeServers := make([]string, 0)

	for serverName, client := range a.Clients {
		if client != nil {
			totalConnections++
			// Check if connection is healthy by trying to list tools
			_, err := client.ListTools(context.Background())
			if err == nil {
				healthyConnections++
				activeServers = append(activeServers, serverName)
			}
		}
	}

	stats["total_connections"] = totalConnections
	stats["healthy_connections"] = healthyConnections
	stats["active_servers"] = activeServers
	if totalConnections > 0 {
		stats["health_ratio"] = float64(healthyConnections) / float64(totalConnections)
	} else {
		stats["health_ratio"] = 0.0
	}

	return stats
}

// Ask runs a single-question interaction with possible tool calls and returns the final answer.
// Delegates to AskWithHistory with a single message
func (a *Agent) Ask(ctx context.Context, question string) (string, error) {
	// Create a single user message for the question
	userMessage := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: question}},
	}

	// Call AskWithHistory with the single message
	answer, _, err := AskWithHistory(a, ctx, []llmtypes.MessageContent{userMessage})
	return answer, err
}

// AskWithHistory runs an interaction using the provided message history (multi-turn conversation).
// Delegates to conversation.go
func (a *Agent) AskWithHistory(ctx context.Context, messages []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return AskWithHistory(a, ctx, messages)
}

// AskStructured runs a single-question interaction and converts the result to structured output
func AskStructured[T any](a *Agent, ctx context.Context, question string, schema T, schemaString string) (T, error) {
	// Create a single user message for the question
	userMessage := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: question}},
	}

	// Call AskWithHistoryStructured with the single message
	answer, _, err := AskWithHistoryStructured(a, ctx, []llmtypes.MessageContent{userMessage}, schema, schemaString)
	return answer, err
}

// AskWithHistoryStructured runs an interaction using message history and converts the result to structured output
func AskWithHistoryStructured[T any](a *Agent, ctx context.Context, messages []llmtypes.MessageContent, schema T, schemaString string) (T, []llmtypes.MessageContent, error) {
	// First, get the text response using the existing method
	textResponse, updatedMessages, err := a.AskWithHistory(ctx, messages)
	if err != nil {
		var zero T
		return zero, updatedMessages, fmt.Errorf("failed to get text response: %w", err)
	}

	// Convert the text response to structured output
	structuredResult, err := ConvertToStructuredOutput(a, ctx, textResponse, schema, schemaString)
	if err != nil {
		var zero T
		return zero, updatedMessages, fmt.Errorf("failed to convert to structured output: %w", err)
	}

	return structuredResult, updatedMessages, nil
}

// AskWithHistoryStructuredViaTool runs an interaction using message history and extracts structured output
// from a dynamically registered tool call. The LLM can call the tool during conversation, and we extract
// the structured data from the tool call arguments after conversation completes.
func AskWithHistoryStructuredViaTool[T any](
	a *Agent,
	ctx context.Context,
	messages []llmtypes.MessageContent,
	toolName string,
	toolDescription string,
	schema string,
) (StructuredOutputResult[T], error) {
	// Parse schema string to get tool parameters
	toolParams, err := parseSchemaForToolParameters(schema)
	if err != nil {
		var zero StructuredOutputResult[T]
		return zero, fmt.Errorf("failed to parse schema for tool parameters: %w", err)
	}

	// Create a cancellable context to break conversation as soon as tool is called
	toolCalledCtx, cancelToolCalled := context.WithCancel(ctx)
	defer cancelToolCalled()

	// Channel to signal that tool was called (thread-safe)
	toolCalledChan := make(chan bool, 1)

	// Register custom tool dynamically
	// The execution function signals that tool was called and cancels the context to break immediately
	executionFunc := func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Signal that tool was called (non-blocking)
		select {
		case toolCalledChan <- true:
		default:
		}
		// Cancel the context to break the conversation immediately
		cancelToolCalled()
		// Return minimal message - we'll break immediately so this won't be processed
		return "", nil
	}

	a.RegisterCustomTool(toolName, toolDescription, toolParams, executionFunc)

	// Call existing AskWithHistory - will break as soon as tool is called
	textResponse, updatedMessages, err := a.AskWithHistory(toolCalledCtx, messages)

	// Check if tool was called (non-blocking check)
	toolCalled := false
	select {
	case <-toolCalledChan:
		toolCalled = true
	default:
	}

	// If tool was called, context cancellation is expected - we still need to extract structured output
	if toolCalled {
		// Scan messages for structured tool call (even if AskWithHistory returned error due to cancellation)
		structuredResult, found, extractErr := extractStructuredToolCall[T](updatedMessages, toolName)
		if extractErr != nil {
			var zero StructuredOutputResult[T]
			return zero, fmt.Errorf("tool was called but structured output extraction failed: %w", extractErr)
		}

		if found {
			// Structured tool was called - return structured result immediately
			return StructuredOutputResult[T]{
				HasStructuredOutput: true,
				StructuredResult:    structuredResult,
				TextResponse:        "",
				Messages:            updatedMessages,
			}, nil
		}

		// Tool was called but not found in messages - error
		var zero StructuredOutputResult[T]
		return zero, fmt.Errorf("tool was called but not found in messages")
	}

	// Tool was not called according to flag - but check messages anyway
	// (context cancellation might have happened even if tool was called)
	// Scan messages for structured tool call (in case it was called but flag wasn't set)
	structuredResult, found, extractErr := extractStructuredToolCall[T](updatedMessages, toolName)
	if extractErr != nil {
		var zero StructuredOutputResult[T]
		return zero, fmt.Errorf("failed to extract structured tool call: %w", extractErr)
	}

	if found {
		// Structured tool was called - return structured result (even if there was an error)
		return StructuredOutputResult[T]{
			HasStructuredOutput: true,
			StructuredResult:    structuredResult,
			TextResponse:        "",
			Messages:            updatedMessages,
		}, nil
	}

	// Tool was not found in messages - check if there was an error
	if err != nil {
		var zero StructuredOutputResult[T]
		return zero, fmt.Errorf("failed to get response from conversation: %w", err)
	}

	// Structured tool was not called - return text response (conversational input)
	return StructuredOutputResult[T]{
		HasStructuredOutput: false,
		StructuredResult:    structuredResult, // zero value
		TextResponse:        textResponse,
		Messages:            updatedMessages,
	}, nil
}

// StructuredOutputResult represents the result of AskWithHistoryStructuredViaTool
// It can contain either structured output (if tool was called) or text response (if tool was not called)
type StructuredOutputResult[T any] struct {
	HasStructuredOutput bool
	StructuredResult    T
	TextResponse        string
	Messages            []llmtypes.MessageContent
}

// parseSchemaForToolParameters parses a JSON schema string and extracts properties for tool parameters
func parseSchemaForToolParameters(schemaString string) (map[string]interface{}, error) {
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(schemaString), &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	// Extract properties - this becomes the tool parameters
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("schema missing 'properties' field or it's not an object")
	}

	// Build tool parameter schema with type "object"
	toolParams := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}

	// Add required fields if present
	if required, ok := schema["required"].([]interface{}); ok {
		toolParams["required"] = required
	}

	return toolParams, nil
}

// extractStructuredToolCall scans messages for tool calls matching the tool name and extracts structured data
func extractStructuredToolCall[T any](messages []llmtypes.MessageContent, toolName string) (T, bool, error) {
	var zero T

	// Scan messages in reverse order to find the last (most recent) tool call
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]

		// Only check AI messages (they contain tool calls)
		if msg.Role != llmtypes.ChatMessageTypeAI {
			continue
		}

		// Check each part for tool calls
		for _, part := range msg.Parts {
			if toolCall, ok := part.(llmtypes.ToolCall); ok {
				if toolCall.FunctionCall != nil && toolCall.FunctionCall.Name == toolName {
					// Found matching tool call - extract arguments
					argsJSON := toolCall.FunctionCall.Arguments
					if argsJSON == "" {
						return zero, false, fmt.Errorf("tool call '%s' has empty arguments", toolName)
					}

					// Parse JSON arguments into struct type T
					var result T
					if err := json.Unmarshal([]byte(argsJSON), &result); err != nil {
						return zero, false, fmt.Errorf("failed to parse tool call arguments: %w", err)
					}

					return result, true, nil
				}
			}
		}
	}

	return zero, false, nil
}

// GetServerNames returns the list of connected server names
func (a *Agent) GetServerNames() []string {
	return getClientNames(a.Clients)
}

// GetContext returns the agent's context for cancellation and lifecycle management
func (a *Agent) GetContext() context.Context {
	return a.ctx
}

// IsCancelled checks if the agent's context has been cancelled
func (a *Agent) IsCancelled() bool {
	return a.ctx.Err() != nil
}

// SetSystemPrompt sets a custom system prompt and marks it as custom to prevent overwriting
func (a *Agent) SetSystemPrompt(systemPrompt string) {
	a.SystemPrompt = systemPrompt
	a.hasCustomSystemPrompt = true
}

// AppendSystemPrompt appends additional content to the existing system prompt
func (a *Agent) AppendSystemPrompt(additionalPrompt string) {
	if additionalPrompt == "" {
		return
	}

	// Track the appended prompt for smart routing
	a.AppendedSystemPrompts = append(a.AppendedSystemPrompts, additionalPrompt)
	a.HasAppendedPrompts = true

	// Store original system prompt if this is the first append
	if a.OriginalSystemPrompt == "" {
		a.OriginalSystemPrompt = a.SystemPrompt
	}

	// If we already have a system prompt, append with separator
	if a.SystemPrompt != "" {
		a.SystemPrompt = a.SystemPrompt + "\n\n" + additionalPrompt
	} else {
		// If no existing system prompt, just set it
		a.SystemPrompt = additionalPrompt
	}

	// Mark as custom to prevent overwriting
	a.hasCustomSystemPrompt = true
}

// RegisterCustomTool registers a single custom tool with both schema and execution function
func (a *Agent) RegisterCustomTool(name string, description string, parameters map[string]interface{}, executionFunc func(ctx context.Context, args map[string]interface{}) (string, error)) {
	if a.customTools == nil {
		a.customTools = make(map[string]CustomTool)
	}

	// Create the tool definition
	tool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        name,
			Description: description,
			Parameters:  llmtypes.NewParameters(parameters),
		},
	}

	// Store both definition and execution function
	a.customTools[name] = CustomTool{
		Definition: tool,
		Execution:  executionFunc,
	}

	// Also add to the main Tools array so the LLM can see it
	a.Tools = append(a.Tools, tool)

	// 🔧 CRITICAL FIX: Also add to filteredTools if smart routing is active
	// This ensures custom tools are available even when smart routing is enabled
	a.filteredTools = append(a.filteredTools, tool)

	// Debug logging
	if a.Logger != nil {
		a.Logger.Infof("🔧 Registered custom tool: %s", name)
		a.Logger.Infof("🔧 Total custom tools registered: %d", len(a.customTools))
		a.Logger.Infof("🔧 Total tools in agent: %d", len(a.Tools))
		a.Logger.Infof("🔧 Total filtered tools: %d", len(a.filteredTools))
	}
}

// GetCustomTools returns the registered custom tools
func (a *Agent) GetCustomTools() map[string]CustomTool {
	return a.customTools
}

// GetAppendedSystemPrompts returns the list of appended system prompts
func (a *Agent) GetAppendedSystemPrompts() []string {
	return a.AppendedSystemPrompts
}

// HasAppendedSystemPrompts returns true if any system prompts were appended
func (a *Agent) HasAppendedSystemPrompts() bool {
	return a.HasAppendedPrompts
}

// GetAppendedPromptCount returns the number of appended system prompts
func (a *Agent) GetAppendedPromptCount() int {
	return len(a.AppendedSystemPrompts)
}

// GetAppendedPromptSummary returns a summary of appended prompts
func (a *Agent) GetAppendedPromptSummary() string {
	if !a.HasAppendedPrompts || len(a.AppendedSystemPrompts) == 0 {
		return ""
	}

	var summary strings.Builder
	for i, prompt := range a.AppendedSystemPrompts {
		if i > 0 {
			summary.WriteString("; ")
		}
		content := prompt
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		summary.WriteString(content)
	}
	return summary.String()
}
