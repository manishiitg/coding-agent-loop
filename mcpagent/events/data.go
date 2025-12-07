package events

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// AgentEventType represents the type of event in the agent flow
// Note: EventType constants are now defined in types.go
// DefaultLargeToolOutputThreshold is the default character threshold for considering tool output as "large"
const DefaultLargeToolOutputThreshold = 20000

type AgentEventType = EventType

// AgentEvent represents a generic agent event with typed data
type AgentEvent struct {
	Type          EventType `json:"type"`
	Timestamp     time.Time `json:"timestamp"`
	EventIndex    int       `json:"event_index"`
	TraceID       string    `json:"trace_id,omitempty"`
	SpanID        string    `json:"span_id,omitempty"`
	ParentID      string    `json:"parent_id,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"` // Links start/end event pairs
	Data          EventData `json:"data"`

	// NEW: Hierarchy fields for frontend tree structure
	HierarchyLevel int    `json:"hierarchy_level"`      // 0=root, 1=child, 2=grandchild
	SessionID      string `json:"session_id,omitempty"` // Group related events
	Component      string `json:"component,omitempty"`  // orchestrator, agent, llm, tool
}

// Getter methods to implement observability.AgentEvent interface
func (e *AgentEvent) GetType() string {
	return string(e.Type)
}

func (e *AgentEvent) GetCorrelationID() string {
	return e.CorrelationID
}

func (e *AgentEvent) GetTimestamp() time.Time {
	return e.Timestamp
}

func (e *AgentEvent) GetData() interface{} {
	return e.Data
}

func (e *AgentEvent) GetTraceID() string {
	return e.TraceID
}

func (e *AgentEvent) GetParentID() string {
	return e.ParentID
}

// GenericEventData is kept for backward compatibility during migration
// TODO: Migrate all usages to FallbackDetailEvent and remove this
type GenericEventData struct {
	BaseEventData
	Data map[string]interface{} `json:"data"`
}

func (e *GenericEventData) GetEventType() EventType {
	return FallbackAttemptEventType // Use fallback type for generic events
}

// FallbackDetailEvent represents detailed fallback operation events
// Use this for type-safe fallback tracking (preferred over GenericEventData)
type FallbackDetailEvent struct {
	BaseEventData
	Turn                  int      `json:"turn"`
	Operation             string   `json:"operation"`       // "fallback_attempt", "fallback_success", "fallback_failure", "all_failed"
	Stage                 string   `json:"stage,omitempty"` // "initialization", "generation"
	FallbackIndex         int      `json:"fallback_index,omitempty"`
	FallbackModel         string   `json:"fallback_model,omitempty"`
	FallbackProvider      string   `json:"fallback_provider,omitempty"`
	FallbackPhase         string   `json:"fallback_phase,omitempty"` // "same_provider", "cross_provider"
	TotalFallbacks        int      `json:"total_fallbacks,omitempty"`
	ErrorType             string   `json:"error_type,omitempty"` // "max_token", "throttling"
	Success               bool     `json:"success"`
	Error                 string   `json:"error,omitempty"`
	Duration              string   `json:"duration,omitempty"`
	Attempts              int      `json:"attempts,omitempty"`
	SuccessfulLLM         string   `json:"successful_llm,omitempty"`
	SuccessfulProvider    string   `json:"successful_provider,omitempty"`
	SuccessfulPhase       string   `json:"successful_phase,omitempty"`
	FailedModels          []string `json:"failed_models,omitempty"`
	SameProviderAttempts  int      `json:"same_provider_attempts,omitempty"`
	CrossProviderAttempts int      `json:"cross_provider_attempts,omitempty"`
}

func (e *FallbackDetailEvent) GetEventType() EventType {
	return FallbackAttemptEventType
}

// NewFallbackSuccessDetailEvent creates a fallback success detail event
func NewFallbackSuccessDetailEvent(turn int, fallbackModel, provider, phase, errorType string, attempts int, duration time.Duration) *FallbackDetailEvent {
	return &FallbackDetailEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:               turn,
		Operation:          "fallback_success",
		FallbackModel:      fallbackModel,
		FallbackProvider:   provider,
		FallbackPhase:      phase,
		ErrorType:          errorType,
		Success:            true,
		Attempts:           attempts,
		SuccessfulLLM:      fallbackModel,
		SuccessfulProvider: provider,
		SuccessfulPhase:    phase,
		Duration:           duration.String(),
	}
}

// NewFallbackAttemptDetailEvent creates a fallback attempt detail event
func NewFallbackAttemptDetailEvent(turn, index, total int, model, provider, phase, errorType string) *FallbackDetailEvent {
	return &FallbackDetailEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:             turn,
		Operation:        "fallback_attempt",
		FallbackIndex:    index,
		FallbackModel:    model,
		FallbackProvider: provider,
		FallbackPhase:    phase,
		TotalFallbacks:   total,
		ErrorType:        errorType,
	}
}

// NewFallbackFailureDetailEvent creates a fallback failure detail event
func NewFallbackFailureDetailEvent(turn int, model, provider, phase, stage, errorType, errMsg string, duration time.Duration) *FallbackDetailEvent {
	return &FallbackDetailEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:             turn,
		Operation:        "fallback_failure",
		Stage:            stage,
		FallbackModel:    model,
		FallbackProvider: provider,
		FallbackPhase:    phase,
		ErrorType:        errorType,
		Success:          false,
		Error:            errMsg,
		Duration:         duration.String(),
	}
}

// NewAllFallbacksFailedEvent creates an event when all fallbacks have failed
func NewAllFallbacksFailedEvent(turn int, errorType string, sameProviderAttempts, crossProviderAttempts int, failedModels []string, finalError string) *FallbackDetailEvent {
	return &FallbackDetailEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:                  turn,
		Operation:             "all_failed",
		ErrorType:             errorType,
		Success:               false,
		Error:                 finalError,
		SameProviderAttempts:  sameProviderAttempts,
		CrossProviderAttempts: crossProviderAttempts,
		FailedModels:          failedModels,
	}
}

// AgentStartEvent represents the start of an agent session
type AgentStartEvent struct {
	BaseEventData
	AgentType            string `json:"agent_type"`
	ModelID              string `json:"model_id"`
	Provider             string `json:"provider"`
	UseCodeExecutionMode bool   `json:"use_code_execution_mode,omitempty"`
}

func (e *AgentStartEvent) GetEventType() EventType {
	return AgentStart
}

// AgentEndEvent represents the end of an agent session
type AgentEndEvent struct {
	BaseEventData
	AgentType             string `json:"agent_type"`
	Success               bool   `json:"success"`
	Error                 string `json:"error,omitempty"`
	PromptTokens          int    `json:"prompt_tokens,omitempty"`
	CompletionTokens      int    `json:"completion_tokens,omitempty"`
	TotalTokens           int    `json:"total_tokens,omitempty"`
	CacheTokens           int    `json:"cache_tokens,omitempty"`
	ReasoningTokens       int    `json:"reasoning_tokens,omitempty"`
	LLMCallCount          int    `json:"llm_call_count,omitempty"`
	CacheEnabledCallCount int    `json:"cache_enabled_call_count,omitempty"`
}

func (e *AgentEndEvent) GetEventType() EventType {
	return AgentEnd
}

// AgentErrorEvent represents an agent error
type AgentErrorEvent struct {
	BaseEventData
	Error    string        `json:"error"`
	Turn     int           `json:"turn"`
	Context  string        `json:"context"`
	Duration time.Duration `json:"duration"`
}

func (e *AgentErrorEvent) GetEventType() EventType {
	return AgentError
}

// ConversationStartEvent represents the start of a conversation
type ConversationStartEvent struct {
	BaseEventData
	Question     string `json:"question"`
	SystemPrompt string `json:"system_prompt"`
	ToolsCount   int    `json:"tools_count"`
	Servers      string `json:"servers"`
}

func (e *ConversationStartEvent) GetEventType() EventType {
	return ConversationStart
}

// SerializedMessage represents a message that can be properly serialized to JSON
type SerializedMessage struct {
	Role  string        `json:"role"`
	Parts []MessagePart `json:"parts,omitempty"`
}

// ToolInfo represents information about a tool available to the LLM
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Server      string `json:"server"`
}

// ConvertToolsToToolInfo converts llmtypes.Tool to ToolInfo slice
func ConvertToolsToToolInfo(tools []llmtypes.Tool, toolToServer map[string]string) []ToolInfo {
	var toolInfos []ToolInfo
	for _, tool := range tools {
		serverName := "unknown"
		if mappedServer, exists := toolToServer[tool.Function.Name]; exists {
			serverName = mappedServer
		}
		toolInfos = append(toolInfos, ToolInfo{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Server:      serverName,
		})
	}
	return toolInfos
}

// MessagePart represents a serializable message part
type MessagePart struct {
	Type    string      `json:"type"`
	Content interface{} `json:"content"`
}

// ConversationTurnEvent represents a conversation turn
type ConversationTurnEvent struct {
	BaseEventData
	Turn           int                 `json:"turn"`
	Question       string              `json:"question"`
	MessagesCount  int                 `json:"messages_count"`
	HasToolCalls   bool                `json:"has_tool_calls"`
	ToolCallsCount int                 `json:"tool_calls_count"`
	Tools          []ToolInfo          `json:"tools,omitempty"`
	Messages       []SerializedMessage `json:"messages,omitempty"`
}

func (e *ConversationTurnEvent) GetEventType() EventType {
	return ConversationTurn
}

// serializeMessage converts llmtypes.MessageContent to SerializedMessage
func serializeMessage(msg llmtypes.MessageContent) SerializedMessage {
	serialized := SerializedMessage{
		Role:  string(msg.Role),
		Parts: []MessagePart{},
	}

	if msg.Parts != nil {
		for _, part := range msg.Parts {
			messagePart := MessagePart{}

			switch p := part.(type) {
			case llmtypes.TextContent:
				messagePart.Type = "text"
				messagePart.Content = p.Text
			case llmtypes.ImageContent:
				messagePart.Type = "image"
				// Store metadata only, not full base64 data (too large for events)
				imageMeta := map[string]interface{}{
					"source_type": p.SourceType,
					"media_type":  p.MediaType,
				}
				if p.SourceType == "url" {
					// Include URL since it's not as large as base64 data
					imageMeta["url"] = p.Data
				} else {
					// For base64, just indicate data length
					imageMeta["data_length"] = len(p.Data)
					imageMeta["data_preview"] = "base64_encoded_image_data"
				}
				messagePart.Content = imageMeta
			case llmtypes.ToolCall:
				messagePart.Type = "tool_call"
				messagePart.Content = map[string]interface{}{
					"id":            p.ID,
					"function_name": p.FunctionCall.Name,
					"function_args": p.FunctionCall.Arguments,
				}
			case llmtypes.ToolCallResponse:
				messagePart.Type = "tool_response"
				messagePart.Content = map[string]interface{}{
					"tool_call_id": p.ToolCallID,
					"content":      p.Content,
				}
			default:
				messagePart.Type = "unknown"
				messagePart.Content = fmt.Sprintf("%T: %+v", part, part)
			}

			serialized.Parts = append(serialized.Parts, messagePart)
		}
	}

	return serialized
}

// LLMGenerationStartEvent represents the start of LLM generation
type LLMGenerationStartEvent struct {
	BaseEventData
	Turn          int     `json:"turn"`
	ModelID       string  `json:"model_id"`
	Temperature   float64 `json:"temperature"`
	ToolsCount    int     `json:"tools_count"`
	MessagesCount int     `json:"messages_count"`
}

func (e *LLMGenerationStartEvent) GetEventType() EventType {
	return LLMGenerationStart
}

