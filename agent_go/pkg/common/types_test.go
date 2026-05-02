package common

import "testing"

func TestResolveBrowserSessionID(t *testing.T) {
	sessionID := "chat-session-test"
	SetSessionBrowserSessionID(sessionID, "workflow-browser-session")
	defer ClearSessionShellConfig(sessionID)

	if got := ResolveBrowserSessionID(sessionID, "default"); got != "workflow-browser-session" {
		t.Fatalf("default browser session should resolve to shared browser session, got %q", got)
	}

	if got := ResolveBrowserSessionID(sessionID, "isolated-123"); got != "isolated-123" {
		t.Fatalf("explicit non-default browser session should win, got %q", got)
	}
}

func TestIsCLIProvider(t *testing.T) {
	tests := []struct {
		provider string
		want     bool
	}{
		{provider: "claude-code", want: true},
		{provider: "gemini-cli", want: true},
		{provider: "codex-cli", want: true},
		{provider: "kimi", want: true},
		{provider: " Codex-CLI ", want: true},
		{provider: "openai", want: false},
		{provider: "vertex", want: false},
		{provider: "", want: false},
	}

	for _, tt := range tests {
		if got := IsCLIProvider(tt.provider); got != tt.want {
			t.Fatalf("IsCLIProvider(%q) = %v, want %v", tt.provider, got, tt.want)
		}
	}
}
