package common

import (
	"slices"
	"strings"
	"testing"
)

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

func TestResolveBrowserSessionIDUsesInstancePrefix(t *testing.T) {
	t.Setenv("AGENTWORKS_BROWSER_SESSION_PREFIX", "video-product-dev")

	if got := ResolveBrowserSessionID("", "isolated-123"); got != "video-product-dev--isolated-123" {
		t.Fatalf("explicit browser session should be instance-qualified, got %q", got)
	}
	if got := ResolveBrowserSessionID("", "video-product-dev--isolated-123"); got != "video-product-dev--isolated-123" {
		t.Fatalf("already-qualified browser session should not be prefixed twice, got %q", got)
	}
}

func TestBrowserSessionsSharedWithinUserAndIsolatedAcrossUsers(t *testing.T) {
	manishChat := "browser-isolation-manish"
	shubhamChat := "browser-isolation-shubham"
	secondManishChat := "browser-isolation-manish-two"
	for _, sessionID := range []string{manishChat, shubhamChat, secondManishChat} {
		t.Cleanup(func() { ClearSessionShellConfig(sessionID) })
	}

	BindSessionBrowserIsolation(manishChat, "manish-user-id")
	BindSessionBrowserIsolation(shubhamChat, "shubham-user-id")
	BindSessionBrowserIsolation(secondManishChat, "manish-user-id")

	manishDefault := ResolveBrowserSessionID(manishChat, "default")
	shubhamDefault := ResolveBrowserSessionID(shubhamChat, "default")
	if manishDefault == shubhamDefault {
		t.Fatalf("different users shared default browser %q", manishDefault)
	}
	if manishDefault != ResolveBrowserSessionID(secondManishChat, "default") {
		t.Fatalf("same user got different browsers: %q", manishDefault)
	}
	if ResolveBrowserSessionID(manishChat, "research") == ResolveBrowserSessionID(shubhamChat, "research") {
		t.Fatal("explicit browser names were not scoped to the authenticated user/chat")
	}
}

func TestBindSessionBrowserIsolationOverridesWorkflowDefault(t *testing.T) {
	sessionID := "browser-isolation-workflow"
	t.Cleanup(func() { ClearSessionShellConfig(sessionID) })
	BindSessionBrowserIsolation(sessionID, "user-id")
	SetSessionBrowserSessionID(sessionID, "workflow-browser-stable")

	BindSessionBrowserIsolation(sessionID, "user-id")
	if got := ResolveBrowserSessionID(sessionID, "default"); got != PrefixBrowserSessionID(BrowserSessionNamespace("user-id", sessionID)+"--browser") {
		t.Fatalf("workflow override escaped user browser: %q", got)
	}
}

func TestBrowserSessionIsolationComposesWithDeploymentPrefix(t *testing.T) {
	t.Setenv("AGENTWORKS_BROWSER_SESSION_PREFIX", "confida")
	sessionID := "browser-isolation-prefix"
	t.Cleanup(func() { ClearSessionShellConfig(sessionID) })
	BindSessionBrowserIsolation(sessionID, "user-id")

	got := ResolveBrowserSessionID(sessionID, "default")
	if !strings.HasPrefix(got, "confida--user-") {
		t.Fatalf("deployment prefix was not applied to isolated browser: %q", got)
	}
}

