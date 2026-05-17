package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	internalevents "mcp-agent-builder-go/agent_go/internal/events"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

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

func TestCaptureChatHistoryAgentRuntimeStoresCLIResumeID(t *testing.T) {
	api := &StreamingAPI{
		claudeCodeSessionIDs: make(map[string]string),
		geminiSessionIDs:     make(map[string]string),
		geminiProjectDirIDs:  make(map[string]string),
	}

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
	if got := api.claudeCodeSessionIDs["session-1"]; got != "claude-session-1" {
		t.Fatalf("expected resume ID stored in memory, got %q", got)
	}
}
