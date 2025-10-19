package eventbridge

import (
	"context"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/events"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/database"
	pkgevents "mcp-agent/agent_go/pkg/events"
)

// EventBridge defines the interface for event bridges
type EventBridge interface {
	Name() string
	HandleEvent(ctx context.Context, event *pkgevents.AgentEvent) error
}

// BaseEventBridge contains the common functionality for all event bridges
type BaseEventBridge struct {
	EventStore      *events.EventStore
	ObserverManager *events.ObserverManager
	ObserverID      string // Observer ID for polling API
	SessionID       string // Session ID for database storage
	Logger          utils.ExtendedLogger
	ChatDB          database.Database // Add database reference for chat history storage
	BridgeName      string // Name of the bridge (used for logging and ID prefix)
}

// HandleEvent processes events and converts them to server events
func (b *BaseEventBridge) HandleEvent(ctx context.Context, event *pkgevents.AgentEvent) error {
	b.Logger.Infof("[%s EVENT BRIDGE] Processing %s event: %s", b.BridgeName, b.BridgeName, event.Type)

	// Create server event with typed AgentEvent data directly - no conversion needed!
	serverEvent := events.Event{
		ID:        fmt.Sprintf("%s_%s_%d", b.BridgeName, event.Type, time.Now().UnixNano()),
		Type:      string(event.Type),
		Timestamp: time.Now(),
		Data:      event,        // Pass through the typed AgentEvent directly
		SessionID: b.ObserverID, // Use observerID for in-memory storage (polling)
	}

	// Store the event in the server's event store for polling API
	// Use the observer ID for in-memory storage (this is what the frontend polls)
	b.EventStore.AddEvent(b.ObserverID, serverEvent)

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
		b.Logger.Infof("[%s EVENT BRIDGE] DEBUG: Using sessionID=%s for database storage (observerID=%s)", b.BridgeName, b.SessionID, b.ObserverID)
		if err := b.ChatDB.StoreEvent(ctx, b.SessionID, agentEvent); err != nil {
			b.Logger.Errorf("[%s EVENT BRIDGE] Failed to store event in database: %v", b.BridgeName, err)
		} else {
			b.Logger.Infof("[%s EVENT BRIDGE] Stored event %s in database for chat history (hierarchy: %d, component: %s)", b.BridgeName, event.Type, hierarchyLevel, component)
		}
	}

	b.Logger.Infof("[%s EVENT BRIDGE] Successfully bridged %s event: %s (typed data preserved)", b.BridgeName, b.BridgeName, event.Type)
	return nil
}
