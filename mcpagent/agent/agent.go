package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/manishiitg/mcpagent/agent/codeexec"
	"github.com/manishiitg/mcpagent/agent/prompt"
	"github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpcache"
	"github.com/manishiitg/mcpagent/mcpcache/codegen"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"
)

// CustomTool represents a custom tool with its definition and execution function
type CustomTool struct {
	Definition llmtypes.Tool
	Execution  func(ctx context.Context, args map[string]interface{}) (string, error)
	Category   string        // Tool category (e.g., "workspace", "human", "virtual", "custom", etc.)
	Timeout    time.Duration // Per-tool timeout. 0 = no timeout (tool runs indefinitely). -1 = use agent default.
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

// AgentOption defines a functional option for configuring an Agent.
// These options modify the Agent's state during initialization (NewAgent).
type AgentOption func(*Agent)

// WithMode sets the agent's operational mode.
//
// Supported modes:
//   - SimpleAgent: Standard tool-using agent (default).
//   - ReActAgent (if available): Reasoning + Acting loop.
//
// Default: SimpleAgent
func WithMode(mode AgentMode) AgentOption {
	return func(a *Agent) {
		a.AgentMode = mode
	}
}

// WithLogger sets a custom logger implementation.
//
// Allows injecting a specialized logger for structured logging or integrating
// with existing application loggers.
//
// Default: loggerv2.NewDefault() (Standard output logger)
func WithLogger(logger loggerv2.Logger) AgentOption {
	return func(a *Agent) {
		a.Logger = logger
	}
}

// WithTracer adds an observability tracer to the agent.
//
// The provided tracer will be wrapped in a StreamingTracer to support real-time
// event streaming. Multiple tracers can be added by calling this option multiple times.
//
// Parameters:
//   - tracer: The observability tracer implementation (e.g., Langfuse, Console, etc.).
//
// Default: No tracers (unless NewAgentWithObservability is used)
func WithTracer(tracer observability.Tracer) AgentOption {
	return func(a *Agent) {
		if tracer != nil {
			// Create streaming tracer that wraps the base tracer
			streamingTracer := NewStreamingTracer(tracer, 100)
			// Add to tracers slice
			a.Tracers = append(a.Tracers, streamingTracer)
		}
	}
}

// WithTraceID sets a specific Trace ID for the agent session.
//
// Useful for correlating agent activities with external systems or requests
// (e.g., setting the TraceID to match an incoming HTTP request ID).
//
// Default: Generated automatically (if using NewAgentWithObservability) or empty.
func WithTraceID(traceID observability.TraceID) AgentOption {
	return func(a *Agent) {
		a.TraceID = traceID
	}
}

// WithProvider explicitly sets the LLM provider name.
//
// This is primarily used for logging and tracking purposes, as the actual
// provider logic is encapsulated in the llmtypes.Model interface.
func WithProvider(provider llm.Provider) AgentOption {
	return func(a *Agent) {
		a.provider = provider
	}
}

// WithMaxTurns sets the maximum number of conversation turns allowed.
//
// A turn consists of one user message and one agent response (which may include multiple tool calls).
// This prevents infinite loops or excessive token usage.
//
// Parameters:
//   - maxTurns: The specific limit to set.
//
// Default: Value returned by GetDefaultMaxTurns(SimpleAgent)
func WithMaxTurns(maxTurns int) AgentOption {
	return func(a *Agent) {
		a.MaxTurns = maxTurns
	}
}

// WithTemperature sets the sampling temperature for the LLM.
//
// Higher values (e.g., 0.8) make output more random/creative.
// Lower values (e.g., 0.2) make output more focused/deterministic.
//
// Default: 0.0 (Deterministic)
func WithTemperature(temperature float64) AgentOption {
	return func(a *Agent) {
		a.Temperature = temperature
	}
}

// WithToolChoice forces a specific tool choice strategy.
//
// Parameters:
//   - toolChoice: "auto", "none", or a specific tool name (depending on provider support).
//
// Default: "auto"
func WithToolChoice(toolChoice string) AgentOption {
	return func(a *Agent) {
		a.ToolChoice = toolChoice
	}
}

// WithContextOffloading enables the "Context Offloading" pattern.
//
// When enabled, if a tool returns a massive output (exceeding LargeOutputThreshold),
// the agent will automatically save it to a file and provide the LLM with a "virtual tool"
// to read that file on demand, rather than flooding the context window.
//
// Default: true (Enabled)
func WithContextOffloading(enabled bool) AgentOption {
	return func(a *Agent) {
		a.EnableContextOffloading = enabled
	}
}

// WithLargeOutputThreshold sets the token count threshold for context offloading.
//
// Tool outputs larger than this value will be offloaded to the filesystem.
// The count is based on token estimate, not character count.
//
// Parameters:
//   - threshold: Token count limit.
//
// Default: 10,000 tokens
func WithLargeOutputThreshold(threshold int) AgentOption {
	return func(a *Agent) {
		a.LargeOutputThreshold = threshold
	}
}

// WithToolOutputRetentionPeriod sets the retention policy for offloaded tool output files.
//
// Files created by context offloading will be deleted if they are older than this duration.
// A periodic cleanup routine runs every hour to remove files older than the retention period.
//
// Parameters:
//   - retentionPeriod: Duration to keep files. Set to 0 to disable automatic cleanup.
//
// Default: 7 days (DefaultToolOutputRetentionPeriod). Periodic cleanup runs every hour.
func WithToolOutputRetentionPeriod(retentionPeriod time.Duration) AgentOption {
	return func(a *Agent) {
		a.ToolOutputRetentionPeriod = retentionPeriod
	}
}

// WithCleanupToolOutputOnSessionEnd configures immediate cleanup behavior.
//
// If enabled, all tool output files created during this session will be deleted
// when EndAgentSession is called.
//
// Default: false (Files persist for debugging or future reference)
func WithCleanupToolOutputOnSessionEnd(enabled bool) AgentOption {
	return func(a *Agent) {
		a.CleanupToolOutputOnSessionEnd = enabled
	}
}

// WithContextSummarization enables automatic conversation summarization.
//
// When the context window fills up (based on TokenThresholdPercent), the agent will
// summarize older messages to free up space while retaining context.
//
// Default: false (Disabled)
func WithContextSummarization(enabled bool) AgentOption {
	return func(a *Agent) {
		a.EnableContextSummarization = enabled
	}
}

// WithSummarizeOnTokenThreshold configures the trigger for summarization.
//
// Parameters:
//   - enabled: Whether to use token-based triggering.
//   - thresholdPercent: The percentage of the model's context window (0.0 - 1.0)
//     that triggers summarization.
//
// Default: 0.8 (80%) if enabled.
func WithSummarizeOnTokenThreshold(enabled bool, thresholdPercent float64) AgentOption {
	return func(a *Agent) {
		a.SummarizeOnTokenThreshold = enabled
		if thresholdPercent > 0 && thresholdPercent <= 1.0 {
			a.TokenThresholdPercent = thresholdPercent
		} else {
			a.TokenThresholdPercent = 0.8 // Default to 80%
		}
	}
}

// WithSummarizeOnFixedTokenThreshold enables fixed token-based summarization triggering
// When enabled, summarization triggers when token usage exceeds the fixed threshold
// (e.g., 200000 = 200k tokens, regardless of context window size)
// Requires EnableContextSummarization to be true
// Can be used together with WithSummarizeOnTokenThreshold (OR logic: either threshold can trigger)
func WithSummarizeOnFixedTokenThreshold(enabled bool, thresholdTokens int) AgentOption {
	return func(a *Agent) {
		a.SummarizeOnFixedTokenThreshold = enabled
		if thresholdTokens > 0 {
			a.FixedTokenThreshold = thresholdTokens
		}
	}
}

// WithSummaryKeepLastMessages sets the number of recent messages to keep when summarizing
// Default is 4 messages (roughly 2 turns)
func WithSummaryKeepLastMessages(count int) AgentOption {
	return func(a *Agent) {
		a.SummaryKeepLastMessages = count
	}
}

// WithSummarizationCooldown sets the number of turns to wait after summarization before allowing another
// This prevents repeated summarization loops when the summarized context is still large
// Default is 3 turns
func WithSummarizationCooldown(turns int) AgentOption {
	return func(a *Agent) {
		a.SummarizationCooldownTurns = turns
	}
}

// WithParallelToolExecution enables concurrent execution of multiple tool calls.
//
// When the LLM returns multiple tool calls in a single response, they will be
// executed concurrently using goroutines (fork-join pattern). Results are collected
// in deterministic order matching the original tool call order.
//
// Default: false (Sequential execution)
func WithParallelToolExecution(enabled bool) AgentOption {
	return func(a *Agent) {
		a.EnableParallelToolExecution = enabled
	}
}

// WithContextEditing enables dynamic context reduction.
//
// Unlike summarization (which compresses history), context editing targets specific
// large tool outputs in the history and replaces them with references if they become
// too old or too large, optimizing the context window.
//
// Default: false (Disabled)
func WithContextEditing(enabled bool) AgentOption {
	return func(a *Agent) {
		a.EnableContextEditing = enabled
	}
}

// WithContextEditingThreshold sets the size threshold for context editing.
//
// Tool outputs larger than this token count are candidates for compaction when they
// become "stale" (old).
//
// Default: 1000 tokens
func WithContextEditingThreshold(threshold int) AgentOption {
	return func(a *Agent) {
		a.ContextEditingThreshold = threshold
	}
}

// WithContextEditingTurnThreshold sets the age threshold for context editing.
//
// Tool outputs must be at least this many turns old before they are compacted.
// This ensures recent tool outputs stay in context for immediate reference.
//
// Default: 10 turns
func WithContextEditingTurnThreshold(turns int) AgentOption {
	return func(a *Agent) {
		a.ContextEditingTurnThreshold = turns
	}
}

// WithToolTimeout sets a global timeout for tool execution.
//
// If a tool takes longer than this duration, it will be cancelled.
//
// Default: 5 minutes
func WithToolTimeout(timeout time.Duration) AgentOption {
	return func(a *Agent) {
		a.ToolTimeout = timeout
	}
}

// WithCustomTools injects custom tools into the agent.
//
// These tools are added to the list of available tools for the LLM.
// Note: For dynamic tool registration after initialization, use RegisterCustomTool.
func WithCustomTools(tools []llmtypes.Tool) AgentOption {
	return func(a *Agent) {
		a.Tools = append(a.Tools, tools...)
	}
}

// WithSmartRouting enables intelligent tool filtering (DEPRECATED - will be removed in future version).
//
// DEPRECATED: This feature is deprecated and will be removed in a future version.
// Only use when explicitly needed for legacy compatibility.
//
// When enabled, the agent attempts to filter the available tools based on the user's
// query to reduce context usage and improve LLM focus.
//
// Default: false (Disabled)
func WithSmartRouting(enabled bool) AgentOption {
	return func(a *Agent) {
		a.EnableSmartRouting = enabled
	}
}

// WithSmartRoutingThresholds configures the triggers for smart routing (DEPRECATED).
//
// DEPRECATED: This feature is deprecated and will be removed in a future version.
// Only use when explicitly needed for legacy compatibility.
//
// Smart routing will only activate if the number of tools or servers exceeds these limits.
//
// Parameters:
//   - maxTools: Max tools allowed before routing logic kicks in.
//   - maxServers: Max servers allowed before routing logic kicks in.
//
// Default: 30 tools, 4 servers
func WithSmartRoutingThresholds(maxTools, maxServers int) AgentOption {
	return func(a *Agent) {
		a.SmartRoutingThreshold.MaxTools = maxTools
		a.SmartRoutingThreshold.MaxServers = maxServers
	}
}

// WithSmartRoutingConfig configures the internal mechanics of the smart router (DEPRECATED).
//
// DEPRECATED: This feature is deprecated and will be removed in a future version.
// Only use when explicitly needed for legacy compatibility.
//
// Parameters:
//   - temperature: LLM temperature for the routing decision.
//   - maxTokens: Max tokens for the routing request.
//   - maxMessages: History limit for routing context.
//   - userMsgLimit: Character limit for user messages in routing context.
//   - assistantMsgLimit: Character limit for assistant messages in routing context.
func WithSmartRoutingConfig(temperature float64, maxTokens, maxMessages, userMsgLimit, assistantMsgLimit int) AgentOption {
	return func(a *Agent) {
		a.SmartRoutingConfig.Temperature = temperature
		a.SmartRoutingConfig.MaxTokens = maxTokens
		a.SmartRoutingConfig.MaxMessages = maxMessages
		a.SmartRoutingConfig.UserMsgLimit = userMsgLimit
		a.SmartRoutingConfig.AssistantMsgLimit = assistantMsgLimit
	}
}

// WithSystemPrompt sets a custom system prompt.
//
// This overrides the default system prompt generation logic. The agent will use
// this exact string as the system instruction.
//
// Note: To append to the system prompt instead of replacing it, use AppendSystemPrompt() method.
func WithSystemPrompt(systemPrompt string) AgentOption {
	return func(a *Agent) {
		a.SystemPrompt = systemPrompt
		a.hasCustomSystemPrompt = true
	}
}

// WithDiscoverResource enables/disables automatic resource discovery.
//
// If enabled, the agent will query all connected MCP servers for their available resources
// and include them in the system prompt.
//
// Default: true
func WithDiscoverResource(enabled bool) AgentOption {
	return func(a *Agent) {
		a.DiscoverResource = enabled
	}
}

// WithDiscoverPrompt enables/disables automatic prompt discovery.
//
// If enabled, the agent will query all connected MCP servers for their available prompts
// and include them in the system prompt.
//
// Default: true
func WithDiscoverPrompt(enabled bool) AgentOption {
	return func(a *Agent) {
		a.DiscoverPrompt = enabled
	}
}

// WithCrossProviderFallback configures automatic model fallback.
//
// If the primary LLM provider fails, the agent can switch to the configured fallback
// provider and model(s).
func WithCrossProviderFallback(crossProviderFallback *CrossProviderFallback) AgentOption {
	return func(a *Agent) {
		a.CrossProviderFallback = crossProviderFallback
	}
}

// WithLLMConfig sets the full LLM configuration (primary + fallbacks).
// This replaces provider, ModelID, and CrossProviderFallback legacy fields.
func WithLLMConfig(config AgentLLMConfiguration) AgentOption {
	return func(a *Agent) {
		a.LLMConfig = config
		// Sync legacy fields for backward compatibility
		a.ModelID = config.Primary.ModelID
		a.provider = llm.Provider(config.Primary.Provider)
	}
}

// WithSelectedTools restricts the agent to a specific subset of tools.
//
// Parameters:
//   - tools: A list of tool identifiers in "server:tool" format (e.g., "github:create_issue").
//
// Only the specified tools will be available to the agent.
func WithSelectedTools(tools []string) AgentOption {
	return func(a *Agent) {
		a.selectedTools = tools
	}
}

// WithSelectedServers restricts the agent to tools from specific servers.
//
// Parameters:
//   - servers: A list of server names (e.g., "github", "filesystem").
//
// All tools from these servers will be available. Tools from other servers will be hidden.
func WithSelectedServers(servers []string) AgentOption {
	return func(a *Agent) {
		// Store selected servers for tool filtering logic
		// This is used to determine which servers should use "all tools" mode
		a.selectedServers = servers
	}
}

// WithCodeExecutionMode enables the Code Execution (Code Act) mode.
//
// In this mode, instead of calling tools directly via JSON-RPC, the LLM is instructed
// to write Go code that imports and calls the tool functions.
//
//   - Enabled: Only "discover_code_files" and "write_code" tools are exposed.
//   - Disabled: All MCP tools are exposed directly (Standard mode).
//
// Default: false (Standard mode)
func WithCodeExecutionMode(enabled bool) AgentOption {
	return func(a *Agent) {
		a.UseCodeExecutionMode = enabled
	}
}

// WithToolSearchMode enables the Tool Search mode.
//
// In this mode, instead of exposing all tools upfront, only the "search_tools"
// virtual tool is initially available. The LLM must search for tools using
// regex patterns, and discovered tools become available for subsequent calls.
//
//   - Enabled: Only "search_tools" is initially exposed. LLM discovers tools via search.
//   - Disabled: All MCP tools are exposed directly (Standard mode).
//
// This mode is useful when working with large tool catalogs (30+ tools) to
// reduce context usage and improve tool selection accuracy.
//
// Default: false (Standard mode)
func WithToolSearchMode(enabled bool) AgentOption {
	return func(a *Agent) {
		a.UseToolSearchMode = enabled
		if enabled {
			a.discoveredTools = make(map[string]llmtypes.Tool)
		}
	}
}

// WithPreDiscoveredTools sets tools that are always available without searching.
//
// When tool search mode is enabled, these tools will be immediately available
// alongside the "search_tools" tool, without requiring the LLM to discover them.
//
// This is useful for frequently used tools that should always be accessible.
//
// Example:
//
//	agent, _ := mcpagent.NewAgent(ctx, llm, configPath,
//	    mcpagent.WithToolSearchMode(true),
//	    mcpagent.WithPreDiscoveredTools([]string{"get_weather", "send_message"}),
//	)
func WithPreDiscoveredTools(toolNames []string) AgentOption {
	return func(a *Agent) {
		a.preDiscoveredTools = toolNames
	}
}

// WithDisableCache controls the MCP client connection cache.
//
//   - disable=true: Always establish fresh connections (slower, but safer for ephemeral tasks).
//   - disable=false: Reuse connections from the pool (faster, default).
//
// Default: false (Caching enabled)
func WithDisableCache(disable bool) AgentOption {
	return func(a *Agent) {
		a.DisableCache = disable
	}
}

// WithRuntimeOverrides sets runtime configuration overrides for MCP servers.
//
// This allows workflow-specific modifications to server configs, such as:
//   - Changing output directories per workflow run
//   - Adding workflow-specific environment variables
//   - Appending additional command arguments
//
// Example:
//
//	overrides := mcpclient.RuntimeOverrides{
//	    "playwright": {
//	        ArgsReplace: map[string]string{"--output-dir": "/path/to/workflow/downloads"},
//	    },
//	}
//	agent, _ := mcpagent.NewAgent(ctx, llm, configPath, mcpagent.WithRuntimeOverrides(overrides))
func WithRuntimeOverrides(overrides mcpclient.RuntimeOverrides) AgentOption {
	return func(a *Agent) {
		a.RuntimeOverrides = overrides
	}
}

// WithStreaming enables streaming for LLM text responses.
//
// When enabled, text content is streamed incrementally with StreamingChunkEvent events.
// Tool calls are processed normally (no streaming events).
//
// Default: false (Streaming disabled)
func WithStreaming(enabled bool) AgentOption {
	return func(a *Agent) {
		a.EnableStreaming = enabled
	}
}

// WithStreamingCallback sets an optional callback function for streaming chunks.
//
// The callback is invoked for each streaming chunk (content fragments only).
// Tool calls are not passed to this callback - they are processed normally.
//
// Parameters:
//   - callback: Function that receives StreamChunk objects (content fragments only).
//
// Default: nil (No callback)
func WithStreamingCallback(callback func(chunk llmtypes.StreamChunk)) AgentOption {
	return func(a *Agent) {
		a.StreamingCallback = callback
	}
}

// WithServerName filters the agent to connect to a specific server(s).
//
// Parameters:
//   - serverName: A specific server name, a comma-separated list, or "all".
//
// Default: "all" (Connect to all configured servers)
func WithServerName(serverName string) AgentOption {
	return func(a *Agent) {
		a.serverName = serverName
	}
}

// WithSessionID sets the session ID for connection sharing across agents.
//
// When set: MCP connections are managed by SessionConnectionRegistry and persist across
// multiple agents with the same SessionID. Agent.Close() does NOT close connections.
// Call CloseSession(sessionID) explicitly when the workflow/conversation ends.
//
// When empty (default): Legacy behavior - each agent creates and owns its connections,
// which are closed when Agent.Close() is called.
//
// Usage:
//
//	// Create agents with shared session
//	agent1, _ := NewSimpleAgent(ctx, llm, config, WithSessionID("workflow-123"))
//	agent1.Close() // Connections preserved
//
//	agent2, _ := NewSimpleAgent(ctx, llm, config, WithSessionID("workflow-123"))
//	agent2.Close() // Connections still preserved
//
//	// At workflow end
//	CloseSession("workflow-123") // Now connections are closed
func WithSessionID(sessionID string) AgentOption {
	return func(a *Agent) {
		a.SessionID = sessionID
	}
}

// WithUserID sets the user ID for per-user OAuth token isolation.
//
// When set, OAuth tokens for MCP servers are stored at user-specific paths:
// ~/.config/mcpagent/tokens/{userID}/{serverName}.json
//
// This enables multi-user deployments where each user's OAuth credentials
// are isolated from other users.
//
// When empty (default): OAuth tokens use the path from MCP server configuration
// (typically a shared default path).
func WithUserID(userID string) AgentOption {
	return func(a *Agent) {
		a.UserID = userID
	}
}

// Agent wraps MCP clients, an LLM, and an observability tracer to answer questions using tool calls.
// It is the central component that orchestrates interactions between the Large Language Model (LLM),
// Model Context Protocol (MCP) servers, and various tools.
//
// The Agent is designed to be generic and reusable across different contexts such as CLI commands,
// backend services, or test suites. It manages conversation history, tool execution, context window
// optimization, and observability.
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
	toolFilter      *ToolFilter   // Unified tool filter for consistent filtering

	// Enhanced tracking info
	SystemPrompt string
	TraceID      observability.TraceID
	configPath   string // Path to MCP config file for on-demand connections
	serverName   string // Server name(s) to connect to (default: AllServers)

	// cached list of server names (for metadata convenience)
	servers []string

	// Event system for observability - REMOVED: No longer using event dispatchers

	// Provider information
	provider llm.Provider

	// Context offloading: handles offloading large tool outputs to filesystem
	toolOutputHandler *ToolOutputHandler

	// Context offloading configuration: enables virtual tools for accessing offloaded outputs
	EnableContextOffloading bool

	// Context offloading threshold: custom threshold for when to offload tool outputs (0 = use default)
	LargeOutputThreshold int

	// Tool output cleanup configuration
	ToolOutputRetentionPeriod     time.Duration // How long to keep tool output files (0 = use default, default: 7 days)
	CleanupToolOutputOnSessionEnd bool          // Whether to clean up current session folder on session end
	cleanupTicker                 *time.Ticker  // Ticker for periodic cleanup of old tool output files
	cleanupDone                   chan bool     // Channel to signal cleanup routine to stop

	// Context summarization configuration (see context_summarization.go)
	EnableContextSummarization     bool    // Enable context summarization feature
	SummaryKeepLastMessages        int     // Number of recent messages to keep when summarizing (0 = use default)
	SummarizeOnTokenThreshold      bool    // Enable token-based summarization trigger (percentage-based)
	TokenThresholdPercent          float64 // Percentage of context window to trigger summarization (0.0-1.0, default: 0.8 = 80%)
	SummarizeOnFixedTokenThreshold bool    // Enable fixed token-based summarization trigger
	FixedTokenThreshold            int     // Fixed token threshold to trigger summarization (e.g., 200000 = 200k tokens)
	SummarizationCooldownTurns     int     // Number of turns to wait after summarization before allowing another (0 = use default: 3)
	lastSummarizationTurn          int     // Track when last summarization occurred (turn number)

	// Context editing configuration (see context_editing.go)
	EnableContextEditing        bool // Enable context editing (dynamic context reduction)
	ContextEditingThreshold     int  // Token threshold for context editing (0 = use default: 1000)
	ContextEditingTurnThreshold int  // Turn age threshold for context editing (0 = use default: 10)

	// Parallel tool execution configuration
	// When enabled and LLM returns multiple tool calls in a single response,
	// tool calls execute concurrently using goroutines (fork-join pattern).
	// Results are collected in deterministic order matching the original tool call order.
	// When disabled (default): tool calls execute sequentially as before.
	EnableParallelToolExecution bool

	// Mutex for concurrent access to Clients map during parallel tool execution
	// Used by broken pipe recovery to safely read/write the Clients map
	clientsMu sync.RWMutex

	// Mutex for concurrent access to event hierarchy state during parallel tool execution
	// Protects currentParentEventID and currentHierarchyLevel in EmitTypedEvent
	eventMu sync.Mutex

	// Store prompts and resources for system prompt rebuilding
	prompts   map[string][]mcp.Prompt
	resources map[string][]mcp.Resource

	// Flag to track if a custom system prompt was provided
	hasCustomSystemPrompt bool

	// Custom tools that are handled as virtual tools
	customTools map[string]CustomTool

	// Custom logger (optional) - uses v2.Logger interface
	Logger loggerv2.Logger

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
	// When enabled: Only virtual tools (discover_code_files, write_code) are exposed to the LLM
	// MCP tools and custom tools are NOT added directly - LLM must use generated Go code via write_code
	// When disabled (default): All MCP tools are added directly as LLM tools
	UseCodeExecutionMode bool

	// Tool search mode configuration
	// When enabled: Only search_tools virtual tool is initially exposed to the LLM
	// LLM must search for tools using regex patterns, discovered tools become available
	// When disabled (default): All tools are exposed directly
	UseToolSearchMode  bool                     // Enable tool search mode
	discoveredTools    map[string]llmtypes.Tool // Tools discovered during this session
	allDeferredTools   []llmtypes.Tool          // All available tools (hidden until discovered)
	preDiscoveredTools []string                 // Tool names that are always available without searching

	// Cache configuration
	// When enabled: Skips cache lookup and always performs fresh connections
	// When disabled (default): Uses cache to speed up connection establishment (60-85% faster)
	DisableCache bool

	// Runtime MCP configuration overrides
	// Allows workflow-specific modifications to server configs (e.g., output directories)
	RuntimeOverrides mcpclient.RuntimeOverrides

	// Session-scoped connection management
	// When set: Connections are stored in SessionConnectionRegistry and shared across agents with same SessionID
	//           Agent.Close() does NOT close connections - call CloseSession(sessionID) at workflow end
	// When empty: Legacy behavior - each agent creates/owns its connections, closed on Agent.Close()
	SessionID string

	// User ID for per-user OAuth token isolation
	// When set: OAuth tokens are stored per-user at ~/.config/mcpagent/tokens/{UserID}/{serverName}.json
	// When empty: OAuth tokens use the default path from MCP config
	UserID string

	// Streaming configuration
	// When enabled: LLM text responses are streamed incrementally with events
	// Tool calls are processed normally (no streaming events)
	EnableStreaming   bool                             // Enable streaming for LLM text responses (default: false)
	StreamingCallback func(chunk llmtypes.StreamChunk) // Optional callback for streaming chunks

	// Folder guard paths for code execution mode
	// These paths are validated at AST level before code execution
	FolderGuardReadPaths  []string // Paths allowed for read operations
	FolderGuardWritePaths []string // Paths allowed for write operations

	// Cross-provider fallback configuration
	CrossProviderFallback *CrossProviderFallback // Cross-provider fallback configuration from frontend

	// API keys for providers (used for fallback LLM creation)
	APIKeys *AgentAPIKeys

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

	// Cumulative pricing tracking for entire conversation
	cumulativeInputCost     float64 // Cumulative cost for input tokens (in USD)
	cumulativeOutputCost    float64 // Cumulative cost for output tokens (in USD)
	cumulativeReasoningCost float64 // Cumulative cost for reasoning tokens (in USD)
	cumulativeCacheCost     float64 // Cumulative cost for cached input tokens (in USD)
	cumulativeTotalCost     float64 // Total cumulative cost (in USD)

	// Context window usage tracking
	// currentContextWindowUsage represents the actual tokens currently in the context window.
	// This is reset after summarization to reflect only the tokens in the current context
	// (system + summary + recent messages), and is used for percentage calculation.
	// Note: This is separate from cumulativePromptTokens which is truly cumulative across
	// all conversation phases (never reset) for accurate pricing and overall usage reporting.
	// Context window is based on input tokens only, not output tokens.
	currentContextWindowUsage int
	modelContextWindow        int // Cached model context window size (0 = not cached yet)

	// LLM Configuration
	LLMConfig AgentLLMConfiguration
}

