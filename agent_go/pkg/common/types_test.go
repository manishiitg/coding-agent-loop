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

func TestSessionShellConfigIsImmutableAcrossCallers(t *testing.T) {
	sessionID := "immutable-shell-config"
	defer ClearSessionShellConfig(sessionID)
	reads := []string{"Workflow/demo"}
	writes := []string{"Workflow/demo/output"}
	SetSessionFolderGuard(sessionID, reads, writes)
	SetSessionFolderGuardBlockedPaths(sessionID, []string{"Workflow/demo/secrets"})
	SetSessionFolderGuardBlockedWritePaths(sessionID, []string{"Workflow/demo/planning"})

	reads[0] = "Workflow/other"
	writes[0] = "Workflow/other/output"
	first := GetSessionShellConfig(sessionID)
	first.ReadPaths[0] = "mutated-read"
	first.WritePaths[0] = "mutated-write"
	first.BlockedPaths[0] = "mutated-block"
	first.BlockedWritePaths[0] = "mutated-write-block"

	second := GetSessionShellConfig(sessionID)
	if got := second.ReadPaths[0]; got != "Workflow/demo" {
		t.Fatalf("stored read path mutated: %q", got)
	}
	if got := second.WritePaths[0]; got != "Workflow/demo/output" {
		t.Fatalf("stored write path mutated: %q", got)
	}
	if got := second.BlockedPaths[0]; got != "Workflow/demo/secrets" {
		t.Fatalf("stored blocked path mutated: %q", got)
	}
	if got := second.BlockedWritePaths[0]; got != "Workflow/demo/planning" {
		t.Fatalf("stored blocked-write path mutated: %q", got)
	}
}

func TestCopySessionFolderGuardPreservesDenyOnlyGuard(t *testing.T) {
	const source = "deny-only-source"
	const target = "deny-only-target"
	defer ClearSessionShellConfig(source)
	defer ClearSessionShellConfig(target)

	SetSessionFolderGuardBlockedPaths(source, []string{"Workflow/demo/secrets"})
	SetSessionFolderGuardBlockedWritePaths(source, []string{"Workflow/demo/planning"})
	if !CopySessionFolderGuard(source, target) {
		t.Fatal("deny-only guard should be copied")
	}

	copied := GetSessionShellConfig(target)
	if copied == nil || len(copied.BlockedPaths) != 1 || copied.BlockedPaths[0] != "Workflow/demo/secrets" {
		t.Fatalf("blocked paths not copied: %+v", copied)
	}
	if len(copied.BlockedWritePaths) != 1 || copied.BlockedWritePaths[0] != "Workflow/demo/planning" {
		t.Fatalf("blocked write paths not copied: %+v", copied)
	}
}
