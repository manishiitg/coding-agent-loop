package events

import (
	"time"
)

// Unified EventType enum combining all event types
type EventType string

// Agent Event Types (from mcpagent/events.go)
const (
	// Conversation events
	ConversationStart    EventType = "conversation_start"
	ConversationEnd      EventType = "conversation_end"
	ConversationError    EventType = "conversation_error"
	ConversationTurn     EventType = "conversation_turn"
	ConversationThinking EventType = "conversation_thinking"

	// LLM events
	LLMGenerationStart EventType = "llm_generation_start"
	LLMGenerationEnd   EventType = "llm_generation_end"
	LLMGenerationError EventType = "llm_generation_error"
	LLMMessages        EventType = "llm_messages"

	// Tool events
	ToolCallStart    EventType = "tool_call_start"
	ToolCallEnd      EventType = "tool_call_end"
	ToolCallError    EventType = "tool_call_error"
	ToolCallProgress EventType = "tool_call_progress"

	// Agent events
	AgentStart EventType = "agent_start"
	AgentEnd   EventType = "agent_end"
	AgentError EventType = "agent_error"

	// System events
	SystemPrompt EventType = "system_prompt"
	UserMessage  EventType = "user_message"

	// Additional tool events
	ToolOutput   EventType = "tool_output"
	ToolResponse EventType = "tool_response"

	// Event type aliases for backward compatibility
	SystemPromptEventType EventType = "system_prompt"
	ToolOutputEventType   EventType = "tool_output"
	ToolResponseEventType EventType = "tool_response"
	UserMessageEventType  EventType = "user_message"

	// Streaming events
	StreamingStart          EventType = "streaming_start"
	StreamingChunk          EventType = "streaming_chunk"
	StreamingEnd            EventType = "streaming_end"
	StreamingError          EventType = "streaming_error"
	StreamingProgress       EventType = "streaming_progress"
	StreamingConnectionLost EventType = "streaming_connection_lost"

	// Debug events
	Debug       EventType = "debug"
	Performance EventType = "performance"
	TokenUsage  EventType = "token_usage"
	ErrorDetail EventType = "error_detail"

	// Event type aliases for backward compatibility
	TokenUsageEventType  EventType = "token_usage"
	ErrorDetailEventType EventType = "error_detail"

	// Large output events
	LargeToolOutputDetected    EventType = "large_tool_output_detected"
	LargeToolOutputFileWritten EventType = "large_tool_output_file_written"

	// Large output event type aliases for backward compatibility
	LargeToolOutputDetectedEventType          EventType = "large_tool_output_detected"
	LargeToolOutputFileWrittenEventType       EventType = "large_tool_output_file_written"
	LargeToolOutputFileWriteErrorEventType    EventType = "large_tool_output_file_write_error"
	LargeToolOutputServerUnavailableEventType EventType = "large_tool_output_server_unavailable"

	// Fallback events
	FallbackModelUsed  EventType = "fallback_model_used"
	ThrottlingDetected EventType = "throttling_detected"
	TokenLimitExceeded EventType = "token_limit_exceeded"
	MaxTurnsReached    EventType = "max_turns_reached"
	ContextCancelled   EventType = "context_cancelled"

	// Fallback event type aliases for backward compatibility
	ModelChangeEventType        EventType = "model_change"
	FallbackModelUsedEventType  EventType = "fallback_model_used"
	ThrottlingDetectedEventType EventType = "throttling_detected"
	TokenLimitExceededEventType EventType = "token_limit_exceeded"
	MaxTurnsReachedEventType    EventType = "max_turns_reached"
	ContextCancelledEventType   EventType = "context_cancelled"
	FallbackAttemptEventType    EventType = "fallback_attempt"

	// MCP server events
	MCPServerConnection      EventType = "mcp_server_connection"
	MCPServerDiscovery       EventType = "mcp_server_discovery"
	MCPServerSelection       EventType = "mcp_server_selection"
	MCPServerConnectionStart EventType = "mcp_server_connection_start"
	MCPServerConnectionEnd   EventType = "mcp_server_connection_end"
	MCPServerConnectionError EventType = "mcp_server_connection_error"

	// ReAct reasoning events
	ReActReasoningStart EventType = "react_reasoning_start"
	ReActReasoningStep  EventType = "react_reasoning_step"
	ReActReasoningFinal EventType = "react_reasoning_final"
	ReActReasoningEnd   EventType = "react_reasoning_end"
	ReActReasoning      EventType = "react_reasoning"

	// ReAct event type aliases for backward compatibility
	ReActReasoningEventType      EventType = "react_reasoning"
	ReActReasoningStartEventType EventType = "react_reasoning_start"
	ReActReasoningEndEventType   EventType = "react_reasoning_end"
	ReActReasoningStepEventType  EventType = "react_reasoning_step"
	ReActReasoningFinalEventType EventType = "react_reasoning_final"

	// Cache events
	CacheHit            EventType = "cache_hit"
	CacheMiss           EventType = "cache_miss"
	CacheWrite          EventType = "cache_write"
	CacheExpired        EventType = "cache_expired"
	CacheCleanup        EventType = "cache_cleanup"
	CacheError          EventType = "cache_error"
	CacheOperationStart EventType = "cache_operation_start"
	ComprehensiveCache  EventType = "comprehensive_cache"

	// Structured output events
	StructuredOutputStart EventType = "structured_output_start"
	StructuredOutputEnd   EventType = "structured_output_end"
	StructuredOutputError EventType = "structured_output_error"
	JSONValidationStart   EventType = "json_validation_start"
	JSONValidationEnd     EventType = "json_validation_end"

	// Tool execution events
	ToolExecution          EventType = "tool_execution"
	LLMGenerationWithRetry EventType = "llm_generation_with_retry"
	StepExecutionStart     EventType = "step_execution_start"
	StepExecutionEnd       EventType = "step_execution_end"

	// Additional event types from mcpagent
	AgentProcessing                  EventType = "agent_processing"
	ModelChange                      EventType = "model_change"
	FallbackAttempt                  EventType = "fallback_attempt"
	SmartRoutingStart                EventType = "smart_routing_start"
	SmartRoutingEnd                  EventType = "smart_routing_end"
	LargeToolOutputFileWriteError    EventType = "large_tool_output_file_write_error"
	LargeToolOutputServerUnavailable EventType = "large_tool_output_server_unavailable"

	// Additional event type aliases for backward compatibility
	CacheEventType             EventType = "cache_event"
	SmartRoutingStartEventType EventType = "smart_routing_start"
	SmartRoutingEndEventType   EventType = "smart_routing_end"

	// Unified completion event
	EventTypeUnifiedCompletion EventType = "unified_completion"
)

