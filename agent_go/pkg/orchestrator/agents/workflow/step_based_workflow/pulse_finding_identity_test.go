package step_based_workflow

import (
	"context"
	"testing"
)

func structuredFinding(module, id, concern string) PulseReviewFindingInput {
	finding := testReviewFinding(module, concern)
	finding.FindingID = id
	finding.Summary = "identity test"
	return finding
}

func TestStructuredFindingIDSurvivesRewordingAndReviewerChange(t *testing.T) {
	ctx := context.Background()
	workspace := concernsWorkspace(t)
	recordTestReviewFinding(t, workspace, "pulse-1", structuredFinding("bug_review", "HARNESS-STABLE-1", "the first wording"))
	recordTestReviewFinding(t, workspace, "pulse-2", structuredFinding("artifact_review", "HARNESS-STABLE-1", "different words for the same behavior"))
	concerns, err := LoadPulseFindingLifecycles(ctx, workspace, "", -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(concerns) != 1 || concerns[0].SeenCount != 2 {
		t.Fatalf("concerns = %+v, want one stable row seen twice", concerns)
	}
}

func TestFindingIdentityMigrationMergesTwinsAndPreservesEvents(t *testing.T) {
	ctx := context.Background()
	workspace := concernsWorkspace(t)
	recordTestReviewFinding(t, workspace, "pulse-1", structuredFinding("bug_review", "HARNESS-MIGRATE-1", "canonical wording"))
	db, err := openRunConcernsDB(ctx, workspace, false)
	if err != nil || db == nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS idx_pulse_finding_details_finding_id`); err != nil {
		t.Fatal(err)
	}
	old := "legacy-twin-fp"
	if _, err := db.ExecContext(ctx, `INSERT INTO run_concerns
		(fingerprint,step_id,phase,text,first_seen_run,first_seen_at,last_seen_run,last_seen_at,seen_count,status)
		VALUES (?, 'artifact_review', 'review', 'legacy wording', 'pulse-0', '2026-08-01T00:00:00Z', 'pulse-0', '2026-08-01T00:00:00Z', 3, 'open')`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO pulse_finding_details
		(fingerprint,finding_id,issue_kind,target_key,detail_json,source_run_id,updated_at)
		VALUES (?, 'HARNESS-MIGRATE-1', 'workflow_issue', '', '{}', 'pulse-0', '2026-08-01T00:00:00Z')`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO pulse_finding_events
		(fingerprint,pulse_run_id,event_type,summary,recorded_at) VALUES (?, 'pulse-1', 'filed', 'legacy event', '2026-08-01T00:00:00Z')`, old); err != nil {
		t.Fatal(err)
	}
	if err := ensurePulseFindingLifecycleSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	var concerns, seen, events int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(seen_count),0) FROM run_concerns`).Scan(&concerns, &seen); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_finding_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if concerns != 1 || seen != 4 || events != 2 {
		t.Fatalf("after migration concerns=%d seen=%d events=%d, want 1/4/2", concerns, seen, events)
	}
}

func TestFindingIdentityMigrationMovesEventsLeftByPartialMigration(t *testing.T) {
	ctx := context.Background()
	workspace := concernsWorkspace(t)
	recordTestReviewFinding(t, workspace, "pulse-1", structuredFinding("bug_review", "HARNESS-PARTIAL-1", "canonical wording"))
	db, err := openRunConcernsDB(ctx, workspace, false)
	if err != nil || db == nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var canonical string
	if err := db.QueryRowContext(ctx, `SELECT fingerprint FROM pulse_finding_details WHERE finding_id='HARNESS-PARTIAL-1'`).Scan(&canonical); err != nil {
		t.Fatal(err)
	}
	const orphan = "old-fingerprint-left-after-partial-migration"
	if _, err := db.ExecContext(ctx, `INSERT INTO pulse_finding_events
		(fingerprint,finding_id,pulse_run_id,event_type,summary,recorded_at)
		VALUES (?, 'HARNESS-PARTIAL-1', 'pulse-0', 'external_action_required', 'old lifecycle event', '2026-08-01T00:00:00Z')`, orphan); err != nil {
		t.Fatal(err)
	}
	if err := ensurePulseFindingLifecycleSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	var orphanEvents, canonicalEvents int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_finding_events WHERE fingerprint=?`, orphan).Scan(&orphanEvents); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_finding_events WHERE fingerprint=?`, canonical).Scan(&canonicalEvents); err != nil {
		t.Fatal(err)
	}
	if orphanEvents != 0 || canonicalEvents < 2 {
		t.Fatalf("orphan events=%d canonical events=%d, want 0 and at least 2", orphanEvents, canonicalEvents)
	}
}

