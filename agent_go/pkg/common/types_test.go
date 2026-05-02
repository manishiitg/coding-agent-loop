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

