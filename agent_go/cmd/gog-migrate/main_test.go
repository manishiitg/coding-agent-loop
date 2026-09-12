package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

func TestMigrationCommandVerifiesBeforeSaving(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOG_HOME", filepath.Join(dir, "gog-home"))
	t.Setenv("GMAIL_OAUTH_TOKEN_DIR", filepath.Join(dir, "legacy-tokens"))
	binary := filepath.Join(dir, "gog")
	script := `#!/bin/sh
cat <<'JSON'
{"accounts":[{"email":"me@example.com","client":"primary","valid":true,"scopes":["https://www.googleapis.com/auth/gmail.send"]}]}
JSON
`
	if err := os.WriteFile(binary, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := services.GmailConfig{GogPath: binary, Connections: []services.GmailConnection{{ID: "gmail_001", Email: "me@example.com", ClientName: "primary", Enabled: true}}}
	raw, _ := json.Marshal(cfg)
	path := filepath.Join(dir, "gmail-config.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(path, false, false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("verification changed configuration")
	}
	if err := run(path, true, false); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(path)
	var migrated services.GmailConfig
	if err := json.Unmarshal(after, &migrated); err != nil {
		t.Fatal(err)
	}
	if !migrated.UseGogBackend || migrated.Connections[0].AuthBackend != "gog" {
		t.Fatal("migration not saved")
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "gmail-config.before-gog-*.json"))
	if len(backups) != 1 {
		t.Fatal("original registry not backed up")
	}
	backup, _ := os.ReadFile(backups[0])
	if string(backup) != string(raw) {
		t.Fatal("backup differs from original")
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := run(path, true, false); err == nil {
		t.Fatal("failed verification was accepted")
	}
	unchanged, _ := os.ReadFile(path)
	if string(unchanged) != string(after) {
		t.Fatal("failure changed saved configuration")
	}
}
