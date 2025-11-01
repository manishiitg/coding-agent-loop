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
	GenerationInfo map[string]interface{}
	// FuncCall is a legacy field for backwards compatibility (deprecated, use ToolCalls instead)
	FuncCall *FunctionCall
}

// Usage represents token usage information
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// Tool represents a tool/function definition that can be called
type Tool struct {
	Type     string
	Function *FunctionDefinition
}

// FunctionDefinition represents a function definition with schema
type FunctionDefinition struct {
	Name        string
	Description string
	Parameters  interface{} // JSON schema (usually map[string]interface{})
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
	Metadata      map[string]interface{} // Provider-specific metadata
}

// CallOption is a function type for setting call options
type CallOption func(*CallOptions)