// LLMGenerationEndEvent represents the completion of LLM generation
type LLMGenerationEndEvent struct {
	BaseEventData
	Turn         int           `json:"turn"`
	Content      string        `json:"content"`
	ToolCalls    int           `json:"tool_calls"`
	Duration     time.Duration `json:"duration"`
	UsageMetrics UsageMetrics  `json:"usage_metrics"`
}

func (e *LLMGenerationEndEvent) GetEventType() EventType {
	return LLMGenerationEnd
}

// UsageMetrics represents LLM usage metrics
type UsageMetrics struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheTokens      int `json:"cache_tokens,omitempty"`     // Cache tokens (CachedContentTokens, CacheReadInputTokens, etc.)
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"` // Reasoning tokens (for models like o3)
}

// ToolCallStartEvent represents the start of a tool call
type ToolCallStartEvent struct {
	BaseEventData
	Turn       int        `json:"turn"`
	ToolName   string     `json:"tool_name"`
	ToolParams ToolParams `json:"tool_params"`
	ServerName string     `json:"server_name"`
}

func (e *ToolCallStartEvent) GetEventType() EventType {
	return ToolCallStart
}

// ToolParams represents tool call parameters
type ToolParams struct {
	Arguments string `json:"arguments"`
}

// ToolCallEndEvent represents the completion of a tool call
type ToolCallEndEvent struct {
	BaseEventData
	Turn       int           `json:"turn"`
	ToolName   string        `json:"tool_name"`
	Result     string        `json:"result"`
	Duration   time.Duration `json:"duration"`
	ServerName string        `json:"server_name"`
}

func (e *ToolCallEndEvent) GetEventType() EventType {
	return ToolCallEnd
}

// WorkspaceFileOperationEvent represents a workspace file operation
type WorkspaceFileOperationEvent struct {
	BaseEventData
	Operation  string `json:"operation"`        // "read", "update", "delete", "list", "patch", "move"
	Filepath   string `json:"filepath"`         // File path (empty for list operations)
	Folder     string `json:"folder,omitempty"` // Folder path (for list operations)
	Turn       int    `json:"turn"`
	ServerName string `json:"server_name"`
}

func (e *WorkspaceFileOperationEvent) GetEventType() EventType {
	return WorkspaceFileOperation
}

// NewWorkspaceFileOperationEvent creates a new WorkspaceFileOperationEvent
func NewWorkspaceFileOperationEvent(operation, filepath, folder string, turn int, serverName string) *WorkspaceFileOperationEvent {
	return &WorkspaceFileOperationEvent{
		BaseEventData: BaseEventData{
			Timestamp:      time.Now(),
			HierarchyLevel: 1,
			Component:      "tool",
		},
		Operation:  operation,
		Filepath:   filepath,
		Folder:     folder,
		Turn:       turn,
		ServerName: serverName,
	}
}

// MCPServerConnectionEvent represents MCP server connection
type MCPServerConnectionEvent struct {
	BaseEventData
	ServerName     string                 `json:"server_name"`
	ConfigPath     string                 `json:"config_path,omitempty"`
	Timeout        string                 `json:"timeout,omitempty"`
	Operation      string                 `json:"operation,omitempty"`
	Status         string                 `json:"status"`
	ToolsCount     int                    `json:"tools_count"`
	ConnectionTime time.Duration          `json:"connection_time"`
	Error          string                 `json:"error,omitempty"`
	ServerInfo     map[string]interface{} `json:"server_info,omitempty"`
}

func (e *MCPServerConnectionEvent) GetEventType() EventType {
	return MCPServerConnectionStart
}

// MCPServerDiscoveryEvent represents MCP server discovery
type MCPServerDiscoveryEvent struct {
	BaseEventData
	ServerName       string        `json:"server_name,omitempty"`
	Operation        string        `json:"operation,omitempty"`
	TotalServers     int           `json:"total_servers"`
	ConnectedServers int           `json:"connected_servers"`
	FailedServers    int           `json:"failed_servers"`
	DiscoveryTime    time.Duration `json:"discovery_time"`
	ToolCount        int           `json:"tool_count,omitempty"`
	Error            string        `json:"error,omitempty"`
}

func (e *MCPServerDiscoveryEvent) GetEventType() EventType {
	return MCPServerDiscovery
}

// MCPServerSelectionEvent represents MCP server selection for a query
type MCPServerSelectionEvent struct {
	BaseEventData
	Turn            int      `json:"turn"`
	SelectedServers []string `json:"selected_servers"`
	TotalServers    int      `json:"total_servers"`
	Source          string   `json:"source"` // "preset", "manual", "all"
	Query           string   `json:"query"`
}

func (e *MCPServerSelectionEvent) GetEventType() EventType {
	return MCPServerSelection
}

// ConversationEndEvent represents the end of a conversation
type ConversationEndEvent struct {
	BaseEventData
	Question string        `json:"question"`
	Result   string        `json:"result"`
	Duration time.Duration `json:"duration"`
	Turns    int           `json:"turns"`
	Status   string        `json:"status"`
	Error    string        `json:"error,omitempty"`
}

func (e *ConversationEndEvent) GetEventType() EventType {
	return ConversationEnd
}

// ConversationErrorEvent represents a conversation error
type ConversationErrorEvent struct {
	BaseEventData
	Question string        `json:"question"`
	Error    string        `json:"error"`
	Turn     int           `json:"turn"`
	Context  string        `json:"context"`
	Duration time.Duration `json:"duration"`
}

func (e *ConversationErrorEvent) GetEventType() EventType {
	return ConversationError
}

// LLMGenerationErrorEvent represents an LLM generation error
type LLMGenerationErrorEvent struct {
	BaseEventData
	Turn     int           `json:"turn"`
	ModelID  string        `json:"model_id"`
	Error    string        `json:"error"`
	Duration time.Duration `json:"duration"`
}

func (e *LLMGenerationErrorEvent) GetEventType() EventType {
	return LLMGenerationError
}

// ToolCallErrorEvent represents a tool call error
type ToolCallErrorEvent struct {
	BaseEventData
	Turn       int           `json:"turn"`
	ToolName   string        `json:"tool_name"`
	Error      string        `json:"error"`
	ServerName string        `json:"server_name"`
	Duration   time.Duration `json:"duration"`
}

func (e *ToolCallErrorEvent) GetEventType() EventType {
	return ToolCallError
}

// TokenUsageEvent represents detailed token usage information
type TokenUsageEvent struct {
	BaseEventData
	Turn             int           `json:"turn"`
	Operation        string        `json:"operation"`
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	TotalTokens      int           `json:"total_tokens"`
	ModelID          string        `json:"model_id"`
	Provider         string        `json:"provider"`
	CostEstimate     float64       `json:"cost_estimate,omitempty"`
	Duration         time.Duration `json:"duration"`
	Context          string        `json:"context"`
	// OpenRouter cache information
	CacheDiscount   float64 `json:"cache_discount,omitempty"`
	ReasoningTokens int     `json:"reasoning_tokens,omitempty"`
	// Raw GenerationInfo for debugging
	GenerationInfo map[string]interface{} `json:"generation_info,omitempty"`
}

func (e *TokenUsageEvent) GetEventType() EventType {
	return TokenUsageEventType
}

// ErrorDetailEvent represents detailed error information
type ErrorDetailEvent struct {
	BaseEventData
	Turn        int           `json:"turn"`
	Error       string        `json:"error"`
	ErrorType   string        `json:"error_type"`
	Component   string        `json:"component"`
	Operation   string        `json:"operation"`
	Context     string        `json:"context"`
	Stack       string        `json:"stack,omitempty"`
	Duration    time.Duration `json:"duration"`
	Recoverable bool          `json:"recoverable"`
	RetryCount  int           `json:"retry_count,omitempty"`
}

func (e *ErrorDetailEvent) GetEventType() EventType {
	return ErrorDetailEventType
}

// ToolContext represents tool information for LLM context
type ToolContext struct {
	ToolName   string `json:"tool_name"`
	ServerName string `json:"server_name"`
	Arguments  string `json:"arguments,omitempty"`
	Result     string `json:"result,omitempty"`
	Status     string `json:"status"`
}

// SystemPromptEvent represents a system prompt being used
type SystemPromptEvent struct {
	BaseEventData
	Content string `json:"content"`
	Turn    int    `json:"turn"`
}

func (e *SystemPromptEvent) GetEventType() EventType {
	return SystemPromptEventType
}

// ToolOutputEvent represents tool output data
type ToolOutputEvent struct {
	BaseEventData
	Turn       int    `json:"turn"`
	ToolName   string `json:"tool_name"`
	Output     string `json:"output"`
	ServerName string `json:"server_name"`
	Size       int    `json:"size"`
}

func (e *ToolOutputEvent) GetEventType() EventType {
	return ToolOutputEventType
}

