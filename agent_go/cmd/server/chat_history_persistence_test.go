package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internalevents "mcp-agent-builder-go/agent_go/internal/events"
	"mcp-agent-builder-go/agent_go/internal/terminals"
	"mcp-agent-builder-go/agent_go/pkg/workspace"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	agentevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// TestCaptureChatHistoryTerminalSnapshotsPersistsNonTmuxMultiAgentPane covers the
// multi-agent / Chief-of-Staff case: the coding agent streams its terminal over
// a non-tmux transport, so the store snapshot has no tmux_session. Persistence
// must still capture it so the pane can be restored after a server restart.
func TestCaptureChatHistoryTerminalSnapshotsPersistsNonTmuxMultiAgentPane(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "chat-non-tmux-multiagent"

	now := time.Now()
	store.HandleEvent(sessionID, internalevents.Event{
		Type:          "streaming_chunk",
		Timestamp:     now,
		SessionID:     sessionID,
		ExecutionKind: "main_agent",
		Data: &agentevents.AgentEvent{
			Type:      agentevents.StreamingChunk,
			Timestamp: now,
			SessionID: sessionID,
			Data: &agentevents.StreamingChunkEvent{
				BaseEventData: agentevents.BaseEventData{
					Timestamp: now,
					SessionID: sessionID,
					Metadata: map[string]interface{}{
						"kind":           "terminal",
						"provider":       "claude-code",
						"execution_kind": "main_agent",
						"scope":          "main_agent",
						// Intentionally no tmux_session / step_transport — the
						// non-tmux transport the bug was reported against.
					},
				},
				Content:    "\x1b[32mchief of staff output\x1b[0m\n",
				ChunkIndex: 0,
			},
		},
	})

	// Sanity: the live store snapshot is non-tmux (the condition the old
	// persist filter dropped).
	stored := store.List(sessionID)
	if len(stored) != 1 {
		t.Fatalf("store snapshot count = %d, want 1", len(stored))
	}
	if chatHistoryTerminalSnapshotIsTmux(stored[0]) {
		t.Fatalf("expected non-tmux store snapshot, got transport=%q source=%q tmux=%q", stored[0].StepTransport, stored[0].ContentSource, stored[0].TmuxSession)
	}

	snapshots := api.captureChatHistoryTerminalSnapshots(sessionID, nil)
	if len(snapshots) != 1 {
		t.Fatalf("persisted snapshot count = %d, want 1 (non-tmux multi-agent pane must persist)", len(snapshots))
	}
	if !strings.Contains(snapshots[0].Content, "chief of staff output") {
		t.Fatalf("persisted snapshot content = %q, want chief of staff output", snapshots[0].Content)
	}
}

func TestCleanChatHistoryForPersistenceRemovesRestoredConversationContext(t *testing.T) {
	originalText := "check what we did here\n\nPrevious workflow-builder conversation file: Workflow/instagram/builder/conversation/2026-05-14/session-old-conversation.json\nThis hidden context should not persist."
	history := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: originalText},
			},
		},
		{
			Role: llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "summary"},
			},
		},
	}

	cleaned := cleanChatHistoryForPersistence(history)

	got := cleaned[0].Parts[0].(llmtypes.TextContent).Text
	if got != "check what we did here" {
		t.Fatalf("expected restored context removed, got %q", got)
	}
	if history[0].Parts[0].(llmtypes.TextContent).Text != originalText {
		t.Fatalf("expected original history to remain unchanged")
	}
}

func TestAppendRestoredConversationContextAddsFallbackAndCleans(t *testing.T) {
	withContext := appendRestoredConversationContext("continue this", "_users/default/chat_history/2026-05-18/session/conversation.json")

	if !strings.Contains(withContext, "\n\nPrevious conversation file: _users/default/chat_history/2026-05-18/session/conversation.json") {
		t.Fatalf("expected fallback conversation file context, got %q", withContext)
	}
	if got := cleanChatHistoryQuery(withContext); got != "continue this" {
		t.Fatalf("expected restored context to clean back to user query, got %q", got)
	}
	if got := appendRestoredConversationContext(withContext, "_users/default/chat_history/other/conversation.json"); got != withContext {
		t.Fatalf("expected existing restored context to remain unchanged")
	}
}

func TestCaptureChatHistoryTerminalSnapshotsRedactsAndPreservesAnsi(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "chat-terminal-snapshot"
	_, ok := store.UpsertStaticSnapshot(sessionID, terminals.Snapshot{
		OwnerID:       "main:" + sessionID,
		ExecutionKind: "main_agent",
		StepTransport: "tmux",
		ContentSource: "tmux_pipe",
		TmuxSession:   "old-live-pane",
		Content:       "\x1b[31mred output\x1b[0m\nMCP_API_TOKEN=super-secret\nSECRET_FOO=hidden-secret\n",
		UpdatedAt:     time.Now(),
	})
	if !ok {
		t.Fatal("expected terminal snapshot to be stored")
	}

	snapshots := api.captureChatHistoryTerminalSnapshots(sessionID, nil)
	if len(snapshots) != 1 {
		t.Fatalf("terminal snapshot count = %d, want 1", len(snapshots))
	}
	got := snapshots[0]
	if got.ContentSource != "tmux_pipe" {
		t.Fatalf("content source = %q, want tmux_pipe", got.ContentSource)
	}
	if !strings.Contains(got.Content, "\x1b[31m") || !strings.Contains(got.Content, "red output") {
		t.Fatalf("expected ANSI terminal content to be preserved, got %q", got.Content)
	}
	for _, leaked := range []string{"super-secret", "hidden-secret"} {
		if strings.Contains(got.Content, leaked) {
			t.Fatalf("terminal snapshot leaked secret %q: %q", leaked, got.Content)
		}
	}
	for _, redacted := range []string{"MCP_API_TOKEN=[redacted]", "SECRET_FOO=[redacted]"} {
		if !strings.Contains(got.Content, redacted) {
			t.Fatalf("terminal snapshot missing redacted marker %q: %q", redacted, got.Content)
		}
	}
	if got.Active || got.State != "stale" || len(got.Rows) != 0 {
		t.Fatalf("persisted snapshot lifecycle = active:%v state:%q rows:%d", got.Active, got.State, len(got.Rows))
	}
}

func TestCaptureChatHistoryTerminalSnapshotsPrefersRuntimeTmuxCapture(t *testing.T) {
	oldCapture := captureChatHistoryTerminalPaneLines
	captureChatHistoryTerminalPaneLines = func(_ context.Context, tmuxSession string, lines int) (string, error) {
		if tmuxSession != "live-runtime-pane" {
			t.Fatalf("tmux session = %q, want live-runtime-pane", tmuxSession)
		}
		if lines != terminalDefaultDetailHistoryLines {
			t.Fatalf("capture lines = %d, want %d", lines, terminalDefaultDetailHistoryLines)
		}
		return "\x1b[32mruntime output\x1b[0m\nMCP_API_TOKEN=super-secret\n", nil
	}
	defer func() { captureChatHistoryTerminalPaneLines = oldCapture }()

	sessionID := "chat-runtime-terminal"
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	if _, ok := store.UpsertStaticSnapshot(sessionID, terminals.Snapshot{
		OwnerID:       "main:" + sessionID,
		ExecutionKind: "main_agent",
		StepTransport: "tmux",
		ContentSource: "tmux_pipe",
		TmuxSession:   "live-runtime-pane",
		Content:       "[terminal output truncated; showing latest output]\n0;bad-title\a\x1b[13;2Hpipe output\n",
		UpdatedAt:     time.Now(),
	}); !ok {
		t.Fatal("expected pipe terminal snapshot to be stored")
	}
	runtime := &ChatHistoryAgentRuntime{
		Kind:          "coding_agent",
		Provider:      "codex-cli",
		Transport:     llmtypes.CodingProviderTransportTmux,
		WorkspacePath: "Workflow/substack",
		AgentSessionHandle: &mcpagent.AgentSessionHandle{
			SessionID: sessionID,
			Provider: llmtypes.CodingProviderSessionHandle{
				Provider:    "codex-cli",
				Transport:   llmtypes.CodingProviderTransportTmux,
				TmuxSession: "live-runtime-pane",
			},
		},
	}

	snapshots := api.captureChatHistoryTerminalSnapshots(sessionID, runtime)
	if len(snapshots) != 1 {
		t.Fatalf("terminal snapshot count = %d, want 1", len(snapshots))
	}
	got := snapshots[0]
	if got.StepTransport != "tmux" || got.TmuxSession != "live-runtime-pane" || got.ContentSource != "tmux_capture" {
		t.Fatalf("runtime fallback metadata = transport:%q tmux:%q source:%q", got.StepTransport, got.TmuxSession, got.ContentSource)
	}
	if got.WorkflowPath != "Workflow/substack" || got.OwnerID != "main:"+sessionID || got.ExecutionKind != "main_agent" {
		t.Fatalf("runtime fallback identity = %#v", got)
	}
	if !strings.Contains(got.Content, "\x1b[32m") || !strings.Contains(got.Content, "runtime output") {
		t.Fatalf("expected ANSI runtime terminal content, got %q", got.Content)
	}
	if strings.Contains(got.Content, "pipe output") || strings.Contains(got.Content, "0;bad-title") {
		t.Fatalf("expected runtime capture to replace raw pipe snapshot, got %q", got.Content)
	}
	if strings.Contains(got.Content, "super-secret") || !strings.Contains(got.Content, "MCP_API_TOKEN=[redacted]") {
		t.Fatalf("runtime fallback snapshot was not redacted: %q", got.Content)
	}
}

