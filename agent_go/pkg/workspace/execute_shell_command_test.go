package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

func TestExecuteShellCommand_RewritesGeminiRelativePathsFromSession(t *testing.T) {
	sessionID := "test-gemini-session"
	projectDirID := "1774194481802-85102"
	SetSessionWorkingDir(sessionID, "Workflow/HRMS")
	SetSessionGeminiProjectDirID(sessionID, projectDirID)
	defer ClearSessionShellConfig(sessionID)

	var got ExecuteShellCommandParams
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/execute" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"stdout":            "ok",
				"stderr":            "",
				"exit_code":         0,
				"execution_time_ms": 1,
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)

	result, err := client.ExecuteShellCommand(ctx, ExecuteShellCommandParams{
		Command: "cat .gemini/policies/restrict-tools.toml",
	})
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned error: %v", err)
	}
	if result.Stdout == "" && result.Stderr == "" {
		t.Fatalf("expected non-empty response, got empty ShellCommandResult")
	}

	expectedCommand := "cat " + geminiProjectDirPath(projectDirID) + "/.gemini/policies/restrict-tools.toml"
	if got.Command != expectedCommand {
		t.Fatalf("expected rewritten command %q, got %q", expectedCommand, got.Command)
	}
	if got.WorkingDirectory != "Workflow/HRMS" {
		t.Fatalf("expected working directory Workflow/HRMS, got %q", got.WorkingDirectory)
	}
	if got.ExtraEnv["GEMINI_PROJECT_DIR"] != geminiProjectDirPath(projectDirID) {
		t.Fatalf("expected GEMINI_PROJECT_DIR %q, got %q", geminiProjectDirPath(projectDirID), got.ExtraEnv["GEMINI_PROJECT_DIR"])
	}
}

func TestRewriteGeminiRelativePathsLeavesAbsolutePathsUntouched(t *testing.T) {
	projectDirID := "1774194481802-85102"
	absolutePath := geminiProjectDirPath(projectDirID) + "/.gemini/policies/restrict-tools.toml"
	command := "cat " + absolutePath

	rewritten := rewriteGeminiRelativePaths(command, projectDirID)
	if rewritten != command {
		t.Fatalf("expected absolute command to stay unchanged, got %q", rewritten)
	}
}

func TestRewriteGeminiRelativePaths_RewritesParentRelativePaths(t *testing.T) {
	projectDirID := "1774194481802-85102"
	command := "cat ../.gemini/policies/restrict-tools.toml"

	rewritten := rewriteGeminiRelativePaths(command, projectDirID)
	expected := "cat " + geminiProjectDirPath(projectDirID) + "/.gemini/policies/restrict-tools.toml"
	if rewritten != expected {
		t.Fatalf("expected parent-relative command %q, got %q", expected, rewritten)
	}
}

func TestRedactShellCommandForLog(t *testing.T) {
	command := `python3 <<'PY'
KEY = "sk-api-abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
headers = {"Authorization": "Bearer abcdefghijklmnopqrstuvwxyz1234567890"}
api_key = "AIzaSyABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"
PY`

	redacted := redactShellCommandForLog(command)
	for _, forbidden := range []string{"sk-api-abcdefghijklmnopqrstuvwxyz", "Bearer abcdefghijklmnopqrstuvwxyz", "AIzaSyABCDEFGHIJKLMNOPQRSTUVWXYZ"} {
		if strings.Contains(redacted, forbidden) {
			t.Fatalf("expected secret fragment %q to be redacted from %q", forbidden, redacted)
		}
	}
	if strings.Count(redacted, "[REDACTED]") < 3 {
		t.Fatalf("expected multiple redactions, got %q", redacted)
	}
}

func TestDetectRawChromeCDPAccessInHeredoc(t *testing.T) {
	command := `python3 <<'PY'
import websocket
tab_id = "abc"
ws = websocket.create_connection(
    f"ws://localhost:9222/devtools/page/{tab_id}",
    header=["Host: localhost"],
)
PY`

	if got := detectRawChromeCDPAccess(command); got == "" {
		t.Fatal("expected raw CDP access to be detected inside heredoc")
	}
}

