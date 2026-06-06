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

func TestSetSessionShellEnvMergesAndCopies(t *testing.T) {
	sid := "sess-env-merge"
	defer ClearSessionShellConfig(sid)

	SetSessionShellEnv(sid, map[string]string{"DB_PATH": "/a", "STEP_OUTPUT_DIR": "/out"})
	SetSessionShellEnv(sid, map[string]string{"DB_PATH": "/b"}) // override one, keep the other

	env := GetSessionShellEnv(sid)
	if env["DB_PATH"] != "/b" {
		t.Fatalf("DB_PATH = %q, want /b (later call overrides)", env["DB_PATH"])
	}
	if env["STEP_OUTPUT_DIR"] != "/out" {
		t.Fatalf("STEP_OUTPUT_DIR = %q, want /out (preserved)", env["STEP_OUTPUT_DIR"])
	}
	// Returned map must be a copy — mutating it must not affect stored config.
	env["DB_PATH"] = "/mutated"
	if GetSessionShellEnv(sid)["DB_PATH"] != "/b" {
		t.Fatal("GetSessionShellEnv must return a copy, not the live map")
	}
	// Empty input is a no-op.
	SetSessionShellEnv(sid, nil)
	if GetSessionShellEnv(sid)["DB_PATH"] != "/b" {
		t.Fatal("nil env should be a no-op")
	}
}
