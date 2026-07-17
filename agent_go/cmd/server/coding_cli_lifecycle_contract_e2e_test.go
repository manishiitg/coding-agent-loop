package server

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	agentevents "github.com/manishiitg/mcpagent/events"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
)

// TestCodingCLILifecycleMatrixHermeticTmux is the credential-free P0 terminal
// ownership contract. It uses real tmux processes while keeping provider CLIs
// hermetic, so every push verifies identical lifecycle behavior for all active
// coding providers without consuming provider quota.
func TestCodingCLILifecycleMatrixHermeticTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for the coding CLI lifecycle contract")
	}

	providers := []struct {
		name   string
		prefix string
	}{
		{name: "claude-code", prefix: "mlp-claude-code-int"},
		{name: "codex-cli", prefix: "mlp-codex-cli-int"},
		{name: "cursor-cli", prefix: "mlp-cursor-cli-int"},
		{name: "pi-cli", prefix: "mlp-pi-cli-int"},
		{name: "agy-cli", prefix: "mlp-agy-cli-int"},
	}

	for _, provider := range providers {
		provider := provider
		t.Run(provider.name+"/child_exit_fails_exact_child_only", func(t *testing.T) {
			tmuxSession := startLifecycleContractTmux(t, provider.prefix)
			sessionID := "lifecycle-child-" + strings.ReplaceAll(provider.name, "-", "_")
			executionID := "child-" + provider.name
			unrelatedID := "unrelated-" + provider.name
			startedAt := time.Now().Add(-time.Second)

			store := terminals.NewStore()
			store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, executionID, tmuxSession, "provider working", 1))
			registry := NewBackgroundAgentRegistry()
			workerCtx, workerCancel := context.WithCancel(context.Background())
			registry.Register(sessionID, &BackgroundAgent{
				ID:        executionID,
				SessionID: sessionID,
				Status:    BGAgentRunning,
				CreatedAt: startedAt,
				cancel:    workerCancel,
			})
			api := &StreamingAPI{
				terminalStore: store,
				activeSessions: map[string]*ActiveSessionInfo{
					sessionID: {SessionID: sessionID, Status: "running", CreatedAt: startedAt},
				},
				stoppedSessions: make(map[string]bool),
				bgAgentRegistry: registry,
				trackedWorkflowExecutions: map[string]*TrackedWorkflowExecution{
					executionID: {ExecutionID: executionID, SessionID: sessionID, Status: trackedExecutionStatusRunning, StartedAt: startedAt},
					unrelatedID: {ExecutionID: unrelatedID, SessionID: sessionID, Status: trackedExecutionStatusRunning, StartedAt: startedAt},
				},
			}

			killLifecycleContractTmux(t, tmuxSession)
			api.reapRateLimitedCodingSessionsOnce(map[string]codingWatchdogObservation{})

			snapshot := mustLifecycleTerminal(t, store, sessionID+":"+executionID)
			if snapshot.State != "failed" || snapshot.ProcessState != "closed" || snapshot.TmuxSession != "" || snapshot.CloseReason == "" {
				t.Fatalf("terminal after child exit = state=%q process=%q tmux=%q reason=%q", snapshot.State, snapshot.ProcessState, snapshot.TmuxSession, snapshot.CloseReason)
			}
			if got := api.trackedWorkflowExecutions[executionID]; got.Status != trackedExecutionStatusFailed || got.LastError == "" {
				t.Fatalf("owning execution = status=%q error=%q, want failed with reason", got.Status, got.LastError)
			}
			if got := api.trackedWorkflowExecutions[unrelatedID]; got.Status != trackedExecutionStatusRunning {
				t.Fatalf("unrelated execution status = %q, want running", got.Status)
			}
			if got := registry.Get(sessionID, executionID); got.GetStatus() != BGAgentFailed {
				t.Fatalf("background owner status = %q, want failed", got.GetStatus())
			}
			select {
			case <-workerCtx.Done():
			default:
				t.Fatal("background owner context was not canceled")
			}
			if got := api.activeSessions[sessionID].Status; got != "running" {
				t.Fatalf("parent session status = %q, want running", got)
			}
			if api.isSessionMarkedStopped(sessionID) {
				t.Fatal("child process exit stopped the parent session")
			}
		})

		t.Run(provider.name+"/main_exit_fails_session", func(t *testing.T) {
			oldCloseAll := closeAllCodingCLISessionsForRuntimeCancel
			oldCloseTmux := closeCodingAgentTmuxForRuntimeCancel
			closeAllCodingCLISessionsForRuntimeCancel = func(string, string) {}
			closeCodingAgentTmuxForRuntimeCancel = func(string, string) bool { return true }
			t.Cleanup(func() {
				closeAllCodingCLISessionsForRuntimeCancel = oldCloseAll
				closeCodingAgentTmuxForRuntimeCancel = oldCloseTmux
			})

			tmuxSession := startLifecycleContractTmux(t, provider.prefix)
			sessionID := "lifecycle-main-" + strings.ReplaceAll(provider.name, "-", "_")
			startedAt := time.Now().Add(-time.Second)
			store := terminals.NewStore()
			event := terminalRouteChunkEvent(sessionID, "main:"+sessionID, tmuxSession, "provider working", 1)
			event.ExecutionKind = "main_agent"
			event.Data.Data.(*agentevents.StreamingChunkEvent).Metadata["execution_kind"] = "main_agent"
			store.HandleEvent(sessionID, event)
			api := &StreamingAPI{
				terminalStore: store,
				activeSessions: map[string]*ActiveSessionInfo{
					sessionID: {SessionID: sessionID, Status: "running", CreatedAt: startedAt},
				},
				stoppedSessions: make(map[string]bool),
			}

			killLifecycleContractTmux(t, tmuxSession)
			api.reapRateLimitedCodingSessionsOnce(map[string]codingWatchdogObservation{})

			if got := api.activeSessions[sessionID].Status; got != "error" {
				t.Fatalf("main session status = %q, want error", got)
			}
			if !api.isSessionMarkedStopped(sessionID) {
				t.Fatal("main process exit did not stop session runtime")
			}
			snapshot := mustLifecycleTerminal(t, store, sessionID+":main:"+sessionID)
			if snapshot.State != "failed" || snapshot.ProcessState != "closed" || snapshot.CloseReason == "" {
				t.Fatalf("main terminal after exit = state=%q process=%q reason=%q", snapshot.State, snapshot.ProcessState, snapshot.CloseReason)
			}
		})
	}
}

