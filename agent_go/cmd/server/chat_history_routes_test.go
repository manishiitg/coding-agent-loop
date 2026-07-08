package server

import (
	"context"
	"strings"
	"testing"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/internal/terminals"
)

func TestRestoredRuntimeUsesLaunchableTransportFromHandle(t *testing.T) {
	runtime := &ChatHistoryAgentRuntime{
		Kind:     "coding_agent",
		Provider: "future-cli",
		AgentSessionHandle: &mcpagent.AgentSessionHandle{
			Provider: llmtypes.CodingProviderSessionHandle{
				Provider:        "future-cli",
				Transport:       llmtypes.CodingProviderTransportTmux,
				NativeSessionID: "native-thread-1",
				TmuxSession:     "mlp-future-1",
			},
		},
	}

	if !restoredRuntimeUsesLaunchableTerminalTransport(runtime) {
		t.Fatalf("expected launchable terminal transport")
	}
	tmuxSession, ok, reason := restoredRuntimeTmuxSession(runtime)
	if !ok || tmuxSession != "mlp-future-1" || reason != "" {
		t.Fatalf("restoredRuntimeTmuxSession = %q, %v, %q", tmuxSession, ok, reason)
	}
}

func TestRestoredRuntimeUsesLaunchableTransportFromRuntime(t *testing.T) {
	runtime := &ChatHistoryAgentRuntime{
		Kind:      "coding_agent",
		Provider:  "future-cli",
		Transport: "tmux",
		AgentSessionHandle: &mcpagent.AgentSessionHandle{
			Provider: llmtypes.CodingProviderSessionHandle{
				Provider:    "future-cli",
				TmuxSession: "mlp-future-runtime-1",
			},
		},
	}

	if !restoredRuntimeUsesLaunchableTerminalTransport(runtime) {
		t.Fatalf("expected launchable terminal transport")
	}
	tmuxSession, ok, reason := restoredRuntimeTmuxSession(runtime)
	if !ok || tmuxSession != "mlp-future-runtime-1" || reason != "" {
		t.Fatalf("restoredRuntimeTmuxSession = %q, %v, %q", tmuxSession, ok, reason)
	}
}

func TestRestoredRuntimeTmuxSessionRejectsNonTmuxTransport(t *testing.T) {
	runtime := &ChatHistoryAgentRuntime{
		Kind:     "coding_agent",
		Provider: "future-cli",
		AgentSessionHandle: &mcpagent.AgentSessionHandle{
			Provider: llmtypes.CodingProviderSessionHandle{
				Provider:  "future-cli",
				Transport: "api",
			},
		},
	}

	if restoredRuntimeUsesLaunchableTerminalTransport(runtime) {
		t.Fatalf("non-tmux transport should not start tmux")
	}
	if _, ok, reason := restoredRuntimeTmuxSession(runtime); ok || reason != "not_tmux_transport" {
		t.Fatalf("restoredRuntimeTmuxSession ok=%v reason=%q, want not_tmux_transport", ok, reason)
	}
}

func TestRestorePersistedTerminalSnapshotCreatesStaticTerminal(t *testing.T) {
	api := &StreamingAPI{terminalStore: terminals.NewStore()}
	runtime := &ChatHistoryAgentRuntime{
		Kind:          "coding_agent",
		Provider:      "codex-cli",
		WorkspacePath: "Workflow/demo",
	}
	snapshots := []terminals.Snapshot{{
		TerminalID:    "old-session:main:old-session",
		SessionID:     "old-session",
		OwnerID:       "main:old-session",
		ExecutionKind: "main_agent",
		StepTransport: "tmux",
		TmuxSession:   "dead-tmux-session",
		ContentSource: "tmux_pipe",
		Content:       "\x1b[32mwhat we did last\x1b[0m",
	}}

	terminal, started, reason := api.restorePersistedTerminalSnapshot(context.Background(), "new-session", runtime, snapshots)
	if !started || terminal == nil || reason != "" {
		t.Fatalf("restorePersistedTerminalSnapshot started=%v terminal=%#v reason=%q", started, terminal, reason)
	}
	if terminal.SessionID != "new-session" || terminal.TmuxSession != "" {
		t.Fatalf("restored terminal session/tmux = %q/%q", terminal.SessionID, terminal.TmuxSession)
	}
	if terminal.State != "stale" || terminal.Active {
		t.Fatalf("restored terminal lifecycle = active:%v state:%q", terminal.Active, terminal.State)
	}
	if terminal.ContentSource != "tmux_pipe" || !strings.Contains(terminal.Content, "\x1b[32m") {
		t.Fatalf("restored terminal lost ANSI/source: source=%q content=%q", terminal.ContentSource, terminal.Content)
	}
	stored, ok := api.terminalStore.Get("new-session:main:new-session")
	if !ok {
		t.Fatalf("expected static terminal in store")
	}
	if stored.TmuxSession != "" || stored.ContentSource != "tmux_pipe" {
		t.Fatalf("stored terminal tmux/source = %q/%q", stored.TmuxSession, stored.ContentSource)
	}
}
