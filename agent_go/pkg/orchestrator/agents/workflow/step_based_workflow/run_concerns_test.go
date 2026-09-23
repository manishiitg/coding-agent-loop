package step_based_workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/pulsemodules"
)

func concernsWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	ws := "Workflow/testing"
	if err := os.MkdirAll(filepath.Join(root, "Workflow", "testing", "db"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return ws
}

// Magnitude is the interesting part of a recurring concern, so two reports that
// differ only in a number must stay distinct rows.
func TestConcernFingerprintKeepsDigitsButIgnoresFormatting(t *testing.T) {
	a := concernFingerprint("s1", "12 learnings items stale")
	b := concernFingerprint("s1", "3 learnings items stale")
	if a == b {
		t.Fatal("differing counts must not collapse to one fingerprint")
	}
	spaced := concernFingerprint("s1", "  12   LEARNINGS   items stale ")
	if a != spaced {
		t.Fatal("case and whitespace must not create a new fingerprint")
	}
	if concernFingerprint("s1", "same text") == concernFingerprint("s2", "same text") {
		t.Fatal("same text from different steps must not merge")
	}
}

// The whole point of the table: a concern reported every run collapses to one
// row whose seen_count is the signal. Twelve rows would bury it.
func TestRunConcernUpsertDedupesAndCountsRecurrence(t *testing.T) {
	ws := concernsWorkspace(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		seedRunConcerns(t, ws, "iteration-0/g", "g", "step-a", ConcernPhaseExecution, "db_health rows have NULL pnl_inr")
	}
	open, err := LoadPulseFindingLifecycles(ctx, ws, "", 10)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("expected 1 deduped row, got %d: %#v", len(open), open)
	}
	if open[0].SeenCount != 3 {
		t.Fatalf("seen_count = %d, want 3", open[0].SeenCount)
	}
	if open[0].Phase != ConcernPhaseExecution || open[0].StepID != "step-a" {
		t.Fatalf("attribution lost: %#v", open[0])
	}
}

// A fix that did not hold is more important than the original report, so a
// resolved concern that comes back reopens. A rejected one does not: someone
// judged it a non-issue and recurrence is not new evidence against that.
func TestRecurrenceReopensResolvedButNotRejected(t *testing.T) {
	ctx := context.Background()
	concern := "selector drifted again"

	for _, tc := range []struct {
		name       string
		status     string
		wantStatus string
	}{
		{"resolved reopens", ConcernStatusResolved, ConcernStatusOpen},
		{"rejected stays closed", ConcernStatusRejected, ConcernStatusRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := concernsWorkspace(t)
			seedRunConcerns(t, ws, "run-1", "g", "step-a", ConcernPhaseExecution, concern)
			open := activeRunConcerns(t, ws)
			if len(open) != 1 {
				t.Fatalf("setup: expected 1 row, got %d", len(open))
			}
			if err := ResolveRunConcern(ctx, ws, open[0].Fingerprint, tc.status, "pulse_fixer", "note"); err != nil {
				t.Fatalf("resolve: %v", err)
			}
			seedRunConcerns(t, ws, "run-2", "g", "step-a", ConcernPhaseExecution, concern)
			reloaded := activeRunConcerns(t, ws)
			if tc.wantStatus == ConcernStatusOpen {
				if len(reloaded) != 1 || reloaded[0].Status != ConcernStatusOpen {
					t.Fatalf("expected reopened row, got %#v", reloaded)
				}
			} else if len(reloaded) != 0 {
				t.Fatalf("rejected concern must not resurface, got %#v", reloaded)
			}
		})
	}
}

func TestFindingBacklogRanksByRecurrence(t *testing.T) {
	ws := concernsWorkspace(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		seedRunConcerns(t, ws, "r", "g", "step-a", ConcernPhaseExecution, "seen twice")
	}
	seedRunConcerns(t, ws, "r", "g", "step-b", ConcernPhaseLearnings, "seen once")

	open, err := LoadPulseFindingLifecycles(ctx, ws, "", 10)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(open))
	}
	if !strings.Contains(open[0].Text, "seen twice") {
		t.Fatalf("most-recurring must rank first, got %q", open[0].Text)
	}
}

func TestFindingBacklogNegativeLimitReturnsCompleteBacklog(t *testing.T) {
	ws := concernsWorkspace(t)
	ctx := context.Background()
	for i := 0; i < 63; i++ {
		seedRunConcerns(t, ws, "run", "g", "step-a", ConcernPhaseExecution, fmt.Sprintf("distinct backlog concern %02d", i))
	}

	preview, err := LoadPulseFindingLifecycles(ctx, ws, "", 25)
	if err != nil {
		t.Fatalf("load preview: %v", err)
	}
	complete, err := LoadPulseFindingLifecycles(ctx, ws, "", -1)
	if err != nil {
		t.Fatalf("load complete backlog: %v", err)
	}
	if len(preview) != 25 || len(complete) != 63 {
		t.Fatalf("preview=%d complete=%d, want 25 and 63", len(preview), len(complete))
	}
}

// A workflow that has never raised a concern has no table; that must read as
// "nothing to report", not as an error the Gate has to handle.
func TestFindingBacklogQuietWhenNeverUsed(t *testing.T) {
	ws := concernsWorkspace(t)
	got, err := LoadPulseFindingLifecycles(context.Background(), ws, "", 10)
	if err != nil {
		t.Fatalf("missing table should not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no concerns, got %#v", got)
	}
}

