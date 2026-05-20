package events

import (
	"fmt"
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

func TestEventStoreTracksSessionOwner(t *testing.T) {
	store := NewEventStore(10)
	defer store.Stop()

	store.SetSessionOwner("session-1", "user-1")

	if owner := store.GetSessionOwner("session-1"); owner != "user-1" {
		t.Fatalf("expected owner user-1, got %q", owner)
	}

	store.SetSessionOwner("session-2", "")
	if owner := store.GetSessionOwner("session-2"); owner != "" {
		t.Fatalf("expected blank owner to be ignored, got %q", owner)
	}
}

func TestGetEventsCanIncludeStreamingForSSEBackfill(t *testing.T) {
	store := NewEventStore(10)
	defer store.Stop()

	now := time.Now()
	sessionID := "session-streaming-backfill"
	for _, eventType := range []string{
		"user_message",
		"streaming_start",
		"streaming_chunk",
		"streaming_end",
		"tool_call_start",
	} {
		store.AddEvent(sessionID, Event{
			ID:        eventType,
			Type:      eventType,
			Timestamp: now,
			SessionID: sessionID,
			Data: &pkgevents.AgentEvent{
				Type:      pkgevents.EventType(eventType),
				Timestamp: now,
			},
		})
	}

	defaultResult := store.GetEvents(sessionID, GetEventsOptions{SinceIndex: -1})
	if got := eventTypes(defaultResult.Events); contains(got, "streaming_start") || contains(got, "streaming_chunk") {
		t.Fatalf("default polling should hide streaming start/chunk, got %v", got)
	}
	if got := eventTypes(defaultResult.Events); !contains(got, "streaming_end") {
		t.Fatalf("default polling should keep streaming_end recoverable, got %v", got)
	}

	backfillResult := store.GetEvents(sessionID, GetEventsOptions{SinceIndex: -1, IncludeStreaming: true})
	if got := eventTypes(backfillResult.Events); !contains(got, "streaming_start") || !contains(got, "streaming_chunk") || !contains(got, "streaming_end") {
		t.Fatalf("SSE backfill should include streaming lifecycle events, got %v", got)
	}
}

func TestGetEventsInitialFetchPreservesStructuralEventsOutsideLimit(t *testing.T) {
	store := NewEventStore(InitialEventsLimit + 10)
	defer store.Stop()

	now := time.Now()
	sessionID := "session-initial-structural"
	store.AddEvent(sessionID, Event{
		ID:        "delegation-start",
		Type:      "delegation_start",
		Timestamp: now,
		SessionID: sessionID,
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.EventType("delegation_start"),
			Timestamp: now,
		},
	})

	for i := 0; i < InitialEventsLimit+5; i++ {
		eventID := fmt.Sprintf("tool-%03d", i)
		store.AddEvent(sessionID, Event{
			ID:        eventID,
			Type:      string(pkgevents.ToolCallStart),
			Timestamp: now.Add(time.Duration(i+1) * time.Millisecond),
			SessionID: sessionID,
			Data: &pkgevents.AgentEvent{
				Type:      pkgevents.ToolCallStart,
				Timestamp: now.Add(time.Duration(i+1) * time.Millisecond),
			},
		})
	}

	result := store.GetEvents(sessionID, GetEventsOptions{SinceIndex: 0})
	gotIDs := eventIDs(result.Events)
	if !contains(gotIDs, "delegation-start") {
		t.Fatalf("initial fetch should preserve old structural parent event, got ids %v", gotIDs)
	}
	if contains(gotIDs, "tool-000") {
		t.Fatalf("initial fetch should still cap old non-structural events, got ids %v", gotIDs)
	}
	if !contains(gotIDs, fmt.Sprintf("tool-%03d", InitialEventsLimit+4)) {
		t.Fatalf("initial fetch should include newest events, got ids %v", gotIDs)
	}
	if len(result.Events) != InitialEventsLimit+1 {
		t.Fatalf("expected capped window plus structural parent, got %d events", len(result.Events))
	}
	if result.Events[0].ID != "delegation-start" {
		t.Fatalf("expected preserved structural event to keep chronological order, first id %q", result.Events[0].ID)
	}
	if !result.HasMore {
		t.Fatal("expected hasMore to remain true when old non-structural events are omitted")
	}
}