// ToolResponseEvent represents a tool response
type ToolResponseEvent struct {
	BaseEventData
	Turn       int    `json:"turn"`
	ToolName   string `json:"tool_name"`
	Response   string `json:"response"`
	ServerName string `json:"server_name"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

func (e *ToolResponseEvent) GetEventType() EventType {
	return ToolResponseEventType
}

// UserMessageEvent represents a user message
type UserMessageEvent struct {
	BaseEventData
	Turn    int    `json:"turn"`
	Content string `json:"content"`
	Role    string `json:"role"`
}

func (e *UserMessageEvent) GetEventType() EventType {
	return UserMessageEventType
}

// NewUserMessageEvent creates a new UserMessageEvent
func NewUserMessageEvent(turn int, content, role string) *UserMessageEvent {
	return &UserMessageEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:    turn,
		Content: content,
		Role:    role,
	}
}

// generateEventID generates a unique event ID
// GenerateEventID creates a unique event ID
func GenerateEventID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to time-based ID if random read fails
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// NewAgentEvent creates a new AgentEvent with typed data
func NewAgentEvent(eventData EventData) *AgentEvent {
	return &AgentEvent{
		Type:           eventData.GetEventType(),
		Timestamp:      time.Now(),
		Data:           eventData,
		HierarchyLevel: 0, // Default to root level
	}
}

// NewAgentEndEvent function removed - no longer needed

// NewAgentStartEvent creates a new AgentStartEvent
func NewAgentStartEvent(agentType, modelID, provider string, useCodeExecutionMode bool) *AgentStartEvent {
	return &AgentStartEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType:            agentType,
		ModelID:              modelID,
		Provider:             provider,
		UseCodeExecutionMode: useCodeExecutionMode,
	}
}

// NewAgentStartEventWithHierarchy creates a new AgentStartEvent with hierarchy fields
func NewAgentStartEventWithHierarchy(agentType, modelID, provider, parentID string, level int, sessionID, component string, useCodeExecutionMode bool) *AgentStartEvent {
	return &AgentStartEvent{
		BaseEventData: BaseEventData{
			Timestamp:      time.Now(),
			ParentID:       parentID,
			HierarchyLevel: level,
			SessionID:      sessionID,
			Component:      component,
		},
		AgentType:            agentType,
		ModelID:              modelID,
		Provider:             provider,
		UseCodeExecutionMode: useCodeExecutionMode,
	}
}

// NewAgentEndEvent creates a new AgentEndEvent
func NewAgentEndEvent(agentType string, success bool, error string) *AgentEndEvent {
	return &AgentEndEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType: agentType,
		Success:   success,
		Error:     error,
	}
}

// NewAgentEndEventWithHierarchy creates a new AgentEndEvent with hierarchy fields
func NewAgentEndEventWithHierarchy(agentType string, success bool, error, parentID string, level int, sessionID, component string) *AgentEndEvent {
	return &AgentEndEvent{
		BaseEventData: BaseEventData{
			Timestamp:      time.Now(),
			ParentID:       parentID,
			HierarchyLevel: level,
			SessionID:      sessionID,
			Component:      component,
		},
		AgentType: agentType,
		Success:   success,
		Error:     error,
	}
}

// NewAgentEndEventWithTokens creates a new AgentEndEvent with token usage information
func NewAgentEndEventWithTokens(agentType string, success bool, error string, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount int) *AgentEndEvent {
	return &AgentEndEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType:             agentType,
		Success:               success,
		Error:                 error,
		PromptTokens:          promptTokens,
		CompletionTokens:      completionTokens,
		TotalTokens:           totalTokens,
		CacheTokens:           cacheTokens,
		ReasoningTokens:       reasoningTokens,
		LLMCallCount:          llmCallCount,
		CacheEnabledCallCount: cacheEnabledCallCount,
	}
}

// NewAgentErrorEvent creates a new AgentErrorEvent
func NewAgentErrorEvent(error string, turn int, context string, duration time.Duration) *AgentErrorEvent {
	return &AgentErrorEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Error:    error,
		Turn:     turn,
		Context:  context,
		Duration: duration,
	}
}

// NewConversationStartEvent creates a new ConversationStartEvent
func NewConversationStartEvent(question, systemPrompt string, toolsCount int, servers string) *ConversationStartEvent {
	return &ConversationStartEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
			EventID:   GenerateEventID(),
		},
		Question:     question,
		SystemPrompt: systemPrompt,
		ToolsCount:   toolsCount,
		Servers:      servers,
	}
}

// NewConversationStartEventWithCorrelation creates a new ConversationStartEvent with correlation data
func NewConversationStartEventWithCorrelation(question, systemPrompt string, toolsCount int, servers string, traceID, parentID string) *ConversationStartEvent {
	return &ConversationStartEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
			TraceID:   traceID,
			EventID:   GenerateEventID(),
			ParentID:  parentID,
		},
		Question:     question,
		SystemPrompt: systemPrompt,
		ToolsCount:   toolsCount,
		Servers:      servers,
	}
}

// NewConversationEndEvent creates a new ConversationEndEvent
func NewConversationEndEvent(question, result string, duration time.Duration, turns int, status, error string) *ConversationEndEvent {
	return &ConversationEndEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Question: question,
		Result:   result,
		Duration: duration,
		Turns:    turns,
		Status:   status,
		Error:    error,
	}
}

// NewConversationErrorEvent creates a new ConversationErrorEvent
func NewConversationErrorEvent(question, error string, turn int, context string, duration time.Duration) *ConversationErrorEvent {
	return &ConversationErrorEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Question: question,
		Error:    error,
		Turn:     turn,
		Context:  context,
		Duration: duration,
	}
}

// NewConversationTurnEvent creates a new ConversationTurnEvent
func NewConversationTurnEvent(turn int, question string, messagesCount int, hasToolCalls bool, toolCallsCount int, tools []ToolInfo, messages []llmtypes.MessageContent) *ConversationTurnEvent {
	// Convert llmtypes.MessageContent to SerializedMessage, filtering out system messages
	var serializedMessages []SerializedMessage
	for _, msg := range messages {
		// Skip system messages
		if msg.Role == llmtypes.ChatMessageTypeSystem {
			continue
		}
		serializedMessages = append(serializedMessages, serializeMessage(msg))
	}

	return &ConversationTurnEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:           turn,
		Question:       question,
		MessagesCount:  messagesCount,
		HasToolCalls:   hasToolCalls,
		ToolCallsCount: toolCallsCount,
		Tools:          tools,
		Messages:       serializedMessages,
	}
}

// NewLLMGenerationStartEvent creates a new LLMGenerationStartEvent
func NewLLMGenerationStartEvent(turn int, modelID string, temperature float64, toolsCount, messagesCount int) *LLMGenerationStartEvent {
	return &LLMGenerationStartEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
			EventID:   GenerateEventID(),
		},
		Turn:          turn,
		ModelID:       modelID,
		Temperature:   temperature,
		ToolsCount:    toolsCount,
		MessagesCount: messagesCount,
	}
}

// NewLLMGenerationStartEventWithCorrelation creates a new LLMGenerationStartEvent with correlation data
func NewLLMGenerationStartEventWithCorrelation(turn int, modelID string, temperature float64, toolsCount, messagesCount int, traceID, parentID string) *LLMGenerationStartEvent {
	return &LLMGenerationStartEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
			TraceID:   traceID,
			EventID:   GenerateEventID(),
			ParentID:  parentID,
		},
		Turn:          turn,
		ModelID:       modelID,
		Temperature:   temperature,
		ToolsCount:    toolsCount,
		MessagesCount: messagesCount,
	}
}

// NewLLMGenerationEndEvent creates a new LLMGenerationEndEvent
func NewLLMGenerationEndEvent(turn int, content string, toolCalls int, duration time.Duration, usageMetrics UsageMetrics) *LLMGenerationEndEvent {
	return &LLMGenerationEndEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:         turn,
		Content:      content,
		ToolCalls:    toolCalls,
		Duration:     duration,
		UsageMetrics: usageMetrics,
	}
}

// NewLLMGenerationErrorEvent creates a new LLMGenerationErrorEvent
func NewLLMGenerationErrorEvent(turn int, modelID string, error string, duration time.Duration) *LLMGenerationErrorEvent {
	return &LLMGenerationErrorEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
			EventID:   GenerateEventID(),
		},
		Turn:     turn,
		ModelID:  modelID,
		Error:    error,
		Duration: duration,
	}
}

// NewToolCallStartEvent creates a new ToolCallStartEvent
func NewToolCallStartEvent(turn int, toolName string, toolParams ToolParams, serverName string, spanID string) *ToolCallStartEvent {
	return &ToolCallStartEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
			EventID:   GenerateEventID(),
			SpanID:    spanID,
		},
		Turn:       turn,
		ToolName:   toolName,
		ToolParams: toolParams,
		ServerName: serverName,
	}
}

// NewToolCallStartEventWithCorrelation creates a new ToolCallStartEvent with correlation data
func NewToolCallStartEventWithCorrelation(turn int, toolName string, toolParams ToolParams, serverName string, traceID, parentID string) *ToolCallStartEvent {
	return &ToolCallStartEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
			TraceID:   traceID,
			EventID:   GenerateEventID(),
			ParentID:  parentID,
		},
		Turn:       turn,
		ToolName:   toolName,
		ToolParams: toolParams,
		ServerName: serverName,
	}
}

// NewToolCallEndEvent creates a new ToolCallEndEvent
func NewToolCallEndEvent(turn int, toolName, result, serverName string, duration time.Duration, spanID string) *ToolCallEndEvent {
	return &ToolCallEndEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
			SpanID:    spanID,
		},
		Turn:       turn,
		ToolName:   toolName,
		Result:     result,
		Duration:   duration,
		ServerName: serverName,
	}
}

// NewToolCallErrorEvent creates a new ToolCallErrorEvent
func NewToolCallErrorEvent(turn int, toolName, error string, serverName string, duration time.Duration) *ToolCallErrorEvent {
	return &ToolCallErrorEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:       turn,
		ToolName:   toolName,
		Error:      error,
		ServerName: serverName,
		Duration:   duration,
	}
}

// NewMCPServerConnectionEvent creates a new MCPServerConnectionEvent
func NewMCPServerConnectionEvent(serverName, status string, toolsCount int, connectionTime time.Duration, error string) *MCPServerConnectionEvent {
	return &MCPServerConnectionEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		ServerName:     serverName,
		Status:         status,
		ToolsCount:     toolsCount,
		ConnectionTime: connectionTime,
		Error:          error,
	}
}

// NewMCPServerDiscoveryEvent creates a new MCPServerDiscoveryEvent
func NewMCPServerDiscoveryEvent(totalServers, connectedServers, failedServers int, discoveryTime time.Duration) *MCPServerDiscoveryEvent {
	return &MCPServerDiscoveryEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		TotalServers:     totalServers,
		ConnectedServers: connectedServers,
		FailedServers:    failedServers,
		DiscoveryTime:    discoveryTime,
	}
}

// NewMCPServerSelectionEvent creates a new MCPServerSelectionEvent
func NewMCPServerSelectionEvent(turn int, selectedServers []string, totalServers int, source, query string) *MCPServerSelectionEvent {
	return &MCPServerSelectionEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:            turn,
		SelectedServers: selectedServers,
		TotalServers:    totalServers,
		Source:          source,
		Query:           query,
	}
}

// NewTokenUsageEvent creates a new TokenUsageEvent
func NewTokenUsageEvent(turn int, operation, modelID, provider string, promptTokens, completionTokens, totalTokens int, duration time.Duration, context string) *TokenUsageEvent {
	return &TokenUsageEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:             turn,
		Operation:        operation,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		ModelID:          modelID,
		Provider:         provider,
		Duration:         duration,
		Context:          context,
	}
}

// NewTokenUsageEventWithCache creates a new TokenUsageEvent with cache information
func NewTokenUsageEventWithCache(turn int, operation, modelID, provider string, promptTokens, completionTokens, totalTokens int, duration time.Duration, context string, cacheDiscount float64, reasoningTokens int, generationInfo map[string]interface{}) *TokenUsageEvent {
	return &TokenUsageEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:             turn,
		Operation:        operation,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		ModelID:          modelID,
		Provider:         provider,
		Duration:         duration,
		Context:          context,
		CacheDiscount:    cacheDiscount,
		ReasoningTokens:  reasoningTokens,
		GenerationInfo:   generationInfo,
	}
}

// NewErrorDetailEvent creates a new ErrorDetailEvent
func NewErrorDetailEvent(turn int, error, errorType, component, operation, context string, duration time.Duration, recoverable bool, retryCount int) *ErrorDetailEvent {
	return &ErrorDetailEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:        turn,
		Error:       error,
		ErrorType:   errorType,
		Component:   component,
		Operation:   operation,
		Context:     context,
		Duration:    duration,
		Recoverable: recoverable,
		RetryCount:  retryCount,
	}
}

// NewSystemPromptEvent creates a new SystemPromptEvent
func NewSystemPromptEvent(content string, turn int) *SystemPromptEvent {
	return &SystemPromptEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Content: content,
		Turn:    turn,
	}
}

// NewToolOutputEvent creates a new ToolOutputEvent
func NewToolOutputEvent(turn int, toolName, output, serverName string, size int) *ToolOutputEvent {
	return &ToolOutputEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:       turn,
		ToolName:   toolName,
		Output:     output,
		ServerName: serverName,
		Size:       size,
	}
}

// NewToolResponseEvent creates a new ToolResponseEvent
func NewToolResponseEvent(turn int, toolName, response, serverName, status string) *ToolResponseEvent {
	return &ToolResponseEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:       turn,
		ToolName:   toolName,
		Response:   response,
		ServerName: serverName,
		Status:     status,
	}
}

// LargeToolOutputDetectedEvent represents detection of a large tool output
type LargeToolOutputDetectedEvent struct {
	BaseEventData
	ToolName        string `json:"tool_name"`
	OutputSize      int    `json:"output_size"`
	Threshold       int    `json:"threshold"`
	OutputFolder    string `json:"output_folder"`
	ServerAvailable bool   `json:"server_available"`
}

func (e *LargeToolOutputDetectedEvent) GetEventType() EventType {
	return LargeToolOutputDetectedEventType
}

// LargeToolOutputFileWrittenEvent represents successful file writing of large tool output
type LargeToolOutputFileWrittenEvent struct {
	BaseEventData
	ToolName     string `json:"tool_name"`
	FilePath     string `json:"file_path"`
	OutputSize   int    `json:"output_size"`
	FileSize     int64  `json:"file_size"`
	OutputFolder string `json:"output_folder"`
	Preview      string `json:"preview,omitempty"` // First 500 lines for observability
}

func (e *LargeToolOutputFileWrittenEvent) GetEventType() EventType {
	return LargeToolOutputFileWrittenEventType
}

// LargeToolOutputFileWriteErrorEvent represents error in writing large tool output to file
type LargeToolOutputFileWriteErrorEvent struct {
	BaseEventData
	ToolName     string `json:"tool_name"`
	Error        string `json:"error"`
	OutputSize   int    `json:"output_size"`
	OutputFolder string `json:"output_folder"`
	FallbackUsed bool   `json:"fallback_used"`
}

func (e *LargeToolOutputFileWriteErrorEvent) GetEventType() EventType {
	return LargeToolOutputFileWriteErrorEventType
}

// LargeToolOutputServerUnavailableEvent represents when server is not available for large tool output handling
type LargeToolOutputServerUnavailableEvent struct {
	BaseEventData
	ToolName   string `json:"tool_name"`
	OutputSize int    `json:"output_size"`
	Threshold  int    `json:"threshold"`
	ServerName string `json:"server_name"`
	Reason     string `json:"reason"`
}

func (e *LargeToolOutputServerUnavailableEvent) GetEventType() EventType {
	return LargeToolOutputServerUnavailableEventType
}

// Constructor functions for large tool output events
func NewLargeToolOutputDetectedEvent(toolName string, outputSize int, outputFolder string) *LargeToolOutputDetectedEvent {
	return &LargeToolOutputDetectedEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		ToolName:        toolName,
		OutputSize:      outputSize,
		Threshold:       DefaultLargeToolOutputThreshold, // Default threshold
		OutputFolder:    outputFolder,
		ServerAvailable: true, // Will be set by caller
	}
}

func NewLargeToolOutputFileWrittenEvent(toolName, filePath string, outputSize int, preview string) *LargeToolOutputFileWrittenEvent {
	return &LargeToolOutputFileWrittenEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		ToolName:     toolName,
		FilePath:     filePath,
		OutputSize:   outputSize,
		FileSize:     0,                    // Will be set by caller if needed
		OutputFolder: "tool_output_folder", // Default
		Preview:      preview,
	}
}

func NewLargeToolOutputFileWriteErrorEvent(toolName, error string, outputSize int) *LargeToolOutputFileWriteErrorEvent {
	return &LargeToolOutputFileWriteErrorEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		ToolName:     toolName,
		Error:        error,
		OutputSize:   outputSize,
		OutputFolder: "tool_output_folder", // Default
		FallbackUsed: true,
	}
}

func NewLargeToolOutputServerUnavailableEvent(toolName string, outputSize int, serverName, reason string) *LargeToolOutputServerUnavailableEvent {
	return &LargeToolOutputServerUnavailableEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		ToolName:   toolName,
		OutputSize: outputSize,
		Threshold:  DefaultLargeToolOutputThreshold, // Default threshold
		ServerName: serverName,
		Reason:     reason,
	}
}

// ModelChangeEvent represents a model change event
type ModelChangeEvent struct {
	BaseEventData
	Turn       int    `json:"turn"`
	OldModelID string `json:"old_model_id"`
	NewModelID string `json:"new_model_id"`
	Reason     string `json:"reason"`
	Provider   string `json:"provider"`
	Duration   string `json:"duration"`
}

func (e *ModelChangeEvent) GetEventType() EventType {
	return ModelChangeEventType
}

// FallbackModelUsedEvent represents when a fallback model is successfully used
type FallbackModelUsedEvent struct {
	BaseEventData
	Turn          int    `json:"turn"`
	OriginalModel string `json:"original_model"`
	FallbackModel string `json:"fallback_model"`
	Provider      string `json:"provider"`
	Reason        string `json:"reason"`
	Duration      string `json:"duration"`
}

func (e *FallbackModelUsedEvent) GetEventType() EventType {
	return FallbackModelUsedEventType
}

// ThrottlingDetectedEvent represents when throttling is detected
type ThrottlingDetectedEvent struct {
	BaseEventData
	Turn        int    `json:"turn"`
	ModelID     string `json:"model_id"`
	Provider    string `json:"provider"`
	Attempt     int    `json:"attempt"`
	MaxAttempts int    `json:"max_attempts"`
	Duration    string `json:"duration"`
	ErrorType   string `json:"error_type,omitempty"`  // "throttling", "empty_content", "connection_error", etc.
	RetryDelay  string `json:"retry_delay,omitempty"` // Wait time before retry (e.g., "22.5s")
}

func (e *ThrottlingDetectedEvent) GetEventType() EventType {
	return ThrottlingDetectedEventType
}

// TokenLimitExceededEvent represents when token limits are exceeded
type TokenLimitExceededEvent struct {
	BaseEventData
	Turn          int    `json:"turn"`
	ModelID       string `json:"model_id"`
	Provider      string `json:"provider"`
	TokenType     string `json:"token_type"` // "input", "output", "total"
	CurrentTokens int    `json:"current_tokens"`
	MaxTokens     int    `json:"max_tokens"`
	Duration      string `json:"duration"`
}

func (e *TokenLimitExceededEvent) GetEventType() EventType {
	return TokenLimitExceededEventType
}

// NewModelChangeEvent creates a new ModelChangeEvent
func NewModelChangeEvent(turn int, oldModelID, newModelID, reason, provider string, duration time.Duration) *ModelChangeEvent {
	return &ModelChangeEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:       turn,
		OldModelID: oldModelID,
		NewModelID: newModelID,
		Reason:     reason,
		Provider:   provider,
		Duration:   duration.String(),
	}
}

// NewFallbackModelUsedEvent creates a new FallbackModelUsedEvent
func NewFallbackModelUsedEvent(turn int, originalModel, fallbackModel, provider, reason string, duration time.Duration) *FallbackModelUsedEvent {
	return &FallbackModelUsedEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:          turn,
		OriginalModel: originalModel,
		FallbackModel: fallbackModel,
		Provider:      provider,
		Reason:        reason,
		Duration:      duration.String(),
	}
}

// NewThrottlingDetectedEvent creates a new ThrottlingDetectedEvent
// errorType can be "throttling", "empty_content", "connection_error", etc.
// retryDelay is the wait time before retry (e.g., "22.5s"), optional
func NewThrottlingDetectedEvent(turn int, modelID, provider string, attempt, maxAttempts int, duration time.Duration, errorType string, retryDelay time.Duration) *ThrottlingDetectedEvent {
	event := &ThrottlingDetectedEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:        turn,
		ModelID:     modelID,
		Provider:    provider,
		Attempt:     attempt,
		MaxAttempts: maxAttempts,
		Duration:    duration.String(),
	}
	if errorType != "" {
		event.ErrorType = errorType
	}
	if retryDelay > 0 {
		event.RetryDelay = retryDelay.String()
	}
	return event
}

// NewTokenLimitExceededEvent creates a new TokenLimitExceededEvent
func NewTokenLimitExceededEvent(turn int, modelID, provider, tokenType string, currentTokens, maxTokens int, duration time.Duration) *TokenLimitExceededEvent {
	return &TokenLimitExceededEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:          turn,
		ModelID:       modelID,
		Provider:      provider,
		TokenType:     tokenType,
		CurrentTokens: currentTokens,
		MaxTokens:     maxTokens,
		Duration:      duration.String(),
	}
}

type FallbackAttemptEvent struct {
	BaseEventData
	Turn          int    `json:"turn"`
	AttemptIndex  int    `json:"attempt_index"`
	TotalAttempts int    `json:"total_attempts"`
	ModelID       string `json:"model_id"`
	Provider      string `json:"provider"`
	Phase         string `json:"phase"` // "same_provider" or "cross_provider"
	Error         string `json:"error,omitempty"`
	Success       bool   `json:"success"`
	Duration      string `json:"duration"`
}

func (e *FallbackAttemptEvent) GetEventType() EventType {
	return FallbackAttemptEventType
}

func NewFallbackAttemptEvent(turn, attemptIndex, totalAttempts int, modelID, provider, phase string, success bool, duration time.Duration, error string) *FallbackAttemptEvent {
	return &FallbackAttemptEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:          turn,
		AttemptIndex:  attemptIndex,
		TotalAttempts: totalAttempts,
		ModelID:       modelID,
		Provider:      provider,
		Phase:         phase,
		Success:       success,
		Duration:      duration.String(),
		Error:         error,
	}
}

// MaxTurnsReachedEvent represents when the agent reaches max turns and is given a final chance
type MaxTurnsReachedEvent struct {
	BaseEventData
	Turn         int    `json:"turn"`
	MaxTurns     int    `json:"max_turns"`
	Question     string `json:"question"`
	FinalMessage string `json:"final_message"`
	Duration     string `json:"duration"`
	AgentMode    string `json:"agent_mode"`
}

func (e *MaxTurnsReachedEvent) GetEventType() EventType {
	return MaxTurnsReachedEventType
}

// NewMaxTurnsReachedEvent creates a new MaxTurnsReachedEvent
func NewMaxTurnsReachedEvent(turn, maxTurns int, question, finalMessage, agentMode string, duration time.Duration) *MaxTurnsReachedEvent {
	return &MaxTurnsReachedEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:         turn,
		MaxTurns:     maxTurns,
		Question:     question,
		FinalMessage: finalMessage,
		Duration:     duration.String(),
		AgentMode:    agentMode,
	}
}

// ContextCancelledEvent represents when a conversation is cancelled due to context cancellation
type ContextCancelledEvent struct {
	BaseEventData
	Turn     int           `json:"turn"`
	Reason   string        `json:"reason"`
	Duration time.Duration `json:"duration"`
}

func (e *ContextCancelledEvent) GetEventType() EventType {
	return ContextCancelledEventType
}

func NewContextCancelledEvent(turn int, reason string, duration time.Duration) *ContextCancelledEvent {
	return &ContextCancelledEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:     turn,
		Reason:   reason,
		Duration: duration,
	}
}

// Unified CacheEvent represents all cache operations across all servers
type CacheEvent struct {
	BaseEventData
	Operation      string `json:"operation"`       // "hit", "miss", "write", "expired", "cleanup", "error", "start"
	ServerName     string `json:"server_name"`     // Server name or "all-servers" for global operations
	CacheKey       string `json:"cache_key"`       // Cache key (optional for some operations)
	ConfigPath     string `json:"config_path"`     // Configuration path
	ToolsCount     int    `json:"tools_count"`     // Number of tools (for hit/write operations)
	DataSize       int64  `json:"data_size"`       // Data size in bytes (for write operations)
	Age            string `json:"age"`             // Age as string (for hit/expired operations)
	TTL            string `json:"ttl"`             // TTL as string (for write/expired operations)
	Reason         string `json:"reason"`          // Reason for miss/expired
	CleanupType    string `json:"cleanup_type"`    // Type of cleanup (for cleanup operations)
	EntriesRemoved int    `json:"entries_removed"` // Entries removed (for cleanup operations)
	EntriesTotal   int    `json:"entries_total"`   // Total entries (for cleanup operations)
	SpaceFreed     int64  `json:"space_freed"`     // Space freed in bytes (for cleanup operations)
	Error          string `json:"error"`           // Error message (for error operations)
	ErrorType      string `json:"error_type"`      // Error type (for error operations)
}

func (e *CacheEvent) GetEventType() EventType {
	return CacheEventType
}

// Unified cache event constructors
func NewCacheHitEvent(serverName, cacheKey, configPath string, toolsCount int, age time.Duration) *CacheEvent {
	return &CacheEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Operation:  "hit",
		ServerName: serverName,
		CacheKey:   cacheKey,
		ConfigPath: configPath,
		ToolsCount: toolsCount,
		Age:        age.String(),
	}
}

func NewCacheMissEvent(serverName, cacheKey, configPath, reason string) *CacheEvent {
	return &CacheEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Operation:  "miss",
		ServerName: serverName,
		CacheKey:   cacheKey,
		ConfigPath: configPath,
		Reason:     reason,
	}
}

func NewCacheWriteEvent(serverName, cacheKey, configPath string, toolsCount int, dataSize int64, ttl time.Duration) *CacheEvent {
	return &CacheEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Operation:  "write",
		ServerName: serverName,
		CacheKey:   cacheKey,
		ConfigPath: configPath,
		ToolsCount: toolsCount,
		DataSize:   dataSize,
		TTL:        ttl.String(),
	}
}

func NewCacheExpiredEvent(serverName, cacheKey, configPath string, age, ttl time.Duration) *CacheEvent {
	return &CacheEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Operation:  "expired",
		ServerName: serverName,
		CacheKey:   cacheKey,
		ConfigPath: configPath,
		Age:        age.String(),
		TTL:        ttl.String(),
	}
}

func NewCacheCleanupEvent(cleanupType string, entriesRemoved, entriesTotal int, spaceFreed int64) *CacheEvent {
	return &CacheEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Operation:      "cleanup",
		ServerName:     "all-servers",
		CleanupType:    cleanupType,
		EntriesRemoved: entriesRemoved,
		EntriesTotal:   entriesTotal,
		SpaceFreed:     spaceFreed,
	}
}

func NewCacheErrorEvent(serverName, cacheKey, configPath, operation, errorMsg, errorType string) *CacheEvent {
	return &CacheEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Operation:  "error",
		ServerName: serverName,
		CacheKey:   cacheKey,
		ConfigPath: configPath,
		Error:      errorMsg,
		ErrorType:  errorType,
	}
}

func NewCacheOperationStartEvent(serverName, configPath string) *CacheEvent {
	return &CacheEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Operation:  "start",
		ServerName: serverName,
		ConfigPath: configPath,
	}
}

// ToolExecutionEvent represents tool execution start/end
type ToolExecutionEvent struct {
	BaseEventData
	Turn       int                    `json:"turn"`
	ToolName   string                 `json:"tool_name"`
	ServerName string                 `json:"server_name"`
	ToolCallID string                 `json:"tool_call_id,omitempty"`
	Arguments  map[string]interface{} `json:"arguments,omitempty"`
	Result     string                 `json:"result,omitempty"`
	Duration   time.Duration          `json:"duration,omitempty"`
	Success    bool                   `json:"success,omitempty"`
	Timeout    string                 `json:"timeout,omitempty"`
	Error      string                 `json:"error,omitempty"`
	ErrorType  string                 `json:"error_type,omitempty"`
	Status     string                 `json:"status,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

func (e *ToolExecutionEvent) GetEventType() EventType {
	return ToolExecution
}

// LLMGenerationWithRetryEvent represents LLM generation with retry logic
type LLMGenerationWithRetryEvent struct {
	BaseEventData
	Turn                   int                    `json:"turn"`
	MaxRetries             int                    `json:"max_retries"`
	PrimaryModel           string                 `json:"primary_model"`
	CurrentLLM             string                 `json:"current_llm"`
	SameProviderFallbacks  []string               `json:"same_provider_fallbacks"`
	CrossProviderFallbacks []string               `json:"cross_provider_fallbacks"`
	Provider               string                 `json:"provider"`
	Operation              string                 `json:"operation"`
	FinalError             string                 `json:"final_error,omitempty"`
	Usage                  map[string]interface{} `json:"usage,omitempty"`
	Status                 string                 `json:"status,omitempty"`
	Metadata               map[string]interface{} `json:"metadata,omitempty"`
}

func (e *LLMGenerationWithRetryEvent) GetEventType() EventType {
	return LLMGenerationWithRetry
}

// LLMTextChunkEvent represents a single text chunk from LLM streaming

// SmartRoutingStartEvent represents the start of smart routing
type SmartRoutingStartEvent struct {
	BaseEventData
	TotalTools   int `json:"total_tools"`
	TotalServers int `json:"total_servers"`
	Thresholds   struct {
		MaxTools   int `json:"max_tools"`
		MaxServers int `json:"max_servers"`
	} `json:"thresholds"`
	// LLM Input/Output for debugging smart routing decisions
	LLMPrompt           string `json:"llm_prompt,omitempty"`           // The prompt sent to LLM for server selection
	UserQuery           string `json:"user_query,omitempty"`           // The user's current query
	ConversationContext string `json:"conversation_context,omitempty"` // Recent conversation history
	// LLM Information for smart routing
	LLMModelID     string  `json:"llm_model_id,omitempty"`    // The LLM model used for smart routing
	LLMProvider    string  `json:"llm_provider,omitempty"`    // The LLM provider used for smart routing
	LLMTemperature float64 `json:"llm_temperature,omitempty"` // Temperature used for smart routing
	LLMMaxTokens   int     `json:"llm_max_tokens,omitempty"`  // Max tokens used for smart routing
}

func (e *SmartRoutingStartEvent) GetEventType() EventType {
	return SmartRoutingStartEventType
}

// SmartRoutingEndEvent represents the completion of smart routing
type SmartRoutingEndEvent struct {
	BaseEventData
	TotalTools       int           `json:"total_tools"`
	FilteredTools    int           `json:"filtered_tools"`
	TotalServers     int           `json:"total_servers"`
	RelevantServers  []string      `json:"relevant_servers"`
	RoutingReasoning string        `json:"routing_reasoning,omitempty"`
	RoutingDuration  time.Duration `json:"routing_duration"`
	Success          bool          `json:"success"`
	Error            string        `json:"error,omitempty"`
	// LLM Output for debugging smart routing decisions
	LLMResponse     string `json:"llm_response,omitempty"`     // The raw response from LLM for server selection
	SelectedServers string `json:"selected_servers,omitempty"` // The parsed server selection from LLM
	// NEW: Appended prompt information
	HasAppendedPrompts    bool   `json:"has_appended_prompts"`
	AppendedPromptCount   int    `json:"appended_prompt_count,omitempty"`
	AppendedPromptSummary string `json:"appended_prompt_summary,omitempty"`
	// LLM Information for smart routing
	LLMModelID     string  `json:"llm_model_id,omitempty"`    // The LLM model used for smart routing
	LLMProvider    string  `json:"llm_provider,omitempty"`    // The LLM provider used for smart routing
	LLMTemperature float64 `json:"llm_temperature,omitempty"` // Temperature used for smart routing
	LLMMaxTokens   int     `json:"llm_max_tokens,omitempty"`  // Max tokens used for smart routing
}

func (e *SmartRoutingEndEvent) GetEventType() EventType {
	return SmartRoutingEndEventType
}

// Constructor functions for smart routing events
func NewSmartRoutingStartEvent(totalTools, totalServers, maxTools, maxServers int) *SmartRoutingStartEvent {
	return &SmartRoutingStartEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		TotalTools:   totalTools,
		TotalServers: totalServers,
		Thresholds: struct {
			MaxTools   int `json:"max_tools"`
			MaxServers int `json:"max_servers"`
		}{
			MaxTools:   maxTools,
			MaxServers: maxServers,
		},
	}
}

