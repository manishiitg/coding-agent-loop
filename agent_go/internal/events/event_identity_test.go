package events

import (
	"testing"
	"time"

	agentevents "github.com/manishiitg/mcpagent/events"
)

func TestStableAgentEventIDUsesTurnForCompletionBoundary(t *testing.T) {
	completion := agentevents.NewUnifiedCompletionEvent("coding_agent", "retained", "", "done", "completed", time.Second, 1)
	first := agentevents.NewAgentEvent(completion)
	first.TurnID = "turn-1"
	second := agentevents.NewAgentEvent(completion)
	second.TurnID = "turn-1"
	second.Timestamp = first.Timestamp.Add(time.Minute)
	if got, want := StableAgentEventID("bridge", second), StableAgentEventID("bridge", first); got != want {
		t.Fatalf("re-emitted completion ID = %q, want %q", got, want)
	}
}

func TestStableAgentEventIDUsesCorrelationForPairedType(t *testing.T) {
	first := agentevents.NewAgentEvent(agentevents.NewToolCallStartEvent(1, "shell", agentevents.ToolParams{}, "", ""))
	first.Data.(interface {
		GetBaseEventData() *agentevents.BaseEventData
	}).GetBaseEventData().EventID = ""
	first.CorrelationID = "call-1"
	second := *first
	second.Timestamp = first.Timestamp.Add(time.Minute)
	if got, want := StableAgentEventID("bridge", &second), StableAgentEventID("bridge", first); got != want {
		t.Fatalf("re-emitted correlated event ID = %q, want %q", got, want)
	}
}
