package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatHistoryDedupeRemovesCopiesKeepsRealRepeats(t *testing.T) {
	docs, state := t.TempDir(), t.TempDir()
	rel := "Workflow/wf/builder/conversation/users/u/2026-09-18/session-s-conversation.json"
	path := filepath.Join(docs, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	text := func(role, s string) string {
		return `{"Role":"` + role + `","Parts":[{"Text":"` + s + `"}]}`
	}
	call := `{"Role":"ai","Parts":[{"ID":"toolu_1","Type":"function","FunctionCall":{"Name":"read","Arguments":"{}"}}]}`
	callWithText := `{"Role":"ai","Parts":[{"Text":"Reading it now."},{"ID":"toolu_1","Type":"function","FunctionCall":{"Name":"read","Arguments":"{}"}}]}`
	result := `{"Role":"tool","Parts":[{"ToolCallID":"toolu_1","Name":"read","Content":"file body"}]}`
	// The real conversation: "check" is sent twice after different replies.
	real := []string{text("human", "check"), text("ai", "one"), call, result, text("ai", "two"), text("human", "check"), text("ai", "three")}
	// Two re-appended copies, the second with the transcript's fuller call.
	copy2 := append([]string{}, real...)
	copy2[2] = callWithText
	rows := []string{text("system", "prompt 1"), text("system", "prompt 2")}
	rows = append(rows, real...)
	rows = append(rows, real...)
	rows = append(rows, copy2...)
	pad := strings.Repeat("x", chatDedupeMinBytes)
	record := `{"session_id":"s","user_id":"u","revision":7,"conversation_history":[` + strings.Join(rows, ",") + `],"runtime":{"provider":"cursor-cli"},"padding":"` + pad + `"}`
	if err := os.WriteFile(path, []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}

	dry, err := dedupeChatHistories(docs, state, false, true)
	if err != nil || dry.Rewritten != 0 || dry.RowsBefore != len(rows) {
		t.Fatalf("dry run = %+v, %v", dry, err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != record {
		t.Fatal("dry run changed the file")
	}

	stats, err := dedupeChatHistories(docs, state, false, false)
	if err != nil || stats.Rewritten != 1 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	var got struct {
		Revision int                      `json:"revision"`
		Runtime  map[string]interface{}   `json:"runtime"`
		History  []map[string]interface{} `json:"conversation_history"`
	}
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, row := range got.History {
		encoded, _ := json.Marshal(row)
		texts = append(texts, string(encoded))
	}
	joined := strings.Join(texts, "\n")
	// Newest system prompt, then the real conversation once, with the fuller call.
	if len(got.History) != 1+len(real) {
		t.Fatalf("got %d rows, want %d:\n%s", len(got.History), 1+len(real), joined)
	}
	if !strings.Contains(texts[0], "prompt 2") || strings.Count(joined, "prompt") != 1 {
		t.Fatalf("want only the newest system prompt first:\n%s", joined)
	}
	if strings.Count(joined, `"check"`) != 2 {
		t.Fatalf("a message really sent twice must survive twice:\n%s", joined)
	}
	if !strings.Contains(texts[3], "Reading it now.") || strings.Count(joined, "toolu_1") != 2 {
		t.Fatalf("the call must keep its fullest version once, in place:\n%s", joined)
	}
	if got.Revision != 8 || got.Runtime["provider"] != "cursor-cli" {
		t.Fatalf("other fields not preserved: revision=%d runtime=%v", got.Revision, got.Runtime)
	}
	if backup, err := os.ReadFile(filepath.Join(state, "migrations", "chat-history-dedupe-v1-backup", rel)); err != nil || string(backup) != record {
		t.Fatalf("original not backed up: %v", err)
	}
	// One-time: the marker stops a second run; a forced rerun changes nothing.
	if again, _ := dedupeChatHistories(docs, state, false, false); again.Scanned != 0 {
		t.Fatalf("marker ignored: %+v", again)
	}
	if forced, _ := dedupeChatHistories(docs, state, true, false); forced.Rewritten != 0 {
		t.Fatalf("a deduped file must be stable: %+v", forced)
	}
}
