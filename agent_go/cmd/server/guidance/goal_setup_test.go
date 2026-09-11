package guidance

import (
	"strings"
	"testing"
)

func TestGoalSetupSharesCanonicalMeasurementFlow(t *testing.T) {
	for _, kind := range []string{"setup-goals", "define-success"} {
		text, err := renderFromRegistry(kind, tmplData{}, allKinds)
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{"configure_goal_metrics", "record_goal_observations", "one primary metric", "backfill", "pulse_enabled=true", "### Primary goals", "### Secondary goals", "Goal priority is independent of metric role", "never infer", "quietly demote or drop"} {
			if !strings.Contains(text, required) {
				t.Errorf("%s missing %s", kind, required)
			}
		}
	}
}
