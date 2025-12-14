package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human"
	"mcpagent/events"
	"mcpagent/mcpcache"

	"github.com/invopop/jsonschema"
)

// =============================================================================
// SECTION 1: Wire Format Types (match actual backend → frontend format)
// =============================================================================

// PollingEventActual matches the actual Event struct from event_store.go
// This is what the backend sends to the frontend over the wire
type PollingEventActual struct {
	ID        string               `json:"id" jsonschema:"description=Unique event identifier"`
	Type      events.EventType     `json:"type" jsonschema:"description=Event type discriminator"`
	Timestamp time.Time            `json:"timestamp" jsonschema:"description=Event timestamp"`
	SessionID string               `json:"session_id,omitempty" jsonschema:"description=Session identifier"`
	Error     string               `json:"error,omitempty" jsonschema:"description=Error message if any"`
	Data      *AgentEventForSchema `json:"data,omitempty" jsonschema:"description=The AgentEvent containing event details"`
}

// AgentEventForSchema matches AgentEvent from data.go - this is the wrapper around actual event data
type AgentEventForSchema struct {
	Type           events.EventType `json:"type" jsonschema:"description=Event type (same as parent)"`
	Timestamp      time.Time        `json:"timestamp" jsonschema:"description=Event timestamp"`
	EventIndex     int              `json:"event_index" jsonschema:"description=Sequential event index"`
	TraceID        string           `json:"trace_id,omitempty" jsonschema:"description=Trace ID for distributed tracing"`
	SpanID         string           `json:"span_id,omitempty" jsonschema:"description=Span ID for distributed tracing"`
	ParentID       string           `json:"parent_id,omitempty" jsonschema:"description=Parent event ID"`
	CorrelationID  string           `json:"correlation_id,omitempty" jsonschema:"description=Links start/end event pairs"`
	HierarchyLevel int              `json:"hierarchy_level" jsonschema:"description=0=root, 1=child, 2=grandchild"`
	SessionID      string           `json:"session_id,omitempty" jsonschema:"description=Group related events"`
	Component      string           `json:"component,omitempty" jsonschema:"description=orchestrator, agent, llm, tool"`
	Data           EventDataUnion   `json:"data" jsonschema:"description=The actual typed event data"`
}

// =============================================================================
// SECTION 2: EventDataUnion - All possible event data types
// This is the union of all typed event data
// =============================================================================

