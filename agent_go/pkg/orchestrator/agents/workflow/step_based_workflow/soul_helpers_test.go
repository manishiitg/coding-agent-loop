package step_based_workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSoulScaffoldContainsOnlyStableIntentSections(t *testing.T) {
	scaffold := SoulScaffold("Demo Workflow")

	for _, want := range []string{"# Demo Workflow", "## Objective", "## Success Criteria"} {
		if !strings.Contains(scaffold, want) {
			t.Fatalf("SoulScaffold missing %q:\n%s", want, scaffold)
		}
	}
	for _, forbidden := range []string{"## Why", "## Decisions & Constraints", "## Key References", "architecture", "decision log"} {
		if strings.Contains(scaffold, forbidden) {
			t.Fatalf("SoulScaffold should not invite revisable content %q:\n%s", forbidden, scaffold)
		}
	}
}

const soulWithConstraints = `# Trading Workflow

## Objective
Beat the benchmark.

## Success Criteria
Outperformance > 0.

## Constraints

- **Risk budget (owner-approved 2026-07-03):** max 1.5% capital risk per trade, up to 7 concurrent
  real positions, no single archetype > 60% of deployed capital.

## Notifications
Send to slack.
`

func readerFor(content string) func(context.Context, string) (string, error) {
	return func(context.Context, string) (string, error) { return content, nil }
}

func TestSoulRuntimePreservesGoalPriorities(t *testing.T) {
	objective := "### Primary goals\n- Make conversations feel immediate.\n\n### Secondary goals\n- Make the learner experience ready quickly."
	content := "## Objective\n" + objective + "\n\n## Success Criteria\n- Meet agreed metric targets.\n\n## Constraints\n- Preserve privacy."
	got, success, constraints, err := ReadWorkflowSoulSections(context.Background(), "Workflow/learner", readerFor(content))
	if err != nil || got != objective || success != "- Meet agreed metric targets." || constraints != "- Preserve privacy." {
		t.Fatalf("priorities must reach runtime without losing acceptance or constraints: %q, %q, %q, %v", got, success, constraints, err)
	}
	if stripSoulTodoPlaceholder(extractSoulSection(SoulScaffold("New workflow"), soulObjectiveSection)) != "" {
		t.Fatal("a scaffold must not be treated as configured outcome goals")
	}
}

// The `## Constraints` section was a documented soul.md convention that nothing
// parsed, so owner-approved values were retyped into step descriptions and
// learnings and drifted from the owner's decision. It must extract cleanly and
// stop at the next H2 — pulling in `## Notifications` would inject delivery
// preferences into every agent prompt as if they were binding policy.
func TestReadWorkflowSoulSectionsExtractsConstraints(t *testing.T) {
	obj, sc, constraints, err := ReadWorkflowSoulSections(context.Background(), "Workflow/trading", readerFor(soulWithConstraints))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj != "Beat the benchmark." {
		t.Fatalf("objective = %q", obj)
	}
	if sc != "Outperformance > 0." {
		t.Fatalf("successCriteria = %q", sc)
	}
	if !strings.Contains(constraints, "up to 7 concurrent") || !strings.Contains(constraints, "1.5%") {
		t.Fatalf("constraints body not captured: %q", constraints)
	}
	if strings.Contains(constraints, "Notifications") || strings.Contains(constraints, "Send to slack") {
		t.Fatalf("constraints bled into the next H2 section: %q", constraints)
	}
}

// A missing soul.md is a valid intermediate state (workflow not yet scaffolded)
// and must not surface as an error, or every step would log a spurious failure.
func TestReadWorkflowSoulSectionsTolerantOfMissingFile(t *testing.T) {
	_, _, constraints, err := ReadWorkflowSoulSections(context.Background(), "Workflow/x",
		func(context.Context, string) (string, error) { return "", errors.New("file not found") })
	if err != nil {
		t.Fatalf("missing soul.md should not error, got %v", err)
	}
	if constraints != "" {
		t.Fatalf("expected empty constraints, got %q", constraints)
	}
}