func TestDetectRawChromeCDPAccessIgnoresUnrelatedWebSocket(t *testing.T) {
	command := `python3 <<'PY'
import websocket
ws = websocket.create_connection("wss://example.com/events")
PY`

	if got := detectRawChromeCDPAccess(command); got != "" {
		t.Fatalf("expected unrelated websocket to pass, got %q", got)
	}
}

func TestContainsAgentBrowserInvocationAllowsReadOnlySkillsDocs(t *testing.T) {
	allowed := []string{
		"agent-browser skills list",
		"agent-browser skills get core",
		"agent-browser skills get core --full",
		"agent-browser skills get electron --full",
	}
	for _, command := range allowed {
		if containsAgentBrowserInvocation(command) {
			t.Fatalf("expected %q to be allowed", command)
		}
	}
}

func TestContainsAgentBrowserInvocationStillBlocksBrowserActions(t *testing.T) {
	blocked := []string{
		"agent-browser open https://example.com",
		"agent-browser snapshot -i",
		"agent-browser skills get core && agent-browser open https://example.com",
		"agent-browser skills get core | cat",
	}
	for _, command := range blocked {
		if !containsAgentBrowserInvocation(command) {
			t.Fatalf("expected %q to be blocked", command)
		}
	}
}

func TestDetectRawChromeCDPAccessAllowsPlanTextMentioningCDPVariable(t *testing.T) {
	command := `cat > /tmp/plan.py <<'PY'
description = """
VAR: TWITTER_CDP_URL
node learnings/step-post-twitter-reply/scripts/post_reply_v2.js $VAR_TWITTER_CDP_URL $tweet_url $payload_path.
"""
PY`

	if got := detectRawChromeCDPAccess(command); got != "" {
		t.Fatalf("expected CDP variable mentions in plan text to pass, got %q", got)
	}
}

func TestDetectRawChromeCDPAccessAllowsDocumentationTextWithEndpoints(t *testing.T) {
	command := `cat > /tmp/browser-docs.md <<'EOF'
Do not use ws://localhost:9222/devtools/page/<id>.
Do not call websocket.create_connection("ws://localhost:9222/devtools/page/<id>").
Do not read http://localhost:9222/json/list directly.
EOF`

	if got := detectRawChromeCDPAccess(command); got != "" {
		t.Fatalf("expected documentation text to pass, got %q", got)
	}
}

func TestDetectRawChromeCDPAccessBlocksCurlJSONEndpoint(t *testing.T) {
	command := `curl -sS http://localhost:9222/json/list`

	if got := detectRawChromeCDPAccess(command); got != "Chrome /json target endpoint" {
		t.Fatalf("expected curl to Chrome JSON endpoint to be blocked, got %q", got)
	}
}

func TestDetectRawChromeCDPAccessBlocksCDPMethodWithVariableInExecutableHeredoc(t *testing.T) {
	command := `python3 <<'PY'
import os
method = "Target.createTarget"
url = os.environ["TWITTER_CDP_URL"]
print(method, url)
PY`

	if got := detectRawChromeCDPAccess(command); got != "Chrome CDP method call" {
		t.Fatalf("expected CDP method with CDP URL variable to be blocked, got %q", got)
	}
}

func TestExecuteShellCommand_BlocksRawChromeCDPAccess(t *testing.T) {
	client := NewClient("http://127.0.0.1:1")
	result, err := client.ExecuteShellCommand(context.Background(), ExecuteShellCommandParams{
		Command: `python3 -c 'import urllib.request; print(urllib.request.urlopen("http://localhost:9222/json/list").read())'`,
	})
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned Go error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "Raw Chrome CDP access") || !strings.Contains(result.Stderr, "agent_browser") {
		t.Fatalf("expected actionable raw CDP error, got %q", result.Stderr)
	}
}

