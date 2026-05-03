package server

import "testing"

func TestValidateInterventionPathsOnlyBlocksForbiddenPaths(t *testing.T) {
	cfg := DefaultExperimentsConfig()
	cfg.AllowedInterventionPaths = []string{"evaluation/"}

	if err := validateInterventionPaths([]InterventionChange{
		{Path: "learnings/_global/SKILL.md", Operation: OpReplace},
	}, cfg); err != nil {
		t.Fatalf("expected path outside allowed_intervention_paths to pass, got %v", err)
	}

	if err := validateInterventionPaths([]InterventionChange{
		{Path: "workflow.json", Operation: OpReplace},
	}, cfg); err == nil {
		t.Fatal("expected forbidden workflow.json path to fail")
	}
}
