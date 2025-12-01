package agents

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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

// OrchestratorAgentConfig defines the configuration for an orchestrator agent
type OrchestratorAgentConfig struct {
	// Required LLM configuration
	Provider    string  `json:"provider" validate:"required"`
	Model       string  `json:"model" validate:"required"`
	Temperature float64 `json:"temperature" validate:"required"`

	// Detailed LLM configuration from frontend
	FallbackModels []FallbackModel `json:"fallback_models,omitempty"` // Unified fallback models with provider info
	APIKeys        *AgentAPIKeys   `json:"api_keys,omitempty"`
	Options        *LLMOptions     `json:"options,omitempty"` // LLM options (common + provider-specific)

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
	// Large output virtual tools configuration
	EnableLargeOutputVirtualTools *bool `json:"enable_large_output_virtual_tools,omitempty"` // Enable/disable large output tools (default: true if nil)

	// System prompt configuration
	OverwriteSystemPrompt *bool `json:"overwrite_system_prompt,omitempty"` // Overwrite (true) or append (false) system prompt during execution (default: false if nil)
}

// ═══════════════════════════════════════════════════════════════════
// FALLBACK MODEL - Unified fallback with provider and priority
// ═══════════════════════════════════════════════════════════════════

// FallbackModel represents a fallback model with provider and priority
type FallbackModel struct {
	ModelID  string      `json:"model_id"`
	Provider string      `json:"provider"`
	Priority int         `json:"priority"`
	Options  *LLMOptions `json:"options,omitempty"` // Override options for this fallback
}

// ═══════════════════════════════════════════════════════════════════
// LLM OPTIONS - Common + Provider-Specific Nested Structs
// ═══════════════════════════════════════════════════════════════════

// LLMOptions represents LLM configuration options (common + provider-specific)
type LLMOptions struct {
	// Common options (all providers support these)
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`

	// Provider-specific options (only one is used based on provider)
	OpenAI     *OpenAIOptions     `json:"openai,omitempty"`
	Anthropic  *AnthropicOptions  `json:"anthropic,omitempty"`
	Vertex     *VertexOptions     `json:"vertex,omitempty"`
	Bedrock    *BedrockLLMOptions `json:"bedrock,omitempty"`
	OpenRouter *OpenRouterOptions `json:"openrouter,omitempty"`
}

// OpenAIOptions represents OpenAI-specific LLM options
type OpenAIOptions struct {
	ReasoningEffort  *string  `json:"reasoning_effort,omitempty"` // "low", "medium", "high" (o3/o4 models)
	Seed             *int     `json:"seed,omitempty"`
	ResponseFormat   *string  `json:"response_format,omitempty"` // "text", "json_object"
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
}

// AnthropicOptions represents Anthropic-specific LLM options
type AnthropicOptions struct {
	ExtendedThinking     *bool `json:"extended_thinking,omitempty"`
	ThinkingBudgetTokens *int  `json:"thinking_budget_tokens,omitempty"`
}

// VertexOptions represents Vertex/Gemini-specific LLM options
type VertexOptions struct {
	ThinkingLevel   *string `json:"thinking_level,omitempty"`   // "none", "low", "medium", "high"
	SafetySettings  *string `json:"safety_settings,omitempty"`  // JSON string of safety config
	GroundingConfig *string `json:"grounding_config,omitempty"` // Search grounding
}

// BedrockLLMOptions represents Bedrock-specific LLM options
type BedrockLLMOptions struct {
	GuardrailIdentifier *string `json:"guardrail_identifier,omitempty"`
	GuardrailVersion    *string `json:"guardrail_version,omitempty"`
	InferenceProfile    *string `json:"inference_profile,omitempty"`
}

// OpenRouterOptions represents OpenRouter-specific LLM options
type OpenRouterOptions struct {
	Transforms []string `json:"transforms,omitempty"` // ["middle-out"]
	Route      *string  `json:"route,omitempty"`      // "fallback"
	Models     []string `json:"models,omitempty"`     // For multi-model routing
}

// ═══════════════════════════════════════════════════════════════════
// API KEYS - Provider credentials
// ═══════════════════════════════════════════════════════════════════

// AgentAPIKeys represents API keys for different providers (for agent config)
type AgentAPIKeys struct {
	OpenRouter *string             `json:"openrouter,omitempty"`
	OpenAI     *string             `json:"openai,omitempty"`
	Anthropic  *string             `json:"anthropic,omitempty"`
	Vertex     *string             `json:"vertex,omitempty"`
	Bedrock    *BedrockAgentConfig `json:"bedrock,omitempty"`
}

// BedrockAgentConfig represents Bedrock-specific configuration (for agent config)
type BedrockAgentConfig struct {
	Region string `json:"region"`
}

// NewOrchestratorAgentConfig creates a new agent configuration with minimal defaults
func NewOrchestratorAgentConfig(name string) *OrchestratorAgentConfig {
	return &OrchestratorAgentConfig{
		Provider:    "", // Must be set by caller
		Model:       "", // Must be set by caller
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

	// Load from environment variables if available
	if provider := os.Getenv("ORCHESTRATOR_PROVIDER"); provider != "" {
		config.Provider = provider
	}
	if model := os.Getenv("ORCHESTRATOR_MODEL"); model != "" {
		config.Model = model
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

	// Check required LLM configuration
	if config.Provider == "" {
		errors = append(errors, "Provider is required")
	}
	if config.Model == "" {
		errors = append(errors, "Model is required")
	}
	if config.Temperature == 0.0 {
		errors = append(errors, "Temperature is required")
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
