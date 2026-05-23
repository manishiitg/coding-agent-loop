package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internalevents "mcp-agent-builder-go/agent_go/internal/events"
	"mcp-agent-builder-go/agent_go/pkg/workspace"

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

	if !api.seedCodingAgentRuntimeFromCurrentConversation("existing-chat", "default", "claude-code", "builder", "Workflow/rtslatency", agent) {
		t.Fatal("expected current conversation runtime to seed native resume state")
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
				Transport:       llmtypes.CodingProviderTransportStructured,
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
