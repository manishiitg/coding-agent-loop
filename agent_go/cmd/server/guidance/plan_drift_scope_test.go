package guidance

import "testing"

func TestPlanDriftReviewsCompatibilityAndCanonicalPromptQuality(t *testing.T) {
	rendered, err := renderFromRegistry("plan-drift-review", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"change compatibility and step-prompt quality",
		"The stale flag requests an impact assessment, not a full checklist replay",
		`read_skill(skills=[{"name":"builder-reference","path":"references/step-description.md"}])`,
		"step_prompt_quality", "individual item prompts", "supplied schema/context",
		"A short prompt can fail and a long prompt can pass",
		"preferred alternative architecture is not itself drift",
		"An artifact created before a prompt/schema change is baseline evidence",
		"Treat applied fixes as fixed unless new evidence reproduces the defect",
		"no repair has been applied", "no step prompt to score",
	} {
		if !containsNormalizedText(rendered, want) {
			t.Errorf("plan drift missing %q", want)
		}
	}
	for _, obsolete := range []string{
		"PRAGMA table_info` across every table",
		"a real fix needs a future run's output to confirm",
		"a retained recent run shows the script actually performs its stated job",
	} {
		if containsNormalizedText(rendered, obsolete) {
			t.Errorf("plan drift retains excessive scope %q", obsolete)
		}
	}
	manual, err := renderFromRegistry("review-artifact-drift", tmplData{}, allKinds)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"step-description.md", "step_prompt_quality", "Do not repeat Part 1's prompt-quality", "closes applied fixes without waiting for later execution"} {
		if !containsNormalizedText(manual, want) {
			t.Errorf("manual drift missing %q", want)
		}
	}
}