func TestResolveRunConcernRejectsUnknownStatus(t *testing.T) {
	ws := concernsWorkspace(t)
	ctx := context.Background()
	seedRunConcerns(t, ws, "r", "g", "step-a", ConcernPhaseExecution, "something")
	open := activeRunConcerns(t, ws)
	if err := ResolveRunConcern(ctx, ws, open[0].Fingerprint, "ignored", "x", ""); err == nil {
		t.Fatal("unknown status must be rejected")
	}
}

// Reviewer artifacts are per-run files that nothing diffs across runs, so a
// finding repeated every cycle reads as new every cycle. Reviewer concerns go
// through the same fingerprinting as step concerns, keyed by current reviewer
// perspective. Historical artifact-named modules now share Engineering Review.
func TestRelatedStepConcernsOutrankAnIsolatedRecurrence(t *testing.T) {
	ws := concernsWorkspace(t)
	ctx := context.Background()

	// Four distinct behavioral symptoms, each seen once, share one step.
	for _, symptom := range []string{"wrong source", "stale selector", "missing retry", "incorrect destination"} {
		seedRunConcerns(t, ws, "run-1", "", "execute-find-opportunities", ConcernPhaseExecution, "execution contract: "+symptom)
	}

	// An unrelated finding that genuinely recurred.
	recurring := testReviewFinding(pulsemodules.TechnicalReviewID, "daily digest was not delivered")
	recurring.StepID = "execute-digest"
	for _, run := range []string{"run-1", "run-2", "run-3"} {
		recordTestReviewFinding(t, ws, run, recurring)
	}

	open, err := LoadPulseFindingLifecycles(ctx, ws, "", -1)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(open) != 5 {
		t.Fatalf("expected 5 concerns, got %d", len(open))
	}
	if open[0].StepID != "execute-find-opportunities" {
		t.Fatalf("the 4-concern cluster must rank above a single 3x recurrence; got %q at position 1 (seen_count=%d)",
			open[0].StepID, open[0].SeenCount)
	}
	// The cluster must stay contiguous so one cause reads as one problem.
	for i := 0; i < 4; i++ {
		if open[i].StepID != "execute-find-opportunities" {
			t.Fatalf("cluster split at position %d by %q", i+1, open[i].StepID)
		}
	}
	if open[4].StepID != "execute-digest" || open[4].SeenCount != 3 {
		t.Fatalf("the isolated recurrence should follow the cluster, got %+v", open[4])
	}
}

func TestLegacyPreValidationFieldRowsMigrateToOneStepFinding(t *testing.T) {
	ws := concernsWorkspace(t)
	ctx := context.Background()
	db, err := openRunConcernsDB(ctx, ws, true)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := ensurePulseFindingLifecycleSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	stepID := "execute-find-opportunities"
	legacy := []struct {
		fp, text, firstRun, lastRun string
		seen                        int
	}{
		{"legacy-field-a", "prevalidation gate failed at opportunities.json $.targets", "run-1", "run-2", 2},
		{"legacy-field-b", "prevalidation gate failed at opportunities.json $.coverage", "run-2", "run-2", 1},
	}
	for _, row := range legacy {
		if _, err := db.ExecContext(ctx, `INSERT INTO run_concerns
			(fingerprint,step_id,phase,text,first_seen_run,first_seen_at,last_seen_run,last_seen_at,seen_count,status)
			VALUES (?,?,?,?,?,'2026-08-01T00:00:00Z',?,'2026-08-02T00:00:00Z',?,?)`,
			row.fp, stepID, ConcernPhasePreValidation, row.text, row.firstRun, row.lastRun, row.seen, ConcernStatusOpen); err != nil {
			t.Fatalf("insert legacy concern: %v", err)
		}
	}
	for _, event := range []struct{ fp, run, kind string }{
		{"legacy-field-a", "run-1", "filed-a"},
		{"legacy-field-a", "run-2", "observed-a"},
		{"legacy-field-b", "run-2", "filed-b"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO pulse_finding_events
			(fingerprint,pulse_run_id,event_type,summary,metadata_json,recorded_at)
			VALUES (?,?,?,?,'{}','2026-08-02T00:00:00Z')`, event.fp, event.run, event.kind, event.kind); err != nil {
			t.Fatalf("insert legacy event: %v", err)
		}
	}

	if err := migratePreValidationConcernGranularity(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var count, seen int
	var fingerprint, text string
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), MIN(fingerprint), MIN(text), MIN(seen_count)
		FROM run_concerns WHERE phase=? AND step_id=?`, ConcernPhasePreValidation, stepID).
		Scan(&count, &fingerprint, &text, &seen); err != nil {
		t.Fatalf("read migrated concern: %v", err)
	}
	if count != 1 || fingerprint != preValidationConcernFingerprint(stepID) {
		t.Fatalf("got count=%d fingerprint=%q, want one canonical step finding", count, fingerprint)
	}
	if seen != 2 {
		t.Fatalf("seen_count=%d, want two distinct runs rather than three field rows", seen)
	}
	if !strings.Contains(text, "2 legacy field-level finding(s) were consolidated") {
		t.Fatalf("migration summary lost granularity evidence: %q", text)
	}
	var events int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_finding_events WHERE fingerprint=?`, fingerprint).Scan(&events); err != nil {
		t.Fatalf("count migrated events: %v", err)
	}
	if events != 3 {
		t.Fatalf("events=%d, want all three field observations preserved", events)
	}
}