// LLMModel represents a single LLM configuration
type LLMModel struct {
	Provider string `json:"provider"` // "anthropic", "openai", "bedrock", etc.
	ModelID  string `json:"model_id"` // "claude-sonnet-4.5", "gpt-5", etc.

	// Auth per model
	APIKey *string `json:"api_key,omitempty"` // For OpenRouter, OpenAI, Anthropic, Vertex
	Region *string `json:"region,omitempty"`  // For Bedrock

	// Model-specific options
	Temperature *float64 `json:"temperature,omitempty"` // Override default temperature (0.0-1.0)
}

// AgentLLMConfiguration holds the primary and fallback LLM configurations
type AgentLLMConfiguration struct {
	Primary   LLMModel   `json:"primary"`
	Fallbacks []LLMModel `json:"fallbacks"`
}

// CrossProviderFallback represents cross-provider fallback configuration
type CrossProviderFallback struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

// AgentAPIKeys holds API keys for different providers (for Agent struct)
type AgentAPIKeys struct {
	OpenRouter *string
	OpenAI     *string
	Anthropic  *string
	Vertex     *string
	Bedrock    *AgentBedrockConfig
	Azure      *AgentAzureConfig
}

// AgentBedrockConfig holds Bedrock-specific configuration (for Agent struct)
type AgentBedrockConfig struct {
	Region string
}

