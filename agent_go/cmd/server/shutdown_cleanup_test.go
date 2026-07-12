package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agent "mcp-agent-builder-go/agent_go/pkg/agentwrapper"
)

type shutdownTestWorkshopSession struct {
	closed        bool
	mainSessionID string
}

func (s *shutdownTestWorkshopSession) Close() {
	s.closed = true
}

func (s *shutdownTestWorkshopSession) MainSessionID() string {
	return s.mainSessionID
}

func TestCancelActiveWorkForShutdown(t *testing.T) {
	agentCanceled := false
	workflowCanceled := false
	workshop := &shutdownTestWorkshopSession{}

	api := &StreamingAPI{
		agentCancelFuncs: map[string]context.CancelFunc{
			"session-1": func() { agentCanceled = true },
		},
		workflowOrchestratorContexts: map[string]context.CancelFunc{
			"query-1": func() { workflowCanceled = true },
		},
		activeSessions: map[string]*ActiveSessionInfo{
			"session-1": {
				SessionID:       "session-1",
				Status:          "running",
				IsSyntheticTurn: true,
			},
		},
		sessionQueryIDs: map[string][]string{
			"session-1": {"query-1"},
		},
		sessionBusy: map[string]bool{
			"session-1": true,
		},
		trackedWorkflowExecutions: map[string]*TrackedWorkflowExecution{
			"exec-1": {
				ExecutionID: "exec-1",
				SessionID:   "session-1",
				Status:      trackedExecutionStatusRunning,
			},
		},
		bgAgentRegistry: NewBackgroundAgentRegistry(),
	}
	api.workshopChatSessions.Store("session-1", workshop)

	api.cancelActiveWorkForShutdown()

	if !agentCanceled {
		t.Fatalf("agent cancel func was not called")
	}
	if !workflowCanceled {
		t.Fatalf("workflow cancel func was not called")
	}
	if len(api.agentCancelFuncs) != 0 {
		t.Fatalf("agent cancel funcs were not cleared: %+v", api.agentCancelFuncs)
	}
	if len(api.workflowOrchestratorContexts) != 0 {
		t.Fatalf("workflow contexts were not cleared: %+v", api.workflowOrchestratorContexts)
	}
	if len(api.sessionQueryIDs) != 0 {
		t.Fatalf("session query IDs were not cleared: %+v", api.sessionQueryIDs)
	}
	if api.sessionBusy["session-1"] {
		t.Fatalf("session busy flag was not cleared")
	}
	if api.activeSessions["session-1"].IsSyntheticTurn {
		t.Fatalf("synthetic turn flag was not cleared")
	}
	if !workshop.closed {
		t.Fatalf("workshop session was not closed")
	}
	if _, ok := api.workshopChatSessions.Load("session-1"); ok {
		t.Fatalf("workshop session was not removed")
	}
	if got := api.trackedWorkflowExecutions["exec-1"].Status; got != trackedExecutionStatusCanceled {
		t.Fatalf("tracked execution status = %q, want %q", got, trackedExecutionStatusCanceled)
	}
}

