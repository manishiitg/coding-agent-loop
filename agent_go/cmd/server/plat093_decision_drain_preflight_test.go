package server

import (
	"strings"
	"testing"
)

func plat093Answered(ids ...string) []ReportHumanInput {
	out := make([]ReportHumanInput, 0, len(ids))
	for _, id := range ids {
		out = append(out, ReportHumanInput{ID: id, Status: "answered", SelectedOptionID: "approve", ApplyContract: ReportHumanInputApplyContract{Mode: "direct_apply", FailurePolicy: "continue_unchanged"}})
	}
	return out
}

func TestDecisionPreflightRoutesOnlyStructuredContracts(t *testing.T) {
	turns := scheduledDecisionPreflightTurns([]ReportHumanInput{
		{ID: "legacy-prose", Status: "answered", SelectedOptionID: "approve"},
		{ID: "safe-setting", Status: "answered", SelectedOptionID: "approve", ApplyContract: ReportHumanInputApplyContract{Mode: "direct_apply"}},
		{ID: "prompt-bloat", WorkspacePath: "Workflow/social-media", Status: "answered", SelectedOptionID: "approve", ApplyContract: ReportHumanInputApplyContract{
			Mode: "targeted_fixer", IssueID: "PUL-55BE3473", ApprovedScope: "Extract one shared contract.", PreRunChecks: []string{"validate_plan_change"}, PostRunProof: "one producing run",
		}},
		{ID: "rejected-prompt-bloat", Status: "answered", SelectedOptionID: "reject", ApplyContract: ReportHumanInputApplyContract{Mode: "targeted_fixer", ApprovedScope: "Must not run."}},
		{ID: "external-wait", Status: "answered", SelectedOptionID: "approve", ApplyContract: ReportHumanInputApplyContract{Mode: "external_wait"}},
	})
	if len(turns) != 2 {
		t.Fatalf("preflight turns = %d, want direct drain plus targeted fixer", len(turns))
	}
	if turns[0].label != "decision-drain-preflight" || !strings.Contains(turns[0].query, "safe-setting") || !strings.Contains(turns[0].query, "rejected-prompt-bloat") || strings.Contains(turns[0].query, "legacy-prose") {
		t.Fatalf("direct drain must contain only safe direct or rejected target decisions: %+v", turns[0])
	}
	fixer := turns[1]
	for _, want := range []string{"decision-fixer-preflight", "PUL-55BE3473", "Extract one shared contract.", "validate_plan_change", "one producing run", "Do NOT run workflow steps"} {
		if !(fixer.label == want || strings.Contains(fixer.query, want)) {
			t.Fatalf("targeted fixer missing %q: %+v", want, fixer)
		}
	}
}

// TestDecisionDrainTurnOnlyExistsWhenThereIsSomethingToApply keeps an ordinary
// run from paying for an extra LLM turn it does not need. Most runs have no
// answered decisions waiting.
func TestDecisionDrainTurnOnlyExistsWhenThereIsSomethingToApply(t *testing.T) {
	if _, ok := scheduledDecisionDrainTurn(nil); ok {
		t.Fatal("no pending decisions must produce no turn")
	}
	if _, ok := scheduledDecisionDrainTurn([]ReportHumanInput{}); ok {
		t.Fatal("an empty slice must produce no turn")
	}
	// An input with no usable id is not something to apply.
	if _, ok := scheduledDecisionDrainTurn([]ReportHumanInput{{ID: "   ", Status: "answered"}}); ok {
		t.Fatal("a blank id must produce no turn")
	}
	if _, ok := scheduledDecisionDrainTurn(plat093Answered("plan-proposal-1")); !ok {
		t.Fatal("a real answered decision must produce a turn")
	}
}

// TestDecisionDrainTurnCarriesTheOperatorsActualAnswer pins that the prompt
// names each decision and the option the operator actually chose. Applying a
// decision without honoring its selected option — approving something they
// rejected — is the worst available outcome, so the answer travels with the id.
func TestDecisionDrainTurnCarriesTheOperatorsActualAnswer(t *testing.T) {
	turn, ok := scheduledDecisionDrainTurn([]ReportHumanInput{
		{ID: "plan-proposal-scoring", WorkspacePath: "Workflow/example", Status: "answered", SelectedOptionID: "approve-both"},
		{ID: "strategy-proposal-stops", WorkspacePath: "Workflow/example", Status: "answered", SelectedOptionID: "reject"},
	})
	if !ok {
		t.Fatal("expected a turn")
	}
	for _, want := range []string{
		"Workflow/example: plan-proposal-scoring (answered: approve-both)",
		"Workflow/example: strategy-proposal-stops (answered: reject)",
		"get_human_input_request(workspace_path=<the exact Workflow/... path shown>",
		"mark_human_input_consumed",
		"BEFORE this run starts",
	} {
		if !strings.Contains(turn.query, want) {
			t.Fatalf("drain prompt missing %q:\n%s", want, turn.query)
		}
	}
	if strings.Contains(turn.query, "report_human_inputs") {
		t.Fatalf("drain prompt refers to the SQLite table as though it were a tool:\n%s", turn.query)
	}
	if !turn.decisionDrain {
		t.Fatal("the turn must be flagged decisionDrain so the loop treats it as non-fatal")
	}
	if turn.upgradeTarget != "" {
		t.Fatal("a decision drain is not a contract upgrade and must not carry an upgrade target")
	}
}