func TestTerminalOwnerReconciliationRejectsStaleGeneration(t *testing.T) {
	store := terminals.NewStore()
	sessionID := "reused-main-session"
	tmuxSession := "mlp-codex-cli-int-historical"
	event := terminalRouteChunkEvent(sessionID, "main:"+sessionID, tmuxSession, "historical pane", 1)
	event.ExecutionKind = "main_agent"
	event.Data.Data.(*agentevents.StreamingChunkEvent).Metadata["execution_kind"] = "main_agent"
	store.HandleEvent(sessionID, event)
	snapshot := mustLifecycleTerminal(t, store, sessionID+":main:"+sessionID)

	api := &StreamingAPI{
		terminalStore: store,
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {
				SessionID: sessionID,
				Status:    "running",
				CreatedAt: snapshot.CreatedAt.Add(time.Second),
			},
		},
		stoppedSessions: make(map[string]bool),
	}
	if api.reconcileUnexpectedTerminalExit(snapshot, "historical pane exited") {
		t.Fatal("stale terminal reconciled into a newer main session")
	}
	if got := api.activeSessions[sessionID].Status; got != "running" {
		t.Fatalf("newer session status = %q, want running", got)
	}
	if api.isSessionMarkedStopped(sessionID) {
		t.Fatal("stale terminal stopped the newer main session")
	}
}

func TestTerminalSnapshotExpiryMatrix(t *testing.T) {
	for _, providerPrefix := range []string{"mlp-claude-code-int", "mlp-codex-cli-int", "mlp-cursor-cli-int", "mlp-pi-cli-int", "mlp-agy-cli-int"} {
		t.Run(providerPrefix, func(t *testing.T) {
			store := terminals.NewStore()
			sessionID := "expiry-" + providerPrefix
			executionID := "workflow-step:expiry"
			tmuxSession := providerPrefix + "-expiry"
			completedAt := time.Now().Add(-2 * time.Second)
			store.HandleEvent(sessionID, codingAgentTmuxReaperChunkEvent(completedAt, sessionID, executionID, tmuxSession))
			// terminalRouteEndEvent uses time.Now by default; replace it with an
			// authoritative historical completion timestamp so expiry is deterministic.
			end := terminalRouteEndEvent(sessionID, executionID, tmuxSession, 1)
			end.Timestamp = completedAt
			store.HandleEvent(sessionID, end)
			if got := store.List(sessionID); len(got) != 0 {
				t.Fatalf("expired snapshot count = %d, want 0", len(got))
			}
		})
	}
}

func startLifecycleContractTmux(t *testing.T, prefix string) string {
	t.Helper()
	name := fmt.Sprintf("%s-e2e-%d", prefix, time.Now().UnixNano())
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name, "sleep 60")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start tmux %s: %v: %s", name, err, strings.TrimSpace(string(output)))
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })
	if got := inspectCodingTmuxPaneState(name); got != codingTmuxPaneAlive {
		t.Fatalf("tmux %s state = %v, want alive", name, got)
	}
	return name
}

func killLifecycleContractTmux(t *testing.T, name string) {
	t.Helper()
	if output, err := exec.Command("tmux", "kill-session", "-t", name).CombinedOutput(); err != nil {
		t.Fatalf("kill tmux %s: %v: %s", name, err, strings.TrimSpace(string(output)))
	}
	if got := inspectCodingTmuxPaneState(name); got != codingTmuxPaneMissing {
		t.Fatalf("tmux %s state after kill = %v, want missing", name, got)
	}
}

func mustLifecycleTerminal(t *testing.T, store *terminals.Store, terminalID string) terminals.Snapshot {
	t.Helper()
	snapshot, ok := store.Get(terminalID)
	if !ok {
		t.Fatalf("terminal %s not found", terminalID)
	}
	return snapshot
}
