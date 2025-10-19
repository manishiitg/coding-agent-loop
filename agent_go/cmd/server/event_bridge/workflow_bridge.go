package eventbridge

import (
	"context"

	pkgevents "mcp-agent/agent_go/pkg/events"
)

// WorkflowEventBridge bridges workflow events to the main server event system
type WorkflowEventBridge struct {
	*BaseEventBridge
}

// Name returns the bridge name
func (b *WorkflowEventBridge) Name() string {
	return "workflow_event_bridge"
}

// HandleEvent processes workflow events and converts them to server events
func (b *WorkflowEventBridge) HandleEvent(ctx context.Context, event *pkgevents.AgentEvent) error {
	return b.BaseEventBridge.HandleEvent(ctx, event)
}
