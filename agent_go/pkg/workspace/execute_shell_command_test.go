package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if result == "" {
		t.Fatalf("expected formatted response, got empty string")
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
