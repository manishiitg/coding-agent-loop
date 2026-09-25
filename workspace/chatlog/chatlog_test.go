package chatlog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func row(role, text string) string {
	return `{"Role":"` + role + `","Parts":[{"Text":"` + text + `"}]}`
}

func record(rows ...string) []byte {
	return []byte(`{"session_id":"s","user_id":"u","runtime":{"provider":"cursor-cli"},"conversation_history":[` + strings.Join(rows, ",") + `],"revision":3}`)
}

type loaded struct {
	SessionID string            `json:"session_id"`
	Runtime   map[string]string `json:"runtime"`
	Revision  int               `json:"revision"`
	History   []json.RawMessage `json:"conversation_history"`
	Total     int               `json:"history_total"`
	Tail      bool              `json:"history_tail"`
}

func loadRecord(t *testing.T, path string) loaded {
	t.Helper()
	raw, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var out loaded
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return out
}

func texts(history []json.RawMessage) string {
	var out []string
	for _, raw := range history {
		var m struct {
			Parts []struct{ Text string }
		}
		_ = json.Unmarshal(raw, &m)
		out = append(out, m.Parts[0].Text)
	}
	return strings.Join(out, ",")
}

func lineCount(t *testing.T, path string) int {
	raw, _ := os.ReadFile(HistoryLogPath(path))
	return strings.Count(string(raw), "\n")
}

func TestLegacyRecordIsReadUnchangedAndConvertedOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "session-s-conversation.json")
	legacy := record(row("system", "p1"), row("human", "hi"), row("ai", "hello"))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	if raw, _ := Load(path); string(raw) != string(legacy) {
		t.Fatal("legacy record must be returned unchanged")
	}
	if err := Save(path, legacy); err != nil {
		t.Fatal(err)
	}
	got := loadRecord(t, path)
	if texts(got.History) != "p1,hi,hello" || got.Runtime["provider"] != "cursor-cli" || got.Revision != 3 {
		t.Fatalf("converted record = %+v", got)
	}
	header, _ := os.ReadFile(path)
	if !strings.Contains(string(header), `"history_log":"jsonl-v1"`) || !strings.Contains(string(header), `"history_tail":true`) {
		t.Fatalf("header must mark its inline history as a tail: %s", header)
	}
	if lineCount(t, path) != 2 {
		t.Fatalf("log should hold 2 non-system rows, has %d", lineCount(t, path))
	}
}

func TestSaveAppendsWhenHistoryExtendsAndRewritesOtherwise(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-s-conversation.json")
	if err := Save(path, record(row("system", "p1"), row("human", "a"), row("ai", "b"))); err != nil {
		t.Fatal(err)
	}
	logBefore, _ := os.Stat(HistoryLogPath(path))
	// Extension with a regenerated system prompt: append only.
	if err := Save(path, record(row("system", "p2"), row("human", "a"), row("ai", "b"), row("human", "c"))); err != nil {
		t.Fatal(err)
	}
	logAfter, _ := os.Stat(HistoryLogPath(path))
	if !os.SameFile(logBefore, logAfter) {
		t.Fatal("an extension must append to the log, not replace it")
	}
	if got := texts(loadRecord(t, path).History); got != "p2,a,b,c" {
		t.Fatalf("after append = %s", got)
	}
	// An edited earlier row: full rewrite, still correct.
	if err := Save(path, record(row("system", "p2"), row("human", "A"), row("ai", "b"))); err != nil {
		t.Fatal(err)
	}
	if got := texts(loadRecord(t, path).History); got != "p2,A,b" {
		t.Fatalf("after rewrite = %s", got)
	}
}

func TestAppendAddsMessagesAndPatchesHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-s-conversation.json")
	if err := os.WriteFile(path, record(row("human", "a")), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Append(path, []json.RawMessage{json.RawMessage(row("ai", "b")), json.RawMessage(row("system", "fresh"))}, map[string]json.RawMessage{"revision": json.RawMessage("4")})
	if err != nil {
		t.Fatal(err)
	}
	got := loadRecord(t, path)
	if texts(got.History) != "fresh,a,b" || got.Revision != 4 {
		t.Fatalf("after append = %s rev %d", texts(got.History), got.Revision)
	}
}

func TestTailIsPartialAndCannotBeSavedBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-s-conversation.json")
	if err := Save(path, record(row("system", "p"), row("human", "1"), row("ai", "2"), row("human", "3"))); err != nil {
		t.Fatal(err)
	}
	raw, err := LoadTail(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	var tail loaded
	_ = json.Unmarshal(raw, &tail)
	if texts(tail.History) != "p,2,3" || tail.Total != 3 || !tail.Tail {
		t.Fatalf("tail = %s total %d flag %v", texts(tail.History), tail.Total, tail.Tail)
	}
	if err := Save(path, raw); !errors.Is(err, ErrPartialRecord) {
		t.Fatalf("saving a tail must be refused, got %v", err)
	}
	if got := texts(loadRecord(t, path).History); got != "p,1,2,3" {
		t.Fatalf("history changed after refused save: %s", got)
	}
}

func TestInterruptedAppendIsIgnoredThenRepaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-s-conversation.json")
	if err := Save(path, record(row("human", "a"))); err != nil {
		t.Fatal(err)
	}
	// A crash mid-append: a committed-but-uncounted line and a partial line.
	file, _ := os.OpenFile(HistoryLogPath(path), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = file.WriteString(row("ai", "ghost") + "\n" + `{"Role":"ai","Pa`)
	file.Close()
	if got := texts(loadRecord(t, path).History); got != "a" {
		t.Fatalf("uncommitted rows must not be read: %s", got)
	}
	if err := Append(path, []json.RawMessage{json.RawMessage(row("ai", "b"))}, nil); err != nil {
		t.Fatal(err)
	}
	if got := texts(loadRecord(t, path).History); got != "a,b" {
		t.Fatalf("after repair = %s", got)
	}
	if err := Save(path, record(row("human", "a"), row("ai", "b"), row("human", "c"))); err != nil {
		t.Fatal(err)
	}
	if got := texts(loadRecord(t, path).History); got != "a,b,c" {
		t.Fatalf("after save = %s", got)
	}
}

func TestRemoveAndMoveHandleBothFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session-s-conversation.json")
	if err := Save(path, record(row("human", "a"))); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(dir, "other", "session-s-conversation.json")
	if err := Move(path, moved); err != nil {
		t.Fatal(err)
	}
	if got := texts(loadRecord(t, moved).History); got != "a" {
		t.Fatalf("moved = %s", got)
	}
	if err := Remove(moved); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(HistoryLogPath(moved)); !os.IsNotExist(err) {
		t.Fatal("log left behind")
	}
}

func TestNonRecordContentIsWrittenAsIs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-s-conversation.json")
	if err := Save(path, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "not json" {
		t.Fatal("content without a history must be stored unchanged")
	}
}

func TestHeaderKeepsOnlyARecentTailForDirectReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "builder", "conversation", "session-s-conversation.json")
	var rows []string
	for i := 0; i < 100; i++ {
		rows = append(rows, row("human", strings.Repeat("x", 10)+string(rune('a'+i%26))))
	}
	if err := Save(path, record(rows...)); err != nil {
		t.Fatal(err)
	}
	// A direct reader (json.load of the file) sees a bounded recent tail.
	raw, _ := os.ReadFile(path)
	var direct loaded
	if err := json.Unmarshal(raw, &direct); err != nil {
		t.Fatal(err)
	}
	if len(direct.History) != headerTailRows || !direct.Tail || direct.Total != 100 {
		t.Fatalf("header tail = %d rows, tail %v, total %d", len(direct.History), direct.Tail, direct.Total)
	}
	// Saving that directly-read header back is refused.
	if err := Save(path, raw); !errors.Is(err, ErrPartialRecord) {
		t.Fatalf("saving a header must be refused, got %v", err)
	}
	if got := loadRecord(t, path); len(got.History) != 100 || got.Tail {
		t.Fatalf("full load = %d rows, tail %v", len(got.History), got.Tail)
	}
	if !IsConversationPath(path) || IsConversationPath("/docs/Workflow/w/runs/1/logs/s/execution/execution-attempt-1-conversation.json") {
		t.Fatal("only chat conversations use the log format")
	}
}
