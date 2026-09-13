package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMuseNativeTranscriptRepairsStaleTailAndMissingReplyOnRestore(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	const nativeID = "muse-stale-tail-regression"
	dir := filepath.Join(dataHome, "muse", "sessions", "2026", "09", "12", nativeID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"payload_type":"runtime.user_intent.accepted","payload":{"intent_id":"one","refill_blocks":[{"kind":"text","text":"check xspaces"}]}}`,
		`{"payload_type":"runtime.session","payload":{"kind":"run","event":{"kind":"assistant_message_committed","text":"xspaces verified"}}}`,
		`{"payload_type":"runtime.user_intent.accepted","payload":{"intent_id":"two","refill_blocks":[{"kind":"text","text":"run full schedule"}]}}`,
		`{"payload_type":"runtime.session","payload":{"kind":"run","event":{"kind":"assistant_message_committed","text":"Fresh extraction is missing."}}}`,
		`{"payload_type":"runtime.user_intent.accepted","payload":{"intent_id":"three","refill_blocks":[{"kind":"text","text":"one at a time"}]}}`,
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const path = "Workflow/regression/builder/conversation/2026-09-12/session-regression-conversation.json"
	for _, withToolMetadata := range []bool{false, true} {
		msg := func(role, text string) map[string]interface{} {
			return map[string]interface{}{"Role": role, "Parts": []map[string]interface{}{{"Text": text}}}
		}
		history := []map[string]interface{}{msg("human", "check xspaces"), msg("ai", "xspaces verified"), msg("human", "run full schedule"), msg("human", "one at a time"), msg("human", "check xspaces"), msg("ai", "xspaces verified")}
		if withToolMetadata {
			history[5]["Parts"].([]map[string]interface{})[0]["ToolCall"] = map[string]string{"ID": "preserve-me"}
		}
		record := map[string]interface{}{"session_id": "regression", "conversation_history": history, "runtime": map[string]string{"provider": "muse-cli", "external_session_id": nativeID}}
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		var conv builderConversationLog
		if err := json.Unmarshal(raw, &conv); err != nil {
			t.Fatal(err)
		}
		workspace := &mockWorkspaceAPI{files: map[string]string{path: string(raw)}}
		server := httptest.NewServer(workspace)
		t.Setenv("WORKSPACE_API_URL", server.URL)
		api := &StreamingAPI{}
		got := api.refreshLatestBuilderConversationFromNativeTranscript(context.Background(), path, string(raw), conv)
		expected := 5
		if withToolMetadata {
			expected = 7
		}
		if len(got.ConversationHistory) != expected {
			t.Fatalf("history length %d, want %d", len(got.ConversationHistory), expected)
		}
		if got.ConversationHistory[3].Parts[0].Text != "Fresh extraction is missing." {
			t.Fatal("reply missing or out of order")
		}
		saved := workspace.files[path]
		if withToolMetadata && !strings.Contains(saved, "preserve-me") {
			t.Fatal("structured metadata removed")
		}
		again := api.refreshLatestBuilderConversationFromNativeTranscript(context.Background(), path, saved, got)
		if !reflect.DeepEqual(again.ConversationHistory, got.ConversationHistory) {
			t.Fatal("second restore changed history")
		}
		server.Close()
	}
}

func TestNativeTranscriptStaleTailRepairPreservesRealRepeats(t *testing.T) {
	msg := func(role, text string) builderConversationMessage {
		return builderConversationMessage{Role: role, Parts: []builderConversationPart{{Text: text}}}
	}
	old := []builderConversationMessage{msg("human", "check xspaces"), msg("ai", "xspaces verified")}
	native := append(append([]builderConversationMessage{}, old...), msg("human", "run full schedule"), msg("ai", "Fresh extraction is missing."), msg("human", "one at a time"))
	stale := append(append([]builderConversationMessage{}, native...), old...)
	if end := trimNativeTranscriptStaleTail(stale, native); end != len(native) {
		t.Fatalf("replayed tail retained: %d", end)
	}
	if end := trimNativeTranscriptStaleTail(stale, stale); end != len(stale) {
		t.Fatal("real native repeated exchange removed")
	}
	unanswered := append(append([]builderConversationMessage{}, native...), old[0])
	if end := trimNativeTranscriptStaleTail(unanswered, native); end != len(unanswered) {
		t.Fatal("unanswered retry removed")
	}
	newPair := append(append([]builderConversationMessage{}, native...), old[0], msg("ai", "new result"))
	if end := trimNativeTranscriptStaleTail(newPair, native); end != len(newPair) {
		t.Fatal("new exchange removed")
	}
	if end := trimNativeTranscriptStaleTail(native, native); end != len(native) {
		t.Fatal("clean history changed")
	}
}
