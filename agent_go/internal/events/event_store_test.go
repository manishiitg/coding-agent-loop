package events

import (
	"testing"
	"time"

	pkgevents "github.com/manishiitg/mcpagent/events"
)

func TestAddEventSnapshotsAgentEvent(t *testing.T) {
	store := NewEventStore(10)
	defer store.Stop()

	original := &pkgevents.AgentEvent{
		Type:      pkgevents.LLMGenerationError,
		Timestamp: time.Now(),
		Data: &pkgevents.LLMGenerationErrorEvent{
			BaseEventData: pkgevents.BaseEventData{
				Metadata: map[string]interface{}{"reason": "before"},
			},
			Turn:     1,
			ModelID:  "gemini-cli/auto",
			Error:    "choice.Content is empty",
			Duration: time.Second,
		},
	}

	store.AddEvent("session-1", Event{
		ID:        "evt-1",
		Type:      string(pkgevents.LLMGenerationError),
		Timestamp: original.Timestamp,
		SessionID: "session-1",
		Data:      original,
	})

	originalData := original.Data.(*pkgevents.LLMGenerationErrorEvent)
	originalData.Error = "mutated"
	originalData.Metadata["reason"] = "after"

	stored := store.events["session-1"][0].Data
	if stored == original {
		t.Fatal("expected stored event to use a detached snapshot")
	}

	storedData, ok := stored.Data.(*pkgevents.LLMGenerationErrorEvent)
	if !ok {
		t.Fatalf("expected *LLMGenerationErrorEvent, got %T", stored.Data)
	}

	if storedData.Error != "choice.Content is empty" {
		t.Fatalf("expected stored event error to remain unchanged, got %q", storedData.Error)
	}
	if storedData.Metadata["reason"] != "before" {
		t.Fatalf("expected stored metadata to remain unchanged, got %v", storedData.Metadata["reason"])
	}
}