func NewSmartRoutingEndEvent(totalTools, filteredTools, totalServers int, relevantServers []string, reasoning string, duration time.Duration, success bool, errorMsg string) *SmartRoutingEndEvent {
	return &SmartRoutingEndEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		TotalTools:       totalTools,
		FilteredTools:    filteredTools,
		TotalServers:     totalServers,
		RelevantServers:  relevantServers,
		RoutingReasoning: reasoning,
		RoutingDuration:  duration,
		Success:          success,
		Error:            errorMsg,
	}
}

// UnifiedCompletionEvent represents a standardized completion event for all agent types
type UnifiedCompletionEvent struct {
	BaseEventData
	AgentType   string                 `json:"agent_type"`         // "simple", "react", "orchestrator"
	AgentMode   string                 `json:"agent_mode"`         // "simple", "ReAct", "orchestrator"
	Question    string                 `json:"question"`           // Original user question
	FinalResult string                 `json:"final_result"`       // The final response to show to user
	Status      string                 `json:"status"`             // "completed", "error", "timeout"
	Duration    time.Duration          `json:"duration"`           // Total execution time
	Turns       int                    `json:"turns"`              // Number of conversation turns
	Error       string                 `json:"error,omitempty"`    // Error message if status is error
	Metadata    map[string]interface{} `json:"metadata,omitempty"` // Additional context
}

