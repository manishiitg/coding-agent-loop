package guidance

import "testing"

func TestTechnicalFixGuidanceClosesAppliedRepairsWithoutVerificationQueue(t *testing.T) {
	for kind, wants := range map[string][]string{
		"pulse-review-fixer":    {"treat the issue as fixed and close it", "Reopen the same issue only when new evidence reproduces", "Do not select closed `changed_unverified` issues"},
		"pulse-gate":            {"An applied technical fix is treated as fixed", "lack of runtime proof alone must not select Technical Review"},
		"fix-verification":      {"close its issue immediately", "no blanket requirement to execute the full workflow", "Do not add a future `next_check` solely for this applied repair"},
		"pulse-bug-review":      {"close it immediately", "Do not wait for a later run"},
		"pulse-fixer-practices": {"it closes immediately without a future verification obligation", "not a stop condition or pending work"},
	} {
		rendered, err := renderFromRegistry(kind, tmplData{}, referenceKinds)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !containsNormalizedText(rendered, want) {
				t.Errorf("%s missing recurrence policy %q", kind, want)
			}
		}
		for _, obsolete := range []string{"Closure requires the real runtime path, a post-change producing run", "first verify every due `changed_unverified`", "on that later run close it through a"} {
			if containsNormalizedText(rendered, obsolete) {
				t.Errorf("%s still requires verification queue: %q", kind, obsolete)
			}
		}
	}
}