func TestBlockAbsoluteHostPaths_AllowsAbsoluteWorkspacePathOutsideGuardForSandboxEnforcement(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs")

	guard := &FolderGuardConfig{
		Enabled:    true,
		ReadPaths:  []string{"Workflow/testing/runs/iteration-0/test-group/execution"},
		WritePaths: []string{"Workflow/testing/runs/iteration-0/test-group/execution/math-solver"},
	}

	err := blockAbsoluteHostPaths(
		`cat '/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs/Workflow/testing/workflow.json'`,
		guard,
	)
	if err != nil {
		t.Fatalf("expected absolute workspace-docs path to pass through to sandbox enforcement, got: %v", err)
	}
}

func TestBlockAbsoluteHostPaths_DeniesAbsoluteHostPathOutsideWorkspaceDocs(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs")

	guard := &FolderGuardConfig{
		Enabled:    true,
		ReadPaths:  []string{"Workflow/testing/runs/iteration-0/test-group/execution"},
		WritePaths: []string{"Workflow/testing/runs/iteration-0/test-group/execution/math-solver"},
	}

	err := blockAbsoluteHostPaths(
		`cat '/Users/mipl/.ssh/id_rsa'`,
		guard,
	)
	if err == nil {
		t.Fatal("expected absolute host path outside workspace-docs to be rejected")
	}
	if !strings.Contains(err.Error(), "absolute host path") {
		t.Fatalf("expected absolute host path error, got: %v", err)
	}
}

func TestBlockAbsoluteHostPaths_AllowsExplicitReadOnlyHostDownloadsGrant(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs")

	guard := &FolderGuardConfig{
		Enabled:           true,
		ReadPaths:         []string{"/Users/mipl/Downloads"},
		WritePaths:        []string{"Workflow/testing/runs/iteration-0/test-group/execution/Downloads"},
		BlockedWritePaths: []string{"/Users/mipl/Downloads"},
	}

	err := blockAbsoluteHostPaths(
		`cp '/Users/mipl/Downloads/statement.pdf' Workflow/testing/runs/iteration-0/test-group/execution/Downloads/statement.pdf`,
		guard,
	)
	if err != nil {
		t.Fatalf("expected explicit read-only host Downloads grant to pass, got: %v", err)
	}
}

func TestBlockAbsoluteHostPaths_DeniesHostReadPathWithoutWriteBlock(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs")

	guard := &FolderGuardConfig{
		Enabled:    true,
		ReadPaths:  []string{"/Users/mipl/Downloads"},
		WritePaths: []string{"Workflow/testing/runs/iteration-0/test-group/execution/Downloads"},
	}

	err := blockAbsoluteHostPaths(
		`cp '/Users/mipl/Downloads/statement.pdf' Workflow/testing/runs/iteration-0/test-group/execution/Downloads/statement.pdf`,
		guard,
	)
	if err == nil {
		t.Fatal("expected host read path without blocked-write protection to be rejected")
	}
}

func TestBlockAbsoluteHostPaths_AllowsAbsoluteWorkspacePathInsideGuardAndIgnoresHeredocData(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs")

	guard := &FolderGuardConfig{
		Enabled:    true,
		ReadPaths:  []string{"Workflow/testing/runs/iteration-0/test-group/execution"},
		WritePaths: []string{"Workflow/testing/runs/iteration-0/test-group/execution/math-solver"},
	}

	command := `cat '/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs/Workflow/testing/runs/iteration-0/test-group/execution/prepare-test-fixtures/test_fixtures.json'
cat > '/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs/Workflow/testing/runs/iteration-0/test-group/execution/math-solver/math_probe.json' <<'EOF'
{
  "attempted_path": "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs/Workflow/testing/workflow.json"
}
EOF`

	if err := blockAbsoluteHostPaths(command, guard); err != nil {
		t.Fatalf("expected allowed absolute execution paths to pass, got: %v", err)
	}
}