// TestDecisionDrainTurnRefusesToRunTheWorkflow guards the boundary that makes
// this safe to run before the schedule's own message: it applies decisions and
// nothing else. A drain that started executing steps would duplicate the run.
func TestDecisionDrainTurnRefusesToRunTheWorkflow(t *testing.T) {
	turn, _ := scheduledDecisionDrainTurn(plat093Answered("plan-proposal-1"))
	if !strings.Contains(turn.query, "Do NOT run the workflow") {
		t.Fatalf("drain prompt must forbid running the workflow:\n%s", turn.query)
	}
	// It must also refuse to consume what it could not apply — consuming to
	// tidy the list would erase the operator's decision while recording that it
	// was honored.
	if !strings.Contains(turn.query, "never to tidy the list") {
		t.Fatalf("drain prompt must forbid consuming what was not applied:\n%s", turn.query)
	}
	if !strings.Contains(turn.query, "post-run Pulse pass will pick it up") {
		t.Fatalf("drain prompt must name the fallback for what it cannot apply:\n%s", turn.query)
	}
}

// TestDecisionDrainTreatsSafeProofAsPartOfApplying captures the Social Media
// flattening regression from 2026-08-20. The operator approved replacing the
// fixed outer orchestrator, but the drain deferred it because a non-producing
// fixture had not run yet. The old, expensive topology then ran for hours. A
// safe fixture is work the pre-run turn can do itself; only evidence requiring
// a production run may be deferred.
func TestDecisionDrainTreatsSafeProofAsPartOfApplying(t *testing.T) {
	turn, _ := scheduledDecisionDrainTurn(plat093Answered("ops-decision-flatten-execution-pipeline"))
	for _, want := range []string{
		"SAFE VALIDATION IS PART OF APPLYING, NOT A REASON TO DEFER",
		"non-producing fixture",
		"Only evidence that inherently requires a real production run or external side effect may wait",
		"migrate both control flow and data flow",
		"exact `context_dependencies`",
		"removed step ids and obsolete path prefixes",
		"old/new artifact coexistence",
		"call validate_plan_change",
		"Treat passed=true as the required deterministic receipt",
		"if any unexplained old reference remains or the proof still fails, do not consume",
		"ISSUE FOLLOW-THROUGH",
		"canonical Pulse issue",
		"record the concrete action taken and close that issue",
		"Do not create a separate impact, verification, assessment, or monitoring record",
		"later Strategic Review can compare outcome evidence",
	} {
		if !strings.Contains(turn.query, want) {
			t.Fatalf("decision drain missing structural-application requirement %q:\n%s", want, turn.query)
		}
	}
}

// TestDecisionDrainRunsAfterUpgradesAndBeforeScheduleMessages pins the
// ordering, which is the entire point of the change. A contract upgrade can
// rewrite the very artifacts a decision edits, so the upgrade goes first; the
// schedule's own message must come last so the run executes on the applied
// decision rather than the stale plan.
func TestDecisionDrainRunsAfterUpgradesAndBeforeScheduleMessages(t *testing.T) {
	upgrades := []scheduledWorkshopTurn{
		{label: "upgrade-a", upgradeTarget: "1.0.21"},
		{label: "upgrade-b", upgradeTarget: "1.0.22"},
	}
	messages := []scheduledWorkshopTurn{
		{label: "schedule-message-1"},
		{label: "schedule-message-2"},
	}
	turns := append(append([]scheduledWorkshopTurn{}, upgrades...), messages...)
	drain, ok := scheduledDecisionDrainTurn(plat093Answered("plan-proposal-1"))
	if !ok {
		t.Fatal("expected a drain turn")
	}

	// Mirrors the insertion in executeWorkshopJob: at the upgrade count.
	insertAt := len(upgrades)
	turns = append(turns[:insertAt], append([]scheduledWorkshopTurn{drain}, turns[insertAt:]...)...)

	gotOrder := make([]string, 0, len(turns))
	for _, turn := range turns {
		gotOrder = append(gotOrder, turn.label)
	}
	want := []string{"upgrade-a", "upgrade-b", "decision-drain-preflight", "schedule-message-1", "schedule-message-2"}
	if len(gotOrder) != len(want) {
		t.Fatalf("turn order = %v, want %v", gotOrder, want)
	}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Fatalf("turn order = %v, want %v", gotOrder, want)
		}
	}
}

func TestPendingDecisionNoticeDoesNotAddATurnOrInferAnAnswer(t *testing.T) {
	turns := []scheduledWorkshopTurn{
		{label: "upgrade", upgradeTarget: "1.0.99", query: "upgrade"},
		{label: "decision-drain-preflight", decisionDrain: true, query: "drain"},
		{label: "schedule-message-1", query: "run the workflow"},
		{label: "schedule-message-2", query: "summarize"},
	}
	got := attachScheduledPendingDecisionNotice(turns, []ReportHumanInput{
		{ID: "ops-decision-step-execution-pipeline", Source: "ops_review", Status: "pending"},
	})
	if len(got) != len(turns) {
		t.Fatalf("pending decision context added a turn: got %d, want %d", len(got), len(turns))
	}
	if got[0].query != "upgrade" || got[1].query != "drain" || got[3].query != "summarize" {
		t.Fatalf("notice must affect only the first normal schedule turn: %+v", got)
	}
	for _, want := range []string{
		"ops-decision-step-execution-pipeline (source: ops_review)",
		"Do not infer an answer",
		"do not block unrelated safe work",
		"get_human_input_request",
		"run the workflow",
	} {
		if !strings.Contains(got[2].query, want) {
			t.Fatalf("pending decision notice missing %q:\n%s", want, got[2].query)
		}
	}
}
