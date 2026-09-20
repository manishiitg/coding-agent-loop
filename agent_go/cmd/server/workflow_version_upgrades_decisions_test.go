package server

import (
	"strings"
	"testing"
)

// confida-login stalled on the 1.0.21 rung three times: each turn found history
// in the retired improve archive that SQLite did not have, correctly refused to
// destroy it, and asked the owner — who was asleep, because these turns are
// fired by the scheduler. The disposition now lives in the instruction, and the
// archive is moved rather than deleted so deciding autonomously costs nothing.
func TestArtifactContractUpgradeDecidesTheArchiveItself(t *testing.T) {
	for _, want := range []string{
		// The refusal this replaced read "retire" as "delete". Lead with the
		// property that makes the objection moot.
		"NOTHING IS DELETED IN THIS MIGRATION",
		"migration-backups/",
		"stay on disk, and in git history, at the new location",
		// The turn verifies the no-loss claim rather than taking it on faith.
		"Read them back at the new path",
		"if the move cannot preserve a file, that IS a blocker",
		// Answer the prior revert on its merits instead of talking over it.
		"A previous turn on this workflow may have declined",
		"DESTROYED",
	} {
		if !strings.Contains(upgradeCurrentArtifactContract, want) {
			t.Errorf("1.0.21 upgrade prompt missing %q", want)
		}
	}
	if strings.Contains(upgradeCurrentArtifactContract, "workflow owner's decision") {
		t.Error("1.0.21 upgrade prompt still defers the archive to an owner who is not reading it")
	}
}

// Every upgrade turn is scheduler-fired with nobody reading it, so each one has
// to say so — as a fact about the execution context, not as pressure.
func TestEveryUpgradeQueryCarriesTheUnattendedContract(t *testing.T) {
	plan := workflowVersionUpgradePlan(&WorkflowManifest{Version: "1.0.9"})
	if len(plan) < 2 {
		t.Fatalf("expected a multi-rung plan from 1.0.9, got %+v", plan)
	}
	for _, step := range plan {
		for _, want := range []string{
			"EXECUTION CONTEXT",
			// Naming what this is matters: an agent that reads the turn as a
			// third party relaying a previously-rejected request treats it as
			// something to resist, rather than as its own maintenance work.
			"automated platform migration",
			"not a user request relayed through the scheduler",
			"take the best action to complete the migration properly",
			"create_human_input_request",
			// Refusing has to stay plainly available, or the note reads as
			// coercion and a correctly-cautious agent refuses the whole turn.
			"stopping without stamping, are both acceptable outcomes",
		} {
			if !strings.Contains(step.query, want) {
				t.Errorf("%s query missing %q", step.label, want)
			}
		}
		// An earlier draft pressured the agent into compliance. An agent that
		// had correctly blocked this migration cited that framing when it
		// refused again.
		for _, coercive := range []string{
			"do not re-open it as a judgement call",
			"reaches nobody",
			"is not an available move",
		} {
			if strings.Contains(step.query, coercive) {
				t.Errorf("%s query pressures the agent (%q); state the context, leave refusing available", step.label, coercive)
			}
		}
	}
}

// Blocked upgrades route through the existing operator-decision lifecycle
// (create_human_input_request, drained pre-run by scheduledDecisionDrainTurn).
// A second, upgrade-specific answer channel in workflow.json was built and then
// removed as a duplicate; this pins that it stays gone.
func TestNoParallelContractUpgradeDecisionChannel(t *testing.T) {
	plan := workflowVersionUpgradePlan(&WorkflowManifest{Version: "1.0.20"})
	for _, step := range plan {
		if strings.Contains(step.query, "contract_upgrade_decision") || strings.Contains(step.query, "OPERATOR DECISION ON THIS UPGRADE") {
			t.Errorf("%s query still references the removed upgrade-specific decision channel", step.label)
		}
	}
}

// Measurement remains producer-owned guidance, not a blocking contract
// migration. Guard the complete upgrade chain so a mandatory route, step,
// table, or retired evaluation turn cannot silently return.
func TestNoUpgradeMandatesMeasurementTopology(t *testing.T) {
	if WorkflowContractCurrentVersion != workflowContractRunScopedRoutesVersion {
		t.Fatalf("current contract = %s, want historical marker %s", WorkflowContractCurrentVersion, workflowContractRunScopedRoutesVersion)
	}
	plan := workflowVersionUpgradePlan(&WorkflowManifest{Version: workflowContractInitialVersion})
	joined := strings.ToLower(strings.Join(func() []string {
		out := make([]string, 0, len(plan)*2)
		for _, upgrade := range plan {
			out = append(out, upgrade.label, upgrade.query)
		}
		return out
	}(), "\n"))
	for _, forbidden := range []string{
		"upgrade-eval-retirement", "set_workflow_contract_version(version=\"1.0.43\")",
		"measurement-router", "measure-outcomes", "workflow_metrics",
		"upgrade-measurement-route", "route-skip-measurement", "planning/metrics.json",
	} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("upgrade chain still mandates retired measurement topology %q", forbidden)
		}
	}
	for _, version := range []string{
		workflowContractExplicitSchedulePulseVersion,
		workflowContractRunScopedRoutesVersion,
		workflowContractEvalRetirementVersion,
	} {
		if got := workflowVersionUpgradePlan(&WorkflowManifest{Version: version}); len(got) != 0 {
			t.Errorf("retired marker %s still emits migration turns: %+v", version, got)
		}
	}
}