// TestFindingIdentityMigrationMergesHarnessTwinsByTargetKeyWhenFindingIDIsEmpty
// pins the PLAT-073 cluster I fix (f2cbf9a1): a harness finding split across
// the fingerprint boundary before either row ever acquired a human-assigned
// finding_id — the exact shape observed live for
// HARNESS-REFDOC-REVIEW-ARTIFACT-DRIFT, filed under fingerprints
// dfeaacf06b8317ec and b32e117e1f4ac80b with neither carrying a finding_id —
// previously could never be seen as duplicates by
// migrateDuplicatePulseFindingIdentities, which only ever grouped on
// finding_id. It must now merge via the shared target_key instead.
func TestFindingIdentityMigrationMergesHarnessTwinsByTargetKeyWhenFindingIDIsEmpty(t *testing.T) {
	ctx := context.Background()
	workspace := concernsWorkspace(t)
	// Bootstrap the schema via one unrelated real finding, matching the
	// pattern of the sibling tests above.
	recordTestReviewFinding(t, workspace, "pulse-1", structuredFinding("bug_review", "HARNESS-UNRELATED-1", "unrelated wording"))
	db, err := openRunConcernsDB(ctx, workspace, false)
	if err != nil || db == nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	const targetKey = "HARNESS-REFDOC-REVIEW-ARTIFACT-DRIFT"
	const first = "dfeaacf06b8317ec"
	const second = "b32e117e1f4ac80b"
	for _, fp := range []string{first, second} {
		if _, err := db.ExecContext(ctx, `INSERT INTO run_concerns
			(fingerprint,step_id,phase,text,first_seen_run,first_seen_at,last_seen_run,last_seen_at,seen_count,status)
			VALUES (?, 'get_reference_doc', 'review', 'review-artifact-drift finding', 'pulse-0', '2026-08-05T00:00:00Z', 'pulse-0', '2026-08-05T00:00:00Z', 1, 'external_action_required')`, fp); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO pulse_finding_details
			(fingerprint,finding_id,issue_kind,target_key,detail_json,source_run_id,updated_at)
			VALUES (?, '', ?, ?, '{}', 'pulse-0', '2026-08-05T00:00:00Z')`, fp, IssueKindHarness, targetKey); err != nil {
			t.Fatal(err)
		}
	}

	if err := ensurePulseFindingLifecycleSchema(ctx, db); err != nil {
		t.Fatal(err)
	}

	var mergedConcerns, mergedSeen int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(seen_count),0) FROM run_concerns WHERE fingerprint IN (?, ?)`, first, second).Scan(&mergedConcerns, &mergedSeen); err != nil {
		t.Fatal(err)
	}
	if mergedConcerns != 1 {
		t.Fatalf("run_concerns rows for the twin fingerprints = %d, want 1 (merged)", mergedConcerns)
	}
	if mergedSeen != 2 {
		t.Fatalf("merged seen_count = %d, want 2 (1+1 from both original rows)", mergedSeen)
	}
}

// TestFindingIdentityMigrationDoesNotMergeNonHarnessRowsByTargetKey guards
// the scope of the fix above: a coincidental target_key match between two
// non-harness findings (or two findings from unrelated workflows/steps) must
// never be merged just because both happen to have an empty finding_id —
// only IssueKindHarness rows share target_key as a real identity signal.
func TestFindingIdentityMigrationDoesNotMergeNonHarnessRowsByTargetKey(t *testing.T) {
	ctx := context.Background()
	workspace := concernsWorkspace(t)
	recordTestReviewFinding(t, workspace, "pulse-1", structuredFinding("bug_review", "HARNESS-UNRELATED-2", "unrelated wording"))
	db, err := openRunConcernsDB(ctx, workspace, false)
	if err != nil || db == nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	const sharedTargetKey = "planning/plan.json"
	const first = "coincidental-target-key-1"
	const second = "coincidental-target-key-2"
	for _, fp := range []string{first, second} {
		if _, err := db.ExecContext(ctx, `INSERT INTO run_concerns
			(fingerprint,step_id,phase,text,first_seen_run,first_seen_at,last_seen_run,last_seen_at,seen_count,status)
			VALUES (?, 'unrelated-step', 'review', 'unrelated finding text', 'pulse-0', '2026-08-05T00:00:00Z', 'pulse-0', '2026-08-05T00:00:00Z', 1, 'external_action_required')`, fp); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO pulse_finding_details
			(fingerprint,finding_id,issue_kind,target_key,detail_json,source_run_id,updated_at)
			VALUES (?, '', 'workflow_issue', ?, '{}', 'pulse-0', '2026-08-05T00:00:00Z')`, fp, sharedTargetKey); err != nil {
			t.Fatal(err)
		}
	}

	if err := ensurePulseFindingLifecycleSchema(ctx, db); err != nil {
		t.Fatal(err)
	}

	var unmergedConcerns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM run_concerns WHERE fingerprint IN (?, ?)`, first, second).Scan(&unmergedConcerns); err != nil {
		t.Fatal(err)
	}
	if unmergedConcerns != 2 {
		t.Fatalf("run_concerns rows for the two non-harness findings = %d, want 2 (must stay unmerged)", unmergedConcerns)
	}
}
