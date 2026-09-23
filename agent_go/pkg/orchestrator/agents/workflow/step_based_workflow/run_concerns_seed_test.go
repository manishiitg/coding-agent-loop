package step_based_workflow

import (
	"context"
	"testing"
)

// seedRunConcerns files concern rows through the lifecycle upsert that the live
// writer (RecordPulseReviewFinding) uses, but
// without their typed validation or detail row. That is the shape of the rows
// no live writer creates any more: untyped legacy step observations and
// prevalidation findings, which live readers still have to handle.
func seedRunConcerns(t *testing.T, workspacePath, runFolder, groupName, stepID, phase string, lines ...string) {
	t.Helper()
	ctx := context.Background()
	db, err := openRunConcernsDB(ctx, workspacePath, true)
	if err != nil || db == nil {
		t.Fatalf("open run concerns db: db=%v err=%v", db, err)
	}
	defer db.Close()
	if err := ensurePulseFindingLifecycleSchema(ctx, db); err != nil {
		t.Fatalf("ensure lifecycle schema: %v", err)
	}
	if _, err := recordRunConcernLinesAtWithFingerprints(ctx, db, runFolder, groupName, stepID, phase, lines, "", nil); err != nil {
		t.Fatalf("seed run concerns: %v", err)
	}
}

// testReviewFinding is the minimum valid typed reviewer finding for concern.
func testReviewFinding(module, concern string) PulseReviewFindingInput {
	return PulseReviewFindingInput{
		Concern: concern,
		Module:  module,
		PulseFindingDetails: PulseFindingDetails{
			IssueKind:      IssueKindWorkflow,
			Classification: "correctness_bug",
			Severity:       "medium",
			Summary:        concern,
			Impact:         "test impact",
			Evidence:       []string{"runs/iteration-0/result.json"},
		},
	}
}

// recordTestReviewFinding files one typed reviewer finding. The review run is
// the Pulse run, matching a reviewer that records during its own pass.
func recordTestReviewFinding(t *testing.T, workspacePath, pulseRunID string, input PulseReviewFindingInput) PulseReviewFindingRecord {
	t.Helper()
	record, err := RecordPulseReviewFinding(context.Background(), workspacePath, pulseRunID, pulseRunID, input)
	if err != nil {
		t.Fatalf("record review finding %q: %v", input.Concern, err)
	}
	return record
}

// activeRunConcerns is the live backlog restricted to findings that still need
// Pulse attention.
func activeRunConcerns(t *testing.T, workspacePath string) []PulseFindingLifecycle {
	t.Helper()
	findings, err := LoadPulseFindingLifecycles(context.Background(), workspacePath, "", -1)
	if err != nil {
		t.Fatalf("load findings: %v", err)
	}
	active := []PulseFindingLifecycle{}
	for _, finding := range findings {
		switch finding.Status {
		case ConcernStatusOpen, ConcernStatusAcknowledged, ConcernStatusFixing,
			ConcernStatusAwaitingVerification, ConcernStatusAwaitingRun, ConcernStatusQueuedForEngineering:
			active = append(active, finding)
		}
	}
	return active
}

// activeRunConcernRows returns the run_concerns rows that still need Pulse
// attention, newest first. It replaces the removed LoadOpenRunConcerns for
// tests that address rows by fingerprint.
func activeRunConcernRows(t *testing.T, workspacePath string) []RunConcern {
	t.Helper()
	ctx := context.Background()
	db, err := openRunConcernsDB(ctx, workspacePath, false)
	if err != nil {
		t.Fatalf("open run concerns db: %v", err)
	}
	if db == nil {
		return nil
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT fingerprint, issue_id, step_id, phase, group_name, text,
			first_seen_run, first_seen_at, last_seen_run, last_seen_at, seen_count, status
		FROM run_concerns WHERE status IN (?, ?, ?, ?, ?, ?)
		ORDER BY last_seen_at DESC, first_seen_at ASC`,
		ConcernStatusOpen, ConcernStatusAcknowledged, ConcernStatusFixing,
		ConcernStatusAwaitingVerification, ConcernStatusAwaitingRun, ConcernStatusQueuedForEngineering)
	if err != nil {
		t.Fatalf("query active run concerns: %v", err)
	}
	defer rows.Close()
	var out []RunConcern
	for rows.Next() {
		var c RunConcern
		if err := rows.Scan(&c.Fingerprint, &c.IssueID, &c.StepID, &c.Phase, &c.GroupName, &c.Text,
			&c.FirstSeenRun, &c.FirstSeenAt, &c.LastSeenRun, &c.LastSeenAt, &c.SeenCount, &c.Status); err != nil {
			t.Fatalf("scan run concern: %v", err)
		}
		out = append(out, c)
	}
	return out
}