// EventDataUnion contains all possible event data types as optional fields
// The frontend uses event.data.type to determine which field is populated
type EventDataUnion struct {
	// Core Agent Events
	AgentStart *events.AgentStartEvent `json:"agent_start,omitempty"`
	AgentEnd   *events.AgentEndEvent   `json:"agent_end,omitempty"`
	AgentError *events.AgentErrorEvent `json:"agent_error,omitempty"`

	// Conversation Events
	ConversationStart *events.ConversationStartEvent `json:"conversation_start,omitempty"`
	ConversationEnd   *events.ConversationEndEvent   `json:"conversation_end,omitempty"`
	ConversationError *events.ConversationErrorEvent `json:"conversation_error,omitempty"`
	ConversationTurn  *events.ConversationTurnEvent  `json:"conversation_turn,omitempty"`

	// LLM Events
	LLMGenerationStart     *events.LLMGenerationStartEvent     `json:"llm_generation_start,omitempty"`
	LLMGenerationEnd       *events.LLMGenerationEndEvent       `json:"llm_generation_end,omitempty"`
	LLMGenerationError     *events.LLMGenerationErrorEvent     `json:"llm_generation_error,omitempty"`
	LLMGenerationWithRetry *events.LLMGenerationWithRetryEvent `json:"llm_generation_with_retry,omitempty"`

	// Tool Events
	ToolCallStart          *events.ToolCallStartEvent          `json:"tool_call_start,omitempty"`
	ToolCallEnd            *events.ToolCallEndEvent            `json:"tool_call_end,omitempty"`
	ToolCallError          *events.ToolCallErrorEvent          `json:"tool_call_error,omitempty"`
	ToolExecution          *events.ToolExecutionEvent          `json:"tool_execution,omitempty"`
	ToolOutput             *events.ToolOutputEvent             `json:"tool_output,omitempty"`
	ToolResponse           *events.ToolResponseEvent           `json:"tool_response,omitempty"`
	WorkspaceFileOperation *events.WorkspaceFileOperationEvent `json:"workspace_file_operation,omitempty"`

	// MCP Server Events
	MCPServerConnection *events.MCPServerConnectionEvent `json:"mcp_server_connection,omitempty"`
	MCPServerDiscovery  *events.MCPServerDiscoveryEvent  `json:"mcp_server_discovery,omitempty"`
	MCPServerSelection  *events.MCPServerSelectionEvent  `json:"mcp_server_selection,omitempty"`

	// System Events
	SystemPrompt *events.SystemPromptEvent `json:"system_prompt,omitempty"`
	UserMessage  *events.UserMessageEvent  `json:"user_message,omitempty"`

	// Token & Usage Events
	TokenUsage       *events.TokenUsageEvent       `json:"token_usage,omitempty"`
	ErrorDetail      *events.ErrorDetailEvent      `json:"error_detail,omitempty"`
	MaxTurnsReached  *events.MaxTurnsReachedEvent  `json:"max_turns_reached,omitempty"`
	ContextCancelled *events.ContextCancelledEvent `json:"context_cancelled,omitempty"`

	// Large Output Events
	LargeToolOutputDetected          *events.LargeToolOutputDetectedEvent          `json:"large_tool_output_detected,omitempty"`
	LargeToolOutputFileWritten       *events.LargeToolOutputFileWrittenEvent       `json:"large_tool_output_file_written,omitempty"`
	LargeToolOutputFileWriteError    *events.LargeToolOutputFileWriteErrorEvent    `json:"large_tool_output_file_write_error,omitempty"`
	LargeToolOutputServerUnavailable *events.LargeToolOutputServerUnavailableEvent `json:"large_tool_output_server_unavailable,omitempty"`

	// Fallback & Resilience Events
	ModelChange        *events.ModelChangeEvent        `json:"model_change,omitempty"`
	FallbackModelUsed  *events.FallbackModelUsedEvent  `json:"fallback_model_used,omitempty"`
	FallbackAttempt    *events.FallbackAttemptEvent    `json:"fallback_attempt,omitempty"`
	ThrottlingDetected *events.ThrottlingDetectedEvent `json:"throttling_detected,omitempty"`
	TokenLimitExceeded *events.TokenLimitExceededEvent `json:"token_limit_exceeded,omitempty"`

	// Cache Events
	CacheEvent         *events.CacheEvent                `json:"cache_event,omitempty"`
	ComprehensiveCache *mcpcache.ComprehensiveCacheEvent `json:"comprehensive_cache_event,omitempty"`

	// Smart Routing Events
	SmartRoutingStart *events.SmartRoutingStartEvent `json:"smart_routing_start,omitempty"`
	SmartRoutingEnd   *events.SmartRoutingEndEvent   `json:"smart_routing_end,omitempty"`

	// Unified Completion Event
	UnifiedCompletion *events.UnifiedCompletionEvent `json:"unified_completion,omitempty"`

	// Orchestrator Events
	OrchestratorStart      *events.OrchestratorStartEvent      `json:"orchestrator_start,omitempty"`
	OrchestratorEnd        *events.OrchestratorEndEvent        `json:"orchestrator_end,omitempty"`
	OrchestratorError      *events.OrchestratorErrorEvent      `json:"orchestrator_error,omitempty"`
	OrchestratorAgentStart *events.OrchestratorAgentStartEvent `json:"orchestrator_agent_start,omitempty"`
	OrchestratorAgentEnd   *events.OrchestratorAgentEndEvent   `json:"orchestrator_agent_end,omitempty"`
	OrchestratorAgentError *events.OrchestratorAgentErrorEvent `json:"orchestrator_agent_error,omitempty"`

	// Step Execution Events
	StepStarted         *todo_creation_human.StepStartedEvent         `json:"step_execution_start,omitempty"`
	StepFinished        *todo_creation_human.StepFinishedEvent        `json:"step_execution_end,omitempty"`
	StepFailed          *events.StepFailedEvent                       `json:"step_execution_failed,omitempty"`
	StepTokenUsage      *events.StepTokenUsageEvent                   `json:"step_token_usage,omitempty"`
	StepProgressUpdated *todo_creation_human.StepProgressUpdatedEvent `json:"step_progress_updated,omitempty"`
	DecisionEvaluated   *todo_creation_human.DecisionEvaluatedEvent   `json:"decision_evaluated,omitempty"`

	// Todo/Planning Events
	TodoStepsExtracted       *todo_creation_human.TodoStepsExtractedEvent       `json:"todo_steps_extracted,omitempty"`
	VariablesExtracted       *todo_creation_human.VariablesExtractedEvent       `json:"variables_extracted,omitempty"`
	IndependentStepsSelected *todo_creation_human.IndependentStepsSelectedEvent `json:"independent_steps_selected,omitempty"`

	// Human Feedback Events
	RequestHumanFeedback      *events.RequestHumanFeedbackEvent      `json:"request_human_feedback,omitempty"`
	BlockingHumanFeedback     *events.BlockingHumanFeedbackEvent     `json:"blocking_human_feedback,omitempty"`
	HumanVerificationResponse *events.HumanVerificationResponseEvent `json:"human_verification_response,omitempty"`

	// Structured Output Events
	StructuredOutputStart *events.StructuredOutputStartEvent `json:"structured_output_start,omitempty"`
	StructuredOutputEnd   *events.StructuredOutputEndEvent   `json:"structured_output_end,omitempty"`
	StructuredOutputError *events.StructuredOutputErrorEvent `json:"structured_output_error,omitempty"`

	// Streaming Events
	StreamingStart          *events.StreamingStartEvent          `json:"streaming_start,omitempty"`
	StreamingChunk          *events.StreamingChunkEvent          `json:"streaming_chunk,omitempty"`
	StreamingEnd            *events.StreamingEndEvent            `json:"streaming_end,omitempty"`
	StreamingError          *events.StreamingErrorEvent          `json:"streaming_error,omitempty"`
	StreamingProgress       *events.StreamingProgressEvent       `json:"streaming_progress,omitempty"`
	StreamingConnectionLost *events.StreamingConnectionLostEvent `json:"streaming_connection_lost,omitempty"`

	// Cache Detail Events
	CacheHit            *events.CacheHitEvent            `json:"cache_hit,omitempty"`
	CacheMiss           *events.CacheMissEvent           `json:"cache_miss,omitempty"`
	CacheWrite          *events.CacheWriteEvent          `json:"cache_write,omitempty"`
	CacheExpired        *events.CacheExpiredEvent        `json:"cache_expired,omitempty"`
	CacheCleanup        *events.CacheCleanupEvent        `json:"cache_cleanup,omitempty"`
	CacheError          *events.CacheErrorEvent          `json:"cache_error,omitempty"`
	CacheOperationStart *events.CacheOperationStartEvent `json:"cache_operation_start,omitempty"`

	// MCP Server Connection Detail Events
	MCPServerConnectionStart *events.MCPServerConnectionStartEvent `json:"mcp_server_connection_start,omitempty"`
	MCPServerConnectionEnd   *events.MCPServerConnectionEndEvent   `json:"mcp_server_connection_end,omitempty"`
	MCPServerConnectionError *events.MCPServerConnectionErrorEvent `json:"mcp_server_connection_error,omitempty"`

	// JSON Validation Events
	JSONValidationStart *events.JSONValidationStartEvent `json:"json_validation_start,omitempty"`
	JSONValidationEnd   *events.JSONValidationEndEvent   `json:"json_validation_end,omitempty"`

	// Other Events
	ConversationThinking *events.ConversationThinkingEvent `json:"conversation_thinking,omitempty"`
	LLMMessages          *events.LLMMessagesEvent          `json:"llm_messages,omitempty"`
	ToolCallProgress     *events.ToolCallProgressEvent     `json:"tool_call_progress,omitempty"`
	Debug                *events.DebugEvent                `json:"debug,omitempty"`
	Performance          *events.PerformanceEvent          `json:"performance,omitempty"`
	LLMTokenUsage        *events.LLMTokenUsageEvent        `json:"llm_token_usage,omitempty"`
	AgentProcessing      *events.AgentProcessingEvent      `json:"agent_processing,omitempty"`

	// Batch Execution Events
	BatchExecutionStart *events.BatchExecutionStartEvent `json:"batch_execution_start,omitempty"`
	BatchGroupStart     *events.BatchGroupStartEvent     `json:"batch_group_start,omitempty"`
	BatchGroupEnd       *events.BatchGroupEndEvent       `json:"batch_group_end,omitempty"`
	BatchExecutionEnd   *events.BatchExecutionEndEvent   `json:"batch_execution_end,omitempty"`
}

