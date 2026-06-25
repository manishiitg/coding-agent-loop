package server

import (
	"testing"
	"time"

	"mcp-agent-builder-go/agent_go/internal/terminals"
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

func TestShouldCompleteIdleForegroundSessionDoesNotCompleteBusySession(t *testing.T) {
	const sessionID = "session-busy-foreground"
	api := &StreamingAPI{
		sessionBusy:      map[string]bool{sessionID: true},
		sessionBusySince: map[string]time.Time{sessionID: time.Now().Add(-time.Minute)},
	}

	if api.shouldCompleteIdleForegroundSession(sessionID, "running", false) {
		t.Fatal("stale busy foreground session should not be completed by passive status polling")
	}
	if !api.isSessionBusy(sessionID) {
		t.Fatal("status polling should not clear the busy flag")
	}
}

func TestAutoNotificationDoesNotClearStaleBusyWhenCodingTmuxLooksBusy(t *testing.T) {
	const sessionID = "session-busy-tmux"
	store := terminals.NewStore()
	store.HandleEvent(sessionID, terminalRouteChunkEvent(
		sessionID,
		"workflow-step:review-plan",
		"mlp-cursor-cli-int-test",
		"Cursor Agent\n\n ⠰⠰ Composing  1.87k tokens\n\n  → Add a follow-up  ctrl+c to stop\n",
		1,
	))
	api := &StreamingAPI{
		terminalStore:    store,
		sessionBusy:      map[string]bool{sessionID: true},
		sessionBusySince: map[string]time.Time{sessionID: time.Now().Add(-autoNotificationStaleBusyAfter - time.Second)},
	}

	if !api.isSessionBusyForAutoNotification(sessionID) {
		t.Fatal("busy coding tmux pane should keep auto-notification serialized behind foreground turn")
	}
	if !api.isSessionBusy(sessionID) {
		t.Fatal("auto-notification busy check should not clear busy while tmux pane looks active")
	}
}