func (e *UnifiedCompletionEvent) GetEventType() EventType {
	return EventTypeUnifiedCompletion
}

// NewUnifiedCompletionEvent creates a new unified completion event
func NewUnifiedCompletionEvent(agentType, agentMode, question, finalResult, status string, duration time.Duration, turns int) *UnifiedCompletionEvent {
	return &UnifiedCompletionEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType:   agentType,
		AgentMode:   agentMode,
		Question:    question,
		FinalResult: finalResult,
		Status:      status,
		Duration:    duration,
		Turns:       turns,
		Metadata:    make(map[string]interface{}),
	}
}

// NewUnifiedCompletionEventWithError creates a new unified completion event with error
func NewUnifiedCompletionEventWithError(agentType, agentMode, question, errorMsg string, duration time.Duration, turns int) *UnifiedCompletionEvent {
	return &UnifiedCompletionEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType:   agentType,
		AgentMode:   agentMode,
		Question:    question,
		FinalResult: "", // No result for error cases
		Status:      "error",
		Duration:    duration,
		Turns:       turns,
		Error:       errorMsg,
		Metadata:    make(map[string]interface{}),
	}
}

// Orchestrator Events
type OrchestratorStartEvent struct {
	BaseEventData
	Objective        string `json:"objective"`
	AgentsCount      int    `json:"agents_count"`
	ServersCount     int    `json:"servers_count"`
	Configuration    string `json:"configuration,omitempty"`
	OrchestratorType string `json:"orchestrator_type,omitempty"`
	ExecutionMode    string `json:"execution_mode,omitempty"`
}

func (e *OrchestratorStartEvent) GetEventType() EventType {
	return OrchestratorStart
}

type OrchestratorEndEvent struct {
	BaseEventData
	Objective        string        `json:"objective"`
	Result           string        `json:"result"`
	Duration         time.Duration `json:"duration"`
	Status           string        `json:"status"`
	Error            string        `json:"error,omitempty"`
	OrchestratorType string        `json:"orchestrator_type,omitempty"`
	ExecutionMode    string        `json:"execution_mode,omitempty"`
}

func (e *OrchestratorEndEvent) GetEventType() EventType {
	return OrchestratorEnd
}

type OrchestratorErrorEvent struct {
	BaseEventData
	Context          string        `json:"context"`
	Error            string        `json:"error"`
	Duration         time.Duration `json:"duration"`
	OrchestratorType string        `json:"orchestrator_type,omitempty"`
	ExecutionMode    string        `json:"execution_mode,omitempty"`
}

func (e *OrchestratorErrorEvent) GetEventType() EventType {
	return OrchestratorError
}

