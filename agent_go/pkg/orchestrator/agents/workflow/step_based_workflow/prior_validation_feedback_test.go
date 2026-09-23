package step_based_workflow

import (
	"context"
	"strings"
	"testing"
)

// A scripted step that fails gets its error handed back via ScriptedPriorError
// and repairs itself. An agentic step had no equivalent: it received the schema,
// wrote output that violated it, prevalidation filed a concern afterwards, and
// the next run read an identical prompt. tectonicusadaytrading's
// deliver-briefing recurred at seen_count 3 for exactly this reason.
func TestPriorPreValidationFailuresAreCarriedIntoTheNextRun(t *testing.T) {
	ctx := context.Background()
	workspacePath := concernsWorkspace(t)

	seedRunConcerns(t, workspacePath, "iteration-0", "", "deliver-briefing", ConcernPhasePreValidation,
		"prevalidation gate failed at delivery_receipt.json $.delivery_status: Path $.delivery_status must exist but was not found")

	failures, err := LoadPriorPreValidationFailures(ctx, workspacePath, "deliver-briefing", 10)
	if err != nil {
		t.Fatalf("load prior failures: %v", err)
	}
	if len(failures) != 1 {
		t.Fatalf("expected the step's own prevalidation failure, got %d", len(failures))
	}

	rendered := FormatPriorPreValidationFailures(failures)
	if !strings.Contains(rendered, "$.delivery_status") {
		t.Fatalf("rendered block does not name the failing path:\n%s", rendered)
	}
	if !strings.Contains(rendered, "CONCERNS:") {
		t.Logf("note: concern text is stored without the CONCERNS: prefix — %q", rendered)
	}
}

// Only this step's failures, and only prevalidation. A step must not be handed
// another step's problems, and a review-phase finding is Pulse's to route.
func TestPriorFailuresAreScopedToTheStepAndPhase(t *testing.T) {
	ctx := context.Background()
	workspacePath := concernsWorkspace(t)

	for _, c := range []struct{ step, phase, text string }{
		{"deliver-briefing", ConcernPhasePreValidation, "mine"},
		{"other-step", ConcernPhasePreValidation, "not mine"},
		{"deliver-briefing", ConcernPhaseExecution, "different phase"},
	} {
		seedRunConcerns(t, workspacePath, "iteration-0", "", c.step, c.phase, c.text)
	}

	failures, err := LoadPriorPreValidationFailures(ctx, workspacePath, "deliver-briefing", 10)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(failures) != 1 || !strings.Contains(failures[0].Text, "mine") {
		t.Fatalf("expected only this step's prevalidation failure, got %+v", failures)
	}
}

// Nothing to say must render nothing, so the prompt block disappears rather than
// showing an empty header to every step that has never failed.
func TestNoPriorFailuresRendersNothing(t *testing.T) {
	if got := FormatPriorPreValidationFailures(nil); got != "" {
		t.Fatalf("expected empty render for no failures, got %q", got)
	}
}
