package server

import (
	"testing"
	"time"
)

func TestBuildActiveSessionInfoSummaryKeepsCompletedStatusForBackgroundAgents(t *testing.T) {
	const sessionID = "session-bg-completed"

	api := &StreamingAPI{
		bgAgentRegistry: NewBackgroundAgentRegistry(),
		trackedWorkflowExecutions: map[string]*TrackedWorkflowExecution{
			"bg-agent": {
				ExecutionID:   "bg-agent",
				SessionID:     sessionID,
				Source:        trackedExecutionSourceWorkshopBackground,
				Status:        trackedExecutionStatusRunning,
				Name:          "Background follow-up",
				WorkspacePath: "Workflow/test",
				StartedAt:     time.Now(),
			},
		},
	}

	api.bgAgentRegistry.Register(sessionID, &BackgroundAgent{
		ID:        "bg-agent",
		Name:      "Background follow-up",
		SessionID: sessionID,
		Status:    BGAgentRunning,
		CreatedAt: time.Now(),
		Metadata: map[string]string{
			"workflow_path": "Workflow/test",
		},
	})

	summary := api.buildActiveSessionInfoSummary(&ActiveSessionInfo{
		SessionID: sessionID,
		Status:    "completed",
		CreatedAt: time.Now(),
	})

	if summary.Status != "completed" {
		t.Fatalf("expected completed foreground status, got %q", summary.Status)
	}
	if !summary.HasRunningBackgroundAgents {
		t.Fatal("expected running background agent flag")
	}
	if summary.RunningBackgroundAgentCount != 1 {
		t.Fatalf("expected 1 running background agent, got %d", summary.RunningBackgroundAgentCount)
	}
	if summary.CurrentExecutionName != "Background follow-up" {
		t.Fatalf("expected current background name, got %q", summary.CurrentExecutionName)
	}
}
