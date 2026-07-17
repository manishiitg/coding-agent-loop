package server

import (
	"testing"
	"time"
)

func TestRunningWorkflowListIncludesWorkflowBuilderTask(t *testing.T) {
	startedAt := time.Now().UTC()
	api := &StreamingAPI{
		trackedWorkflowExecutions: map[string]*TrackedWorkflowExecution{
			"builder-1": {
				ExecutionID:   "builder-1",
				SessionID:     "session-builder",
				Source:        trackedExecutionSourceWorkshopBackground,
				Kind:          "workflow_builder_task",
				Name:          "Review plan drift",
				Title:         "Review plan drift",
				PresetQueryID: "preset-1",
				WorkspacePath: "Workflow/rts-video",
				PhaseID:       "workflow-builder",
				PhaseName:     "Workflow Builder",
				Status:        trackedExecutionStatusRunning,
				UserID:        "user-1",
				StartedAt:     startedAt,
			},
		},
	}

	running := api.listRunningWorkflowExecutions("user-1")
	if len(running) != 1 {
		t.Fatalf("running len = %d, want 1", len(running))
	}
	if running[0].SessionID != "session-builder" {
		t.Fatalf("session_id = %q, want session-builder", running[0].SessionID)
	}
	if running[0].Kind != "workflow_builder_task" {
		t.Fatalf("kind = %q, want workflow_builder_task", running[0].Kind)
	}

	api.trackedWorkflowExecutionsMux.RLock()
	found := api.runningWorkflowListExecutionBySessionLocked("session-builder")
	api.trackedWorkflowExecutionsMux.RUnlock()
	if found == nil {
		t.Fatal("runningWorkflowListExecutionBySessionLocked did not find builder task")
	}
}

func TestRunningWorkflowListKeepsInternalWorkflowStepsOut(t *testing.T) {
	api := &StreamingAPI{
		trackedWorkflowExecutions: map[string]*TrackedWorkflowExecution{
			"step-1": {
				ExecutionID:   "step-1",
				SessionID:     "session-builder",
				Source:        trackedExecutionSourceWorkshopBackground,
				Kind:          "workflow_step",
				Name:          "Step -> collect data",
				WorkspacePath: "Workflow/rts-video",
				PhaseID:       "workflow-builder",
				Status:        trackedExecutionStatusRunning,
				UserID:        "user-1",
				StartedAt:     time.Now().UTC(),
			},
		},
	}

	running := api.listRunningWorkflowExecutions("user-1")
	if len(running) != 0 {
		t.Fatalf("running len = %d, want 0 for internal workflow step", len(running))
	}
}

func TestFindRunningTrackedExecutionForWorkspaceWhereDoesNotLetNewerScheduleHideBuilder(t *testing.T) {
	now := time.Now().UTC()
	api := &StreamingAPI{
		trackedWorkflowExecutions: map[string]*TrackedWorkflowExecution{
			"builder": {
				ExecutionID:   "builder",
				SessionID:     "chat-session",
				WorkspacePath: "Workflow/demo",
				PhaseID:       "workflow-builder",
				Status:        trackedExecutionStatusRunning,
				StartedAt:     now.Add(-time.Minute),
			},
			"schedule": {
				ExecutionID:   "schedule",
				SessionID:     "schedule-cron--demo_1",
				WorkspacePath: "Workflow/demo",
				PhaseID:       "execute-workflow",
				Status:        trackedExecutionStatusRunning,
				TriggeredBy:   "cron",
				StartedAt:     now,
			},
		},
	}

	found := api.findRunningTrackedExecutionForWorkspaceWhere("Workflow/demo", func(exec *TrackedWorkflowExecution) bool {
		return exec.PhaseID == "workflow-builder"
	})
	if found == nil || found.SessionID != "chat-session" {
		t.Fatalf("builder lookup = %#v, want chat-session", found)
	}
}
