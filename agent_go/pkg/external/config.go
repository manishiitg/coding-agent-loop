package external

import (
	"os"
	"time"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
)

// MCPServerConfig holds configuration for a single MCP server
type MCPServerConfig struct {
	// Server description
	Description string

	// Connection details (one of these must be set)
	URL      string   // HTTP/SSE server URL
	Command  string   // Command for stdio servers
	Args     []string // Arguments for stdio servers
	Protocol string   // Protocol type (http, sse, stdio)

	// Environment variables for stdio servers
	Env map[string]string

	// Headers for HTTP/SSE servers
	Headers map[string]string
}

// SystemPromptConfig holds configuration for custom system prompts
type SystemPromptConfig struct {
	// Custom system prompt template (overrides default)
	CustomTemplate string

	// System prompt mode (simple, react, custom)
	Mode string

	// Additional instructions to append to system prompt
	AdditionalInstructions string

	// Whether to include default tool handling instructions
	IncludeToolInstructions bool

	// Whether to include default large output handling instructions
	IncludeLargeOutputInstructions bool
}

// Config holds configuration for the external agent
type Config struct {
	// Agent configuration
	AgentMode AgentMode

	// Server configuration
	ServerName string // Optional: if empty, defaults to "all" servers
	ConfigPath string

	// 🆕 NEW: Direct MCP server configuration (alternative to config file)
	MCPServers map[string]MCPServerConfig

	// LLM configuration
	Provider    llm.Provider // LLM provider (bedrock, openai, anthropic, openrouter)
	ModelID     string       // Model identifier
	Temperature float64      // LLM temperature (0.0 to 1.0)
	ToolChoice  string       // Tool choice strategy
	MaxTurns    int          // Maximum conversation turns

	// Observability configuration
	TraceProvider string               // Tracing provider (console, langfuse, noop)
	LangfuseHost  string               // Langfuse host URL
	Tracer        observability.Tracer // 🆕 NEW: Optional tracer instance

	// Timeout configuration
	Timeout     time.Duration
	ToolTimeout time.Duration // Tool execution timeout (default: 5 minutes)

	// Custom logger (optional) - uses our ExtendedLogger interface
	Logger utils.ExtendedLogger

	// System prompt configuration
	SystemPrompt SystemPromptConfig
}

// DefaultConfig returns a default configuration
func DefaultConfig() Config {
	return Config{
		AgentMode:     SimpleAgent,
		ServerName:    "all", // Default to all servers
		ConfigPath:    "configs/mcp_servers.json",
		Provider:      llm.ProviderBedrock,
		ModelID:       os.Getenv("BEDROCK_PRIMARY_MODEL"),
		Temperature:   0.2,
		ToolChoice:    "auto",
		MaxTurns:      20,
		TraceProvider: "console",
		LangfuseHost:  "https://cloud.langfuse.com",
		Timeout:       5 * time.Minute,
		ToolTimeout:   5 * time.Minute, // Default 5-minute tool timeout
		SystemPrompt: SystemPromptConfig{
			Mode:                           "auto", // auto-detect based on agent mode
			IncludeToolInstructions:        true,
			IncludeLargeOutputInstructions: true,
		},
	}
}

// WithAgentMode sets the agent mode
//
// Deprecated: Use NewAgentBuilder().WithAgentMode() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithAgentMode(mode AgentMode) Config {
	c.AgentMode = mode
	return c
}

// WithServer sets the server configuration
// If name is empty, it will default to "all" servers
//
// Deprecated: Use NewAgentBuilder().WithServer() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithServer(name, configPath string) Config {
	c.ServerName = name
	c.ConfigPath = configPath
	return c
}

// WithLLM sets the LLM configuration
//
// Deprecated: Use NewAgentBuilder().WithLLM() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithLLM(provider llm.Provider, modelID string, temperature float64) Config {
	c.Provider = provider
	c.ModelID = modelID
	c.Temperature = temperature
	return c
}

// WithToolChoice sets the tool choice strategy
//
// Deprecated: Use NewAgentBuilder().WithToolChoice() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithToolChoice(toolChoice string) Config {
	c.ToolChoice = toolChoice
	return c
}

// WithMaxTurns sets the maximum conversation turns
//
// Deprecated: Use NewAgentBuilder().WithMaxTurns() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithMaxTurns(maxTurns int) Config {
	c.MaxTurns = maxTurns
	return c
}

// WithObservability sets the observability configuration
//
// Deprecated: Use NewAgentBuilder().WithObservability() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithObservability(traceProvider, langfuseHost string) Config {
	c.TraceProvider = traceProvider
	c.LangfuseHost = langfuseHost
	return c
}

// WithTimeout sets the timeout
//
// Deprecated: Use NewAgentBuilder().WithTimeout() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithTimeout(timeout time.Duration) Config {
	c.Timeout = timeout
	return c
}

// WithToolTimeout sets the tool execution timeout
//
// Deprecated: Use NewAgentBuilder().WithToolTimeout() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithToolTimeout(toolTimeout time.Duration) Config {
	c.ToolTimeout = toolTimeout
	return c
}

// WithLogger sets the custom logger
//
// Deprecated: Use NewAgentBuilder().WithLogger() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithLogger(logger utils.ExtendedLogger) Config {
	c.Logger = logger
	return c
}

// WithCustomSystemPrompt sets a completely custom system prompt template
//
// Deprecated: Use NewAgentBuilder().WithCustomSystemPrompt() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithCustomSystemPrompt(template string) Config {
	c.SystemPrompt.CustomTemplate = template
	c.SystemPrompt.Mode = "custom"
	return c
}

// WithSystemPromptMode sets the system prompt mode
//
// Deprecated: Use NewAgentBuilder().WithSystemPromptMode() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithSystemPromptMode(mode string) Config {
	c.SystemPrompt.Mode = mode
	return c
}

// WithAdditionalInstructions adds additional instructions to the system prompt
//
// Deprecated: Use NewAgentBuilder().WithAdditionalInstructions() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithAdditionalInstructions(instructions string) Config {
	c.SystemPrompt.AdditionalInstructions = instructions
	return c
}

// WithToolInstructions enables/disables default tool handling instructions
//
// Deprecated: Use NewAgentBuilder().WithToolInstructions() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithToolInstructions(include bool) Config {
	c.SystemPrompt.IncludeToolInstructions = include
	return c
}

// WithLargeOutputInstructions enables/disables default large output handling instructions
//
// Deprecated: Use NewAgentBuilder().WithLargeOutputInstructions() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithLargeOutputInstructions(include bool) Config {
	c.SystemPrompt.IncludeLargeOutputInstructions = include
	return c
}

// WithTracer sets the tracer for the configuration
//
// Deprecated: Use NewAgentBuilder().WithTracer() instead for better readability and immutability.
// This method will be removed in a future version.
func (c Config) WithTracer(tracer observability.Tracer) Config {
	c.Tracer = tracer
	return c
}
