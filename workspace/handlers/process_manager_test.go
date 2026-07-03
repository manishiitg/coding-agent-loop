package handlers

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestInferWorkflowOwnerFromPath(t *testing.T) {
	owner := inferWorkflowOwnerFromPath("/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs/Workflow/tectonicusadaytrading/runs/iteration-0/default/execution/score-and-plan/output.json")

	if owner.WorkflowID != "tectonicusadaytrading" {
		t.Fatalf("WorkflowID = %q", owner.WorkflowID)
	}
	if owner.RunID != "iteration-0/default" {
		t.Fatalf("RunID = %q", owner.RunID)
	}
	if owner.StepID != "score-and-plan" {
		t.Fatalf("StepID = %q", owner.StepID)
	}
}

func TestOwnerFromShellRequestUsesRunloopEnvBeforePath(t *testing.T) {
	owner := ownerFromShellRequest(map[string]string{
		"RUNLOOP_WORKFLOW_ID": "explicit-workflow",
		"RUNLOOP_RUN_ID":      "explicit-run",
		"RUNLOOP_STEP_ID":     "explicit-step",
		"RUNLOOP_SESSION_ID":  "session-1",
		"STEP_OUTPUT_DIR":     "/workspace-docs/Workflow/path-workflow/runs/iteration-0/default/execution/path-step",
	}, "", "")

	if owner.WorkflowID != "explicit-workflow" || owner.RunID != "explicit-run" || owner.StepID != "explicit-step" {
		t.Fatalf("explicit RUNLOOP env was not preserved: %#v", owner)
	}
	if owner.SessionID != "session-1" {
		t.Fatalf("SessionID = %q", owner.SessionID)
	}
}

func TestParsePSElapsed(t *testing.T) {
	tests := map[string]time.Duration{
		"05:12":       5*time.Minute + 12*time.Second,
		"11:53:34":    11*time.Hour + 53*time.Minute + 34*time.Second,
		"03-11:53:34": 3*24*time.Hour + 11*time.Hour + 53*time.Minute + 34*time.Second,
	}
	for input, want := range tests {
		got, err := parsePSElapsed(input)
		if err != nil {
			t.Fatalf("parsePSElapsed(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("parsePSElapsed(%q) = %s, want %s", input, got, want)
		}
	}
}

func TestParsePSLine(t *testing.T) {
	line := "98265     1 03-11:53:34 sh -c cd /workspace-docs/Workflow/w/runs/iteration-0/default/execution/step"
	pid, ppid, elapsed, command, ok := parsePSLine(line)
	if !ok {
		t.Fatal("parsePSLine returned !ok")
	}
	if pid != 98265 || ppid != 1 {
		t.Fatalf("pid/ppid = %d/%d", pid, ppid)
	}
	if elapsed != 3*24*time.Hour+11*time.Hour+53*time.Minute+34*time.Second {
		t.Fatalf("elapsed = %s", elapsed)
	}
	if command != "sh -c cd /workspace-docs/Workflow/w/runs/iteration-0/default/execution/step" {
		t.Fatalf("command = %q", command)
	}
}

func TestPersistManagedProcessRoundTrip(t *testing.T) {
	docsDir := t.TempDir()
	previousDocsDir := viper.GetString("docs-dir")
	viper.Set("docs-dir", docsDir)
	t.Cleanup(func() { viper.Set("docs-dir", previousDocsDir) })

	record := ManagedProcess{
		PID:        12345,
		PGID:       12345,
		Command:    "sleep 999",
		WorkingDir: docsDir,
		StartedAt:  time.Now().Add(-3 * time.Hour),
		Owner: ProcessOwner{
			Owner:      "workflow",
			WorkflowID: "wf-1",
			RunID:      "iteration-0/default",
			StepID:     "step-a",
		},
		Status: "running",
	}

	if err := persistManagedProcess(record); err != nil {
		t.Fatalf("persistManagedProcess: %v", err)
	}
	records := readPersistedProcessRecords(docsDir)
	if len(records) != 1 {
		t.Fatalf("readPersistedProcessRecords len = %d", len(records))
	}
	if records[0].PID != record.PID || records[0].Owner.WorkflowID != "wf-1" {
		t.Fatalf("unexpected record: %#v", records[0])
	}

	removePersistedProcessRecord(docsDir, record.PID)
	if records := readPersistedProcessRecords(docsDir); len(records) != 0 {
		t.Fatalf("record was not removed: %#v", records)
	}
}

func TestPersistManagedProcessSkipsNonWorkflow(t *testing.T) {
	docsDir := t.TempDir()
	previousDocsDir := viper.GetString("docs-dir")
	viper.Set("docs-dir", docsDir)
	t.Cleanup(func() { viper.Set("docs-dir", previousDocsDir) })

	if err := persistManagedProcess(ManagedProcess{PID: 12345, Command: "sleep 999"}); err != nil {
		t.Fatalf("persistManagedProcess: %v", err)
	}
	if records := readPersistedProcessRecords(docsDir); len(records) != 0 {
		t.Fatalf("non-workflow process should not be persisted: %#v", records)
	}
}

func TestProcessLooksReused(t *testing.T) {
	record := ManagedProcess{StartedAt: time.Now().Add(-3 * time.Hour)}
	if !processLooksReused(record, 10*time.Minute) {
		t.Fatal("expected short elapsed process to look reused")
	}
	if processLooksReused(record, 2*time.Hour+58*time.Minute) {
		t.Fatal("expected similar elapsed process to not look reused")
	}
}
