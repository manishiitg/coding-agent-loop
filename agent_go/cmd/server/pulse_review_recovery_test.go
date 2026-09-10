package server

import (
	"context"
	"strings"
	"testing"
)

func TestIncompleteReviewIsForcedDueAndClearedByTerminalResult(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	workspacePath := "Workflow/recovery"

	if err := markPulseReviewRecovery(ctx, workspacePath, pulseModuleStrategicReview, "pulse-old", "runs/pulse/pulse-old/strategic-review.md", "worker exited after turn 1 of 4"); err != nil {
		t.Fatalf("mark recovery: %v", err)
	}
	decisions := completePulseWorklistDecisions(map[string]PulseWorklistDecision{
		pulseModuleStrategicReview: {
			Module: pulseModuleStrategicReview, Due: false, Reason: "No newly matured strategic evidence.", CooldownRuns: 3,
		},
	})
	if _, err := recordPulseWorklist(ctx, workspacePath, "pulse-new", decisions); err != nil {
		t.Fatalf("record forced worklist: %v", err)
	}
	worklist, ok, err := getPulseWorklistForRun(ctx, workspacePath, "pulse-new")
	if err != nil || !ok {
		t.Fatalf("read worklist: ok=%v err=%v", ok, err)
	}
	strategic := worklist[pulseModuleStrategicReview]
	if strategic.LastDecision != "due" || !strings.Contains(strategic.LastReason, "Mandatory recovery") {
		t.Fatalf("strategic decision = %#v, want mandatory due recovery", strategic)
	}
	if strategic.CooldownRuns != 0 {
		t.Fatalf("recovery cooldown = %d, want 0", strategic.CooldownRuns)
	}

	if _, err := markPulseModuleResultFromAgent(ctx, workspacePath, pulseModuleStrategicReview, "pulse-new", "done", "Resumed from the durable checkpoint and persisted the final result.", nil); err != nil {
		t.Fatalf("record terminal recovery result: %v", err)
	}
	recoveries, err := pendingPulseReviewRecoveries(ctx, workspacePath)
	if err != nil {
		t.Fatalf("read recoveries: %v", err)
	}
	if len(recoveries) != 0 {
		t.Fatalf("recoveries = %#v, want cleared after terminal result", recoveries)
	}
}

func TestReviewFixMissingReceiptCreatesRecoveryWithoutMandatoryFile(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	workspacePath := "Workflow/recovery"
	pulseRunID := "pulse-interrupted"
	if _, err := recordPulseWorklist(ctx, workspacePath, pulseRunID, completePulseWorklistDecisions(map[string]PulseWorklistDecision{
		pulseModuleStrategicReview: {Module: pulseModuleStrategicReview, Due: true, Reason: "Matured strategy question."},
	})); err != nil {
		t.Fatalf("record worklist: %v", err)
	}
	recovered, err := recordIncompletePulseReviewRecoveries(ctx, workspacePath, pulseRunID, pulseLifecycleStepRunResult{outcome: pulseLifecycleStepTimedOut})
	if err != nil {
		t.Fatalf("record incomplete recovery: %v", err)
	}
	if len(recovered) != 1 || recovered[0] != pulseModuleStrategicReview {
		t.Fatalf("recovered = %v, want strategic_review", recovered)
	}
	recoveries, err := pendingPulseReviewRecoveries(ctx, workspacePath)
	if err != nil || len(recoveries) != 1 {
		t.Fatalf("recoveries = %#v err=%v, want one", recoveries, err)
	}
	if got := recoveries[0].CheckpointPath; got != "" {
		t.Fatalf("checkpoint = %q", got)
	}
}

func TestRunningReviewAttemptSurvivesRestartAsRecovery(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	workspacePath := "Workflow/recovery"
	if err := beginPulseReviewRecovery(ctx, workspacePath, pulseModuleStrategicReview, "pulse-running", "runs/pulse/pulse-running/strategic-review.md"); err != nil {
		t.Fatalf("begin recovery attempt: %v", err)
	}
	changed, err := finalizeRunningPulseReviewRecoveries(ctx, workspacePath, "server restarted")
	if err != nil || changed != 1 {
		t.Fatalf("finalize running recovery = %d, %v; want 1, nil", changed, err)
	}
	recoveries, err := pendingPulseReviewRecoveries(ctx, workspacePath)
	if err != nil || len(recoveries) != 1 {
		t.Fatalf("recoveries = %#v err=%v, want one", recoveries, err)
	}
	if recoveries[0].Reason != "server restarted" {
		t.Fatalf("recovery reason = %q", recoveries[0].Reason)
	}
}

func TestPlanDriftDoesNotSuppressIndependentReviewRecovery(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	workspacePath := "Workflow/recovery"
	if err := markPulseReviewRecovery(ctx, workspacePath, pulseModuleStrategicReview, "pulse-old", "runs/pulse/pulse-old/strategic-review.md", "missing terminal result"); err != nil {
		t.Fatalf("mark recovery: %v", err)
	}
	decisions := completePulseWorklistDecisions(map[string]PulseWorklistDecision{
		pulseModulePlanDriftReview: {Module: pulseModulePlanDriftReview, Due: true, Reason: "A step needs drift review."},
		pulseModuleStrategicReview: {Module: pulseModuleStrategicReview, Due: false, Reason: "Deferred while plan drift is selected.", CooldownRuns: 1},
	})
	forced, err := forcePendingPulseReviewRecoveries(ctx, workspacePath, decisions)
	if err != nil {
		t.Fatalf("force recovery: %v", err)
	}
	for _, decision := range forced {
		if normalizePulseModule(decision.Module) == pulseModuleStrategicReview && !decision.Due {
			t.Fatalf("strategic recovery must remain due after the preceding drift step: %#v", decision)
		}
	}
	if err := validatePulseWorklistDecisions(forced); err != nil {
		t.Fatalf("independently due modules should be valid: %v", err)
	}
	for index := range decisions {
		if normalizePulseModule(decisions[index].Module) == pulseModuleTechnicalReview {
			decisions[index].Due = true
			decisions[index].Reason = "Incorrect concurrent technical review."
		}
	}
	if err := validatePulseWorklistDecisions(decisions); err != nil {
		t.Fatalf("concurrent plan/technical worklist error = %v", err)
	}
}
