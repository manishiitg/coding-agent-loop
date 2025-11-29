package llmtypes

import "context"

// Model is the core interface for LLM implementations
type Model interface {
	GenerateContent(ctx context.Context, messages []MessageContent, options ...CallOption) (*ContentResponse, error)
}

// ChatMessageType represents the role of a chat message
type ChatMessageType string

const (
	ChatMessageTypeSystem   ChatMessageType = "system"
	ChatMessageTypeHuman    ChatMessageType = "human"
	ChatMessageTypeAI       ChatMessageType = "ai"
	ChatMessageTypeTool     ChatMessageType = "tool"
	ChatMessageTypeGeneric  ChatMessageType = "generic"
	ChatMessageTypeFunction ChatMessageType = "function"
)

// ContentPart is an interface for different types of message parts
type ContentPart interface{}

// TextContent represents a text content part
type TextContent struct {
	Text string
}

// ImageContent represents an image content part
// Supports both base64-encoded images and image URLs
type ImageContent struct {
	// SourceType is either "base64" or "url"
	SourceType string
	// MediaType is the MIME type (e.g., "image/jpeg", "image/png", "image/gif", "image/webp")
	// Required for base64 source type
	MediaType string
	// Data contains either:
	// - Base64-encoded image data (without data: URL prefix) for SourceType "base64"
	// - Image URL for SourceType "url"
	Data string
}

// StreamChunkType represents the type of a streaming chunk
type StreamChunkType string

const (
	StreamChunkTypeContent  StreamChunkType = "content"   // Text content chunk
	StreamChunkTypeToolCall StreamChunkType = "tool_call" // Complete tool call
)

// StreamChunk represents a single chunk in a streaming response
// It can contain either content text or a complete tool call
type StreamChunk struct {
	Type     StreamChunkType // Type of chunk: "content" or "tool_call"
	Content  string          // Text content (when Type is "content")
	ToolCall *ToolCall       // Complete tool call (when Type is "tool_call")
}

// ToolCall represents a tool/function call request
type ToolCall struct {
	ID               string
	Type             string
	FunctionCall     *FunctionCall
	ThoughtSignature string // For Gemini 3 Pro: thought signature from extra_content.google.thought_signature
}

// FunctionCall represents a function call with name and arguments
type FunctionCall struct {
	Name      string
	Arguments string // JSON string
}

// ToolCallResponse represents a tool/function call response
type ToolCallResponse struct {
	ToolCallID string
	Name       string // Name of the tool/function that was called
	Content    string
}

// MessageContent represents a message in the conversation
type MessageContent struct {
	Role  ChatMessageType
	Parts []ContentPart
}

// ContentResponse represents the response from an LLM
type ContentResponse struct {
	Choices []*ContentChoice
	Usage   *Usage `json:"usage,omitempty"` // Token usage information (LLM-agnostic)
}

// ContentChoice represents a single choice in the response
type ContentChoice struct {
	Content        string
	StopReason     string
	ToolCalls      []ToolCall
	GenerationInfo *GenerationInfo `json:"generation_info,omitempty"`
	// FuncCall is a legacy field for backwards compatibility (deprecated, use ToolCalls instead)
	FuncCall *FunctionCall
}

// Usage represents token usage information
type Usage struct {
	InputTokens     int
	OutputTokens    int
	TotalTokens     int
	ReasoningTokens *int `json:"reasoning_tokens,omitempty"` // Reasoning tokens (OpenAI gpt-5.1, etc.)
	ThoughtsTokens  *int `json:"thoughts_tokens,omitempty"`  // Thoughts tokens (Gemini 3 Pro, etc.)
	CacheTokens     *int `json:"cache_tokens,omitempty"`     // Cache tokens (sum of all cache-related tokens from various providers)
}

