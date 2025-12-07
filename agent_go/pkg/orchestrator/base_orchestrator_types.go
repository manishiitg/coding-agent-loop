package orchestrator

import (
	"time"

	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// LLMConfig represents the LLM configuration from frontend
type LLMConfig struct {
	Provider              string                        `json:"provider"`
	ModelID               string                        `json:"model_id"`
	FallbackModels        []string                      `json:"fallback_models"`
	CrossProviderFallback *agents.CrossProviderFallback `json:"cross_provider_fallback,omitempty"`
	APIKeys               *APIKeys                      `json:"api_keys,omitempty"`
}

// APIKeys represents API keys for different providers
type APIKeys struct {
	OpenRouter *string     `json:"openrouter,omitempty"`
	OpenAI     *string     `json:"openai,omitempty"`
	Anthropic  *string     `json:"anthropic,omitempty"`
	Vertex     *string     `json:"vertex,omitempty"`
	Bedrock    *BedrockKey `json:"bedrock,omitempty"`
}

// BedrockKey represents Bedrock configuration
type BedrockKey struct {
	Region string `json:"region"`
}

// OrchestratorType represents the type of orchestrator
type OrchestratorType string

const (
	OrchestratorTypePlanner  OrchestratorType = "planner"
	OrchestratorTypeWorkflow OrchestratorType = "workflow"
)

// StepTokenUsage represents accumulated token usage for a workflow step
type StepTokenUsage struct {
	InputTokens           int
	OutputTokens          int
	CacheTokens           int
	ReasoningTokens       int
	LLMCallCount          int
	CacheEnabledCallCount int
	CacheDiscountSum      float64 // Sum of cache discounts for averaging
}

// StepTokenData represents token data for a step to be persisted
type StepTokenData struct {
	Phase           string
	Step            int
	StepTitle       string
	InputTokens     int
	OutputTokens    int
	CacheTokens     int
	ReasoningTokens int
	LLMCallCount    int
}

// ModelTokenData represents token data for a model to be persisted
type ModelTokenData struct {
	ModelID         string
	Provider        string
	InputTokens     int
	OutputTokens    int
	CacheTokens     int
	ReasoningTokens int
	LLMCallCount    int
}

// TokenUsageFile represents persisted token usage data per iteration
type TokenUsageFile struct {
	CreatedAt      time.Time                              `json:"created_at"`
	UpdatedAt      time.Time                              `json:"updated_at"`
	ByModel        map[string]*ModelTokenUsage            `json:"by_model"`          // Aggregated by model (across all steps)
	ByStepAndModel map[string]map[string]*ModelTokenUsage `json:"by_step_and_model"` // Nested map: stepKey -> modelID -> token usage
}

// TokenUsageSummary represents total token usage across all models and steps
type TokenUsageSummary struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	CacheTokens     int `json:"cache_tokens"`
	ReasoningTokens int `json:"reasoning_tokens"`
	LLMCallCount    int `json:"llm_call_count"`
}

// ModelTokenUsage represents token usage for a specific model (JSON format)
// Stores both raw integers and string-formatted millions (with "M" suffix)
type ModelTokenUsage struct {
	Provider         string `json:"provider"`
	InputTokens      int    `json:"input_tokens"`       // raw count
	OutputTokens     int    `json:"output_tokens"`      // raw count
	InputTokensM     string `json:"input_tokens_m"`     // formatted as "17.016M"
	OutputTokensM    string `json:"output_tokens_m"`    // formatted as "0.116M"
	CacheTokens      int    `json:"cache_tokens"`       // raw count
	CacheTokensM     string `json:"cache_tokens_m"`     // formatted as "4.546M"
	ReasoningTokens  int    `json:"reasoning_tokens"`   // raw count
	ReasoningTokensM string `json:"reasoning_tokens_m"` // formatted as "0.000M"
	LLMCallCount     int    `json:"llm_call_count"`     // count
}

// StepTokenSummary represents token usage summary for a workflow step
// Stores both raw integers and string-formatted millions (with "M" suffix)
type StepTokenSummary struct {
	StepType         string `json:"step_type"` // e.g., "execution", "validation", "learning"
	StepTitle        string `json:"step_title,omitempty"`
	InputTokens      int    `json:"input_tokens"`       // raw count
	OutputTokens     int    `json:"output_tokens"`      // raw count
	InputTokensM     string `json:"input_tokens_m"`     // formatted as "17.016M"
	OutputTokensM    string `json:"output_tokens_m"`    // formatted as "0.116M"
	CacheTokens      int    `json:"cache_tokens"`       // raw count
	CacheTokensM     string `json:"cache_tokens_m"`     // formatted as "4.546M"
	ReasoningTokens  int    `json:"reasoning_tokens"`   // raw count
	ReasoningTokensM string `json:"reasoning_tokens_m"` // formatted as "0.000M"
	LLMCallCount     int    `json:"llm_call_count"`     // count
}

// StepTypeTokenUsage represents aggregated token usage for a step type across all steps
// Stores both raw integers and string-formatted millions (with "M" suffix)
type StepTypeTokenUsage struct {
	StepType         string `json:"step_type"`          // e.g., "execution", "validation", "learning"
	InputTokens      int    `json:"input_tokens"`       // raw count
	OutputTokens     int    `json:"output_tokens"`      // raw count
	InputTokensM     string `json:"input_tokens_m"`     // formatted as "17.016M"
	OutputTokensM    string `json:"output_tokens_m"`    // formatted as "0.116M"
	CacheTokens      int    `json:"cache_tokens"`       // raw count
	CacheTokensM     string `json:"cache_tokens_m"`     // formatted as "4.546M"
	ReasoningTokens  int    `json:"reasoning_tokens"`   // raw count
	ReasoningTokensM string `json:"reasoning_tokens_m"` // formatted as "0.000M"
	LLMCallCount     int    `json:"llm_call_count"`     // count
}
