package agents

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcpagent/mcpclient"
)

// OrchestratorAgent defines the interface for all orchestrator agents
type OrchestratorAgent interface {
	// Execute executes the agent with the given template variables and returns the result and updated conversation history
	Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error)

	// GetType returns the agent type (planning, execution, validation, plan_organizer)
	GetType() string

	// GetConfig returns the agent configuration
	GetConfig() *OrchestratorAgentConfig

	// Initialize initializes the agent with its configuration
	Initialize(ctx context.Context) error

	// Close closes the agent and cleans up resources
	Close() error

	// Event system - now handled by unified events system

	// GetBaseAgent returns the base agent for event listener attachment
	GetBaseAgent() *BaseAgent
}

// OutputFormat represents the output format for an agent
type OutputFormat string

const (
	OutputFormatText       OutputFormat = "text"
	OutputFormatMarkdown   OutputFormat = "markdown"
	OutputFormatStructured OutputFormat = "structured"
)

// LLMModel represents a single LLM configuration
type LLMModel struct {
	Provider string  `json:"provider"` // "anthropic", "openai", "bedrock", etc.
	ModelID  string  `json:"model_id"` // "claude-sonnet-4.5", "gpt-5", etc.

	// Auth per model
	APIKey *string `json:"api_key,omitempty"` // For OpenRouter, OpenAI, Anthropic, Vertex
	Region *string `json:"region,omitempty"`  // For Bedrock
}

// LLMConfig holds the primary and fallback LLM configurations
type LLMConfig struct {
	Primary   LLMModel   `json:"primary"`
	Fallbacks []LLMModel `json:"fallbacks"`
}

// OrchestratorAgentConfig defines the configuration for an orchestrator agent
type OrchestratorAgentConfig struct {
	// Unified LLM configuration (Primary + Fallbacks)
	LLMConfig LLMConfig `json:"llm_config"`

	// Temperature is kept separate as it may be overridden per-agent
	Temperature float64 `json:"temperature"`

	// API keys for different providers
	APIKeys *AgentAPIKeys `json:"api_keys,omitempty"`

	// Required Agent behavior
	Mode         AgentMode    `json:"mode" validate:"required"`
	OutputFormat OutputFormat `json:"output_format" validate:"required"`

	// Required MCP configuration
	ServerNames   []string `json:"server_names" validate:"required"`
	SelectedTools []string `json:"selected_tools,omitempty"` // Array of "server:tool" strings
	MCPConfigPath string   `json:"mcp_config_path" validate:"required"`
	ToolChoice    string   `json:"tool_choice" validate:"required"`
	MaxTurns      int      `json:"max_turns" validate:"required"`

	// Required settings
	MaxRetries int `json:"max_retries" validate:"required"`
	Timeout    int `json:"timeout" validate:"required"`    // in seconds
	RateLimit  int `json:"rate_limit" validate:"required"` // requests per minute

	// Optional instructions
	Instructions string `json:"instructions,omitempty"`

	// Optional fields
	Description         string                 `json:"description,omitempty"`
	UseStructuredOutput bool                   `json:"use_structured_output,omitempty"`
	CustomSettings      map[string]interface{} `json:"custom_settings,omitempty"`

	// Agent name (unique identifier for this agent instance)
	AgentName string `json:"agent_name,omitempty"` // e.g., "execution-agent-step-1-title"

	// Structured output configuration
	StructuredOutputSchema string `json:"structured_output_schema,omitempty"`
	StructuredOutputType   string `json:"structured_output_type,omitempty"` // "plan", "steps", "custom"

	// Code execution mode: When enabled, only virtual tools are added to LLM
	// MCP tools are accessed via generated Go code using discover_code_files and write_code
	UseCodeExecutionMode bool `json:"use_code_execution_mode,omitempty"`
	// Context offloading configuration
	EnableContextOffloading *bool `json:"enable_context_offloading,omitempty"` // Enable/disable context offloading (default: true if nil)

	// System prompt configuration
	OverwriteSystemPrompt *bool `json:"overwrite_system_prompt,omitempty"` // Overwrite (true) or append (false) system prompt during execution (default: false if nil)

	// Context summarization configuration
	EnableContextSummarization     bool    `json:"enable_context_summarization,omitempty"`       // Enable context summarization feature
	SummarizeOnTokenThreshold      bool    `json:"summarize_on_token_threshold,omitempty"`       // Enable token-based summarization trigger (percentage-based)
	TokenThresholdPercent          float64 `json:"token_threshold_percent,omitempty"`            // Percentage of context window to trigger summarization (0.0-1.0, default: 0.8 = 80%)
	SummarizeOnFixedTokenThreshold bool    `json:"summarize_on_fixed_token_threshold,omitempty"` // Enable fixed token-based summarization trigger
	FixedTokenThreshold            int     `json:"fixed_token_threshold,omitempty"`              // Fixed token threshold to trigger summarization (e.g., 200000 = 200k tokens, default: 200k)
	SummaryKeepLastMessages        int     `json:"summary_keep_last_messages,omitempty"`         // Number of recent messages to keep when summarizing (default: 4)

	// Context editing configuration
	EnableContextEditing        bool `json:"enable_context_editing,omitempty"`         // Enable context editing (dynamic context reduction)
	ContextEditingThreshold     int  `json:"context_editing_threshold,omitempty"`      // Token threshold for context editing (0 = use default: 100)
	ContextEditingTurnThreshold int  `json:"context_editing_turn_threshold,omitempty"` // Turn age threshold for context editing (0 = use default: 20)

	// MCP session ID for connection management
	// When set, MCP connections are shared across agents with the same session ID
	// Connections persist until CloseSession() is called (not when agent closes)
	MCPSessionID string `json:"mcp_session_id,omitempty"`

	// Runtime config overrides for MCP servers
	// Allows workflow-specific modifications like output directories per run
	RuntimeOverrides mcpclient.RuntimeOverrides `json:"runtime_overrides,omitempty"`
}

