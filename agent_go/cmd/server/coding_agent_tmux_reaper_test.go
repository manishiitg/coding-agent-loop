package server

import (
	"context"
	"strings"
	"testing"
	"time"

	storeevents "mcp-agent-builder-go/agent_go/internal/events"
	"mcp-agent-builder-go/agent_go/internal/terminals"

	agentevents "github.com/manishiitg/mcpagent/events"
)

func TestCleanupStaleCodingAgentTmuxSessionsClosesMissingOwner(t *testing.T) {
	store := terminals.NewStore()
	sessionID := "missing-owner-session"
	tmuxSession := "mlp-codex-cli-orphan"
	store.HandleEvent(sessionID, codingAgentTmuxReaperChunkEvent(time.Now(), sessionID, "main:"+sessionID, tmuxSession))
	api := &StreamingAPI{terminalStore: store}

	gotArgs := stubTerminalTmuxCommand(t)

	closed := api.cleanupStaleCodingAgentTmuxSessions(time.Now())

	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	if got := strings.Join(*gotArgs, " "); got != "kill-session -t "+tmuxSession {
		t.Fatalf("tmux args = %q, want kill-session", got)
	}
	snapshot, ok := store.Get(sessionID + ":main:" + sessionID)
	if !ok {
		t.Fatal("expected stale terminal snapshot to remain visible")
	}
	if snapshot.Active || snapshot.State != "stale" || snapshot.TmuxSession != "" {
		t.Fatalf("snapshot active/state/tmux = %v/%q/%q, want false/stale/empty", snapshot.Active, snapshot.State, snapshot.TmuxSession)
	}
}

func TestCleanupStaleCodingAgentTmuxSessionsClosesCompletedIdleBackstop(t *testing.T) {
	now := time.Now()
	store := terminals.NewStore()
	sessionID := "completed-session"
	tmuxSession := "mlp-gemini-cli-idle"
	store.HandleEvent(sessionID, codingAgentTmuxReaperChunkEvent(now.Add(-4*time.Hour), sessionID, "main:"+sessionID, tmuxSession))
	api := &StreamingAPI{
		terminalStore: store,
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "completed"},
		},
	}

	gotArgs := stubTerminalTmuxCommand(t)

	closed := api.cleanupStaleCodingAgentTmuxSessions(now)

	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	if got := strings.Join(*gotArgs, " "); got != "kill-session -t "+tmuxSession {
		t.Fatalf("tmux args = %q, want kill-session", got)
	}
}

func TestCleanupStaleCodingAgentTmuxSessionsKeepsRecentCompletedSession(t *testing.T) {
	now := time.Now()
	store := terminals.NewStore()
	sessionID := "recent-completed-session"
	tmuxSession := "mlp-claude-code-recent"
	store.HandleEvent(sessionID, codingAgentTmuxReaperChunkEvent(now.Add(-30*time.Minute), sessionID, "main:"+sessionID, tmuxSession))
	api := &StreamingAPI{
		terminalStore: store,
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "completed"},
		},
	}

	gotArgs := stubTerminalTmuxCommand(t)

	closed := api.cleanupStaleCodingAgentTmuxSessions(now)

	if closed != 0 {
		t.Fatalf("closed = %d, want 0", closed)
	}
	if len(*gotArgs) != 0 {
		t.Fatalf("tmux command should not run for recent completed session, got %v", *gotArgs)
	}
	snapshot, ok := store.Get(sessionID + ":main:" + sessionID)
	if !ok || snapshot.TmuxSession != tmuxSession {
		t.Fatalf("snapshot tmux = %q ok=%v, want %q", snapshot.TmuxSession, ok, tmuxSession)
	}
}

func TestCleanupStaleCodingAgentTmuxSessionsKeepsRunningActiveSession(t *testing.T) {
	now := time.Now()
	store := terminals.NewStore()
	sessionID := "running-session"
	tmuxSession := "mlp-agy-cli-running"
	store.HandleEvent(sessionID, codingAgentTmuxReaperChunkEvent(now.Add(-4*time.Hour), sessionID, "workflow-step:review-plan", tmuxSession))
	api := &StreamingAPI{
		terminalStore: store,
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "running"},
		},
	}

	gotArgs := stubTerminalTmuxCommand(t)

	closed := api.cleanupStaleCodingAgentTmuxSessions(now)

	if closed != 0 {
		t.Fatalf("closed = %d, want 0", closed)
	}
	if len(*gotArgs) != 0 {
		t.Fatalf("tmux command should not run for active running session, got %v", *gotArgs)
	}
}

func TestCleanupStaleCodingAgentTmuxSessionsClosesStoppedSessionImmediately(t *testing.T) {
	now := time.Now()
	store := terminals.NewStore()
	sessionID := "stopped-session"
	tmuxSession := "mlp-pi-cli-stopped"
	store.HandleEvent(sessionID, codingAgentTmuxReaperChunkEvent(now, sessionID, "main:"+sessionID, tmuxSession))
	api := &StreamingAPI{
		terminalStore: store,
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "stopped"},
		},
	}

	gotArgs := stubTerminalTmuxCommand(t)

	closed := api.cleanupStaleCodingAgentTmuxSessions(now)

	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	if got := strings.Join(*gotArgs, " "); got != "kill-session -t "+tmuxSession {
		t.Fatalf("tmux args = %q, want kill-session", got)
	}
}

func stubTerminalTmuxCommand(t *testing.T) *[]string {
	t.Helper()
	t.Setenv(envCodingAgentTmuxOrphanIdleSeconds, "")
	gotArgs := []string{}
	oldRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}
	t.Cleanup(func() { runTerminalTmuxCommand = oldRun })
	return &gotArgs
}

func codingAgentTmuxReaperChunkEvent(timestamp time.Time, sessionID, executionID, tmuxSession string) storeevents.Event {
	executionKind := "workflow_step"
	scope := "workflow_step"
	if strings.HasPrefix(executionID, "main:") {
		executionKind = "main_agent"
		scope = "main_agent"
	}
	return storeevents.Event{
		Type:          "streaming_chunk",
		Timestamp:     timestamp,
		SessionID:     sessionID,
		ExecutionID:   executionID,
		ExecutionKind: executionKind,
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingChunk,
			Data: &agentevents.StreamingChunkEvent{
				BaseEventData: agentevents.BaseEventData{
					Metadata: map[string]interface{}{
						"kind":           "terminal",
						"tmux_session":   tmuxSession,
						"execution_kind": executionKind,
						"scope":          scope,
					},
				},
				Content:    "pane",
				ChunkIndex: 1,
			},
		},
	}
}
