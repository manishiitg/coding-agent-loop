package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	todo_creation_human "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	"github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/mcpcache"

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

	// Context Summarization Events
	ContextSummarizationStarted   *events.ContextSummarizationStartedEvent   `json:"context_summarization_started,omitempty"`
	ContextSummarizationCompleted *events.ContextSummarizationCompletedEvent `json:"context_summarization_completed,omitempty"`
	ContextSummarizationError     *events.ContextSummarizationErrorEvent     `json:"context_summarization_error,omitempty"`

	// Context Editing Events
	ContextEditingCompleted *events.ContextEditingCompletedEvent `json:"context_editing_completed,omitempty"`
	ContextEditingError     *events.ContextEditingErrorEvent     `json:"context_editing_error,omitempty"`

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
	OrchestratorStart      *orchestrator_events.OrchestratorStartEvent      `json:"orchestrator_start,omitempty"`
	OrchestratorEnd        *orchestrator_events.OrchestratorEndEvent        `json:"orchestrator_end,omitempty"`
	OrchestratorError      *orchestrator_events.OrchestratorErrorEvent      `json:"orchestrator_error,omitempty"`
	OrchestratorAgentStart *orchestrator_events.OrchestratorAgentStartEvent `json:"orchestrator_agent_start,omitempty"`
	OrchestratorAgentEnd   *orchestrator_events.OrchestratorAgentEndEvent   `json:"orchestrator_agent_end,omitempty"`
	OrchestratorAgentError *orchestrator_events.OrchestratorAgentErrorEvent `json:"orchestrator_agent_error,omitempty"`

	// Step Execution Events
	StepTokenUsage         *todo_creation_human.StepTokenUsageEvent         `json:"step_token_usage,omitempty"`
	StepProgressUpdated    *todo_creation_human.StepProgressUpdatedEvent    `json:"step_progress_updated,omitempty"`
	DecisionEvaluated      *todo_creation_human.DecisionEvaluatedEvent      `json:"decision_evaluated,omitempty"`
	PreValidationCompleted *todo_creation_human.PreValidationCompletedEvent `json:"pre_validation_completed,omitempty"`

	// Todo/Planning Events
	TodoStepsExtracted       *todo_creation_human.TodoStepsExtractedEvent       `json:"todo_steps_extracted,omitempty"`
	VariablesExtracted       *todo_creation_human.VariablesExtractedEvent       `json:"variables_extracted,omitempty"`
	IndependentStepsSelected *todo_creation_human.IndependentStepsSelectedEvent `json:"independent_steps_selected,omitempty"`

	// Human Feedback Events
	RequestHumanFeedback      *orchestrator_events.RequestHumanFeedbackEvent      `json:"request_human_feedback,omitempty"`
	BlockingHumanFeedback     *orchestrator_events.BlockingHumanFeedbackEvent     `json:"blocking_human_feedback,omitempty"`
	HumanVerificationResponse *orchestrator_events.HumanVerificationResponseEvent `json:"human_verification_response,omitempty"`

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
	BatchExecutionStart *orchestrator_events.BatchExecutionStartEvent `json:"batch_execution_start,omitempty"`
	BatchGroupStart     *orchestrator_events.BatchGroupStartEvent     `json:"batch_group_start,omitempty"`
	BatchGroupEnd       *orchestrator_events.BatchGroupEndEvent       `json:"batch_group_end,omitempty"`
	BatchExecutionEnd   *orchestrator_events.BatchExecutionEndEvent   `json:"batch_execution_end,omitempty"`

	// Prerequisite Navigation Event
	PrerequisiteNavigation *events.PrerequisiteNavigationEvent `json:"prerequisite_navigation,omitempty"`
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

	// Context Summarization Events
	events.ContextSummarizationStarted:   "context_summarization_started",
	events.ContextSummarizationCompleted: "context_summarization_completed",
	events.ContextSummarizationError:     "context_summarization_error",

	// Context Editing Events
	events.ContextEditingCompleted: "context_editing_completed",
	events.ContextEditingError:     "context_editing_error",

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
	orchestrator_events.OrchestratorStart:      "orchestrator_start",
	orchestrator_events.OrchestratorEnd:        "orchestrator_end",
	orchestrator_events.OrchestratorError:      "orchestrator_error",
	orchestrator_events.OrchestratorAgentStart: "orchestrator_agent_start",
	orchestrator_events.OrchestratorAgentEnd:   "orchestrator_agent_end",
	orchestrator_events.OrchestratorAgentError: "orchestrator_agent_error",

	// Step Execution Events
	orchestrator_events.StepTokenUsage:         "step_token_usage",
	orchestrator_events.StepProgressUpdated:    "step_progress_updated",
	orchestrator_events.DecisionEvaluated:      "decision_evaluated",
	orchestrator_events.PreValidationCompleted: "pre_validation_completed",
	events.PrerequisiteNavigation:              "prerequisite_navigation",

	// Todo/Planning Events
	orchestrator_events.TodoStepsExtracted:       "todo_steps_extracted",
	orchestrator_events.VariablesExtracted:       "variables_extracted",
	orchestrator_events.IndependentStepsSelected: "independent_steps_selected",

	// Human Feedback Events
	orchestrator_events.RequestHumanFeedback:      "request_human_feedback",
	orchestrator_events.BlockingHumanFeedback:     "blocking_human_feedback",
	orchestrator_events.HumanVerificationResponse: "human_verification_response",

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
	orchestrator_events.BatchExecutionStart: "batch_execution_start",
	orchestrator_events.BatchGroupStart:     "batch_group_start",
	orchestrator_events.BatchGroupEnd:       "batch_group_end",
	orchestrator_events.BatchExecutionEnd:   "batch_execution_end",
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

	// Debug: Check what fields were reflected
	if props := schema.Properties; props != nil {
		// Use reflection to check the struct type
		vt := reflect.TypeOf(v)
		if vt.Kind() == reflect.Struct || (vt.Kind() == reflect.Ptr && vt.Elem().Kind() == reflect.Struct) {
			if vt.Kind() == reflect.Ptr {
				vt = vt.Elem()
			}
			fmt.Printf("🔍 Debug: Struct %s has %d fields\n", vt.Name(), vt.NumField())

			// Check for context_editing fields in the struct
			hasContextEditing := false
			for i := 0; i < vt.NumField(); i++ {
				f := vt.Field(i)
				if f.Name == "ContextEditingCompletedEvent" || f.Name == "ContextEditingErrorEvent" {
					hasContextEditing = true
					fmt.Printf("✅ Found field in struct: %s (JSON: %s)\n", f.Name, f.Tag.Get("json"))
				}
			}
			if !hasContextEditing {
				fmt.Printf("❌ ContextEditing fields NOT found in struct definition\n")
			}
		}

		// Check if they're in the schema properties
		propsJSON, _ := json.Marshal(schema)
		var propsMap map[string]interface{}
		json.Unmarshal(propsJSON, &propsMap)
		if propsData, ok := propsMap["properties"].(map[string]interface{}); ok {
			hasInSchema := false
			for k := range propsData {
				if k == "context_editing_completed" || k == "context_editing_error" {
					hasInSchema = true
					fmt.Printf("✅ Found in schema properties: %s\n", k)
				}
			}
			if !hasInSchema {
				fmt.Printf("❌ ContextEditing events NOT found in schema properties\n")
				fmt.Printf("   Total properties in schema: %d\n", len(propsData))
			}
		}
	}

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

	// Debug: Check OrderedMap directly before encoding
	if props := schema.Properties; props != nil {
		// Try to access the OrderedMap's internal data
		propsBytes, _ := json.Marshal(props)
		var propsMap map[string]interface{}
		json.Unmarshal(propsBytes, &propsMap)
		hasBeforeEncode := false
		for k := range propsMap {
			if k == "context_editing_completed" || k == "context_editing_error" {
				hasBeforeEncode = true
				fmt.Printf("✅ Found in OrderedMap before encode: %s\n", k)
			}
		}
		if !hasBeforeEncode {
			fmt.Printf("❌ NOT in OrderedMap before encode! Total: %d\n", len(propsMap))
		}
	}

	// Convert OrderedMap to regular map via JSON round-trip to ensure proper serialization
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	var schemaMap map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
		return fmt.Errorf("failed to unmarshal schema: %w", err)
	}

	// Debug: Verify the converted map has the events
	if propsData, ok := schemaMap["properties"].(map[string]interface{}); ok {
		hasInConvertedMap := false
		allKeys := make([]string, 0, len(propsData))
		for k := range propsData {
			allKeys = append(allKeys, k)
			if k == "context_editing_completed" || k == "context_editing_error" {
				hasInConvertedMap = true
				fmt.Printf("✅ Found in converted map: %s\n", k)
			}
		}
		if !hasInConvertedMap {
			fmt.Printf("❌ NOT in converted map! Total: %d\n", len(propsData))
			// Show context keys that ARE in the map
			ctxKeys := make([]string, 0)
			for _, k := range allKeys {
				if strings.Contains(k, "context") {
					ctxKeys = append(ctxKeys, k)
				}
			}
			fmt.Printf("   Context keys in converted map: %v\n", ctxKeys)
		} else {
			fmt.Printf("   Total keys in converted map: %d\n", len(propsData))
		}
	}

	// Write the converted map with proper indentation using MarshalIndent
	finalBytes, err := json.MarshalIndent(schemaMap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schema map: %w", err)
	}

	// Verify what we're about to write
	var writeCheck map[string]interface{}
	json.Unmarshal(finalBytes, &writeCheck)
	if writeProps, ok := writeCheck["properties"].(map[string]interface{}); ok {
		hasBeforeWrite := false
		for k := range writeProps {
			if k == "context_editing_completed" || k == "context_editing_error" {
				hasBeforeWrite = true
				fmt.Printf("✅ Found in bytes to write: %s\n", k)
			}
		}
		if !hasBeforeWrite {
			fmt.Printf("❌ NOT in bytes to write! Total: %d\n", len(writeProps))
		}
	}

	// Write bytes directly
	bytesWritten, err := f.Write(finalBytes)
	if err != nil {
		return fmt.Errorf("failed to write schema: %w", err)
	}
	fmt.Printf("📝 Wrote %d bytes to file\n", bytesWritten)

	// Add newline
	if _, err := f.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// Close file before verification
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	// Immediately verify what's on disk
	if verifyBytes, readErr := os.ReadFile(filename); readErr == nil {
		fmt.Printf("📄 Read %d bytes from file for verification\n", len(verifyBytes))
		// Check raw bytes first
		if bytes.Contains(verifyBytes, []byte("context_editing_completed")) {
			fmt.Printf("✅ Found 'context_editing_completed' in raw file bytes\n")
		} else {
			fmt.Printf("❌ 'context_editing_completed' NOT in raw file bytes\n")
		}

		var immediateCheck map[string]interface{}
		if json.Unmarshal(verifyBytes, &immediateCheck) == nil {
			if immediateProps, ok := immediateCheck["properties"].(map[string]interface{}); ok {
				hasImmediate := false
				for k := range immediateProps {
					if k == "context_editing_completed" || k == "context_editing_error" {
						hasImmediate = true
						fmt.Printf("✅ Verified in parsed JSON: %s\n", k)
					}
				}
				if !hasImmediate {
					fmt.Printf("❌ NOT in parsed JSON! Total: %d\n", len(immediateProps))
				}
			}
		}
	}

	// Verify what was actually written (after file is closed)
	if writtenBytes, readErr := os.ReadFile(filename); readErr == nil {
		var verifyMap map[string]interface{}
		if json.Unmarshal(writtenBytes, &verifyMap) == nil {
			if verifyProps, ok := verifyMap["properties"].(map[string]interface{}); ok {
				hasInFile := false
				allKeys := make([]string, 0, len(verifyProps))
				for k := range verifyProps {
					allKeys = append(allKeys, k)
					if k == "context_editing_completed" || k == "context_editing_error" {
						hasInFile = true
						fmt.Printf("✅ Verified in written file: %s\n", k)
					}
				}
				if !hasInFile {
					fmt.Printf("❌ ERROR: ContextEditing events NOT in written file!\n")
					fmt.Printf("   Total properties in file: %d\n", len(verifyProps))
					// Show context-related keys that ARE in the file
					ctxKeys := make([]string, 0)
					for _, k := range allKeys {
						if strings.Contains(k, "context") {
							ctxKeys = append(ctxKeys, k)
						}
					}
					fmt.Printf("   Context keys in file: %v\n", ctxKeys)
				}
			}
		}
	}

	return nil
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

	// Context Summarization Events
	ContextSummarizationStartedEvent   events.ContextSummarizationStartedEvent   `json:"context_summarization_started"`
	ContextSummarizationCompletedEvent events.ContextSummarizationCompletedEvent `json:"context_summarization_completed"`
	ContextSummarizationErrorEvent     events.ContextSummarizationErrorEvent     `json:"context_summarization_error"`

	// Context Editing Events
	ContextEditingCompletedEvent events.ContextEditingCompletedEvent `json:"context_editing_completed"`
	ContextEditingErrorEvent     events.ContextEditingErrorEvent     `json:"context_editing_error"`

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
	OrchestratorStartEvent      orchestrator_events.OrchestratorStartEvent      `json:"orchestrator_start"`
	OrchestratorEndEvent        orchestrator_events.OrchestratorEndEvent        `json:"orchestrator_end"`
	OrchestratorErrorEvent      orchestrator_events.OrchestratorErrorEvent      `json:"orchestrator_error"`
	OrchestratorAgentStartEvent orchestrator_events.OrchestratorAgentStartEvent `json:"orchestrator_agent_start"`
	OrchestratorAgentEndEvent   orchestrator_events.OrchestratorAgentEndEvent   `json:"orchestrator_agent_end"`
	OrchestratorAgentErrorEvent orchestrator_events.OrchestratorAgentErrorEvent `json:"orchestrator_agent_error"`

	// Human Verification Events
	RequestHumanFeedbackEvent orchestrator_events.RequestHumanFeedbackEvent `json:"request_human_feedback"`

	// Step Execution Events
	StepTokenUsageEvent         todo_creation_human.StepTokenUsageEvent         `json:"step_token_usage"`
	StepProgressUpdatedEvent    todo_creation_human.StepProgressUpdatedEvent    `json:"step_progress_updated"`
	PreValidationCompletedEvent todo_creation_human.PreValidationCompletedEvent `json:"pre_validation_completed"`

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

	// Nested types that need to be included in schema (not events themselves)
	// TodoStep is used by frontend but not directly in events, so we include it here to ensure it's generated
	TodoStep orchestrator_events.TodoStep `json:"todo_step,omitempty"`
}

// =============================================================================
// SECTION 6: Main Entry Point
// =============================================================================

func main() {
	fmt.Println("Generating JSON schemas for event types...")

	// Generate unified events schema (for backward compatibility)
	// Write to agent_go/schemas/ so frontend can find it
	if err := writeSchema("agent_go/schemas/unified-events-complete.schema.json", UnifiedEvent{}); err != nil {
		fmt.Printf("Error generating unified events schema: %v\n", err)
		os.Exit(1)
	}

	// Generate the new PollingEvent schema with proper wire format
	// Generate to both root schemas/ and agent_go/schemas/ for compatibility
	if err := generateDiscriminatedUnionSchema("schemas/polling-event.schema.json"); err != nil {
		fmt.Printf("Error generating polling event schema: %v\n", err)
		os.Exit(1)
	}
	// Also generate to agent_go/schemas/ for frontend to use
	if err := generateDiscriminatedUnionSchema("agent_go/schemas/polling-event.schema.json"); err != nil {
		fmt.Printf("Error generating polling event schema to agent_go/schemas: %v\n", err)
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
