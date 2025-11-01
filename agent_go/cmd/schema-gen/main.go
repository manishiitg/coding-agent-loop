package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpcache"

	"github.com/invopop/jsonschema"
)

// UnifiedEvent represents a container for all event types
type UnifiedEvent struct {
	// MCP Agent Events (from unified events package)
	ToolCallStartEvent      events.ToolCallStartEvent      `json:"tool_call_start"`
	ToolCallEndEvent        events.ToolCallEndEvent        `json:"tool_call_end"`
	ToolCallErrorEvent      events.ToolCallErrorEvent      `json:"tool_call_error"`
	LLMGenerationStartEvent events.LLMGenerationStartEvent `json:"llm_generation_start"`
	LLMGenerationEndEvent   events.LLMGenerationEndEvent   `json:"llm_generation_end"`
	MCPAgentStartEvent      events.AgentStartEvent         `json:"agent_start"`
	MCPAgentEndEvent        events.AgentEndEvent           `json:"agent_end"`
	MCPAgentErrorEvent      events.AgentErrorEvent         `json:"mcp_agent_error"`
	// Note: AgentProgressEvent doesn't exist in backend, removed
	ConversationErrorEvent   events.ConversationErrorEvent   `json:"conversation_error"`
	LLMGenerationErrorEvent  events.LLMGenerationErrorEvent  `json:"llm_generation_error"`
	MCPServerConnectionEvent events.MCPServerConnectionEvent `json:"mcp_server_connection"`
	MCPServerDiscoveryEvent  events.MCPServerDiscoveryEvent  `json:"mcp_server_discovery"`
	MCPServerSelectionEvent  events.MCPServerSelectionEvent  `json:"mcp_server_selection"`
	ConversationStartEvent   events.ConversationStartEvent   `json:"conversation_start"`
	ConversationEndEvent     events.ConversationEndEvent     `json:"conversation_end"`
	ConversationTurnEvent    events.ConversationTurnEvent    `json:"conversation_turn"`

	SystemPromptEvent events.SystemPromptEvent `json:"system_prompt"`
	UserMessageEvent  events.UserMessageEvent  `json:"user_message"`

	LargeToolOutputDetectedEvent    events.LargeToolOutputDetectedEvent    `json:"large_tool_output_detected"`
	LargeToolOutputFileWrittenEvent events.LargeToolOutputFileWrittenEvent `json:"large_tool_output_file_written"`
	FallbackModelUsedEvent          events.FallbackModelUsedEvent          `json:"fallback_model_used"`
	ThrottlingDetectedEvent         events.ThrottlingDetectedEvent         `json:"throttling_detected"`
	TokenLimitExceededEvent         events.TokenLimitExceededEvent         `json:"token_limit_exceeded"`
	TokenUsageEvent                 events.TokenUsageEvent                 `json:"token_usage"`
	MaxTurnsReachedEvent            events.MaxTurnsReachedEvent            `json:"max_turns_reached"`
	ContextCancelledEvent           events.ContextCancelledEvent           `json:"context_cancelled"`
	ReActReasoningStartEvent        events.ReActReasoningStartEvent        `json:"react_reasoning_start"`
	ReActReasoningStepEvent         events.ReActReasoningStepEvent         `json:"react_reasoning_step"`
	ReActReasoningFinalEvent        events.ReActReasoningFinalEvent        `json:"react_reasoning_final"`
	ReActReasoningEndEvent          events.ReActReasoningEndEvent          `json:"react_reasoning_end"`

	// Additional MCP Agent Events that exist in backend
	ToolOutputEvent   events.ToolOutputEvent   `json:"tool_output"`
	ToolResponseEvent events.ToolResponseEvent `json:"tool_response"`

	ModelChangeEvent            events.ModelChangeEvent            `json:"model_change"`
	FallbackAttemptEvent        events.FallbackAttemptEvent        `json:"fallback_attempt"`
	CacheEvent                  events.CacheEvent                  `json:"cache_event"`
	ComprehensiveCacheEvent     mcpcache.ComprehensiveCacheEvent   `json:"comprehensive_cache_event"`
	ToolExecutionEvent          events.ToolExecutionEvent          `json:"tool_execution"`
	LLMGenerationWithRetryEvent events.LLMGenerationWithRetryEvent `json:"llm_generation_with_retry"`

	// Smart Routing Events (from unified events package)
	SmartRoutingStartEvent events.SmartRoutingStartEvent `json:"smart_routing_start"`
	SmartRoutingEndEvent   events.SmartRoutingEndEvent   `json:"smart_routing_end"`

	// Orchestrator Events - now handled by unified events system
	OrchestratorStartEvent      events.OrchestratorStartEvent      `json:"orchestrator_start"`
	OrchestratorEndEvent        events.OrchestratorEndEvent        `json:"orchestrator_end"`
	OrchestratorErrorEvent      events.OrchestratorErrorEvent      `json:"orchestrator_error"`
	OrchestratorAgentStartEvent events.OrchestratorAgentStartEvent `json:"orchestrator_agent_start"`
	OrchestratorAgentEndEvent   events.OrchestratorAgentEndEvent   `json:"orchestrator_agent_end"`
	OrchestratorAgentErrorEvent events.OrchestratorAgentErrorEvent `json:"orchestrator_agent_error"`

	// Human Verification Events
	RequestHumanFeedbackEvent events.RequestHumanFeedbackEvent `json:"request_human_feedback"`
}