// Orchestrator Agent Events
type OrchestratorAgentStartEvent struct {
	BaseEventData
	AgentType    string            `json:"agent_type"`           // planning, execution, validation, organizer
	AgentName    string            `json:"agent_name"`           // specific agent name
	Objective    string            `json:"objective"`            // what the agent is trying to accomplish
	InputData    map[string]string `json:"input_data"`           // template variables passed to agent
	ModelID      string            `json:"model_id"`             // which LLM model
	Provider     string            `json:"provider"`             // which LLM provider
	ServersCount int               `json:"servers_count"`        // number of MCP servers available
	MaxTurns     int               `json:"max_turns"`            // maximum conversation turns
	PlanID       string            `json:"plan_id,omitempty"`    // associated plan ID
	StepIndex    int               `json:"step_index,omitempty"` // which step in the plan
	Iteration    int               `json:"iteration,omitempty"`  // which iteration of the loop
}

func (e *OrchestratorAgentStartEvent) GetEventType() EventType {
	return OrchestratorAgentStart
}

type OrchestratorAgentEndEvent struct {
	BaseEventData
	AgentType          string                 `json:"agent_type"`                    // planning, execution, validation, organizer
	AgentName          string                 `json:"agent_name"`                    // specific agent name
	Objective          string                 `json:"objective"`                     // what the agent was trying to accomplish
	InputData          map[string]string      `json:"input_data"`                    // template variables passed to agent
	Result             string                 `json:"result"`                        // agent's output/result (text summary)
	StructuredResponse map[string]interface{} `json:"structured_response,omitempty"` // structured response data (for ExecuteStructured calls)
	Success            bool                   `json:"success"`                       // whether agent completed successfully
	Error              string                 `json:"error,omitempty"`               // error message if failed
	Duration           time.Duration          `json:"duration"`                      // how long the agent took
	ModelID            string                 `json:"model_id"`                      // which LLM model was used
	Provider           string                 `json:"provider"`                      // which LLM provider
	ServersCount       int                    `json:"servers_count"`                 // number of MCP servers used
	MaxTurns           int                    `json:"max_turns"`                     // maximum conversation turns
	PlanID             string                 `json:"plan_id,omitempty"`             // associated plan ID
	StepIndex          int                    `json:"step_index,omitempty"`          // which step in the plan
	Iteration          int                    `json:"iteration,omitempty"`           // which iteration of the loop
	// Token usage fields
	PromptTokens          int `json:"prompt_tokens,omitempty"`
	CompletionTokens      int `json:"completion_tokens,omitempty"`
	TotalTokens           int `json:"total_tokens,omitempty"`
	CacheTokens           int `json:"cache_tokens,omitempty"`
	ReasoningTokens       int `json:"reasoning_tokens,omitempty"`
	LLMCallCount          int `json:"llm_call_count,omitempty"`
	CacheEnabledCallCount int `json:"cache_enabled_call_count,omitempty"`
}

func (e *OrchestratorAgentEndEvent) GetEventType() EventType {
	return OrchestratorAgentEnd
}

type OrchestratorAgentErrorEvent struct {
	BaseEventData
	AgentType    string        `json:"agent_type"`           // planning, execution, validation, organizer
	AgentName    string        `json:"agent_name"`           // specific agent name
	Objective    string        `json:"objective"`            // what the agent was trying to accomplish
	Error        string        `json:"error"`                // error message
	Duration     time.Duration `json:"duration"`             // how long before error occurred
	ModelID      string        `json:"model_id"`             // which LLM model was used
	Provider     string        `json:"provider"`             // which LLM provider
	ServersCount int           `json:"servers_count"`        // number of MCP servers available
	MaxTurns     int           `json:"max_turns"`            // maximum conversation turns
	PlanID       string        `json:"plan_id,omitempty"`    // associated plan ID
	StepIndex    int           `json:"step_index,omitempty"` // which step in the plan
	Iteration    int           `json:"iteration,omitempty"`  // which iteration of the loop
}

func (e *OrchestratorAgentErrorEvent) GetEventType() EventType {
	return OrchestratorAgentError
}

// StepTokenUsageEvent represents token usage summary for a workflow step
type StepTokenUsageEvent struct {
	BaseEventData
	Phase                 string `json:"phase"`                // e.g., "execution"
	Step                  int    `json:"step"`                 // step index (0-based)
	StepTitle             string `json:"step_title,omitempty"` // optional step title for display
	PromptTokens          int    `json:"prompt_tokens"`
	CompletionTokens      int    `json:"completion_tokens"`
	TotalTokens           int    `json:"total_tokens"`
	CacheTokens           int    `json:"cache_tokens"`
	ReasoningTokens       int    `json:"reasoning_tokens"`
	LLMCallCount          int    `json:"llm_call_count"`
	CacheEnabledCallCount int    `json:"cache_enabled_call_count"`
}

func (e *StepTokenUsageEvent) GetEventType() EventType {
	return StepTokenUsage
}

// NewStepTokenUsageEvent creates a new StepTokenUsageEvent
func NewStepTokenUsageEvent(phase string, step int, stepTitle string, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount int) *StepTokenUsageEvent {
	return &StepTokenUsageEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		Phase:                 phase,
		Step:                  step,
		StepTitle:             stepTitle,
		PromptTokens:          promptTokens,
		CompletionTokens:      completionTokens,
		TotalTokens:           totalTokens,
		CacheTokens:           cacheTokens,
		ReasoningTokens:       reasoningTokens,
		LLMCallCount:          llmCallCount,
		CacheEnabledCallCount: cacheEnabledCallCount,
	}
}

// Human Verification Events

type HumanVerificationResponseEvent struct {
	BaseEventData
	SessionID        string `json:"session_id"`
	WorkflowID       string `json:"workflow_id"`
	Response         string `json:"response"`          // "approved", "rejected", or revision feedback
	Feedback         string `json:"feedback"`          // Human feedback text
	RequiresRevision bool   `json:"requires_revision"` // Whether todo list needs revision
}

func (e *HumanVerificationResponseEvent) GetEventType() EventType {
	return HumanVerificationResponse
}

type RequestHumanFeedbackEvent struct {
	BaseEventData
	Objective        string `json:"objective"`
	TodoListMarkdown string `json:"todo_list_markdown"`
	SessionID        string `json:"session_id"`
	WorkflowID       string `json:"workflow_id"`
	RequestID        string `json:"request_id"` // Unique ID for this feedback request
	// NEW: Dynamic verification fields
	VerificationType  string `json:"verification_type,omitempty"`  // "planning_verification", "refinement_verification", "report_verification"
	NextPhase         string `json:"next_phase,omitempty"`         // The phase to transition to after approval
	Title             string `json:"title,omitempty"`              // Custom title text
	ActionLabel       string `json:"action_label,omitempty"`       // Custom button text
	ActionDescription string `json:"action_description,omitempty"` // Custom description text
}

func (e *RequestHumanFeedbackEvent) GetEventType() EventType {
	return RequestHumanFeedback
}

type BlockingHumanFeedbackEvent struct {
	BaseEventData
	Question      string   `json:"question"`       // Question to ask user
	AllowFeedback bool     `json:"allow_feedback"` // Whether to allow text feedback (defaults to true)
	Context       string   `json:"context"`        // Additional context (e.g., validation results)
	SessionID     string   `json:"session_id"`
	WorkflowID    string   `json:"workflow_id"`
	RequestID     string   `json:"request_id"`          // Unique ID for this feedback request
	YesNoOnly     bool     `json:"yes_no_only"`         // If true, show only Approve/Reject buttons (no textarea)
	YesLabel      string   `json:"yes_label,omitempty"` // Custom label for Approve button (default: "Approve")
	NoLabel       string   `json:"no_label,omitempty"`  // Custom label for Reject button (default: "Reject")
	Options       []string `json:"options,omitempty"`   // Array of option labels for multiple choice (renders as buttons)
}

func (e *BlockingHumanFeedbackEvent) GetEventType() EventType {
	return BlockingHumanFeedback
}

// TodoStep represents a todo step in the execution
type TodoStep struct {
	ID                  string   `json:"id,omitempty"` // Stable step ID (from PlanStep) - required for frontend matching
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	SuccessCriteria     string   `json:"success_criteria"`
	WhyThisStep         string   `json:"why_this_step"`
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
	SuccessPatterns     []string `json:"success_patterns,omitempty"` // what worked (includes tools)
	FailurePatterns     []string `json:"failure_patterns,omitempty"` // what failed (includes tools to avoid)
}

// TodoStepsExtractedEvent represents the event when todo steps are extracted from a plan
type TodoStepsExtractedEvent struct {
	BaseEventData
	TotalStepsExtracted int        `json:"total_steps_extracted"`
	ExtractedSteps      []TodoStep `json:"extracted_steps"`
	ExtractionMethod    string     `json:"extraction_method"`
	PlanSource          string     `json:"plan_source"` // "existing_plan" or "new_plan"
}

func (e *TodoStepsExtractedEvent) GetEventType() EventType {
	return TodoStepsExtracted
}

// BranchStepProgress tracks branch execution progress for conditional steps
type BranchStepProgress struct {
	BranchExecuted string   `json:"branch_executed"` // "if_true" or "if_false"
	CompletedSteps []string `json:"completed_steps"` // e.g., ["step-3-if-true-0", "step-3-if-true-1"]
}

// StepProgressUpdatedEvent represents the event when step progress is updated (steps_done.json changes)
type StepProgressUpdatedEvent struct {
	BaseEventData
	CompletedStepIndices []int                      `json:"completed_step_indices"` // 0-based indices of completed steps
	TotalSteps           int                        `json:"total_steps"`            // Total number of steps in the plan
	WorkspacePath        string                     `json:"workspace_path"`         // Workspace path for file operations
	RunFolder            string                     `json:"run_folder"`             // Run folder name (e.g., "iteration-1")
	LastCompletedStep    int                        `json:"last_completed_step"`    // Most recently completed step index (-1 if unknown)
	BranchSteps          map[int]BranchStepProgress `json:"branch_steps,omitempty"` // Branch step progress for conditional steps
}

func (e *StepProgressUpdatedEvent) GetEventType() EventType {
	return StepProgressUpdated
}

// StepStartedEvent represents the event when a step execution starts
type StepStartedEvent struct {
	BaseEventData
	StepID        string `json:"step_id"`        // Step ID from plan
	StepIndex     int    `json:"step_index"`     // 0-based step index
	StepTitle     string `json:"step_title"`     // Step title
	StepPath      string `json:"step_path"`      // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep  bool   `json:"is_branch_step"` // Whether this is a branch step
	RunFolder     string `json:"run_folder"`     // Run folder name (e.g., "iteration-1")
	WorkspacePath string `json:"workspace_path"` // Workspace path for file operations
}

func (e *StepStartedEvent) GetEventType() EventType {
	return StepExecutionStart
}

// StepFinishedEvent represents the event when a step execution completes successfully
type StepFinishedEvent struct {
	BaseEventData
	StepID       string `json:"step_id"`        // Step ID from plan
	StepIndex    int    `json:"step_index"`     // 0-based step index
	StepTitle    string `json:"step_title"`     // Step title
	StepPath     string `json:"step_path"`      // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep bool   `json:"is_branch_step"` // Whether this is a branch step
}

func (e *StepFinishedEvent) GetEventType() EventType {
	return StepExecutionEnd
}

// StepFailedEvent represents the event when a step execution fails
type StepFailedEvent struct {
	BaseEventData
	StepID       string `json:"step_id"`        // Step ID from plan
	StepIndex    int    `json:"step_index"`     // 0-based step index
	StepTitle    string `json:"step_title"`     // Step title
	StepPath     string `json:"step_path"`      // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep bool   `json:"is_branch_step"` // Whether this is a branch step
	Error        string `json:"error"`          // Error message
}

