package orchestrator

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/events"
	baseevents "github.com/manishiitg/mcpagent/events"
)

type recordingListener struct {
	mu     sync.Mutex
	events []*baseevents.AgentEvent
}

func (l *recordingListener) HandleEvent(_ context.Context, event *baseevents.AgentEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
	return nil
}

func (l *recordingListener) Name() string { return "recording" }

func (l *recordingListener) types() []baseevents.EventType {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]baseevents.EventType, 0, len(l.events))
	for _, event := range l.events {
		out = append(out, event.Type)
	}
	return out
}

// A workflow-step approval answered from any surface must leave a durable
// marker; without it a refreshed chat shows the answered prompt as pending.
func TestRequestHumanFeedbackEmitsResolutionMarkerWhenAnswered(t *testing.T) {
	listener := &recordingListener{}
	bo := &BaseOrchestrator{contextAwareBridge: listener}
	requestID := "req-orchestrator-resolved-" + time.Now().Format("150405.000000000")

	done := make(chan error, 1)
	go func() {
		_, _, err := bo.RequestHumanFeedback(context.Background(), requestID, "Approve plan?", "", "sess-1", "wf-1")
		done <- err
	}()

	store := virtualtools.GetHumanFeedbackStore()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := store.SubmitResponse(requestID, "Approve & Continue"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("feedback request never became answerable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := <-done; err != nil {
		t.Fatalf("RequestHumanFeedback: %v", err)
	}

	types := listener.types()
	if len(types) != 2 || types[0] != events.BlockingHumanFeedback || types[1] != events.HumanFeedbackResolved {
		t.Fatalf("emitted %v, want [blocking_human_feedback human_feedback_resolved]", types)
	}
	raw, err := json.Marshal(listener.events[1].Data)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["request_id"] != requestID || payload["outcome"] != "answered" {
		t.Fatalf("marker payload must carry flat request_id/outcome, got %s", raw)
	}
}
