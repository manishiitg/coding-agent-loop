package agents

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

// OrchestratorAgent defines the interface for all orchestrator agents
type OrchestratorAgent interface {
	// Execute executes the agent with the given template variables and returns the result
	Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error)

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
	FallbackModels        []string               `json:"fallback_models,omitempty"`
	CrossProviderFallback *CrossProviderFallback `json:"cross_provider_fallback,omitempty"`

	// Required Agent behavior
	Mode         AgentMode    `json:"mode" validate:"required"`
	OutputFormat OutputFormat `json:"output_format" validate:"required"`

	// Required MCP configuration
	ServerNames   []string `json:"server_names" validate:"required"`
	MCPConfigPath string   `json:"mcp_config_path" validate:"required"`
	ToolChoice    string   `json:"tool_choice" validate:"required"`
	MaxTurns      int      `json:"max_turns" validate:"required"`
	CacheOnly     bool     `json:"cache_only,omitempty"`

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

	// Structured output configuration
	StructuredOutputSchema string `json:"structured_output_schema,omitempty"`
	StructuredOutputType   string `json:"structured_output_type,omitempty"` // "plan", "steps", "custom"
}

// CrossProviderFallback represents cross-provider fallback configuration
type CrossProviderFallback struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

// NewOrchestratorAgentConfig creates a new agent configuration with minimal defaults
func NewOrchestratorAgentConfig(agentType, name string) *OrchestratorAgentConfig {
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
func LoadOrchestratorAgentConfigFromEnv(agentType, name string) *OrchestratorAgentConfig {
	config := NewOrchestratorAgentConfig(agentType, name)

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