func TestReadChatHistoryTerminalSnapshotsFromPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	convDir := filepath.Join(root, "_users", "default", "chat_history", "2026-05-18")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("mkdir conversation dir: %v", err)
	}
	convPath := filepath.Join(convDir, "session-chat-1-conversation.json")
	data := `{
  "session_id": "chat-1",
  "conversation_history": [],
  "terminal_snapshots": [
    {
      "terminal_id": "chat-1:main:chat-1",
      "session_id": "chat-1",
      "owner_id": "main:chat-1",
      "execution_kind": "main_agent",
      "step_transport": "tmux",
      "content_source": "tmux_pipe",
      "content": "\u001b[34mblue output\u001b[0m\nMCP_API_TOKEN=super-secret",
      "state": "running"
    }
  ]
}`
	if err := os.WriteFile(convPath, []byte(data), 0o600); err != nil {
		t.Fatalf("write conversation: %v", err)
	}

	snapshots, ok, err := ReadChatHistoryTerminalSnapshotsFromPath("default", "_users/default/chat_history/2026-05-18/session-chat-1-conversation.json")
	if err != nil {
		t.Fatalf("read terminal snapshots error = %v", err)
	}
	if !ok || len(snapshots) != 1 {
		t.Fatalf("snapshots ok=%v len=%d, want ok with 1", ok, len(snapshots))
	}
	got := snapshots[0]
	if got.ContentSource != "tmux_pipe" || !strings.Contains(got.Content, "\x1b[34m") {
		t.Fatalf("snapshot source/content = %q/%q", got.ContentSource, got.Content)
	}
	if strings.Contains(got.Content, "super-secret") || !strings.Contains(got.Content, "MCP_API_TOKEN=[redacted]") {
		t.Fatalf("snapshot was not redacted: %q", got.Content)
	}
	if got.Active || got.State != "stale" {
		t.Fatalf("snapshot lifecycle = active:%v state:%q", got.Active, got.State)
	}
}

func TestReadChatHistoryTerminalSnapshotsFallsBackToUIEvents(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	convDir := filepath.Join(root, "_users", "default", "chat_history", "2026-05-18")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("mkdir conversation dir: %v", err)
	}
	convPath := filepath.Join(convDir, "session-legacy-codex-conversation.json")
	data := `{
  "session_id": "legacy-codex",
  "runtime": {
    "kind": "coding_agent",
    "provider": "codex-cli",
    "transport": "tmux",
    "workspace_path": "Workflow/substack"
  },
  "conversation_history": [],
  "ui_events": [
    {
      "type": "streaming_chunk",
      "timestamp": "2026-06-13T12:00:00Z",
      "session_id": "legacy-codex",
      "execution_id": "main:legacy-codex",
      "execution_kind": "main_agent",
      "data": {
        "type": "streaming_chunk",
        "timestamp": "2026-06-13T12:00:00Z",
        "session_id": "legacy-codex",
        "data": {
          "timestamp": "2026-06-13T12:00:00Z",
          "session_id": "legacy-codex",
          "content": "\u001b[35mlegacy pane\u001b[0m\nSECRET_FOO=hidden-secret\n",
          "chunk_index": 7,
          "metadata": {
            "kind": "terminal",
            "provider": "codex-cli",
            "tmux_session": "legacy-pane"
          }
        }
      }
    }
  ]
}`
	if err := os.WriteFile(convPath, []byte(data), 0o600); err != nil {
		t.Fatalf("write conversation: %v", err)
	}

	snapshots, ok, err := ReadChatHistoryTerminalSnapshotsFromPath("default", "_users/default/chat_history/2026-05-18/session-legacy-codex-conversation.json")
	if err != nil {
		t.Fatalf("read terminal snapshots error = %v", err)
	}
	if !ok || len(snapshots) != 1 {
		t.Fatalf("snapshots ok=%v len=%d, want ok with 1", ok, len(snapshots))
	}
	got := snapshots[0]
	if got.TmuxSession != "legacy-pane" || got.StepTransport != "tmux" || got.ContentSource != "tmux_capture" {
		t.Fatalf("legacy snapshot metadata = tmux:%q transport:%q source:%q", got.TmuxSession, got.StepTransport, got.ContentSource)
	}
	if got.WorkflowPath != "Workflow/substack" || got.ChunkIndex != 7 {
		t.Fatalf("legacy snapshot workflow/chunk = %q/%d", got.WorkflowPath, got.ChunkIndex)
	}
	if !strings.Contains(got.Content, "\x1b[35m") || !strings.Contains(got.Content, "legacy pane") {
		t.Fatalf("expected ANSI legacy terminal content, got %q", got.Content)
	}
	if strings.Contains(got.Content, "hidden-secret") || !strings.Contains(got.Content, "SECRET_FOO=[redacted]") {
		t.Fatalf("legacy snapshot was not redacted: %q", got.Content)
	}
}

func TestResolveRestoredCodingConversationPersistTargetIsTransportSpecific(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	convDir := filepath.Join(root, "_users", "default", "chat_history", "2026-05-18")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("mkdir conversation dir: %v", err)
	}
	codingPath := filepath.Join(convDir, "session-codex-old-conversation.json")
	codingJSON := `{
  "session_id": "codex-old",
  "conversation_history": [
    {"Role": "human", "Parts": [{"Text": "old request"}]},
    {"Role": "ai", "Parts": [{"Text": "old answer"}]}
  ],
  "runtime": {
    "kind": "coding_agent",
    "provider": "codex-cli",
    "transport": "tmux",
    "external_session_id": "codex-native-thread",
    "resume_supported": true
  }
}`
	if err := os.WriteFile(codingPath, []byte(codingJSON), 0o600); err != nil {
		t.Fatalf("write coding conversation: %v", err)
	}

	api := &StreamingAPI{}
	target, ok, err := api.resolveRestoredCodingConversationPersistTarget(
		"default",
		"new-ui-session",
		"_users/default/chat_history/2026-05-18/session-codex-old-conversation.json",
		"",
		"",
		"codex-cli",
		"",
	)
	if err != nil {
		t.Fatalf("resolve coding target error = %v", err)
	}
	if !ok || target == nil {
		t.Fatal("expected coding-agent target")
	}
	if target.SessionID != "codex-old" || target.ConversationPath != "_users/default/chat_history/2026-05-18/session-codex-old-conversation.json" {
		t.Fatalf("target = %#v", target)
	}
	merged := mergeRestoredChatHistory(target.History, []llmtypes.MessageContent{{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "new request"}},
	}})
	if len(merged) != 3 {
		t.Fatalf("merged history length = %d, want 3", len(merged))
	}

	remembered, ok, err := api.resolveRestoredCodingConversationPersistTarget(
		"default",
		"new-ui-session",
		"",
		"",
		"",
		"codex-cli",
		"",
	)
	if err != nil {
		t.Fatalf("resolve remembered target error = %v", err)
	}
	if !ok || remembered == nil || remembered.SessionID != "codex-old" {
		t.Fatalf("expected remembered coding target, ok=%v target=%#v", ok, remembered)
	}

	normalPath := filepath.Join(convDir, "session-normal-old-conversation.json")
	normalJSON := `{
  "session_id": "normal-old",
  "conversation_history": [
    {"Role": "human", "Parts": [{"Text": "normal old request"}]}
  ],
  "runtime": {
    "kind": "llm_agent",
    "provider": "openai",
    "resume_supported": false
  }
}`
	if err := os.WriteFile(normalPath, []byte(normalJSON), 0o600); err != nil {
		t.Fatalf("write normal conversation: %v", err)
	}
	normalTarget, normalOK, err := api.resolveRestoredCodingConversationPersistTarget(
		"default",
		"normal-new-ui-session",
		"_users/default/chat_history/2026-05-18/session-normal-old-conversation.json",
		"",
		"",
		"openai",
		"",
	)
	if err != nil {
		t.Fatalf("resolve normal target error = %v", err)
	}
	if normalOK || normalTarget != nil {
		t.Fatalf("normal llm restore should not reuse old persistence target, ok=%v target=%#v", normalOK, normalTarget)
	}
}

