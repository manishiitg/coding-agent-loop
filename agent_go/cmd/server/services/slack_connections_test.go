package services

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSlackMigrateLegacyConfig(t *testing.T) {
	legacy := &SlackConfig{Enabled: true, BotToken: "xoxb-legacy", AppToken: "xapp-legacy"}
	if !migrateLegacySlackConfig(legacy) {
		t.Fatal("legacy credentials were not migrated")
	}
	if len(legacy.Connections) != 1 {
		t.Fatalf("connections = %d, want 1", len(legacy.Connections))
	}
	conn := legacy.Connections[0]
	if conn.ID != "slack_001" || conn.DisplayName == "" || conn.BotToken != "xoxb-legacy" || conn.AppToken != "xapp-legacy" || !conn.Enabled {
		t.Fatalf("migrated connection = %+v", conn)
	}
	if legacy.DefaultConnectionID != "slack_001" {
		t.Fatalf("default = %q", legacy.DefaultConnectionID)
	}
	if legacy.BotToken != "" || legacy.AppToken != "" || legacy.Enabled {
		t.Fatalf("legacy fields not cleared: %+v", legacy)
	}

	registry := &SlackConfig{Connections: []SlackConnection{{ID: "a", BotToken: "x", AppToken: "y"}}, BotToken: "xoxb-stray"}
	if migrateLegacySlackConfig(registry) {
		t.Fatal("migration overwrote an existing registry")
	}
	if registry.BotToken != "xoxb-stray" {
		t.Fatal("stray legacy field touched despite registry")
	}

	empty := &SlackConfig{}
	if migrateLegacySlackConfig(empty) {
		t.Fatal("empty config reported a migration")
	}
}

func TestNormalizeSlackConnections(t *testing.T) {
	cfg := &SlackConfig{
		Connections: []SlackConnection{
			{ID: " b ", BotToken: "xoxb-1", AppToken: "xapp-1"},
			{ID: "", BotToken: "xoxb-blank", AppToken: "xapp-blank"},
			{ID: "b", BotToken: "xoxb-dup", AppToken: "xapp-dup"},
			{ID: "c", DisplayName: " C ", BotToken: "xoxb-2", AppToken: "xapp-2"},
		},
		DefaultConnectionID: "missing",
	}
	normalizeSlackConnections(cfg)
	if len(cfg.Connections) != 2 || cfg.Connections[0].ID != "b" || cfg.Connections[1].ID != "c" {
		t.Fatalf("normalized = %+v", cfg.Connections)
	}
	if cfg.Connections[0].DisplayName == "" || cfg.Connections[1].DisplayName != "C" {
		t.Fatalf("display names = %q, %q", cfg.Connections[0].DisplayName, cfg.Connections[1].DisplayName)
	}
	if cfg.DefaultConnectionID != "" {
		t.Fatalf("dangling default kept: %q", cfg.DefaultConnectionID)
	}

	single := &SlackConfig{Connections: []SlackConnection{{ID: "only", BotToken: "x", AppToken: "y"}}}
	normalizeSlackConnections(single)
	if single.DefaultConnectionID != "only" {
		t.Fatalf("single connection did not become default: %q", single.DefaultConnectionID)
	}

	scoped := &SlackConfig{Connections: []SlackConnection{{ID: "w", BotToken: "x", AppToken: "y", WorkspacePath: "Workflow/w"}}}
	normalizeSlackConnections(scoped)
	if scoped.DefaultConnectionID != "" {
		t.Fatalf("lone workflow-scoped connection became default: %q", scoped.DefaultConnectionID)
	}
	if _, ok := effectiveSlackConnection(scoped, ""); ok {
		t.Fatal("empty selection borrowed the workflow-scoped connection")
	}
}

func TestEffectiveSlackConnection(t *testing.T) {
	cfg := &SlackConfig{
		Connections: []SlackConnection{
			{ID: "d", DisplayName: "Default", BotToken: "xoxb-d", AppToken: "xapp-d", Enabled: true},
			{ID: "w", DisplayName: "Work", BotToken: "xoxb-w", AppToken: "xapp-w", Enabled: true},
		},
		DefaultConnectionID: "d",
	}
	if conn, ok := effectiveSlackConnection(cfg, ""); !ok || conn.ID != "d" {
		t.Fatalf("empty selection = %+v, %v; want default", conn, ok)
	}
	if conn, ok := effectiveSlackConnection(cfg, "w"); !ok || conn.BotToken != "xoxb-w" {
		t.Fatalf("explicit selection = %+v, %v", conn, ok)
	}
	if _, ok := effectiveSlackConnection(cfg, "nope"); ok {
		t.Fatal("unknown connection resolved")
	}

	legacy := &SlackConfig{Enabled: true, BotToken: "xoxb-l", AppToken: "xapp-l"}
	if conn, ok := effectiveSlackConnection(cfg, "legacy-ignored"); ok || conn.ID != "" {
		t.Fatal("explicit selection fell back to another identity")
	}
	if conn, ok := effectiveSlackConnection(legacy, ""); !ok || conn.BotToken != "xoxb-l" {
		t.Fatalf("legacy fallback = %+v, %v", conn, ok)
	}
	incomplete := &SlackConfig{Enabled: true, BotToken: "xoxb-l"}
	if _, ok := effectiveSlackConnection(incomplete, ""); ok {
		t.Fatal("incomplete legacy config resolved")
	}
}

