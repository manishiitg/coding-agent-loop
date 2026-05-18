package orchestrator

import (
	"context"
	"testing"
	"time"

	mcpagent_events "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	orchevents "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
)

type captureEventListener struct {
	event *mcpagent_events.AgentEvent
}

func (l *captureEventListener) Name() string { return "capture" }

func (l *captureEventListener) HandleEvent(ctx context.Context, event *mcpagent_events.AgentEvent) error {
	l.event = event
	return nil
}

func TestContextAwareBridgeTagsTerminalStreamWithExecutionOwner(t *testing.T) {
	listener := &captureEventListener{}
	bridge := NewContextAwareEventBridge(listener, loggerv2.NewNoop())
	bridge.SetCurrentStepID("shared-step")

	ctx := context.WithValue(context.Background(), orchevents.ParentExecutionIDKey, "sub-exec-rds-evidence-123")
	event := &mcpagent_events.AgentEvent{
		Type:      mcpagent_events.StreamingChunk,
		Timestamp: time.Now(),
		Data: &mcpagent_events.StreamingChunkEvent{
			BaseEventData: mcpagent_events.BaseEventData{
				Timestamp: time.Now(),
				Metadata: map[string]interface{}{
					"kind": "terminal",
				},
			},
			Content:    "terminal snapshot",
			ChunkIndex: 1,
		},
	}

	if err := bridge.HandleEvent(ctx, event); err != nil {
		t.Fatalf("HandleEvent returned error: %v", err)
	}
	if listener.event == nil {
		t.Fatal("event was not forwarded")
	}
	chunk, ok := listener.event.Data.(*mcpagent_events.StreamingChunkEvent)
	if !ok {
		t.Fatalf("forwarded event data = %T, want *StreamingChunkEvent", listener.event.Data)
	}
	metadata := chunk.GetBaseEventData().Metadata
	if got := metadata["execution_owner_id"]; got != "sub-exec-rds-evidence-123" {
		t.Fatalf("execution_owner_id = %v, want sub-exec-rds-evidence-123", got)
	}
	if got := metadata["background_agent_id"]; got != "sub-exec-rds-evidence-123" {
		t.Fatalf("background_agent_id = %v, want sub-exec-rds-evidence-123", got)
	}
	if got := metadata["current_step_id"]; got != "shared-step" {
		t.Fatalf("current_step_id = %v, want shared-step", got)
	}
}
