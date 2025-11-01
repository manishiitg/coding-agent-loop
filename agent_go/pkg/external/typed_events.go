package external

// No imports needed for this file

// TypedEventData represents the base interface for all typed event data
// This interface provides type-safe access to event information
type TypedEventData interface {
	// GetEventType returns the string identifier for this event type
	GetEventType() string
}

// EventData is an alias for TypedEventData to maintain compatibility
type EventData = TypedEventData

// EventType constants for easy comparison
const (
	// Core Agent Events
	EventTypeAgentError = "agent_error"

	// Conversation Events - removed, now using unified events package
	// EventTypeConversationStart    = "conversation_start"
	// EventTypeConversationEnd      = "conversation_end"
	// EventTypeConversationError    = "conversation_error"
	// EventTypeConversationTurn     = "conversation_turn"
	// EventTypeConversationThinking = "conversation_thinking"

	// LLM Events
	EventTypeLLMGenerationStart = "llm_generation_start"
	EventTypeLLMGenerationEnd   = "llm_generation_end"
	EventTypeLLMGenerationError = "llm_generation_error"

	// Tool Events
	EventTypeToolCallStart = "tool_call_start"
	EventTypeToolCallEnd   = "tool_call_end"
	EventTypeToolCallError = "tool_call_error"

	// MCP Server Events
	EventTypeMCPServerConnection = "mcp_server_connection"
	EventTypeMCPServerDiscovery  = "mcp_server_discovery"
	EventTypeMCPServerSelection  = "mcp_server_selection"

	// ReAct Reasoning Events
	EventTypeReActReasoningStart = "react_reasoning_start"
	EventTypeReActReasoningStep  = "react_reasoning_step"
	EventTypeReActReasoningFinal = "react_reasoning_final"
	EventTypeReActReasoningEnd   = "react_reasoning_end"

	// System Events
	EventTypeSystemPrompt = "system_prompt"
	EventTypeUserMessage  = "user_message"
	EventTypeTokenUsage   = "token_usage"

	// Large Tool Output Events
	EventTypeLargeToolOutputDetected    = "large_tool_output_detected"
	EventTypeLargeToolOutputFileWritten = "large_tool_output_file_written"

	// Fallback & Error Events
	EventTypeFallbackModelUsed  = "fallback_model_used"
	EventTypeThrottlingDetected = "throttling_detected"
	//nolint:gosec // G101: This is an event type constant, not a credential
	EventTypeTokenLimitExceeded = "token_limit_exceeded"
	EventTypeMaxTurnsReached    = "max_turns_reached"
	EventTypeContextCancelled   = "context_cancelled"
)

// Type assertion helpers for safe event data access
func AsToolCallStart(data TypedEventData) (*ToolCallStartEvent, bool) {
	if event, ok := data.(*ToolCallStartEvent); ok {
		return event, true
	}
	return nil, false
}

func AsToolCallEnd(data TypedEventData) (*ToolCallEndEvent, bool) {
	if event, ok := data.(*ToolCallEndEvent); ok {
		return event, true
	}
	return nil, false
}

func AsToolCallError(data TypedEventData) (*ToolCallErrorEvent, bool) {
	if event, ok := data.(*ToolCallErrorEvent); ok {
		return event, true
	}
	return nil, false
}

func AsLLMGenerationStart(data TypedEventData) (*LLMGenerationStartEvent, bool) {
	if event, ok := data.(*LLMGenerationStartEvent); ok {
		return event, true
	}
	return nil, false
}

func AsLLMGenerationEnd(data TypedEventData) (*LLMGenerationEndEvent, bool) {
	if event, ok := data.(*LLMGenerationEndEvent); ok {
		return event, true
	}
	return nil, false
}

func AsTokenUsage(data TypedEventData) (*TokenUsageEvent, bool) {
	if event, ok := data.(*TokenUsageEvent); ok {
		return event, true
	}
	return nil, false
}

func AsReActReasoningStep(data TypedEventData) (*ReActReasoningStepEvent, bool) {
	if event, ok := data.(*ReActReasoningStepEvent); ok {
		return event, true
	}
	return nil, false
}

// Conversation event type assertion helpers removed - now using unified events package
// func AsConversationStart(data TypedEventData) (*ConversationStartEvent, bool) {
// func AsConversationEnd(data TypedEventData) (*ConversationEndEvent, bool) {
