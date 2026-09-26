package server

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMigrationFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMigrationFixture(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMigrateCrewRootMovesCrewsAndRewritesReferences(t *testing.T) {
	docs := t.TempDir()
	docs, _ = filepath.EvalSymlinks(docs)
	state := t.TempDir()
	home := t.TempDir()
	oldRoot := "_users/alice/Chats/Work/projects/sde-1a2b3c4d"
	oldAbs := filepath.Join(docs, filepath.FromSlash(oldRoot))
	newAbs := filepath.Join(docs, "Crew", "sde-1a2b3c4d")

	// Alice's crew, with its transcript holding every spelling.
	writeMigrationFixture(t, docs, oldRoot+"/product.json", `{"schema_version":1,"product":"work","id":"11111111-aaaa","title":"SDE","session_id":"work:project:11111111-aaaa"}`)
	writeMigrationFixture(t, docs, oldRoot+"/workflow.json", `{"id":"11111111-aaaa","workflow_context_paths":["Workflow/ops"]}`)
	writeMigrationFixture(t, docs, oldRoot+"/code/main.py", "print('Chats/Work/projects/sde-1a2b3c4d stays: user code')\n")
	writeMigrationFixture(t, docs, oldRoot+"/builder/conversation/2026-09-26/session-x-conversation.json",
		`{"workspace_path":"Chats/Work/projects/sde-1a2b3c4d","runtime":{"workspace_path":"`+oldRoot+`","agent_session_handle":{"provider":{"working_dir":"`+oldAbs+`","project_dir_id":"`+oldAbs+`"}}}}`)
	// A second crew whose folder name extends the first: must not be touched by it.
	writeMigrationFixture(t, docs, "_users/alice/Chats/Work/projects/sde-1a2b3c4d9/product.json", `{"product":"work","id":"22222222-bbbb","title":"Other"}`)
	// Bob has a crew with the same folder name: a conflict, left in place.
	writeMigrationFixture(t, docs, "_users/bob/Chats/Work/projects/sde-1a2b3c4d/product.json", `{"product":"work","id":"33333333-cccc","title":"Bob's"}`)
	// A non-crew product project is not a crew.
	writeMigrationFixture(t, docs, "_users/alice/Chats/Video Studio/projects/v1/product.json", `{"product":"video-studio","id":"v1"}`)

	// Stored references.
	writeMigrationFixture(t, docs, "_users/alice/chat_history/product-conversations.json", `{"entries":[{"workspace_path":"`+oldRoot+`"}]}`)
	writeMigrationFixture(t, docs, "Workflow/ops/workflow.json", `{"crew_attachments":[{"crew_workspace_path":"`+oldRoot+`"}],"workflow_context_paths":["Chats/Work/projects/sde-1a2b3c4d","Chats/Work/projects/sde-1a2b3c4d9"]}`)
	writeMigrationFixture(t, docs, "config/slack-config.json", `{"connections":[{"workspace_path":"`+oldRoot+`","profile_id":"work"}]}`)
	costDB := filepath.Join(docs, "_system", "costs.sqlite")
	if err := os.MkdirAll(filepath.Dir(costDB), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", costDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE cost_events(id INTEGER PRIMARY KEY, workflow_id TEXT, tokens INTEGER);
		INSERT INTO cost_events(workflow_id, tokens) VALUES ('Chats/Work/projects/sde-1a2b3c4d', 1), (?, 2), ('Workflow/ops', 3)`, oldRoot+"/code"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Coding-CLI stores keyed by the old working directory.
	writeMigrationFixture(t, home, ".claude/projects/"+claudeProjectDirName(oldAbs)+"/sid-1.jsonl", `{"cwd":"`+oldAbs+`","type":"user"}`+"\n")
	writeMigrationFixture(t, home, ".cursor/chats/"+cursorChatsDirName(oldAbs)+"/agent-1/store.db", "binary-store")
	writeMigrationFixture(t, home, ".cursor/projects/"+cursorProjectDirName(oldAbs)+"/agent-transcripts/c1/c1.jsonl", `{"cwd":"`+oldAbs+`"}`+"\n")

	// Dry run: plan only, nothing changes.
	plan, err := migrateCrewRoot(crewRootMigrationOptions{DocsRoot: docs, StateRoot: state, CLIHomes: []string{home}})
	if err != nil || len(plan.Moves) != 2 || len(plan.Conflicts) != 1 {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	if _, err := os.Stat(oldAbs); err != nil {
		t.Fatal("dry run moved a crew")
	}

	report, err := migrateCrewRoot(crewRootMigrationOptions{DocsRoot: docs, StateRoot: state, Apply: true, CLIHomes: []string{home}})
	if err != nil {
		t.Fatalf("migrate: %v (%+v)", err, report)
	}
	if len(report.Moves) != 2 || len(report.Conflicts) != 1 || !strings.Contains(report.Conflicts[0], "_users/bob/") {
		t.Fatalf("report: %+v", report)
	}
	if _, err := os.Stat(oldAbs); !os.IsNotExist(err) {
		t.Fatal("old crew folder still present")
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(readMigrationFixture(t, docs, "Crew/sde-1a2b3c4d/product.json")), &manifest); err != nil || manifest["owner_id"] != "alice" {
		t.Fatalf("owner_id not recorded: %v %v", manifest, err)
	}
	if _, err := os.Stat(filepath.Join(docs, "_users/bob/Chats/Work/projects/sde-1a2b3c4d/product.json")); err != nil {
		t.Fatal("conflicting crew was moved")
	}

	transcript := readMigrationFixture(t, docs, "Crew/sde-1a2b3c4d/builder/conversation/2026-09-26/session-x-conversation.json")
	if strings.Contains(transcript, "Chats/Work/projects") || !strings.Contains(transcript, `"working_dir":"`+newAbs+`"`) || !strings.Contains(transcript, `"workspace_path":"Crew/sde-1a2b3c4d"`) {
		t.Fatalf("transcript not rewritten: %s", transcript)
	}
	if got := readMigrationFixture(t, docs, "Crew/sde-1a2b3c4d/code/main.py"); !strings.Contains(got, "Chats/Work/projects/sde-1a2b3c4d") {
		t.Fatal("user code was rewritten")
	}
	workflow := readMigrationFixture(t, docs, "Workflow/ops/workflow.json")
	if !strings.Contains(workflow, `"crew_workspace_path":"Crew/sde-1a2b3c4d"`) || !strings.Contains(workflow, `"Crew/sde-1a2b3c4d","Crew/sde-1a2b3c4d9"`) {
		t.Fatalf("workflow references: %s", workflow)
	}
	for _, rel := range []string{"_users/alice/chat_history/product-conversations.json", "config/slack-config.json"} {
		if got := readMigrationFixture(t, docs, rel); strings.Contains(got, oldRoot) || !strings.Contains(got, "Crew/sde-1a2b3c4d") {
			t.Fatalf("%s not rewritten: %s", rel, got)
		}
	}
	db, _ = sql.Open("sqlite", costDB)
	rows, err := db.Query(`SELECT workflow_id FROM cost_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	db.Close()
	if strings.Join(ids, ",") != "Crew/sde-1a2b3c4d,Crew/sde-1a2b3c4d/code,Workflow/ops" {
		t.Fatalf("cost ledger: %v", ids)
	}

	aliases := readMigrationFixture(t, docs, crewPathAliasFile)
	if !strings.Contains(aliases, `"`+oldRoot+`": "Crew/sde-1a2b3c4d"`) {
		t.Fatalf("aliases: %s", aliases)
	}
	copied := readMigrationFixture(t, home, ".claude/projects/"+claudeProjectDirName(newAbs)+"/sid-1.jsonl")
	if !strings.Contains(copied, newAbs) {
		t.Fatalf("claude session not carried over: %s", copied)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude/projects", claudeProjectDirName(oldAbs), "sid-1.jsonl")); err != nil {
		t.Fatal("original claude session removed; it is the rollback copy")
	}
	if got := readMigrationFixture(t, home, ".cursor/chats/"+cursorChatsDirName(newAbs)+"/agent-1/store.db"); got != "binary-store" {
		t.Fatalf("cursor store: %q", got)
	}
	if got := readMigrationFixture(t, home, ".cursor/projects/"+cursorProjectDirName(newAbs)+"/agent-transcripts/c1/c1.jsonl"); !strings.Contains(got, newAbs) {
		t.Fatalf("cursor project transcripts not carried over: %q", got)
	}

	again, err := migrateCrewRoot(crewRootMigrationOptions{DocsRoot: docs, StateRoot: state, Apply: true, CLIHomes: []string{home}})
	if err != nil || !again.AlreadyDone {
		t.Fatalf("second run not a no-op: %+v %v", again, err)
	}
}

// An interrupted pass (crew moved, references not yet rewritten) is finished
// by the next run, driven by the alias file.
func TestMigrateCrewRootResumesInterruptedPass(t *testing.T) {
	docs := t.TempDir()
	docs, _ = filepath.EvalSymlinks(docs)
	state := t.TempDir()
	oldRoot := "_users/alice/Chats/Work/projects/ops-9f8e7d6c"
	writeMigrationFixture(t, docs, "Crew/ops-9f8e7d6c/product.json", `{"product":"work","id":"x","owner_id":"alice"}`)
	writeMigrationFixture(t, docs, crewPathAliasFile, `{"aliases":{"`+oldRoot+`":"Crew/ops-9f8e7d6c"}}`)
	writeMigrationFixture(t, docs, "Workflow/w/workflow.json", `{"workflow_context_paths":["`+oldRoot+`"]}`)
	report, err := migrateCrewRoot(crewRootMigrationOptions{DocsRoot: docs, StateRoot: state, Apply: true, CLIHomes: []string{t.TempDir()}})
	if err != nil || len(report.Moves) != 0 {
		t.Fatalf("%+v %v", report, err)
	}
	if got := readMigrationFixture(t, docs, "Workflow/w/workflow.json"); !strings.Contains(got, `"Crew/ops-9f8e7d6c"`) {
		t.Fatalf("interrupted pass not finished: %s", got)
	}
}

// Cursor's per-project store naming, as observed on RTS.
func TestCursorProjectDirName(t *testing.T) {
	got := cursorProjectDirName("/data/video-studio/docs/_users/aa73da63e26b40a1bb701c2b4c024870/Chats/Work/projects/rts-flow-tester-5090fe7e")
	if got != "data-video-studio-docs-users-aa73da63e26b40a1bb701c2b4c024870-Chats-Work-projects-rts-flow-tester-5090fe7e" {
		t.Fatalf("cursor project dir %q", got)
	}
}
