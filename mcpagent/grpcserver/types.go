package grpcserver

import "time"

// CreateAgentRequest represents the request to create a new agent
type CreateAgentRequest struct {
	SessionID string      `json:"session_id,omitempty"`
	Config    AgentConfig `json:"config"`
}

// AgentConfig holds the configuration for creating an agent
type AgentConfig struct {
	Provider                   string                 `json:"provider,omitempty"`
	ModelID                    string                 `json:"model_id,omitempty"`
	Temperature                *float64               `json:"temperature,omitempty"`
	MaxTurns                   int                    `json:"max_turns,omitempty"`
	MCPConfigPath              string                 `json:"mcp_config_path,omitempty"`
	SelectedServers            []string               `json:"selected_servers,omitempty"`
	SelectedTools              []string               `json:"selected_tools,omitempty"`
	SystemPrompt               string                 `json:"system_prompt,omitempty"`
	EnableContextSummarization bool                   `json:"enable_context_summarization,omitempty"`
	EnableContextOffloading    bool                   `json:"enable_context_offloading,omitempty"`
	EnableStreaming            bool                   `json:"enable_streaming,omitempty"`
	CustomTools                []CustomToolDefinition `json:"custom_tools,omitempty"`
	APIKeys                    *ProviderAPIKeys       `json:"api_keys,omitempty"`
}

// ProviderAPIKeys holds API keys for different providers
type ProviderAPIKeys struct {
	OpenAI     *string         `json:"openai,omitempty"`
	Anthropic  *string         `json:"anthropic,omitempty"`
	OpenRouter *string         `json:"openrouter,omitempty"`
	Vertex     *string         `json:"vertex,omitempty"`
	Bedrock    *BedrockConfig  `json:"bedrock,omitempty"`
	Azure      *AzureAPIConfig `json:"azure,omitempty"`
}

// BedrockConfig holds AWS Bedrock-specific configuration
type BedrockConfig struct {
	Region string `json:"region,omitempty"`
}

// AzureAPIConfig holds Azure-specific configuration
type AzureAPIConfig struct {
	Endpoint   string `json:"endpoint"`
	APIKey     string `json:"api_key"`
	APIVersion string `json:"api_version,omitempty"`
	Region     string `json:"region,omitempty"`
}

// CustomToolDefinition represents a custom tool (for gRPC, callbacks are handled via the stream)
type CustomToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	TimeoutMs   int                    `json:"timeout_ms,omitempty"`
	Category    string                 `json:"category,omitempty"`
}

// CreateAgentResponse represents the response after creating an agent
type CreateAgentResponse struct {
	AgentID      string       `json:"agent_id"`
	SessionID    string       `json:"session_id"`
	Status       string       `json:"status"`
	CreatedAt    time.Time    `json:"created_at"`
	Capabilities Capabilities `json:"capabilities"`
}

// Capabilities describes what the agent can do
type Capabilities struct {
	Tools   []string `json:"tools"`
	Servers []string `json:"servers"`
}

// GetAgentResponse represents agent info
type GetAgentResponse struct {
	AgentID      string       `json:"agent_id"`
	SessionID    string       `json:"session_id"`
	Status       string       `json:"status"`
	CreatedAt    time.Time    `json:"created_at"`
	Capabilities Capabilities `json:"capabilities"`
	TokenUsage   TokenUsage   `json:"token_usage"`
}

// AskRequest represents a single question request
type AskRequest struct {
	Question string `json:"question"`
}

// AskResponse represents the response to a question
type AskResponse struct {
	Response   string     `json:"response"`
	TokenUsage TokenUsage `json:"token_usage"`
	DurationMs int64      `json:"duration_ms"`
}

// Message represents a conversation message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AskWithHistoryRequest represents a multi-turn conversation request
type AskWithHistoryRequest struct {
	Messages []Message `json:"messages"`
}

// AskWithHistoryResponse represents the response to a multi-turn conversation
type AskWithHistoryResponse struct {
	Response        string     `json:"response"`
	UpdatedMessages []Message  `json:"updated_messages"`
	TokenUsage      TokenUsage `json:"token_usage"`
	DurationMs      int64      `json:"duration_ms"`
}

// TokenUsage represents token consumption metrics
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheTokens      int `json:"cache_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	LLMCallCount     int `json:"llm_call_count"`
}

// TokenUsageWithPricing includes cost information
type TokenUsageWithPricing struct {
	TokenUsage
	Costs Costs `json:"costs"`
}

// Costs represents pricing in USD
type Costs struct {
	InputCost     float64 `json:"input_cost"`
	OutputCost    float64 `json:"output_cost"`
	ReasoningCost float64 `json:"reasoning_cost,omitempty"`
	CacheCost     float64 `json:"cache_cost,omitempty"`
	TotalCost     float64 `json:"total_cost"`
}

// DestroyAgentResponse represents the response after destroying an agent
type DestroyAgentResponse struct {
	AgentID   string `json:"agent_id"`
	Destroyed bool   `json:"destroyed"`
}

// ErrorResponse represents an API error
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error information
type ErrorDetail struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// ListAgentsResponse represents the list of active agents
type ListAgentsResponse struct {
	Agents []AgentSummary `json:"agents"`
}

// AgentSummary is a brief summary of an agent
type AgentSummary struct {
	AgentID   string    `json:"agent_id"`
	SessionID string    `json:"session_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