// TestExecuteShellCommand_InjectsSessionEnv proves that per-session shell env
// (e.g. DB_PATH set by the workflow orchestrator) reaches the bridge request,
// and that an explicit per-call extra_env overrides the session value.
func TestExecuteShellCommand_InjectsSessionEnv(t *testing.T) {
	sessionID := "test-session-env"
	common.SetSessionShellEnv(sessionID, map[string]string{
		"DB_PATH":         "/abs/workflow/db/db.sqlite",
		"STEP_OUTPUT_DIR": "/abs/workspace-docs/Workflow/test-workflow/runs/iteration-0/default/execution/step-score",
	})
	defer ClearSessionShellConfig(sessionID)

	var got ExecuteShellCommandParams
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data":    map[string]interface{}{"stdout": "ok", "exit_code": 0},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)

	// Per-call extra_env sets DB_PATH explicitly — it must win over the session value.
	if _, err := client.ExecuteShellCommand(ctx, ExecuteShellCommandParams{
		Command:  `sqlite3 "$DB_PATH" "select 1"`,
		ExtraEnv: map[string]string{"DB_PATH": "/per/call/override.sqlite"},
	}); err != nil {
		t.Fatalf("ExecuteShellCommand error: %v", err)
	}

	if got.ExtraEnv["DB_PATH"] != "/per/call/override.sqlite" {
		t.Fatalf("per-call extra_env should win: DB_PATH=%q", got.ExtraEnv["DB_PATH"])
	}
	if got.ExtraEnv["STEP_OUTPUT_DIR"] != "/abs/workspace-docs/Workflow/test-workflow/runs/iteration-0/default/execution/step-score" {
		t.Fatalf("session STEP_OUTPUT_DIR not injected: %q", got.ExtraEnv["STEP_OUTPUT_DIR"])
	}
	if got.ExtraEnv["RUNLOOP_OWNER"] != "workflow" {
		t.Fatalf("RUNLOOP_OWNER not inferred: %q", got.ExtraEnv["RUNLOOP_OWNER"])
	}
	if got.ExtraEnv["RUNLOOP_WORKFLOW_ID"] != "test-workflow" {
		t.Fatalf("RUNLOOP_WORKFLOW_ID not inferred: %q", got.ExtraEnv["RUNLOOP_WORKFLOW_ID"])
	}
	if got.ExtraEnv["RUNLOOP_RUN_ID"] != "iteration-0/default" {
		t.Fatalf("RUNLOOP_RUN_ID not inferred: %q", got.ExtraEnv["RUNLOOP_RUN_ID"])
	}
	if got.ExtraEnv["RUNLOOP_STEP_ID"] != "step-score" {
		t.Fatalf("RUNLOOP_STEP_ID not inferred: %q", got.ExtraEnv["RUNLOOP_STEP_ID"])
	}
	if got.ExtraEnv["RUNLOOP_SESSION_ID"] != sessionID {
		t.Fatalf("RUNLOOP_SESSION_ID not set: %q", got.ExtraEnv["RUNLOOP_SESSION_ID"])
	}

	// Without a per-call override, the session DB_PATH must be injected.
	got = ExecuteShellCommandParams{}
	if _, err := client.ExecuteShellCommand(ctx, ExecuteShellCommandParams{Command: `sqlite3 "$DB_PATH" "select 1"`}); err != nil {
		t.Fatalf("ExecuteShellCommand error: %v", err)
	}
	if got.ExtraEnv["DB_PATH"] != "/abs/workflow/db/db.sqlite" {
		t.Fatalf("session DB_PATH not injected: %q", got.ExtraEnv["DB_PATH"])
	}
}

