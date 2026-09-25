package step_based_workflow

import (
	"strings"
	"testing"
)

// run_full_workflow variables must be declared: a typo must not become a
// silently ignored value.
func TestUndeclaredRunVariables(t *testing.T) {
	manifest := &VariablesManifest{Variables: []Variable{{Name: "PR_NUMBER"}, {Name: "GITHUB_REPO"}}}
	if problem := undeclaredRunVariables(manifest, map[string]string{"PR_NUMBER": "149"}); problem != "" {
		t.Fatalf("declared variable refused: %s", problem)
	}
	problem := undeclaredRunVariables(manifest, map[string]string{"PR_NUBMER": "149"})
	if !strings.Contains(problem, "PR_NUBMER") || !strings.Contains(problem, "GITHUB_REPO, PR_NUMBER") {
		t.Fatalf("unknown variable not reported with the declared names: %q", problem)
	}
	if undeclaredRunVariables(nil, map[string]string{"X": "1"}) == "" {
		t.Fatal("no manifest: every variable is undeclared")
	}
}

// Per-run variables override the group's saved values for that run only.
func TestRunVariablesOverrideGroupValues(t *testing.T) {
	groups := []VariableGroup{{Name: "staging", Values: map[string]string{"PR_NUMBER": "1", "GITHUB_REPO": "app"}}}
	got := applyWebhookVariableOverrides(groups, map[string]string{"PR_NUMBER": "149"})
	if got[0].Values["PR_NUMBER"] != "149" || got[0].Values["GITHUB_REPO"] != "app" || groups[0].Values["PR_NUMBER"] != "1" {
		t.Fatalf("override = %v, original = %v", got[0].Values, groups[0].Values)
	}
}
