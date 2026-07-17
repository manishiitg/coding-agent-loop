package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
)

func TestActiveSessionsIncludesTrackedWorkflowWithoutChatRow(t *testing.T) {
	const sessionID = "workflow-after-restart"
	startedAt := time.Now().Add(-time.Minute).UTC()
	api := &StreamingAPI{
		runtimeCoordinator: NewRuntimeCoordinator(),
		activeSessions:     map[string]*ActiveSessionInfo{},
		trackedWorkflowExecutions: map[string]*TrackedWorkflowExecution{
			"workflow-1": {
				ExecutionID: "workflow-1", SessionID: sessionID,
				Source: trackedExecutionSourceWorkflowRun, Kind: "workflow", Status: trackedExecutionStatusRunning,
				PresetName: "Daily QA", WorkspacePath: "Workflow/daily-qa", StartedAt: startedAt,
			},
		},
	}

	recorder := httptest.NewRecorder()
	api.handleGetActiveSessions(recorder, httptest.NewRequest("GET", "/api/sessions/active", nil))
	var response GetActiveSessionsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode active sessions: %v", err)
	}
	if len(response.ActiveSessions) != 1 {
		t.Fatalf("active sessions = %#v, want one tracked workflow", response.ActiveSessions)
	}
	session := response.ActiveSessions[0]
	if session.SessionID != sessionID || session.WorkflowName != "Daily QA" || session.RuntimeState == nil {
		t.Fatalf("tracked workflow was not synthesized into runtime index: %#v", session)
	}
	if session.RuntimeState.Phase != runtimePhaseRunning || session.DisplayStatus != sessionExecutionDisplayBusy {
		t.Fatalf("synthesized runtime state = %#v display=%q", session.RuntimeState, session.DisplayStatus)
	}
}

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

func TestBuildActiveSessionInfoSummaryKeepsOldRunningBackgroundAgentActive(t *testing.T) {
	const sessionID = "session-bg-old-running"

	api := &StreamingAPI{
		bgAgentRegistry: NewBackgroundAgentRegistry(),
	}
	api.bgAgentRegistry.Register(sessionID, &BackgroundAgent{
		ID:        "bg-old-running",
		Name:      "Old background follow-up",
		SessionID: sessionID,
		Status:    BGAgentRunning,
		CreatedAt: time.Now().Add(-time.Hour),
	})

	summary := api.buildActiveSessionInfoSummary(&ActiveSessionInfo{
		SessionID: sessionID,
		Status:    "completed",
		CreatedAt: time.Now().Add(-time.Hour),
	})

	if !summary.HasRunningBackgroundAgents {
		t.Fatal("old running background agent should keep the session active")
	}
	if summary.RunningBackgroundAgentCount != 1 {
		t.Fatalf("expected 1 running background agent, got %d", summary.RunningBackgroundAgentCount)
	}
	if summary.CurrentExecutionName != "Old background follow-up" {
		t.Fatalf("expected current execution name for old running agent, got %q", summary.CurrentExecutionName)
	}
}

func TestBuildActiveSessionInfoSummaryReportsRetainedTmuxWithoutBusyStatus(t *testing.T) {
	const sessionID = "session-retained-tmux"
	store := terminals.NewStore()
	store.HandleEvent(sessionID, codingAgentTmuxReaperChunkEvent(time.Now(), sessionID, "main:"+sessionID, "mlp-codex-cli-retained"))
	api := &StreamingAPI{terminalStore: store}

	summary := api.buildActiveSessionInfoSummary(&ActiveSessionInfo{
		SessionID: sessionID,
		Status:    "completed",
		CreatedAt: time.Now(),
	})
	if !summary.HasRetainedTmuxSession {
		t.Fatal("expected active tmux pane to be reported even when session status is completed")
	}

	if _, ok := store.MarkCompleted(sessionID + ":main:" + sessionID); !ok {
		t.Fatal("expected terminal to be marked completed")
	}
	summary = api.buildActiveSessionInfoSummary(&ActiveSessionInfo{
		SessionID: sessionID,
		Status:    "completed",
		CreatedAt: time.Now(),
	})
	if summary.HasRetainedTmuxSession {
		t.Fatal("completed terminal snapshot should not be reported as retained tmux")
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