func TestShouldAttachRestoredConversationFallbackSkipsMatchingCodingAgent(t *testing.T) {
	runtime := &ChatHistoryAgentRuntime{
		Kind:              "coding_agent",
		Provider:          "codex-cli",
		ExternalSessionID: "codex-thread-1",
		ResumeSupported:   true,
		WorkshopMode:      "optimizer",
	}

	if shouldAttachRestoredConversationFallback(runtime, "codex-cli", "optimizer") {
		t.Fatalf("expected coding-agent CLI restore to skip visible file fallback")
	}
}

func TestShouldAttachRestoredConversationFallbackKeepsFallbackForNonCodingOrMismatch(t *testing.T) {
	cases := []struct {
		name            string
		runtime         *ChatHistoryAgentRuntime
		currentProvider string
		currentMode     string
	}{
		{
			name: "missing runtime",
		},
		{
			name: "plain llm agent",
			runtime: &ChatHistoryAgentRuntime{
				Kind:     "llm_agent",
				Provider: "openai",
			},
			currentProvider: "openai",
		},
		{
			name: "provider mismatch",
			runtime: &ChatHistoryAgentRuntime{
				Kind:              "coding_agent",
				Provider:          "codex-cli",
				ExternalSessionID: "codex-thread-1",
				ResumeSupported:   true,
				WorkshopMode:      "workshop",
			},
			currentProvider: "claude-code",
			currentMode:     "workshop",
		},
		{
			// Before the workshop merge, "builder" vs "optimizer" was a real
			// mismatch. Both now normalize to "workshop", so the only true
			// workshop-mode mismatch is workshop ↔ run.
			name: "mode mismatch",
			runtime: &ChatHistoryAgentRuntime{
				Kind:              "coding_agent",
				Provider:          "codex-cli",
				ExternalSessionID: "codex-thread-1",
				ResumeSupported:   true,
				WorkshopMode:      "workshop",
			},
			currentProvider: "codex-cli",
			currentMode:     "run",
		},
		{
			name: "coding agent missing native resume support",
			runtime: &ChatHistoryAgentRuntime{
				Kind:         "coding_agent",
				Provider:     "codex-cli",
				WorkshopMode: "workshop",
			},
			currentProvider: "codex-cli",
			currentMode:     "workshop",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !shouldAttachRestoredConversationFallback(tc.runtime, tc.currentProvider, tc.currentMode) {
				t.Fatalf("expected restored file fallback for %#v", tc.runtime)
			}
		})
	}
}

func TestCleanChatHistoryForPersistenceLeavesAssistantMessages(t *testing.T) {
	text := "assistant note\n\nPrevious workflow-builder conversation file: should remain"
	history := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: text},
			},
		},
	}

	cleaned := cleanChatHistoryForPersistence(history)

	got := cleaned[0].Parts[0].(llmtypes.TextContent).Text
	if got != text {
		t.Fatalf("assistant message should not be cleaned, got %q", got)
	}
}

func TestChatHistoryPreviewMessagesSkipsNoisyEntries(t *testing.T) {
	history := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "[AUTO-NOTIFICATION] Agent completed."},
			},
		},
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "check what we did here\n\nPrevious workflow-builder conversation file: old.json"},
			},
		},
		{
			Role: llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "[Previous tool result]: noisy"},
			},
		},
		{
			Role: llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "We added the format library."},
			},
		},
	}

	preview := chatHistoryPreviewMessages(history)

	if len(preview) != 2 {
		t.Fatalf("expected 2 preview messages, got %d: %#v", len(preview), preview)
	}
	if preview[0].Role != "human" || preview[0].Text != "check what we did here" {
		t.Fatalf("unexpected first preview: %#v", preview[0])
	}
	if preview[1].Role != "ai" || preview[1].Text != "We added the format library." {
		t.Fatalf("unexpected second preview: %#v", preview[1])
	}
}

func TestTrimChatHistoryUIEventsKeepsMostRecentEvents(t *testing.T) {
	events := make([]internalevents.Event, maxPersistedChatHistoryUIEvents+5)
	for i := range events {
		events[i] = internalevents.Event{ID: fmt.Sprintf("event-%03d", i)}
	}

	trimmed := trimChatHistoryUIEvents(events)

	if len(trimmed) != maxPersistedChatHistoryUIEvents {
		t.Fatalf("trimmed length = %d, want %d", len(trimmed), maxPersistedChatHistoryUIEvents)
	}
	if trimmed[0].ID != "event-005" {
		t.Fatalf("first retained event = %q, want event-005", trimmed[0].ID)
	}
	if trimmed[len(trimmed)-1].ID != "event-204" {
		t.Fatalf("last retained event = %q, want event-204", trimmed[len(trimmed)-1].ID)
	}
}

func TestReadWorkflowScopedChatHistoryConversationDirect(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	workspacePath := "Workflow/demo"
	sessionID := "direct-1"
	convDir := filepath.Join(root, filepath.FromSlash(workspacePath), "builder", "conversation", "2026-05-16")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("mkdir conversation dir: %v", err)
	}
	want := []byte(`{"session_id":"direct-1","conversation_history":[]}`)
	if err := os.WriteFile(filepath.Join(convDir, "session-"+sessionID+"-conversation.json"), want, 0o600); err != nil {
		t.Fatalf("write conversation: %v", err)
	}

	got, ok, err := readWorkflowScopedChatHistoryConversationDirect(sessionID, workspacePath)
	if err != nil {
		t.Fatalf("direct read error = %v", err)
	}
	if !ok {
		t.Fatal("direct read ok = false, want true")
	}
	if string(got) != string(want) {
		t.Fatalf("direct read = %s, want %s", got, want)
	}
}

func TestListChatHistorySessionsFromDiskReadsDateBucketLayout(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	convDir := filepath.Join(root, "_users", "default", "chat_history", "2026-05-17")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("mkdir conversation dir: %v", err)
	}
	convPath := filepath.Join(convDir, "session-chat-1-conversation.json")
	data := `{
  "session_id": "chat-1",
  "agent_mode": "simple",
  "conversation_history": [
    {"Role": "human", "Parts": [{"Text": "hello from date bucket"}]}
  ],
  "updated_at": "2026-05-17T10:00:00Z"
}`
	if err := os.WriteFile(convPath, []byte(data), 0o600); err != nil {
		t.Fatalf("write conversation: %v", err)
	}

	sessions, ok, err := listChatHistorySessionsFromDisk("default", "_users/default/chat_history", "", 10, 0)
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	if !ok {
		t.Fatal("list ok = false, want true")
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1: %#v", len(sessions), sessions)
	}
	if sessions[0].SessionID != "chat-1" {
		t.Fatalf("session id = %q, want chat-1", sessions[0].SessionID)
	}
	if sessions[0].ConversationPath != "_users/default/chat_history/2026-05-17/session-chat-1-conversation.json" {
		t.Fatalf("conversation path = %q", sessions[0].ConversationPath)
	}
}