// PollingEvent represents the unified event structure for the frontend
type PollingEvent struct {
	Type      string    `json:"type"`
	Timestamp string    `json:"timestamp"`
	Data      EventData `json:"data"`
}

// EventData represents the discriminated union of all event types
type EventData struct {
	// MCP Agent Events
	ToolCallStart      *events.ToolCallStartEvent      `json:"tool_call_start,omitempty"`
	ToolCallEnd        *events.ToolCallEndEvent        `json:"tool_call_end,omitempty"`
	ToolCallError      *events.ToolCallErrorEvent      `json:"tool_call_error,omitempty"`
	LLMGenerationStart *events.LLMGenerationStartEvent `json:"llm_generation_start,omitempty"`
	LLMGenerationEnd   *events.LLMGenerationEndEvent   `json:"llm_generation_end,omitempty"`
	MCPAgentStart      *events.AgentStartEvent         `json:"agent_start,omitempty"`
	MCPAgentEnd        *events.AgentEndEvent           `json:"agent_end,omitempty"`
	AgentError         *events.AgentErrorEvent         `json:"agent_error,omitempty"`
	// Note: AgentProgressEvent doesn't exist in backend, removed
	ConversationError   *events.ConversationErrorEvent   `json:"conversation_error,omitempty"`
	LLMGenerationError  *events.LLMGenerationErrorEvent  `json:"llm_generation_error,omitempty"`
	MCPServerConnection *events.MCPServerConnectionEvent `json:"mcp_server_connection,omitempty"`
	MCPServerDiscovery  *events.MCPServerDiscoveryEvent  `json:"mcp_server_discovery,omitempty"`
	MCPServerSelection  *events.MCPServerSelectionEvent  `json:"mcp_server_selection,omitempty"`
	ConversationStart   *events.ConversationStartEvent   `json:"conversation_start,omitempty"`
	ConversationEnd     *events.ConversationEndEvent     `json:"conversation_end,omitempty"`
	ConversationTurn    *events.ConversationTurnEvent    `json:"conversation_turn,omitempty"`

	SystemPrompt *events.SystemPromptEvent `json:"system_prompt,omitempty"`
	UserMessage  *events.UserMessageEvent  `json:"user_message,omitempty"`

	LargeToolOutputDetected    *events.LargeToolOutputDetectedEvent    `json:"large_tool_output_detected,omitempty"`
	LargeToolOutputFileWritten *events.LargeToolOutputFileWrittenEvent `json:"large_tool_output_file_written,omitempty"`
	FallbackModelUsed          *events.FallbackModelUsedEvent          `json:"fallback_model_used,omitempty"`
	ThrottlingDetected         *events.ThrottlingDetectedEvent         `json:"throttling_detected,omitempty"`
	TokenLimitExceeded         *events.TokenLimitExceededEvent         `json:"token_limit_exceeded,omitempty"`
	TokenUsage                 *events.TokenUsageEvent                 `json:"token_usage,omitempty"`
	ErrorDetail                *events.ErrorDetailEvent                `json:"error_detail,omitempty"`
	MaxTurnsReached            *events.MaxTurnsReachedEvent            `json:"max_turns_reached,omitempty"`
	ContextCancelled           *events.ContextCancelledEvent           `json:"context_cancelled,omitempty"`
	ReActReasoningStart        *events.ReActReasoningStartEvent        `json:"react_reasoning_start,omitempty"`
	ReActReasoningStep         *events.ReActReasoningStepEvent         `json:"react_reasoning_step,omitempty"`
	ReActReasoningFinal        *events.ReActReasoningFinalEvent        `json:"react_reasoning_final,omitempty"`
	ReActReasoningEnd          *events.ReActReasoningEndEvent          `json:"react_reasoning_end,omitempty"`

	// Additional MCP Agent Events that exist in backend
	ToolOutput   *events.ToolOutputEvent   `json:"tool_output,omitempty"`
	ToolResponse *events.ToolResponseEvent `json:"tool_response,omitempty"`

	ModelChange            *events.ModelChangeEvent            `json:"model_change,omitempty"`
	FallbackAttempt        *events.FallbackAttemptEvent        `json:"fallback_attempt,omitempty"`
	CacheEvent             *events.CacheEvent                  `json:"cache_event,omitempty"`
	ComprehensiveCache     *mcpcache.ComprehensiveCacheEvent   `json:"comprehensive_cache_event,omitempty"`
	ToolExecution          *events.ToolExecutionEvent          `json:"tool_execution,omitempty"`
	LLMGenerationWithRetry *events.LLMGenerationWithRetryEvent `json:"llm_generation_with_retry,omitempty"`

	// Smart Routing Events (from unified events package)
	SmartRoutingStart *events.SmartRoutingStartEvent `json:"smart_routing_start,omitempty"`
	SmartRoutingEnd   *events.SmartRoutingEndEvent   `json:"smart_routing_end,omitempty"`

	// Orchestrator Events (from unified events system)
	OrchestratorStart      *events.OrchestratorStartEvent      `json:"orchestrator_start,omitempty"`
	OrchestratorEnd        *events.OrchestratorEndEvent        `json:"orchestrator_end,omitempty"`
	OrchestratorError      *events.OrchestratorErrorEvent      `json:"orchestrator_error,omitempty"`
	OrchestratorAgentStart *events.OrchestratorAgentStartEvent `json:"orchestrator_agent_start,omitempty"`
	OrchestratorAgentEnd   *events.OrchestratorAgentEndEvent   `json:"orchestrator_agent_end,omitempty"`
	OrchestratorAgentError *events.OrchestratorAgentErrorEvent `json:"orchestrator_agent_error,omitempty"`

	// Human Verification Events
	RequestHumanFeedbackEvent *events.RequestHumanFeedbackEvent `json:"request_human_feedback,omitempty"`

	// Todo Creation Events
	TodoStepsExtracted *events.TodoStepsExtractedEvent `json:"todo_steps_extracted,omitempty"`
}

func writeSchema(filename string, v any) error {
	r := new(jsonschema.Reflector)
	r.ExpandedStruct = true
	r.DoNotReference = false
	r.RequiredFromJSONSchemaTags = true

	schema := r.Reflect(v)

	// Ensure the output directory exists
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	//nolint:gosec // G304: filename comes from command-line/config, not user input
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filename, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(schema)
}

func main() {
	fmt.Println("Generating JSON schemas for event types...")

	// Generate unified events schema
	if err := writeSchema("schemas/unified-events-complete.schema.json", UnifiedEvent{}); err != nil {
		fmt.Printf("Error generating unified events schema: %v\n", err)
		os.Exit(1)
	}

	// Generate PollingEvent schema (the actual frontend contract)
	if err := writeSchema("schemas/polling-event.schema.json", PollingEvent{}); err != nil {
		fmt.Printf("Error generating polling event schema: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Successfully generated schemas:")
	fmt.Println("  - schemas/unified-events-complete.schema.json")
	fmt.Println("  - schemas/polling-event.schema.json")
}
