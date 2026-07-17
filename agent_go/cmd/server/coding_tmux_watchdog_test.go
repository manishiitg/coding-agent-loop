package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
	agentevents "github.com/manishiitg/mcpagent/events"
)

func TestInspectCodingTmuxPaneStateUsesRealPaneState(t *testing.T) {
	original := runTerminalTmuxOutputCommand
	t.Cleanup(func() { runTerminalTmuxOutputCommand = original })

	tests := []struct {
		name   string
		output string
		err    error
		want   codingTmuxPaneState
	}{
		{name: "alive", output: "0\n", want: codingTmuxPaneAlive},
		{name: "dead", output: "1\n", want: codingTmuxPaneDead},
		{name: "missing", err: errors.New("can't find session: mlp-test"), want: codingTmuxPaneMissing},
		{name: "transient failure", err: errors.New("temporary tmux failure"), want: codingTmuxPaneUnknown},
		{name: "unexpected response", output: "", want: codingTmuxPaneUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runTerminalTmuxOutputCommand = func(context.Context, ...string) (string, error) {
				return tc.output, tc.err
			}
			if got := inspectCodingTmuxPaneState("mlp-test"); got != tc.want {
				t.Fatalf("inspectCodingTmuxPaneState = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInspectCodingTmuxPaneStateRejectsEmptySession(t *testing.T) {
	if got := inspectCodingTmuxPaneState("  "); got != codingTmuxPaneMissing {
		t.Fatalf("inspectCodingTmuxPaneState(empty) = %v, want missing", got)
	}
}

func TestCodingTmuxWatchdogReconcilesFromActualPaneState(t *testing.T) {
	originalOutput := runTerminalTmuxOutputCommand
	originalCapture := captureTmuxPanePlainForWatchdog
	originalKill := runTmuxKill
	originalClose := closeCodingCLITmuxForWatchdog
	t.Cleanup(func() {
		runTerminalTmuxOutputCommand = originalOutput
		captureTmuxPanePlainForWatchdog = originalCapture
		runTmuxKill = originalKill
		closeCodingCLITmuxForWatchdog = originalClose
	})
	captureTmuxPanePlainForWatchdog = func(string) string { return "" }
	runTmuxKill = func(context.Context, string) error { return nil }
	closeCodingCLITmuxForWatchdog = func(string, string) bool { return true }

	tests := []struct {
		name       string
		paneState  string
		paneErr    error
		wantActive bool
		wantState  string
		wantTmux   bool
	}{
		{name: "live pane stays active", paneState: "0\n", wantActive: true, wantState: "running", wantTmux: true},
		{name: "dead active pane fails", paneState: "1\n", wantActive: false, wantState: "failed", wantTmux: false},
		{name: "missing active pane fails", paneErr: errors.New("can't find session: mlp-test"), wantActive: false, wantState: "failed", wantTmux: false},
		{name: "unknown failure stays active", paneErr: errors.New("temporary tmux failure"), wantActive: true, wantState: "running", wantTmux: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := terminals.NewStore()
			sessionID := "watchdog-session"
			terminalID := sessionID + ":main:" + sessionID
			store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, "mlp-test", "stable pane", 1))
			api := &StreamingAPI{terminalStore: store}

			runTerminalTmuxOutputCommand = func(context.Context, ...string) (string, error) {
				return tc.paneState, tc.paneErr
			}
			api.reapRateLimitedCodingSessionsOnce(map[string]codingWatchdogObservation{})

			snapshot, ok := store.Get(terminalID)
			if !ok {
				t.Fatalf("terminal snapshot missing")
			}
			if snapshot.Active != tc.wantActive || snapshot.State != tc.wantState {
				t.Fatalf("active/state = %v/%q, want %v/%q", snapshot.Active, snapshot.State, tc.wantActive, tc.wantState)
			}
			if gotTmux := snapshot.TmuxSession != ""; gotTmux != tc.wantTmux {
				t.Fatalf("tmux present = %v, want %v", gotTmux, tc.wantTmux)
			}
		})
	}
}

func TestCodingTmuxWatchdogRequiresDistinctTicksPerTerminal(t *testing.T) {
	oldOutput := runTerminalTmuxOutputCommand
	oldCapture := captureTmuxPanePlainForWatchdog
	t.Cleanup(func() {
		runTerminalTmuxOutputCommand = oldOutput
		captureTmuxPanePlainForWatchdog = oldCapture
	})
	runTerminalTmuxOutputCommand = func(context.Context, ...string) (string, error) {
		return "0", nil
	}
	captureTmuxPanePlainForWatchdog = func(string) string {
		return "You've hit your usage limit"
	}

	store := terminals.NewStore()
	sessionID := "shared-watchdog-session"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:one", "mlp-codex-cli-int-one", "limited", 1))
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:two", "mlp-codex-cli-int-two", "limited", 1))
	api := &StreamingAPI{
		terminalStore: store,
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "running"},
		},
	}
	streak := map[string]codingWatchdogObservation{}

	api.reapRateLimitedCodingSessionsOnce(streak)

	if got := api.activeSessions[sessionID].Status; got != "running" {
		t.Fatalf("session stopped after one tick across two terminals: %q", got)
	}
	if streak["mlp-codex-cli-int-one"].count != 1 || streak["mlp-codex-cli-int-two"].count != 1 {
		t.Fatalf("terminal streaks = %#v, want one observation each", streak)
	}
}

