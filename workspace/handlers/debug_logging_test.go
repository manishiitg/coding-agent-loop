package handlers

import "testing"

func TestWorkspaceDebugLoggingIsExplicitOptIn(t *testing.T) {
	for _, value := range []string{"", "0", "false", "off", "unexpected"} {
		t.Setenv(workspaceDebugLoggingEnv, value)
		if workspaceDebugLoggingEnabled() {
			t.Fatalf("debug logging enabled for %q", value)
		}
	}
	for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Setenv(workspaceDebugLoggingEnv, value)
		if !workspaceDebugLoggingEnabled() {
			t.Fatalf("debug logging disabled for %q", value)
		}
	}
}

func TestShellCommandLogIdentityDoesNotExposeCommand(t *testing.T) {
	command := "curl -H 'Authorization: Bearer secret-value' https://example.test"
	length, fingerprint := shellCommandLogIdentity(command)
	if length != len(command) || fingerprint == "" || fingerprint == command {
		t.Fatalf("unsafe command identity: length=%d fingerprint=%q", length, fingerprint)
	}
}
