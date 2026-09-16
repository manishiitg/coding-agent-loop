package virtualtools

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
)

func TestSetSessionShellEnvReachesEveryLiveClientOfTheSession(t *testing.T) {
	_, _ = CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv("u1", "sess-live", map[string]string{"SECRET_A": "1"})
	_, _ = CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv("u1", "sess-live", nil)
	_, _ = CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv("u1", "sess-other", nil)

	if n := SetSessionShellEnv("sess-live", "SECRET_NEW", "v"); n != 2 {
		t.Fatalf("updated %d clients, want the session's 2", n)
	}
	sessionShellClients.mu.Lock()
	clients := sessionShellClients.m["sess-live"]
	other := sessionShellClients.m["sess-other"]
	sessionShellClients.mu.Unlock()
	for _, c := range clients {
		if v, ok := c.ExtraEnvValue("SECRET_NEW"); !ok || v != "v" {
			t.Fatalf("client missing SECRET_NEW: %q %v", v, ok)
		}
	}
	if v, ok := clients[0].ExtraEnvValue("SECRET_A"); !ok || v != "1" {
		t.Fatal("existing env must survive")
	}
	for _, c := range other {
		if _, ok := c.ExtraEnvValue("SECRET_NEW"); ok {
			t.Fatal("another session's client must not be touched")
		}
	}
	if n := DeleteSessionShellEnv("sess-live", "SECRET_NEW"); n != 2 {
		t.Fatalf("delete touched %d clients, want 2", n)
	}
	if _, ok := clients[0].ExtraEnvValue("SECRET_NEW"); ok {
		t.Fatal("deleted secret still present")
	}
	if n := SetSessionShellEnv("", "SECRET_X", "1"); n != 0 {
		t.Fatalf("empty session id must register nothing, got %d", n)
	}
}

func TestReplaceSessionShellSecretsRemovesDeselectedValues(t *testing.T) {
	client := workspace.NewClient("http://example.invalid", workspace.WithExtraEnv(map[string]string{"SECRET_OLD": "old"}))
	registerSessionShellClient("sess-replace", client)
	SetSessionShellEnv("sess-replace", "SECRET_OLD", "old")
	if n := ReplaceSessionShellSecrets("sess-replace", map[string]string{"SECRET_NEW": "new"}); n != 1 {
		t.Fatalf("updated clients = %d, want 1", n)
	}
	if _, ok := client.ExtraEnvValue("SECRET_OLD"); ok {
		t.Fatal("deselected secret remained in retained client")
	}
	if value, ok := client.ExtraEnvValue("SECRET_NEW"); !ok || value != "new" {
		t.Fatalf("replacement secret = %q, %v", value, ok)
	}
	if n := ReplaceSessionShellSecrets("sess-replace", map[string]string{"SECRET_NEW": "newer"}); n != 1 {
		t.Fatalf("second replacement clients = %d, want 1", n)
	}
}

func TestSessionShellClientsAreBounded(t *testing.T) {
	for i := 0; i < sessionShellClientsKept+5; i++ {
		_, _ = CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv("u1", "sess-bounded", nil)
	}
	if n := SetSessionShellEnv("sess-bounded", "K", "v"); n != sessionShellClientsKept {
		t.Fatalf("kept %d clients, want %d", n, sessionShellClientsKept)
	}
}