func TestCodingTmuxWatchdogRateLimitedChildDoesNotStopParent(t *testing.T) {
	oldOutput := runTerminalTmuxOutputCommand
	oldCapture := captureTmuxPanePlainForWatchdog
	oldKill := runTmuxKill
	oldClose := closeCodingCLITmuxForWatchdog
	t.Cleanup(func() {
		runTerminalTmuxOutputCommand = oldOutput
		captureTmuxPanePlainForWatchdog = oldCapture
		runTmuxKill = oldKill
		closeCodingCLITmuxForWatchdog = oldClose
	})
	runTerminalTmuxOutputCommand = func(context.Context, ...string) (string, error) { return "0", nil }
	captureTmuxPanePlainForWatchdog = func(string) string { return "You've hit your usage limit" }
	runTmuxKill = func(context.Context, string) error { return nil }
	closeCodingCLITmuxForWatchdog = func(string, string) bool { return true }

	store := terminals.NewStore()
	sessionID := "scheduled-parent"
	terminalID := sessionID + ":workflow-step:collect-insider"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(
		sessionID,
		"workflow-step:collect-insider",
		"mlp-claude-code-child",
		"limited",
		1,
	))
	api := &StreamingAPI{
		terminalStore: store,
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "running"},
		},
		stoppedSessions: make(map[string]bool),
	}
	streak := map[string]codingWatchdogObservation{}

	api.reapRateLimitedCodingSessionsOnce(streak)
	api.reapRateLimitedCodingSessionsOnce(streak)

	if got := api.activeSessions[sessionID].Status; got != "running" {
		t.Fatalf("parent status = %q, want running", got)
	}
	if api.isSessionMarkedStopped(sessionID) {
		t.Fatal("child rate limit marked the parent session stopped")
	}
	snapshot, ok := store.Get(terminalID)
	if !ok {
		t.Fatal("child terminal snapshot missing")
	}
	if snapshot.State != "failed" || snapshot.Active {
		t.Fatalf("child state/active = %q/%v, want failed/false", snapshot.State, snapshot.Active)
	}
}

