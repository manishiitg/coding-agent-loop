package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/events"

	pkgevents "github.com/manishiitg/mcpagent/events"
)

func TestWriteSSEEventFormatsCorrectly(t *testing.T) {
	w := httptest.NewRecorder()

	msg := sseEventMessage{
		Events: []events.Event{
			{
				ID:        "evt-1",
				Type:      "user_message",
				Timestamp: time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
				SessionID: "sess-1",
			},
		},
		SessionStatus:      "running",
		LastProcessedIndex: 42,
	}

	err := writeSSEEvent(w, "event", 42, msg)
	if err != nil {
		t.Fatalf("writeSSEEvent error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "id: 42\n") {
		t.Fatalf("missing id line in SSE output:\n%s", body)
	}
	if !strings.Contains(body, "event: event\n") {
		t.Fatalf("missing event line in SSE output:\n%s", body)
	}
	if !strings.Contains(body, "data: ") {
		t.Fatalf("missing data line in SSE output:\n%s", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("SSE message should end with double newline:\n%q", body)
	}

	// Verify data is valid JSON
	dataLine := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimPrefix(line, "data: ")
		}
	}
	if dataLine == "" {
		t.Fatal("no data line found")
	}
	var parsed sseEventMessage
	if err := json.Unmarshal([]byte(dataLine), &parsed); err != nil {
		t.Fatalf("data line not valid JSON: %v\ndata: %s", err, dataLine)
	}
	if parsed.SessionStatus != "running" {
		t.Fatalf("parsed SessionStatus = %q, want running", parsed.SessionStatus)
	}
	if parsed.LastProcessedIndex != 42 {
		t.Fatalf("parsed LastProcessedIndex = %d, want 42", parsed.LastProcessedIndex)
	}
	if len(parsed.Events) != 1 {
		t.Fatalf("parsed events count = %d, want 1", len(parsed.Events))
	}
}

func TestWriteSSEEventNoIDWhenNegative(t *testing.T) {
	w := httptest.NewRecorder()

	msg := sseStatusMessage{SessionStatus: "idle"}
	err := writeSSEEvent(w, "status", -1, msg)
	if err != nil {
		t.Fatalf("writeSSEEvent error: %v", err)
	}

	body := w.Body.String()
	if strings.Contains(body, "id:") {
		t.Fatalf("negative id should not produce id line:\n%s", body)
	}
	if !strings.Contains(body, "event: status\n") {
		t.Fatalf("missing event line:\n%s", body)
	}
}

func TestWriteSSEEventRecoversPanic(t *testing.T) {
	w := httptest.NewRecorder()

	// A type that panics during JSON marshal (concurrent map read)
	// Simulate by using a channel which json.Marshal can't handle
	type unserializable struct {
		Ch chan int
	}

	err := writeSSEEvent(w, "event", 1, unserializable{Ch: make(chan int)})
	if err == nil {
		t.Fatal("expected error for unserializable data")
	}
}

func TestSSEEventMessageStructure(t *testing.T) {
	now := time.Now()
	msg := sseEventMessage{
		Events: []events.Event{
			{
				ID:        "evt-error-1",
				Type:      "conversation_error",
				Timestamp: now,
				SessionID: "sess-1",
				Data: &pkgevents.AgentEvent{
					Type:      pkgevents.EventType("conversation_error"),
					Timestamp: now,
					Data: &pkgevents.GenericEventData{
						Data: map[string]interface{}{
							"error": "CLI binary not found",
						},
					},
				},
			},
		},
		SessionStatus:      "error",
		LastProcessedIndex: 10,
	}

	jsonBytes, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if parsed["session_status"] != "error" {
		t.Fatalf("session_status = %v, want error", parsed["session_status"])
	}
	evts, ok := parsed["events"].([]interface{})
	if !ok || len(evts) != 1 {
		t.Fatalf("events not properly serialized: %v", parsed["events"])
	}
}