// GenerationInfo contains token usage and generation metadata from LLM providers.
// It supports multiple naming conventions used by different providers.
type GenerationInfo struct {
	// Primary token fields (used by most providers)
	InputTokens  *int `json:"input_tokens,omitempty"`
	OutputTokens *int `json:"output_tokens,omitempty"`
	TotalTokens  *int `json:"total_tokens,omitempty"`

	// Alternative naming conventions (OpenAI-style)
	PromptTokens     *int `json:"prompt_tokens,omitempty"`
	CompletionTokens *int `json:"completion_tokens,omitempty"`

	// Capitalized variants (some providers use capitalized keys)
	PromptTokensCap     *int `json:"PromptTokens,omitempty"`
	CompletionTokensCap *int `json:"CompletionTokens,omitempty"`
	InputTokensCap      *int `json:"InputTokens,omitempty"`
	OutputTokensCap     *int `json:"OutputTokens,omitempty"`
	TotalTokensCap      *int `json:"TotalTokens,omitempty"`

	// Optional/cache-related fields
	CachedContentTokens *int     `json:"cached_content_tokens,omitempty"`
	ToolUsePromptTokens *int     `json:"tool_use_prompt_tokens,omitempty"`
	ThoughtsTokens      *int     `json:"thoughts_tokens,omitempty"`
	ReasoningTokens     *int     `json:"ReasoningTokens,omitempty"`
	CacheDiscount       *float64 `json:"cache_discount,omitempty"`

	// Additional fields for extensibility (provider-specific)
	Additional map[string]interface{} `json:"-"`
}

// ExtractUsageFromGenerationInfo extracts token usage from GenerationInfo in an LLM-agnostic way.
// It handles different field naming conventions used by various providers (OpenAI, Anthropic, Bedrock, etc.)
// and returns a unified Usage struct. Returns nil if no token information is available.
func ExtractUsageFromGenerationInfo(genInfo *GenerationInfo) *Usage {
	if genInfo == nil {
		return nil
	}

	usage := &Usage{}

	// Extract input tokens (check multiple naming conventions in priority order)
	if genInfo.InputTokens != nil {
		usage.InputTokens = *genInfo.InputTokens
	} else if genInfo.InputTokensCap != nil {
		usage.InputTokens = *genInfo.InputTokensCap
	} else if genInfo.PromptTokens != nil {
		usage.InputTokens = *genInfo.PromptTokens
	} else if genInfo.PromptTokensCap != nil {
		usage.InputTokens = *genInfo.PromptTokensCap
	}

	// Extract output tokens (check multiple naming conventions in priority order)
	if genInfo.OutputTokens != nil {
		usage.OutputTokens = *genInfo.OutputTokens
	} else if genInfo.OutputTokensCap != nil {
		usage.OutputTokens = *genInfo.OutputTokensCap
	} else if genInfo.CompletionTokens != nil {
		usage.OutputTokens = *genInfo.CompletionTokens
	} else if genInfo.CompletionTokensCap != nil {
		usage.OutputTokens = *genInfo.CompletionTokensCap
	}

	// Extract total tokens (check multiple naming conventions in priority order)
	if genInfo.TotalTokens != nil {
		usage.TotalTokens = *genInfo.TotalTokens
	} else if genInfo.TotalTokensCap != nil {
		usage.TotalTokens = *genInfo.TotalTokensCap
	}

	// Extract reasoning tokens (OpenAI gpt-5.1 and similar models)
	if genInfo.ReasoningTokens != nil {
		usage.ReasoningTokens = genInfo.ReasoningTokens
	}

	// Extract thoughts tokens (Gemini 3 Pro and similar models)
	if genInfo.ThoughtsTokens != nil {
		usage.ThoughtsTokens = genInfo.ThoughtsTokens
	}

	// Extract cache tokens (from multiple sources and providers)
	cacheTokens := 0

	// 1. Check CachedContentTokens (OpenAI, Gemini, OpenRouter)
	if genInfo.CachedContentTokens != nil {
		cacheTokens += *genInfo.CachedContentTokens
	}

	// 2. Check Anthropic cache tokens from Additional map
	if genInfo.Additional != nil {
		// CacheReadInputTokens (tokens read from cache)
		if cacheRead, ok := genInfo.Additional["CacheReadInputTokens"]; ok {
			if cacheReadInt, ok := cacheRead.(int); ok {
				cacheTokens += cacheReadInt
			} else if cacheReadFloat, ok := cacheRead.(float64); ok {
				cacheTokens += int(cacheReadFloat)
			}
		}
		// Also check lowercase variant
		if cacheRead, ok := genInfo.Additional["cache_read_input_tokens"]; ok {
			if cacheReadInt, ok := cacheRead.(int); ok {
				cacheTokens += cacheReadInt
			} else if cacheReadFloat, ok := cacheRead.(float64); ok {
				cacheTokens += int(cacheReadFloat)
			}
		}

		// CacheCreationInputTokens (tokens used to create cache)
		if cacheCreate, ok := genInfo.Additional["CacheCreationInputTokens"]; ok {
			if cacheCreateInt, ok := cacheCreate.(int); ok {
				cacheTokens += cacheCreateInt
			} else if cacheCreateFloat, ok := cacheCreate.(float64); ok {
				cacheTokens += int(cacheCreateFloat)
			}
		}
		// Also check lowercase variant
		if cacheCreate, ok := genInfo.Additional["cache_creation_input_tokens"]; ok {
			if cacheCreateInt, ok := cacheCreate.(int); ok {
				cacheTokens += cacheCreateInt
			} else if cacheCreateFloat, ok := cacheCreate.(float64); ok {
				cacheTokens += int(cacheCreateFloat)
			}
		}
	}

	// Set cache tokens if any were found
	if cacheTokens > 0 {
		usage.CacheTokens = &cacheTokens
	}

	// Calculate total tokens if not provided by the provider
	// Note: TotalTokens from provider may already include reasoning/thoughts tokens
	if usage.TotalTokens == 0 && usage.InputTokens > 0 && usage.OutputTokens > 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		// If we have reasoning/thoughts tokens, they're typically already included in TotalTokens
		// from the provider, so we don't add them again here
	}

	// Return nil if no token information was found
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 {
		return nil
	}

	return usage
}