func TestExecuteShellCommand_PassesCDPHostDownloadsReadOnlyGuard(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs")
	t.Setenv("PI_HOST_DOWNLOADS_PATH", "/Users/mipl/Downloads")

	sessionID := "test-cdp-downloads"
	common.SetSessionFolderGuard(
		sessionID,
		[]string{"Workflow/testing/runs/iteration-0/test-group/execution"},
		[]string{"Workflow/testing/runs/iteration-0/test-group/execution/Downloads"},
	)
	common.GrantSessionCDPHostDownloadsReadOnly(sessionID, "cdp")
	defer ClearSessionShellConfig(sessionID)

	var got ExecuteShellCommandParams
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data":    map[string]interface{}{"stdout": "ok", "exit_code": 0},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)
	if _, err := client.ExecuteShellCommand(ctx, ExecuteShellCommandParams{
		Command: `cp '/Users/mipl/Downloads/statement.pdf' Workflow/testing/runs/iteration-0/test-group/execution/Downloads/statement.pdf`,
	}); err != nil {
		t.Fatalf("ExecuteShellCommand error: %v", err)
	}

	if got.FolderGuard == nil {
		t.Fatal("expected folder guard in execute request")
	}
	if !containsString(got.FolderGuard.ReadPaths, "/Users/mipl/Downloads") {
		t.Fatalf("expected host Downloads in read paths, got %v", got.FolderGuard.ReadPaths)
	}
	if containsString(got.FolderGuard.WritePaths, "/Users/mipl/Downloads") {
		t.Fatalf("host Downloads must not be writable, got write paths %v", got.FolderGuard.WritePaths)
	}
	if !containsString(got.FolderGuard.BlockedWritePaths, "/Users/mipl/Downloads") {
		t.Fatalf("expected host Downloads in blocked-write paths, got %v", got.FolderGuard.BlockedWritePaths)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestIsGitPushCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"git push", true},
		{"git push origin main", true},
		{"cd repo && git pull --no-edit && git push", true},
		{`git -C /x push`, true},
		{"git status", false},
		{"git pull", false},
		{`echo "git push" >> notes.txt`, false},      // quoted data, not executable
		{"python3 -c 'print(\"git push\")'", false},  // inside quotes
		{"git commit -m 'do git push later'", false}, // mentioned in message
	}
	for _, c := range cases {
		if got := isGitPushCommand(c.cmd); got != c.want {
			t.Errorf("isGitPushCommand(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestIsWorkflowSecretPath(t *testing.T) {
	secret := []string{"secrets.json", "db/secrets.json", "workflow_secrets/x", "workflow_secrets/a/b.txt",
		"a/workflow_secrets/c", "key.pem", "id_rsa.key", "gh.token", ".env", ".env.production", "credentials", "credentials.json"}
	notSecret := []string{"plan.json", "planning/plan.json", "knowledgebase/notes/x.md", "report.html",
		"keyboard.md", "credentialspolicy.md", "db/db.sqlite", "env.go"}
	for _, p := range secret {
		if !isWorkflowSecretPath(p) {
			t.Errorf("isWorkflowSecretPath(%q) = false, want true", p)
		}
	}
	for _, p := range notSecret {
		if isWorkflowSecretPath(p) {
			t.Errorf("isWorkflowSecretPath(%q) = true, want false", p)
		}
	}
}

func TestParseGitHubOwnerRepo(t *testing.T) {
	cases := []struct {
		url, owner, repo string
		ok               bool
	}{
		{"https://github.com/manishiitg/mcp-agent-builder-go.git", "manishiitg", "mcp-agent-builder-go", true},
		{"git@github.com:owner/repo.git", "owner", "repo", true},
		{"ssh://git@github.com/owner/repo", "owner", "repo", true},
		{"https://github.com/owner/repo", "owner", "repo", true},
		{"https://gitlab.com/owner/repo.git", "", "", false},
		{"https://example.com/x/y", "", "", false},
	}
	for _, c := range cases {
		o, r, ok := parseGitHubOwnerRepo(c.url)
		if ok != c.ok || o != c.owner || r != c.repo {
			t.Errorf("parseGitHubOwnerRepo(%q) = (%q,%q,%v), want (%q,%q,%v)", c.url, o, r, ok, c.owner, c.repo, c.ok)
		}
	}
}