func TestMergeSlackConfigForSave(t *testing.T) {
	stored := &SlackConfig{
		Connections: []SlackConnection{
			{ID: "d", DisplayName: "Default", BotToken: "xoxb-d", AppToken: "xapp-d", Enabled: true},
			{ID: "w", DisplayName: "Work", BotToken: "xoxb-w", AppToken: "xapp-w", Enabled: false, WorkspacePath: "Workflow/w"},
		},
		DefaultConnectionID: "d",
	}

	// Legacy shape folds onto the default and preserves the rest.
	merged, err := mergeSlackConfigForSave(stored, &SlackConfig{Enabled: false, BotToken: "xoxb-...x-d", AppToken: "xapp-new"})
	if err != nil {
		t.Fatal(err)
	}
	def, _ := findSlackConnection(merged, "d")
	if def.Enabled || def.BotToken != "xoxb-d" || def.AppToken != "xapp-new" {
		t.Fatalf("default after legacy save = %+v", def)
	}
	if other, _ := findSlackConnection(merged, "w"); other.BotToken != "xoxb-w" || other.WorkspacePath != "Workflow/w" {
		t.Fatalf("sibling connection disturbed: %+v", other)
	}

	// Registry shape replaces wholesale; masked and empty tokens preserve.
	merged, err = mergeSlackConfigForSave(stored, &SlackConfig{
		Connections: []SlackConnection{
			{ID: "d", BotToken: "xoxb-...x-d", AppToken: "", Enabled: true},
			{ID: "n", DisplayName: "New", BotToken: "xoxb-n", AppToken: "xapp-n", Enabled: true},
		},
		DefaultConnectionID: "n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if def, _ := findSlackConnection(merged, "d"); def.BotToken != "xoxb-d" || def.AppToken != "xapp-d" || def.DisplayName != "Default" {
		t.Fatalf("masked/empty tokens not preserved: %+v", def)
	}
	if merged.DefaultConnectionID != "n" {
		t.Fatalf("default = %q", merged.DefaultConnectionID)
	}
	if _, ok := findSlackConnection(merged, "w"); ok {
		t.Fatal("dropped connection survived a registry replace")
	}

	// Masked tokens for an unknown connection are rejected, not stored.
	if _, err := mergeSlackConfigForSave(stored, &SlackConfig{
		Connections: []SlackConnection{{ID: "ghost", BotToken: "xoxb-...ZZZZ"}},
	}); err == nil {
		t.Fatal("masked tokens accepted for unknown connection")
	}
	if _, err := mergeSlackConfigForSave(stored, &SlackConfig{
		Connections:         []SlackConnection{{ID: "d", BotToken: "xoxb-d", AppToken: "xapp-d"}},
		DefaultConnectionID: "ghost",
	}); err == nil {
		t.Fatal("unknown default accepted")
	}
	if _, err := mergeSlackConfigForSave(stored, &SlackConfig{Enabled: true, BotToken: "wrong-type", AppToken: "xapp-d"}); err == nil {
		t.Fatal("wrong token type accepted on legacy save")
	}

	// Legacy save onto an empty store creates the default.
	fresh, err := mergeSlackConfigForSave(&SlackConfig{}, &SlackConfig{Enabled: true, BotToken: "xoxb-f", AppToken: "xapp-f"})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.DefaultConnectionID != "slack_001" || len(fresh.Connections) != 1 {
		t.Fatalf("fresh legacy save = %+v", fresh)
	}
	disabled, err := mergeSlackConfigForSave(&SlackConfig{}, &SlackConfig{})
	if err != nil || len(disabled.Connections) != 0 {
		t.Fatalf("empty legacy save created connections: %+v, %v", disabled, err)
	}
}

func TestSlackServiceForConnectionRouting(t *testing.T) {
	cfg := &SlackConfig{
		Connections: []SlackConnection{
			{ID: "d", BotToken: "xoxb-d", AppToken: "xapp-d", Enabled: true},
			{ID: "w", BotToken: "xoxb-w", AppToken: "xapp-w", Enabled: true},
			{ID: "off", BotToken: "xoxb-o", AppToken: "xapp-o", Enabled: false},
		},
		DefaultConnectionID: "d",
	}
	child := &SlackService{connectionID: "w", config: cfg}
	root := &SlackService{config: cfg, children: map[string]*SlackService{"w": child}}

	if got, err := root.serviceForConnection(""); err != nil || got != root {
		t.Fatalf("empty selection did not resolve to root: %v", err)
	}
	if got, err := root.serviceForConnection("d"); err != nil || got != root {
		t.Fatalf("default selection did not resolve to root: %v", err)
	}
	if got, err := root.serviceForConnection("w"); err != nil || got != child {
		t.Fatalf("explicit selection misrouted: %v", err)
	}
	if _, err := root.serviceForConnection("ghost"); err == nil {
		t.Fatal("unknown connection resolved")
	}
	if _, err := root.serviceForConnection("off"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled connection error = %v", err)
	}
	if got, err := child.serviceForConnection(""); err != nil || got != child {
		t.Fatalf("child empty selection misrouted: %v", err)
	}
	if _, err := child.serviceForConnection("d"); err == nil {
		t.Fatal("child served another connection")
	}
	var nilSvc *SlackService
	if _, err := nilSvc.serviceForConnection(""); err == nil {
		t.Fatal("nil service resolved")
	}
}

func TestSlackGetConfigMasksRegistry(t *testing.T) {
	svc := &SlackService{config: &SlackConfig{
		Connections: []SlackConnection{
			{ID: "d", DisplayName: "Default", BotToken: "xoxb-secret-default", AppToken: "xapp-secret-default", Enabled: true},
			{ID: "w", DisplayName: "Work", BotToken: "xoxb-secret-work", AppToken: "xapp-secret-work", Enabled: false, WorkspacePath: "Workflow/w"},
		},
		DefaultConnectionID: "d",
	}}
	got := svc.GetConfig()
	if got.BotToken != "xoxb-...ault" || got.AppToken != "xapp-...ault" || !got.Enabled {
		t.Fatalf("legacy mirror = %+v", got)
	}
	if len(got.Connections) != 2 || got.Connections[1].BotToken != "xoxb-...work" || got.Connections[1].WorkspacePath != "Workflow/w" {
		t.Fatalf("registry mirror = %+v", got.Connections)
	}
	for _, conn := range got.Connections {
		if strings.Contains(conn.BotToken, "secret") || strings.Contains(conn.AppToken, "secret") {
			t.Fatalf("unmasked token leaked: %+v", conn)
		}
	}
}

func TestThreadIDKeyIgnoresConnection(t *testing.T) {
	plain := ThreadID{Platform: "slack", ChannelID: "C1", ThreadTS: "1.0"}
	scoped := ThreadID{Platform: "slack", ChannelID: "C1", ThreadTS: "1.0", ConnectionID: "w"}
	if plain.Key() != scoped.Key() {
		t.Fatalf("keys differ: %q vs %q", plain.Key(), scoped.Key())
	}
}

func TestValidateSlackConnectionTokens(t *testing.T) {
	if err := validateSlackConnectionTokens("", ""); err != nil {
		t.Fatalf("empty tokens rejected: %v", err)
	}
	if err := validateSlackConnectionTokens("xoxb-a", "xapp-b"); err != nil {
		t.Fatalf("valid tokens rejected: %v", err)
	}
	if err := validateSlackConnectionTokens("xapp-a", "xapp-b"); err == nil {
		t.Fatal("swapped bot token accepted")
	}
	if err := validateSlackConnectionTokens("xoxb-a", "xoxb-b"); err == nil {
		t.Fatal("swapped app token accepted")
	}
}

func TestDecryptSlackConfigConnections(t *testing.T) {
	encrypt, decrypt := slackCredentialEncrypt, slackCredentialDecrypt
	defer ConfigureSlackCredentialCodec(encrypt, decrypt)
	ConfigureSlackCredentialCodec(
		func(s string) (string, error) { return base64.StdEncoding.EncodeToString([]byte(s)), nil },
		func(s string) (string, error) {
			raw, err := base64.StdEncoding.DecodeString(s)
			return string(raw), err
		},
	)
	stored := &SlackConfig{Connections: []SlackConnection{{ID: "d", BotToken: "encrypted:v1:" + base64.StdEncoding.EncodeToString([]byte("xoxb-d")), AppToken: "plaintext-legacy"}}}
	decoded, err := decryptSlackConfig(stored)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Connections[0].BotToken != "xoxb-d" || decoded.Connections[0].AppToken != "plaintext-legacy" {
		t.Fatalf("decoded = %+v", decoded.Connections[0])
	}
}

func TestMintSlackConnectionID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := mintSlackConnectionID(seen)
		if !strings.HasPrefix(id, "slack_") || len(id) != len("slack_")+8 {
			t.Fatalf("bad id %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}
