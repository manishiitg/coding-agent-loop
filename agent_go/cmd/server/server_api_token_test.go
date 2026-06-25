package server

import "testing"

func TestResolveServerAPITokenUsesExplicitStartupToken(t *testing.T) {
	t.Setenv(envMCPServerAPIToken, " test-token-123 ")

	if got := resolveServerAPIToken(); got != "test-token-123" {
		t.Fatalf("resolveServerAPIToken() = %q, want explicit startup token", got)
	}
}

func TestResolveServerAPITokenGeneratesDefaultToken(t *testing.T) {
	t.Setenv(envMCPServerAPIToken, "")

	got := resolveServerAPIToken()
	if got == "" {
		t.Fatal("resolveServerAPIToken() returned empty token")
	}
	if got == "test-token-123" {
		t.Fatal("resolveServerAPIToken() reused explicit test token with no env set")
	}
}
