package step_based_workflow

import (
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
