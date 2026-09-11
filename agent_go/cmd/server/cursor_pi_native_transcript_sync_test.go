package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func TestBuilderConversationMessagesFromLLMTypesKeepsTextTurnsOnly(t *testing.T) {
	got := builderConversationMessagesFromLLMTypes([]llmtypes.MessageContent{
		{Role: llmtypes.ChatMessageTypeSystem, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "system"}}},
		{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "  hello  "}}},
		{Role: llmtypes.ChatMessageTypeAI, Parts: []llmtypes.ContentPart{llmtypes.ToolCall{ID: "t1"}}},
		{Role: llmtypes.ChatMessageTypeAI, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "first"}, llmtypes.TextContent{Text: "second"}}},
		{Role: llmtypes.ChatMessageTypeTool, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "tool output"}}},
	})
	want := []builderConversationMessage{
		{Role: "human", Parts: []builderConversationPart{{Text: "hello"}}},
		{Role: "ai", Parts: []builderConversationPart{{Text: "first\n\nsecond"}}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if builderConversationMessageKey(got[i]) != builderConversationMessageKey(want[i]) {
			t.Fatalf("message %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Same failure as the Codex case, on pi: the record was last saved after
// one full turn, then the chat continued through retained live input.
func TestRefreshLatestBuilderConversationFromPiTranscript(t *testing.T) {
	sessionsDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", sessionsDir)
	const nativeSessionID = "mlp-pi-f47fd4180d3db2c1"
	dir := filepath.Join(sessionsDir, "--tmp-ws--")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"session","version":3,"id":"` + nativeSessionID + `","timestamp":"2026-09-02T05:40:37.091Z","cwd":"/tmp/ws"}`,
		`{"type":"message","timestamp":"2026-09-02T05:40:37.250Z","message":{"role":"user","content":[{"type":"text","text":"System: # Builder"}]}}`,
		`{"type":"message","timestamp":"2026-09-02T05:40:38.000Z","message":{"role":"user","content":[{"type":"text","text":"what is our top strategy"}]}}`,
		`{"type":"message","timestamp":"2026-09-02T05:40:41.000Z","message":{"role":"assistant","content":[{"type":"text","text":"The top strategy is the builder flywheel."}]}}`,
		`{"type":"message","timestamp":"2026-09-02T06:10:00.000Z","message":{"role":"user","content":[{"type":"text","text":"and how many posts"}]}}`,
		`{"type":"message","timestamp":"2026-09-02T06:10:05.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Twelve so far."}]}}`,
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-09-02T05-40-37-091Z_"+nativeSessionID+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	conv := builderConversationLog{
		SessionID: "builder-session",
		PhaseID:   "workflow-builder",
		UpdatedAt: "2026-09-02T11:10:41+05:30",
		ConversationHistory: []builderConversationMessage{
			{Role: "human", Parts: []builderConversationPart{{Text: "what is our top strategy"}}},
			{Role: "ai", Parts: []builderConversationPart{{Text: "The top strategy is the builder flywheel."}}},
		},
	}
	record := map[string]interface{}{
		"session_id": conv.SessionID,
		"phase_id":   conv.PhaseID,
		"updated_at": conv.UpdatedAt,
		"conversation_history": []map[string]interface{}{
			{"Role": "human", "Parts": []map[string]interface{}{{"Text": "what is our top strategy"}}},
			{"Role": "ai", "Parts": []map[string]interface{}{{"Text": "The top strategy is the builder flywheel."}}},
		},
		"runtime": map[string]interface{}{
			"provider":            "pi-cli",
			"external_session_id": nativeSessionID,
			"agent_session_handle": map[string]interface{}{
				"provider": map[string]interface{}{
					"provider": "pi-cli", "native_session_id": nativeSessionID, "working_dir": "/tmp/ws",
				},
			},
		},
	}
	rawContent, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}

	api := &StreamingAPI{}
	refreshed := api.refreshLatestBuilderConversationFromNativeTranscript(context.Background(), "Workflow/x/builder/conversation/2026-09-02/session-builder-session-conversation.json", string(rawContent), conv)

	if len(refreshed.ConversationHistory) != 4 {
		t.Fatalf("expected 4 messages after catch-up, got %d: %+v", len(refreshed.ConversationHistory), refreshed.ConversationHistory)
	}
	if last := refreshed.ConversationHistory[3]; last.Role != "ai" || last.Parts[0].Text != "Twelve so far." {
		t.Fatalf("expected pi's newest reply last, got %+v", last)
	}
	wantUpdatedAt, _ := time.Parse(time.RFC3339Nano, "2026-09-02T06:10:05.000Z")
	if got := parseBuilderConversationUpdatedAt(refreshed.UpdatedAt); !got.Equal(wantUpdatedAt) {
		t.Fatalf("updated_at = %v, want %v", got, wantUpdatedAt)
	}
}

// Same failure class as the Codex/Pi cases, on muse: the persisted record
// was last saved after one full turn, then the chat continued through
// retained live input muse's own transcript captured but the record never
// caught up to. Pins the fix for the gap found live 2026-09-11: muse had no
// case in nativeTranscriptMessagesForRuntime at all, so this resync path
// never ran for it.
func TestRefreshLatestBuilderConversationFromMuseTranscript(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	const nativeSessionID = "01a08ec6-9a6e-77c3-bcbb-ae96565a0f6b"
	dir := filepath.Join(dataHome, "muse", "sessions", "2026", "09", "11", nativeSessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"payload_type":"runtime.command_intake.received","payload":{"record":{"command_id":"c1","command":{"prompt":"what is our top strategy"}}}}`,
		`{"payload_type":"runtime.session","payload":{"kind":"run","run_id":"run-1","event":{"kind":"assistant_message_committed","text":"The top strategy is the builder flywheel."}}}`,
		`{"payload_type":"runtime.command_intake.received","payload":{"record":{"command_id":"c2","command":{"prompt":"and how many posts"}}}}`,
		`{"payload_type":"runtime.session","payload":{"kind":"run","run_id":"run-2","event":{"kind":"assistant_message_committed","text":"Twelve so far."}}}`,
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	conv := builderConversationLog{
		SessionID: "builder-session",
		PhaseID:   "workflow-builder",
		UpdatedAt: "2026-09-11T11:10:41+05:30",
		ConversationHistory: []builderConversationMessage{
			{Role: "human", Parts: []builderConversationPart{{Text: "what is our top strategy"}}},
			{Role: "ai", Parts: []builderConversationPart{{Text: "The top strategy is the builder flywheel."}}},
		},
	}
	record := map[string]interface{}{
		"session_id": conv.SessionID,
		"phase_id":   conv.PhaseID,
		"updated_at": conv.UpdatedAt,
		"conversation_history": []map[string]interface{}{
			{"Role": "human", "Parts": []map[string]interface{}{{"Text": "what is our top strategy"}}},
			{"Role": "ai", "Parts": []map[string]interface{}{{"Text": "The top strategy is the builder flywheel."}}},
		},
		"runtime": map[string]interface{}{
			"provider":            "muse-cli",
			"external_session_id": nativeSessionID,
			"agent_session_handle": map[string]interface{}{
				"provider": map[string]interface{}{
					"provider": "muse-cli", "native_session_id": nativeSessionID,
				},
			},
		},
	}
	rawContent, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}

	api := &StreamingAPI{}
	refreshed := api.refreshLatestBuilderConversationFromNativeTranscript(context.Background(), "Workflow/x/builder/conversation/2026-09-11/session-builder-session-conversation.json", string(rawContent), conv)

	if len(refreshed.ConversationHistory) != 4 {
		t.Fatalf("expected 4 messages after catch-up, got %d: %+v", len(refreshed.ConversationHistory), refreshed.ConversationHistory)
	}
	if last := refreshed.ConversationHistory[3]; last.Role != "ai" || last.Parts[0].Text != "Twelve so far." {
		t.Fatalf("expected muse's newest reply last, got %+v", last)
	}
}

func TestNativeTranscriptMessagesForRuntimeCursorNeedsWorkingDirAndSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tc := range []struct{ session, dir string }{{"", "/tmp/ws"}, {"agent-1", ""}, {"agent-1", "/tmp/ws"}} {
		_, _, _, ok, err := nativeTranscriptMessagesForRuntime("cursor-cli", tc.session, tc.dir)
		if ok || err != nil {
			t.Fatalf("cursor session=%q dir=%q: ok=%v err=%v, want false/nil (no store present)", tc.session, tc.dir, ok, err)
		}
	}
}