func TestListChatHistorySessionsFromDiskCollapsesMultiAgentScheduleRunsBySchedule(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	convDir := filepath.Join(root, "_users", "default", "chat_history", "2026-05-17")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("mkdir conversation dir: %v", err)
	}

	writeConversation := func(sessionID, updatedAt, text string, mtime time.Time) {
		t.Helper()
		data := fmt.Sprintf(`{
  "session_id": %q,
  "agent_mode": "simple",
  "conversation_history": [{"Role": "human", "Parts": [{"Text": %q}]}],
  "updated_at": %q
}`, sessionID, text, updatedAt)
		path := filepath.Join(convDir, "session-"+sessionID+"-conversation.json")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatalf("write conversation %s: %v", sessionID, err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("chtimes conversation %s: %v", sessionID, err)
		}
	}

	oldCronSession := "schedule-cron--abc12345_100"
	legacySession := "sched_abc12345_150"
	latestManualSession := "schedule-manual--abc12345_300"
	otherScheduleSession := "schedule-cron--def67890_100"
	normalSession := "normal-chat-1"

	writeConversation(oldCronSession, "2026-05-17T10:00:00Z", "old cron run", time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC))
	writeConversation(legacySession, "2026-05-17T11:00:00Z", "legacy schedule run", time.Date(2026, 5, 17, 11, 0, 0, 0, time.UTC))
	writeConversation(latestManualSession, "2026-05-18T10:00:00Z", "latest manual run", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC))
	writeConversation(otherScheduleSession, "2026-05-17T12:00:00Z", "other schedule run", time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC))
	writeConversation(normalSession, "2026-05-18T11:00:00Z", "normal chat", time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC))

	runsPath := filepath.Join(root, "_users", "default", "multiagent-schedule-runs.json")
	runsJSON := fmt.Sprintf(`[
  {"id":"run-new","schedule_id":"sched-a","session_id":%q,"status":"success","started_at":"2026-05-18T10:00:00Z"},
  {"id":"run-legacy","schedule_id":"sched-a","session_id":%q,"status":"success","started_at":"2026-05-17T11:00:00Z"},
  {"id":"run-old","schedule_id":"sched-a","session_id":%q,"status":"success","started_at":"2026-05-17T10:00:00Z"},
  {"id":"run-other","schedule_id":"sched-b","session_id":%q,"status":"success","started_at":"2026-05-17T12:00:00Z"}
]`, latestManualSession, legacySession, oldCronSession, otherScheduleSession)
	if err := os.WriteFile(runsPath, []byte(runsJSON), 0o600); err != nil {
		t.Fatalf("write multiagent schedule runs: %v", err)
	}

	sessions, ok, err := listChatHistorySessionsFromDisk("default", "_users/default/chat_history", "", 10, 0)
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	if !ok {
		t.Fatal("list ok = false, want true")
	}

	ids := map[string]bool{}
	for _, session := range sessions {
		ids[session.SessionID] = true
	}
	if len(sessions) != 3 {
		t.Fatalf("session count = %d, want 3: %#v", len(sessions), sessions)
	}
	for _, want := range []string{normalSession, latestManualSession, otherScheduleSession} {
		if !ids[want] {
			t.Fatalf("missing session %q in %#v", want, sessions)
		}
	}
	for _, collapsed := range []string{oldCronSession, legacySession} {
		if ids[collapsed] {
			t.Fatalf("collapsed schedule session %q should not be listed: %#v", collapsed, sessions)
		}
	}
}

func TestListWorkflowChatHistorySessionsCollapsesScheduleRunsBySchedule(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	workflowPath := "Workflow/instagram"
	convDir := filepath.Join(root, "Workflow", "instagram", "builder", "conversation", "2026-05-17")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("mkdir workflow conversation dir: %v", err)
	}

	writeConversation := func(sessionID, updatedAt, text string, mtime time.Time) {
		t.Helper()
		data := fmt.Sprintf(`{
  "session_id": %q,
  "agent_mode": "workflow",
  "conversation_history": [{"Role": "human", "Parts": [{"Text": %q}]}],
  "updated_at": %q
}`, sessionID, text, updatedAt)
		path := filepath.Join(convDir, "session-"+sessionID+"-conversation.json")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatalf("write conversation %s: %v", sessionID, err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("chtimes conversation %s: %v", sessionID, err)
		}
	}

	oldScheduleSession := "schedule-cron--abc12345_100"
	unindexedScheduleSession := "schedule-cron--abc12345_150"
	latestScheduleSession := "schedule-cron--abc12345_200"
	otherScheduleSession := "schedule-cron--def67890_100"
	manualSession := "manual-chat-1"

	writeConversation(oldScheduleSession, "2026-05-17T10:00:00Z", "old schedule run", time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC))
	writeConversation(unindexedScheduleSession, "2026-05-17T11:00:00Z", "unindexed schedule run", time.Date(2026, 5, 17, 11, 0, 0, 0, time.UTC))
	writeConversation(latestScheduleSession, "2026-05-18T10:00:00Z", "latest schedule run", time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC))
	writeConversation(otherScheduleSession, "2026-05-17T12:00:00Z", "other schedule run", time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC))
	writeConversation(manualSession, "2026-05-18T11:00:00Z", "manual chat", time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC))

	scheduleRuns := fmt.Sprintf(`[
  {"id":"run-new","schedule_id":"sched-a","session_id":%q,"status":"success","started_at":"2026-05-18T10:00:00Z"},
  {"id":"run-old","schedule_id":"sched-a","session_id":%q,"status":"success","started_at":"2026-05-17T10:00:00Z"},
  {"id":"run-other","schedule_id":"sched-b","session_id":%q,"status":"success","started_at":"2026-05-17T12:00:00Z"}
]`, latestScheduleSession, oldScheduleSession, otherScheduleSession)
	if err := os.WriteFile(filepath.Join(root, "Workflow", "instagram", "schedule-runs.json"), []byte(scheduleRuns), 0o600); err != nil {
		t.Fatalf("write schedule-runs: %v", err)
	}

	sessions, err := ListChatHistorySessions("default", 10, 0, workflowPath)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}

	ids := map[string]bool{}
	for _, session := range sessions {
		ids[session.SessionID] = true
	}
	if len(sessions) != 3 {
		t.Fatalf("session count = %d, want 3: %#v", len(sessions), sessions)
	}
	for _, want := range []string{manualSession, latestScheduleSession, otherScheduleSession} {
		if !ids[want] {
			t.Fatalf("missing session %q in %#v", want, sessions)
		}
	}
	if ids[oldScheduleSession] {
		t.Fatalf("old schedule session %q should be collapsed out: %#v", oldScheduleSession, sessions)
	}
	if ids[unindexedScheduleSession] {
		t.Fatalf("unindexed schedule session %q should still collapse by schedule prefix: %#v", unindexedScheduleSession, sessions)
	}
}

