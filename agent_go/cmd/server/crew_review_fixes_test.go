package server

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/workproduct"
)

// An owner is trusted once: after the record names a creator, a later claim
// (or an edited product.json) changes nothing.
func TestCrewAccessRecordPinsFirstCreator(t *testing.T) {
	dir := stubCrewAccess(t, map[string]crewAccess{"Crew/pinned-1": {Creator: "alice", Owners: []string{"alice"}}}, nil)
	if acl, err := crewAccessRecords.claim("Crew/pinned-1", "mallory"); err != nil || acl.Creator != "alice" {
		t.Fatalf("a later claim replaced the recorded creator: %+v %v", acl, err)
	}
	if err := workproduct.RecordCrewCreator(context.Background(), "Crew/pinned-1", "mallory"); err == nil {
		t.Fatal("creating a crew over another owner's record must fail")
	}
	if acl, err := crewAccessRecords.claim("Crew/new-2", "bob"); err != nil || acl.Creator != "bob" {
		t.Fatalf("first claim: %+v %v", acl, err)
	}
	records, err := readCrewAccessFile(filepath.Join(dir, crewAccessFileName))
	if err != nil || records["Crew/new-2"].Creator != "bob" || records["Crew/pinned-1"].Creator != "alice" {
		t.Fatalf("records on disk: %+v %v", records, err)
	}
	if got := crewOwners.owner(context.Background(), "Crew/pinned-1"); got != "alice" {
		t.Fatalf("creator %q", got)
	}
}

// An unreadable record file is an error, never an empty map to overwrite.
func TestCrewAccessFileReadFailureDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, crewAccessFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := updateCrewAccessFile(dir, func(m map[string]crewAccess) error {
		m["Crew/x-1"] = crewAccess{Creator: "eve", Owners: []string{"eve"}}
		return nil
	}); err == nil {
		t.Fatal("update over a corrupt file must fail")
	}
	if raw, _ := os.ReadFile(path); string(raw) != "{not json" {
		t.Fatalf("corrupt file was overwritten: %s", raw)
	}
}

