package eventbridge

import (
	"context"
	"fmt"
	"time"

	"mcp-agent-builder-go/agent_go/internal/events"
	"mcp-agent-builder-go/agent_go/pkg/database"
	pkgevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// EventBridge defines the interface for event bridges
type EventBridge interface {
	Name() string
	HandleEvent(ctx context.Context, event *pkgevents.AgentEvent) error
}

// SKIP_EVENTS contains event types that should NOT be emitted (no UI component, pure waste)
var SKIP_EVENTS = map[string]bool{
	// Tool extras - no UI component
	"tool_execution":     true,
	"tool_output":        true,
	"tool_response":      true,
	"tool_call_progress": true,
	// Cache events - all 9 (only 2 had UI, disabling all for now)
	"cache_event":               true,
	"comprehensive_cache_event": true,
	"cache_hit":                 true,
	"cache_miss":                true,
	"cache_write":               true,
	"cache_expired":             true,
	"cache_cleanup":             true,
	"cache_error":               true,
	"cache_operation_start":     true,
}

// DB_SKIP_EVENTS contains event types that should NOT be stored in the database (to save space)
// This aligns with "Micro" mode in the frontend, keeping only high-level and important events.
var DB_SKIP_EVENTS = map[string]bool{
	// High volume / low value
	"step_progress_updated": true,
	"step_token_usage":      true,
	
	// Detailed LLM logs (huge content)
	"llm_generation_start":      true,
	"llm_generation_with_retry": true,
	"llm_generation_end":        true, // Final output is usually available in agent_end or tool_call_end
	"llm_generation_error":      true, // Covered by agent_error/orchestrator_error
	"llm_messages":              true,
	"llm_token_usage":           true,
	"unified_completion":        true,

	// Conversation flow (redundant or hidden in micro)
	"conversation_start":    true,
	"conversation_end":      true,
	"conversation_turn":     true,
	"conversation_thinking": true,
	"conversation_error":    true,

	// System internals
	"system_prompt":                  true,
	"agent_processing":               true,
	"large_tool_output_detected":     true,
	"large_tool_output_file_written": true,
	"large_tool_output_file_write_error":     true,
	"large_tool_output_server_unavailable":   true,
	
	// Streaming (should be skipped by SKIP_EVENTS mostly, but explicit here)
	"streaming_start":           true,
	"streaming_chunk":           true,
	"streaming_end":             true,
	"streaming_error":           true,
	"streaming_progress":        true,
	"streaming_connection_lost": true,

	// Connection & Validation
	"mcp_server_connection_start": true,
	"mcp_server_connection_end":   true,
	"mcp_server_connection_error": true,
	"json_validation_start":       true,
	"json_validation_end":         true,

	// Context & Resilience
	"context_summarization_started":   true,
	"context_summarization_completed": true,
	"context_summarization_error":     true,
	"context_editing_completed":       true,
	"context_editing_error":           true,
	"context_cancelled":               true,
	"throttling_detected":             true,
	"token_limit_exceeded":            true,
	"fallback_model_used":             true,
	"fallback_attempt":                true,
	"model_change":                    true,

	// Agent lifecycle (hidden in micro/tiny mode on frontend)
	"agent_start": true,
	"agent_end":   true,
	"agent_error": true,

	// Debug & Performance
	"debug":       true,
	"performance": true,

	// Detailed Orchestrator/Workflow events
	"independent_steps_selected": true,
	"todo_steps_extracted":       true,
	"variables_extracted":        true,
	"batch_execution_start":      true,
	"batch_execution_end":        true,
	"batch_execution_canceled":   true,
	"batch_group_start":          true,
	"batch_group_end":            true,
	"prerequisite_navigation":    true,
	"learning_skipped":           true,
	"temp_llm_skipped":           true,
	"decision_evaluated":         true,
	"pre_validation_completed":   true,

	// Smart Routing & Structured Output
	"smart_routing_start":     true,
	"smart_routing_end":       true,
	"structured_output_start": true,
	"structured_output_end":   true,
	"structured_output_error": true,

	// All Cache events (redundant with SKIP_EVENTS but good to be explicit)
	"cache_event":               true,
	"comprehensive_cache_event": true,
	"cache_hit":                 true,
	"cache_miss":                true,
	"cache_write":               true,
	"cache_expired":             true,
	"cache_cleanup":             true,
	"cache_error":               true,
	"cache_operation_start":     true,
}

// ShouldStoreEvent returns true if the event type should be stored in the database
func ShouldStoreEvent(eventType string) bool {
	return !DB_SKIP_EVENTS[eventType]
}

// BaseEventBridge contains the common functionality for all event bridges
type BaseEventBridge struct {
	EventStore *events.EventStore
	SessionID  string // Session ID for event storage
	Logger     loggerv2.Logger
	ChatDB     database.Database // Add database reference for chat history storage
	BridgeName string            // Name of the bridge (used for logging and ID prefix)
}

// HandleEvent processes events and converts them to server events
func (b *BaseEventBridge) HandleEvent(ctx context.Context, event *pkgevents.AgentEvent) error {
	// Skip events that have no UI component (reduces bandwidth and storage)
	if SKIP_EVENTS[string(event.Type)] {
		return nil
	}

	// Create server event with typed AgentEvent data directly - no conversion needed!
	serverEvent := events.Event{
		ID:        fmt.Sprintf("%s_%s_%d", b.BridgeName, event.Type, time.Now().UnixNano()),
		Type:      string(event.Type),
		Timestamp: time.Now(),
		Data:      event,       // Pass through the typed AgentEvent directly
		SessionID: b.SessionID, // Use sessionID for event storage
	}

	// Store the event in the server's event store for polling API
	// Use the session ID for in-memory storage (multiple observers can view the same session)
	if b.SessionID == "" {
		b.Logger.Warn("⚠️ [BaseEventBridge] SessionID is empty! Event will not be stored correctly.")
	}
	b.EventStore.AddEvent(b.SessionID, serverEvent)

	// ✅ CHAT HISTORY FIX: Store event in database for chat history
	if b.ChatDB != nil && !DB_SKIP_EVENTS[string(event.Type)] {
		// Extract hierarchy information from event data if available
		hierarchyLevel := 0
		component := b.BridgeName

		// Try to extract hierarchy info from BaseEventData if the event data has it
		if baseData, ok := event.Data.(interface {
			GetBaseEventData() *pkgevents.BaseEventData
		}); ok {
			if base := baseData.GetBaseEventData(); base != nil {
				hierarchyLevel = base.HierarchyLevel
				if base.Component != "" {
					component = base.Component
				}
			}
		}

		// Convert unified event to database-compatible agent event
		agentEvent := &pkgevents.AgentEvent{
			Type:            event.Type,
			Timestamp:       event.Timestamp,
			EventIndex:      0, // Will be set by database
			TraceID:         event.TraceID,
			SpanID:          event.SpanID,
			ParentID:        event.ParentID,
			CorrelationID:   event.CorrelationID, // Preserve delegation hierarchy
			HierarchyLevel:  hierarchyLevel,      // Use extracted hierarchy level
			SessionID:       b.SessionID,          // Use sessionID for database storage
			Component:       component,            // Use extracted component
		}

		// Store in database using the session ID (same as chat session)
		if err := b.ChatDB.StoreEvent(ctx, b.SessionID, agentEvent); err != nil {
			b.Logger.Warn(fmt.Sprintf("Failed to store event in DB: %v", err))
		}
	}

	return nil
}