// Orchestrator Event Types (from orchestrator/events/events.go)
const (
	// Orchestrator events
	OrchestratorStart EventType = "orchestrator_start"
	OrchestratorEnd   EventType = "orchestrator_end"
	OrchestratorError EventType = "orchestrator_error"

	// Orchestrator Agent lifecycle events
	OrchestratorAgentStart EventType = "orchestrator_agent_start"
	OrchestratorAgentEnd   EventType = "orchestrator_agent_end"
	OrchestratorAgentError EventType = "orchestrator_agent_error"

	// Parallel execution events
	IndependentStepsSelected EventType = "independent_steps_selected"

	// Human Verification events
	HumanVerificationResponse EventType = "human_verification_response"
	RequestHumanFeedback      EventType = "request_human_feedback"
)

// Unified Event structure with hierarchy support
type Event struct {
	Type          EventType              `json:"type"`
	Timestamp     time.Time              `json:"timestamp"`
	TraceID       string                 `json:"trace_id,omitempty"`
	SpanID        string                 `json:"span_id,omitempty"`
	ParentID      string                 `json:"parent_id,omitempty"` // NEW: Parent event ID
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Data          EventData              `json:"data"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`

	// NEW: Hierarchy fields
	HierarchyLevel int       `json:"hierarchy_level"`       // 0=root, 1=child, 2=grandchild
	ParentType     EventType `json:"parent_type,omitempty"` // Type of parent event
	SessionID      string    `json:"session_id,omitempty"`  // Group related events
	Component      string    `json:"component,omitempty"`   // orchestrator, agent, llm, tool
	Query          string    `json:"query,omitempty"`       // Store the actual query
}