func eventTypes(events []Event) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func eventIDs(events []Event) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func TestAddEventParentsDelegationToBackgroundAgent(t *testing.T) {
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
				DelegationID:      "delegation-123",
				BackgroundAgentID: "bg-agent-1",
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
	if events[0].ParentExecutionID != "bg-agent-1" {
		t.Fatalf("expected delegation parent bg-agent-1, got %q", events[0].ParentExecutionID)
	}
	if events[0].ExecutionKind != "delegation" {
		t.Fatalf("expected delegation kind, got %q", events[0].ExecutionKind)
	}
}

func TestAddEventUsesBackgroundAgentParentExecutionID(t *testing.T) {
	store := NewEventStore(10)
	defer store.Stop()

	now := time.Now()
	store.AddEvent("session-1", Event{
		ID:        "background-start",
		Type:      "background_agent_started",
		Timestamp: now,
		SessionID: "session-1",
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.EventType("background_agent_started"),
			Timestamp: now,
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{
					"agent_id":            "bg-agent-1",
					"parent_execution_id": "parent-bg-agent",
				},
			},
		},
	})

	events := store.GetAllEventsRaw("session-1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ExecutionID != "bg-agent-1" {
		t.Fatalf("expected background execution ownership, got %q", events[0].ExecutionID)
	}
	if events[0].ParentExecutionID != "parent-bg-agent" {
		t.Fatalf("expected background parent parent-bg-agent, got %q", events[0].ParentExecutionID)
	}
	if events[0].ExecutionKind != "background_agent" {
		t.Fatalf("expected background_agent kind, got %q", events[0].ExecutionKind)
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

func TestAddEventParentsWorkflowStepOwnershipToBackgroundExecution(t *testing.T) {
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
			CorrelationID: "workshop-step-prepare-test-fixtures-123",
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{
					"tool_name": "execute_shell_command",
					"metadata": map[string]interface{}{
						"workshop_step_id":    "workshop-step-prepare-test-fixtures-123",
						"current_step_id":     "prepare-test-fixtures",
						"parent_execution_id": "exec-prepare-test-fixtures-84000",
					},
				},
			},
		},
	})

	events := store.GetAllEventsRaw("session-1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ExecutionID != "workflow-step:exec-prepare-test-fixtures-84000:prepare-test-fixtures" {
		t.Fatalf("expected background-owned workflow step execution, got %q", events[0].ExecutionID)
	}
	if events[0].ParentExecutionID != "exec-prepare-test-fixtures-84000" {
		t.Fatalf("expected background execution parent, got %q", events[0].ParentExecutionID)
	}
	if events[0].ExecutionKind != "workflow_step" {
		t.Fatalf("expected workflow_step kind, got %q", events[0].ExecutionKind)
	}
}

func TestAddEventUsesParentExecutionForWorkflowLifecycleOwnership(t *testing.T) {
	store := NewEventStore(10)
	defer store.Stop()

	now := time.Now()
	store.AddEvent("session-1", Event{
		ID:        "workflow-start",
		Type:      "orchestrator_agent_start",
		Timestamp: now,
		SessionID: "session-1",
		Data: &pkgevents.AgentEvent{
			Type:          pkgevents.EventType("orchestrator_agent_start"),
			Timestamp:     now,
			CorrelationID: "workshop-workflow-full-generated-id",
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{
					"metadata": map[string]interface{}{
						"workshop_step_id":    "workshop-workflow-full-generated-id",
						"parent_execution_id": "workflow-full-real-id",
					},
				},
			},
		},
	})

	events := store.GetAllEventsRaw("session-1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ExecutionID != "workflow-full-real-id" {
		t.Fatalf("expected parent workflow execution ownership, got %q", events[0].ExecutionID)
	}
	if events[0].ParentExecutionID != "main:session-1" {
		t.Fatalf("expected workflow parent main:session-1, got %q", events[0].ParentExecutionID)
	}
	if events[0].ExecutionKind != "workflow" {
		t.Fatalf("expected workflow kind, got %q", events[0].ExecutionKind)
	}
}