func TestGrantSessionCDPHostDownloadsReadWriteWidensPriorReadOnlyGrant(t *testing.T) {
	t.Setenv("PI_HOST_DOWNLOADS_PATH", "/tmp/test-host-downloads")
	sessionID := "cdp-host-downloads-read-write"
	t.Cleanup(func() { ClearSessionShellConfig(sessionID) })
	SetSessionFolderGuard(sessionID, []string{"Workflow/test"}, []string{"Workflow/test"})
	SetSessionFolderGuardBlockedWritePaths(sessionID, []string{"/tmp/test-host-downloads"})

	if got := GrantSessionCDPHostDownloadsReadWrite(sessionID, "cdp"); got != "/tmp/test-host-downloads" {
		t.Fatalf("grant path = %q", got)
	}
	config := GetSessionShellConfig(sessionID)
	if !slices.Contains(config.ReadPaths, "/tmp/test-host-downloads") || !slices.Contains(config.WritePaths, "/tmp/test-host-downloads") {
		t.Fatalf("host Downloads was not added to read and write paths: %#v", config)
	}
	if slices.Contains(config.BlockedWritePaths, "/tmp/test-host-downloads") {
		t.Fatalf("stale read-only deny remained after widening: %#v", config.BlockedWritePaths)
	}
	if got := GrantSessionCDPHostDownloadsReadWrite(sessionID, "headless"); got != "" {
		t.Fatalf("headless mode unexpectedly granted host Downloads: %q", got)
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

func TestReconcileSessionWorkflowFolderAccessIsWorkflowScoped(t *testing.T) {
	const first = "folder-reconcile-first"
	const second = "folder-reconcile-second"
	defer ClearSessionShellConfig(first)
	defer ClearSessionShellConfig(second)
	shared := "/tmp/shared-grant"
	SetSessionWorkflowPath(first, "Workflow/first")
	SetSessionFolderGuard(first, []string{"Workflow/first", shared}, []string{shared})
	SetSessionFolderGuardBlockedWritePaths(first, []string{"Workflow/first/planning", shared})
	SetSessionShellEnv(first, map[string]string{"WORKFLOW_FOLDER_SHARED": shared})
	SetSessionWorkflowPath(second, "Workflow/second")
	SetSessionFolderGuard(second, []string{"Workflow/second", shared}, []string{shared})
	SetSessionShellEnv(second, map[string]string{"WORKFLOW_FOLDER_SHARED": shared})

	ReconcileSessionWorkflowFolderAccess("Workflow/first", []string{shared}, nil, nil, nil, nil)
	firstConfig := GetSessionShellConfig(first)
	if len(firstConfig.ReadPaths) != 1 || firstConfig.ReadPaths[0] != "Workflow/first" || len(firstConfig.WritePaths) != 0 {
		t.Fatalf("first workflow grant was not revoked: %#v", firstConfig)
	}
	if len(firstConfig.BlockedWritePaths) != 1 || firstConfig.BlockedWritePaths[0] != "Workflow/first/planning" {
		t.Fatalf("revoked read-only deny was not removed cleanly: %#v", firstConfig.BlockedWritePaths)
	}
	if _, exists := firstConfig.Env["WORKFLOW_FOLDER_SHARED"]; exists {
		t.Fatal("revoked alias remained in first workflow environment")
	}
	ReconcileSessionWorkflowFolderAccess(
		"Workflow/first", nil, []string{shared}, nil, []string{shared},
		map[string]string{"WORKFLOW_FOLDER_SHARED": shared},
	)
	firstConfig = GetSessionShellConfig(first)
	if len(firstConfig.WritePaths) != 0 || !slices.Contains(firstConfig.ReadPaths, shared) || !slices.Contains(firstConfig.BlockedWritePaths, shared) {
		t.Fatalf("read-only grant was not applied live: %#v", firstConfig)
	}
	ReconcileSessionWorkflowFolderAccess(
		"Workflow/first", []string{shared}, []string{shared}, []string{shared}, nil,
		map[string]string{"WORKFLOW_FOLDER_SHARED": shared},
	)
	firstConfig = GetSessionShellConfig(first)
	if !slices.Contains(firstConfig.WritePaths, shared) || slices.Contains(firstConfig.BlockedWritePaths, shared) {
		t.Fatalf("read-only grant was not widened cleanly: %#v", firstConfig)
	}
	secondConfig := GetSessionShellConfig(second)
	if len(secondConfig.WritePaths) != 1 || secondConfig.WritePaths[0] != shared || secondConfig.Env["WORKFLOW_FOLDER_SHARED"] != shared {
		t.Fatalf("second workflow was incorrectly changed: %#v", secondConfig)
	}
}

func TestReconcileSessionWorkflowFolderAccessFindsLegacyUntaggedWorkflowSession(t *testing.T) {
	sessionID := "legacy-untagged-workflow-session"
	defer ClearSessionShellConfig(sessionID)
	SetSessionWorkingDir(sessionID, "Workflow/websiteaeo")
	SetSessionFolderGuard(sessionID, []string{"Workflow/websiteaeo"}, []string{"Workflow/websiteaeo"})
	external := "/tmp/public-website"

	ReconcileSessionWorkflowFolderAccess(
		"Workflow/websiteaeo", nil, []string{external}, []string{external}, nil,
		map[string]string{"WORKFLOW_FOLDER_PUBLIC_WEBSITE": external},
	)

	config := GetSessionShellConfig(sessionID)
	if config.WorkflowPath != "Workflow/websiteaeo" || !slices.Contains(config.ReadPaths, external) || !slices.Contains(config.WritePaths, external) {
		t.Fatalf("legacy session was not adopted and widened: %#v", config)
	}
	if config.Env["WORKFLOW_FOLDER_PUBLIC_WEBSITE"] != external {
		t.Fatalf("legacy session did not receive alias environment: %#v", config.Env)
	}
}

func TestApplySessionWorkflowFolderAccessRefreshesAndRevokesCurrentGrant(t *testing.T) {
	sessionID := "refresh-current-workflow-session"
	defer ClearSessionShellConfig(sessionID)
	SetSessionFolderGuard(sessionID, []string{"Workflow/websiteaeo"}, []string{"Workflow/websiteaeo"})
	external := "/tmp/public-website"
	env := map[string]string{"WORKFLOW_FOLDER_PUBLIC_WEBSITE": external}

	ApplySessionWorkflowFolderAccess(sessionID, "Workflow/websiteaeo", []string{external}, []string{external}, nil, env)
	config := GetSessionShellConfig(sessionID)
	if !slices.Contains(config.ReadPaths, external) || !slices.Contains(config.WritePaths, external) {
		t.Fatalf("current grant was not applied: %#v", config)
	}

	ApplySessionWorkflowFolderAccess(sessionID, "Workflow/websiteaeo", []string{external}, nil, []string{external}, env)
	config = GetSessionShellConfig(sessionID)
	if slices.Contains(config.WritePaths, external) || !slices.Contains(config.BlockedWritePaths, external) {
		t.Fatalf("current grant was not narrowed to read-only: %#v", config)
	}

	ApplySessionWorkflowFolderAccess(sessionID, "Workflow/websiteaeo", nil, nil, nil, nil)
	config = GetSessionShellConfig(sessionID)
	if slices.Contains(config.ReadPaths, external) || slices.Contains(config.WritePaths, external) || slices.Contains(config.BlockedWritePaths, external) {
		t.Fatalf("current grant was not revoked: %#v", config)
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

func TestUserBrowserAliasesAndChildSessions(t *testing.T) {
	t.Setenv("AGENTWORKS_BROWSER_SESSION_PREFIX", "confida")
	BindSessionBrowserIsolation("parent", "alice")
	SetSessionBrowserNamespace("child", GetSessionShellConfig("parent").BrowserSessionNamespace)
	SetSessionBrowserSessionID("child", "obsolete-workflow-browser")
	defer ClearSessionShellConfig("parent")
	defer ClearSessionShellConfig("child")
	expected := ResolveBrowserSessionID("parent", "default")
	for _, sid := range []string{"parent", "child"} {
		for _, name := range []string{"", "default", "main", "ai-news", expected, "user-someone-else--browser"} {
			if got := ResolveBrowserSessionID(sid, name); got != expected {
				t.Fatalf("%s/%s: %s != %s", sid, name, got, expected)
			}
		}
	}
	BindSessionBrowserIsolation("parent", "bob")
	if ResolveBrowserSessionID("parent", "main") == expected {
		t.Fatal("ownership change kept old user browser")
	}
	if BrowserSessionNamespace("", "guest-one") == BrowserSessionNamespace("", "guest-two") {
		t.Fatal("anonymous chats share cookies")
	}
}
