package step_based_workflow

import (
	"context"
	"testing"
	"time"

	baseevents "github.com/manishiitg/mcpagent/events"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
)

type recordingExecutionNotifier struct {
	starts    []WorkshopExecutionStart
	completes []recordedExecutionComplete
}

type recordedExecutionComplete struct {
	id     string
	name   string
	result string
	meta   map[string]string
	err    error
}

func (n *recordingExecutionNotifier) OnExecutionStart(start WorkshopExecutionStart) {
	n.starts = append(n.starts, start)
}

func (n *recordingExecutionNotifier) OnExecutionComplete(execID, name, result string, meta map[string]string, err error) {
	n.completes = append(n.completes, recordedExecutionComplete{
		id:     execID,
		name:   name,
		result: result,
		meta:   meta,
		err:    err,
	})
}

func (n *recordingExecutionNotifier) OnExecutionTerminated(execID, name string) {}

func TestWorkflowProgressBridgeNotifiesStepStartAndCompletion(t *testing.T) {
	notifier := &recordingExecutionNotifier{}
	session := &WorkshopChatSession{
		StepRegistry:      NewWorkshopStepRegistry(),
		executionNotifier: notifier,
	}
	bridge := &workflowProgressBridge{
		session:   session,
		parentID:  "workflow-full-123",
		iteration: "iteration-0",
		groupName: "job-search",
	}

	start := &baseevents.AgentEvent{
		Type:      orchestrator_events.OrchestratorAgentStart,
		Timestamp: time.Now(),
		Data: &orchestrator_events.OrchestratorAgentStartEvent{
			AgentType: "todo_planner_execution",
			AgentName: "search-score-jobs",
			StepIndex: 2,
		},
	}
	if err := bridge.HandleEvent(context.Background(), start); err != nil {
		t.Fatalf("start HandleEvent failed: %v", err)
	}

	end := &baseevents.AgentEvent{
		Type:      orchestrator_events.OrchestratorAgentEnd,
		Timestamp: time.Now(),
		Data: &orchestrator_events.OrchestratorAgentEndEvent{
			AgentType: "todo_planner_execution",
			AgentName: "search-score-jobs",
			StepIndex: 2,
			Result:    "saved 5 candidates",
			Success:   true,
		},
	}
	if err := bridge.HandleEvent(context.Background(), end); err != nil {
		t.Fatalf("end HandleEvent failed: %v", err)
	}

	if len(notifier.starts) != 1 {
		t.Fatalf("expected one start notification, got %d", len(notifier.starts))
	}
	if len(notifier.completes) != 1 {
		t.Fatalf("expected one completion notification, got %d", len(notifier.completes))
	}
	if notifier.starts[0].ID != notifier.completes[0].id {
		t.Fatalf("expected start/end IDs to match, got %q and %q", notifier.starts[0].ID, notifier.completes[0].id)
	}
	if got := notifier.completes[0].meta["group_name"]; got != "job-search" {
		t.Fatalf("expected group metadata, got %q", got)
	}
	if got := notifier.completes[0].meta["iteration"]; got != "iteration-0" {
		t.Fatalf("expected iteration metadata, got %q", got)
	}
	if exec := session.StepRegistry.Get(notifier.completes[0].id); exec == nil {
		t.Fatal("expected progress execution to be registered")
	}
}

func TestWorkflowProgressBridgeNotifiesCompletionWithoutStart(t *testing.T) {
	notifier := &recordingExecutionNotifier{}
	session := &WorkshopChatSession{
		StepRegistry:      NewWorkshopStepRegistry(),
		executionNotifier: notifier,
	}
	bridge := &workflowProgressBridge{
		session:  session,
		parentID: "workflow-full-123",
	}

	end := &baseevents.AgentEvent{
		Type:      orchestrator_events.OrchestratorAgentEnd,
		Timestamp: time.Now(),
		Data: &orchestrator_events.OrchestratorAgentEndEvent{
			AgentType: "conditional",
			AgentName: "route-by-mode",
			StepIndex: 1,
			Error:     "route failed",
			Success:   false,
		},
	}
	if err := bridge.HandleEvent(context.Background(), end); err != nil {
		t.Fatalf("end HandleEvent failed: %v", err)
	}

	if len(notifier.starts) != 1 {
		t.Fatalf("expected synthesized start notification, got %d", len(notifier.starts))
	}
	if len(notifier.completes) != 1 {
		t.Fatalf("expected one completion notification, got %d", len(notifier.completes))
	}
	if notifier.completes[0].err == nil {
		t.Fatal("expected failure error")
	}
}
