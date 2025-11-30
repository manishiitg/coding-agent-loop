package orchestrator

import (
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