// =============================================================================
// SECTION 3: Discriminated Union Schema Generation
// =============================================================================

// EventTypeMapping maps event types to their corresponding data struct field names
// This is used to generate the discriminated union schema
var EventTypeMapping = map[events.EventType]string{
	// Core Agent Events
	events.AgentStart: "agent_start",
	events.AgentEnd:   "agent_end",
	events.AgentError: "agent_error",

	// Conversation Events
	events.ConversationStart: "conversation_start",
	events.ConversationEnd:   "conversation_end",
	events.ConversationError: "conversation_error",
	events.ConversationTurn:  "conversation_turn",

	// LLM Events
	events.LLMGenerationStart:     "llm_generation_start",
	events.LLMGenerationEnd:       "llm_generation_end",
	events.LLMGenerationError:     "llm_generation_error",
	events.LLMGenerationWithRetry: "llm_generation_with_retry",

	// Tool Events
	events.ToolCallStart:          "tool_call_start",
	events.ToolCallEnd:            "tool_call_end",
	events.ToolCallError:          "tool_call_error",
	events.ToolExecution:          "tool_execution",
	events.ToolOutput:             "tool_output",
	events.ToolResponse:           "tool_response",
	events.WorkspaceFileOperation: "workspace_file_operation",

	// MCP Server Events
	events.MCPServerConnection: "mcp_server_connection",
	events.MCPServerDiscovery:  "mcp_server_discovery",
	events.MCPServerSelection:  "mcp_server_selection",

	// System Events
	events.SystemPrompt: "system_prompt",
	events.UserMessage:  "user_message",

	// Token & Usage Events
	events.TokenUsage:       "token_usage",
	events.ErrorDetail:      "error_detail",
	events.MaxTurnsReached:  "max_turns_reached",
	events.ContextCancelled: "context_cancelled",

	// Large Output Events
	events.LargeToolOutputDetected:                   "large_tool_output_detected",
	events.LargeToolOutputFileWritten:                "large_tool_output_file_written",
	events.LargeToolOutputFileWriteErrorEventType:    "large_tool_output_file_write_error",
	events.LargeToolOutputServerUnavailableEventType: "large_tool_output_server_unavailable",

	// Fallback & Resilience Events
	events.ModelChange:        "model_change",
	events.FallbackModelUsed:  "fallback_model_used",
	events.FallbackAttempt:    "fallback_attempt",
	events.ThrottlingDetected: "throttling_detected",
	events.TokenLimitExceeded: "token_limit_exceeded",

	// Cache Events (comprehensive only - specific cache events are below)
	events.ComprehensiveCache: "comprehensive_cache_event",

	// Smart Routing Events
	events.SmartRoutingStart: "smart_routing_start",
	events.SmartRoutingEnd:   "smart_routing_end",

	// Unified Completion Event
	events.EventTypeUnifiedCompletion: "unified_completion",

	// Orchestrator Events
	events.OrchestratorStart:      "orchestrator_start",
	events.OrchestratorEnd:        "orchestrator_end",
	events.OrchestratorError:      "orchestrator_error",
	events.OrchestratorAgentStart: "orchestrator_agent_start",
	events.OrchestratorAgentEnd:   "orchestrator_agent_end",
	events.OrchestratorAgentError: "orchestrator_agent_error",

	// Step Execution Events
	events.StepExecutionStart:              "step_execution_start",
	events.StepExecutionEnd:                "step_execution_end",
	events.StepExecutionFailed:             "step_execution_failed",
	events.StepTokenUsage:                  "step_token_usage",
	events.StepProgressUpdated:             "step_progress_updated",
	events.EventType("decision_evaluated"): "decision_evaluated",

	// Todo/Planning Events
	events.TodoStepsExtracted:       "todo_steps_extracted",
	events.VariablesExtracted:       "variables_extracted",
	events.IndependentStepsSelected: "independent_steps_selected",

	// Human Feedback Events
	events.RequestHumanFeedback:      "request_human_feedback",
	events.BlockingHumanFeedback:     "blocking_human_feedback",
	events.HumanVerificationResponse: "human_verification_response",

	// Structured Output Events
	events.StructuredOutputStart: "structured_output_start",
	events.StructuredOutputEnd:   "structured_output_end",
	events.StructuredOutputError: "structured_output_error",

	// Streaming Events
	events.StreamingStart:          "streaming_start",
	events.StreamingChunk:          "streaming_chunk",
	events.StreamingEnd:            "streaming_end",
	events.StreamingError:          "streaming_error",
	events.StreamingProgress:       "streaming_progress",
	events.StreamingConnectionLost: "streaming_connection_lost",

	// Cache Detail Events
	events.CacheHit:            "cache_hit",
	events.CacheMiss:           "cache_miss",
	events.CacheWrite:          "cache_write",
	events.CacheExpired:        "cache_expired",
	events.CacheCleanup:        "cache_cleanup",
	events.CacheError:          "cache_error",
	events.CacheOperationStart: "cache_operation_start",

	// MCP Server Connection Detail Events
	events.MCPServerConnectionStart: "mcp_server_connection_start",
	events.MCPServerConnectionEnd:   "mcp_server_connection_end",
	events.MCPServerConnectionError: "mcp_server_connection_error",

	// JSON Validation Events
	events.JSONValidationStart: "json_validation_start",
	events.JSONValidationEnd:   "json_validation_end",

	// Other Events
	events.ConversationThinking: "conversation_thinking",
	events.LLMMessages:          "llm_messages",
	events.ToolCallProgress:     "tool_call_progress",
	events.Debug:                "debug",
	events.Performance:          "performance",
	events.LLMTokenUsage:        "llm_token_usage",
	events.AgentProcessing:      "agent_processing",

	// Batch Execution Events
	events.BatchExecutionStart: "batch_execution_start",
	events.BatchGroupStart:     "batch_group_start",
	events.BatchGroupEnd:       "batch_group_end",
	events.BatchExecutionEnd:   "batch_execution_end",
}

