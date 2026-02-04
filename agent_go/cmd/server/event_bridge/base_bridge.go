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
	if b.ChatDB != nil {
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
			Type:           event.Type,
			Timestamp:      event.Timestamp,
			EventIndex:     0, // Will be set by database
			TraceID:        event.TraceID,
			SpanID:         event.SpanID,
			ParentID:       event.ParentID,
			HierarchyLevel: hierarchyLevel, // Use extracted hierarchy level
			SessionID:      b.SessionID,    // Use sessionID for database storage
			Component:      component,      // Use extracted component
		}

		// Store in database using the session ID (same as chat session)
		if err := b.ChatDB.StoreEvent(ctx, b.SessionID, agentEvent); err != nil {
			// Error storing event in database - continue execution
		}
	}

	return nil
}
