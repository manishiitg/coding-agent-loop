package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecidePulseFixRunStartsOnlyWhenThereIsSomethingToFix(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	if due, _ := decidePulseFixRun(pulseFixSignals{}, time.Time{}, 0, now); due {
		t.Fatal("a stable workflow with nothing to fix must get no fix run")
	}
	due, reason := decidePulseFixRun(pulseFixSignals{OpenIssues: 2, FailedRuns: 1}, time.Time{}, 0, now)
	if !due || !strings.Contains(reason, "2 open issue(s)") || !strings.Contains(reason, "1 failed scheduled run(s)") {
		t.Fatalf("open issues should start a fix run with a reason, got %v %q", due, reason)
	}
	if due, reason := decidePulseFixRun(pulseFixSignals{OpenIssues: 1}, now.Add(-time.Hour), 1, now); due || !strings.Contains(reason, "minimum gap") {
		t.Fatalf("a fix run within the minimum gap must wait, got %v %q", due, reason)
	}
	if due, reason := decidePulseFixRun(pulseFixSignals{NewConcerns: 1}, now.Add(-5*time.Hour), pulseFixRunMaxPerDay, now); due || !strings.Contains(reason, "daily limit") {
		t.Fatalf("the daily limit must stop further fix runs, got %v %q", due, reason)
	}
	if due, _ := decidePulseFixRun(pulseFixSignals{NewConcerns: 1}, now.Add(-4*time.Hour), 1, now); !due {
		t.Fatal("after the gap and under the limit a fix run should start")
	}
	// Fresh trouble is fixed quickly; an older backlog waits longer.
	if due, _ := decidePulseFixRun(pulseFixSignals{FailedRuns: 1}, now.Add(-2*time.Hour), 1, now); !due {
		t.Fatal("a new failed run two hours after the last fix run should start one")
	}
	if due, reason := decidePulseFixRun(pulseFixSignals{OpenIssues: 3}, now.Add(-2*time.Hour), 1, now); due || !strings.Contains(reason, "4h0m0s") {
		t.Fatalf("an unchanged backlog must wait the longer gap, got %v %q", due, reason)
	}
}

func TestPulseFixRunHistoryCountsTheLastDay(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	ctx := context.Background()
	ws := "Workflow/fix-history"
	now := time.Now().UTC()
	for i, at := range []time.Time{now.Add(-30 * time.Hour), now.Add(-6 * time.Hour), now.Add(-2 * time.Hour)} {
		if err := recordPulseFixRunStarted(ctx, ws, "fix-"+string(rune('a'+i)), "open issues", at); err != nil {
			t.Fatal(err)
		}
	}
	last, lastDay, err := pulseFixRunHistory(ctx, ws, now)
	if err != nil || lastDay != 2 || now.Sub(last) > 3*time.Hour {
		t.Fatalf("history = last %v, lastDay %d (%v); want the 2-hour-old run and 2 in the last day", last, lastDay, err)
	}
}

// The fix run records its own worklist: Technical due, Goal Work and
// Architecture left to the full Pulse, and Plan Drift due only when the drift
// checks require it.
func TestPulseFixRunWorklistMakesTechnicalDue(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	ctx := context.Background()
	ws := "Workflow/fix-worklist"
	planningDir := filepath.Join(root, ws, "planning")
	if err := os.MkdirAll(planningDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planningDir, "plan.json"), []byte(`{"steps":[{"id":"step-a","type":"regular"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planningDir, "step_config.json"), []byte(`{"steps":[{"id":"step-a"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recordPulseFixRunWorklist(ctx, ws, "fix-1", "1 open issue(s)"); err != nil {
		t.Fatalf("record fix-run worklist: %v", err)
	}
	for module, want := range map[string]bool{
		pulseModuleTechnicalReview:    true,
		pulseModuleStrategicReview:    false,
		pulseModuleArchitectureReview: false,
	} {
		if due, err := pulseWorklistModulesDue(ctx, ws, "fix-1", module); err != nil || due != want {
			t.Fatalf("%s due = %v (%v), want %v", module, due, err, want)
		}
	}
}
