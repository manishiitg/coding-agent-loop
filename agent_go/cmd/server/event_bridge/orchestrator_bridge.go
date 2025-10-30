package eventbridge

import (
	"context"

	pkgevents "mcp-agent/agent_go/pkg/events"
)

// OrchestratorAgentEventBridge bridges individual agent events from within orchestrator to the main server event system
type OrchestratorAgentEventBridge struct {
	*BaseEventBridge
}

// Name returns the bridge name
func (b *OrchestratorAgentEventBridge) Name() string {
	return "orchestrator_agent_event_bridge"
}

// HandleEvent processes agent events from within orchestrator and converts them to server events
func (b *OrchestratorAgentEventBridge) HandleEvent(ctx context.Context, event *pkgevents.AgentEvent) error {
	return b.BaseEventBridge.HandleEvent(ctx, event)
}