// PropertySchema represents a single property in a JSON schema
type PropertySchema struct {
	Type        string                 `json:"type,omitempty"`
	Description string                 `json:"description,omitempty"`
	Properties  map[string]interface{} `json:"properties,omitempty"`
	Items       interface{}            `json:"items,omitempty"`
	Enum        []interface{}          `json:"enum,omitempty"`
	Default     interface{}            `json:"default,omitempty"`
	Minimum     *float64               `json:"minimum,omitempty"`
	Maximum     *float64               `json:"maximum,omitempty"`
	MinLength   *int                   `json:"minLength,omitempty"`
	MaxLength   *int                   `json:"maxLength,omitempty"`
	Pattern     string                 `json:"pattern,omitempty"`
	Format      string                 `json:"format,omitempty"`
	// Additional fields for extensibility
	Additional map[string]interface{} `json:"-"`
}

// Parameters represents a JSON schema for function parameters.
// This follows the JSON Schema specification used by LLM providers for function definitions.
type Parameters struct {
	Type                 string                 `json:"type,omitempty"` // Typically "object"
	Properties           map[string]interface{} `json:"properties,omitempty"`
	Required             []string               `json:"required,omitempty"`
	AdditionalProperties interface{}            `json:"additionalProperties,omitempty"`
	PatternProperties    map[string]interface{} `json:"patternProperties,omitempty"`
	MinProperties        *int                   `json:"minProperties,omitempty"`
	MaxProperties        *int                   `json:"maxProperties,omitempty"`
	// Additional fields for extensibility
	Additional map[string]interface{} `json:"-"`
}

// UsageMetadata represents usage-related metadata for LLM requests
type UsageMetadata struct {
	Include bool `json:"include,omitempty"`
}

// Metadata contains provider-specific metadata for LLM generation requests.
// It supports structured fields for common use cases and extensibility for provider-specific needs.
type Metadata struct {
	// Structured fields for common metadata
	Usage *UsageMetadata `json:"usage,omitempty"`

	// Custom fields for provider-specific metadata
	Custom map[string]interface{} `json:"custom,omitempty"`
}

// Tool represents a tool/function definition that can be called
type Tool struct {
	Type     string
	Function *FunctionDefinition
}

// FunctionDefinition represents a function definition with schema
type FunctionDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  *Parameters `json:"parameters,omitempty"`
}

// ToolChoice represents tool choice configuration
type ToolChoice struct {
	Type     string // "auto", "none", "required"
	Function *FunctionName
	Any      bool
	None     bool
}

// FunctionName represents a specific function to call
type FunctionName struct {
	Name string
}

