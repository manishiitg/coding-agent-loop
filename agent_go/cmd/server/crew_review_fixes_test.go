package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stubCrewOwnerRegistry(t *testing.T, initial map[string]string) *map[string]string {
	t.Helper()
	state := map[string]string{}
	for k, v := range initial {
		state[k] = v
	}
	prevRead, prevWrite := crewOwnerRegistry.read, crewOwnerRegistry.write
	crewOwnerRegistry.read = func(context.Context) (string, bool, error) {
		encoded, _ := json.Marshal(map[string]interface{}{"owners": state})
		return string(encoded), true, nil
	}
	crewOwnerRegistry.write = func(_ context.Context, content string) error {
		var file struct {
			Owners map[string]string `json:"owners"`
		}
		if err := json.Unmarshal([]byte(content), &file); err != nil {
			return err
		}
		state = file.Owners
		return nil
	}
	reset := func() {
		crewOwnerRegistry.mu.Lock()
		crewOwnerRegistry.owners, crewOwnerRegistry.loaded = nil, crewOwnerRegistry.loaded.AddDate(-1, 0, 0)
		crewOwnerRegistry.mu.Unlock()
	}
	reset()
	t.Cleanup(func() { crewOwnerRegistry.read, crewOwnerRegistry.write = prevRead, prevWrite; reset() })
	return &state
}

// An owner_id is trusted once: after the registry records it, editing
// product.json (by the owner, their agent, or another crew) changes nothing.
func TestCrewOwnerRegistryPinsFirstOwner(t *testing.T) {
	state := stubCrewOwnerRegistry(t, map[string]string{"Crew/pinned-1": "alice"})
	if got := crewOwnerRegistry.claim(context.Background(), "Crew/pinned-1", "mallory"); got != "alice" {
		t.Fatalf("a later claim replaced the pinned owner: %q", got)
	}
	if got := crewOwnerRegistry.claim(context.Background(), "Crew/new-2", "bob"); got != "bob" || (*state)["Crew/new-2"] != "bob" {
		t.Fatalf("first claim not recorded: %q %v", got, *state)
	}
	if owner, ok := crewOwnerRegistry.owner(context.Background(), "Crew/pinned-1"); !ok || owner != "alice" {
		t.Fatalf("registry owner %q %v", owner, ok)
	}
}

// An empty or invalid owner_id is nobody, never the "default" account.
func TestCrewManifestOwnerRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"", "  ", "!!", "a/b", strings.Repeat("x", 200)} {
		if got := crewManifestOwner(raw); got != "" {
			t.Fatalf("%q -> %q", raw, got)
		}
	}
	if got := crewManifestOwner(" alice "); got != "alice" {
		t.Fatalf("valid owner: %q", got)
	}
}

// Another owner's attached crew: its manifests are write-blocked; this
// crew's own are not (identity/selections go through server tools).
func TestForeignCrewManifestsAreWriteBlocked(t *testing.T) {
	stubCrewLookups(t, map[string]string{"Crew/own-1": "alice", "Crew/other-2": "bob"}, nil)
	blocked := foreignCrewManifestWriteBlockedPaths("Crew/own-1", []string{"Crew/own-1", "Crew/other-2", "Workflow/x"})
	got := strings.Join(blocked, ",")
	if got != "Crew/other-2/product.json,Crew/other-2/workflow.json" {
		t.Fatalf("blocked: %v", blocked)
	}
}

// Slack trigger input lands where the agent is told to read it.
func TestSlackTriggerFilePathForSharedCrew(t *testing.T) {
	stubCrewLookups(t, map[string]string{"Crew/sde-1": "alice"}, nil)
	if got := slackTriggerFilePath("alice", "Crew/sde-1/slack-inputs/d1.json"); got != "Crew/sde-1/slack-inputs/d1.json" {
		t.Fatalf("crew input path %q", got)
	}
	if got := slackTriggerFilePath("alice", "Chats/SparkQuill/activities/a/slack-inputs/d1.json"); got != "_users/alice/Chats/SparkQuill/activities/a/slack-inputs/d1.json" {
		t.Fatalf("per-user product input path %q", got)
	}
}

// A crew the migration could not move (name conflict) still binds from the
// owner's tree.
func TestCrewBindingFallsBackToLegacyRoot(t *testing.T) {
	stubCrewLookups(t, sharedCrewOwners(), nil)
	store := sharedCrewStore()
	profile := sharedCrewProfile()
	binding, err := resolveProductProjectBindingWithStore(context.Background(), "carol", profile, "crew-d", store)
	if err != nil || binding.WorkspacePath != "_users/carol/Chats/Work/projects/old-dddd4444" {
		t.Fatalf("legacy fallback: %+v %v", binding, err)
	}
}

// The SQLite rewrite touches only platform stores, and in WhatsApp's
// session.db only its routing table.
func TestCrewRootSQLiteRewriteIsAllowlisted(t *testing.T) {
	docs := t.TempDir()
	mk := func(rel, ddl string, rows ...string) string {
		path := filepath.Join(docs, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if _, err := db.Exec(row); err != nil {
				t.Fatal(err)
			}
		}
		return path
	}
	old := "_users/alice/Chats/Work/projects/sde-1"
	session := mk("config/whatsapp-sessions/alice/session.db",
		`CREATE TABLE whatsapp_meta(k TEXT, v TEXT); CREATE TABLE whatsmeow_device(jid TEXT, note TEXT)`,
		`INSERT INTO whatsapp_meta VALUES ('routes', '{"w":"`+old+`"}')`, `INSERT INTO whatsmeow_device VALUES ('x', '`+old+`')`)
	other := mk("_system/unrelated.sqlite", `CREATE TABLE t(v TEXT)`, `INSERT INTO t VALUES ('`+old+`')`)
	reps := crewRootReplacements(docs, []crewRootMove{{Owner: "alice", Dir: "sde-1", From: old, To: "Crew/sde-1"}})
	if _, warnings := rewriteCrewRootSQLite(docs, t.TempDir(), reps); len(warnings) > 0 {
		t.Fatal(warnings)
	}
	read := func(path, query string) string {
		db, _ := sql.Open("sqlite", path)
		defer db.Close()
		var v string
		_ = db.QueryRow(query).Scan(&v)
		return v
	}
	if got := read(session, `SELECT v FROM whatsapp_meta`); !strings.Contains(got, "Crew/sde-1") {
		t.Fatalf("whatsapp routing not rewritten: %s", got)
	}
	if got := read(session, `SELECT note FROM whatsmeow_device`); got != old {
		t.Fatalf("whatsmeow table touched: %s", got)
	}
	if got := read(other, `SELECT v FROM t`); got != old {
		t.Fatalf("unlisted database touched: %s", got)
	}
}
