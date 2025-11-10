package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcp-agent/agent_go/internal/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent/codeexec"
	"mcp-agent/agent_go/pkg/mcpagent/prompt"
	"mcp-agent/agent_go/pkg/mcpcache"
	"mcp-agent/agent_go/pkg/mcpcache/codegen"
	"mcp-agent/agent_go/pkg/mcpclient"
)

// CustomTool represents a custom tool with its definition and execution function
type CustomTool struct {
	Definition llmtypes.Tool
	Execution  func(ctx context.Context, args map[string]interface{}) (string, error)
	Category   string // Tool category (e.g., "workspace", "human", "virtual", "custom", etc.)
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

// WithCodeExecutionMode enables/disables code execution mode
// When enabled: Only virtual tools (discover_code_structure, discover_code_files, write_code) are added to LLM
// MCP tools are NOT added directly - LLM must use generated Go code via write_code
// When disabled (default): All MCP tools are added directly as LLM tools
func WithCodeExecutionMode(enabled bool) AgentOption {
	return func(a *Agent) {
		a.UseCodeExecutionMode = enabled
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

	// Resource discovery configuration
	DiscoverResource bool // If true, include resource details in system prompt (default: true)

	// Prompt discovery configuration
	DiscoverPrompt bool // If true, include prompt details in system prompt (default: true)

	// Code execution mode configuration
	// When enabled: Only virtual tools (discover_code_structure, discover_code_files, write_code) are added to LLM
	// MCP tools are NOT added directly - LLM must use generated Go code via write_code
	// When disabled (default): All MCP tools are added directly as LLM tools
	UseCodeExecutionMode bool

	// Cross-provider fallback configuration
	CrossProviderFallback *CrossProviderFallback // Cross-provider fallback configuration from frontend
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

	// Create agent with default values
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

		// Initialize resource discovery (default: true - include resources in system prompt)
		DiscoverResource: true,

		// Initialize prompt discovery (default: true - include prompts in system prompt)
		DiscoverPrompt: true,
	}

	// Apply all options
	for _, option := range options {
		option(ag)
	}

	// 🆕 DETAILED AGENT CONNECTION DEBUG LOGGING
	logger.Infof("🤖 [DEBUG] About to call NewAgentConnection - Time: %v", time.Now())
	logger.Infof("🤖 [DEBUG] NewAgentConnection params - ServerName: %s, ConfigPath: %s", serverName, configPath)
	logger.Infof("🤖 [DEBUG] LLM details - Provider: %T, Model: %v", llm, llm != nil)

	clients, toolToServer, allLLMTools, servers, prompts, resources, systemPrompt, err := NewAgentConnection(ctx, llm, serverName, configPath, string(traceID), tracers, logger)

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
	ag.SystemPrompt = systemPrompt
	ag.servers = servers
	ag.toolOutputHandler = toolOutputHandler
	ag.prompts = prompts
	ag.resources = resources
	ag.configPath = configPath

	// Set selectedServers based on serverName parameter if not already set via options
	// This ensures discover_code_structure filters correctly when a single server is specified
	if len(ag.selectedServers) == 0 && serverName != "" && serverName != "all" {
		// serverName was specified and selectedServers wasn't set via options
		// Use the servers list from NewAgentConnection (which already filtered based on serverName)
		ag.selectedServers = servers
		if logger != nil {
			logger.Infof("🔧 Set selectedServers from serverName parameter: %v", ag.selectedServers)
		}
	}

	// Auto-disable code execution mode if no MCP servers are available
	// Code execution mode only makes sense when there are MCP servers to generate code for
	if ag.UseCodeExecutionMode && (len(clients) == 0 || serverName == mcpclient.NoServers) {
		ag.UseCodeExecutionMode = false
		if logger != nil {
			logger.Infof("🔧 Code execution mode automatically disabled - no MCP servers available (code execution requires MCP servers)")
		}
	}

	// Handle code execution mode: filter out MCP tools and custom tools if enabled
	var toolsToUse []llmtypes.Tool
	if ag.UseCodeExecutionMode {
		// Code execution mode: Only include virtual tools (discover_code_structure, discover_code_files, write_code)
		// Exclude all MCP server tools and custom tools (they'll be accessed via generated code)
		logger.Infof("🔧 Code execution mode enabled - excluding MCP tools and custom tools from LLM (will use generated code)")

		// Build set of custom tool names for filtering
		customToolNames := make(map[string]bool)
		for toolName := range ag.customTools {
			customToolNames[toolName] = true
		}

		for _, tool := range allLLMTools {
			// Check if this tool is an MCP tool (exists in toolToServer)
			_, isMCPTool := toolToServer[tool.Function.Name]
			// Check if this tool is a custom tool
			isCustomTool := customToolNames[tool.Function.Name]

			// In code execution mode, exclude both MCP tools and custom tools
			// Only include virtual tools (which will be filtered later to only discover_code_structure, discover_code_files, and write_code)
			if !isMCPTool && !isCustomTool {
				// Not an MCP tool or custom tool - include it (virtual tools only)
				toolsToUse = append(toolsToUse, tool)
			}
		}
		logger.Infof("🔧 Code execution mode: %d tools available (only virtual tools, MCP and custom tools excluded)", len(toolsToUse))
	} else {
		// Normal mode: Use all tools
		toolsToUse = allLLMTools
	}

	ag.Tools = toolsToUse
	ag.filteredTools = toolsToUse

	// Apply selected tools filter if specified
	// Empty selectedTools array means "use all tools" (no filtering)
	// Non-empty selectedTools array means "use only these specific tools"
	// IMPORTANT: If a server is in selectedServers but has NO tools in selectedTools,
	// it means "use ALL tools from that server" (all tools mode for that server)
	if len(ag.selectedTools) > 0 {
		logger.Infof("🔧 Tool filtering active: %d specific tools selected", len(ag.selectedTools))

		// Create set for fast lookup of specific tools
		selectedToolSet := make(map[string]bool)
		for _, fullName := range ag.selectedTools {
			selectedToolSet[fullName] = true
		}

		// Build map of which servers have specific tools
		serversWithSpecificTools := make(map[string]bool)
		for _, fullName := range ag.selectedTools {
			// Parse "server:tool" format
			parts := strings.SplitN(fullName, ":", 2)
			if len(parts) == 2 {
				serversWithSpecificTools[parts[0]] = true
			}
		}

		// Filter tools: include specific tools OR all tools from servers without specific tools
		var filteredTools []llmtypes.Tool
		for _, tool := range toolsToUse {
			// Get server name for this tool
			serverName, exists := toolToServer[tool.Function.Name]
			if !exists {
				// Custom/virtual tool - always include
				filteredTools = append(filteredTools, tool)
				continue
			}

			// In code execution mode, MCP tools should already be filtered out
			// But if we're here, it means we're in normal mode with tool selection
			// Check if this server has specific tools selected
			hasSpecificTools := serversWithSpecificTools[serverName]

			if hasSpecificTools {
				// Server has specific tools - check if this tool is selected
				fullName := fmt.Sprintf("%s:%s", serverName, tool.Function.Name)
				if selectedToolSet[fullName] {
					filteredTools = append(filteredTools, tool)
				}
			} else {
				// Server has no specific tools - include ALL tools from this server
				// (this is "all tools" mode for this server)
				filteredTools = append(filteredTools, tool)
			}
		}

		logger.Infof("🔧 Tool filtering complete: %d tools selected from %d total", len(filteredTools), len(toolsToUse))
		ag.Tools = filteredTools
		ag.filteredTools = filteredTools
	} else {
		// No specific tools selected - use all available tools (already filtered by code execution mode if enabled)
		logger.Infof("🔧 Using all available tools: %d tools (no filtering applied)", len(toolsToUse))
		ag.Tools = toolsToUse
		ag.filteredTools = toolsToUse
	}

	// Initialize tool registry for code execution
	// Convert custom tools to executor functions
	customToolExecutors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))
	for name, customTool := range ag.customTools {
		customToolExecutors[name] = customTool.Execution
	}

	// Add virtual tools to the LLM tools list
	virtualTools := ag.CreateVirtualTools()

	// In code execution mode, only include discover_code_files and write_code (discover_code_structure removed)
	if ag.UseCodeExecutionMode {
		var filteredVirtualTools []llmtypes.Tool
		for _, tool := range virtualTools {
			if tool.Function != nil {
				toolName := tool.Function.Name
				// Only include code execution tools in code execution mode (discover_code_structure removed)
				if toolName == "discover_code_files" || toolName == "write_code" {
					filteredVirtualTools = append(filteredVirtualTools, tool)
				}
			}
		}
		virtualTools = filteredVirtualTools
		logger.Infof("🔧 Code execution mode: Filtered virtual tools - only discover_code_files and write_code available")
	}

	ag.Tools = append(ag.Tools, virtualTools...)

	// Convert virtual tools to executor functions
	// Note: We need to capture the tool name in the closure
	virtualToolExecutors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))
	for _, virtualTool := range virtualTools {
		if virtualTool.Function != nil {
			toolName := virtualTool.Function.Name
			// Create a closure that captures the tool name and agent reference
			virtualToolExecutors[toolName] = func(name string) func(ctx context.Context, args map[string]interface{}) (string, error) {
				return func(ctx context.Context, args map[string]interface{}) (string, error) {
					return ag.HandleVirtualTool(ctx, name, args)
				}
			}(toolName)
		}
	}

	// Initialize registry with virtual tools
	codeexec.InitRegistryWithVirtualTools(ag.Clients, customToolExecutors, virtualToolExecutors, ag.toolToServer, logger)

	// Generate Go code for virtual tools
	generatedDir := ag.getGeneratedDir()
	if err := codegen.GenerateVirtualToolsCode(virtualTools, generatedDir, logger); err != nil {
		if logger != nil {
			logger.Warnf("Failed to generate Go code for virtual tools: %v", err)
		}
		// Don't fail agent initialization if code generation fails
	}

	// In code execution mode, discover tool structure and include it in system prompt
	var toolStructureJSON string
	if ag.UseCodeExecutionMode {
		// Discover all available tools and include structure in system prompt
		toolStructure, err := ag.discoverAllServersAndTools(generatedDir)
		if err != nil {
			if logger != nil {
				logger.Warnf("Failed to discover tool structure for system prompt: %v", err)
			}
			// Continue without tool structure if discovery fails
		} else {
			toolStructureJSON = toolStructure
			if logger != nil {
				logger.Infof("✅ Discovered tool structure for system prompt (%d bytes)", len(toolStructureJSON))
			}
		}
	}

	// Always rebuild system prompt with the correct agent mode and tool structure
	// This ensures Simple agents get Simple prompts and ReAct agents get ReAct prompts
	// In code execution mode, tool structure is automatically included
	if !ag.hasCustomSystemPrompt {
		ag.SystemPrompt = prompt.BuildSystemPromptWithoutTools(ag.prompts, ag.resources, string(ag.AgentMode), ag.DiscoverResource, ag.DiscoverPrompt, ag.UseCodeExecutionMode, toolStructureJSON, ag.Logger)
	}

	// 🎯 SMART ROUTING INITIALIZATION - Run AFTER all tools are loaded (including virtual tools)
	// This ensures we have the complete tool count for accurate smart routing decisions
	logger.Infof("🎯 [DEBUG] Smart routing check - EnableSmartRouting: %v, shouldUseSmartRouting: %v", ag.EnableSmartRouting, ag.shouldUseSmartRouting())
	logger.Infof("🎯 [DEBUG] Smart routing context - Time: %v", time.Now())

	if ag.shouldUseSmartRouting() {
		// Get server count for logging
		serverCount := len(ag.Clients)
		serverType := "active"

		logger.Infof("🎯 Smart routing enabled - determining relevant tools after full initialization")
		logger.Infof("🎯 Total tools loaded: %d, %s servers: %d (thresholds: tools>%d, servers>%d)",
			len(ag.Tools), serverType, serverCount, ag.SmartRoutingThreshold.MaxTools, ag.SmartRoutingThreshold.MaxServers)

		// For now, use all tools since we don't have conversation context yet
		// Smart routing will be re-evaluated in AskWithHistory with full conversation context
		ag.filteredTools = ag.Tools
		logger.Infof("🎯 Smart routing will be applied during conversation with full context")
	} else {
		// Get server count for logging
		serverCount := len(ag.Clients)
		serverType := "active"
		logger.Infof("🔧 DEBUG: Active mode - Clients map has %d entries", serverCount)

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

// createOnDemandConnection creates a connection to a specific server when needed
func (a *Agent) createOnDemandConnection(ctx context.Context, serverName string) (mcpclient.ClientInterface, error) {
	logger := getLogger(a)
	logger.Infof("[ON-DEMAND CONNECTION] Creating connection for server: %s", serverName)

	// Load the merged config to get server details
	config, err := mcpclient.LoadMergedConfig(a.configPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load merged config for on-demand connection: %w", err)
	}

	serverConfig, exists := config.MCPServers[serverName]
	if !exists {
		return nil, fmt.Errorf("server %s not found in config", serverName)
	}

	// Create a new client for this specific server
	client := mcpclient.New(mcpclient.MCPServerConfig{
		Command:  serverConfig.Command,
		Args:     serverConfig.Args,
		URL:      serverConfig.URL,
		Protocol: serverConfig.Protocol,
		Env:      serverConfig.Env, // Include environment variables
	}, logger)

	// Connect to the server
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to server %s: %w", serverName, err)
	}

	logger.Infof("[ON-DEMAND CONNECTION] Successfully connected to server: %s", serverName)
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

// EndLLMGeneration ends the current LLM generation
func (a *Agent) EndLLMGeneration(ctx context.Context, result string, turn int, toolCalls int, duration time.Duration, usageMetrics events.UsageMetrics) {
	// Emit LLM generation end event to close hierarchy
	llmEndEvent := events.NewLLMGenerationEndEvent(turn, result, toolCalls, duration, usageMetrics)
	a.EmitTypedEvent(ctx, llmEndEvent)
}

// EndTurn ends the current turn
func (a *Agent) EndTurn(ctx context.Context) {
	// This method is no longer needed as hierarchy is removed
}

// EndAgentSession ends the current agent session
func (a *Agent) EndAgentSession(ctx context.Context) {
	// Emit agent end event to close hierarchy
	agentEndEvent := events.NewAgentEndEvent(string(a.AgentMode), true, "")
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
	// In code execution mode, rediscover tool structure after filtering
	var toolStructureJSON string
	if a.UseCodeExecutionMode {
		generatedDir := a.getGeneratedDir()
		toolStructure, err := a.discoverAllServersAndTools(generatedDir)
		if err != nil {
			if a.Logger != nil {
				a.Logger.Warnf("Failed to rediscover tool structure after filtering: %v", err)
			}
		} else {
			toolStructureJSON = toolStructure
		}
	}
	newSystemPrompt := prompt.BuildSystemPromptWithoutTools(
		filteredPrompts,
		filteredResources,
		string(a.AgentMode),
		a.DiscoverResource,
		a.DiscoverPrompt,
		a.UseCodeExecutionMode,
		toolStructureJSON,
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

	clients, toolToServer, allLLMTools, servers, prompts, resources, systemPrompt, err := NewAgentConnection(ctx, llm, serverName, configPath, string(traceID), tracers, logger)
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

	// Register with "structured_output" category so it's always available even in code execution mode
	a.RegisterCustomTool(toolName, toolDescription, toolParams, executionFunc, "structured_output")

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

	// Tool was not called - check if there was an error
	if err != nil {
		var zero StructuredOutputResult[T]
		return zero, fmt.Errorf("failed to get response from conversation: %w", err)
	}

	// Scan messages for structured tool call (in case it was called but flag wasn't set)
	structuredResult, found, extractErr := extractStructuredToolCall[T](updatedMessages, toolName)
	if extractErr != nil {
		var zero StructuredOutputResult[T]
		return zero, fmt.Errorf("failed to extract structured tool call: %w", extractErr)
	}

	if found {
		// Structured tool was called - return structured result
		return StructuredOutputResult[T]{
			HasStructuredOutput: true,
			StructuredResult:    structuredResult,
			TextResponse:        "",
			Messages:            updatedMessages,
		}, nil
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
// category is an optional parameter that specifies the tool's category (e.g., "workspace", "human", "virtual", "custom")
// If not provided or empty, defaults to "custom"
func (a *Agent) RegisterCustomTool(name string, description string, parameters map[string]interface{}, executionFunc func(ctx context.Context, args map[string]interface{}) (string, error), category ...string) {
	if a.customTools == nil {
		a.customTools = make(map[string]CustomTool)
	}

	// Determine category (default to "custom" if not provided)
	// This is a fallback default - actual categories should be passed from tool creation functions
	toolCategory := "custom"
	if len(category) > 0 && category[0] != "" {
		toolCategory = category[0]
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

	// Store both definition and execution function with category
	a.customTools[name] = CustomTool{
		Definition: tool,
		Execution:  executionFunc,
		Category:   toolCategory,
	}

	// In code execution mode, do NOT add custom tools to LLM tools list
	// They should only be accessible via generated Go code
	// EXCEPTION: Structured output tools (category "structured_output") must always be available
	// because they're orchestration/control tools, not regular MCP tools
	isStructuredOutputTool := toolCategory == "structured_output"

	if !a.UseCodeExecutionMode || isStructuredOutputTool {
		// Normal mode OR structured output tool: Add to the main Tools array so the LLM can see it
		a.Tools = append(a.Tools, tool)

		// 🔧 CRITICAL FIX: Also add to filteredTools if smart routing is active
		// This ensures custom tools are available even when smart routing is enabled
		a.filteredTools = append(a.filteredTools, tool)

		if a.UseCodeExecutionMode && isStructuredOutputTool {
			if a.Logger != nil {
				a.Logger.Debugf("🔧 Code execution mode: Structured output tool %s added to LLM tools (required for orchestration)", name)
			}
		}
	} else {
		// Code execution mode: Don't add to LLM tools, but still generate code and update registry
		if a.Logger != nil {
			a.Logger.Debugf("🔧 Code execution mode: Custom tool %s registered but not added to LLM tools (will use generated code)", name)
		}
	}

	// Generate Go code for custom tools
	generatedDir := a.getGeneratedDir()
	customToolsForCodeGen := make(map[string]codegen.CustomToolForCodeGen)
	for toolName, customTool := range a.customTools {
		customToolsForCodeGen[toolName] = codegen.CustomToolForCodeGen{
			Definition: customTool.Definition,
			Category:   customTool.Category, // Pass category to code generation
		}
	}
	if err := codegen.GenerateCustomToolsCode(customToolsForCodeGen, generatedDir, a.Logger); err != nil {
		if a.Logger != nil {
			a.Logger.Warnf("Failed to generate Go code for custom tools: %v", err)
		}
		// Don't fail tool registration if code generation fails
	}

	// Update registry with new custom tool
	if a.Clients != nil {
		customToolExecutors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))
		for toolName, customTool := range a.customTools {
			customToolExecutors[toolName] = customTool.Execution
		}
		if a.Logger != nil {
			a.Logger.Debugf("🔧 [CODE_EXECUTION] Updating registry with %d custom tools (including %s)", len(customToolExecutors), name)
			// Log all custom tool names for debugging
			toolNames := make([]string, 0, len(customToolExecutors))
			for toolName := range customToolExecutors {
				toolNames = append(toolNames, toolName)
			}
			a.Logger.Debugf("🔧 [CODE_EXECUTION] Custom tools in registry: %v", toolNames)
		}
		codeexec.InitRegistry(a.Clients, customToolExecutors, a.toolToServer, a.Logger)
		if a.Logger != nil {
			a.Logger.Debugf("🔧 [CODE_EXECUTION] Registry updated successfully for tool: %s", name)
		}
	} else {
		if a.Logger != nil {
			a.Logger.Warnf("⚠️ [CODE_EXECUTION] Cannot update registry - a.Clients is nil for tool: %s", name)
		}
	}

	// Debug logging
	if a.Logger != nil {
		a.Logger.Infof("🔧 Registered custom tool: %s (category: %s)", name, toolCategory)
		a.Logger.Infof("🔧 Total custom tools registered: %d", len(a.customTools))
		a.Logger.Infof("🔧 Total tools in agent: %d", len(a.Tools))
		a.Logger.Infof("🔧 Total filtered tools: %d", len(a.filteredTools))
	}
}

// GetCustomToolsByCategory returns all custom tools filtered by category
func (a *Agent) GetCustomToolsByCategory(category string) map[string]CustomTool {
	result := make(map[string]CustomTool)
	for name, tool := range a.customTools {
		if tool.Category == category {
			result[name] = tool
		}
	}
	return result
}

// GetCustomToolCategories returns a list of all unique categories for registered custom tools
func (a *Agent) GetCustomToolCategories() []string {
	categorySet := make(map[string]bool)
	for _, tool := range a.customTools {
		if tool.Category != "" {
			categorySet[tool.Category] = true
		}
	}

	categories := make([]string, 0, len(categorySet))
	for cat := range categorySet {
		categories = append(categories, cat)
	}
	return categories
}

// GetCustomTools returns the registered custom tools
func (a *Agent) GetCustomTools() map[string]CustomTool {
	return a.customTools
}

// UpdateCodeExecutionRegistry explicitly updates the code execution registry with all custom tools
// This is useful when tools are registered after agent initialization (e.g., workspace/human tools)
// It also rebuilds the system prompt to include the newly registered tools in the tool structure
func (a *Agent) UpdateCodeExecutionRegistry() error {
	if a.Clients == nil {
		if a.Logger != nil {
			a.Logger.Warnf("⚠️ [CODE_EXECUTION] Cannot update registry - a.Clients is nil")
		}
		return fmt.Errorf("cannot update registry: Clients is nil")
	}

	// Build custom tool executors map from all registered custom tools
	customToolExecutors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))
	for toolName, customTool := range a.customTools {
		customToolExecutors[toolName] = customTool.Execution
	}

	if a.Logger != nil {
		a.Logger.Infof("🔧 [CODE_EXECUTION] Explicitly updating registry with %d custom tools", len(customToolExecutors))
		// Log all custom tool names for debugging
		toolNames := make([]string, 0, len(customToolExecutors))
		for toolName := range customToolExecutors {
			toolNames = append(toolNames, toolName)
		}
		a.Logger.Debugf("🔧 [CODE_EXECUTION] Custom tools being registered: %v", toolNames)
	}

	// Update the registry
	codeexec.InitRegistry(a.Clients, customToolExecutors, a.toolToServer, a.Logger)

	if a.Logger != nil {
		a.Logger.Infof("✅ [CODE_EXECUTION] Registry updated successfully with %d custom tools", len(customToolExecutors))
	}

	// 🔧 CRITICAL: Rebuild system prompt with updated tool structure in code execution mode
	// This ensures workspace and human tools appear in the system prompt
	if a.UseCodeExecutionMode {
		if err := a.rebuildSystemPromptWithUpdatedToolStructure(); err != nil {
			if a.Logger != nil {
				a.Logger.Warnf("⚠️ [CODE_EXECUTION] Failed to rebuild system prompt with updated tool structure: %v", err)
			}
			// Don't fail registry update if system prompt rebuild fails
		} else {
			if a.Logger != nil {
				a.Logger.Infof("✅ [CODE_EXECUTION] System prompt rebuilt with updated tool structure (workspace and human tools now included)")
			}
		}
	}

	return nil
}

// rebuildSystemPromptWithUpdatedToolStructure rebuilds the system prompt with the latest tool structure
// This is called after custom tools are registered to ensure they appear in the system prompt
func (a *Agent) rebuildSystemPromptWithUpdatedToolStructure() error {
	if !a.UseCodeExecutionMode {
		return nil // Only needed in code execution mode
	}

	generatedDir := a.getGeneratedDir()
	toolStructure, err := a.discoverAllServersAndTools(generatedDir)
	if err != nil {
		return fmt.Errorf("failed to discover tool structure: %w", err)
	}

	// Rebuild system prompt with updated tool structure
	newSystemPrompt := prompt.BuildSystemPromptWithoutTools(
		a.prompts,
		a.resources,
		string(a.AgentMode),
		a.DiscoverResource,
		a.DiscoverPrompt,
		a.UseCodeExecutionMode,
		toolStructure,
		a.Logger,
	)

	// Update the agent's system prompt
	a.SystemPrompt = newSystemPrompt

	if a.Logger != nil {
		a.Logger.Debugf("🔧 [CODE_EXECUTION] System prompt rebuilt - length: %d bytes, tool structure: %d bytes", len(newSystemPrompt), len(toolStructure))
	}

	return nil
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

// getGeneratedDir returns the path to the generated/ directory
func (a *Agent) getGeneratedDir() string {
	// Use environment variable if set, otherwise default to agent_go/generated
	generatedDir := os.Getenv("MCP_GENERATED_DIR")
	if generatedDir == "" {
		// Default to agent_go/generated directory
		// Try to get absolute path to ensure we're in the right directory
		absPath, err := filepath.Abs("generated")
		if err == nil {
			generatedDir = absPath
		} else {
			// Fallback to relative path
			generatedDir = filepath.Join(".", "generated")
		}
	}
	// Ensure directory exists
	if err := os.MkdirAll(generatedDir, 0755); err != nil {
		if a.Logger != nil {
			a.Logger.Warnf("Failed to create generated directory %s: %v", generatedDir, err)
		}
	}
	return generatedDir
}
