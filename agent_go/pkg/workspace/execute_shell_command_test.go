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
