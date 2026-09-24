package step_based_workflow

import (
	"context"
	"strings"
	"testing"
)

// issue_kind is written once at filing; status is written later and repeatedly
// by a different tool. Nothing compared them, so upwork accumulated two findings
// filed as harness_issue and then parked as queued_for_engineering — a defect
// the workflow cannot repair, queued for a workflow-level Engineering Review
// pass. Each later pass re-reads it as actionable, spends a reviewer slot
// rediscovering that it is not, and re-defers it.
//
// Scope note: only this one pairing is contradictory. Of 48 harness findings
// across all workflows, 38 are correctly external_action_required and the
// remainder are resolved (behavior stopped or disproven), acknowledged
// (recovered by retry), or awaiting_verification (workflow-side workaround
// applied) — all legitimate, all deliberately still allowed.

// filedKindedConcern seeds a finding carrying an explicit issue_kind, for any
// module. filedAdvisorConcern hardcodes workflow_issue and targets the advisor
// routing contract, so it cannot express the harness case.
func filedKindedConcern(t *testing.T, workspacePath, pulseRunID, module, text, issueKind string) PulseFindingLifecycle {
	t.Helper()
	details := PulseFindingDetails{
		IssueKind:      issueKind,
		Classification: "correctness_bug",
		Severity:       "medium",
		Summary:        text,
		Impact:         "reviewer slots are spent rediscovering an unactionable finding",
		Evidence:       []string{"runs/iteration-0/default/logs/example.json"},
	}
	// A harness finding must carry enough for someone outside the workflow to
	// act without re-deriving the observation.
	if issueKind == IssueKindHarness {
		details.TargetKey = "harness:agent_browser:snapshot-overflow"
		details.Reproduction.Expected = "snapshot returns within the result cap"
		details.Reproduction.Observed = "snapshot overflows and is truncated"
	}
	recordTestReviewFinding(t, workspacePath, pulseRunID, PulseReviewFindingInput{
		Concern:             text,
		Module:              module,
		PulseFindingDetails: details,
	})
	concerns, err := LoadPulseFindingLifecycles(context.Background(), workspacePath, "", 10)
	if err != nil || len(concerns) != 1 {
		t.Fatalf("load %s concern: concerns=%+v err=%v", issueKind, concerns, err)
	}
	return concerns[0]
}

// recordFindingDispositionsErr mirrors recordFindingDispositions but returns the
// error instead of failing the test, so rejection can be asserted.
func recordFindingDispositionsErr(t *testing.T, workspacePath, module, pulseRunID string, dispositions []PulseFindingDisposition) error {
	t.Helper()
	ctx := context.Background()
	db, err := openRunConcernsDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		t.Fatalf("open lifecycle db: db=%v err=%v", db, err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin lifecycle transaction: %v", err)
	}
	defer tx.Rollback()
	if err := RecordPulseFindingDispositionsTx(ctx, tx, module, pulseRunID, dispositions, "2026-08-09T08:00:00Z"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit dispositions: %v", err)
	}
	return nil
}

func queuedForEngineering(fingerprint string) PulseFindingDisposition {
	return PulseFindingDisposition{
		Fingerprint: fingerprint,
		FindingID:   "PUL-COHERENCE-1",
		Disposition: FindingDispositionQueuedForEngineering,
		Summary:     "Deferred to the next Engineering pass.",
		NextCheck:   "next scheduled Engineering Review pass",
	}
}

// Pulse no longer parks issues for a later pass: queued_for_engineering is
// refused for every issue kind (it was 75 events in upwork alone), and the
// rejection names the ways to close the issue instead.
func TestQueuedForEngineeringIsRefusedForEveryIssueKind(t *testing.T) {
	module := "workflow_review"
	for kind, text := range map[string]string{
		IssueKindHarness:  "agent_browser snapshot results overflow with no pagination recipe",
		IssueKindWorkflow: "shortlist depth is not enforced in the step contract",
	} {
		workspacePath := concernsWorkspace(t)
		concern := filedKindedConcern(t, workspacePath, "pulse-1", module, text, kind)
		err := recordFindingDispositionsErr(t, workspacePath, module, "pulse-1",
			[]PulseFindingDisposition{queuedForEngineering(concern.Fingerprint)})
		if err == nil {
			t.Fatal("queued_for_engineering was accepted; Pulse can still park an issue")
		}
		for _, want := range []string{"retired", "fixed_verified", "awaiting_user", "external_action_required"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("rejection does not name %q: %v", want, err)
			}
		}
	}
}

func TestHarnessIssueStillReachesExternalActionRequired(t *testing.T) {
	ctx := context.Background()
	workspacePath := concernsWorkspace(t)
	module := "workflow_review"
	concern := filedKindedConcern(t, workspacePath, "pulse-1", module,
		"agent_browser snapshot results overflow with no pagination recipe", IssueKindHarness)

	if err := recordFindingDispositionsErr(t, workspacePath, module, "pulse-1", []PulseFindingDisposition{{
		Fingerprint:     concern.Fingerprint,
		FindingID:       "PUL-COHERENCE-1",
		Disposition:     FindingDispositionExternalAction,
		Summary:         "Escalated to the platform register.",
		ExternalOwner:   "platform",
		ReasonCode:      "missing_platform_tool",
		ReopenCondition: "a scoping/pagination recipe ships for agent_browser snapshots",
	}}); err != nil {
		t.Fatalf("the escape hatch the rejection points at was itself rejected: %v", err)
	}

	lifecycles, err := LoadPulseFindingLifecycles(ctx, workspacePath, module, 10)
	if err != nil {
		t.Fatalf("load lifecycle: %v", err)
	}
	if len(lifecycles) != 1 || lifecycles[0].Status != ConcernStatusExternalActionRequired {
		t.Fatalf("harness finding did not reach external_action_required: %+v", lifecycles)
	}
}