// CrossProviderFallback represents cross-provider fallback configuration
type CrossProviderFallback struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

// AgentAPIKeys represents API keys for different providers (for agent config)
type AgentAPIKeys struct {
	OpenRouter *string
	OpenAI     *string
	Anthropic  *string
	Vertex     *string
	Bedrock    *BedrockAgentConfig
}

// BedrockAgentConfig represents Bedrock-specific configuration (for agent config)
type BedrockAgentConfig struct {
	Region string
}

// NewOrchestratorAgentConfig creates a new agent configuration with minimal defaults
func NewOrchestratorAgentConfig(name string) *OrchestratorAgentConfig {
	return &OrchestratorAgentConfig{
		// LLMConfig.Primary must be set by caller
		LLMConfig:   LLMConfig{},
		Temperature: 0.0,

		Mode:           "", // Must be set by caller
		OutputFormat:   OutputFormatText,
		ServerNames:    []string{},
		MaxRetries:     0,
		Timeout:        0,
		RateLimit:      0,
		MCPConfigPath:  "", // Must be set by caller
		ToolChoice:     "", // Must be set by caller
		MaxTurns:       0,
		CustomSettings: make(map[string]interface{}),
	}
}

// LoadOrchestratorAgentConfigFromEnv creates a new agent configuration with values from environment variables
func LoadOrchestratorAgentConfigFromEnv(name string) *OrchestratorAgentConfig {
	config := NewOrchestratorAgentConfig(name)

	// Load LLM configuration from environment variables
	if provider := os.Getenv("ORCHESTRATOR_PROVIDER"); provider != "" {
		config.LLMConfig.Primary.Provider = provider
	}
	if model := os.Getenv("ORCHESTRATOR_MODEL"); model != "" {
		config.LLMConfig.Primary.ModelID = model
	}
	if tempStr := os.Getenv("ORCHESTRATOR_TEMPERATURE"); tempStr != "" {
		if temp, err := strconv.ParseFloat(tempStr, 64); err == nil {
			config.Temperature = temp
		}
	}

	if mode := os.Getenv("ORCHESTRATOR_MODE"); mode != "" {
		config.Mode = AgentMode(mode)
	}
	if maxRetriesStr := os.Getenv("ORCHESTRATOR_MAX_RETRIES"); maxRetriesStr != "" {
		if maxRetries, err := strconv.Atoi(maxRetriesStr); err == nil {
			config.MaxRetries = maxRetries
		}
	}
	if timeoutStr := os.Getenv("ORCHESTRATOR_TIMEOUT"); timeoutStr != "" {
		if timeout, err := strconv.Atoi(timeoutStr); err == nil {
			config.Timeout = timeout
		}
	}
	if rateLimitStr := os.Getenv("ORCHESTRATOR_RATE_LIMIT"); rateLimitStr != "" {
		if rateLimit, err := strconv.Atoi(rateLimitStr); err == nil {
			config.RateLimit = rateLimit
		}
	}
	if mcpConfigPath := os.Getenv("ORCHESTRATOR_MCP_CONFIG_PATH"); mcpConfigPath != "" {
		config.MCPConfigPath = mcpConfigPath
	}
	if toolChoice := os.Getenv("ORCHESTRATOR_TOOL_CHOICE"); toolChoice != "" {
		config.ToolChoice = toolChoice
	}
	if maxTurnsStr := os.Getenv("ORCHESTRATOR_MAX_TURNS"); maxTurnsStr != "" {
		if maxTurns, err := strconv.Atoi(maxTurnsStr); err == nil {
			config.MaxTurns = maxTurns
		}
	}

	return config
}

// ValidateOrchestratorAgentConfig validates that all required fields are provided
func ValidateOrchestratorAgentConfig(config *OrchestratorAgentConfig) error {
	var errors []string

	// Check required LLM configuration (using unified LLMConfig)
	if config.LLMConfig.Primary.Provider == "" {
		errors = append(errors, "LLMConfig.Primary.Provider is required")
	}
	if config.LLMConfig.Primary.ModelID == "" {
		errors = append(errors, "LLMConfig.Primary.ModelID is required")
	}

	// Check required agent behavior
	if config.Mode == "" {
		errors = append(errors, "Mode is required")
	}
	if config.OutputFormat == "" {
		errors = append(errors, "OutputFormat is required")
	}

	// Check required MCP configuration
	if len(config.ServerNames) == 0 {
		errors = append(errors, "ServerNames is required")
	}
	if config.MCPConfigPath == "" {
		errors = append(errors, "MCPConfigPath is required")
	}
	if config.ToolChoice == "" {
		errors = append(errors, "ToolChoice is required")
	}
	if config.MaxTurns == 0 {
		errors = append(errors, "MaxTurns is required")
	}

	// Check required settings
	if config.MaxRetries == 0 {
		errors = append(errors, "MaxRetries is required")
	}
	if config.Timeout == 0 {
		errors = append(errors, "Timeout is required")
	}
	if config.RateLimit == 0 {
		errors = append(errors, "RateLimit is required")
	}

	if len(errors) > 0 {
		return fmt.Errorf("OrchestratorAgentConfig validation failed: %s", strings.Join(errors, ", "))
	}

	return nil
}
