package step_based_workflow

import "testing"

func TestPopulateRuntimeFields_PopulatesExecutionTierForRegularStep(t *testing.T) {
	step := &RegularPlanStep{
		CommonStepFields: CommonStepFields{
			ID:    "step-1",
			Title: "Regular Step",
		},
	}

	err := populateRuntimeFields(step, []StepConfig{
		{
			ID: "step-1",
			AgentConfigs: &AgentConfigs{
				ExecutionTier: "medium",
			},
		},
	})
	if err != nil {
		t.Fatalf("populateRuntimeFields returned error: %v", err)
	}
	if step.AgentConfigs == nil {
		t.Fatal("expected AgentConfigs to be populated for regular step")
	}
	if step.AgentConfigs.ExecutionTier != "medium" {
		t.Fatalf("expected execution_tier to be populated on regular step, got %q", step.AgentConfigs.ExecutionTier)
	}
}
