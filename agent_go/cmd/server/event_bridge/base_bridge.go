package eventbridge

import (
	"context"
	"fmt"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	orchestratorevents "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/events"
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
		Error:     errorEventText(event.Data),
	}
	if b.SessionID == "" {
		b.Logger.Warn("⚠️ [BaseEventBridge] SessionID is empty! Event will not be stored correctly.")
	}
	b.EventStore.AddEvent(b.SessionID, serverEvent)
	return nil
}

// errorEventText surfaces the flat error string a handful of typed events
// carry on their own Data payload, so a caller that only sees the generic
// stored Event — scheduledTurnFailure, which classifies a scheduled run as a
// capacity wall (PLAT-101) versus a real failure by matching text like
// "[quota_exhausted]" — gets the actual failure detail instead of the
// top-level Event.Error this bridge otherwise leaves blank for every event
// type. Without this, that classification silently falls back to a generic
// "turn ended with <type>" string with no error text to match against, and a
// transient capacity wall gets recorded as a hard failure instead of
// suspending and auto-resuming.
func errorEventText(data pkgevents.EventData) string {
	switch e := data.(type) {
	case *orchestratorevents.OrchestratorAgentErrorEvent:
		return e.Error
	case *pkgevents.AgentErrorEvent:
		return e.Error
	case *pkgevents.ConversationErrorEvent:
		return e.Error
	default:
		return ""
	}
}
