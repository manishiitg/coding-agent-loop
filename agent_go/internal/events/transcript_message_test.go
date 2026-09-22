package events

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	pkgevents "github.com/manishiitg/mcpagent/events"
)

func transcriptMessage(id, content string) Event {
	return Event{ID: id, Type: "streaming_chunk", Data: &pkgevents.AgentEvent{
		Type: pkgevents.EventType("streaming_chunk"),
		Data: &pkgevents.StreamingChunkEvent{Source: "transcript", Content: content},
	}}
}

func TestDurableChatProjectionBoundsLargeTranscriptWithDiagnosticReference(t *testing.T) {
	event := transcriptMessage("large-message", strings.Repeat("x", 200*1024))
	projected, keep := projectDurableChatEvent(event)
	if !keep {
		t.Fatal("large transcript was dropped")
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxDurableChatEventBytes {
		t.Fatalf("projected row is %d bytes, limit %d", len(encoded), maxDurableChatEventBytes)
	}
	var document map[string]interface{}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	data := document["data"].(map[string]interface{})["data"].(map[string]interface{})
	if data["payload_truncated"] != true || data["artifact_event_id"] != "large-message" {
		t.Fatalf("missing diagnostic reference: %+v", data)
	}
}

func TestDurableChatProjectionHardBoundsManyLargeFields(t *testing.T) {
	fields := map[string]interface{}{}
	for _, key := range []string{"content", "final_result", "result", "error", "message", "question", "tool_name", "tool_call_id", "name", "status", "role", "source"} {
		fields[key] = strings.Repeat(key, 32*1024)
	}
	event := Event{ID: "large-tool", Type: "tool_call_end", Data: &pkgevents.AgentEvent{
		Type: pkgevents.EventType("tool_call_end"), Data: NewGenericEventData("tool_call_end", fields),
	}}
	projected, keep := projectDurableChatEvent(event)
	if !keep {
		t.Fatal("large tool event was dropped")
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxDurableChatEventBytes {
		t.Fatalf("projected row is %d bytes, limit %d", len(encoded), maxDurableChatEventBytes)
	}
}

func TestTranscriptMessageClassificationSurvivesJSON(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chunk pkgevents.StreamingChunkEvent
		want  bool
	}{
		{"message", pkgevents.StreamingChunkEvent{Source: "transcript", Content: "Checking the helpers."}, true},
		{"delta", pkgevents.StreamingChunkEvent{Source: "transcript", Content: "Check", IsDelta: true}, false},
		{"tool", pkgevents.StreamingChunkEvent{Source: "transcript", Content: "exec", IsToolCall: true}, false},
		{"blank", pkgevents.StreamingChunkEvent{Source: "transcript", Content: "  "}, false},
		{"terminal", pkgevents.StreamingChunkEvent{Source: "terminal", Content: "screen"}, false},
		{"replacement", pkgevents.StreamingChunkEvent{Source: "transcript", Content: "screen", BaseEventData: pkgevents.BaseEventData{Metadata: map[string]interface{}{"replace": true}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := transcriptMessage("message", "")
			event.Data.Data = &tc.chunk
			if got := IsTranscriptMessage(event); got != tc.want {
				t.Fatalf("typed = %v, want %v", got, tc.want)
			}
			encoded, err := json.Marshal(event.Data.Data)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]interface{}
			if err := json.Unmarshal(encoded, &payload); err != nil {
				t.Fatal(err)
			}
			restored := transcriptMessage("restored", "")
			restored.Data.Data = &pkgevents.GenericEventData{Data: payload}
			if got := IsTranscriptMessage(restored); got != tc.want {
				t.Fatalf("restored = %v, want %v", got, tc.want)
			}
			encoded, err = json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if restored.ID != event.ID || IsTranscriptMessage(restored) != tc.want {
				t.Fatalf("persisted event did not round-trip: %+v", restored)
			}
		})
	}
}

func TestDurableChatProjectionKeepsSemanticMainEventsOnly(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  bool
	}{
		{name: "user message", event: Event{Type: "user_message", ExecutionKind: "main_agent"}, want: true},
		{name: "whole assistant message", event: transcriptMessage("reply", "Hello"), want: true},
		{name: "token chunk", event: Event{Type: "streaming_chunk", ExecutionKind: "main_agent", Data: pkgevents.NewAgentEvent(&pkgevents.StreamingChunkEvent{Source: "llm", Content: "H", IsDelta: true})}},
		{name: "system prompt", event: Event{Type: "system_prompt", ExecutionKind: "main_agent"}},
		{name: "main tool", event: Event{Type: "tool_call_end", ExecutionKind: "main_agent"}, want: true},
		{name: "child tool", event: Event{Type: "tool_call_end", ExecutionKind: "sub_agent"}},
		{name: "child summary", event: Event{Type: "background_agent_completed", ExecutionKind: "sub_agent"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got := projectDurableChatEvent(test.event)
			if got != test.want {
				t.Fatalf("durable=%v, want %v", got, test.want)
			}
		})
	}
}

func TestTranscriptMessagesReachPollingAndPagedCatchup(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()
	store.InitializeSession("s", 0)
	store.events["s"] = []Event{
		{ID: "user", Type: "user_message"},
		transcriptMessage("first", "Checking the helpers."),
		{ID: "screen", Type: "streaming_chunk"},
		{ID: "tool", Type: "tool_call_start"},
		transcriptMessage("second", "Now validating."),
		{ID: "final", Type: "conversation_end"},
	}
	ids := func(events []Event) []string {
		var out []string
		for _, e := range events {
			out = append(out, e.ID)
		}
		return out
	}
	want := []string{"user", "first", "tool", "second", "final"}
	if got := ids(store.GetEvents("s", GetEventsOptions{SinceIndex: -1}).Events); !reflect.DeepEqual(got, want) {
		t.Fatalf("polling = %v, want %v", got, want)
	}
	var paged []Event
	cursor := -1
	for {
		page := store.GetForwardEventPage("s", cursor, 2)
		paged = append(paged, page.Events...)
		if !page.HasMore {
			break
		}
		if page.LastProcessedIndex <= cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = page.LastProcessedIndex
	}
	if got := ids(paged); !reflect.DeepEqual(got, want) {
		t.Fatalf("paged catchup = %v, want %v", got, want)
	}
	retained, _ := pruneEventsForRetention(store.events["s"], 5)
	if got := ids(retained); !reflect.DeepEqual(got, want) {
		t.Fatalf("retention = %v, want %v", got, want)
	}
}