func (e *StepFailedEvent) GetEventType() EventType {
	return StepExecutionFailed
}

// LearningSkippedEvent represents the event when learning is skipped due to temp LLM override
type LearningSkippedEvent struct {
	BaseEventData
	StepID          string `json:"step_id"`                     // Step ID from plan
	StepIndex       int    `json:"step_index"`                  // 0-based step index
	StepTitle       string `json:"step_title"`                  // Step title
	StepPath        string `json:"step_path"`                   // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep    bool   `json:"is_branch_step"`              // Whether this is a branch step
	Reason          string `json:"reason"`                      // Reason for skipping (e.g., "temp_llm_override")
	TempLLMProvider string `json:"temp_llm_provider,omitempty"` // Temp override LLM provider
	TempLLMModel    string `json:"temp_llm_model,omitempty"`    // Temp override LLM model
	RunFolder       string `json:"run_folder"`                  // Run folder name (e.g., "iteration-1")
	WorkspacePath   string `json:"workspace_path"`              // Workspace path
}

func (e *LearningSkippedEvent) GetEventType() EventType {
	return LearningSkipped
}

// Variable represents a single variable definition extracted from objective
type Variable struct {
	Name        string `json:"name"`        // e.g., "AWS_ACCOUNT_ID"
	Value       string `json:"value"`       // Original value from objective
	Description string `json:"description"` // e.g., "AWS account number for deployment"
}

// VariablesExtractedEvent represents the event when variables are extracted from objective
type VariablesExtractedEvent struct {
	BaseEventData
	Variables          []Variable `json:"variables"`
	TemplatedObjective string     `json:"templated_objective"`
	WorkspacePath      string     `json:"workspace_path"`       // Workspace path for file operations (required)
	RunFolder          string     `json:"run_folder,omitempty"` // Run folder name for run-specific configs
}

func (e *VariablesExtractedEvent) GetEventType() EventType {
	return VariablesExtracted
}

// IndependentStepsSelectedEvent represents the event when independent steps are selected for parallel execution
type IndependentStepsSelectedEvent struct {
	BaseEventData
	StepIndices    []int    `json:"step_indices"`    // Indices of steps selected for parallel execution
	StepTitles     []string `json:"step_titles"`     // Titles of selected steps
	TotalSteps     int      `json:"total_steps"`     // Total number of steps in plan
	ExecutionBatch int      `json:"execution_batch"` // Which batch of parallel execution this is
}

func (e *IndependentStepsSelectedEvent) GetEventType() EventType {
	return IndependentStepsSelected
}

// StructuredOutputStartEvent represents the start of structured output extraction
type StructuredOutputStartEvent struct {
	BaseEventData
	SchemaName string `json:"schema_name,omitempty"` // Name of the schema being used
	TargetType string `json:"target_type,omitempty"` // Target Go type name
}

func (e *StructuredOutputStartEvent) GetEventType() EventType {
	return StructuredOutputStart
}

// StructuredOutputEndEvent represents the end of structured output extraction
type StructuredOutputEndEvent struct {
	BaseEventData
	Success      bool   `json:"success"`
	SchemaName   string `json:"schema_name,omitempty"`
	TargetType   string `json:"target_type,omitempty"`
	ParsedOutput string `json:"parsed_output,omitempty"` // JSON string of parsed output
}

func (e *StructuredOutputEndEvent) GetEventType() EventType {
	return StructuredOutputEnd
}

// StructuredOutputErrorEvent represents an error during structured output extraction
type StructuredOutputErrorEvent struct {
	BaseEventData
	Error      string `json:"error"`
	SchemaName string `json:"schema_name,omitempty"`
	TargetType string `json:"target_type,omitempty"`
	RawOutput  string `json:"raw_output,omitempty"` // The raw output that failed to parse
}

func (e *StructuredOutputErrorEvent) GetEventType() EventType {
	return StructuredOutputError
}

// =============================================================================
// STREAMING EVENTS
// =============================================================================

// StreamingStartEvent represents the start of a streaming response
type StreamingStartEvent struct {
	BaseEventData
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
}

func (e *StreamingStartEvent) GetEventType() EventType {
	return StreamingStart
}

// StreamingChunkEvent represents a single chunk in a streaming response
type StreamingChunkEvent struct {
	BaseEventData
	Content      string `json:"content"`                 // The text content of this chunk
	ChunkIndex   int    `json:"chunk_index"`             // Sequential index of this chunk
	IsToolCall   bool   `json:"is_tool_call"`            // Whether this chunk is part of a tool call
	FinishReason string `json:"finish_reason,omitempty"` // Reason for finishing (if this is the last chunk)
}

func (e *StreamingChunkEvent) GetEventType() EventType {
	return StreamingChunk
}

// StreamingEndEvent represents the end of a streaming response
type StreamingEndEvent struct {
	BaseEventData
	TotalChunks  int    `json:"total_chunks"`
	TotalTokens  int    `json:"total_tokens,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
	Duration     string `json:"duration,omitempty"`
}

func (e *StreamingEndEvent) GetEventType() EventType {
	return StreamingEnd
}

// StreamingErrorEvent represents an error during streaming
type StreamingErrorEvent struct {
	BaseEventData
	Error       string `json:"error"`
	ChunkIndex  int    `json:"chunk_index,omitempty"` // Index where error occurred
	Recoverable bool   `json:"recoverable"`           // Whether the error is recoverable
}

func (e *StreamingErrorEvent) GetEventType() EventType {
	return StreamingError
}

// StreamingProgressEvent represents progress during streaming
type StreamingProgressEvent struct {
	BaseEventData
	ChunksReceived int    `json:"chunks_received"`
	BytesReceived  int    `json:"bytes_received"`
	ElapsedTime    string `json:"elapsed_time,omitempty"`
}

func (e *StreamingProgressEvent) GetEventType() EventType {
	return StreamingProgress
}

// StreamingConnectionLostEvent represents a lost connection during streaming
type StreamingConnectionLostEvent struct {
	BaseEventData
	Error          string `json:"error"`
	ChunksReceived int    `json:"chunks_received"` // Chunks received before connection loss
	WillRetry      bool   `json:"will_retry"`
	RetryAttempt   int    `json:"retry_attempt,omitempty"`
	MaxRetries     int    `json:"max_retries,omitempty"`
}

func (e *StreamingConnectionLostEvent) GetEventType() EventType {
	return StreamingConnectionLost
}

// =============================================================================
// CACHE EVENTS
// =============================================================================

// CacheHitEvent represents a cache hit
type CacheHitEvent struct {
	BaseEventData
	CacheKey     string `json:"cache_key"`
	CacheType    string `json:"cache_type,omitempty"` // "prompt", "response", "tool", etc.
	TTLRemaining string `json:"ttl_remaining,omitempty"`
}

func (e *CacheHitEvent) GetEventType() EventType {
	return CacheHit
}

// CacheMissEvent represents a cache miss
type CacheMissEvent struct {
	BaseEventData
	CacheKey  string `json:"cache_key"`
	CacheType string `json:"cache_type,omitempty"`
	Reason    string `json:"reason,omitempty"` // "not_found", "expired", "invalidated"
}

func (e *CacheMissEvent) GetEventType() EventType {
	return CacheMiss
}

// CacheWriteEvent represents a cache write operation
type CacheWriteEvent struct {
	BaseEventData
	CacheKey  string `json:"cache_key"`
	CacheType string `json:"cache_type,omitempty"`
	TTL       string `json:"ttl,omitempty"`
	Size      int    `json:"size,omitempty"` // Size in bytes
}

func (e *CacheWriteEvent) GetEventType() EventType {
	return CacheWrite
}

// CacheExpiredEvent represents an expired cache entry
type CacheExpiredEvent struct {
	BaseEventData
	CacheKey  string `json:"cache_key"`
	CacheType string `json:"cache_type,omitempty"`
	Age       string `json:"age,omitempty"` // How long the entry was cached
}

func (e *CacheExpiredEvent) GetEventType() EventType {
	return CacheExpired
}

// CacheCleanupEvent represents a cache cleanup operation
type CacheCleanupEvent struct {
	BaseEventData
	EntriesRemoved int    `json:"entries_removed"`
	BytesFreed     int    `json:"bytes_freed,omitempty"`
	Duration       string `json:"duration,omitempty"`
	Reason         string `json:"reason,omitempty"` // "scheduled", "memory_pressure", "manual"
}

func (e *CacheCleanupEvent) GetEventType() EventType {
	return CacheCleanup
}

// CacheErrorEvent represents an error during cache operation
type CacheErrorEvent struct {
	BaseEventData
	Operation string `json:"operation"` // "read", "write", "delete", "cleanup"
	CacheKey  string `json:"cache_key,omitempty"`
	Error     string `json:"error"`
}

func (e *CacheErrorEvent) GetEventType() EventType {
	return CacheError
}

// CacheOperationStartEvent represents the start of a cache operation
type CacheOperationStartEvent struct {
	BaseEventData
	Operation string `json:"operation"` // "read", "write", "delete", "cleanup"
	CacheKey  string `json:"cache_key,omitempty"`
	CacheType string `json:"cache_type,omitempty"`
}

func (e *CacheOperationStartEvent) GetEventType() EventType {
	return CacheOperationStart
}

// =============================================================================
// MCP SERVER CONNECTION EVENTS
// =============================================================================

// MCPServerConnectionStartEvent represents the start of an MCP server connection
type MCPServerConnectionStartEvent struct {
	BaseEventData
	ServerName string `json:"server_name"`
	ServerURL  string `json:"server_url,omitempty"`
	Protocol   string `json:"protocol,omitempty"` // "sse", "http", "stdio"
}

func (e *MCPServerConnectionStartEvent) GetEventType() EventType {
	return MCPServerConnectionStart
}

// MCPServerConnectionEndEvent represents successful MCP server connection
type MCPServerConnectionEndEvent struct {
	BaseEventData
	ServerName string   `json:"server_name"`
	ToolCount  int      `json:"tool_count,omitempty"`
	ToolNames  []string `json:"tool_names,omitempty"`
	Duration   string   `json:"duration,omitempty"`
}

func (e *MCPServerConnectionEndEvent) GetEventType() EventType {
	return MCPServerConnectionEnd
}

// MCPServerConnectionErrorEvent represents an MCP server connection error
type MCPServerConnectionErrorEvent struct {
	BaseEventData
	ServerName string `json:"server_name"`
	Error      string `json:"error"`
	Retryable  bool   `json:"retryable"`
	RetryCount int    `json:"retry_count,omitempty"`
}

func (e *MCPServerConnectionErrorEvent) GetEventType() EventType {
	return MCPServerConnectionError
}

// =============================================================================
// JSON VALIDATION EVENTS
// =============================================================================

// JSONValidationStartEvent represents the start of JSON validation
type JSONValidationStartEvent struct {
	BaseEventData
	SchemaName string `json:"schema_name,omitempty"`
	InputSize  int    `json:"input_size,omitempty"` // Size of input in bytes
}

func (e *JSONValidationStartEvent) GetEventType() EventType {
	return JSONValidationStart
}

// JSONValidationEndEvent represents the end of JSON validation
type JSONValidationEndEvent struct {
	BaseEventData
	SchemaName string   `json:"schema_name,omitempty"`
	Valid      bool     `json:"valid"`
	Errors     []string `json:"errors,omitempty"` // Validation errors if not valid
	Duration   string   `json:"duration,omitempty"`
}

func (e *JSONValidationEndEvent) GetEventType() EventType {
	return JSONValidationEnd
}

// =============================================================================
// OTHER MISSING EVENTS
// =============================================================================

// ConversationThinkingEvent represents the agent's thinking/reasoning process
type ConversationThinkingEvent struct {
	BaseEventData
	Thinking string `json:"thinking"` // The thinking/reasoning content
	Turn     int    `json:"turn"`
}

func (e *ConversationThinkingEvent) GetEventType() EventType {
	return ConversationThinking
}

// LLMMessage represents a single message in the LLM conversation
type LLMMessage struct {
	Role    string `json:"role"`              // "system", "user", "assistant", "tool"
	Content string `json:"content,omitempty"` // Message content
}

// LLMMessagesEvent represents the messages sent to/from the LLM
type LLMMessagesEvent struct {
	BaseEventData
	Messages     []LLMMessage `json:"messages"`               // The messages
	MessageCount int          `json:"message_count"`          // Total message count
	Direction    string       `json:"direction,omitempty"`    // "request" or "response"
	TotalTokens  int          `json:"total_tokens,omitempty"` // Estimated token count
}

func (e *LLMMessagesEvent) GetEventType() EventType {
	return LLMMessages
}

// ToolCallProgressEvent represents progress during a tool call
type ToolCallProgressEvent struct {
	BaseEventData
	ToolName    string `json:"tool_name"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
	Progress    int    `json:"progress"` // 0-100 percentage
	Status      string `json:"status"`   // "running", "waiting", "processing"
	Message     string `json:"message,omitempty"`
	ElapsedTime string `json:"elapsed_time,omitempty"`
}