func TestDeleteChatHistorySessionDeletesUserDateBucketAndLegacyFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	legacyDir := filepath.Join(root, "_users", "default", "chat_history", "chat-1")
	dateDir := filepath.Join(root, "_users", "default", "chat_history", "2026-05-17")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir date dir: %v", err)
	}
	legacyPath := filepath.Join(legacyDir, "conversation.json")
	datePath := filepath.Join(dateDir, "session-chat-1-conversation.json")
	if err := os.WriteFile(legacyPath, []byte(`{"session_id":"chat-1","conversation_history":[]}`), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if err := os.WriteFile(datePath, []byte(`{"session_id":"chat-1","conversation_history":[]}`), 0o600); err != nil {
		t.Fatalf("write date bucket: %v", err)
	}

	result, err := DeleteChatHistorySession("default", "chat-1", "")
	if err != nil {
		t.Fatalf("delete error = %v", err)
	}
	if result.DeletedCount != 2 {
		t.Fatalf("deleted count = %d, want 2: %#v", result.DeletedCount, result.DeletedPaths)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy conversation still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(datePath); !os.IsNotExist(err) {
		t.Fatalf("date conversation still exists or stat failed unexpectedly: %v", err)
	}
}

func TestDeleteChatHistoryOlderThanUsesJSONTimestampAndSkipsEmpty(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	convDir := filepath.Join(root, "_users", "default", "chat_history", "2026-05-17")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("mkdir conversation dir: %v", err)
	}

	oldWithFreshMTime := filepath.Join(convDir, "session-old-json-conversation.json")
	if err := os.WriteFile(oldWithFreshMTime, []byte(`{
  "session_id": "old-json",
  "conversation_history": [{"Role": "human", "Parts": [{"Text": "old by json"}]}],
  "updated_at": "`+time.Now().AddDate(0, 0, -15).Format(time.RFC3339)+`"
}`), 0o600); err != nil {
		t.Fatalf("write old-json: %v", err)
	}

	newWithOldMTime := filepath.Join(convDir, "session-new-json-conversation.json")
	if err := os.WriteFile(newWithOldMTime, []byte(`{
  "session_id": "new-json",
  "conversation_history": [{"Role": "human", "Parts": [{"Text": "new by json"}]}],
  "updated_at": "`+time.Now().Format(time.RFC3339)+`"
}`), 0o600); err != nil {
		t.Fatalf("write new-json: %v", err)
	}
	oldMTime := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(newWithOldMTime, oldMTime, oldMTime); err != nil {
		t.Fatalf("chtimes new-json: %v", err)
	}

	emptyOld := filepath.Join(convDir, "session-empty-old-conversation.json")
	if err := os.WriteFile(emptyOld, []byte(`{
  "session_id": "empty-old",
  "conversation_history": [],
  "updated_at": "`+time.Now().AddDate(0, 0, -15).Format(time.RFC3339)+`"
}`), 0o600); err != nil {
		t.Fatalf("write empty-old: %v", err)
	}

	result, err := DeleteChatHistoryOlderThan("default", 14, "")
	if err != nil {
		t.Fatalf("cleanup error = %v", err)
	}
	if result.DeletedCount != 1 {
		t.Fatalf("deleted count = %d, want 1: %#v", result.DeletedCount, result.DeletedPaths)
	}
	if _, err := os.Stat(oldWithFreshMTime); !os.IsNotExist(err) {
		t.Fatalf("old json conversation still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(newWithOldMTime); err != nil {
		t.Fatalf("new json conversation should remain: %v", err)
	}
	if _, err := os.Stat(emptyOld); err != nil {
		t.Fatalf("empty old conversation should remain: %v", err)
	}
}

func TestDeleteChatHistoryOlderThanWorkflowUsesJSONTimestampAndSkipsEmpty(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	convDir := filepath.Join(root, "Workflow", "instagram", "builder", "conversation", "2026-05-17")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("mkdir workflow conversation dir: %v", err)
	}

	oldWorkflow := filepath.Join(convDir, "session-workflow-old-conversation.json")
	if err := os.WriteFile(oldWorkflow, []byte(`{
  "session_id": "workflow-old",
  "conversation_history": [{"Role": "human", "Parts": [{"Text": "old workflow"}]}],
  "updated_at": "`+time.Now().AddDate(0, 0, -15).Format(time.RFC3339)+`"
}`), 0o600); err != nil {
		t.Fatalf("write workflow-old: %v", err)
	}

	emptyWorkflow := filepath.Join(convDir, "session-workflow-empty-conversation.json")
	if err := os.WriteFile(emptyWorkflow, []byte(`{
  "session_id": "workflow-empty",
  "conversation_history": [],
  "updated_at": "`+time.Now().AddDate(0, 0, -15).Format(time.RFC3339)+`"
}`), 0o600); err != nil {
		t.Fatalf("write workflow-empty: %v", err)
	}

	result, err := DeleteChatHistoryOlderThan("default", 14, "Workflow/instagram")
	if err != nil {
		t.Fatalf("cleanup error = %v", err)
	}
	if result.DeletedCount != 1 {
		t.Fatalf("deleted count = %d, want 1: %#v", result.DeletedCount, result.DeletedPaths)
	}
	if _, err := os.Stat(oldWorkflow); !os.IsNotExist(err) {
		t.Fatalf("old workflow conversation still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(emptyWorkflow); err != nil {
		t.Fatalf("empty workflow conversation should remain: %v", err)
	}
}

func TestReadUserChatHistoryConversationDirectPrefersNewestDateBucket(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	oldDir := filepath.Join(root, "_users", "default", "chat_history", "chat-1")
	newDir := filepath.Join(root, "_users", "default", "chat_history", "2026-05-17")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("mkdir date dir: %v", err)
	}
	legacy := []byte(`{"session_id":"chat-1","updated_at":"2026-05-16T10:00:00Z","conversation_history":[]}`)
	dateBucket := []byte(`{"session_id":"chat-1","updated_at":"2026-05-17T10:00:00Z","conversation_history":[]}`)
	if err := os.WriteFile(filepath.Join(oldDir, "conversation.json"), legacy, 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "session-chat-1-conversation.json"), dateBucket, 0o600); err != nil {
		t.Fatalf("write date bucket: %v", err)
	}

	got, ok, err := readUserChatHistoryConversationDirect("default", "chat-1")
	if err != nil {
		t.Fatalf("direct read error = %v", err)
	}
	if !ok {
		t.Fatal("direct read ok = false, want true")
	}
	if string(got) != string(dateBucket) {
		t.Fatalf("direct read = %s, want %s", got, dateBucket)
	}
}

func TestParseLocalChatHistorySessionIncludesRuntime(t *testing.T) {
	data := `{
  "session_id": "session-1",
  "agent_mode": "simple",
  "runtime": {
    "kind": "coding_agent",
    "provider": "claude-code",
    "model_id": "claude-code",
    "external_session_id": "claude-session-1",
    "resume_supported": true,
    "resume_flag": "--resume",
    "workspace_path": "Workflow/example"
  },
  "conversation_history": [
    {
      "Role": "human",
      "Parts": [{"Text": "hello"}]
    }
  ],
  "updated_at": "2026-05-15T10:00:00Z"
}`

	session, ok := parseLocalChatHistorySession("default", "_users/default/chat_history", "", "fallback", data, time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("expected session to parse")
	}
	if session.Runtime == nil {
		t.Fatal("expected runtime metadata")
	}
	if session.Runtime.Provider != "claude-code" || session.Runtime.ExternalSessionID != "claude-session-1" || !session.Runtime.ResumeSupported {
		t.Fatalf("unexpected runtime metadata: %#v", session.Runtime)
	}
}

func TestParseLocalChatHistorySessionSkipsLowSignalTitle(t *testing.T) {
	data := `{
  "session_id": "session-1",
  "agent_mode": "simple",
  "conversation_history": [
    {
      "Role": "human",
      "Parts": [{"Text": "hello"}]
    },
    {
      "Role": "ai",
      "Parts": [{"Text": "Hi"}]
    },
    {
      "Role": "human",
      "Parts": [{"Text": "ok.. which image generated tools do you have"}]
    }
  ],
  "updated_at": "2026-05-15T10:00:00Z"
}`

	session, ok := parseLocalChatHistorySession("default", "_users/default/chat_history", "", "fallback", data, time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("expected session to parse")
	}
	if session.Query != "ok.. which image generated tools do you have" {
		t.Fatalf("expected substantive query title, got %q", session.Query)
	}
}

func TestParseLocalChatHistorySessionUsesLatestSubstantiveTitle(t *testing.T) {
	data := `{
  "session_id": "session-1",
  "agent_mode": "workflow",
  "conversation_history": [
    {
      "Role": "human",
      "Parts": [{"Text": "Explain this codebase"}]
    },
    {
      "Role": "ai",
      "Parts": [{"Text": "Sure."}]
    },
    {
      "Role": "human",
      "Parts": [{"Text": "what did you fix last time?"}]
    }
  ],
  "updated_at": "2026-05-15T10:00:00Z"
}`

	session, ok := parseLocalChatHistorySession("default", "Workflow/example", "Workflow/example", "fallback", data, time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("expected session to parse")
	}
	if session.Query != "what did you fix last time?" {
		t.Fatalf("expected latest substantive query title, got %q", session.Query)
	}
}

func TestParseLocalChatHistorySessionIgnoresTrailingLowSignalTitle(t *testing.T) {
	data := `{
  "session_id": "session-1",
  "agent_mode": "workflow",
  "conversation_history": [
    {
      "Role": "human",
      "Parts": [{"Text": "Explain this codebase"}]
    },
    {
      "Role": "human",
      "Parts": [{"Text": "what did you fix last time?"}]
    },
    {
      "Role": "human",
      "Parts": [{"Text": "ok"}]
    }
  ],
  "updated_at": "2026-05-15T10:00:00Z"
}`

	session, ok := parseLocalChatHistorySession("default", "Workflow/example", "Workflow/example", "fallback", data, time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("expected session to parse")
	}
	if session.Query != "what did you fix last time?" {
		t.Fatalf("expected latest substantive query title, got %q", session.Query)
	}
}

func TestCaptureChatHistoryAgentRuntimeStoresCLIResumeID(t *testing.T) {
	api := &StreamingAPI{}

	runtime := api.captureChatHistoryAgentRuntime("session-1", "claude-code", "claude-code", "Workflow/example", &mcpagent.Agent{
		ClaudeCodeSessionID: "claude-session-1",
	})

	if runtime == nil {
		t.Fatal("expected runtime metadata")
	}
	if runtime.Kind != "coding_agent" || runtime.Provider != "claude-code" {
		t.Fatalf("unexpected runtime identity: %#v", runtime)
	}
	if runtime.ExternalSessionID != "claude-session-1" || !runtime.ResumeSupported || runtime.ResumeFlag != "--resume" {
		t.Fatalf("unexpected resume metadata: %#v", runtime)
	}
}