func TestHandleStopSessionCancelsActiveWorkAndPreventsPaneReuse(t *testing.T) {
	agentCanceled := false
	workflowCanceled := false
	backgroundCanceled := false
	workshop := &shutdownTestWorkshopSession{mainSessionID: "session-1"}
	registry := NewBackgroundAgentRegistry()
	registry.Register("session-1", &BackgroundAgent{
		ID:        "bg-1",
		Name:      "background-check",
		SessionID: "session-1",
		Status:    BGAgentRunning,
		cancel:    func() { backgroundCanceled = true },
	})

	api := &StreamingAPI{
		agentCancelFuncs: map[string]context.CancelFunc{
			"session-1": func() { agentCanceled = true },
		},
		workflowOrchestratorContexts: map[string]context.CancelFunc{
			"query-1": func() { workflowCanceled = true },
		},
		activeWorkflowExecutions: map[string]*ActiveWorkflowExecution{
			"query-1": {QueryID: "query-1", SessionID: "session-1", Status: "running"},
		},
		trackedWorkflowExecutions: map[string]*TrackedWorkflowExecution{
			"exec-1": {ExecutionID: "exec-1", SessionID: "session-1", Status: trackedExecutionStatusRunning},
		},
		sessionQueryIDs: map[string][]string{
			"session-1": {"query-1"},
		},
		workflowObjectives: map[string]string{
			"session-1": "objective",
		},
		activeSessions: map[string]*ActiveSessionInfo{
			"session-1": {
				SessionID:       "session-1",
				Status:          "running",
				IsSyntheticTurn: true,
			},
		},
		stoppedSessions: map[string]bool{},
		bgAgentRegistry: registry,
		sessionBusy: map[string]bool{
			"session-1": true,
		},
		pendingCompletions: map[string][]string{
			"session-1": {"bg-1"},
		},
		lastQueryRequests: map[string]QueryRequest{
			"session-1": {Query: "old"},
		},
		sessionWorkspaceFolders: map[string]string{
			"session-1": "Workflow/test",
		},
		sessionAgents: map[string]*agent.LLMAgentWrapper{},
		completionLoopStarted: map[string]bool{
			"session-1": true,
		},
	}
	api.workshopChatSessions.Store("drifted-workshop-key", workshop)

	req := httptest.NewRequest(http.MethodPost, "/api/session/stop?cancelAgents=true", nil)
	req.Header.Set("X-Session-ID", "session-1")
	rec := httptest.NewRecorder()

	api.handleStopSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("stop status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if !agentCanceled {
		t.Fatalf("agent cancel func was not called")
	}
	if !workflowCanceled {
		t.Fatalf("workflow cancel func was not called")
	}
	if !backgroundCanceled {
		t.Fatalf("background agent cancel func was not called")
	}
	if !workshop.closed {
		t.Fatalf("workshop session was not closed")
	}
	if _, ok := api.workshopChatSessions.Load("drifted-workshop-key"); ok {
		t.Fatalf("workshop session was not removed")
	}
	if !api.stoppedSessions["session-1"] {
		t.Fatalf("session was not marked stopped")
	}
	if got := api.activeSessions["session-1"].Status; got != "stopped" {
		t.Fatalf("active session status = %q, want stopped", got)
	}
	if api.activeSessions["session-1"].IsSyntheticTurn {
		t.Fatalf("synthetic turn flag was not cleared")
	}
	if api.sessionBusy["session-1"] {
		t.Fatalf("session busy flag was not cleared")
	}
	if _, ok := api.agentCancelFuncs["session-1"]; ok {
		t.Fatalf("agent cancel func was not removed")
	}
	if _, ok := api.workflowOrchestratorContexts["query-1"]; ok {
		t.Fatalf("workflow cancel func was not removed")
	}
	if _, ok := api.sessionQueryIDs["session-1"]; ok {
		t.Fatalf("session query IDs were not removed")
	}
	if _, ok := api.activeWorkflowExecutions["query-1"]; ok {
		t.Fatalf("active workflow execution was not removed")
	}
	if got := api.trackedWorkflowExecutions["exec-1"].Status; got != trackedExecutionStatusCanceled {
		t.Fatalf("tracked execution status = %q, want canceled", got)
	}
	if registry.Get("session-1", "bg-1") != nil {
		t.Fatalf("background agent registry was not cleaned up")
	}
	if _, ok := api.pendingCompletions["session-1"]; ok {
		t.Fatalf("pending completions were not cleared")
	}
	if _, ok := api.lastQueryRequests["session-1"]; ok {
		t.Fatalf("last query request was not cleared")
	}
	if _, ok := api.sessionWorkspaceFolders["session-1"]; ok {
		t.Fatalf("session workspace folder was not cleared")
	}
	if _, ok := api.completionLoopStarted["session-1"]; ok {
		t.Fatalf("completion loop marker was not cleared")
	}
	if _, ok := api.workflowObjectives["session-1"]; ok {
		t.Fatalf("workflow objective was not cleared")
	}
	if body := rec.Body.String(); !strings.Contains(body, "Session stopped") {
		t.Fatalf("unexpected stop response body: %q", body)
	}
}

