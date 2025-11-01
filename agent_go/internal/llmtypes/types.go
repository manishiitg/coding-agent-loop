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

// ToolCall represents a tool/function call request
type ToolCall struct {
	ID           string
	Type         string
	FunctionCall *FunctionCall
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
	InputTokens  int
	OutputTokens int
	TotalTokens  int
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

// CallOptions holds all call options for LLM generation
type CallOptions struct {
	Model         string
	Temperature   float64
	MaxTokens     int
	JSONMode      bool
	Tools         []Tool
	ToolChoice    *ToolChoice
	StreamingFunc func(string)
	Metadata      *Metadata `json:"metadata,omitempty"` // Provider-specific metadata
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