func TestCaptureChatHistoryAgentRuntimeStoresGeminiSessionAndProjectDir(t *testing.T) {
	sessionID := "gemini-chat-session"
	workspace.ClearSessionShellConfig(sessionID)
	t.Cleanup(func() { workspace.ClearSessionShellConfig(sessionID) })

	api := &StreamingAPI{}

	runtime := api.captureChatHistoryAgentRuntime(sessionID, "gemini-cli", "auto", "_users/default/Chats", &mcpagent.Agent{
		GeminiSessionID:       "gemini-native-session-1",
		GeminiProjectDirID:    "session-gemini-chat-session",
		CodingAgentWorkingDir: "/tmp/user-chat",
	})

	if runtime == nil {
		t.Fatal("expected runtime metadata")
	}
	if runtime.Kind != "coding_agent" || runtime.Provider != "gemini-cli" {
		t.Fatalf("unexpected runtime identity: %#v", runtime)
	}
	if runtime.ExternalSessionID != "gemini-native-session-1" || !runtime.ResumeSupported || runtime.ResumeFlag != "--resume" {
		t.Fatalf("unexpected Gemini resume metadata: %#v", runtime)
	}
	if runtime.ProjectDirID != "session-gemini-chat-session" {
		t.Fatalf("runtime project dir ID = %q, want stable helper project dir", runtime.ProjectDirID)
	}
	if cfg := workspace.GetSessionShellConfig(sessionID); cfg == nil || cfg.GeminiProjectDirID != "session-gemini-chat-session" {
		t.Fatalf("workspace shell Gemini project dir config = %#v", cfg)
	}
}

func TestCaptureChatHistoryAgentRuntimeStoresCodexThreadID(t *testing.T) {
	api := &StreamingAPI{}

	runtime := api.captureChatHistoryAgentRuntime("codex-session", "codex-cli", "gpt-5.4", "Workflow/example", &mcpagent.Agent{
		CodexSessionID:    "codex-thread-1",
		CodexProjectDirID: "Workflow/example",
	})

	if runtime == nil {
		t.Fatal("expected runtime metadata")
	}
	if runtime.Kind != "coding_agent" || runtime.Provider != "codex-cli" {
		t.Fatalf("unexpected runtime identity: %#v", runtime)
	}
	if runtime.ExternalSessionID != "codex-thread-1" || !runtime.ResumeSupported || runtime.ResumeFlag != "resume" {
		t.Fatalf("unexpected Codex resume metadata: %#v", runtime)
	}
	if runtime.ProjectDirID != "Workflow/example" {
		t.Fatalf("runtime project dir ID = %q", runtime.ProjectDirID)
	}
}

func TestCaptureChatHistoryAgentRuntimeStoresAgyConversationID(t *testing.T) {
	api := &StreamingAPI{}

	runtime := api.captureChatHistoryAgentRuntime("agy-session", "agy-cli", "agy-cli", "Workflow/example", &mcpagent.Agent{
		AgySessionID: "agy-conversation-1",
	})

	if runtime == nil {
		t.Fatal("expected runtime metadata")
	}
	if runtime.Kind != "coding_agent" || runtime.Provider != "agy-cli" {
		t.Fatalf("unexpected runtime identity: %#v", runtime)
	}
	if runtime.ExternalSessionID != "agy-conversation-1" || !runtime.ResumeSupported || runtime.ResumeFlag != "--conversation" {
		t.Fatalf("unexpected Agy resume metadata: %#v", runtime)
	}
}

func TestCaptureChatHistoryAgentRuntimeStoresPiSessionID(t *testing.T) {
	api := &StreamingAPI{}

	runtime := api.captureChatHistoryAgentRuntime("pi-session", "pi-cli", "google/gemini-3.5-flash", "Workflow/example", &mcpagent.Agent{
		PiSessionID: "mlp-pi-session-1",
	})

	if runtime == nil {
		t.Fatal("expected runtime metadata")
	}
	if runtime.Kind != "coding_agent" || runtime.Provider != "pi-cli" {
		t.Fatalf("unexpected runtime identity: %#v", runtime)
	}
	if runtime.ExternalSessionID != "mlp-pi-session-1" || !runtime.ResumeSupported || runtime.ResumeFlag != "--session-id" {
		t.Fatalf("unexpected Pi resume metadata: %#v", runtime)
	}
}

func TestCaptureChatHistoryAgentRuntimePersistsAgentSessionHandle(t *testing.T) {
	api := &StreamingAPI{}

	agent := &mcpagent.Agent{
		SessionID: "chat-session-1",
		CodingProviderSessionHandle: llmtypes.CodingProviderSessionHandle{
			Provider:        "codex-cli",
			Transport:       llmtypes.CodingProviderTransportTmux,
			NativeSessionID: "codex-thread-typed",
			TmuxSession:     "tmux-codex-1",
			WorkingDir:      "/workspace",
			ProjectDirID:    "Workflow/example",
			Model:           "gpt-5.4",
			Status:          llmtypes.CodingProviderSessionStatusIdle,
		},
	}

	runtime := api.captureChatHistoryAgentRuntime("chat-session-1", "codex-cli", "gpt-5.4", "Workflow/example", agent)

	if runtime == nil || runtime.AgentSessionHandle == nil {
		t.Fatalf("expected persisted agent session handle, got %#v", runtime)
	}
	if runtime.AgentSessionHandle.Provider.NativeSessionID != "codex-thread-typed" {
		t.Fatalf("runtime handle = %#v", runtime.AgentSessionHandle)
	}
	if runtime.ExternalSessionID != "codex-thread-typed" || runtime.ProjectDirID != "Workflow/example" {
		t.Fatalf("legacy fields not mirrored from handle: %#v", runtime)
	}
	if runtime.Transport != llmtypes.CodingProviderTransportTmux {
		t.Fatalf("runtime transport = %q, want tmux", runtime.Transport)
	}
}

func TestCaptureChatHistoryAgentRuntimeInfersProviderFromSessionHandle(t *testing.T) {
	api := &StreamingAPI{}

	agent := &mcpagent.Agent{
		SessionID: "chat-session-1",
		CodingProviderSessionHandle: llmtypes.CodingProviderSessionHandle{
			Provider:        "codex-cli",
			Transport:       llmtypes.CodingProviderTransportTmux,
			NativeSessionID: "codex-thread-typed",
			ProjectDirID:    "Workflow/example",
			Model:           "gpt-5.4",
		},
	}

	runtime := api.captureChatHistoryAgentRuntime("chat-session-1", "", "", "Workflow/example", agent)

	if runtime == nil {
		t.Fatal("expected runtime metadata")
	}
	if runtime.Kind != "coding_agent" || runtime.Provider != "codex-cli" || runtime.ModelID != "gpt-5.4" {
		t.Fatalf("runtime did not infer coding provider from handle: %#v", runtime)
	}
	if runtime.ExternalSessionID != "codex-thread-typed" || !runtime.ResumeSupported {
		t.Fatalf("runtime did not mirror native session from handle: %#v", runtime)
	}
}

func TestSeedCodingAgentRuntimeRestoresGeminiProjectDirForNextTurn(t *testing.T) {
	sessionID := "gemini-restore-session"
	workspace.ClearSessionShellConfig(sessionID)
	t.Cleanup(func() { workspace.ClearSessionShellConfig(sessionID) })

	api := &StreamingAPI{}
	runtime := &ChatHistoryAgentRuntime{
		Kind:              "coding_agent",
		Provider:          "gemini-cli",
		ExternalSessionID: "gemini-native-session-2",
		ProjectDirID:      "session-gemini-restore-session",
		ResumeSupported:   true,
	}
	nextTurnAgent := &mcpagent.Agent{}

	if !api.seedCodingAgentRuntimeFromRestoredConversation(sessionID, "gemini-cli", "", runtime, nextTurnAgent) {
		t.Fatal("expected Gemini runtime to seed native resume state")
	}

	if nextTurnAgent.GeminiSessionID != "gemini-native-session-2" {
		t.Fatalf("restored Gemini session ID = %q", nextTurnAgent.GeminiSessionID)
	}
	if nextTurnAgent.GeminiProjectDirID != "session-gemini-restore-session" {
		t.Fatalf("restored Gemini project dir ID = %q", nextTurnAgent.GeminiProjectDirID)
	}
	if cfg := workspace.GetSessionShellConfig(sessionID); cfg == nil || cfg.GeminiProjectDirID != "session-gemini-restore-session" {
		t.Fatalf("workspace shell Gemini project dir config = %#v", cfg)
	}
}

