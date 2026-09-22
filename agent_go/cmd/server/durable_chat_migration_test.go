package server

import (
	"testing"
)

func TestLegacyChatMigrationInterleavesToolTraceWithMessages(t *testing.T) {
	raw := []byte(`{
		"session_id":"chat-1",
		"updated_at":"2026-09-22T12:00:00Z",
		"conversation_history":[
			{"Role":"human","Parts":[{"Text":"first question"}]},
			{"Role":"ai","Parts":[{"Text":"first answer"}]},
			{"Role":"human","Parts":[{"Text":"second question"}]},
			{"Role":"ai","Parts":[{"Text":"second answer"}]}
		],
		"ui_events":[
			{"id":"trace-user","type":"user_message","timestamp":"2026-09-22T11:59:58Z","data":{"type":"user_message","data":{"content":"second question"}}},
			{"id":"trace-tool","type":"tool_call_start","timestamp":"2026-09-22T11:59:59Z","data":{"type":"tool_call_start","data":{"tool_name":"exec"}}},
			{"id":"trace-answer","type":"unified_completion","timestamp":"2026-09-22T12:00:00Z","data":{"type":"unified_completion","data":{"final_result":"second answer"}}}
		]
	}`)
	got, err := decodeLegacyChatEvents(raw, "chat-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"user_message", "streaming_chunk", "user_message", "tool_call_start", "streaming_chunk"}
	if len(got) != len(want) {
		t.Fatalf("migrated %d events, want %d: %+v", len(got), len(want), got)
	}
	for i, event := range got {
		if event.Type != want[i] {
			t.Fatalf("event %d type = %q, want %q", i, event.Type, want[i])
		}
	}
	if got[3].ID != "trace-tool" {
		t.Fatalf("tool call did not stay between its user and answer: %+v", got)
	}
}
