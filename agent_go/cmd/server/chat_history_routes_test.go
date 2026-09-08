package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func TestDecorateSharedBuilderHistoryShowsAuthorAndProtectsResume(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"owner-1","username":"manish","admin":true,"can_create":true,"products":[]},{"id":"member-1","username":"laxmi","can_create":false,"can_edit":true,"products":["agentworks"]}]}`)

	sessions := []ChatHistorySession{
		{SessionID: "mine", UserID: "member-1"},
		{SessionID: "theirs", UserID: "owner-1"},
		{SessionID: "legacy", UserID: "default"},
	}
	decorateChatHistorySessions(sessions, "member-1", WorkflowAccessWrite, true)

	if sessions[0].Username != "laxmi" || !sessions[0].CanResume || !sessions[0].CanDelete {
		t.Fatalf("own session decoration = %#v", sessions[0])
	}
	if sessions[1].Username != "manish" || sessions[1].CanResume || sessions[1].CanDelete {
		t.Fatalf("other collaborator session decoration = %#v", sessions[1])
	}
	if sessions[2].Username != "System / legacy" || sessions[2].CanResume || sessions[2].CanDelete {
		t.Fatalf("legacy session decoration = %#v", sessions[2])
	}

	decorateChatHistorySessions(sessions, "member-1", WorkflowAccessOwner, true)
	if sessions[1].CanResume || !sessions[1].CanDelete || !sessions[2].CanResume {
		t.Fatalf("workflow owner policy not applied: other=%#v legacy=%#v", sessions[1], sessions[2])
	}
}

func TestVisibleSharedBuilderHistoryIsOwnerWideButMemberPrivate(t *testing.T) {
	sessions := []ChatHistorySession{
		{SessionID: "mine", UserID: "member-1"},
		{SessionID: "theirs", UserID: "member-2"},
		{SessionID: "legacy", UserID: "default"},
	}

	memberView := visibleChatHistorySessions(sessions, "member-1", WorkflowAccessWrite)
	if len(memberView) != 1 || memberView[0].SessionID != "mine" {
		t.Fatalf("member view = %#v, want only own chat", memberView)
	}
	readerView := visibleChatHistorySessions(sessions, "member-2", WorkflowAccessRead)
	if len(readerView) != 1 || readerView[0].SessionID != "theirs" {
		t.Fatalf("reader view = %#v, want only own chat", readerView)
	}
	ownerView := visibleChatHistorySessions(sessions, "owner-1", WorkflowAccessOwner)
	if len(ownerView) != len(sessions) {
		t.Fatalf("owner view count = %d, want %d", len(ownerView), len(sessions))
	}
}

func TestParseWorkflowBuilderHistoryUsesPersistedAuthor(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"author-1","username":"yoav","can_create":true,"products":["agentworks"]}]}`)

	session, ok := parseLocalChatHistorySession(
		"viewer-2",
		"Workflow/demo",
		"Workflow/demo",
		"session-1",
		`{"session_id":"session-1","user_id":"author-1","username":"old-name","conversation_history":[],"updated_at":"2026-09-08T10:00:00Z"}`,
		time.Time{},
	)
	if !ok {
		t.Fatal("expected workflow Builder transcript to parse")
	}
	if session.UserID != "author-1" || session.Username != "yoav" {
		t.Fatalf("author = %q/%q, want author-1/yoav", session.UserID, session.Username)
	}

	legacy, ok := parseLocalChatHistorySession(
		"viewer-2",
		"Workflow/demo",
		"Workflow/demo",
		"legacy-1",
		`{"session_id":"legacy-1","conversation_history":[],"updated_at":"2026-09-08T10:00:00Z"}`,
		time.Time{},
	)
	if !ok || legacy.UserID != "default" || legacy.Username != "System / legacy" {
		t.Fatalf("legacy author = %#v, want stable legacy attribution", legacy)
	}
}

func TestWorkflowPathFromBuilderConversation(t *testing.T) {
	path := "Workflow/demo/builder/conversation/2026-09-08/session-abc-conversation.json"
	if got := workflowPathFromBuilderConversation(path); got != "Workflow/demo" {
		t.Fatalf("workflow path = %q", got)
	}
	if got := workflowPathFromBuilderConversation("_users/alice/chat_history/session/conversation.json"); got != "" {
		t.Fatalf("personal path must not resolve as shared workflow: %q", got)
	}
}

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