func TestReadChatHistoryRuntimeFromPathReadsDateBucketRuntime(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	convDir := filepath.Join(root, "_users", "default", "chat_history", "2026-05-17")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	convPath := filepath.Join(convDir, "session-chat-1-conversation.json")
	if err := os.WriteFile(convPath, []byte(`{
  "session_id": "chat-1",
  "runtime": {
    "kind": "coding_agent",
    "provider": "claude-code",
    "model_id": "claude-sonnet-4-6",
    "external_session_id": "claude-native-1",
    "resume_supported": true,
    "resume_flag": "--resume"
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	runtime, ok, err := ReadChatHistoryRuntimeFromPath("default", "_users/default/chat_history/2026-05-17/session-chat-1-conversation.json")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || runtime == nil {
		t.Fatalf("expected runtime metadata, ok=%v runtime=%#v", ok, runtime)
	}
	if runtime.Provider != "claude-code" || runtime.ExternalSessionID != "claude-native-1" {
		t.Fatalf("unexpected runtime: %#v", runtime)
	}
}

func TestReadChatHistoryRuntimeForSessionReadsWorkflowScopedRuntime(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	convDir := filepath.Join(root, "Workflow", "rtslatency", "builder", "conversation", "2026-05-20")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	convPath := filepath.Join(convDir, "session-chat-1-conversation.json")
	if err := os.WriteFile(convPath, []byte(`{
  "session_id": "chat-1",
  "workshop_mode": "builder",
  "runtime": {
    "kind": "coding_agent",
    "provider": "claude-code",
    "model_id": "claude-opus-4-6",
    "external_session_id": "claude-native-workflow",
    "resume_supported": true,
    "resume_flag": "--resume"
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	runtime, ok, err := ReadChatHistoryRuntimeForSession("default", "chat-1", "Workflow/rtslatency")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || runtime == nil {
		t.Fatalf("expected runtime metadata, ok=%v runtime=%#v", ok, runtime)
	}
	// "builder" on disk is normalized to "workshop" on read (post-merge).
	if runtime.Provider != "claude-code" || runtime.ExternalSessionID != "claude-native-workflow" || runtime.WorkshopMode != "workshop" {
		t.Fatalf("unexpected runtime: %#v", runtime)
	}
}

func TestFindChatHistoryConversationPathForSessionReadsWorkflowScopedPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	convDir := filepath.Join(root, "Workflow", "rtslatency", "builder", "conversation", "2026-05-20")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	convPath := filepath.Join(convDir, "session-chat-1-conversation.json")
	if err := os.WriteFile(convPath, []byte(`{
  "session_id": "chat-1",
  "conversation_history": [
    {"Role": "human", "Parts": [{"Text": "continue"}]}
  ],
  "updated_at": "2026-05-20T10:00:00Z"
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok, err := FindChatHistoryConversationPathForSession("default", "chat-1", "Workflow/rtslatency")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected conversation path")
	}
	want := "Workflow/rtslatency/builder/conversation/2026-05-20/session-chat-1-conversation.json"
	if got != want {
		t.Fatalf("conversation path = %q, want %q", got, want)
	}
}

func TestSeedCodingAgentRuntimeFromRestoredConversationRestoresClaude(t *testing.T) {
	api := &StreamingAPI{}
	agent := &mcpagent.Agent{SessionID: "new-ui-session"}
	runtime := &ChatHistoryAgentRuntime{
		Kind:              "coding_agent",
		Provider:          "claude-code",
		ExternalSessionID: "claude-native-restored",
		ResumeSupported:   true,
	}

	if !api.seedCodingAgentRuntimeFromRestoredConversation("new-ui-session", "claude-code", "", runtime, agent) {
		t.Fatal("expected native resume state to be seeded")
	}

	if agent.ClaudeCodeSessionID != "claude-native-restored" {
		t.Fatalf("agent ClaudeCodeSessionID = %q", agent.ClaudeCodeSessionID)
	}
}

func TestSeedCodingAgentRuntimeFromCurrentConversationRestoresClaude(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	convDir := filepath.Join(root, "Workflow", "rtslatency", "builder", "conversation", "2026-05-20")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	convPath := filepath.Join(convDir, "session-existing-chat-conversation.json")
	if err := os.WriteFile(convPath, []byte(`{
  "session_id": "existing-chat",
  "workshop_mode": "builder",
  "runtime": {
    "kind": "coding_agent",
    "provider": "claude-code",
    "external_session_id": "claude-native-existing",
    "resume_supported": true,
    "resume_flag": "--resume"
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	api := &StreamingAPI{}
	agent := &mcpagent.Agent{}

	seeded, recoveredRuntime := api.seedCodingAgentRuntimeFromCurrentConversation("existing-chat", "default", "claude-code", "builder", "Workflow/rtslatency", agent)
	if !seeded {
		t.Fatal("expected current conversation runtime to seed native resume state")
	}
	if recoveredRuntime == nil {
		t.Fatal("expected the recovered runtime to be returned for the auto-resume re-launch path")
	}
	if agent.ClaudeCodeSessionID != "claude-native-existing" {
		t.Fatalf("agent ClaudeCodeSessionID = %q", agent.ClaudeCodeSessionID)
	}
}

func TestSeedCodingAgentRuntimeFromRestoredConversationRestoresCodex(t *testing.T) {
	api := &StreamingAPI{}
	agent := &mcpagent.Agent{}
	runtime := &ChatHistoryAgentRuntime{
		Kind:              "coding_agent",
		Provider:          "codex-cli",
		ExternalSessionID: "codex-thread-restored",
		ProjectDirID:      "Workflow/example",
		ResumeSupported:   true,
	}

	if !api.seedCodingAgentRuntimeFromRestoredConversation("codex-ui-session", "codex-cli", "", runtime, agent) {
		t.Fatal("expected Codex resume state to be seeded")
	}

	if agent.CodexSessionID != "codex-thread-restored" {
		t.Fatalf("agent CodexSessionID = %q", agent.CodexSessionID)
	}
	if agent.CodexProjectDirID != "Workflow/example" {
		t.Fatalf("agent CodexProjectDirID = %q", agent.CodexProjectDirID)
	}
}

func TestSeedCodingAgentRuntimeFromRestoredConversationRestoresAgy(t *testing.T) {
	api := &StreamingAPI{}
	agent := &mcpagent.Agent{}
	runtime := &ChatHistoryAgentRuntime{
		Kind:              "coding_agent",
		Provider:          "agy-cli",
		ExternalSessionID: "agy-conversation-restored",
		ResumeSupported:   true,
	}

	if !api.seedCodingAgentRuntimeFromRestoredConversation("agy-ui-session", "agy-cli", "", runtime, agent) {
		t.Fatal("expected Agy resume state to be seeded")
	}

	if agent.AgySessionID != "agy-conversation-restored" {
		t.Fatalf("agent AgySessionID = %q", agent.AgySessionID)
	}
}

func TestSeedCodingAgentRuntimeFromRestoredConversationRestoresPi(t *testing.T) {
	api := &StreamingAPI{}
	agent := &mcpagent.Agent{}
	runtime := &ChatHistoryAgentRuntime{
		Kind:              "coding_agent",
		Provider:          "pi-cli",
		ExternalSessionID: "mlp-pi-restored",
		ResumeSupported:   true,
	}

	if !api.seedCodingAgentRuntimeFromRestoredConversation("pi-ui-session", "pi-cli", "", runtime, agent) {
		t.Fatal("expected Pi resume state to be seeded")
	}

	if agent.PiSessionID != "mlp-pi-restored" {
		t.Fatalf("agent PiSessionID = %q", agent.PiSessionID)
	}
}

// TestSeedCodingAgentRuntimeFromRestoredConversationAppliesPersistedSystemPrompt
// locks in the fix for the "rules file shows wrong content after restart"
// bug. Without this, the chat-restore path leaves the agent on its default
// mcpagent base prompt and the coding-CLI adapter then projects that base
// prompt into <workflow>/.agents/rules/mlp-system.md (etc.) instead of the
// workflow-builder workshop template that was active at chat-save time.
func TestSeedCodingAgentRuntimeFromRestoredConversationAppliesPersistedSystemPrompt(t *testing.T) {
	api := &StreamingAPI{}
	agent := &mcpagent.Agent{}
	// Workshop template stand-in. The real one is 10K+ chars; this sentinel
	// is enough to verify the restored prompt routes through SetSystemPrompt.
	workshopPrompt := "<workshop-template>workflow-builder system prompt</workshop-template>"
	capability := "<capability>browser available</capability>"
	secretBlock := "<secrets>SECRET_FOO=$SECRET_FOO</secrets>"
	runtime := &ChatHistoryAgentRuntime{
		Kind:                  "coding_agent",
		Provider:              "agy-cli",
		ExternalSessionID:     "agy-conversation-restored",
		ResumeSupported:       true,
		SystemPrompt:          workshopPrompt,
		AppendedSystemPrompts: []string{capability, secretBlock},
	}

	if !api.seedCodingAgentRuntimeFromRestoredConversation("agy-ui-session", "agy-cli", "", runtime, agent) {
		t.Fatal("expected Agy resume state to be seeded")
	}

	// AppendSystemPrompt mutates systemPrompt into the concatenated form, so
	// GetSystemPrompt() now returns base + "\n\n" + each appended section.
	// Verify all three pieces survived the restore.
	got := agent.GetSystemPrompt()
	if !strings.Contains(got, workshopPrompt) {
		t.Fatalf("restored composite prompt must contain the workshop template\n  got:  %q\n  want substring: %q", got, workshopPrompt)
	}
	if !strings.Contains(got, capability) {
		t.Fatalf("restored composite prompt must contain the capability append\n  got:  %q\n  want substring: %q", got, capability)
	}
	if !strings.Contains(got, secretBlock) {
		t.Fatalf("restored composite prompt must contain the secret-block append\n  got:  %q\n  want substring: %q", got, secretBlock)
	}
	appended := agent.GetAppendedSystemPrompts()
	if len(appended) != 2 || appended[0] != capability || appended[1] != secretBlock {
		t.Fatalf("agent appended prompts = %q, want [%q %q]", appended, capability, secretBlock)
	}
}

// TestCaptureChatHistoryAgentRuntimeDoesNotDuplicateAppendsAcrossRestores guards
// against the prompt-bloat regression where every chat-restore cycle added
// another copy of the supplementary appendix (capability / secrets / browser).
// Cause: capture used to save GetSystemPrompt() (= base + all appends merged in)
// AND the appendix list separately; restore then SetSystemPrompt(merged) +
// re-AppendSystemPrompt each entry — so the appendix doubled per cycle and
// .agents/rules/mlp-system.md ballooned linearly with chat resumes. Fix is to
// strip the appendix from the saved SystemPrompt so the persisted base is
// pre-append. This test runs 5 capture→restore cycles and verifies the
// in-memory composite stays the same size as a single-cycle restore.
func TestCaptureChatHistoryAgentRuntimeDoesNotDuplicateAppendsAcrossRestores(t *testing.T) {
	api := &StreamingAPI{}
	workshopPrompt := "<workshop-template>workflow-builder system prompt</workshop-template>"
	capability := "<capability>browser available</capability>"
	secretBlock := "<secrets>SECRET_FOO=$SECRET_FOO</secrets>"

	agent := &mcpagent.Agent{
		CodingProviderSessionHandle: llmtypes.CodingProviderSessionHandle{
			Provider:        "agy-cli",
			NativeSessionID: "agy-conversation-roundtrip",
			ProjectDirID:    "Workflow/example",
			Model:           "agy-cli",
		},
	}
	agent.SetSystemPrompt(workshopPrompt)
	agent.AppendSystemPrompt(capability)
	agent.AppendSystemPrompt(secretBlock)
	baselineLen := len(agent.GetSystemPrompt())

	for i := 0; i < 5; i++ {
		runtime := api.captureChatHistoryAgentRuntime("agy-ui-session", "agy-cli", "agy-cli", "Workflow/example", agent)
		if runtime == nil {
			t.Fatalf("cycle %d: expected runtime", i)
		}
		// Stored SystemPrompt must be the base alone — no merged appendix.
		if strings.Contains(runtime.SystemPrompt, capability) || strings.Contains(runtime.SystemPrompt, secretBlock) {
			t.Fatalf("cycle %d: stored SystemPrompt must be base-only, got %q", i, runtime.SystemPrompt)
		}
		fresh := &mcpagent.Agent{}
		if !api.seedCodingAgentRuntimeFromRestoredConversation("agy-ui-session", "agy-cli", "", runtime, fresh) {
			t.Fatalf("cycle %d: expected restore to succeed", i)
		}
		if got := len(fresh.GetSystemPrompt()); got != baselineLen {
			t.Fatalf("cycle %d: composite prompt grew from %d to %d chars (appendix duplication regressed)", i, baselineLen, got)
		}
		agent = fresh
	}
}

// TestSeedCodingAgentRuntimeFromRestoredConversationSkipsEmptySystemPrompt
// guards against a runtime saved before the SystemPrompt field existed:
// a missing system prompt must NOT wipe the agent's existing prompt
// (which may have been set by a parallel /api/query path).
func TestSeedCodingAgentRuntimeFromRestoredConversationSkipsEmptySystemPrompt(t *testing.T) {
	api := &StreamingAPI{}
	agent := &mcpagent.Agent{}
	preExisting := "default agent prompt set elsewhere"
	agent.SetSystemPrompt(preExisting)
	runtime := &ChatHistoryAgentRuntime{
		Kind:              "coding_agent",
		Provider:          "agy-cli",
		ExternalSessionID: "agy-conversation-restored",
		ResumeSupported:   true,
		// SystemPrompt intentionally empty (legacy runtime)
	}

	if !api.seedCodingAgentRuntimeFromRestoredConversation("agy-ui-session", "agy-cli", "", runtime, agent) {
		t.Fatal("expected Agy resume state to be seeded")
	}

	if got := agent.GetSystemPrompt(); got != preExisting {
		t.Fatalf("agent.GetSystemPrompt() = %q, want %q unchanged — empty runtime SystemPrompt must NOT clobber a prompt set elsewhere", got, preExisting)
	}
}

func TestSeedCodingAgentRuntimeFromRestoredConversationAppliesAgentSessionHandle(t *testing.T) {
	api := &StreamingAPI{}
	agent := &mcpagent.Agent{}
	runtime := &ChatHistoryAgentRuntime{
		Kind:     "coding_agent",
		Provider: "codex-cli",
		AgentSessionHandle: &mcpagent.AgentSessionHandle{
			SessionID: "old-chat-session",
			Provider: llmtypes.CodingProviderSessionHandle{
				Provider:        "codex-cli",
				Transport:       llmtypes.CodingProviderTransportTmux,
				NativeSessionID: "codex-thread-from-handle",
				WorkingDir:      "/workspace",
				ProjectDirID:    "Workflow/example",
				Model:           "gpt-5.4",
				Status:          llmtypes.CodingProviderSessionStatusIdle,
			},
		},
	}

	if !api.seedCodingAgentRuntimeFromRestoredConversation("new-ui-session", "codex-cli", "", runtime, agent) {
		t.Fatal("expected typed handle to seed native resume state")
	}
	if agent.SessionID != "new-ui-session" {
		t.Fatalf("agent SessionID = %q, want current UI session owner", agent.SessionID)
	}
	if agent.CodexSessionID != "codex-thread-from-handle" {
		t.Fatalf("agent CodexSessionID = %q", agent.CodexSessionID)
	}
	if agent.CodingAgentWorkingDir != "/workspace" {
		t.Fatalf("agent CodingAgentWorkingDir = %q", agent.CodingAgentWorkingDir)
	}
}

func TestSeedCodingAgentRuntimeFromRestoredConversationRejectsProviderMismatch(t *testing.T) {
	api := &StreamingAPI{}
	agent := &mcpagent.Agent{}
	runtime := &ChatHistoryAgentRuntime{
		Kind:              "coding_agent",
		Provider:          "claude-code",
		ExternalSessionID: "claude-native-restored",
		ResumeSupported:   true,
	}

	if api.seedCodingAgentRuntimeFromRestoredConversation("new-ui-session", "gemini-cli", "", runtime, agent) {
		t.Fatal("expected provider mismatch to skip native resume")
	}
	if agent.ClaudeCodeSessionID != "" {
		t.Fatalf("agent ClaudeCodeSessionID = %q", agent.ClaudeCodeSessionID)
	}
}

func TestSeedCodingAgentRuntimeFromRestoredConversationRejectsWorkshopModeMismatch(t *testing.T) {
	api := &StreamingAPI{}
	agent := &mcpagent.Agent{}
	// After the workshop merge, "builder" and "optimizer" both normalize to
	// "workshop". The only real workshop-mode mismatch is workshop ↔ run.
	runtime := &ChatHistoryAgentRuntime{
		Kind:              "coding_agent",
		Provider:          "claude-code",
		ExternalSessionID: "claude-native-restored",
		ResumeSupported:   true,
		WorkshopMode:      "workshop",
	}

	if api.seedCodingAgentRuntimeFromRestoredConversation("new-ui-session", "claude-code", "run", runtime, agent) {
		t.Fatal("expected workshop mode mismatch to skip native resume")
	}
	if agent.ClaudeCodeSessionID != "" {
		t.Fatalf("agent ClaudeCodeSessionID = %q", agent.ClaudeCodeSessionID)
	}
}
