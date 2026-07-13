package server

import (
	"testing"
	"time"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"

	pkgevents "github.com/manishiitg/mcpagent/events"
)

func TestBuildSessionExecutionTreeIncludesCompletedMainAgentFromEvents(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	now := time.Now()
	sessionID := "session-1"
	store.AddEvent(sessionID, internalevents.Event{
		ID:        "tool-start",
		Type:      string(pkgevents.ToolCallStart),
		Timestamp: now,
		SessionID: sessionID,
		Data: &pkgevents.AgentEvent{
			Type:          pkgevents.ToolCallStart,
			Timestamp:     now,
			CorrelationID: "tool-call-1",
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{"tool_name": "execute_shell_command"},
			},
		},
	})
	store.AddEvent(sessionID, internalevents.Event{
		ID:        "agent-end",
		Type:      "agent_end",
		Timestamp: now.Add(time.Second),
		SessionID: sessionID,
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.EventType("agent_end"),
			Timestamp: now.Add(time.Second),
		},
	})

	api := &StreamingAPI{
		eventStore:      store,
		bgAgentRegistry: NewBackgroundAgentRegistry(),
	}

	tree := api.buildSessionExecutionTree(&ActiveSessionInfo{
		SessionID:    sessionID,
		AgentMode:    "simple",
		Status:       "completed",
		CreatedAt:    now.Add(-time.Second),
		LastActivity: now.Add(time.Second),
		Query:        "hello",
	})
	if tree == nil {
		t.Fatal("expected execution tree")
	}
	if len(tree.Root.Children) != 1 {
		t.Fatalf("expected one root child, got %d", len(tree.Root.Children))
	}
	main := tree.Root.Children[0]
	if main.ExecutionID != "main:"+sessionID {
		t.Fatalf("expected main node, got %q", main.ExecutionID)
	}
	if main.Status != trackedExecutionStatusCompleted {
		t.Fatalf("expected completed main node, got %q", main.Status)
	}
}

