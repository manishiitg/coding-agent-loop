package server

import (
	"os"
	"path/filepath"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
)

func TestMCPRuntimeConfigSurvivesReleaseReplacement(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	release := func(name string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "mcp_servers.json")
		if err := mcpclient.SaveConfig(p, &mcpclient.MCPConfig{MCPServers: map[string]mcpclient.MCPServerConfig{name: {URL: "https://example.test/mcp"}}}); err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := release("release1")
	runtime, err := prepareMCPRuntimeConfig(first, state)
	if err != nil {
		t.Fatal(err)
	}
	api := &StreamingAPI{mcpConfigPath: runtime, logger: loggerv2.NewNoop()}
	policy := resolveWorkflowChatPolicy("workshop", "chat", QueryRequest{}, nil, false)
	before := api.chatPolicySessionKey(policy)
	if err := api.persistOAuthConfig("custom", mcpclient.MCPServerConfig{URL: "https://custom.test/mcp"}); err != nil {
		t.Fatal(err)
	}
	after := api.chatPolicySessionKey(policy)
	if before == after {
		t.Fatal("installation did not invalidate native catalog identity")
	}
	if after != api.chatPolicySessionKey(policy) {
		t.Fatal("unchanged config must keep session stable")
	}
	second := release("release2")
	next, err := prepareMCPRuntimeConfig(second, state)
	if err != nil {
		t.Fatal(err)
	}
	if next != runtime {
		t.Fatal("runtime path changed across deployment")
	}
	if err := os.RemoveAll(filepath.Dir(first)); err != nil {
		t.Fatal(err)
	}
	cfg, err := mcpclient.LoadMergedConfig(next, loggerv2.NewNoop())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"release2", "custom"} {
		if _, err := cfg.GetServer(name); err != nil {
			t.Fatalf("%s lost: %v", name, err)
		}
	}

}

func TestMCPRuntimeConfigMigratesLegacyOverlayWithoutOverwrite(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "base.json")
	legacy := filepath.Join(root, "base_user.json")
	state := filepath.Join(root, "state")
	for _, path := range []string{source, legacy} {
		if err := os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	dest, err := prepareMCPRuntimeConfig(source, state)
	if err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(filepath.Dir(dest), "base_user.json")
	saved := []byte(`{"mcpServers":{"saved":{"url":"https://example.test/mcp"}}}`)
	if err := os.WriteFile(overlay, saved, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareMCPRuntimeConfig(source, state); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(overlay)
	if err != nil || string(got) != string(saved) {
		t.Fatalf("existing overlay overwritten: %s %v", got, err)
	}
	info, _ := os.Stat(overlay)
	if info.Mode().Perm() != 0600 {
		t.Fatal("overlay permissions")
	}
	if got, err := prepareMCPRuntimeConfig(source, ""); err != nil || got != source {
		t.Fatal("local compatibility changed")
	}
}