func TestAddEventAssignsAutoNotificationToCompletedBackgroundExecution(t *testing.T) {
	store := NewEventStore(10)
	defer store.Stop()

	now := time.Now()
	store.AddEvent("session-1", Event{
		ID:        "auto-notification",
		Type:      "user_message",
		Timestamp: now,
		SessionID: "session-1",
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.EventType("user_message"),
			Timestamp: now,
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{
					"content": "[AUTO-NOTIFICATION]\nAgent 'step-1' (ID: workflow-full-abc-step-0-def) completed.\nStatus: completed",
				},
			},
		},
	})

	events := store.GetAllEventsRaw("session-1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ExecutionID != "workflow-full-abc-step-0-def" {
		t.Fatalf("expected auto-notification to belong to background execution, got %q", events[0].ExecutionID)
	}
	if events[0].ParentExecutionID != "main:session-1" {
		t.Fatalf("expected parent main:session-1, got %q", events[0].ParentExecutionID)
	}
	if events[0].ExecutionKind != "workflow" {
		t.Fatalf("expected workflow kind, got %q", events[0].ExecutionKind)
	}
}

func TestSubscribeReceivesLiveEvents(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()

	sessionID := "session-sub-live"
	sub := store.Subscribe(sessionID)
	defer store.Unsubscribe(sessionID, sub)

	store.AddEvent(sessionID, Event{
		ID:        "evt-1",
		Type:      "user_message",
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.EventType("user_message"),
			Timestamp: time.Now(),
		},
	})

	select {
	case event := <-sub.Ch:
		if event.Type != "user_message" {
			t.Fatalf("event type = %q, want user_message", event.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not receive event within 2s")
	}
}

func TestSubscribeFiltersHiddenEvents(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()

	sessionID := "session-sub-hidden"
	sub := store.Subscribe(sessionID)
	defer store.Unsubscribe(sessionID, sub)

	store.AddEvent(sessionID, Event{
		ID:        "evt-hidden",
		Type:      "agent_start",
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.EventType("agent_start"),
			Timestamp: time.Now(),
		},
	})

	store.AddEvent(sessionID, Event{
		ID:        "evt-visible",
		Type:      "unified_completion",
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.EventType("unified_completion"),
			Timestamp: time.Now(),
		},
	})

	select {
	case event := <-sub.Ch:
		if event.Type == "agent_start" {
			t.Fatal("subscriber should not receive HIDDEN_EVENTS (agent_start)")
		}
		if event.Type != "unified_completion" {
			t.Fatalf("first received event = %q, want unified_completion", event.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not receive any events")
	}
}

func TestSubscribeAlwaysReceivesStreamingEvents(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()

	sessionID := "session-sub-streaming"
	sub := store.Subscribe(sessionID)
	defer store.Unsubscribe(sessionID, sub)

	for _, eventType := range []string{"streaming_start", "streaming_chunk", "streaming_end"} {
		store.AddEvent(sessionID, Event{
			ID:        "evt-" + eventType,
			Type:      eventType,
			Timestamp: time.Now(),
			SessionID: sessionID,
			Data: &pkgevents.AgentEvent{
				Type:      pkgevents.EventType(eventType),
				Timestamp: time.Now(),
			},
		})
	}

	var received []string
	for i := 0; i < 3; i++ {
		select {
		case event := <-sub.Ch:
			received = append(received, event.Type)
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for event %d, received so far: %v", i, received)
		}
	}

	if len(received) != 3 {
		t.Fatalf("received %d events, want 3: %v", len(received), received)
	}
	if !contains(received, "streaming_start") || !contains(received, "streaming_chunk") || !contains(received, "streaming_end") {
		t.Fatalf("streaming events not all delivered to subscriber: %v", received)
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()

	sessionID := "session-unsub"
	sub := store.Subscribe(sessionID)

	store.Unsubscribe(sessionID, sub)

	_, ok := <-sub.Ch
	if ok {
		t.Fatal("channel should be closed after Unsubscribe")
	}
}

func TestShouldShowEventFiltersCorrectly(t *testing.T) {
	tests := []struct {
		eventType string
		want      bool
	}{
		{"user_message", true},
		{"unified_completion", true},
		{"tool_call_start", true},
		{"tool_call_end", true},
		{"streaming_end", true},
		{"agent_error", true},
		{"batch_execution_canceled", true},
		{"agent_start", false},
		{"system_prompt", false},
		{"conversation_start", false},
		{"streaming_start", false},
		{"streaming_chunk", false},
		{"tool_execution", false},
		{"cache_event", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			if got := ShouldShowEvent(tt.eventType); got != tt.want {
				t.Fatalf("ShouldShowEvent(%q) = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}