// AgentAzureConfig holds Azure-specific configuration (for Agent struct)
type AgentAzureConfig struct {
	Endpoint   string
	APIKey     string
	APIVersion string
	Region     string
}

// GetProvider returns the provider
func (a *Agent) GetProvider() llm.Provider {
	return a.provider
}

// GetToolOutputHandler returns the tool output handler
func (a *Agent) GetToolOutputHandler() *ToolOutputHandler {
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
func (a *Agent) SetToolOutputHandler(handler *ToolOutputHandler) {
	a.toolOutputHandler = handler
}

// SetFolderGuardPaths sets the folder guard paths for code execution validation
// readPaths: paths allowed for read operations (workspace package read functions)
// writePaths: paths allowed for write operations (workspace package write functions)
func (a *Agent) SetFolderGuardPaths(readPaths, writePaths []string) {
	a.FolderGuardReadPaths = readPaths
	a.FolderGuardWritePaths = writePaths
	if a.Logger != nil {
		a.Logger.Info("🔒 [CODE_EXECUTION] Folder guard paths set",
			loggerv2.Any("read_paths", readPaths),
			loggerv2.Any("write_paths", writePaths))
	}
}

// GetFolderGuardPaths returns the folder guard paths
func (a *Agent) GetFolderGuardPaths() (readPaths, writePaths []string) {
	return a.FolderGuardReadPaths, a.FolderGuardWritePaths
}

// extractModelIDFromLLM extracts the model ID from the LLM instance
// Returns the model ID from llm.GetModelID(), or "unknown" if empty
//
// GetModelID() is now part of the llmtypes.Model interface, so all implementations
// must provide it. This makes the extraction straightforward and type-safe.
func extractModelIDFromLLM(llm llmtypes.Model) string {
	modelID := llm.GetModelID()
	if modelID == "" {
		return "unknown"
	}
	return modelID
}

// extractProviderFromLLM extracts the provider from the LLM instance
// Checks if the LLM implements GetProvider() method
func extractProviderFromLLM(model llmtypes.Model) llm.Provider {
	// Check if model implements GetProvider()
	if p, ok := model.(interface{ GetProvider() llm.Provider }); ok {
		return p.GetProvider()
	}
	return ""
}

// extractAPIKeysFromLLM extracts the API keys from the LLM instance
// Checks if the LLM implements GetAPIKeys() method (e.g., ProviderAwareLLM)
// This allows the agent to automatically use keys passed when creating the LLM
func extractAPIKeysFromLLM(model llmtypes.Model) *AgentAPIKeys {
	// Check if model implements GetAPIKeys()
	if p, ok := model.(interface{ GetAPIKeys() *llm.ProviderAPIKeys }); ok {
		providerKeys := p.GetAPIKeys()
		if providerKeys == nil {
			return nil
		}
		// Convert llm.ProviderAPIKeys to AgentAPIKeys
		agentKeys := &AgentAPIKeys{
			OpenRouter: providerKeys.OpenRouter,
			OpenAI:     providerKeys.OpenAI,
			Anthropic:  providerKeys.Anthropic,
			Vertex:     providerKeys.Vertex,
		}
		if providerKeys.Bedrock != nil {
			agentKeys.Bedrock = &AgentBedrockConfig{
				Region: providerKeys.Bedrock.Region,
			}
		}
		if providerKeys.Azure != nil {
			agentKeys.Azure = &AgentAzureConfig{
				Endpoint:   providerKeys.Azure.Endpoint,
				APIKey:     providerKeys.Azure.APIKey,
				APIVersion: providerKeys.Azure.APIVersion,
				Region:     providerKeys.Azure.Region,
			}
		}
		return agentKeys
	}
	return nil
}

// NewAgent creates a new Agent instance with the provided configuration.
//
// It initializes the agent with the given context, LLM model, and MCP configuration path.
// Additional behavior can be configured using AgentOption functions.
//
// Parameters:
//   - ctx: The base context for the agent's lifecycle.
//   - llm: The LLM provider implementation (must implement llmtypes.Model).
//   - configPath: Path to the MCP configuration file (e.g., mcp_config.json).
//   - options: Variadic list of AgentOption functions to configure the agent.
//
// Returns:
//   - *Agent: A pointer to the initialized Agent.
//   - error: An error if initialization fails (e.g., LLM is nil, config load fails).
//
// By default, the agent connects to all servers defined in the config. Use WithServerName() option to filter.
func NewAgent(ctx context.Context, llm llmtypes.Model, configPath string, options ...AgentOption) (*Agent, error) {
	if llm == nil {
		return nil, fmt.Errorf("LLM cannot be nil")
	}

	// Extract model ID from LLM instance
	// This ensures the modelID matches the actual LLM being used
	modelID := extractModelIDFromLLM(llm)

	// Create agent with default values
	ag := &Agent{
		ctx:                           ctx,
		LLM:                           llm,
		Tracers:                       []observability.Tracer{},        // Default: empty tracers array
		MaxTurns:                      GetDefaultMaxTurns(SimpleAgent), // Default to simple mode
		Temperature:                   0.0,                             // Default temperature
		ToolChoice:                    "auto",                          // Default tool choice
		ModelID:                       modelID,
		AgentMode:                     SimpleAgent,                      // Default to simple mode
		TraceID:                       "",                               // Default: empty trace ID
		provider:                      "",                               // Will be set by caller or extracted
		EnableContextOffloading:       true,                             // Default to enabled
		LargeOutputThreshold:          0,                                // Default: 0 means use default threshold (10000)
		ToolOutputRetentionPeriod:     DefaultToolOutputRetentionPeriod, // Default: 7 days
		CleanupToolOutputOnSessionEnd: false,                            // Default: false means files persist after session
		cleanupDone:                   make(chan bool, 1),               // Initialize cleanup done channel (buffered to prevent blocking/leaks)
		EnableContextSummarization:    false,                            // Default to disabled
		SummarizeOnTokenThreshold:     false,                            // Default to disabled
		TokenThresholdPercent:         0.8,                              // Default to 80% if enabled
		SummaryKeepLastMessages:       0,                                // Default: 0 means use default (4 messages)
		SummarizationCooldownTurns:    0,                                // Default: 0 means use default (3 turns)
		lastSummarizationTurn:         -1,                               // Default: -1 means never summarized
		EnableContextEditing:          false,                            // Default to disabled
		ContextEditingThreshold:       0,                                // Default: 0 means use default threshold (1000)
		ContextEditingTurnThreshold:   0,                                // Default: 0 means use default (10 turns)
		Logger:                        loggerv2.NewDefault(),            // Default logger
		customTools:                   make(map[string]CustomTool),      // Initialize custom tools map

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

		// Initialize cache (default: false - caching enabled by default)
		DisableCache: false,

		// Initialize streaming (default: false - streaming disabled)
		EnableStreaming:   false,
		StreamingCallback: nil,

		// Initialize server name (default: AllServers - connect to all servers)
		serverName: mcpclient.AllServers,
	}

	// Apply all options
	for _, option := range options {
		option(ag)
	}

	// If provider is not set, try to extract it from LLM
	if ag.provider == "" {
		ag.provider = extractProviderFromLLM(llm)
	}

	// Extract API keys from LLM if available
	// This allows users to pass keys only when creating the LLM
	if ag.APIKeys == nil {
		ag.APIKeys = extractAPIKeysFromLLM(llm)
	}

	// Use logger from options (or default if not set)
	logger := ag.Logger
	if logger == nil {
		logger = loggerv2.NewDefault()
		ag.Logger = logger
	}

	// Use serverName from options (or default AllServers)
	serverName := ag.serverName
	if serverName == "" {
		serverName = mcpclient.AllServers
	}

	// Initialize TraceID if not set (prevent empty folder collisions)
	if ag.TraceID == "" {
		ag.TraceID = observability.TraceID(uuid.New().String())
	}

	logger.Info("🔍 [DEBUG] NewAgent: Starting initialization", loggerv2.String("config_path", configPath), loggerv2.String("server_name", serverName))
	logger.Info("NewAgent started", loggerv2.String("config_path", configPath))
	logger.Info("NewAgent initialization", loggerv2.String("server_name", serverName), loggerv2.String("config_path", configPath))

	// Load merged MCP servers configuration (base + user)
	logger.Info("🔍 [DEBUG] NewAgent: About to load merged MCP config", loggerv2.String("config_path", configPath))
	configLoadStartTime := time.Now()
	config, err := mcpclient.LoadMergedConfig(configPath, logger)
	configLoadDuration := time.Since(configLoadStartTime)
	if err != nil {
		logger.Error("❌ [DEBUG] NewAgent: Failed to load merged MCP config", err, loggerv2.String("duration", configLoadDuration.String()))
		return nil, fmt.Errorf("failed to load merged MCP config: %w", err)
	}
	logger.Info("✅ [DEBUG] NewAgent: Merged MCP config loaded successfully", loggerv2.String("duration", configLoadDuration.String()), loggerv2.Int("server_count", len(config.MCPServers)))

	logger.Debug("Merged config contains servers", loggerv2.Int("server_count", len(config.MCPServers)))
	for name := range config.MCPServers {
		logger.Debug("Server found", loggerv2.String("server_name", name))
	}

	if modelID == "unknown" {
		logger.Warn("Could not extract model ID from LLM instance, using 'unknown'",
			loggerv2.String("fallback", "unknown"))
	}

	// Enable code generation in cache manager if code execution mode is enabled
	// This ensures MCP server code is only generated when needed
	logger.Debug("Getting cache manager")
	cacheManagerStartTime := time.Now()
	cacheManager := mcpcache.GetCacheManager(logger)
	cacheManagerDuration := time.Since(cacheManagerStartTime)
	logger.Debug("Cache manager obtained", loggerv2.String("duration", cacheManagerDuration.String()))

	logger.Debug("Setting code generation enabled", loggerv2.Any("enabled", ag.UseCodeExecutionMode))
	setCodeGenStartTime := time.Now()
	cacheManager.SetCodeGenerationEnabled(ag.UseCodeExecutionMode)
	setCodeGenDuration := time.Since(setCodeGenStartTime)
	logger.Debug("Code generation enabled set", loggerv2.String("duration", setCodeGenDuration.String()))

	logger.Info("🔍 [DEBUG] NewAgent: About to call NewAgentConnection", loggerv2.String("server_name", serverName), loggerv2.String("config_path", configPath), loggerv2.Any("disable_cache", ag.DisableCache), loggerv2.String("session_id", ag.SessionID))
	connectionStartTime := time.Now()

	// Check if session-scoped connection management is enabled
	var clients map[string]mcpclient.ClientInterface
	var toolToServer map[string]string
	var allLLMTools []llmtypes.Tool
	var servers []string
	var prompts map[string][]mcp.Prompt
	var resources map[string][]mcp.Resource
	var systemPrompt string

	// SessionID is mandatory for connection management via the session registry.
	// Default to "global" if not set, so all agents share connections and we never
	// fall into the legacy path that spawns fresh subprocesses on every call.
	if ag.SessionID == "" {
		ag.SessionID = "global"
		logger.Warn("SessionID not set — defaulting to 'global' for shared connection management")
	}

	logger.Info("Using session-scoped connection management", loggerv2.String("session_id", ag.SessionID))
	clients, toolToServer, allLLMTools, servers, prompts, resources, systemPrompt, err =
		NewAgentConnectionWithSession(ctx, llm, serverName, configPath, ag.SessionID, string(ag.TraceID), ag.Tracers, logger, ag.DisableCache, ag.RuntimeOverrides, ag.UserID)

	connectionDuration := time.Since(connectionStartTime)
	if err != nil {
		logger.Error("❌ [DEBUG] NewAgent: NewAgentConnection failed", err, loggerv2.String("duration", connectionDuration.String()), loggerv2.String("server_name", serverName))
		return nil, err
	}
	logger.Info("✅ [DEBUG] NewAgent: NewAgentConnection completed successfully", loggerv2.String("duration", connectionDuration.String()), loggerv2.Int("clients_count", len(clients)), loggerv2.Int("tools_count", len(allLLMTools)), loggerv2.Int("servers_count", len(servers)), loggerv2.String("session_id", ag.SessionID))

	// Use first client for legacy compatibility
	var firstClient mcpclient.ClientInterface
	if len(clients) > 0 {
		for _, c := range clients {
			firstClient = c
			break
		}
	}

	// Initialize tool output handler
	toolOutputHandler := NewToolOutputHandler()

	// Apply custom threshold if set via WithLargeOutputThreshold option
	if ag.LargeOutputThreshold > 0 {
		toolOutputHandler.SetThreshold(ag.LargeOutputThreshold)
		logger.Info("Context offloading threshold set", loggerv2.Int("threshold", ag.LargeOutputThreshold))
	}

	// Large output handling is now done via virtual tools, not MCP server
	// Virtual tools are enabled by default and handle file operations directly
	toolOutputHandler.SetServerAvailable(true) // Always available with virtual tools

	// Set session ID for organizing files by conversation
	toolOutputHandler.SetSessionID(string(ag.TraceID))

	// Set LLM for provider-aware token counting
	toolOutputHandler.SetLLM(llm)

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

	// Start periodic cleanup routine for tool output files
	ag.startCleanupRoutine()

	// 🔧 Ensure generated code exists for all connected MCP servers
	// This handles cases where cache exists but generated code was deleted
	if ag.UseCodeExecutionMode {
		// Use agent's ToolTimeout (same as used for normal tool calls)
		toolTimeout := getToolExecutionTimeout(ag)
		cacheManager.EnsureGeneratedCodeForServers(servers, config, toolTimeout, logger)
	}

	// Set selectedServers based on serverName parameter if not already set via options
	// This ensures discover_code_structure filters correctly when a single server is specified
	// IMPORTANT: Only auto-assign selectedServers if BOTH selectedServers AND selectedTools are empty
	// If selectedTools is set, the user wants specific tool filtering, not all tools from the server
	if len(ag.selectedServers) == 0 && len(ag.selectedTools) == 0 && serverName != "" && serverName != "all" {
		// serverName was specified and no filtering was configured via options
		// Use the servers list from NewAgentConnection (which already filtered based on serverName)
		ag.selectedServers = servers
		logger.Debug("Set selectedServers from serverName parameter",
			loggerv2.Any("selected_servers", ag.selectedServers))
	} else if len(ag.selectedServers) == 0 && len(ag.selectedTools) > 0 {
		// selectedTools is set but selectedServers is not - respect the specific tool filtering
		logger.Debug("Using selectedTools for filtering, not auto-assigning selectedServers",
			loggerv2.Any("selected_tools", ag.selectedTools))
	}

	// Create unified ToolFilter for consistent filtering across both modes
	// This filter is used by both LLM tool registration and discovery
	customCategories := ag.GetCustomToolCategories()
	ag.toolFilter = NewToolFilter(
		ag.selectedTools,
		ag.selectedServers,
		clients,
		customCategories,
		logger,
	)

	// Handle code execution mode: filter out MCP tools and custom tools if enabled
	var toolsToUse []llmtypes.Tool
	if ag.UseCodeExecutionMode {
		// Code execution mode: Only include virtual tools (discover_code_files, write_code)
		// Exclude all MCP server tools and custom tools (they'll be accessed via generated code)
		logger.Debug("Code execution mode enabled - excluding MCP tools and custom tools from LLM (will use generated code)")

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
			// Only include virtual tools (which will be filtered later to only discover_code_files and write_code)
			if !isMCPTool && !isCustomTool {
				// Not an MCP tool or custom tool - include it (virtual tools only)
				toolsToUse = append(toolsToUse, tool)
			}
		}
		logger.Debug("Code execution mode: tools available (only virtual tools, MCP and custom tools excluded)",
			loggerv2.Int("tool_count", len(toolsToUse)))
	} else if ag.UseToolSearchMode {
		// Tool search mode: Store filtered tools as deferred, expose only search_tools
		logger.Debug("Tool search mode enabled - storing tools as deferred (with filtering)")

		// Apply tool filtering to deferred tools
		// Only tools that pass the filter should be discoverable via search_tools
		if !ag.toolFilter.IsNoFilteringActive() {
			// Build set of custom tool names for category determination
			customToolNames := make(map[string]bool)
			for toolName, customTool := range ag.customTools {
				customToolNames[toolName] = true
				if customTool.Category != "" {
					customToolNames[customTool.Category+":"+toolName] = true
				}
			}

			// Filter deferred tools
			var filteredDeferredTools []llmtypes.Tool
			for _, tool := range allLLMTools {
				if tool.Function == nil {
					continue
				}
				toolName := tool.Function.Name

				// Determine the package/server name and tool type
				serverName, isMCPTool := toolToServer[toolName]
				isCustomTool := customToolNames[toolName]

				// Determine package name
				var packageName string
				if isMCPTool {
					packageName = serverName
				} else if isCustomTool {
					if customTool, ok := ag.customTools[toolName]; ok && customTool.Category != "" {
						packageName = customTool.Category
					} else {
						packageName = "custom"
					}
				} else {
					// Virtual tool - always include in deferred (will be filtered later)
					filteredDeferredTools = append(filteredDeferredTools, tool)
					continue
				}

				// Use unified filter to check if tool should be included
				if ag.toolFilter.ShouldIncludeTool(packageName, toolName, isCustomTool, false) {
					filteredDeferredTools = append(filteredDeferredTools, tool)
				}
			}
			ag.allDeferredTools = filteredDeferredTools
			logger.Debug("Tool search mode: Filtered tools deferred for discovery",
				loggerv2.Int("deferred_count", len(ag.allDeferredTools)),
				loggerv2.Int("total_available", len(allLLMTools)))
		} else {
			// No filtering - all tools available for discovery
			ag.allDeferredTools = allLLMTools
			logger.Debug("Tool search mode: All MCP tools deferred for discovery (no filtering)",
				loggerv2.Int("deferred_count", len(ag.allDeferredTools)))
		}

		// Don't add any MCP tools to the active tool list yet
		// They will be discovered dynamically via search_tools
		toolsToUse = []llmtypes.Tool{}
	} else {
		// Normal mode: Use all tools
		toolsToUse = allLLMTools
	}

	ag.Tools = toolsToUse
	ag.filteredTools = toolsToUse

	// Apply selected tools filter using unified ToolFilter
	// This ensures consistent filtering between LLM tools and discovery
	// Empty selectedTools/selectedServers means "use all tools" (no filtering)
	// Non-empty means "use only matching tools"
	// Also supports "server:*" pattern to explicitly request all tools from a server
	if !ag.toolFilter.IsNoFilteringActive() {
		logger.Debug("Tool filtering active",
			loggerv2.Int("selected_tools", len(ag.selectedTools)),
			loggerv2.Int("selected_servers", len(ag.selectedServers)))

		// Build set of custom tool names for category determination
		customToolNames := make(map[string]bool)
		for toolName, customTool := range ag.customTools {
			customToolNames[toolName] = true
			// Also store category for this tool
			if customTool.Category != "" {
				customToolNames[customTool.Category+":"+toolName] = true
			}
		}

		// Filter tools using unified ToolFilter
		var filteredTools []llmtypes.Tool
		for _, tool := range toolsToUse {
			if tool.Function == nil {
				continue
			}
			toolName := tool.Function.Name

			// Determine the package/server name and tool type
			serverName, isMCPTool := toolToServer[toolName]
			isCustomTool := customToolNames[toolName]

			// Determine package name for custom tools
			var packageName string
			if isMCPTool {
				packageName = serverName
			} else if isCustomTool {
				// Find the category for this custom tool
				if customTool, ok := ag.customTools[toolName]; ok && customTool.Category != "" {
					packageName = customTool.Category
				} else {
					packageName = "custom"
				}
			} else {
				// Virtual tool - always include
				filteredTools = append(filteredTools, tool)
				continue
			}

			// Use unified filter to check if tool should be included
			// Virtual tools are handled above, so isVirtualTool=false here
			if ag.toolFilter.ShouldIncludeTool(packageName, toolName, isCustomTool, false) {
				filteredTools = append(filteredTools, tool)
			}
		}

		logger.Debug("Tool filtering complete",
			loggerv2.Int("selected_tools", len(filteredTools)),
			loggerv2.Int("total_tools", len(toolsToUse)))
		ag.Tools = filteredTools
		ag.filteredTools = filteredTools
	} else {
		// No filtering active - use all available tools (already filtered by code execution mode if enabled)
		logger.Debug("Using all available tools (no filtering applied)",
			loggerv2.Int("tool_count", len(toolsToUse)))
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

	// Filter virtual tools based on code execution mode
	if ag.UseCodeExecutionMode {
		// In code execution mode, only include discover_code_files and write_code
		var filteredVirtualTools []llmtypes.Tool
		for _, tool := range virtualTools {
			if tool.Function != nil {
				toolName := tool.Function.Name
				// Only include code execution tools in code execution mode
				if toolName == "discover_code_files" || toolName == "write_code" {
					filteredVirtualTools = append(filteredVirtualTools, tool)
				}
			}
		}
		virtualTools = filteredVirtualTools
		logger.Debug("Code execution mode: Filtered virtual tools - only discover_code_files and write_code available")
	} else if ag.UseToolSearchMode {
		// In tool search mode, only include search_tools and context offloading tools
		// Context offloading tools (search_large_output, etc.) must be immediately available
		// because they're needed to access offloaded content when large outputs occur
		// All other tools are discovered dynamically via search
		var filteredVirtualTools []llmtypes.Tool
		for _, tool := range virtualTools {
			if tool.Function != nil {
				toolName := tool.Function.Name

				// Explicitly exclude code execution tools from discovery in tool search mode
				// They should only be available if UseCodeExecutionMode is true (handled in the if block above)
				if toolName == "discover_code_files" || toolName == "write_code" {
					continue
				}

				// Context offloading tools must be immediately available (pre-discovered)
				// They're infrastructure tools needed when large outputs are offloaded
				isContextOffloadingTool := toolName == "search_large_output" ||
					toolName == "read_large_output" ||
					toolName == "query_large_output"

				// Only include search_tools and context offloading tools immediately
				if toolName == "search_tools" || isContextOffloadingTool {
					filteredVirtualTools = append(filteredVirtualTools, tool)
				} else {
					// Store other virtual tools in deferred tools for discovery
					ag.allDeferredTools = append(ag.allDeferredTools, tool)
				}
			}
		}
		// Add tool search tools (search_tools, add_tool, show_all_tools)
		filteredVirtualTools = append(filteredVirtualTools, CreateToolSearchTools()...)
		virtualTools = filteredVirtualTools
		logger.Debug("Tool search mode: search_tools and context offloading tools available, other virtual tools deferred",
			loggerv2.Int("virtual_count", len(virtualTools)),
			loggerv2.Int("deferred_count", len(ag.allDeferredTools)))

		// Initialize tool search mode (pre-discover configured tools)
		ag.initializeToolSearch()
	} else {
		// In non-code execution mode, exclude discover_code_files and write_code
		var filteredVirtualTools []llmtypes.Tool
		for _, tool := range virtualTools {
			if tool.Function != nil {
				toolName := tool.Function.Name
				// Exclude code execution tools in non-code execution mode
				if toolName != "discover_code_files" && toolName != "write_code" {
					filteredVirtualTools = append(filteredVirtualTools, tool)
				}
			}
		}
		virtualTools = filteredVirtualTools
		logger.Debug("Non-code execution mode: Excluded discover_code_files and write_code from virtual tools")
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

	// Also register session-scoped custom tools to prevent cross-workflow contamination
	if ag.SessionID != "" {
		codeexec.InitRegistryForSession(ag.SessionID, customToolExecutors, logger)
		logger.Info("✅ Session-scoped custom tools registered during initialization",
			loggerv2.String("session_id", ag.SessionID),
			loggerv2.Int("count", len(customToolExecutors)))
	}

	// Generate Go code for virtual tools (only needed in code execution mode)
	// In simple agent mode, virtual tools are called directly via HandleVirtualTool()
	// The generated code is only used when LLM writes Go code that imports these packages
	var generatedDir string
	if ag.UseCodeExecutionMode {
		generatedDir = ag.getGeneratedDir()
		// Use agent's ToolTimeout (same as used for normal tool calls)
		toolTimeout := getToolExecutionTimeout(ag)
		if err := codegen.GenerateVirtualToolsCode(virtualTools, generatedDir, logger, toolTimeout); err != nil {
			logger.Warn("Failed to generate Go code for virtual tools", loggerv2.Error(err))
			// Don't fail agent initialization if code generation fails
		}
		logger.Debug("MCP server code generation handled by cache manager (no regeneration needed)")
	}

	// In code execution mode, discover tool structure and include it in system prompt
	var toolStructureJSON string
	if ag.UseCodeExecutionMode {
		// Discover all available tools and include structure in system prompt
		toolStructure, err := ag.discoverAllServersAndTools(generatedDir)
		if err != nil {
			logger.Warn("Failed to discover tool structure for system prompt", loggerv2.Error(err))
			// Continue without tool structure if discovery fails
		} else {
			toolStructureJSON = toolStructure
		}
	}

	// Always rebuild system prompt with the correct agent mode and tool structure
	// This ensures Simple agents get Simple prompts and ReAct agents get ReAct prompts
	// In code execution mode, tool structure is automatically included
	if !ag.hasCustomSystemPrompt {
		// Get tool categories for tool search mode (server/package names)
		var toolCategories []string
		if ag.UseToolSearchMode {
			for serverName := range ag.Clients {
				toolCategories = append(toolCategories, serverName)
			}
		}
		ag.SystemPrompt = prompt.BuildSystemPromptWithoutTools(ag.prompts, ag.resources, string(ag.AgentMode), ag.DiscoverResource, ag.DiscoverPrompt, ag.UseCodeExecutionMode, toolStructureJSON, ag.UseToolSearchMode, toolCategories, ag.Logger, ag.EnableParallelToolExecution)
	}

	// 🎯 SMART ROUTING INITIALIZATION - Run AFTER all tools are loaded (including virtual tools)
	// This ensures we have the complete tool count for accurate smart routing decisions

	if ag.shouldUseSmartRouting() {
		// Get server count for logging
		serverCount := len(ag.Clients)
		serverType := "active"

		logger.Warn("⚠️ SMART ROUTING IS DEPRECATED - This feature will be removed in a future version")
		logger.Info("Smart routing enabled - determining relevant tools after full initialization")
		logger.Debug("Total tools loaded",
			loggerv2.Int("tool_count", len(ag.Tools)),
			loggerv2.String("server_type", serverType),
			loggerv2.Int("server_count", serverCount),
			loggerv2.Int("max_tools_threshold", ag.SmartRoutingThreshold.MaxTools),
			loggerv2.Int("max_servers_threshold", ag.SmartRoutingThreshold.MaxServers))

		// For now, use all tools since we don't have conversation context yet
		// Smart routing will be re-evaluated in AskWithHistory with full conversation context
		ag.filteredTools = ag.Tools
		logger.Debug("Smart routing will be applied during conversation with full context")
	} else {
		// Get server count for logging
		serverCount := len(ag.Clients)
		serverType := "active"
		logger.Debug("Active mode",
			loggerv2.Int("client_count", serverCount))

		// No smart routing - use all tools
		ag.filteredTools = ag.Tools
		logger.Debug("Smart routing disabled - using all tools",
			loggerv2.Int("tool_count", len(ag.Tools)),
			loggerv2.String("server_type", serverType),
			loggerv2.Int("server_count", serverCount),
			loggerv2.Int("max_tools_threshold", ag.SmartRoutingThreshold.MaxTools),
			loggerv2.Int("max_servers_threshold", ag.SmartRoutingThreshold.MaxServers))
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

// StartAgentSession initializes a new session for the agent.
//
// It emits an AgentStartEvent, which marks the beginning of a logical session in the
// observability/tracing system. This creates the root or high-level node in the event tree.
func (a *Agent) StartAgentSession(ctx context.Context) {
	// Emit agent start event to create hierarchy
	agentStartEvent := events.NewAgentStartEvent(string(a.AgentMode), a.ModelID, string(a.provider), a.UseCodeExecutionMode, a.UseToolSearchMode)
	a.EmitTypedEvent(ctx, agentStartEvent)
}

// StartTurn creates a new turn-level event tree
func (a *Agent) StartTurn(ctx context.Context, turn int) {
	// Emit conversation turn event (this is already being emitted in conversation.go)
	// This method is kept for consistency but the actual turn event is emitted in AskWithHistory
}

// StartLLMGeneration marks the start of an LLM generation call.
//
// It emits an LLMGenerationStartEvent to the observability system. This should be called
// immediately before sending a request to the LLM provider.
func (a *Agent) StartLLMGeneration(ctx context.Context) {
	// Emit LLM generation start event to create hierarchy
	llmStartEvent := events.NewLLMGenerationStartEvent(0, a.ModelID, a.Temperature, len(a.filteredTools), 0)
	a.EmitTypedEvent(ctx, llmStartEvent)
}

// calculateCostFromTokens calculates the cost for tokens based on model metadata
// Returns cost in USD
func calculateCostFromTokens(tokenCount int, costPer1MTokens float64) float64 {
	if tokenCount <= 0 || costPer1MTokens <= 0 {
		return 0.0
	}
	// Convert from cost per 1M tokens to cost for this token count
	return (float64(tokenCount) / 1_000_000.0) * costPer1MTokens
}

// accumulateTokenUsage accumulates token usage from an LLM call.
// It accepts ContentResponse to use the unified Usage field, with fallback to GenerationInfo.
// Only accumulates if we have actual token values from LLM response (not estimates).
func (a *Agent) accumulateTokenUsage(ctx context.Context, usageMetrics events.UsageMetrics, resp *llmtypes.ContentResponse, turn int) {
	// Check if we have actual token values from LLM response
	// Only accumulate if resp has actual usage data (not estimated)
	hasActualUsage := resp != nil && ((resp.Usage != nil && (resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0)) ||
		(len(resp.Choices) > 0 && resp.Choices[0].GenerationInfo != nil &&
			(resp.Choices[0].GenerationInfo.InputTokens != nil || resp.Choices[0].GenerationInfo.OutputTokens != nil)))

	// Also check if usageMetrics has actual values (from extractUsageMetrics)
	// If usageMetrics has values but resp is nil, it might be from estimation - skip it
	if !hasActualUsage && (usageMetrics.PromptTokens > 0 || usageMetrics.CompletionTokens > 0) {
		// This means usageMetrics was populated but resp is nil or has no actual values
		// This could be from estimation - don't accumulate
		logger := getLogger(a)
		logger.Debug("Skipping token accumulation - no actual usage data from LLM response",
			loggerv2.Int("turn", turn),
			loggerv2.Int("usage_metrics_prompt", usageMetrics.PromptTokens),
			loggerv2.Int("usage_metrics_completion", usageMetrics.CompletionTokens))
		return
	}

	// If we have actual values, proceed with accumulation
	a.tokenTrackingMutex.Lock()
	defer a.tokenTrackingMutex.Unlock()

	// Use passed-in cache and reasoning tokens from usageMetrics (preferred)
	// Fall back to extraction from resp only if passed values are 0
	// Cache tokens: subset of prompt tokens that were cached (for pricing at lower rate)
	// Reasoning tokens: part of output (total output = completion + reasoning)
	cacheTokens := usageMetrics.CacheTokens
	reasoningTokens := usageMetrics.ReasoningTokens

	// If not passed in usageMetrics, extract from response as fallback
	if cacheTokens == 0 || reasoningTokens == 0 {
		extractedCache, _, extractedReasoning := extractAllTokenTypes(resp)
		if cacheTokens == 0 {
			cacheTokens = extractedCache
		}
		if reasoningTokens == 0 {
			reasoningTokens = extractedReasoning
		}
	}

	// Extract cache discount (only available in GenerationInfo)
	var cacheDiscount float64
	if resp != nil && len(resp.Choices) > 0 && resp.Choices[0].GenerationInfo != nil {
		generationInfo := resp.Choices[0].GenerationInfo
		if generationInfo.CacheDiscount != nil {
			cacheDiscount = *generationInfo.CacheDiscount
		}
	}

	// Accumulate tokens (only actual values from LLM response)
	// - PromptTokens: total input tokens (includes cached portion)
	// - CompletionTokens: output tokens (excludes reasoning tokens)
	// - CacheTokens: subset of PromptTokens that were cached (for metrics/billing)
	// - ReasoningTokens: additional output tokens for reasoning (total output = completion + reasoning)
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

	// Calculate and accumulate pricing
	// Get model metadata to calculate costs (fetch once and cache context window)
	modelID := a.ModelID
	if modelID == "" {
		modelID = a.LLM.GetModelID()
	}

	// Calculate costs for this turn
	var inputCost, outputCost, reasoningCost, cacheCost float64
	if a.LLM != nil {
		metadata, err := a.LLM.GetModelMetadata(modelID)
		if err == nil && metadata != nil {
			// Cache context window if not already cached
			if a.modelContextWindow == 0 {
				a.modelContextWindow = metadata.ContextWindow
			}

			// Calculate input cost (excluding cached tokens which are charged separately)
			// Input tokens = total prompt tokens - cached tokens (cached tokens are charged separately at a different rate)
			inputTokens := usageMetrics.PromptTokens - cacheTokens
			if inputTokens < 0 {
				// Safety check: cache tokens should not exceed prompt tokens
				// This could indicate a data inconsistency, but we'll clamp to 0 to prevent negative costs
				inputTokens = 0
			}
			if inputTokens > 0 {
				inputCost = calculateCostFromTokens(inputTokens, metadata.InputCostPer1MTokens)
			}

			// Calculate output cost
			if usageMetrics.CompletionTokens > 0 {
				outputCost = calculateCostFromTokens(usageMetrics.CompletionTokens, metadata.OutputCostPer1MTokens)
			}

			// Calculate reasoning cost
			// If model has specific reasoning cost, use it; otherwise fallback to input token rate
			if reasoningTokens > 0 {
				if metadata.ReasoningCostPer1MTokens > 0 {
					reasoningCost = calculateCostFromTokens(reasoningTokens, metadata.ReasoningCostPer1MTokens)
				} else {
					// Fallback to input token rate when reasoning cost is not specified
					// Reasoning tokens are part of input processing, so charge at input rate
					reasoningCost = calculateCostFromTokens(reasoningTokens, metadata.InputCostPer1MTokens)
				}
			}

			// Calculate cache cost (cached tokens are charged at a different rate)
			if cacheTokens > 0 && metadata.CachedInputCostPer1MTokens > 0 {
				cacheCost = calculateCostFromTokens(cacheTokens, metadata.CachedInputCostPer1MTokens)
			}
		}
	}

	// Accumulate costs
	a.cumulativeInputCost += inputCost
	a.cumulativeOutputCost += outputCost
	a.cumulativeReasoningCost += reasoningCost
	a.cumulativeCacheCost += cacheCost
	a.cumulativeTotalCost += inputCost + outputCost + reasoningCost + cacheCost

	// Update context window usage (current input tokens in conversation)
	// Set currentContextWindowUsage to the actual prompt tokens from this LLM call.
	// This represents the actual tokens currently in the context window (the messages sent to LLM).
	// Note: currentContextWindowUsage represents the actual tokens currently in the
	// context window (reset after summarization), while cumulativePromptTokens is
	// truly cumulative across all conversation phases (never reset) for pricing/reporting.
	// Context window is based on input tokens only, not output tokens
	a.currentContextWindowUsage = usageMetrics.PromptTokens

	// Token usage is tracked via events - log at debug level for per-turn, but also log cumulative
	logger := getLogger(a)
	logger.Debug("Turn tokens",
		loggerv2.Int("turn", turn),
		loggerv2.Int("input_tokens", usageMetrics.PromptTokens),
		loggerv2.Int("output_tokens", usageMetrics.CompletionTokens),
		loggerv2.Int("total_tokens", usageMetrics.TotalTokens),
		loggerv2.Int("cache_tokens", cacheTokens),
		loggerv2.Int("reasoning_tokens", reasoningTokens),
		loggerv2.Int("cumulative_total", a.cumulativeTotalTokens))
}

// EndLLMGeneration marks the completion of an LLM generation call.
//
// It captures the result, token usage metrics, and duration, emitting an LLMGenerationEndEvent.
// This matches the corresponding StartLLMGeneration call in the event tree.
//
// Parameters:
//   - ctx: Context for the operation.
//   - result: The text content generated by the LLM.
//   - turn: The conversation turn index.
//   - toolCalls: Number of tool calls generated.
//   - duration: Time taken for the generation.
//   - usageMetrics: Token usage statistics.
//   - resp: The full content response object (optional, for detailed metrics).
func (a *Agent) EndLLMGeneration(ctx context.Context, result string, turn int, toolCalls int, duration time.Duration, usageMetrics events.UsageMetrics, resp *llmtypes.ContentResponse) {
	// Accumulate token usage (including cache tokens) - uses unified Usage field
	a.accumulateTokenUsage(ctx, usageMetrics, resp, turn)

	// Extract cache and reasoning tokens to include in UsageMetrics
	// Use unified extraction from multi-llm-provider-go
	cacheTokens, _, reasoningTokens := extractAllTokenTypes(resp)

	// Add cache and reasoning tokens to usage metrics
	usageMetrics.CacheTokens = cacheTokens
	usageMetrics.ReasoningTokens = reasoningTokens

	// Calculate context window usage percentage
	var contextUsagePercent float64
	var fixedThresholdPercent float64
	a.tokenTrackingMutex.RLock()
	currentUsage := a.currentContextWindowUsage
	if a.modelContextWindow > 0 {
		contextUsagePercent = (float64(currentUsage) / float64(a.modelContextWindow)) * 100.0
	}
	// Calculate fixed threshold percentage if enabled
	if a.SummarizeOnFixedTokenThreshold && a.FixedTokenThreshold > 0 {
		fixedThresholdPercent = (float64(currentUsage) / float64(a.FixedTokenThreshold)) * 100.0
	}
	a.tokenTrackingMutex.RUnlock()

	// Emit LLM generation end event with complete token information
	llmEndEvent := events.NewLLMGenerationEndEvent(turn, result, toolCalls, duration, usageMetrics)

	// Add context usage percentage to metadata
	if llmEndEvent.Metadata == nil {
		llmEndEvent.Metadata = make(map[string]interface{})
	}
	llmEndEvent.Metadata["context_usage_percent"] = contextUsagePercent
	if a.modelContextWindow > 0 {
		llmEndEvent.Metadata["model_context_window"] = a.modelContextWindow
	}
	if fixedThresholdPercent > 0 {
		llmEndEvent.Metadata["fixed_threshold_percent"] = fixedThresholdPercent
		llmEndEvent.Metadata["fixed_threshold_tokens"] = a.FixedTokenThreshold
	}

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

	// Calculate context window usage percentage
	var contextUsagePercent float64
	var fixedThresholdPercent float64
	currentUsage := a.currentContextWindowUsage
	if a.modelContextWindow > 0 {
		contextUsagePercent = (float64(currentUsage) / float64(a.modelContextWindow)) * 100.0
	}
	// Calculate fixed threshold percentage if enabled
	if a.SummarizeOnFixedTokenThreshold && a.FixedTokenThreshold > 0 {
		fixedThresholdPercent = (float64(currentUsage) / float64(a.FixedTokenThreshold)) * 100.0
	}

	// Create generation info map with cumulative cache information and pricing
	generationInfo := make(map[string]interface{})
	generationInfo["cumulative_prompt_tokens"] = a.cumulativePromptTokens
	generationInfo["cumulative_completion_tokens"] = a.cumulativeCompletionTokens
	generationInfo["cumulative_total_tokens"] = a.cumulativeTotalTokens
	generationInfo["cumulative_cache_tokens"] = a.cumulativeCacheTokens
	generationInfo["cumulative_reasoning_tokens"] = a.cumulativeReasoningTokens
	generationInfo["llm_call_count"] = a.llmCallCount
	generationInfo["cache_enabled_call_count"] = a.cacheEnabledCallCount

	// Add pricing information
	generationInfo["cumulative_input_cost"] = a.cumulativeInputCost
	generationInfo["cumulative_output_cost"] = a.cumulativeOutputCost
	generationInfo["cumulative_reasoning_cost"] = a.cumulativeReasoningCost
	generationInfo["cumulative_cache_cost"] = a.cumulativeCacheCost
	generationInfo["cumulative_total_cost"] = a.cumulativeTotalCost

	// Add context window usage information
	generationInfo["current_context_window_usage"] = currentUsage
	generationInfo["model_context_window"] = a.modelContextWindow
	generationInfo["context_usage_percent"] = contextUsagePercent
	if fixedThresholdPercent > 0 {
		generationInfo["fixed_threshold_percent"] = fixedThresholdPercent
		generationInfo["fixed_threshold_tokens"] = a.FixedTokenThreshold
	}

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

	// Set pricing and context window fields directly on the event
	totalTokenEvent.InputCost = a.cumulativeInputCost
	totalTokenEvent.OutputCost = a.cumulativeOutputCost
	totalTokenEvent.ReasoningCost = a.cumulativeReasoningCost
	totalTokenEvent.CacheCost = a.cumulativeCacheCost
	totalTokenEvent.TotalCost = a.cumulativeTotalCost
	totalTokenEvent.ContextWindowUsage = a.currentContextWindowUsage
	totalTokenEvent.ModelContextWindow = a.modelContextWindow
	totalTokenEvent.ContextUsagePercent = contextUsagePercent

	// Set agent mode information
	totalTokenEvent.SetAgentMode(string(a.AgentMode), a.UseCodeExecutionMode, a.UseToolSearchMode)

	a.EmitTypedEvent(ctx, totalTokenEvent)

	// Log total token usage summary at Info level for visibility
	logger := getLogger(a)
	logger.Info("🔧 [TOKEN_USAGE] Conversation total token usage",
		loggerv2.Int("total_tokens", a.cumulativeTotalTokens),
		loggerv2.Int("input_tokens", a.cumulativePromptTokens),
		loggerv2.Int("output_tokens", a.cumulativeCompletionTokens),
		loggerv2.Int("cache_tokens", a.cumulativeCacheTokens),
		loggerv2.Int("reasoning_tokens", a.cumulativeReasoningTokens),
		loggerv2.Int("llm_calls", a.llmCallCount),
		loggerv2.Int("cache_enabled_calls", a.cacheEnabledCallCount),
		loggerv2.Any("duration", conversationDuration))

	// Log pricing information
	if a.cumulativeTotalCost > 0 {
		logger.Info("💰 [PRICING] Conversation total cost",
			loggerv2.Any("total_cost_usd", a.cumulativeTotalCost),
			loggerv2.Any("input_cost_usd", a.cumulativeInputCost),
			loggerv2.Any("output_cost_usd", a.cumulativeOutputCost),
			loggerv2.Any("reasoning_cost_usd", a.cumulativeReasoningCost),
			loggerv2.Any("cache_cost_usd", a.cumulativeCacheCost))
	}

	// Log context window usage
	if a.modelContextWindow > 0 {
		logger.Info("📊 [CONTEXT_WINDOW] Context usage",
			loggerv2.Int("current_usage_tokens", a.currentContextWindowUsage),
			loggerv2.Int("context_window_tokens", a.modelContextWindow),
			loggerv2.Any("usage_percent", contextUsagePercent))
	}

	logger.Info("============================================================")
}

// GetTokenUsage returns the current cumulative token usage metrics
// Returns: promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount
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

// GetTokenUsageWithPricing returns the current cumulative token usage metrics with pricing and context usage
// Returns: promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount,
//
//	inputCost, outputCost, reasoningCost, cacheCost, totalCost, contextUsagePercent
func (a *Agent) GetTokenUsageWithPricing() (
	promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount int,
	inputCost, outputCost, reasoningCost, cacheCost, totalCost float64,
	contextUsagePercent float64,
) {
	a.tokenTrackingMutex.RLock()
	defer a.tokenTrackingMutex.RUnlock()

	promptTokens = a.cumulativePromptTokens
	completionTokens = a.cumulativeCompletionTokens
	totalTokens = a.cumulativeTotalTokens
	cacheTokens = a.cumulativeCacheTokens
	reasoningTokens = a.cumulativeReasoningTokens
	llmCallCount = a.llmCallCount
	cacheEnabledCallCount = a.cacheEnabledCallCount

	inputCost = a.cumulativeInputCost
	outputCost = a.cumulativeOutputCost
	reasoningCost = a.cumulativeReasoningCost
	cacheCost = a.cumulativeCacheCost
	totalCost = a.cumulativeTotalCost

	// Calculate context window usage percentage
	if a.modelContextWindow > 0 {
		contextUsagePercent = (float64(a.currentContextWindowUsage) / float64(a.modelContextWindow)) * 100.0
	}

	return
}

// EndAgentSession finalizes the current agent session.
//
// It performs usage reporting, resource cleanup (e.g., temporary tool output files),
// and emits an AgentEndEvent. It should be called when the agent's work is complete.
//
// Parameters:
//   - ctx: Context for the operation.
//   - conversationDuration: The total duration of the session/conversation.
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

	// Stop periodic cleanup routine
	a.stopCleanupRoutine()

	// Cleanup agent-specific generated directory (only in code execution mode)
	if a.UseCodeExecutionMode {
		a.cleanupAgentGeneratedDir()
	}

	// Cleanup tool output files
	if a.toolOutputHandler != nil {
		// Clean up old files if retention period is configured
		if a.ToolOutputRetentionPeriod > 0 {
			if err := a.toolOutputHandler.CleanupOldFiles(a.ToolOutputRetentionPeriod); err != nil {
				if a.Logger != nil {
					a.Logger.Warn("Failed to cleanup old tool output files", loggerv2.Error(err))
				}
			} else if a.Logger != nil {
				a.Logger.Info("Cleaned up old tool output files", loggerv2.Any("retention_period", a.ToolOutputRetentionPeriod))
			}
		}

		// Clean up current session folder if enabled
		if a.CleanupToolOutputOnSessionEnd {
			if err := a.toolOutputHandler.CleanupCurrentSessionFolder(); err != nil {
				if a.Logger != nil {
					a.Logger.Warn("Failed to cleanup current session tool output folder", loggerv2.Error(err))
				}
			} else if a.Logger != nil {
				a.Logger.Info("Cleaned up current session tool output folder")
			}
		}
	}
}

// cleanupAgentGeneratedDir removes the agent-specific generated directory
func (a *Agent) cleanupAgentGeneratedDir() {
	agentDir := a.getAgentGeneratedDir()

	// Check if directory exists
	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		// Directory doesn't exist, nothing to clean
		return
	}

	// Remove the entire agent directory
	if err := os.RemoveAll(agentDir); err != nil {
		if a.Logger != nil {
			a.Logger.Warn("⚠️ Failed to cleanup agent directory", loggerv2.Error(err), loggerv2.String("directory", agentDir))
		}
	} else if a.Logger != nil {
		a.Logger.Info("🧹 Cleaned up agent directory", loggerv2.String("directory", agentDir))
	}
}

// startCleanupRoutine starts the background cleanup routine for old tool output files.
// It runs periodically (every hour by default) to clean up files older than the retention period.
// This ensures cleanup happens even if sessions don't end properly or agents run for long periods.
func (a *Agent) startCleanupRoutine() {
	// Only start if context offloading is enabled and retention period is set
	if !a.EnableContextOffloading || a.toolOutputHandler == nil {
		return
	}

	// If retention period is 0, automatic cleanup is disabled
	if a.ToolOutputRetentionPeriod == 0 {
		return
	}

	// Use default retention period if negative (safety check)
	retentionPeriod := a.ToolOutputRetentionPeriod
	if retentionPeriod < 0 {
		retentionPeriod = DefaultToolOutputRetentionPeriod
	}

	// Create ticker for periodic cleanup (default: every hour)
	a.cleanupTicker = time.NewTicker(DefaultToolOutputCleanupInterval)

	go func() {
		for {
			select {
			case <-a.cleanupTicker.C:
				// Perform periodic cleanup
				if a.toolOutputHandler != nil && retentionPeriod > 0 {
					if err := a.toolOutputHandler.CleanupOldFiles(retentionPeriod); err != nil {
						if a.Logger != nil {
							a.Logger.Warn("Periodic cleanup of old tool output files failed", loggerv2.Error(err))
						}
					} else if a.Logger != nil {
						a.Logger.Debug("Periodic cleanup of old tool output files completed", loggerv2.Any("retention_period", retentionPeriod))
					}
				}
			case <-a.cleanupDone:
				if a.Logger != nil {
					a.Logger.Debug("Tool output cleanup routine stopped")
				}
				return
			}
		}
	}()
}

// stopCleanupRoutine stops the background cleanup routine.
// This should be called when the agent is closed or session ends to prevent resource leaks.
func (a *Agent) stopCleanupRoutine() {
	if a.cleanupTicker != nil {
		a.cleanupTicker.Stop()
		a.cleanupTicker = nil
		// Signal cleanup routine to stop (non-blocking)
		select {
		case a.cleanupDone <- true:
		default:
			// Channel already has a signal, skip
		}
	}
}

// RebuildSystemPromptWithFilteredServers rebuilds the system prompt with only prompts/resources from relevant servers
func (a *Agent) RebuildSystemPromptWithFilteredServers(ctx context.Context, relevantServers []string) error {
	logger := a.Logger
	logger.Info("🔄 Rebuilding system prompt with filtered servers",
		loggerv2.Any("relevant_servers", relevantServers),
		loggerv2.Int("total_servers", len(a.Clients)))

	// Get fresh prompts and resources from unified cache using simple server names
	filteredPrompts := make(map[string][]mcp.Prompt)
	filteredResources := make(map[string][]mcp.Resource)

	// Load MCP configuration to get server configs for cache keys
	config, err := mcpclient.LoadMergedConfig(a.configPath, logger)
	if err != nil {
		logger.Warn("Failed to load MCP config for cache lookup", loggerv2.Error(err))
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// Get cache manager
	cacheManager := mcpcache.GetCacheManager(logger)

	for _, serverName := range relevantServers {
		// Get server configuration for this server
		serverConfig, exists := config.MCPServers[serverName]
		if !exists {
			logger.Warn("Server configuration not found, skipping cache lookup", loggerv2.String("server", serverName))
			continue
		}

		// Generate configuration-aware cache key
		cacheKey := mcpcache.GenerateUnifiedCacheKey(serverName, serverConfig)

		// Try to get cached data
		cachedEntry, found := cacheManager.Get(cacheKey)
		if !found {
			logger.Debug("Cache miss for server", loggerv2.String("server", serverName))
			continue
		}

		if cachedEntry != nil && cachedEntry.IsValid {
			logger.Info("✅ Cache hit for server - using cached prompts and resources", loggerv2.String("server", serverName))

			// Add cached prompts and resources to filtered collections
			if len(cachedEntry.Prompts) > 0 {
				filteredPrompts[serverName] = cachedEntry.Prompts
			}
			if len(cachedEntry.Resources) > 0 {
				filteredResources[serverName] = cachedEntry.Resources
			}
		} else {
			logger.Debug("Cache miss or invalid entry for server", loggerv2.String("server", serverName))
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
				a.Logger.Warn("Failed to rediscover tool structure after filtering", loggerv2.Error(err))
			}
		} else {
			toolStructureJSON = toolStructure
		}
	}
	// Get tool categories for tool search mode
	var toolCategoriesFiltered []string
	if a.UseToolSearchMode {
		for serverName := range filteredPrompts {
			toolCategoriesFiltered = append(toolCategoriesFiltered, serverName)
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
		a.UseToolSearchMode,
		toolCategoriesFiltered,
		a.Logger,
		a.EnableParallelToolExecution,
	)

	// Update the agent's system prompt
	a.SystemPrompt = newSystemPrompt

	logger.Info("✅ System prompt rebuilt with filtered servers",
		loggerv2.Int("filtered_prompts_count", len(filteredPrompts)),
		loggerv2.Int("filtered_resources_count", len(filteredResources)),
		loggerv2.Int("new_prompt_length", len(newSystemPrompt)))

	return nil
}

// NewAgentWithObservability creates a new Agent with simplified observability defaults.
//
// Unlike NewAgent, this constructor automatically ensures a tracer is configured (using a noop tracer if none provided)
// and generates a TraceID if one is not specified. This is useful for applications that need immediate
// observability compliance without manual setup.
//
// Parameters:
//   - ctx: Context for the agent.
//   - llm: The LLM model.
//   - configPath: Path to MCP config.
//   - options: Configuration options.
//
// Returns:
//   - *Agent: The initialized agent.
//   - error: An error if initialization fails.
func NewAgentWithObservability(ctx context.Context, llm llmtypes.Model, configPath string, options ...AgentOption) (*Agent, error) {
	if llm == nil {
		return nil, fmt.Errorf("LLM cannot be nil")
	}

	// Extract model ID from LLM instance
	modelID := extractModelIDFromLLM(llm)

	// Create agent with default values first to apply options
	ag := &Agent{
		ctx:                           ctx,
		LLM:                           llm,
		Tracers:                       []observability.Tracer{}, // Default: empty tracers array
		MaxTurns:                      GetDefaultMaxTurns(SimpleAgent),
		Temperature:                   0.0,
		ToolChoice:                    "auto",
		ModelID:                       modelID,
		AgentMode:                     SimpleAgent,
		TraceID:                       "", // Will be generated if not set via options
		EnableContextOffloading:       true,
		ToolOutputRetentionPeriod:     DefaultToolOutputRetentionPeriod, // Default: 7 days
		CleanupToolOutputOnSessionEnd: false,                            // Default: false means files persist after session
		cleanupDone:                   make(chan bool),                  // Initialize cleanup done channel
		EnableContextSummarization:    false,                            // Default to disabled
		SummarizeOnTokenThreshold:     false,                            // Default to disabled
		TokenThresholdPercent:         0.8,                              // Default to 80% if enabled
		SummaryKeepLastMessages:       0,                                // Default: 0 means use default (4 messages)
		SummarizationCooldownTurns:    0,                                // Default: 0 means use default (3 turns)
		lastSummarizationTurn:         -1,                               // Default: -1 means never summarized
		Logger:                        loggerv2.NewDefault(),            // Default logger
		customTools:                   make(map[string]CustomTool),
		EnableSmartRouting:            false,
		DiscoverResource:              true,
		DiscoverPrompt:                true,
		DisableCache:                  false,                // Default: cache enabled
		EnableStreaming:               false,                // Default: streaming disabled
		StreamingCallback:             nil,                  // Default: no callback
		serverName:                    mcpclient.AllServers, // Default: all servers
	}

	// Apply all options
	for _, option := range options {
		option(ag)
	}

	// If provider is not set, try to extract it from LLM
	if ag.provider == "" {
		ag.provider = extractProviderFromLLM(llm)
	}

	// Extract API keys from LLM if available
	// This allows users to pass keys only when creating the LLM
	if ag.APIKeys == nil {
		ag.APIKeys = extractAPIKeysFromLLM(llm)
	}

	// Use logger from options (or default if not set)
	logger := ag.Logger
	if logger == nil {
		logger = loggerv2.NewDefault()
		ag.Logger = logger
	}

	// Use serverName from options (or default AllServers)
	serverName := ag.serverName
	if serverName == "" {
		serverName = mcpclient.AllServers
	}

	if modelID == "unknown" {
		logger.Warn("Could not extract model ID from LLM instance, using 'unknown'",
			loggerv2.String("fallback", "unknown"))
	}

	// If no tracer was provided via options, create a noop tracer
	if len(ag.Tracers) == 0 {
		baseTracer := observability.GetTracerWithLogger("noop", logger)
		streamingTracer := NewStreamingTracer(baseTracer, 100)
		ag.Tracers = []observability.Tracer{streamingTracer}
	}

	// If no trace ID was provided via options, generate one
	if ag.TraceID == "" {
		ag.TraceID = observability.TraceID(fmt.Sprintf("agent-session-%s-%d", modelID, time.Now().UnixNano()))
	}

	// Check if session-scoped connection management is enabled
	var clients map[string]mcpclient.ClientInterface
	var toolToServer map[string]string
	var allLLMTools []llmtypes.Tool
	var servers []string
	var prompts map[string][]mcp.Prompt
	var resources map[string][]mcp.Resource
	var systemPrompt string
	var err error

	// SessionID is mandatory for connection management via the session registry.
	// Default to "global" if not set, so all agents share connections and we never
	// fall into the legacy path that spawns fresh subprocesses on every call.
	if ag.SessionID == "" {
		ag.SessionID = "global"
		logger.Warn("SessionID not set — defaulting to 'global' for shared connection management")
	}

	logger.Info("Using session-scoped connection management", loggerv2.String("session_id", ag.SessionID))
	clients, toolToServer, allLLMTools, servers, prompts, resources, systemPrompt, err =
		NewAgentConnectionWithSession(ctx, llm, serverName, configPath, ag.SessionID, string(ag.TraceID), ag.Tracers, logger, ag.DisableCache, ag.RuntimeOverrides, ag.UserID)

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

	// Initialize tool output handler for context offloading
	toolOutputHandler := NewToolOutputHandler()

	// Apply custom threshold if set via WithLargeOutputThreshold option
	if ag.LargeOutputThreshold > 0 {
		toolOutputHandler.SetThreshold(ag.LargeOutputThreshold)
		logger.Info("Context offloading threshold set", loggerv2.Int("threshold", ag.LargeOutputThreshold))
	}

	// Context offloading is done via virtual tools, not MCP server
	// Virtual tools are enabled by default and handle file operations directly
	toolOutputHandler.SetServerAvailable(true) // Always available with virtual tools

	// Set session ID for organizing files by conversation
	toolOutputHandler.SetSessionID(string(ag.TraceID))

	// Set LLM for provider-aware token counting
	toolOutputHandler.SetLLM(llm)

	// Debug logging for context offloading (observability version)
	// Use the logger we created earlier
	logger.Info("🔍 Context offloading via virtual tools (observability)",
		loggerv2.Any("virtual_tools_enabled", true),
		loggerv2.Int("total_clients", len(clients)),
		loggerv2.Any("client_names", getClientNames(clients)))

	// Update the agent struct with connection data
	ag.Client = firstClient
	ag.Clients = clients
	ag.toolToServer = toolToServer
	ag.LLM = llm
	ag.Tools = allLLMTools
	ag.SystemPrompt = systemPrompt
	ag.servers = servers
	ag.toolOutputHandler = toolOutputHandler
	ag.prompts = prompts
	ag.resources = resources

	// Start periodic cleanup routine for tool output files
	ag.startCleanupRoutine()

	// No more event listeners - events go directly to tracer
	// Tracing is handled by the tracer itself based on TRACING_PROVIDER

	// Agent initialization complete

	return ag, nil
}

// NewSimpleAgent creates a pre-configured Agent in "Simple" mode.
//
// This is a convenience constructor that applies the WithMode(SimpleAgent) option automatically.
// Simple agents are optimized for direct tool usage without complex reasoning loops.
//
// Parameters:
//   - ctx: Context for the agent.
//   - llm: The LLM model.
//   - configPath: Path to MCP config.
//   - options: Additional configuration options.
//
// Returns:
//   - *Agent: The initialized simple agent.
//   - error: An error if initialization fails.
func NewSimpleAgent(ctx context.Context, llm llmtypes.Model, configPath string, options ...AgentOption) (*Agent, error) {
	return NewAgent(ctx, llm, configPath, append(options, WithMode(SimpleAgent))...)
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
		a.Logger.Info("🔍 Streaming tracer enabled for event listener", loggerv2.String("listener", listener.Name()))

		// The streaming tracer is already active and will forward events to all listeners
		// No additional setup needed - events automatically flow through the streaming system
	} else {
		a.Logger.Warn("Streaming tracer not available, using traditional event listener system")
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

// EmitTypedEvent sends a typed event to all tracers AND all listeners.
// Thread-safe: uses eventMu to protect hierarchy state (currentParentEventID, currentHierarchyLevel)
// which can be mutated concurrently during parallel tool execution.
func (a *Agent) EmitTypedEvent(ctx context.Context, eventData events.EventData) {

	// Lock eventMu to protect hierarchy state reads and writes
	a.eventMu.Lock()

	// ✅ SET HIERARCHY FIELDS ON EVENT DATA FIRST (SINGLE SOURCE OF TRUTH)
	// Use interface-based approach - works for ALL event types that embed BaseEventData
	if baseEventData, ok := eventData.(interface {
		SetHierarchyFields(string, int, string, string)
	}); ok {
		// Use SessionID for event storage (links events to chat sessions)
		// Fall back to TraceID if SessionID is not set (legacy behavior)
		sessionIDForEvents := a.SessionID
		if sessionIDForEvents == "" {
			sessionIDForEvents = string(a.TraceID)
		}
		baseEventData.SetHierarchyFields(a.currentParentEventID, a.currentHierarchyLevel, sessionIDForEvents, events.GetComponentFromEventType(eventData.GetEventType()))
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
		switch eventType {
		case events.ConversationTurn:
			a.currentHierarchyLevel = 2 // Reset to level 2 for new conversation turn
			a.currentParentEventID = event.SpanID
		case events.ToolCallStart:
			// ✅ SPECIAL HANDLING: tool_call_start should be sibling of llm_generation_end
			// Don't increment level - use current level (same as llm_generation_end)
			a.currentParentEventID = event.SpanID
		default:
			// ✅ FIX: Increment level FIRST, then use it for next event
			a.currentHierarchyLevel++
			a.currentParentEventID = event.SpanID
		}
		// ✅ For end events: Level remains unchanged
		// SPECIAL HANDLING: tool_call_end should be sibling of tool_call_start
		// FIX: Don't decrement level immediately - let the next start event handle it
		// This allows token_usage and tool_call_start to be siblings of llm_generation_end
	}

	// Done with hierarchy state - unlock before I/O operations
	a.eventMu.Unlock()

	// Add correlation ID for start/end event pairs
	if isStartOrEndEvent(events.EventType(eventData.GetEventType())) {
		event.CorrelationID = fmt.Sprintf("%s_%d", string(eventData.GetEventType()), time.Now().UnixNano())
	}

	// Send to all tracers (multiple tracer support)
	// The streaming tracer will automatically forward events to subscribers
	for _, tracer := range a.Tracers {
		if err := tracer.EmitEvent(event); err != nil {
			a.Logger.Warn("Failed to emit event to tracer", loggerv2.Error(err), loggerv2.String("tracer_type", fmt.Sprintf("%T", tracer)))
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
			a.Logger.Warn("Failed to emit event to listener", loggerv2.Error(err), loggerv2.String("listener_type", fmt.Sprintf("%T", listener)))
		}
	}
}

// HandleEvent implements the WorkspaceEventEmitter interface for workspace tools.
// This allows workspace tools to emit events when called via the agent conversation loop.
// The workspace_tools.go file expects this interface to emit workspace_file_operation events.
func (a *Agent) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	if event != nil && event.Data != nil {
		a.EmitTypedEvent(ctx, event.Data)
	}
	return nil
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

// Close gracefully terminates the agent and closes all underlying resources.
//
// It iterates through all active MCP client connections and closes them.
// This method should be called when the agent is no longer needed to prevent resource leaks.
func (a *Agent) Close() {
	// Stop periodic cleanup routine
	a.stopCleanupRoutine()

	// Check if using session-scoped connections
	if a.SessionID != "" {
		// Session-scoped mode: connections are shared and managed by session registry
		// Do NOT close connections here - they persist until CloseSession(sessionID) is called
		a.Logger.Info("Agent closed (session-scoped mode: connections persist in session registry)",
			loggerv2.String("session_id", a.SessionID),
			loggerv2.Int("client_count", len(a.Clients)))
		return
	}

	// Legacy mode: agent owns its connections, close them on agent close
	// Close all clients in the map
	for serverName, client := range a.Clients {
		if client != nil {
			a.Logger.Info(fmt.Sprintf("🔌 Closing connection to %s", serverName), loggerv2.String("server_name", serverName))
			_ = client.Close() // Ignore errors during cleanup
		}
	}

	// Legacy single client cleanup (may be redundant but safe)
	if a.Client != nil {
		_ = a.Client.Close() // Ignore errors during cleanup
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

// Ask processes a single question from the user and returns the agent's response.
//
// This is a convenience wrapper around AskWithHistory that creates a single-message
// conversation history. It handles the full ReAct loop (Reasoning + Acting), allowing
// the agent to call tools as needed to answer the question.
//
// Parameters:
//   - ctx: Context for the request (can be used for cancellation).
//   - question: The user's input question.
//
// Returns:
//   - string: The final text response from the agent.
//   - error: An error if the interaction fails.
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

// AskWithHistory runs a multi-turn conversation interaction using the provided message history.
//
// It continues an existing conversation, processing the latest user message (last in the slice)
// and generating a response. It handles tool execution, context management, and recursive
// calls for multi-step reasoning.
//
// Parameters:
//   - ctx: Context for the request.
//   - messages: The conversation history, including the new user message.
//
// Returns:
//   - string: The final text response from the agent.
//   - []llmtypes.MessageContent: The updated conversation history (including the new response).
//   - error: An error if the interaction fails.
func (a *Agent) AskWithHistory(ctx context.Context, messages []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return AskWithHistory(a, ctx, messages)
}

// AskStructured processes a single question and strictly forces the output to match a structured schema.
//
// It performs a standard agent interaction and then uses a reliable conversion process
// to map the agent's textual response into the specified Go struct type T.
//
// Parameters:
//   - a: The Agent instance.
//   - ctx: Context for the request.
//   - question: The user's input question.
//   - schema: An instance of generic type T (used for type inference).
//   - schemaString: A JSON schema string describing T, used to guide the LLM.
//
// Returns:
//   - T: The result parsed into type T.
//   - error: An error if processing or conversion fails.
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

// AskWithHistoryStructured runs a multi-turn interaction and converts the final result to structured output.
//
// It extends AskWithHistory by applying a structured output conversion step to the final response.
//
// Parameters:
//   - a: The Agent instance.
//   - ctx: Context for the request.
//   - messages: The conversation history.
//   - schema: An instance of generic type T.
//   - schemaString: A JSON schema string describing T.
//
// Returns:
//   - T: The result parsed into type T.
//   - []llmtypes.MessageContent: The updated conversation history.
//   - error: An error if processing or conversion fails.
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

// AskWithHistoryStructuredViaTool runs an interaction where the structured output is delivered via a specific tool call.
//
// Instead of parsing the final text response, this method registers a temporary tool with the given schema.
// It instructs the LLM to call this tool to provide the answer. This often yields higher reliability
// for complex structured data than text parsing.
//
// Parameters:
//   - a: The Agent instance.
//   - ctx: Context for the request.
//   - messages: Conversation history.
//   - toolName: Name for the temporary tool (e.g., "submit_report").
//   - toolDescription: Description for the tool (e.g., "Submit the final report").
//   - schema: JSON schema string defining the expected data structure.
//
// Returns:
//   - StructuredOutputResult[T]: Result containing the structured data or fallback text.
//   - error: An error if the process fails.
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
	if err := a.RegisterCustomTool(toolName, toolDescription, toolParams, executionFunc, "structured_output"); err != nil {
		var zero StructuredOutputResult[T]
		return zero, fmt.Errorf("failed to register custom tool: %w", err)
	}

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
// Always overwrites the existing system prompt (removed prepending behavior for code execution mode)
// In code execution mode, if the prompt contains {{TOOL_STRUCTURE}} placeholder, it will be replaced with actual tool structure JSON
func (a *Agent) SetSystemPrompt(systemPrompt string) {
	// Always overwrite the system prompt (removed prepending behavior)
	// In code execution mode, replace {{TOOL_STRUCTURE}} placeholder if present
	if a.UseCodeExecutionMode && strings.Contains(systemPrompt, prompt.ToolStructurePlaceholder) {
		// Get tool structure and replace placeholder
		generatedDir := a.getGeneratedDir()
		toolStructure, err := a.discoverAllServersAndTools(generatedDir)
		if err != nil {
			if a.Logger != nil {
				a.Logger.Warn("⚠️ [CODE_EXECUTION] Failed to discover tool structure for placeholder replacement", loggerv2.Error(err))
			}
			// Remove placeholder if discovery fails
			systemPrompt = strings.ReplaceAll(systemPrompt, prompt.ToolStructurePlaceholder, "")
		} else {
			// Replace placeholder with tool structure section
			toolStructureSection := "\n\n<available_code>\n" +
				"**AVAILABLE CODE FILES AND FUNCTIONS:**\n\n" +
				"The following code files and functions are available for use in your Go code. This structure shows all servers, custom tools, and their functions:\n\n" +
				"```json\n" +
				toolStructure + "\n" +
				"```\n\n" +
				"**How to use:**\n" +
				"- The JSON structure shows package names as keys (e.g., \"google_sheets\", \"workspace\")\n" +
				"- Each package contains a \"tools\" array with available function names (e.g., \"GetDocument\", \"ListSpreadsheets\")\n" +
				"- Use the package name as \"server_name\" in discover_code_files (e.g., discover_code_files(server_name=\"google_sheets\", tool_names=[\"GetDocument\"]))\n" +
				"- Import the package and call the function in your Go code (e.g., import \"google_sheets\")\n" +
				"</available_code>\n"
			systemPrompt = strings.ReplaceAll(systemPrompt, prompt.ToolStructurePlaceholder, toolStructureSection)
			if a.Logger != nil {
				a.Logger.Info("🔧 [CODE_EXECUTION] Replaced {{TOOL_STRUCTURE}} placeholder with tool structure", loggerv2.Int("bytes", len(toolStructure)))
			}
		}
	}

	a.SystemPrompt = systemPrompt
	if a.Logger != nil {
		a.Logger.Debug("✅ System prompt overwritten", loggerv2.Int("length_chars", len(systemPrompt)))
	}
	a.hasCustomSystemPrompt = true
}

// AppendSystemPrompt appends additional content to the existing system prompt
// Removes "AI Staff Engineer" text from existing prompt when appending
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

	// If we already have a system prompt, remove AI Staff Engineer text and append with separator
	if a.SystemPrompt != "" {
		// Remove "AI Staff Engineer" text from existing prompt before appending
		existingPrompt := prompt.RemoveAIStaffEngineerText(a.SystemPrompt)
		a.SystemPrompt = existingPrompt + "\n\n" + additionalPrompt
		if a.Logger != nil {
			a.Logger.Debug("✅ System prompt appended - AI Staff Engineer text removed", loggerv2.Int("length_chars", len(additionalPrompt)))
		}
	} else {
		// If no existing system prompt, just set it
		a.SystemPrompt = additionalPrompt
	}

	// Mark as custom to prevent overwriting
	a.hasCustomSystemPrompt = true
}

// RegisterCustomTool registers a dynamic custom tool with the agent.
//
// This allows adding tools at runtime that are not provided by an MCP server.
// The tool will be available for the LLM to use during interactions.
//
// Parameters:
//   - name: The unique name of the tool.
//   - description: A description of what the tool does (used by LLM).
//   - parameters: JSON schema defining the tool's expected arguments.
//   - executionFunc: The Go function to execute when the tool is called.
//   - category: REQUIRED. The tool's category (e.g., "workspace", "human", "virtual").
//
// Returns:
//   - error: An error if registration fails (e.g., missing category).
func (a *Agent) RegisterCustomTool(name string, description string, parameters map[string]interface{}, executionFunc func(ctx context.Context, args map[string]interface{}) (string, error), category ...string) error {
	if a.customTools == nil {
		a.customTools = make(map[string]CustomTool)
	}

	// Determine category - REQUIRED, no default
	// All tools must have a category from ToolCategories map
	var toolCategory string
	if len(category) > 0 && category[0] != "" {
		toolCategory = category[0]
	} else {
		// Category is required - return error
		err := fmt.Errorf("tool %s registered without category - category is REQUIRED for all tools", name)
		if a.Logger != nil {
			a.Logger.Error("❌ [DISCOVERY] Tool registered without category", err)
		}
		return err
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

	// 🔧 CRITICAL FIX: Add custom tools to toolToServer mapping with special "custom" marker
	// This ensures they're recognized during tool lookup even when NoServers is used
	if a.toolToServer == nil {
		a.toolToServer = make(map[string]string)
		if a.Logger != nil {
			a.Logger.Debug("🔧 [TOOL_REGISTRATION] Initialized toolToServer map for custom tools")
		}
	}
	a.toolToServer[name] = "custom"
	if a.Logger != nil {
		a.Logger.Debug(fmt.Sprintf("🔧 [TOOL_REGISTRATION] Added custom tool '%s' to toolToServer mapping (category: %s)", name, toolCategory))
	}

	// 🔁 Ensure tool registration is idempotent by name
	// Some higher-level agents (e.g., structured-output orchestration/decision agents)
	// may call RegisterCustomTool with the same name multiple times over the lifetime
	// of a shared Agent. The LLM provider requires unique function names per request,
	// so we must avoid accumulating duplicate entries for the same tool name.
	//
	// Before appending, strip any existing tool with this name from Tools and filteredTools.
	if len(a.Tools) > 0 {
		cleanTools := make([]llmtypes.Tool, 0, len(a.Tools))
		for _, t := range a.Tools {
			if t.Function == nil || t.Function.Name != name {
				cleanTools = append(cleanTools, t)
			}
		}
		a.Tools = cleanTools
	}

	if len(a.filteredTools) > 0 {
		cleanFiltered := make([]llmtypes.Tool, 0, len(a.filteredTools))
		for _, t := range a.filteredTools {
			if t.Function == nil || t.Function.Name != name {
				cleanFiltered = append(cleanFiltered, t)
			}
		}
		a.filteredTools = cleanFiltered
	}

	// In code execution mode, do NOT add custom tools to LLM tools list
	// They should only be accessible via generated Go code
	// EXCEPTION: Structured output tools (category "structured_output") must always be available
	// because they're orchestration/control tools, not regular MCP tools
	// EXCEPTION: Human tools (category "human") must always be available
	// because they require event bridge access for frontend UI and cannot work via generated code
	isStructuredOutputTool := toolCategory == "structured_output"
	isHumanTool := toolCategory == "human"

	// 🔍 TOOL SEARCH MODE: Handle custom tools differently
	// Custom tools should be added to allDeferredTools so they can be discovered via search_tools
	// Pre-discovered tools or special categories should be immediately available
	if a.UseToolSearchMode {
		// Check tool filter first - if filtering is active and tool doesn't pass, skip adding to deferred
		// Special categories (structured_output, human) bypass filtering as they're system tools
		shouldIncludeInDeferred := isStructuredOutputTool || isHumanTool
		if !shouldIncludeInDeferred && a.toolFilter != nil && !a.toolFilter.IsNoFilteringActive() {
			// Apply tool filter - use category as package name for custom tools
			shouldIncludeInDeferred = a.toolFilter.ShouldIncludeTool(toolCategory, name, true, false)
			if !shouldIncludeInDeferred {
				if a.Logger != nil {
					a.Logger.Debug(fmt.Sprintf("🔍 [TOOL_SEARCH] Custom tool '%s' excluded by tool filter (category: %s)", name, toolCategory))
				}
				// Tool is filtered out - don't add to deferred tools
				// But still register the execution function (in case filter changes or for other uses)
			}
		} else if !shouldIncludeInDeferred {
			shouldIncludeInDeferred = true // No filtering active, include by default
		}

		// Only proceed with Tool Search registration if tool passes filter
		if shouldIncludeInDeferred {
			// Check if this tool is in the pre-discovered list
			isPreDiscovered := false
			for _, preDiscoveredName := range a.preDiscoveredTools {
				if preDiscoveredName == name {
					isPreDiscovered = true
					break
				}
			}

			// Special categories (structured_output, human) are always immediately available
			// Pre-discovered tools are also immediately available
			if isStructuredOutputTool || isHumanTool || isPreDiscovered {
				// Add to discoveredTools map so it's immediately available
				if a.discoveredTools == nil {
					a.discoveredTools = make(map[string]llmtypes.Tool)
				}
				a.discoveredTools[name] = tool

				// Also add to allDeferredTools so it can still be found via search
				a.allDeferredTools = append(a.allDeferredTools, tool)

				// Refresh filteredTools with tool search mode tools (includes discovered tools)
				a.filteredTools = a.getToolsForToolSearchMode()

				if a.Logger != nil {
					if isPreDiscovered {
						a.Logger.Info(fmt.Sprintf("🔍 [TOOL_SEARCH] Pre-discovered custom tool '%s' added to discovered tools (category: %s)", name, toolCategory))
					} else {
						a.Logger.Info(fmt.Sprintf("🔍 [TOOL_SEARCH] Special category custom tool '%s' added to discovered tools (category: %s)", name, toolCategory))
					}
				}
			} else {
				// Regular custom tool in tool search mode: add to deferred tools only
				// Agent must use search_tools + add_tool to discover it
				a.allDeferredTools = append(a.allDeferredTools, tool)

				if a.Logger != nil {
					a.Logger.Info(fmt.Sprintf("🔍 [TOOL_SEARCH] Custom tool '%s' added to deferred tools for discovery (category: %s)", name, toolCategory))
				}
			}
		}
	} else if !a.UseCodeExecutionMode || isStructuredOutputTool || isHumanTool {
		// Normal mode OR structured output tool OR human tool: Add to the main Tools array so the LLM can see it
		a.Tools = append(a.Tools, tool)

		// 🔧 CRITICAL FIX: Also add to filteredTools if smart routing is active
		// This ensures custom tools are available even when smart routing is enabled
		a.filteredTools = append(a.filteredTools, tool)

		if a.UseCodeExecutionMode && isStructuredOutputTool {
			if a.Logger != nil {
				a.Logger.Debug("🔧 Code execution mode: Structured output tool added to LLM tools (required for orchestration)", loggerv2.String("tool", name))
			}
		}
		if a.UseCodeExecutionMode && isHumanTool {
			if a.Logger != nil {
				a.Logger.Info(fmt.Sprintf("🔧 Code execution mode: Human tool %s added to LLM tools (requires event bridge for frontend UI) - total tools now: %d", name, len(a.Tools)))
			}
		}
	} else {
		// Code execution mode: Don't add to LLM tools, but still generate code and update registry
		if a.Logger != nil {
			a.Logger.Debug("🔧 Code execution mode: Custom tool registered but not added to LLM tools (will use generated code)", loggerv2.String("tool", name))
		}
	}

	// Generate Go code for ONLY the new tool (not all tools - that was causing O(n²) regeneration)
	// Only generate code in code execution mode - simple agent mode doesn't need generated code
	// EXCEPTION: Skip code generation for human tools - they must only be used as direct LLM tools
	// (not via generated code) because they need event bridge access for frontend UI
	if a.UseCodeExecutionMode {
		if isHumanTool {
			if a.Logger != nil {
				a.Logger.Info(fmt.Sprintf("🔧 Skipping code generation for human tool %s - must be used as direct LLM tool only", name))
			}
		} else {
			generatedDir := a.getGeneratedDir()
			singleToolForCodeGen := map[string]codegen.CustomToolForCodeGen{
				name: {
					Definition: tool,
					Category:   toolCategory,
				},
			}
			if a.Logger != nil {
				a.Logger.Debug(fmt.Sprintf("🔍 [DISCOVERY] Generating code for new tool: %s (category: %s)", name, toolCategory))
			}
			// Use agent's ToolTimeout (same as used for normal tool calls)
			toolTimeout := getToolExecutionTimeout(a)
			if err := codegen.GenerateCustomToolsCode(singleToolForCodeGen, generatedDir, a.Logger, toolTimeout); err != nil {
				if a.Logger != nil {
					a.Logger.Warn(fmt.Sprintf("🔍 [DISCOVERY] Failed to generate Go code for tool %s: %v", name, err))
				}
				// Don't fail tool registration if code generation fails
			} else if a.Logger != nil {
				a.Logger.Debug(fmt.Sprintf("🔍 [DISCOVERY] Successfully generated code for tool: %s", name))
			}
		}
	}

	// Update registry with new custom tool
	if a.Clients != nil {
		customToolExecutors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))
		for toolName, customTool := range a.customTools {
			customToolExecutors[toolName] = customTool.Execution
		}
		if a.Logger != nil {
			a.Logger.Debug("🔧 [CODE_EXECUTION] Updating registry with custom tools",
				loggerv2.Int("count", len(customToolExecutors)),
				loggerv2.String("including", name))
			// Log all custom tool names for debugging
			toolNames := make([]string, 0, len(customToolExecutors))
			for toolName := range customToolExecutors {
				toolNames = append(toolNames, toolName)
			}
			a.Logger.Debug("🔧 [CODE_EXECUTION] Custom tools in registry", loggerv2.Any("tools", toolNames))
		}
		codeexec.InitRegistry(a.Clients, customToolExecutors, a.toolToServer, a.Logger)
		// Also register session-scoped tools
		if a.SessionID != "" {
			codeexec.InitRegistryForSession(a.SessionID, customToolExecutors, a.Logger)
		}
		if a.Logger != nil {
			a.Logger.Debug("🔧 [CODE_EXECUTION] Registry updated successfully for tool", loggerv2.String("tool", name))
		}
	} else {
		if a.Logger != nil {
			a.Logger.Warn("⚠️ [CODE_EXECUTION] Cannot update registry - a.Clients is nil for tool", loggerv2.String("tool", name))
		}
	}

	// 🔧 CRITICAL: Rebuild system prompt with updated tool structure in code execution mode
	// This ensures custom tools appear in the system prompt's tool structure JSON
	// so the LLM knows they exist and can use them via generated Go code
	if a.UseCodeExecutionMode {
		if err := a.rebuildSystemPromptWithUpdatedToolStructure(); err != nil {
			if a.Logger != nil {
				a.Logger.Warn("⚠️ [CODE_EXECUTION] Failed to rebuild system prompt with updated tool structure", loggerv2.Error(err))
			}
			// Don't fail tool registration if system prompt rebuild fails
		} else {
			if a.Logger != nil {
				a.Logger.Info("✅ [CODE_EXECUTION] System prompt rebuilt with updated tool structure (custom tool now included)", loggerv2.String("tool", name))
			}
		}
	}

	// Debug logging
	if a.Logger != nil {
		a.Logger.Info("🔧 Registered custom tool", loggerv2.String("tool", name), loggerv2.String("category", toolCategory))
		a.Logger.Info("🔧 Total custom tools registered", loggerv2.Int("count", len(a.customTools)))
		a.Logger.Info("🔧 Total tools in agent", loggerv2.Int("count", len(a.Tools)))
		a.Logger.Info("🔧 Total filtered tools", loggerv2.Int("count", len(a.filteredTools)))
	}

	return nil
}

// RegisterCustomToolWithTimeout registers a dynamic custom tool with a specific per-tool timeout.
//
// This is an extension of RegisterCustomTool that allows specifying a custom timeout for this tool.
// This is useful for tools that may take longer than the default timeout (e.g., sub-agent execution).
//
// Parameters:
//   - name: The unique name of the tool.
//   - description: A description of what the tool does (used by LLM).
//   - parameters: JSON schema defining the tool's expected arguments.
//   - executionFunc: The Go function to execute when the tool is called.
//   - timeout: Per-tool timeout. 0 = no timeout (tool runs indefinitely). -1 = use agent default.
//   - category: REQUIRED. The tool's category (e.g., "workspace", "human", "virtual").
//
// Returns:
//   - error: An error if registration fails (e.g., missing category).
func (a *Agent) RegisterCustomToolWithTimeout(name string, description string, parameters map[string]interface{}, executionFunc func(ctx context.Context, args map[string]interface{}) (string, error), timeout time.Duration, category ...string) error {
	// First register the tool using the standard method
	err := a.RegisterCustomTool(name, description, parameters, executionFunc, category...)
	if err != nil {
		return err
	}

	// Now update the timeout for this tool
	if customTool, exists := a.customTools[name]; exists {
		customTool.Timeout = timeout
		a.customTools[name] = customTool
		if a.Logger != nil {
			if timeout == 0 {
				a.Logger.Info("🔧 Custom tool registered with NO timeout (runs indefinitely)", loggerv2.String("tool", name))
			} else if timeout == -1 {
				a.Logger.Info("🔧 Custom tool registered with agent default timeout", loggerv2.String("tool", name))
			} else {
				a.Logger.Info("🔧 Custom tool registered with custom timeout", loggerv2.String("tool", name), loggerv2.String("timeout", timeout.String()))
			}
		}
	}

	return nil
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
			a.Logger.Warn("⚠️ [CODE_EXECUTION] Cannot update registry - a.Clients is nil")
		}
		return fmt.Errorf("cannot update registry: Clients is nil")
	}

	// Build custom tool executors map from all registered custom tools
	customToolExecutors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))
	for toolName, customTool := range a.customTools {
		customToolExecutors[toolName] = customTool.Execution
	}

	if a.Logger != nil {
		a.Logger.Info("🔧 [CODE_EXECUTION] Explicitly updating registry with custom tools", loggerv2.Int("count", len(customToolExecutors)))
		// Log all custom tool names for debugging
		toolNames := make([]string, 0, len(customToolExecutors))
		for toolName := range customToolExecutors {
			toolNames = append(toolNames, toolName)
		}
		a.Logger.Debug("🔧 [CODE_EXECUTION] Custom tools being registered", loggerv2.Any("tools", toolNames))
	}

	// Update the global registry (for backward compatibility)
	codeexec.InitRegistry(a.Clients, customToolExecutors, a.toolToServer, a.Logger)

	// Also register session-scoped tools to prevent cross-workflow contamination
	// When multiple workflows run concurrently, each gets its own scoped tools
	if a.SessionID != "" {
		codeexec.InitRegistryForSession(a.SessionID, customToolExecutors, a.Logger)
		if a.Logger != nil {
			a.Logger.Info("✅ [CODE_EXECUTION] Session-scoped tools registered",
				loggerv2.String("session_id", a.SessionID),
				loggerv2.Int("count", len(customToolExecutors)))
		}
	}

	if a.Logger != nil {
		a.Logger.Info("✅ [CODE_EXECUTION] Registry updated successfully with custom tools", loggerv2.Int("count", len(customToolExecutors)))
	}

	// 🔧 CRITICAL: Rebuild system prompt with updated tool structure in code execution mode
	// This ensures workspace and human tools appear in the system prompt
	if a.UseCodeExecutionMode {
		if err := a.rebuildSystemPromptWithUpdatedToolStructure(); err != nil {
			if a.Logger != nil {
				a.Logger.Warn("⚠️ [CODE_EXECUTION] Failed to rebuild system prompt with updated tool structure", loggerv2.Error(err))
			}
			// Don't fail registry update if system prompt rebuild fails
		} else {
			if a.Logger != nil {
				a.Logger.Info("✅ [CODE_EXECUTION] System prompt rebuilt with updated tool structure (workspace and human tools now included)")
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
	// Note: This function is only called in code execution mode, so UseToolSearchMode is false
	newSystemPrompt := prompt.BuildSystemPromptWithoutTools(
		a.prompts,
		a.resources,
		string(a.AgentMode),
		a.DiscoverResource,
		a.DiscoverPrompt,
		a.UseCodeExecutionMode,
		toolStructure,
		false, // UseToolSearchMode - not applicable in code execution mode
		nil,   // toolCategories - not applicable in code execution mode
		a.Logger,
		a.EnableParallelToolExecution,
	)

	// Update the agent's system prompt
	a.SystemPrompt = newSystemPrompt

	if a.Logger != nil {
		a.Logger.Debug("🔧 [CODE_EXECUTION] System prompt rebuilt",
			loggerv2.Int("prompt_bytes", len(newSystemPrompt)),
			loggerv2.Int("tool_structure_bytes", len(toolStructure)))
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
// Only creates the directory if code execution mode is enabled
func (a *Agent) getGeneratedDir() string {
	// Use shared utility for path calculation (single source of truth)
	path := mcpcache.GetGeneratedDirPath()

	// Only create directory if code execution mode is enabled
	// In simple agent mode, we don't need the generated directory
	if a.UseCodeExecutionMode {
		_ = mcpcache.EnsureGeneratedDir(path, a.Logger)
	}

	return path
}
