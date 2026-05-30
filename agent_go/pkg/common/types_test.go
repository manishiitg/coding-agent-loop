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

func TestPopulateMCPBridgeShortEnv(t *testing.T) {
	env := map[string]string{
		"MCP_API_URL":   "http://example.test/s/session-1/",
		"MCP_API_TOKEN": "test-token",
	}

	PopulateMCPBridgeShortEnv(env)

	want := map[string]string{
		"MCP_AUTH":    "Authorization: Bearer test-token",
		"MCP_MCP":     "http://example.test/s/session-1/tools/mcp",
		"MCP_CUSTOM":  "http://example.test/s/session-1/tools/custom",
		"MCP_VIRTUAL": "http://example.test/s/session-1/tools/virtual",
	}
	for k, v := range want {
		if got := env[k]; got != v {
			t.Fatalf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestPopulateMCPBridgeShortEnvClearsStaleValues(t *testing.T) {
	env := map[string]string{
		"MCP_MCP":     "old",
		"MCP_CUSTOM":  "old",
		"MCP_VIRTUAL": "old",
		"MCP_AUTH":    "old",
	}

	PopulateMCPBridgeShortEnv(env)

	for _, k := range []string{"MCP_MCP", "MCP_CUSTOM", "MCP_VIRTUAL", "MCP_AUTH"} {
		if _, exists := env[k]; exists {
			t.Fatalf("%s should be cleared when source env is missing", k)
		}
	}
}