// JSONSchemaConfig holds configuration for JSON Schema structured outputs
type JSONSchemaConfig struct {
	Name        string                 // Schema name
	Description string                 // Schema description
	Schema      map[string]interface{} // JSON Schema definition
	Strict      bool                   // Whether to enforce strict schema compliance
}

// CallOptions holds all call options for LLM generation
type CallOptions struct {
	Model           string
	Temperature     float64
	MaxTokens       int
	JSONMode        bool
	JSONSchema      *JSONSchemaConfig // JSON Schema for structured outputs
	Tools           []Tool
	ToolChoice      *ToolChoice
	StreamChan      chan<- StreamChunk // Channel for streaming chunks (content and tool calls)
	Metadata        *Metadata          `json:"metadata,omitempty"` // Provider-specific metadata
	ReasoningEffort string             // Reasoning effort level: "minimal", "low", "medium", "high" (for gpt-5.1 and similar models)
	Verbosity       string             // Response verbosity level: "low", "medium", "high" (for reasoning models)
	ThinkingLevel   string             // Thinking level: "low", "high" (for Gemini 3 Pro)
}

// CallOption is a function type for setting call options
type CallOption func(*CallOptions)

// NewParameters creates a new Parameters struct from a map.
// This is a convenience function for converting maps to typed Parameters.
func NewParameters(paramsMap map[string]interface{}) *Parameters {
	if paramsMap == nil {
		return nil
	}

	params := &Parameters{}
	if typ, ok := paramsMap["type"].(string); ok {
		params.Type = typ
	}
	if properties, ok := paramsMap["properties"].(map[string]interface{}); ok {
		params.Properties = properties
	}
	if required, ok := paramsMap["required"].([]interface{}); ok {
		requiredStr := make([]string, 0, len(required))
		for _, r := range required {
			if s, ok := r.(string); ok {
				requiredStr = append(requiredStr, s)
			}
		}
		params.Required = requiredStr
	} else if required, ok := paramsMap["required"].([]string); ok {
		params.Required = required
	}
	if additionalProps, ok := paramsMap["additionalProperties"]; ok {
		params.AdditionalProperties = additionalProps
	}
	if patternProps, ok := paramsMap["patternProperties"].(map[string]interface{}); ok {
		params.PatternProperties = patternProps
	}
	if minProps, ok := paramsMap["minProperties"].(float64); ok {
		min := int(minProps)
		params.MinProperties = &min
	} else if minProps, ok := paramsMap["minProperties"].(int); ok {
		params.MinProperties = &minProps
	}
	if maxProps, ok := paramsMap["maxProperties"].(float64); ok {
		max := int(maxProps)
		params.MaxProperties = &max
	} else if maxProps, ok := paramsMap["maxProperties"].(int); ok {
		params.MaxProperties = &maxProps
	}
	// Store any additional fields
	params.Additional = make(map[string]interface{})
	for k, v := range paramsMap {
		switch k {
		case "type", "properties", "required", "additionalProperties", "patternProperties", "minProperties", "maxProperties":
			// Already handled
		default:
			params.Additional[k] = v
		}
	}
	return params
}

// Embedding represents a single embedding vector with metadata
type Embedding struct {
	Index     int       `json:"index"`            // Index of the embedding in the batch
	Embedding []float32 `json:"embedding"`        // The embedding vector
	Object    string    `json:"object,omitempty"` // Object type (usually "embedding")
}

// EmbeddingUsage represents token usage information for embedding generation
type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"` // Number of tokens in the input
	TotalTokens  int `json:"total_tokens"`  // Total tokens used
}

// EmbeddingResponse represents the response from an embedding generation request
type EmbeddingResponse struct {
	Embeddings []Embedding     `json:"embeddings"`       // Array of embeddings
	Model      string          `json:"model"`            // Model used for generation
	Usage      *EmbeddingUsage `json:"usage,omitempty"`  // Token usage information
	Object     string          `json:"object,omitempty"` // Object type (usually "list")
}

// EmbeddingOptions holds all options for embedding generation
type EmbeddingOptions struct {
	Model      string // Model ID (e.g., "text-embedding-3-small")
	Dimensions *int   // Optional dimensions parameter (for text-embedding-3 models)
}

// EmbeddingOption is a function type for setting embedding options
type EmbeddingOption func(*EmbeddingOptions)
