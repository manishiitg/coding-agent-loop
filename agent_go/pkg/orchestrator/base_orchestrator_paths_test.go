package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePathForAllowedPathsAcceptsAbsoluteDocsPathFromEnv(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs")

	got, _, err := normalizePathForAllowedPaths(
		[]string{"Workflow/demo/learnings/_global"},
		"/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs/Workflow/demo/learnings/_global/SKILL.md",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "Workflow/demo/learnings/_global/SKILL.md"
	if got != want {
		t.Fatalf("normalizePathForAllowedPaths() = %q, want %q", got, want)
	}
}

func TestNormalizePathForAllowedPathsDiscoversSiblingWorkspaceDocs(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", "")
	root := t.TempDir()
	agentDir := filepath.Join(root, "agent_go")
	docsDir := filepath.Join(root, "workspace-docs")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(agentDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldCwd)
	})
	actualAgentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	actualDocsDir := filepath.Join(filepath.Dir(actualAgentDir), "workspace-docs")

	got, _, err := normalizePathForAllowedPaths(
		[]string{"Workflow/demo/learnings/_global"},
		filepath.Join(actualDocsDir, "Workflow/demo/learnings/_global/SKILL.md"),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "Workflow/demo/learnings/_global/SKILL.md"
	if got != want {
		t.Fatalf("normalizePathForAllowedPaths() = %q, want %q", got, want)
	}
}
