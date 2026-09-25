package events

import (
	"reflect"
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

func steerRow(id, eventType string) Event {
	return Event{ID: id, Type: eventType, Data: &pkgevents.AgentEvent{
		Type: pkgevents.EventType(eventType),
		Data: NewGenericEventData(eventType, map[string]interface{}{"tool_name": id}),
	}}
}

func steerUser(id string) Event {
	return Event{ID: id, Type: "user_message", Data: &pkgevents.AgentEvent{
		Type: pkgevents.EventType("user_message"),
		Data: NewGenericEventData("user_message", map[string]interface{}{"content": id}),
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

func expectIDs(t *testing.T, store *EventStore, sessionID string, want ...string) {
	t.Helper()
	if got := liveIDs(store, sessionID); !(len(got) == 0 && len(want) == 0) && !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func waitForIDs(t *testing.T, store *EventStore, sessionID string, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(liveIDs(store, sessionID)) < count {
		if time.Now().After(deadline) {
			t.Fatalf("rows never released: %v", liveIDs(store, sessionID))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDeferredSteerIntoBusySessionLetsInFlightAnswerFinish(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()
	store.AddEvent("chat", transcriptMessage("answer-a", "in flight"))

	user := steerUser("user:b")
	store.BeginDeferredSteer("chat", user)
	store.AddEvent("chat", steerRow("tool-a", "tool_call_start"))
	store.AddEvent("chat", steerCompletion("completion-a"))
	store.AddEvent("chat", transcriptMessage("answer-b", "reply to b"))
	expectIDs(t, store, "chat", "answer-a", "tool-a", "completion-a")

	store.CompleteDeferredSteer("chat", user)
	expectIDs(t, store, "chat", "answer-a", "tool-a", "completion-a", "user:b", "answer-b")
}

// RTS, Cursor: an idle retained session; the reply's first tool call reached
// the journal ~0.7s before the durable ack wrote the user's message.
func TestDeferredSteerIntoIdleSessionHoldsTheReplyImmediately(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()
	store.AddEvent("chat", transcriptMessage("answer-a", "earlier answer"))
	store.AddEvent("chat", steerCompletion("completion-a"))

	user := steerUser("user:yes")
	store.BeginDeferredSteer("chat", user)
	store.AddEvent("chat", steerRow("tool-b", "tool_call_start"))
	store.AddEvent("chat", transcriptMessage("answer-b", "reply to yes"))
	expectIDs(t, store, "chat", "answer-a", "completion-a")

	store.CompleteDeferredSteer("chat", user)
	expectIDs(t, store, "chat", "answer-a", "completion-a", "user:yes", "tool-b", "answer-b")
}

func TestDeferredSteerIntoFreshSessionHoldsImmediately(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()
	user := steerUser("user:first")
	store.BeginDeferredSteer("chat", user)
	store.AddEvent("chat", transcriptMessage("answer", "reply"))
	expectIDs(t, store, "chat")
	store.CompleteDeferredSteer("chat", user)
	expectIDs(t, store, "chat", "user:first", "answer")
}

func TestTurnEndWithoutCompletionCountsAsIdle(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()
	store.AddEvent("chat", transcriptMessage("answer-a", "cancelled answer"))
	store.AddEvent("chat", steerRow("end-a", "context_cancelled"))

	user := steerUser("user:b")
	store.BeginDeferredSteer("chat", user)
	store.AddEvent("chat", transcriptMessage("answer-b", "reply"))
	store.CompleteDeferredSteer("chat", user)
	expectIDs(t, store, "chat", "answer-a", "end-a", "user:b", "answer-b")
}

func TestDeferredSteerTimeoutWritesUserBeforeReleasingReply(t *testing.T) {
	previous := deferredSteerHoldTimeout
	deferredSteerHoldTimeout = 50 * time.Millisecond
	defer func() { deferredSteerHoldTimeout = previous }()
	store := NewEventStore(100)
	defer store.Stop()

	user := steerUser("user:slow")
	store.BeginDeferredSteer("chat", user)
	store.AddEvent("chat", transcriptMessage("answer", "reply before any ack"))
	expectIDs(t, store, "chat")
	waitForIDs(t, store, "chat", 2)
	expectIDs(t, store, "chat", "user:slow", "answer")

	// The late ack must not write the user row a second time.
	store.CompleteDeferredSteer("chat", user)
	store.AddEvent("chat", transcriptMessage("after", "flows normally"))
	expectIDs(t, store, "chat", "user:slow", "answer", "after")
}

func TestDeferredSteerDoesNotHoldOtherSessionsOrChildRows(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()
	user := steerUser("user:b")
	store.BeginDeferredSteer("chat", user)
	defer store.CompleteDeferredSteer("chat", user)
	child := transcriptMessage("child", "sub-agent output")
	child.ExecutionKind = "delegation"
	store.AddEvent("chat", child)
	store.AddEvent("other", transcriptMessage("other-answer", "unrelated"))
	expectIDs(t, store, "chat", "child")
	expectIDs(t, store, "other", "other-answer")
}

// A message sent while the previous answer is still streaming is taken by
// the CLI only when that answer ends. The short timeout must count from that
// end: counted from the send, it wrote the second message into the middle of
// the first answer (msg1, msg2, rest of reply1, reply2) although the CLI ran
// msg1 -> reply1 -> msg2 -> reply2.
func TestDeferredSteerTimeoutStartsWhenTheInFlightAnswerEnds(t *testing.T) {
	previous := deferredSteerHoldTimeout
	deferredSteerHoldTimeout = 50 * time.Millisecond
	defer func() { deferredSteerHoldTimeout = previous }()
	store := NewEventStore(100)
	defer store.Stop()
	store.AddEvent("chat", steerUser("user:a"))
	store.AddEvent("chat", transcriptMessage("answer-a1", "first part"))

	user := steerUser("user:b")
	store.BeginDeferredSteer("chat", user)
	// The first answer keeps streaming well past the short timeout.
	time.Sleep(150 * time.Millisecond)
	store.AddEvent("chat", transcriptMessage("answer-a2", "second part"))
	expectIDs(t, store, "chat", "user:a", "answer-a1", "answer-a2")
	store.AddEvent("chat", steerCompletion("completion-a"))

	// Now the CLI takes message b; its ack arrives before the short timeout.
	store.CompleteDeferredSteer("chat", user)
	store.AddEvent("chat", transcriptMessage("answer-b", "reply to b"))
	expectIDs(t, store, "chat", "user:a", "answer-a1", "answer-a2", "completion-a", "user:b", "answer-b")
}

// Without an ack, the user row still appears, but after the first answer.
func TestDeferredSteerTimeoutAfterTheAnswerKeepsOrder(t *testing.T) {
	previous := deferredSteerHoldTimeout
	deferredSteerHoldTimeout = 50 * time.Millisecond
	defer func() { deferredSteerHoldTimeout = previous }()
	store := NewEventStore(100)
	defer store.Stop()
	store.AddEvent("chat", steerUser("user:a"))
	store.AddEvent("chat", transcriptMessage("answer-a1", "first part"))
	store.BeginDeferredSteer("chat", steerUser("user:b"))
	time.Sleep(120 * time.Millisecond)
	store.AddEvent("chat", steerCompletion("completion-a"))
	store.AddEvent("chat", transcriptMessage("answer-b", "reply to b"))
	waitForIDs(t, store, "chat", 5)
	expectIDs(t, store, "chat", "user:a", "answer-a1", "completion-a", "user:b", "answer-b")
}
