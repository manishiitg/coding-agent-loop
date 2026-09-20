package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
)

func productSecretsMigrationTestDocs(t *testing.T) (string, chathistory.Store) {
	t.Helper()
	t.Setenv("AUTH_SECRET", "migration-test-auth-secret-0123456789")
	docs := t.TempDir()
	store, err := chathistory.NewFilesystemStore(docs)
	if err != nil {
		t.Fatal(err)
	}
	return docs, store
}

func seedPersonalSecret(t *testing.T, store chathistory.Store, user, name, value string) {
	t.Helper()
	encrypted, err := encryptSecretValueWithAAD(value, []byte(user))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertUserSecret(context.Background(), user, name, encrypted); err != nil {
		t.Fatal(err)
	}
}

func readBoxValues(t *testing.T, store chathistory.Store, box string) map[string]string {
	t.Helper()
	recs, err := store.ListWorkflowSecrets(context.Background(), chathistory.SharedWorkflowSecretsUserID, box)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, rec := range recs {
		plain, err := decryptSharedWorkflowSecret(box, rec)
		if err != nil {
			t.Fatalf("box %s secret %s unreadable: %v", box, rec.Name, err)
		}
		out[rec.Name] = plain
	}
	return out
}

func readManifestSecrets(t *testing.T, docs, box string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(docs, filepath.FromSlash(box), "product.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	caps, _ := manifest["capabilities"].(map[string]interface{})
	items, _ := caps["selected_secrets"].([]interface{})
	names := []string{}
	for _, item := range items {
		if name, ok := item.(string); ok {
			names = append(names, name)
		}
	}
	return names
}

func TestMigrateProductSecretsSparkQuillApply(t *testing.T) {
	docs, store := productSecretsMigrationTestDocs(t)
	seedPersonalSecret(t, store, "alice", "PORTAL_PASSWORD", "hunter2")
	seedPersonalSecret(t, store, "alice", "API_TOKEN", "tok-123")

	report, err := runProductSecretsMigration(productSecretsMigrationOptions{
		Product: "sparkquill", DocsDir: docs, Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Users) != 1 || report.Users[0].Personal != 2 || !report.Users[0].Archived {
		t.Fatalf("unexpected report: %+v", report.Users)
	}
	box := "_users/alice/Chats/SparkQuill"
	values := readBoxValues(t, store, box)
	if values["PORTAL_PASSWORD"] != "hunter2" || values["API_TOKEN"] != "tok-123" {
		t.Fatalf("box values wrong: %v", values)
	}
	selected := readManifestSecrets(t, docs, box)
	if len(selected) != 2 || selected[0] != "API_TOKEN" || selected[1] != "PORTAL_PASSWORD" {
		t.Fatalf("manifest selected_secrets wrong: %v", selected)
	}
	if _, err := os.Stat(filepath.Join(docs, "_users", "alice", "secrets.json")); !os.IsNotExist(err) {
		t.Fatal("personal file should be archived away")
	}
	archived, _ := filepath.Glob(filepath.Join(docs, "_users", "alice", "secrets.json.migrated-*"))
	if len(archived) != 1 {
		t.Fatalf("expected one archive, got %v", archived)
	}
}

func TestMigrateProductSecretsDryRunWritesNothing(t *testing.T) {
	docs, store := productSecretsMigrationTestDocs(t)
	seedPersonalSecret(t, store, "alice", "PORTAL_PASSWORD", "hunter2")

	report, err := runProductSecretsMigration(productSecretsMigrationOptions{
		Product: "sparkquill", DocsDir: docs, Apply: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Users) != 1 || len(report.Users[0].Boxes) != 1 || len(report.Users[0].Boxes[0].Migrated) != 1 {
		t.Fatalf("dry-run should plan one migration: %+v", report.Users)
	}
	box := "_users/alice/Chats/SparkQuill"
	if recs, _ := store.ListWorkflowSecrets(context.Background(), chathistory.SharedWorkflowSecretsUserID, box); len(recs) != 0 {
		t.Fatal("dry-run wrote box secrets")
	}
	if _, err := os.Stat(filepath.Join(docs, filepath.FromSlash(box), "product.json")); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote a manifest")
	}
	if _, err := os.Stat(filepath.Join(docs, "_users", "alice", "secrets.json")); err != nil {
		t.Fatal("dry-run touched the personal file")
	}
}

func TestMigrateProductSecretsIsIdempotentAndNeverOverwrites(t *testing.T) {
	docs, store := productSecretsMigrationTestDocs(t)
	seedPersonalSecret(t, store, "alice", "SHARED_NAME", "personal-value")
	box := "_users/alice/Chats/SparkQuill"
	aad, err := sharedWorkflowSecretAAD(box)
	if err != nil {
		t.Fatal(err)
	}
	boxed, err := encryptSecretValueWithAAD("box-value-wins", aad)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertWorkflowSecret(context.Background(), chathistory.SharedWorkflowSecretsUserID, box, "SHARED_NAME", boxed); err != nil {
		t.Fatal(err)
	}

	report, err := runProductSecretsMigration(productSecretsMigrationOptions{
		Product: "sparkquill", DocsDir: docs, Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Users[0].Boxes[0].AlreadyPresent) != 1 || len(report.Users[0].Boxes[0].Migrated) != 0 {
		t.Fatalf("collision should skip the write: %+v", report.Users[0].Boxes)
	}
	if values := readBoxValues(t, store, box); values["SHARED_NAME"] != "box-value-wins" {
		t.Fatalf("box value overwritten: %v", values)
	}
	if !report.Users[0].Archived {
		t.Fatal("fully resolved user should still archive")
	}
	// Second run finds nothing to do.
	again, err := runProductSecretsMigration(productSecretsMigrationOptions{
		Product: "sparkquill", DocsDir: docs, Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.Users[0].Personal != 0 || len(again.Users[0].Boxes) != 0 || again.Users[0].Archived {
		t.Fatalf("second run should be a no-op: %+v", again.Users)
	}
}

func TestMigrateProductSecretsVideoStudioFansOutPerProject(t *testing.T) {
	docs, store := productSecretsMigrationTestDocs(t)
	seedPersonalSecret(t, store, "alice", "RENDER_KEY", "rk-1")
	projects := filepath.Join(docs, "_users", "alice", "Chats", "Video Studio", "projects")
	for _, project := range []string{"trailer", "teaser"} {
		if err := os.MkdirAll(filepath.Join(projects, project), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(projects, "note.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	report, err := runProductSecretsMigration(productSecretsMigrationOptions{
		Product: "video-studio", DocsDir: docs, Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Users) != 1 || len(report.Users[0].Boxes) != 2 {
		t.Fatalf("expected two project boxes: %+v", report.Users)
	}
	for _, box := range []string{
		"_users/alice/Chats/Video Studio/projects/trailer",
		"_users/alice/Chats/Video Studio/projects/teaser",
	} {
		if values := readBoxValues(t, store, box); values["RENDER_KEY"] != "rk-1" {
			t.Fatalf("box %s wrong: %v", box, values)
		}
		if selected := readManifestSecrets(t, docs, box); len(selected) != 1 || selected[0] != "RENDER_KEY" {
			t.Fatalf("box %s manifest wrong: %v", box, selected)
		}
	}
}

func TestMigrateProductSecretsLeavesUnreadableUsersInPlace(t *testing.T) {
	docs, store := productSecretsMigrationTestDocs(t)
	seedPersonalSecret(t, store, "alice", "GOOD", "fine")
	if err := store.UpsertUserSecret(context.Background(), "alice", "BROKEN", "!!!not-ciphertext!!!"); err != nil {
		t.Fatal(err)
	}
	seedPersonalSecret(t, store, "bob", "BOB_KEY", "bob-value")

	report, err := runProductSecretsMigration(productSecretsMigrationOptions{
		Product: "sparkquill", DocsDir: docs, Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	byUser := map[string]productSecretsUserReport{}
	for _, user := range report.Users {
		byUser[user.User] = user
	}
	if len(byUser["alice"].Errors) != 1 || len(byUser["alice"].Boxes) != 0 || byUser["alice"].Archived {
		t.Fatalf("alice should be untouched with one error: %+v", byUser["alice"])
	}
	if _, err := os.Stat(filepath.Join(docs, "_users", "alice", "secrets.json")); err != nil {
		t.Fatal("alice personal file should remain")
	}
	if !byUser["bob"].Archived || len(byUser["bob"].Errors) != 0 {
		t.Fatalf("bob should migrate cleanly: %+v", byUser["bob"])
	}
	if values := readBoxValues(t, store, "_users/bob/Chats/SparkQuill"); values["BOB_KEY"] != "bob-value" {
		t.Fatalf("bob box wrong: %v", values)
	}
}

func TestMigrateProductSecretsSkipsUsersWithoutBoxesAndPseudoUsers(t *testing.T) {
	docs, store := productSecretsMigrationTestDocs(t)
	seedPersonalSecret(t, store, "alice", "KEY", "v")
	seedPersonalSecret(t, store, chathistory.SharedWorkflowSecretsUserID, "X", "x")

	report, err := runProductSecretsMigration(productSecretsMigrationOptions{
		Product: "video-studio", DocsDir: docs, Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Users) != 1 || report.Users[0].Skipped == "" || report.Users[0].Archived {
		t.Fatalf("user without projects should skip without archiving: %+v", report.Users)
	}
	if _, err := os.Stat(filepath.Join(docs, "_users", "alice", "secrets.json")); err != nil {
		t.Fatal("personal file should remain for a later run")
	}
}

func TestMigrateProductSecretsUserFilterAndFlagValidation(t *testing.T) {
	docs, store := productSecretsMigrationTestDocs(t)
	seedPersonalSecret(t, store, "alice", "A", "a")
	seedPersonalSecret(t, store, "bob", "B", "b")

	report, err := runProductSecretsMigration(productSecretsMigrationOptions{
		Product: "sparkquill", DocsDir: docs, User: "bob", Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Users) != 1 || report.Users[0].User != "bob" {
		t.Fatalf("filter should migrate bob only: %+v", report.Users)
	}
	if _, err := os.Stat(filepath.Join(docs, "_users", "alice", "secrets.json")); err != nil {
		t.Fatal("alice should be untouched")
	}
	if _, err := runProductSecretsMigration(productSecretsMigrationOptions{Product: "nope", DocsDir: docs}); err == nil {
		t.Fatal("bad product should fail")
	}
	if _, err := runProductSecretsMigration(productSecretsMigrationOptions{Product: "sparkquill", DocsDir: ""}); err == nil {
		t.Fatal("missing docs dir should fail")
	}
	if _, err := runProductSecretsMigration(productSecretsMigrationOptions{Product: "sparkquill", DocsDir: docs, User: "ghost", Apply: true}); err == nil {
		t.Fatal("unknown user filter should fail")
	}
	t.Setenv("AUTH_SECRET", "")
	if _, err := runProductSecretsMigration(productSecretsMigrationOptions{Product: "sparkquill", DocsDir: docs}); err == nil {
		t.Fatal("missing AUTH_SECRET should fail")
	}
}
