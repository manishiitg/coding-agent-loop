package step_based_workflow

import "testing"

func TestToAbsPathsLeavesAbsoluteHostPathsUntouched(t *testing.T) {
	got := toAbsPaths("/app/workspace-docs", []string{
		"Workflow/demo/execution",
		"/Users/mipl/Downloads",
	})

	if got[0] != "/app/workspace-docs/Workflow/demo/execution" {
		t.Fatalf("expected workspace-relative path to be rooted, got %q", got[0])
	}
	if got[1] != "/Users/mipl/Downloads" {
		t.Fatalf("expected absolute host path to stay untouched, got %q", got[1])
	}
}