func TestHandleCancelCurrentTurnPreservesSessionAndBackgroundWork(t *testing.T) {
	const sessionID = "schedule-cron--test_1"
	turnCanceled := false
	backgroundCanceled := false
	registry := NewBackgroundAgentRegistry()
	registry.Register(sessionID, &BackgroundAgent{
		ID:        "bg-1",
		Name:      "background-check",
		SessionID: sessionID,
		Status:    BGAgentRunning,
		cancel:    func() { backgroundCanceled = true },
	})

	api := &StreamingAPI{
		agentCancelFuncs: map[string]context.CancelFunc{
			sessionID: func() { turnCanceled = true },
		},
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "running"},
		},
		stoppedSessions: map[string]bool{},
		bgAgentRegistry: registry,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/session/cancel-turn", nil)
	req.Header.Set("X-Session-ID", sessionID)
	rec := httptest.NewRecorder()

	api.handleCancelCurrentTurn(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("cancel-turn status = %d body=%s, want 204", rec.Code, rec.Body.String())
	}
	if !turnCanceled {
		t.Fatalf("foreground turn cancel func was not called")
	}
	if backgroundCanceled {
		t.Fatalf("background work was canceled by foreground turn stop")
	}
	if registry.Get(sessionID, "bg-1") == nil {
		t.Fatalf("background agent was removed by foreground turn stop")
	}
	if api.stoppedSessions[sessionID] {
		t.Fatalf("session was marked stopped by foreground turn stop")
	}
	if got := api.activeSessions[sessionID].Status; got != "running" {
		t.Fatalf("active session status = %q, want running", got)
	}
	if _, ok := api.agentCancelFuncs[sessionID]; ok {
		t.Fatalf("foreground cancel func was not removed after cancellation")
	}
	if !api.consumeSessionTurnInterrupted(sessionID) {
		t.Fatalf("foreground turn cancellation did not record a sequence interruption")
	}
}

func TestHandleCancelCurrentTurnRecordsInterruptionBetweenTurns(t *testing.T) {
	const sessionID = "schedule-cron--test_2"
	api := &StreamingAPI{
		agentCancelFuncs: map[string]context.CancelFunc{},
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "running"},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/session/cancel-turn", nil)
	req.Header.Set("X-Session-ID", sessionID)
	rec := httptest.NewRecorder()

	api.handleCancelCurrentTurn(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("cancel-turn status = %d body=%s, want 204", rec.Code, rec.Body.String())
	}
	if !api.consumeSessionTurnInterrupted(sessionID) {
		t.Fatalf("cancel between turns did not preserve the user's sequence-stop intent")
	}
}

func TestCleanupCodingAgentInteractiveSessionsCallsEveryProvider(t *testing.T) {
	oldClaude := cleanupClaudeCodeProviderSessions
	oldCodex := cleanupCodexCLIProviderSessions
	oldCursor := cleanupCursorCLIProviderSessions
	oldPi := cleanupPiCLIProviderSessions
	t.Cleanup(func() {
		cleanupClaudeCodeProviderSessions = oldClaude
		cleanupCodexCLIProviderSessions = oldCodex
		cleanupCursorCLIProviderSessions = oldCursor
		cleanupPiCLIProviderSessions = oldPi
	})

	called := map[string]bool{}
	cleanupClaudeCodeProviderSessions = func(context.Context) error {
		called["claude"] = true
		return errors.New("simulated claude cleanup error")
	}
	cleanupCodexCLIProviderSessions = func(context.Context) error {
		called["codex"] = true
		return nil
	}
	cleanupCursorCLIProviderSessions = func(context.Context) error {
		called["cursor"] = true
		return nil
	}
	cleanupPiCLIProviderSessions = func(context.Context) error {
		called["pi"] = true
		return nil
	}

	cleanupCodingAgentInteractiveSessions("test")

	for _, provider := range []string{"claude", "codex", "cursor", "pi"} {
		if !called[provider] {
			t.Fatalf("cleanup for %s was not called", provider)
		}
	}
}
