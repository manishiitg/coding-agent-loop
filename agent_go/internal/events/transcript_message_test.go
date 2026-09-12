package events

import (
	"encoding/json"
	"reflect"
	"testing"

	pkgevents "github.com/manishiitg/mcpagent/events"
)

func transcriptMessage(id, content string) Event {
	return Event{ID: id, Type: "streaming_chunk", Data: &pkgevents.AgentEvent{
		Type: pkgevents.EventType("streaming_chunk"),
		Data: &pkgevents.StreamingChunkEvent{Source: "transcript", Content: content},
	}}
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
