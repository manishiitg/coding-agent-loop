package server

import (
	"context"
	"errors"
	"testing"
)

type shutdownTestWorkshopSession struct {
	closed bool
}

func (s *shutdownTestWorkshopSession) Close() {
	s.closed = true
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

func TestCleanupCodingAgentInteractiveSessionsCallsEveryProvider(t *testing.T) {
	oldClaude := cleanupClaudeCodeProviderSessions
	oldCodex := cleanupCodexCLIProviderSessions
	oldGemini := cleanupGeminiCLIProviderSessions
	oldCursor := cleanupCursorCLIProviderSessions
	t.Cleanup(func() {
		cleanupClaudeCodeProviderSessions = oldClaude
		cleanupCodexCLIProviderSessions = oldCodex
		cleanupGeminiCLIProviderSessions = oldGemini
		cleanupCursorCLIProviderSessions = oldCursor
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
	cleanupGeminiCLIProviderSessions = func(context.Context) error {
		called["gemini"] = true
		return nil
	}
	cleanupCursorCLIProviderSessions = func(context.Context) error {
		called["cursor"] = true
		return nil
	}

	cleanupCodingAgentInteractiveSessions("test")

	for _, provider := range []string{"claude", "codex", "gemini", "cursor"} {
		if !called[provider] {
			t.Fatalf("cleanup for %s was not called", provider)
		}
	}
}