// Co-owners are owners; a private crew is invisible to everyone else; any
// other work user reads a shared crew.
func TestCrewAccessCoOwnersAndPrivate(t *testing.T) {
	stubCrewAccess(t, map[string]crewAccess{
		"Crew/team-1": {Creator: "alice", Owners: []string{"alice", "bob"}},
		"Crew/solo-2": {Creator: "alice", Owners: []string{"alice"}, Private: true},
	}, nil)
	ctx := context.Background()
	reader := &UserClaims{UserID: "carol"}
	team, _ := resolveCrewPath(ctx, "bob", "Crew/team-1")
	solo, _ := resolveCrewPath(ctx, "bob", "Crew/solo-2")
	if got := crewAccessFor(&UserClaims{UserID: "bob"}, team); got != crewAccessOwner {
		t.Fatalf("co-owner access %v", got)
	}
	if got := crewAccessFor(&UserClaims{UserID: "bob"}, solo); got != crewAccessNone {
		t.Fatalf("private crew visible to non-owner: %v", got)
	}
	if got := crewAccessFor(&UserClaims{UserID: "alice"}, solo); got != crewAccessOwner {
		t.Fatalf("private crew hidden from its owner: %v", got)
	}
	if !crewRootOwnedBy(ctx, "Crew/team-1", "bob") || crewRootCreatedBy(ctx, "Crew/team-1", "bob") || !crewRootCreatedBy(ctx, "Crew/team-1", "alice") {
		t.Fatal("co-owner owns but did not create; schedules run as the creator only")
	}
	if crewRootOwnedBy(ctx, "Crew/team-1", reader.UserID) {
		t.Fatal("a reader is not an owner")
	}
	write, read := splitCrewReferenceFolders("bob", []string{"Crew/team-1", "Crew/solo-2"})
	if strings.Join(write, ",") != "Crew/team-1/" || strings.Join(read, ",") != "Crew/solo-2" {
		t.Fatalf("co-owner writes: %v read=%v", write, read)
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

// The access endpoint's change: co-owners replace, the creator stays, and an
// explicit empty list clears co-owners; nil leaves them alone.
func TestSetCrewAccessKeepsCreator(t *testing.T) {
	stubCrewAccess(t, map[string]crewAccess{"Crew/team-1": {Creator: "alice", Owners: []string{"alice"}}}, nil)
	private := true
	acl, err := setCrewAccess("Crew/team-1", []string{"bob", "alice", "bob", "!!"}, &private)
	if err != nil || strings.Join(acl.Owners, ",") != "alice,bob" || !acl.Private || acl.Creator != "alice" {
		t.Fatalf("set: %+v %v", acl, err)
	}
	if acl, _ = setCrewAccess("Crew/team-1", nil, nil); strings.Join(acl.Owners, ",") != "alice,bob" || !acl.Private {
		t.Fatalf("nil change altered the record: %+v", acl)
	}
	if acl, _ = setCrewAccess("Crew/team-1", []string{}, nil); strings.Join(acl.Owners, ",") != "alice" {
		t.Fatalf("empty list must leave only the creator: %+v", acl)
	}
	if !crewRootOwnedBy(context.Background(), "Crew/team-1", "alice") || crewRootOwnedBy(context.Background(), "Crew/team-1", "bob") {
		t.Fatal("the cached view did not follow the update")
	}
	if _, err := setCrewAccess("Crew/unknown-2", []string{"bob"}, nil); err == nil {
		t.Fatal("a crew without a record must not gain one through the access endpoint")
	}
}

// Crew paths map onto workflow access levels: owners (and admins) own,
// other work users read, private crews and non-work users get nothing, and
// bot routes keep their own rules.
func TestCrewWorkflowAccessMapping(t *testing.T) {
	withMemoryUserDirectory(t, `{"users":[{"id":"root","username":"root","role":"admin"},{"id":"alice","username":"alice","role":"editor","products":["work"]},{"id":"bob","username":"bob","role":"editor","products":["work"]},{"id":"carol","username":"carol","products":["agentworks"]}]}`)
	stubCrewAccess(t, map[string]crewAccess{
		"Crew/team-1": {Creator: "alice", Owners: []string{"alice", "bob"}},
		"Crew/solo-2": {Creator: "alice", Owners: []string{"alice"}, Private: true},
	}, nil)
	ctx := context.Background()
	for _, tc := range []struct {
		user, path string
		want       WorkflowAccessLevel
	}{
		{"alice", "Crew/team-1/db/x.json", WorkflowAccessOwner},
		{"bob", "Crew/team-1", WorkflowAccessOwner},
		{"bob", "Crew/solo-2", WorkflowAccessNone},
		{"carol", "Crew/team-1", WorkflowAccessNone},
		{"root", "Crew/solo-2", WorkflowAccessOwner},
	} {
		got, ok := crewWorkflowAccess(ctx, &UserClaims{UserID: tc.user, Username: tc.user}, tc.path)
		if !ok || got != tc.want {
			t.Fatalf("%s on %s = %q ok=%v, want %q", tc.user, tc.path, got, ok, tc.want)
		}
	}
	if _, ok := crewWorkflowAccess(ctx, &UserClaims{UserID: "bob", Provider: "bot_route"}, "Crew/team-1"); ok {
		t.Fatal("bot routes must not take the crew mapping")
	}
	if _, ok := crewWorkflowAccess(ctx, &UserClaims{UserID: "bob"}, "Workflow/reports"); ok {
		t.Fatal("a workflow path is not a crew")
	}
}

// A view-only or disabled account listed as an owner acts as a reader; the
// access endpoint also refuses to add one.
func TestCrewOwnershipCappedByAccountRole(t *testing.T) {
	withMemoryUserDirectory(t, `{"users":[{"id":"alice","username":"alice","role":"creator"},{"id":"vic","username":"vic","role":"viewer","products":["work"]},{"id":"dan","username":"dan","role":"editor","products":["work"],"disabled":true}]}`)
	stubCrewAccess(t, map[string]crewAccess{"Crew/team-1": {Creator: "alice", Owners: []string{"alice", "vic", "dan"}}}, nil)
	ctx := context.Background()
	if !crewRootOwnedBy(ctx, "Crew/team-1", "alice") {
		t.Fatal("creator lost ownership")
	}
	for _, user := range []string{"vic", "dan"} {
		if crewRootOwnedBy(ctx, "Crew/team-1", user) || crewProjectOwnedByCaller(user, "Crew/team-1") {
			t.Fatalf("%s must not act as an owner", user)
		}
	}
	ref, _ := resolveCrewPath(ctx, "vic", "Crew/team-1")
	if got := crewAccessFor(&UserClaims{UserID: "vic", Username: "vic"}, ref); got != crewAccessReader {
		t.Fatalf("view-only co-owner access %v, want reader", got)
	}
}
