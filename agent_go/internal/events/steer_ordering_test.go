package events

import (
	"testing"
	"time"

	pkgevents "github.com/manishiitg/mcpagent/events"
)

func steerCompletion(id string) Event {
	return Event{ID: id, Type: "unified_completion", Data: &pkgevents.AgentEvent{
		Type: pkgevents.EventType("unified_completion"),
		Data: NewGenericEventData("unified_completion", map[string]interface{}{"final_result": id}),
	}}
}

func liveIDs(store *EventStore, sessionID string) []string {
	result := store.GetEvents(sessionID, GetEventsOptions{SinceIndex: -1})
	ids := make([]string, 0, len(result.Events))
	for _, event := range result.Events {
		ids = append(ids, event.ID)
	}
	return ids
}

func TestDeferredSteerHoldReleasesOnTimeoutWithoutAck(t *testing.T) {
	previous := deferredSteerHoldTimeout
	deferredSteerHoldTimeout = 50 * time.Millisecond
	defer func() { deferredSteerHoldTimeout = previous }()
	store := NewEventStore(100)
	defer store.Stop()

	store.BeginDeferredSteer("chat")
	store.AddEvent("chat", transcriptMessage("answer-a", "first answer"))
	store.AddEvent("chat", steerCompletion("completion-a"))
	store.AddEvent("chat", transcriptMessage("answer-b", "held until release"))
	if got := liveIDs(store, "chat"); len(got) != 2 {
		t.Fatalf("rows after the boundary were not held: %v", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(liveIDs(store, "chat")) < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("held row never released: %v", liveIDs(store, "chat"))
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A late End after the timeout is harmless.
	store.EndDeferredSteer("chat")
	store.AddEvent("chat", transcriptMessage("after", "flows normally"))
	if got := liveIDs(store, "chat"); got[len(got)-1] != "after" {
		t.Fatalf("rows after release were not appended in order: %v", got)
	}
}

func TestDeferredSteerDoesNotHoldOtherSessionsOrChildRows(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()
	store.BeginDeferredSteer("chat")
	defer store.EndDeferredSteer("chat")
	store.AddEvent("chat", steerCompletion("completion-a"))
	child := transcriptMessage("child", "sub-agent output")
	child.ExecutionKind = "delegation"
	store.AddEvent("chat", child)
	store.AddEvent("other", transcriptMessage("other-answer", "unrelated"))
	if got := liveIDs(store, "chat"); len(got) != 2 {
		t.Fatalf("child row was held: %v", got)
	}
	if got := liveIDs(store, "other"); len(got) != 1 {
		t.Fatalf("other session was held: %v", got)
	}
}
