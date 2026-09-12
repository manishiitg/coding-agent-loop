package server

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	pkgevents "github.com/manishiitg/mcpagent/events"
)

func chunkEvent(id, execID string) internalevents.Event {
	return internalevents.Event{ID: id, Type: "streaming_chunk", ExecutionID: execID}
}

func otherEvent(id, typ string) internalevents.Event {
	return internalevents.Event{ID: id, Type: typ}
}

// Only the last frame of a run survives -- it is the one the UI would have
// rendered anyway, since chunks overwrite the pane rather than appending.
func TestCollapseStreamingChunksKeepsOnlyLastOfEachRun(t *testing.T) {
	got := collapseChatHistoryStreamingChunks([]internalevents.Event{
		otherEvent("user", "user_message"),
		chunkEvent("c1", "main"),
		chunkEvent("c2", "main"),
		chunkEvent("c3", "main"),
		otherEvent("tool", "tool_call_start"),
		chunkEvent("c4", "main"),
		chunkEvent("c5", "main"),
		otherEvent("end", "llm_generation_end"),
	})

	want := []string{"user", "c3", "tool", "c5", "end"}
	if len(got) != len(want) {
		t.Fatalf("got %d events %v, want %d %v", len(got), eventIDs(got), len(want), want)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("event[%d] = %q, want %q (all: %v)", i, got[i].ID, id, eventIDs(got))
		}
	}
}

// Two terminals streaming at once must not collapse into each other: keeping
// only the globally-last chunk would erase one terminal's final pane.
func TestCollapseStreamingChunksKeepsEachExecutionsFinalFrame(t *testing.T) {
	got := collapseChatHistoryStreamingChunks([]internalevents.Event{
		chunkEvent("a1", "exec-a"),
		chunkEvent("a2", "exec-a"),
		chunkEvent("b1", "exec-b"),
		chunkEvent("b2", "exec-b"),
		chunkEvent("a3", "exec-a"),
	})

	want := []string{"a2", "b2", "a3"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", eventIDs(got), want)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("event[%d] = %q, want %q (all: %v)", i, got[i].ID, id, eventIDs(got))
		}
	}
}

// Non-chunk events must pass through untouched, in order.
func TestCollapseStreamingChunksLeavesConversationEventsAlone(t *testing.T) {
	in := []internalevents.Event{
		otherEvent("a", "user_message"),
		otherEvent("b", "tool_call_start"),
		otherEvent("c", "tool_call_end"),
		otherEvent("d", "llm_generation_end"),
	}
	got := collapseChatHistoryStreamingChunks(in)
	if len(got) != len(in) {
		t.Fatalf("got %v, want all %d preserved", eventIDs(got), len(in))
	}
	for i := range in {
		if got[i].ID != in[i].ID {
			t.Errorf("event[%d] = %q, want %q", i, got[i].ID, in[i].ID)
		}
	}
}

// Collapsing runs before the cap is the point: a real conversation was 58%
// streaming_chunk, so the cap used to evict genuine conversation events to
// make room for duplicate screens. Shaped after the measured production file
// (129 events, 75 of them chunks).
func TestCollapseStreamingChunksRunsBeforeTheEventCap(t *testing.T) {
	var events []internalevents.Event
	for turn := 0; turn < 30; turn++ {
		events = append(events, otherEvent(fmt.Sprintf("tool-%02d", turn), "tool_call_start"))
		for frame := 0; frame < 10; frame++ {
			events = append(events, chunkEvent(fmt.Sprintf("chunk-%02d-%02d", turn, frame), "main"))
		}
		events = append(events, otherEvent(fmt.Sprintf("answer-%02d", turn), "llm_generation_end"))
	}

	trimmed := trimChatHistoryUIEvents(events)
	if len(trimmed) > maxPersistedChatHistoryUIEvents {
		t.Fatalf("trimmed to %d events, over the %d cap", len(trimmed), maxPersistedChatHistoryUIEvents)
	}

	// Every answer must survive; before collapsing, 300 duplicate frames would
	// have pushed the early ones out of the retained window.
	for turn := 0; turn < 30; turn++ {
		want := fmt.Sprintf("answer-%02d", turn)
		found := false
		for _, e := range trimmed {
			if e.ID == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s was evicted by duplicate pane frames", want)
		}
	}
}

func eventIDs(events []internalevents.Event) []string {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	return ids
}

func TestCollapseStreamingChunksPreservesAdjacentTranscriptMessages(t *testing.T) {
	message := func(id string) internalevents.Event {
		e := chunkEvent(id, "main")
		e.Data = &pkgevents.AgentEvent{Data: &pkgevents.StreamingChunkEvent{Source: "transcript", Content: id}}
		return e
	}
	input := []internalevents.Event{
		chunkEvent("frame-1", "main"), chunkEvent("frame-2", "main"),
		message("Checking helpers."), message("Now inserting the render function."),
		chunkEvent("frame-3", "main"), chunkEvent("frame-4", "main"),
		otherEvent("tool", "tool_call_start"), message("Now validating."),
		chunkEvent("frame-5", "main"), otherEvent("final", "unified_completion"),
	}
	got := collapseChatHistoryStreamingChunks(input)
	want := []string{"frame-2", "Checking helpers.", "Now inserting the render function.", "frame-4", "tool", "Now validating.", "frame-5", "final"}
	if !reflect.DeepEqual(eventIDs(got), want) {
		t.Fatalf("got %v, want %v", eventIDs(got), want)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var restored []internalevents.Event
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if ids := eventIDs(collapseChatHistoryStreamingChunks(restored)); !reflect.DeepEqual(ids, want) {
		t.Fatalf("restored got %v, want %v", ids, want)
	}
	projected := attachChatHistoryUIEventsForResume([]byte(`{"session_id":"s"}`), encoded)
	var doc struct {
		Events []internalevents.Event `json:"ui_events"`
	}
	if err := json.Unmarshal(projected, &doc); err != nil {
		t.Fatal(err)
	}
	visible := []string{"Checking helpers.", "Now inserting the render function.", "tool", "Now validating.", "final"}
	if ids := eventIDs(doc.Events); !reflect.DeepEqual(ids, visible) {
		t.Fatalf("resume got %v, want %v", ids, visible)
	}
}
