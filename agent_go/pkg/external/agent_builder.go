package external

import (
	"context"
	"time"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
)

// AgentBuilder provides a fluent interface for building agent configurations
type AgentBuilder struct {
	// Agent configuration
	agentMode AgentMode

	// Server configuration
	serverName string
	configPath string
	mcpServers map[string]MCPServerConfig

	// LLM configuration
	provider    llm.Provider
	modelID     string
	temperature float64
	toolChoice  string
	maxTurns    int

	// Observability configuration
	traceProvider string
	langfuseHost  string
	tracer        observability.Tracer

	// Timeout configuration
	timeout     time.Duration
	toolTimeout time.Duration

	// Custom logger
	logger utils.ExtendedLogger

	// System prompt configuration
	systemPrompt SystemPromptConfig
}

// NewAgentBuilder creates a new agent builder with default values
func NewAgentBuilder() *AgentBuilder {
	return &AgentBuilder{
		agentMode:     SimpleAgent,
		serverName:    "all",
		configPath:    "configs/mcp_servers.json",
		provider:      llm.ProviderBedrock,
		modelID:       "us.anthropic.claude-sonnet-4-20250514-v1:0",
		temperature:   0.2,
		toolChoice:    "auto",
		maxTurns:      20,
		traceProvider: "console",
		langfuseHost:  "https://cloud.langfuse.com",
		timeout:       5 * time.Minute,
		toolTimeout:   5 * time.Minute,
		systemPrompt: SystemPromptConfig{
			Mode:                           "auto",
			IncludeToolInstructions:        true,
			IncludeLargeOutputInstructions: true,
		},
	}
}

// WithAgentMode sets the agent mode
func (b *AgentBuilder) WithAgentMode(mode AgentMode) *AgentBuilder {
	b.agentMode = mode
	return b
}

// WithServer sets the server configuration
func (b *AgentBuilder) WithServer(name, configPath string) *AgentBuilder {
	b.serverName = name
	b.configPath = configPath
	return b
}

// WithMCPServers sets direct MCP server configuration
func (b *AgentBuilder) WithMCPServers(servers map[string]MCPServerConfig) *AgentBuilder {
	b.mcpServers = servers
	return b
}

// WithLLM sets the LLM configuration
func (b *AgentBuilder) WithLLM(provider llm.Provider, modelID string, temperature float64) *AgentBuilder {
	b.provider = provider
	b.modelID = modelID
	b.temperature = temperature
	return b
}

// WithToolChoice sets the tool choice strategy
func (b *AgentBuilder) WithToolChoice(toolChoice string) *AgentBuilder {
	b.toolChoice = toolChoice
	return b
}

// WithMaxTurns sets the maximum conversation turns
func (b *AgentBuilder) WithMaxTurns(maxTurns int) *AgentBuilder {
	b.maxTurns = maxTurns
	return b
}

// WithObservability sets the observability configuration
func (b *AgentBuilder) WithObservability(traceProvider, langfuseHost string) *AgentBuilder {
	b.traceProvider = traceProvider
	b.langfuseHost = langfuseHost
	return b
}

// WithTimeout sets the timeout
func (b *AgentBuilder) WithTimeout(timeout time.Duration) *AgentBuilder {
	b.timeout = timeout
	return b
}

// WithToolTimeout sets the tool execution timeout
func (b *AgentBuilder) WithToolTimeout(toolTimeout time.Duration) *AgentBuilder {
	b.toolTimeout = toolTimeout
	return b
}

// WithLogger sets the custom logger
func (b *AgentBuilder) WithLogger(logger utils.ExtendedLogger) *AgentBuilder {
	b.logger = logger
	return b
}

// WithTracer sets the tracer
func (b *AgentBuilder) WithTracer(tracer observability.Tracer) *AgentBuilder {
	b.tracer = tracer
	return b
}

// WithCustomSystemPrompt sets a completely custom system prompt template
func (b *AgentBuilder) WithCustomSystemPrompt(template string) *AgentBuilder {
	b.systemPrompt.CustomTemplate = template
	b.systemPrompt.Mode = "custom"
	return b
}

// WithSystemPromptMode sets the system prompt mode
func (b *AgentBuilder) WithSystemPromptMode(mode string) *AgentBuilder {
	b.systemPrompt.Mode = mode
	return b
}

// WithAdditionalInstructions adds additional instructions to the system prompt
func (b *AgentBuilder) WithAdditionalInstructions(instructions string) *AgentBuilder {
	b.systemPrompt.AdditionalInstructions = instructions
	return b
}

// WithToolInstructions enables/disables default tool handling instructions
func (b *AgentBuilder) WithToolInstructions(include bool) *AgentBuilder {
	b.systemPrompt.IncludeToolInstructions = include
	return b
}

// WithLargeOutputInstructions enables/disables default large output handling instructions
func (b *AgentBuilder) WithLargeOutputInstructions(include bool) *AgentBuilder {
	b.systemPrompt.IncludeLargeOutputInstructions = include
	return b
}

// Build creates the agent configuration and returns the agent
func (b *AgentBuilder) Build(ctx context.Context) (Agent, error) {
	// Convert builder to internal config for compatibility
	config := Config{
		AgentMode:     b.agentMode,
		ServerName:    b.serverName,
		ConfigPath:    b.configPath,
		MCPServers:    b.mcpServers,
		Provider:      b.provider,
		ModelID:       b.modelID,
		Temperature:   b.temperature,
		ToolChoice:    b.toolChoice,
		MaxTurns:      b.maxTurns,
		TraceProvider: b.traceProvider,
		LangfuseHost:  b.langfuseHost,
		Tracer:        b.tracer,
		Timeout:       b.timeout,
		ToolTimeout:   b.toolTimeout,
		Logger:        b.logger,
		SystemPrompt:  b.systemPrompt,
	}

	// Use the existing NewAgent function for now
	return NewAgent(ctx, config)
}

// Create is an alias for Build for more intuitive naming
func (b *AgentBuilder) Create(ctx context.Context) (Agent, error) {
	return b.Build(ctx)
}
