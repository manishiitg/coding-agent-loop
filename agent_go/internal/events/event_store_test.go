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

func TestAddEventAssignsExecutionOwnership(t *testing.T) {
	store := NewEventStore(10)
	defer store.Stop()

	now := time.Now()
	store.AddEvent("session-1", Event{
		ID:        "agent-start",
		Type:      "orchestrator_agent_start",
		Timestamp: now,
		SessionID: "session-1",
		Data: &pkgevents.AgentEvent{
			Type:          pkgevents.EventType("orchestrator_agent_start"),
			Timestamp:     now,
			SpanID:        "agent-span",
			CorrelationID: "agent-123",
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{"agent_type": "worker"},
			},
		},
	})
	store.AddEvent("session-1", Event{
		ID:        "child-tool",
		Type:      string(pkgevents.ToolCallStart),
		Timestamp: now.Add(time.Millisecond),
		SessionID: "session-1",
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.ToolCallStart,
			Timestamp: now.Add(time.Millisecond),
			ParentID:  "agent-span",
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{"tool_name": "execute_shell_command"},
			},
		},
	})

	events := store.GetAllEventsRaw("session-1")
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	for _, event := range events {
		if event.ExecutionID != "agent:agent-123" {
			t.Fatalf("expected %s to belong to agent execution, got %q", event.ID, event.ExecutionID)
		}
		if event.ParentExecutionID != "main:session-1" {
			t.Fatalf("expected %s parent execution main:session-1, got %q", event.ID, event.ParentExecutionID)
		}
		if event.ExecutionKind != "agent" {
			t.Fatalf("expected %s execution kind agent, got %q", event.ID, event.ExecutionKind)
		}
	}
}

func TestAddEventAssignsDelegationExecutionOwnership(t *testing.T) {
	store := NewEventStore(10)
	defer store.Stop()

	now := time.Now()
	store.AddEvent("session-1", Event{
		ID:        "delegation-start",
		Type:      "delegation_start",
		Timestamp: now,
		SessionID: "session-1",
		Data: &pkgevents.AgentEvent{
			Type:          pkgevents.EventType("delegation_start"),
			Timestamp:     now,
			CorrelationID: "delegation-123",
			Data: &DelegationStartEventData{
				DelegationID: "delegation-123",
				Instruction:  "Check the repo",
				Timestamp:    now.Format(time.RFC3339),
			},
		},
	})

	events := store.GetAllEventsRaw("session-1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ExecutionID != "delegation:delegation-123" {
		t.Fatalf("expected delegation execution ownership, got %q", events[0].ExecutionID)
	}
	if events[0].ParentExecutionID != "main:session-1" {
		t.Fatalf("expected delegation parent main:session-1, got %q", events[0].ParentExecutionID)
	}
	if events[0].ExecutionKind != "delegation" {
		t.Fatalf("expected delegation kind, got %q", events[0].ExecutionKind)
	}
}

func TestAddEventAssignsWorkflowStepOwnershipFromMetadata(t *testing.T) {
	store := NewEventStore(10)
	defer store.Stop()

	now := time.Now()
	store.AddEvent("session-1", Event{
		ID:        "workshop-tool",
		Type:      string(pkgevents.ToolCallStart),
		Timestamp: now,
		SessionID: "session-1",
		Data: &pkgevents.AgentEvent{
			Type:          pkgevents.ToolCallStart,
			Timestamp:     now,
			CorrelationID: "workshop-workflow-full-123",
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{
					"tool_name": "execute_shell_command",
					"metadata": map[string]interface{}{
						"workshop_step_id":     "workshop-workflow-full-123",
						"current_step_id":      "prepare-test-fixtures",
						"orchestrator_step_id": "prepare-test-fixtures",
					},
				},
			},
		},
	})

	events := store.GetAllEventsRaw("session-1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ExecutionID != "workflow-step:workflow-full-123:prepare-test-fixtures" {
		t.Fatalf("expected workflow step execution ownership, got %q", events[0].ExecutionID)
	}
	if events[0].ParentExecutionID != "workflow-full-123" {
		t.Fatalf("expected workflow step parent workflow-full-123, got %q", events[0].ParentExecutionID)
	}
	if events[0].ExecutionKind != "workflow_step" {
		t.Fatalf("expected workflow_step kind, got %q", events[0].ExecutionKind)
	}
}
