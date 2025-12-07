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
	PromptTokens          int
	CompletionTokens      int
	TotalTokens           int
	CacheTokens           int
	ReasoningTokens       int
	LLMCallCount          int
	CacheEnabledCallCount int
	CacheDiscountSum      float64 // Sum of cache discounts for averaging
}

// TokenUsageFile represents persisted token usage data per iteration
type TokenUsageFile struct {
	CreatedAt  time.Time                      `json:"created_at"`
	UpdatedAt  time.Time                      `json:"updated_at"`
	ByModel    map[string]*ModelTokenUsage    `json:"by_model"`
	ByStep     map[string]*StepTokenSummary   `json:"by_step"`
	ByStepType map[string]*StepTypeTokenUsage `json:"by_step_type"` // Aggregated by step type (execution, validation, learning)
}

// TokenUsageSummary represents total token usage across all models and steps
type TokenUsageSummary struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheTokens      int `json:"cache_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens"`
	LLMCallCount     int `json:"llm_call_count"`
}

// ModelTokenUsageInternal represents internal token usage accumulation (raw integers)
type ModelTokenUsageInternal struct {
	Provider         string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CacheTokens      int
	ReasoningTokens  int
	LLMCallCount     int
}

// ModelTokenUsage represents token usage for a specific model (JSON format)
// Token counts are stored in millions for easier cost calculations (pricing is typically per million)
type ModelTokenUsage struct {
	Provider         string  `json:"provider"`
	PromptTokens     float64 `json:"prompt_tokens"`     // in millions
	CompletionTokens float64 `json:"completion_tokens"` // in millions
	TotalTokens      float64 `json:"total_tokens"`      // in millions
	CacheTokens      float64 `json:"cache_tokens"`      // in millions
	ReasoningTokens  float64 `json:"reasoning_tokens"`  // in millions
	LLMCallCount     int     `json:"llm_call_count"`    // count, not in millions
}

// StepTokenSummary represents token usage summary for a workflow step
// Token counts are stored in millions for easier cost calculations (pricing is typically per million)
type StepTokenSummary struct {
	StepType         string  `json:"step_type"` // e.g., "execution", "validation", "learning"
	StepTitle        string  `json:"step_title,omitempty"`
	PromptTokens     float64 `json:"prompt_tokens"`     // in millions
	CompletionTokens float64 `json:"completion_tokens"` // in millions
	TotalTokens      float64 `json:"total_tokens"`      // in millions
	CacheTokens      float64 `json:"cache_tokens"`      // in millions
	ReasoningTokens  float64 `json:"reasoning_tokens"`  // in millions
	LLMCallCount     int     `json:"llm_call_count"`    // count, not in millions
}

// StepTypeTokenUsage represents aggregated token usage for a step type across all steps
// Token counts are stored in millions for easier cost calculations (pricing is typically per million)
type StepTypeTokenUsage struct {
	StepType         string  `json:"step_type"`         // e.g., "execution", "validation", "learning"
	PromptTokens     float64 `json:"prompt_tokens"`     // in millions
	CompletionTokens float64 `json:"completion_tokens"` // in millions
	TotalTokens      float64 `json:"total_tokens"`      // in millions
	CacheTokens      float64 `json:"cache_tokens"`      // in millions
	ReasoningTokens  float64 `json:"reasoning_tokens"`  // in millions
	LLMCallCount     int     `json:"llm_call_count"`    // count, not in millions
}
