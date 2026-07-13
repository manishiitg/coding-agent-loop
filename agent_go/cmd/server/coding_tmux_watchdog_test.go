package server

import (
	"context"
	"errors"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
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
	t.Cleanup(func() {
		runTerminalTmuxOutputCommand = originalOutput
		captureTmuxPanePlainForWatchdog = originalCapture
		runTmuxKill = originalKill
	})
	captureTmuxPanePlainForWatchdog = func(string) string { return "" }
	runTmuxKill = func(context.Context, string) error { return nil }

	tests := []struct {
		name       string
		paneState  string
		paneErr    error
		wantActive bool
		wantState  string
		wantTmux   bool
	}{
		{name: "live pane stays active", paneState: "0\n", wantActive: true, wantState: "running", wantTmux: true},
		{name: "dead pane completes", paneState: "1\n", wantActive: false, wantState: "completed", wantTmux: false},
		{name: "missing session becomes stale", paneErr: errors.New("can't find session: mlp-test"), wantActive: false, wantState: "stale", wantTmux: false},
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
			api.reapRateLimitedCodingSessionsOnce(map[string]int{})

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
	streak := map[string]int{}

	api.reapRateLimitedCodingSessionsOnce(streak)

	if got := api.activeSessions[sessionID].Status; got != "running" {
		t.Fatalf("session stopped after one tick across two terminals: %q", got)
	}
	if streak["mlp-codex-cli-int-one"] != 1 || streak["mlp-codex-cli-int-two"] != 1 {
		t.Fatalf("terminal streaks = %#v, want one observation each", streak)
	}
}