func (e *ToolCallProgressEvent) GetEventType() EventType {
	return ToolCallProgress
}

// DebugEvent represents debug information
type DebugEvent struct {
	BaseEventData
	Level     string                 `json:"level"`     // "debug", "trace", "verbose"
	Component string                 `json:"component"` // Which component generated this
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

func (e *DebugEvent) GetEventType() EventType {
	return Debug
}

// PerformanceEvent represents performance metrics
type PerformanceEvent struct {
	BaseEventData
	Operation  string  `json:"operation"`             // What operation was measured
	Duration   string  `json:"duration"`              // Duration as string (e.g., "1.5s")
	DurationMs float64 `json:"duration_ms"`           // Duration in milliseconds
	MemoryUsed int64   `json:"memory_used,omitempty"` // Memory used in bytes
	CPUPercent float64 `json:"cpu_percent,omitempty"` // CPU percentage
	Component  string  `json:"component,omitempty"`   // Which component
}

func (e *PerformanceEvent) GetEventType() EventType {
	return Performance
}

// LLMTokenUsageEvent represents detailed per-call token usage (advanced mode)
type LLMTokenUsageEvent struct {
	BaseEventData
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	CachedTokens int     `json:"cached_tokens,omitempty"`
	Cost         float64 `json:"cost,omitempty"`
	Turn         int     `json:"turn,omitempty"`
	CallType     string  `json:"call_type,omitempty"` // "generation", "tool_call", "structured_output"
}

func (e *LLMTokenUsageEvent) GetEventType() EventType {
	return LLMTokenUsage
}

// AgentProcessingEvent represents agent processing status
type AgentProcessingEvent struct {
	BaseEventData
	Status      string `json:"status"` // "thinking", "planning", "executing", "waiting"
	Turn        int    `json:"turn"`
	Message     string `json:"message,omitempty"`
	ElapsedTime string `json:"elapsed_time,omitempty"`
}

func (e *AgentProcessingEvent) GetEventType() EventType {
	return AgentProcessing
}

// =============================================================================
// BATCH EXECUTION EVENTS (for variable groups)
// =============================================================================

// BatchExecutionStartEvent represents the start of batch execution across multiple variable groups
type BatchExecutionStartEvent struct {
	BaseEventData
	TotalGroups     int      `json:"total_groups"`      // Total number of enabled groups
	EnabledGroupIDs []string `json:"enabled_group_ids"` // List of group IDs to execute
	IterationNumber int      `json:"iteration_number"`  // Current iteration number
	WorkspacePath   string   `json:"workspace_path"`
}

func (e *BatchExecutionStartEvent) GetEventType() EventType {
	return BatchExecutionStart
}

// NewBatchExecutionStartEvent creates a new BatchExecutionStartEvent
func NewBatchExecutionStartEvent(totalGroups int, enabledGroupIDs []string, iterationNumber int, workspacePath string) *BatchExecutionStartEvent {
	return &BatchExecutionStartEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		TotalGroups:     totalGroups,
		EnabledGroupIDs: enabledGroupIDs,
		IterationNumber: iterationNumber,
		WorkspacePath:   workspacePath,
	}
}

// BatchGroupStartEvent represents the start of execution for a specific variable group
type BatchGroupStartEvent struct {
	BaseEventData
	GroupID         string            `json:"group_id"`         // Current group ID
	GroupIndex      int               `json:"group_index"`      // 0-based index in enabled groups
	TotalGroups     int               `json:"total_groups"`     // Total number of enabled groups
	VariableValues  map[string]string `json:"variable_values"`  // Values for this group
	RunFolder       string            `json:"run_folder"`       // e.g., "iteration-1-group-1"
	IterationNumber int               `json:"iteration_number"` // Current iteration number
	WorkspacePath   string            `json:"workspace_path"`
}

func (e *BatchGroupStartEvent) GetEventType() EventType {
	return BatchGroupStart
}

// NewBatchGroupStartEvent creates a new BatchGroupStartEvent
func NewBatchGroupStartEvent(groupID string, groupIndex, totalGroups int, variableValues map[string]string, runFolder string, iterationNumber int, workspacePath string) *BatchGroupStartEvent {
	return &BatchGroupStartEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		GroupID:         groupID,
		GroupIndex:      groupIndex,
		TotalGroups:     totalGroups,
		VariableValues:  variableValues,
		RunFolder:       runFolder,
		IterationNumber: iterationNumber,
		WorkspacePath:   workspacePath,
	}
}

// BatchGroupEndEvent represents the completion of execution for a specific variable group
type BatchGroupEndEvent struct {
	BaseEventData
	GroupID         string        `json:"group_id"`         // Current group ID
	GroupIndex      int           `json:"group_index"`      // 0-based index in enabled groups
	TotalGroups     int           `json:"total_groups"`     // Total number of enabled groups
	Success         bool          `json:"success"`          // Whether this group completed successfully
	Error           string        `json:"error,omitempty"`  // Error message if failed
	Duration        time.Duration `json:"duration"`         // How long this group took
	CompletedSteps  int           `json:"completed_steps"`  // Number of steps completed
	TotalSteps      int           `json:"total_steps"`      // Total number of steps
	RunFolder       string        `json:"run_folder"`       // e.g., "iteration-1-group-1"
	RemainingGroups int           `json:"remaining_groups"` // How many groups are left
}

func (e *BatchGroupEndEvent) GetEventType() EventType {
	return BatchGroupEnd
}

// NewBatchGroupEndEvent creates a new BatchGroupEndEvent
func NewBatchGroupEndEvent(groupID string, groupIndex, totalGroups int, success bool, errorMsg string, duration time.Duration, completedSteps, totalSteps int, runFolder string, remainingGroups int) *BatchGroupEndEvent {
	return &BatchGroupEndEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		GroupID:         groupID,
		GroupIndex:      groupIndex,
		TotalGroups:     totalGroups,
		Success:         success,
		Error:           errorMsg,
		Duration:        duration,
		CompletedSteps:  completedSteps,
		TotalSteps:      totalSteps,
		RunFolder:       runFolder,
		RemainingGroups: remainingGroups,
	}
}

// BatchExecutionEndEvent represents the completion of all batch execution
type BatchExecutionEndEvent struct {
	BaseEventData
	TotalGroups       int           `json:"total_groups"`        // Total number of enabled groups
	CompletedGroups   int           `json:"completed_groups"`    // Number of groups that completed
	FailedGroups      int           `json:"failed_groups"`       // Number of groups that failed
	CanceledGroups    int           `json:"canceled_groups"`     // Number of groups that were canceled
	Duration          time.Duration `json:"duration"`            // Total batch execution time
	Success           bool          `json:"success"`             // Whether all groups succeeded
	Error             string        `json:"error,omitempty"`     // Error message if batch failed
	IterationNumber   int           `json:"iteration_number"`    // Current iteration number
	CompletedGroupIDs []string      `json:"completed_group_ids"` // IDs of completed groups
	FailedGroupIDs    []string      `json:"failed_group_ids"`    // IDs of failed groups
}

func (e *BatchExecutionEndEvent) GetEventType() EventType {
	return BatchExecutionEnd
}

// NewBatchExecutionEndEvent creates a new BatchExecutionEndEvent
func NewBatchExecutionEndEvent(totalGroups, completedGroups, failedGroups, canceledGroups int, duration time.Duration, success bool, errorMsg string, iterationNumber int, completedGroupIDs, failedGroupIDs []string) *BatchExecutionEndEvent {
	return &BatchExecutionEndEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		TotalGroups:       totalGroups,
		CompletedGroups:   completedGroups,
		FailedGroups:      failedGroups,
		CanceledGroups:    canceledGroups,
		Duration:          duration,
		Success:           success,
		Error:             errorMsg,
		IterationNumber:   iterationNumber,
		CompletedGroupIDs: completedGroupIDs,
		FailedGroupIDs:    failedGroupIDs,
	}
}

// BatchExecutionCanceledEvent represents when batch execution is canceled by user
type BatchExecutionCanceledEvent struct {
	BaseEventData
	TotalGroups       int      `json:"total_groups"`        // Total number of enabled groups
	CompletedGroups   int      `json:"completed_groups"`    // Number of groups that completed before cancel
	CanceledGroupID   string   `json:"canceled_group_id"`   // ID of group that was running when canceled
	RemainingGroupIDs []string `json:"remaining_group_ids"` // IDs of groups that were not executed
	Reason            string   `json:"reason"`              // Reason for cancellation
}

func (e *BatchExecutionCanceledEvent) GetEventType() EventType {
	return BatchExecutionCanceled
}

// NewBatchExecutionCanceledEvent creates a new BatchExecutionCanceledEvent
func NewBatchExecutionCanceledEvent(totalGroups, completedGroups int, canceledGroupID string, remainingGroupIDs []string, reason string) *BatchExecutionCanceledEvent {
	return &BatchExecutionCanceledEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		TotalGroups:       totalGroups,
		CompletedGroups:   completedGroups,
		CanceledGroupID:   canceledGroupID,
		RemainingGroupIDs: remainingGroupIDs,
		Reason:            reason,
	}
}

// PrerequisiteNavigationEvent represents navigation back to a prerequisite step due to prerequisite failure
type PrerequisiteNavigationEvent struct {
	BaseEventData
	FromStepIndex int    `json:"from_step_index"` // 0-based index of step that failed
	ToStepIndex   int    `json:"to_step_index"`   // 0-based index of step to navigate to
	Reason        string `json:"reason"`          // Reason for navigation
	FailureType   string `json:"failure_type"`    // "prerequisite" or "execution"
}

func (e *PrerequisiteNavigationEvent) GetEventType() EventType {
	return PrerequisiteNavigation
}

// NewPrerequisiteNavigationEvent creates a new PrerequisiteNavigationEvent
func NewPrerequisiteNavigationEvent(fromStep, toStep int, reason, failureType string) *PrerequisiteNavigationEvent {
	return &PrerequisiteNavigationEvent{
		BaseEventData: BaseEventData{
			Timestamp: time.Now(),
		},
		FromStepIndex: fromStep,
		ToStepIndex:   toStep,
		Reason:        reason,
		FailureType:   failureType,
	}
}
