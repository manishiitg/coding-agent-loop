package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPulseWorklistUsesWorkflowLocalDB(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/example"
	dbPath := filepath.Join(root, "Workflow", "example", "db", "db.sqlite")

	states, err := getPulseModuleStates(ctx, workspacePath)
	if err != nil {
		t.Fatalf("list before create: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("list before create returned %d states, want 0", len(states))
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("read-only list created db unexpectedly: stat err=%v", err)
	}

	recorded, err := recordPulseWorklist(ctx, workspacePath, "pulse-run-1", []PulseWorklistDecision{
		{
			Module:       pulseModuleHarden,
			Due:          true,
			Reason:       "Latest run skipped a required step.",
			Evidence:     []string{"runs/iteration-0/logs/step-a"},
			CooldownRuns: 0,
		},
		{
			Module:       pulseModuleLearningHealth,
			Due:          false,
			Reason:       "No plan or selector change since the last reviewed run.",
			Evidence:     []string{"planning/changelog"},
			CooldownRuns: 2,
		},
	})
	if err != nil {
		t.Fatalf("record worklist: %v", err)
	}
	if len(recorded) != 2 {
		t.Fatalf("recorded states = %d, want 2", len(recorded))
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected workflow-local db at %s: %v", dbPath, err)
	}

	worklist, ok, err := getPulseWorklistForRun(ctx, workspacePath, "pulse-run-1")
	if err != nil {
		t.Fatalf("get worklist: %v", err)
	}
	if !ok {
		t.Fatal("get worklist ok=false, want true")
	}
	if got := worklist[pulseModuleHarden].LastDecision; got != "due" {
		t.Fatalf("harden decision = %q, want due", got)
	}
	if got := worklist[pulseModuleLearningHealth].LastDecision; got != "skipped" {
		t.Fatalf("learning decision = %q, want skipped", got)
	}

	updated, err := markPulseModuleResult(ctx, workspacePath, pulseModuleHarden, "pulse-run-1", "changed", "Harden fixed the skipped step.", []string{"builder/improve.html#decision"})
	if err != nil {
		t.Fatalf("mark result: %v", err)
	}
	if updated.LastDecision != "changed" || updated.LastRanAt == "" {
		t.Fatalf("updated state mismatch: %+v", updated)
	}
}