// =============================================================================
// SECTION 4: Schema Generation Functions
// =============================================================================

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

// generateDiscriminatedUnionSchema generates a JSON schema with proper oneOf discriminated union
func generateDiscriminatedUnionSchema(filename string) error {
	r := new(jsonschema.Reflector)
	r.ExpandedStruct = true
	r.DoNotReference = false
	r.RequiredFromJSONSchemaTags = true

	// Generate the base schema first to get all definitions
	baseSchema := r.Reflect(&PollingEventActual{})

	// Generate all event type enum values
	eventTypes := make([]interface{}, 0, len(EventTypeMapping))
	for eventType := range EventTypeMapping {
		eventTypes = append(eventTypes, string(eventType))
	}

	// Add EventType enum to definitions if not present
	if baseSchema.Definitions == nil {
		baseSchema.Definitions = make(jsonschema.Definitions)
	}

	// Create EventType enum schema
	eventTypeSchema := &jsonschema.Schema{
		Type: "string",
		Enum: eventTypes,
	}
	baseSchema.Definitions["EventType"] = eventTypeSchema

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
	return enc.Encode(baseSchema)
}

// =============================================================================
// SECTION 5: Legacy Types (for backward compatibility)
// =============================================================================

// UnifiedEvent represents a container for all event types (legacy, for unified-events-complete.schema.json)
type UnifiedEvent struct {
	// MCP Agent Events (from unified events package)
	ToolCallStartEvent       events.ToolCallStartEvent       `json:"tool_call_start"`
	ToolCallEndEvent         events.ToolCallEndEvent         `json:"tool_call_end"`
	ToolCallErrorEvent       events.ToolCallErrorEvent       `json:"tool_call_error"`
	LLMGenerationStartEvent  events.LLMGenerationStartEvent  `json:"llm_generation_start"`
	LLMGenerationEndEvent    events.LLMGenerationEndEvent    `json:"llm_generation_end"`
	MCPAgentStartEvent       events.AgentStartEvent          `json:"agent_start"`
	MCPAgentEndEvent         events.AgentEndEvent            `json:"agent_end"`
	MCPAgentErrorEvent       events.AgentErrorEvent          `json:"mcp_agent_error"`
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

	// Step Execution Events
	StepStartedEvent         todo_creation_human.StepStartedEvent         `json:"step_execution_start"`
	StepFinishedEvent        todo_creation_human.StepFinishedEvent        `json:"step_execution_end"`
	StepFailedEvent          events.StepFailedEvent                       `json:"step_execution_failed"`
	StepTokenUsageEvent      events.StepTokenUsageEvent                   `json:"step_token_usage"`
	StepProgressUpdatedEvent todo_creation_human.StepProgressUpdatedEvent `json:"step_progress_updated"`

	// Todo/Planning Events
	TodoStepsExtractedEvent       todo_creation_human.TodoStepsExtractedEvent       `json:"todo_steps_extracted"`
	VariablesExtractedEvent       todo_creation_human.VariablesExtractedEvent       `json:"variables_extracted"`
	IndependentStepsSelectedEvent todo_creation_human.IndependentStepsSelectedEvent `json:"independent_steps_selected"`

	// Structured Output Events
	StructuredOutputStartEvent events.StructuredOutputStartEvent `json:"structured_output_start"`
	StructuredOutputEndEvent   events.StructuredOutputEndEvent   `json:"structured_output_end"`
	StructuredOutputErrorEvent events.StructuredOutputErrorEvent `json:"structured_output_error"`

	// Workspace Events
	WorkspaceFileOperationEvent events.WorkspaceFileOperationEvent `json:"workspace_file_operation"`

	// Large Output Error Events
	LargeToolOutputFileWriteErrorEvent    events.LargeToolOutputFileWriteErrorEvent    `json:"large_tool_output_file_write_error"`
	LargeToolOutputServerUnavailableEvent events.LargeToolOutputServerUnavailableEvent `json:"large_tool_output_server_unavailable"`
}

