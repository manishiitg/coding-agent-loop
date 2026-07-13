package server

import (
	"fmt"
	"testing"
	"time"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"

	pkgevents "github.com/manishiitg/mcpagent/events"
)

func findActivityNode(root *SessionActivityTreeNode, executionID string) *SessionActivityTreeNode {
	if root == nil {
		return nil
	}
	if root.ExecutionID == executionID {
		return root
	}
	for _, child := range root.Children {
		if found := findActivityNode(child, executionID); found != nil {
			return found
		}
	}
	return nil
}

func TestBuildSessionActivityTreeAttachesRetainedRouteSelectionToWorkflowStep(t *testing.T) {
	store := internalevents.NewEventStore(5)
	defer store.Stop()

	now := time.Now()
	sessionID := "session-activity-tree"
	workflowExecutionID := "workflow-full-123"
	stepExecutionID := "workflow-step:workflow-full-123:execution-regression-router"

	store.AddEvent(sessionID, internalevents.Event{
		ID:                "route-selected",
		Type:              "todo_task_route_selected",
		Timestamp:         now,
		SessionID:         sessionID,
		ExecutionID:       stepExecutionID,
		ParentExecutionID: workflowExecutionID,
		ExecutionKind:     "workflow_step",
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.EventType("todo_task_route_selected"),
			Timestamp: now,
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{
					"step_id":             "execution-regression-router",
					"step_title":          "Execution Regression Router",
					"selected_route_id":   "math-solver",
					"selected_route_name": "Math Solver",
					"todo_id_to_execute":  "math-solver-todo",
				},
			},
		},
	})

	for i := 0; i < 10; i++ {
		store.AddEvent(sessionID, internalevents.Event{
			ID:                fmt.Sprintf("tool-%02d", i),
			Type:              string(pkgevents.ToolCallStart),
			Timestamp:         now.Add(time.Duration(i+1) * time.Millisecond),
			SessionID:         sessionID,
			ExecutionID:       stepExecutionID,
			ParentExecutionID: workflowExecutionID,
			ExecutionKind:     "workflow_step",
			Data: &pkgevents.AgentEvent{
				Type:      pkgevents.ToolCallStart,
				Timestamp: now.Add(time.Duration(i+1) * time.Millisecond),
			},
		})
	}

	api := &StreamingAPI{
		eventStore:      store,
		bgAgentRegistry: NewBackgroundAgentRegistry(),
	}

	tree := api.buildSessionActivityTree(&ActiveSessionInfo{
		SessionID:    sessionID,
		AgentMode:    "workflow_phase",
		Status:       "completed",
		CreatedAt:    now.Add(-time.Second),
		LastActivity: now.Add(time.Second),
		Query:        "run workflow",
	})
	if tree == nil {
		t.Fatal("expected activity tree")
	}

	step := findActivityNode(tree.Root, stepExecutionID)
	if step == nil {
		t.Fatalf("expected workflow step node %q", stepExecutionID)
	}
	if len(step.Events) != 1 {
		t.Fatalf("expected one visible milestone event under step, got %d", len(step.Events))
	}
	if step.Events[0].Type != "todo_task_route_selected" {
		t.Fatalf("expected route-selected event under step, got %q", step.Events[0].Type)
	}
	if step.Events[0].Title != "Math Solver" {
		t.Fatalf("expected route title from payload, got %q", step.Events[0].Title)
	}
	if step.ToolEventCount == 0 {
		t.Fatal("expected noisy tool events to be summarized on the step")
	}
}

func TestBuildSessionActivityTreeKeepsPreValidationMilestoneVisible(t *testing.T) {
	store := internalevents.NewEventStore(20)
	defer store.Stop()

	now := time.Now()
	sessionID := "session-prevalidation-tree"
	stepExecutionID := "workflow-step:prepare-test-fixtures"

	store.AddEvent(sessionID, internalevents.Event{
		ID:                "prevalidation",
		Type:              "pre_validation_completed",
		Timestamp:         now,
		SessionID:         sessionID,
		ExecutionID:       stepExecutionID,
		ParentExecutionID: "main:" + sessionID,
		ExecutionKind:     "workflow_step",
		Data: &pkgevents.AgentEvent{
			Type:      pkgevents.EventType("pre_validation_completed"),
			Timestamp: now,
			Data: &pkgevents.GenericEventData{
				Data: map[string]interface{}{
					"step_id":    "prepare-test-fixtures",
					"step_title": "Prepare Regression Fixtures",
					"status":     "passed",
				},
			},
		},
	})

	api := &StreamingAPI{
		eventStore:      store,
		bgAgentRegistry: NewBackgroundAgentRegistry(),
	}

	tree := api.buildSessionActivityTree(&ActiveSessionInfo{
		SessionID:    sessionID,
		AgentMode:    "workflow_phase",
		Status:       "completed",
		CreatedAt:    now.Add(-time.Second),
		LastActivity: now.Add(time.Second),
		Query:        "run workflow",
	})
	if tree == nil {
		t.Fatal("expected activity tree")
	}

	step := findActivityNode(tree.Root, stepExecutionID)
	if step == nil {
		t.Fatalf("expected workflow step node %q", stepExecutionID)
	}
	if len(step.Events) != 1 {
		t.Fatalf("expected one milestone event under step, got %d", len(step.Events))
	}
	if step.Events[0].Type != "pre_validation_completed" {
		t.Fatalf("expected pre-validation event under step, got %q", step.Events[0].Type)
	}
	if step.Events[0].Summary != "passed" {
		t.Fatalf("expected pre-validation summary from payload, got %q", step.Events[0].Summary)
	}
}
