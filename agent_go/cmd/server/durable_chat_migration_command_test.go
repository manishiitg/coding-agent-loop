package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
)

func TestMigrateDurableChatsFromDiskUsesNewestConversationAndSkipsExecutions(t *testing.T) {
	docsRoot := t.TempDir()
	stateRoot := t.TempDir()
	older := filepath.Join(docsRoot, "Workflow", "demo", "builder", "2026-01-01")
	newer := filepath.Join(docsRoot, "Workflow", "demo", "builder", "2026-01-02")
	if err := os.MkdirAll(older, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newer, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(older, "session-chat-1-conversation.json"), `{"session_id":"chat-1","updated_at":"2026-01-01T00:00:00Z","conversation_history":[{"Role":"human","Parts":[{"Text":"old"}]}]}`)
	write(filepath.Join(newer, "session-chat-1-conversation.json"), `{"session_id":"chat-1","updated_at":"2026-01-02T00:00:00Z","conversation_history":[{"Role":"human","Parts":[{"Text":"new"}]},{"Role":"ai","Parts":[{"Text":"answer"}]}]}`)
	write(filepath.Join(newer, "session-schedule-run-conversation.json"), `{"session_id":"schedule-run","updated_at":"2026-01-02T00:00:00Z","conversation_history":[{"Role":"human","Parts":[{"Text":"execution"}]}]}`)

	if err := migrateDurableChatsFromDisk(docsRoot, stateRoot, false); err != nil {
		t.Fatal(err)
	}
	journal, err := internalevents.OpenSQLiteEventJournal(filepath.Join(stateRoot, "structured-chat-events-v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	page, err := journal.ReadPage("chat-1", internalevents.DurableEventPageOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 {
		t.Fatalf("migrated events = %d, want 2", len(page.Events))
	}
	encoded, err := json.Marshal(page.Events[0])
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Data struct {
			Data map[string]interface{} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Data["content"] != "new" {
		t.Fatalf("migrated user payload = %s", encoded)
	}
	if scheduled, err := journal.ReadPage("schedule-run", internalevents.DurableEventPageOptions{Limit: 10}); err != nil || scheduled.Exists {
		t.Fatalf("scheduled execution was migrated: %+v err=%v", scheduled, err)
	}
	if err := migrateDurableChatsFromDisk(docsRoot, stateRoot, false); err != nil {
		t.Fatalf("marker-backed rerun failed: %v", err)
	}
}