// EventData interface for all event data types
type EventData interface {
	GetEventType() EventType
}

// Base event data structure
type BaseEventData struct {
	Timestamp      time.Time              `json:"timestamp"`
	TraceID        string                 `json:"trace_id,omitempty"`
	SpanID         string                 `json:"span_id,omitempty"`
	EventID        string                 `json:"event_id,omitempty"`
	ParentID       string                 `json:"parent_id,omitempty"`
	IsEndEvent     bool                   `json:"is_end_event,omitempty"`
	CorrelationID  string                 `json:"correlation_id,omitempty"` // Links start/end event pairs
	HierarchyLevel int                    `json:"hierarchy_level"`          // 0=root, 1=child, 2=grandchild
	SessionID      string                 `json:"session_id,omitempty"`     // Group related events
	Component      string                 `json:"component,omitempty"`      // orchestrator, agent, llm, tool
	Metadata       map[string]interface{} `json:"metadata,omitempty"`       // Additional context data
}

// SetHierarchyFields sets the hierarchy-related fields on BaseEventData
func (b *BaseEventData) SetHierarchyFields(parentID string, level int, sessionID string, component string) {
	b.ParentID = parentID
	b.HierarchyLevel = level
	b.SessionID = sessionID
	b.Component = component
}

// GetBaseEventData returns a pointer to the BaseEventData for hierarchy field setting
func (b *BaseEventData) GetBaseEventData() *BaseEventData {
	return b
}

// Helper function to get component from event type
func GetComponentFromEventType(eventType EventType) string {
	switch {
	case eventType == OrchestratorStart || eventType == OrchestratorEnd || eventType == OrchestratorError ||
		eventType == OrchestratorAgentStart || eventType == OrchestratorAgentEnd || eventType == OrchestratorAgentError ||
		eventType == StructuredOutputStart || eventType == StructuredOutputEnd || eventType == StructuredOutputError ||
		eventType == JSONValidationStart || eventType == JSONValidationEnd ||
		eventType == IndependentStepsSelected:
		return "orchestrator"
	case eventType == AgentStart || eventType == AgentEnd || eventType == AgentError ||
		eventType == ReActReasoningStart || eventType == ReActReasoningStep ||
		eventType == ReActReasoningFinal || eventType == ReActReasoningEnd || eventType == ReActReasoning:
		return "agent"
	case eventType == LLMGenerationStart || eventType == LLMGenerationEnd || eventType == LLMGenerationError ||
		eventType == SmartRoutingStart || eventType == SmartRoutingEnd:
		return "llm"
	case eventType == ToolCallStart || eventType == ToolCallEnd || eventType == ToolCallError:
		return "tool"
	case eventType == ConversationStart || eventType == ConversationEnd || eventType == ConversationError || eventType == ConversationTurn || eventType == ConversationThinking:
		return "conversation"
	case eventType == CacheHit || eventType == CacheMiss || eventType == CacheWrite ||
		eventType == CacheExpired || eventType == CacheCleanup || eventType == CacheError ||
		eventType == CacheOperationStart || eventType == ComprehensiveCache:
		return "cache"
	case eventType == SystemPrompt || eventType == UserMessage:
		return "system"
	default:
		return "system"
	}
}

// Helper function to check if event is a start event
func IsStartEvent(eventType EventType) bool {
	return eventType == ConversationStart ||
		eventType == ConversationTurn ||
		eventType == LLMGenerationStart ||
		eventType == ToolCallStart ||
		eventType == AgentStart ||
		eventType == OrchestratorStart ||
		eventType == OrchestratorAgentStart
}

// Helper function to check if event is an end event
func IsEndEvent(eventType EventType) bool {
	return eventType == ConversationEnd ||
		eventType == LLMGenerationEnd ||
		eventType == ToolCallEnd ||
		eventType == AgentEnd ||
		eventType == OrchestratorEnd ||
		eventType == OrchestratorAgentEnd
}
