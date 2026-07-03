package step_based_workflow

import (
	"strings"
	"testing"
)

func TestUpdateChiefOfStaffRecommendationStatusHTML(t *testing.T) {
	input := `<section>
<article class="entry cos-rec" data-cos-rec-id="cos-2026-07-03-test" data-status="proposed">
  <p>Recommendation</p>
</article>
</section>`

	got, changed := updateChiefOfStaffRecommendationStatusHTML(
		input,
		"cos-2026-07-03-test",
		"queued_auto_improve",
		"Accepted as strategy work.",
		"builder/improve.html#decision-1",
		"2026-07-03T12:00:00Z",
		"workflow-pulse",
	)
	if !changed {
		t.Fatal("updateChiefOfStaffRecommendationStatusHTML changed = false")
	}
	for _, want := range []string{
		`data-status="queued_auto_improve"`,
		`data-status-updated-at="2026-07-03T12:00:00Z"`,
		`data-status-updated-by="workflow-pulse"`,
		`data-status-note="Accepted as strategy work."`,
		`data-status-evidence="builder/improve.html#decision-1"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("updated HTML missing %q:\n%s", want, got)
		}
	}
}

func TestUpdateChiefOfStaffRecommendationStatusHTMLMissingID(t *testing.T) {
	input := `<article data-cos-rec-id="other" data-status="proposed"></article>`
	got, changed := updateChiefOfStaffRecommendationStatusHTML(input, "missing", "done", "Done.", "", "2026-07-03T12:00:00Z", "workflow-pulse")
	if changed {
		t.Fatal("updateChiefOfStaffRecommendationStatusHTML changed = true for missing id")
	}
	if got != input {
		t.Fatalf("content changed for missing id:\n%s", got)
	}
}

func TestWorkshopModeIncludesChiefOfStaffRecommendationStatusTool(t *testing.T) {
	tools := GetToolsForWorkshopMode("workshop")
	for _, tool := range tools {
		if tool == chiefOfStaffRecommendationStatusToolName {
			return
		}
	}
	t.Fatalf("workshop mode tools missing %s", chiefOfStaffRecommendationStatusToolName)
}