func TestCodingTmuxWatchdogRateLimitedMainStopsSession(t *testing.T) {
	oldOutput := runTerminalTmuxOutputCommand
	oldCapture := captureTmuxPanePlainForWatchdog
	oldKill := runTmuxKill
	oldClose := closeCodingCLITmuxForWatchdog
	oldCloseAllRuntime := closeAllCodingCLISessionsForRuntimeCancel
	oldCloseRuntime := closeCodingAgentTmuxForRuntimeCancel
	t.Cleanup(func() {
		runTerminalTmuxOutputCommand = oldOutput
		captureTmuxPanePlainForWatchdog = oldCapture
		runTmuxKill = oldKill
		closeCodingCLITmuxForWatchdog = oldClose
		closeAllCodingCLISessionsForRuntimeCancel = oldCloseAllRuntime
		closeCodingAgentTmuxForRuntimeCancel = oldCloseRuntime
	})
	runTerminalTmuxOutputCommand = func(context.Context, ...string) (string, error) { return "0", nil }
	captureTmuxPanePlainForWatchdog = func(string) string { return "You've hit your usage limit" }
	runTmuxKill = func(context.Context, string) error { return nil }
	closeCodingCLITmuxForWatchdog = func(string, string) bool { return true }
	closeAllCodingCLISessionsForRuntimeCancel = func(string, string) {}
	closeCodingAgentTmuxForRuntimeCancel = func(string, string) bool { return true }

	store := terminals.NewStore()
	sessionID := "main-rate-limited"
	event := terminalRouteChunkEvent(sessionID, "main:"+sessionID, "mlp-claude-code-main", "limited", 1)
	event.ExecutionKind = "main_agent"
	chunk := event.Data.Data.(*agentevents.StreamingChunkEvent)
	chunk.Metadata["execution_kind"] = "main_agent"
	store.HandleEvent(sessionID, event)
	api := &StreamingAPI{
		terminalStore: store,
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "running"},
		},
		stoppedSessions: make(map[string]bool),
	}
	streak := map[string]codingWatchdogObservation{}

	api.reapRateLimitedCodingSessionsOnce(streak)
	api.reapRateLimitedCodingSessionsOnce(streak)

	if got := api.activeSessions[sessionID].Status; got != "error" {
		t.Fatalf("main session status = %q, want error", got)
	}
	if !api.isSessionMarkedStopped(sessionID) {
		t.Fatal("main rate limit did not stop the session")
	}
}

func TestCodingTmuxWatchdogRequiresStableEvidence(t *testing.T) {
	oldOutput := runTerminalTmuxOutputCommand
	oldCapture := captureTmuxPanePlainForWatchdog
	t.Cleanup(func() {
		runTerminalTmuxOutputCommand = oldOutput
		captureTmuxPanePlainForWatchdog = oldCapture
	})
	runTerminalTmuxOutputCommand = func(context.Context, ...string) (string, error) { return "0", nil }
	contents := []string{
		"You've hit your usage limit · resets at 10:00\nretrying request",
		"You've hit your usage limit · resets at 10:00\nrequest resumed",
	}
	captureTmuxPanePlainForWatchdog = func(string) string {
		content := contents[0]
		contents = contents[1:]
		return content
	}

	store := terminals.NewStore()
	sessionID := "main-session"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, "mlp-claude-main", "limited", 1))
	api := &StreamingAPI{
		terminalStore: store,
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "running"},
		},
		stoppedSessions: make(map[string]bool),
	}
	streak := map[string]codingWatchdogObservation{}

	api.reapRateLimitedCodingSessionsOnce(streak)
	api.reapRateLimitedCodingSessionsOnce(streak)

	if got := streak["mlp-claude-main"].count; got != 1 {
		t.Fatalf("changing evidence count = %d, want reset to 1", got)
	}
	if got := api.activeSessions[sessionID].Status; got != "running" {
		t.Fatalf("session status = %q, want running", got)
	}
}

func TestCodingWatchdogRateLimitEvidenceIgnoresOldScrollback(t *testing.T) {
	lines := []string{"You've hit your usage limit"}
	for i := 0; i < codingWatchdogRateLimitTailLines; i++ {
		lines = append(lines, "current progress")
	}
	if got := codingWatchdogRateLimitEvidence(strings.Join(lines, "\n")); got != "" {
		t.Fatalf("old scrollback evidence = %q, want empty", got)
	}
}
