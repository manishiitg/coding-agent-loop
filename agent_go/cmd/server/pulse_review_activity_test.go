package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPulseReviewActivityKeepsOldScopeCoverageAndDrift(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	workspace := "Workflow/review-coverage"
	_, db, err := openPulseModuleStateDB(ctx, workspace, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	insertFocus := func(run, scope, at string) {
		t.Helper()
		_, err := db.ExecContext(ctx, `INSERT INTO pulse_review_focus_history
   (workspace_path,module,pulse_run_id,focus_key,priority_class,selection_reason,verdict,route_scope,evidence_json,issue_ids_json,deferred_focuses_json,recorded_at)
   VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, workspace, pulseModuleTechnicalReview, run, "store_integrity", "maintenance", "Inspect stored knowledge", "Checked", scope, `["learnings review proof"]`, `["PUL-LEARN"]`, `[]`, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	insertFocus("learning-old", "workflow/learnings", "2026-08-31T10:16:48Z")
	insertFocus("learning-new", "workflow/learnings", "2026-09-01T10:16:48Z")
	for i := 0; i < 260; i++ {
		insertFocus(fmt.Sprintf("db-%d", i), "workflow/database", fmt.Sprintf("2026-09-09T10:%02d:%02dZ", i/60, i%60))
	}
	_, err = db.ExecContext(ctx, `INSERT INTO pulse_module_audit (workspace_path,module,pulse_run_id,result,reason,verification_json,changed_files_json,recorded_at)
 VALUES (?,?,?,?,?,?,?,?)`, workspace, "plan_drift_review", "drift-old", "done", "Plan aligned", `["contract check passed"]`, `["planning/plan.json"]`, "2026-08-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 105; i++ {
		_, err = db.ExecContext(ctx, `INSERT INTO pulse_module_audit (workspace_path,module,pulse_run_id,result,reason,recorded_at) VALUES (?,?,?,?,?,?)`, workspace, pulseModuleTechnicalReview, fmt.Sprintf("run-%d", i), "done", "Checks passed", fmt.Sprintf("2026-09-09T11:%02d:%02dZ", i/60, i%60))
		if err != nil {
			t.Fatal(err)
		}
	}
	coverage, audits, err := loadPulseReviewActivity(ctx, workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(coverage) != 2 {
		t.Fatalf("coverage = %#v", coverage)
	}
	var found bool
	for _, row := range coverage {
		if row.RouteScope == "workflow/learnings" {
			found = true
			if row.LastPulseRunID != "learning-new" || len(row.Evidence) != 1 || len(row.IssueIDs) != 1 {
				t.Fatalf("lost learning evidence: %#v", row)
			}
		}
	}
	if !found {
		t.Fatal("older learnings scope disappeared behind recent database reviews")
	}
	if len(audits) != 101 || audits[len(audits)-1].PulseRunID != "drift-old" || len(audits[len(audits)-1].Verification) != 1 {
		t.Fatalf("older drift baseline missing: %#v", audits)
	}
	coverage, audits, err = loadPulseReviewActivity(ctx, workspace, "strategic_review")
	if err != nil || len(coverage) != 0 || len(audits) != 0 {
		t.Fatalf("module isolation failed: %v %#v %#v", err, coverage, audits)
	}
}

func TestPulseReviewReportsIncludeExistingCheckpointsOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspace := "Workflow/reports"
	run := filepath.Join(root, workspace, "runs", "pulse", "run-1")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "strategic-review.md"), []byte("# In-progress review"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(run, "strategic-review.md"), filepath.Join(run, "technical-review.md")); err != nil {
		t.Fatal(err)
	}
	reports, err := listPulseReviewReports(workspace, "")
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports = %#v, %v", reports, err)
	}
	if reports[0].Path != workspace+"/runs/pulse/run-1/strategic-review.md" || reports[0].UpdatedAt == "" {
		t.Fatalf("bad report: %#v", reports[0])
	}
	reports, err = listPulseReviewReports(workspace, "technical_review")
	if err != nil || len(reports) != 0 {
		t.Fatalf("unexpected report %#v %v", reports, err)
	}
	if _, err = listPulseReviewReports("../escape", ""); err == nil {
		t.Fatal("expected invalid workspace rejection")
	}
}
