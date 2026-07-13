package eventbridge

import (
	"context"
	"fmt"
	"time"

	pkgevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
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

// BaseEventBridge contains the common functionality for all event bridges.
// Events are pushed to the in-memory EventStore only — there is no durable
// per-session event log.
type BaseEventBridge struct {
	EventStore *events.EventStore
	SessionID  string
	Logger     loggerv2.Logger
	BridgeName string
}

// HandleEvent pushes the event to the in-memory store for polling / SSE
// consumers, filtering out event types that have no UI component.
func (b *BaseEventBridge) HandleEvent(_ context.Context, event *pkgevents.AgentEvent) error {
	if SKIP_EVENTS[string(event.Type)] {
		return nil
	}

	serverEvent := events.Event{
		ID:        fmt.Sprintf("%s_%s_%d", b.BridgeName, event.Type, time.Now().UnixNano()),
		Type:      string(event.Type),
		Timestamp: time.Now(),
		Data:      event,
		SessionID: b.SessionID,
	}
	if b.SessionID == "" {
		b.Logger.Warn("⚠️ [BaseEventBridge] SessionID is empty! Event will not be stored correctly.")
	}
	b.EventStore.AddEvent(b.SessionID, serverEvent)
	return nil
}