// =============================================================================
// SECTION 6: Main Entry Point
// =============================================================================

func main() {
	fmt.Println("Generating JSON schemas for event types...")

	// Generate unified events schema (for backward compatibility)
	if err := writeSchema("schemas/unified-events-complete.schema.json", UnifiedEvent{}); err != nil {
		fmt.Printf("Error generating unified events schema: %v\n", err)
		os.Exit(1)
	}

	// Generate the new PollingEvent schema with proper wire format
	if err := generateDiscriminatedUnionSchema("schemas/polling-event.schema.json"); err != nil {
		fmt.Printf("Error generating polling event schema: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Successfully generated schemas:")
	fmt.Println("  - schemas/unified-events-complete.schema.json")
	fmt.Println("  - schemas/polling-event.schema.json")
	fmt.Println("")
	fmt.Println("📋 Schema Structure (matching actual wire format):")
	fmt.Println("  PollingEventActual")
	fmt.Println("  ├── id: string")
	fmt.Println("  ├── type: EventType (discriminator)")
	fmt.Println("  ├── timestamp: string")
	fmt.Println("  ├── session_id?: string")
	fmt.Println("  ├── error?: string")
	fmt.Println("  └── data: AgentEventForSchema")
	fmt.Println("       ├── type: EventType")
	fmt.Println("       ├── timestamp: string")
	fmt.Println("       ├── event_index: number")
	fmt.Println("       ├── trace_id?: string")
	fmt.Println("       ├── ... (hierarchy fields)")
	fmt.Println("       └── data: EventDataUnion (typed event data)")
}