func TestBuildSessionExecutionTreeFinalizesEventDerivedNodesForCompletedSession(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	now := time.Now()
	sessionID := "session-1"
	store.AddEvent(sessionID, internalevents.Event{
		ID:                "step-start",
		Type:              "pre_validation_completed",
		Timestamp:         now,
		SessionID:         sessionID,
		ExecutionID:       "workflow-step:one",
		ParentExecutionID: "workflow:root",
		ExecutionKind:     "workflow_step",
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.EventType("pre_validation_completed"),
			Timestamp: now,
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{"step_id": "one"},
			},
		},
	})

	api := &StreamingAPI{
		eventStore:      store,
		bgAgentRegistry: NewBackgroundAgentRegistry(),
	}

	tree := api.buildSessionExecutionTree(&ActiveSessionInfo{
		SessionID:    sessionID,
		AgentMode:    "workflow_phase",
		Status:       "completed",
		CreatedAt:    now.Add(-time.Second),
		LastActivity: now.Add(time.Second),
		Query:        "run workflow",
	})
	if tree == nil {
		t.Fatal("expected execution tree")
	}
	if tree.Summary.RunningCount != 0 {
		t.Fatalf("expected no running executions, got %d", tree.Summary.RunningCount)
	}
	if tree.Summary.DisplayStatus != sessionExecutionDisplayStopped {
		t.Fatalf("expected stopped display status, got %q", tree.Summary.DisplayStatus)
	}

	var step *SessionExecutionTreeNode
	var walk func(*SessionExecutionTreeNode)
	walk = func(node *SessionExecutionTreeNode) {
		if node == nil || step != nil {
			return
		}
		if node.ExecutionID == "workflow-step:one" {
			step = node
			return
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(tree.Root)
	if step == nil {
		t.Fatal("expected workflow step node")
	}
	if step.Status != trackedExecutionStatusCompleted {
		t.Fatalf("expected completed workflow step, got %q", step.Status)
	}
}

func TestBuildSessionExecutionTreeKeepsOldRunningBackgroundAgentActive(t *testing.T) {
	now := time.Now()
	sessionID := "session-old-running-bg"
	registry := NewBackgroundAgentRegistry()
	registry.Register(sessionID, &BackgroundAgent{
		ID:        "work-0001",
		Name:      "Workflow: old running",
		SessionID: sessionID,
		Kind:      "workflow_run_tool",
		Status:    BGAgentRunning,
		CreatedAt: now.Add(-time.Hour),
	})

	api := &StreamingAPI{
		bgAgentRegistry: registry,
	}
	tree := api.buildSessionExecutionTree(&ActiveSessionInfo{
		SessionID:    sessionID,
		AgentMode:    "multi-agent",
		Status:       "completed",
		CreatedAt:    now.Add(-time.Hour),
		LastActivity: now.Add(-time.Hour),
		Query:        "run workflow",
	})
	if tree == nil {
		t.Fatal("expected execution tree")
	}
	if tree.Summary.RunningCount != 1 {
		t.Fatalf("expected old running background agent to count as running, got %d", tree.Summary.RunningCount)
	}
	if !tree.Summary.HasRunningBackgroundAgents {
		t.Fatal("old running background agent should mark tree summary as background-running")
	}

	var oldRunning *SessionExecutionTreeNode
	var walk func(*SessionExecutionTreeNode)
	walk = func(node *SessionExecutionTreeNode) {
		if node == nil || oldRunning != nil {
			return
		}
		if node.ExecutionID == "work-0001" {
			oldRunning = node
			return
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(tree.Root)
	if oldRunning == nil {
		t.Fatal("expected old running background node to remain inspectable")
	}
	if oldRunning.Status != "running" {
		t.Fatalf("expected running node status, got %q", oldRunning.Status)
	}
}

func TestBuildSessionExecutionTreeIgnoresStreamingLifecycleEvents(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	now := time.Now()
	sessionID := "session-1"
	store.AddEvent(sessionID, internalevents.Event{
		ID:        "streaming-end",
		Type:      "streaming_end",
		Timestamp: now,
		SessionID: sessionID,
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.EventType("streaming_end"),
			Timestamp: now,
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{"step_id": "streaming-noise"},
			},
		},
	})

	api := &StreamingAPI{
		eventStore:      store,
		bgAgentRegistry: NewBackgroundAgentRegistry(),
	}

	tree := api.buildSessionExecutionTree(&ActiveSessionInfo{
		SessionID:    sessionID,
		AgentMode:    "workflow_phase",
		Status:       "running",
		CreatedAt:    now.Add(-time.Second),
		LastActivity: now,
		Query:        "run workflow",
	})
	if tree == nil {
		t.Fatal("expected execution tree")
	}

	var step *SessionExecutionTreeNode
	var walk func(*SessionExecutionTreeNode)
	walk = func(node *SessionExecutionTreeNode) {
		if node == nil || step != nil {
			return
		}
		if node.ExecutionID == "workflow-step:streaming-noise" {
			step = node
			return
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(tree.Root)
	if step != nil {
		t.Fatalf("streaming lifecycle event should not create execution-tree node: %#v", step)
	}
}

func TestSynthesizeSessionInfoFromEvents(t *testing.T) {
	now := time.Now()
	sessionID := "session-1"
	session := synthesizeSessionInfoFromEvents(sessionID, []internalevents.Event{
		{
			ID:        "user-message",
			Type:      "user_message",
			Timestamp: now,
			SessionID: sessionID,
			Data: &pkgevents.AgentEvent{
				Type:      pkgevents.UserMessage,
				Timestamp: now,
				Data: &pkgevents.GenericEventData{
					Data: map[string]interface{}{"content": "run workflow"},
				},
			},
		},
		{
			ID:        "done",
			Type:      "unified_completion",
			Timestamp: now.Add(time.Second),
			SessionID: sessionID,
			Data: &pkgevents.AgentEvent{
				Type:      pkgevents.EventType("unified_completion"),
				Timestamp: now.Add(time.Second),
			},
		},
	})
	if session == nil {
		t.Fatal("expected synthesized session")
	}
	if session.SessionID != sessionID {
		t.Fatalf("expected session %q, got %q", sessionID, session.SessionID)
	}
	if session.Status != trackedExecutionStatusCompleted {
		t.Fatalf("expected completed status, got %q", session.Status)
	}
	if session.Query != "run workflow" {
		t.Fatalf("expected query from user message, got %q", session.Query)
	}
}

func TestEventDerivedExecutionStatusTreatsWorkflowLifecycleAsTerminal(t *testing.T) {
	tests := []struct {
		eventType string
		want      string
		failed    bool
	}{
		{eventType: "workflow_end", want: trackedExecutionStatusCompleted},
		{eventType: "batch_execution_end", want: trackedExecutionStatusCompleted},
		{eventType: "batch_group_end", want: trackedExecutionStatusCompleted},
		{eventType: "todo_task_step_completed", want: trackedExecutionStatusCompleted},
		{eventType: "workflow_error", want: trackedExecutionStatusFailed, failed: true},
		{eventType: "context_canceled", want: trackedExecutionStatusCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			status, completed, failed := eventDerivedExecutionStatus(internalevents.Event{Type: tt.eventType}, nil)
			if !completed {
				t.Fatalf("expected %s to be terminal", tt.eventType)
			}
			if status != tt.want {
				t.Fatalf("expected status %q, got %q", tt.want, status)
			}
			if failed != tt.failed {
				t.Fatalf("expected failed=%v, got %v", tt.failed, failed)
			}
		})
	}
}
