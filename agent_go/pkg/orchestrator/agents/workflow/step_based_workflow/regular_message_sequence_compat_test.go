package step_based_workflow

import (
	"strings"
	"testing"
)

func TestScriptedToolNamesArePlanMutationsAndLegacyNamesAreNot(t *testing.T) {
	for _, name := range []string{"add_scripted_step", "update_scripted_step"} {
		if !IsPlanModificationTool(name) {
			t.Fatalf("expected %q to be recognized as a plan mutation", name)
		}
	}
	for _, name := range []string{"add_regular_step", "update_regular_step"} {
		if IsPlanModificationTool(name) {
			t.Fatalf("legacy authoring tool %q must not remain callable", name)
		}
	}
}

func TestValidateScriptedStepUpdateTarget(t *testing.T) {
	plan := &PlanningResponse{Steps: []PlanStepInterface{
		&RegularPlanStep{CommonStepFields: CommonStepFields{ID: "scripted"}},
		&RegularPlanStep{CommonStepFields: CommonStepFields{ID: "legacy-agentic"}},
		&MessageSequencePlanStep{CommonStepFields: CommonStepFields{ID: "sequence"}},
	}}
	configs := []StepConfig{{
		ID:           "scripted",
		AgentConfigs: &AgentConfigs{DeclaredExecutionMode: StepModeScripted},
	}}

	if err := validateScriptedStepUpdateTarget(plan, configs, "scripted"); err != nil {
		t.Fatalf("declared scripted step was rejected: %v", err)
	}
	if err := validateScriptedStepUpdateTarget(plan, configs, "legacy-agentic"); err == nil {
		t.Fatal("legacy agentic regular step must not be editable through update_scripted_step")
	}
	if err := validateScriptedStepUpdateTarget(plan, configs, "sequence"); err == nil {
		t.Fatal("message_sequence must use its type-specific update tool")
	}
}

func TestLegacyAgenticRegularStepIsRejected(t *testing.T) {
	regular := &RegularPlanStep{
		Type:             StepTypeRegular,
		CommonStepFields: CommonStepFields{ID: "analyze-results", Title: "Analyze results"},
		AgentConfigs:     &AgentConfigs{DeclaredExecutionMode: StepModeAgentic},
	}

	err := validateRegularStepExecutionModes([]PlanStepInterface{regular})
	if err == nil {
		t.Fatal("agentic regular step must be rejected")
	}
	if got := err.Error(); got == "" || !containsAll(got, "analyze-results", "message_sequence", "scripted regular") {
		t.Fatalf("unexpected rejection error: %q", got)
	}
}

func TestScriptedRegularStepRemainsSupported(t *testing.T) {
	regular := &RegularPlanStep{
		CommonStepFields: CommonStepFields{ID: "fetch-data"},
		AgentConfigs:     &AgentConfigs{DeclaredExecutionMode: StepModeScripted},
	}
	if err := validateRegularStepExecutionModes([]PlanStepInterface{regular}); err != nil {
		t.Fatalf("scripted regular step was rejected: %v", err)
	}
}

func TestNestedLegacyAgenticRegularStepIsRejected(t *testing.T) {
	nested := &RegularPlanStep{
		CommonStepFields: CommonStepFields{ID: "nested-analysis"},
		AgentConfigs:     nil,
	}
	root := &TodoTaskPlanStep{
		CommonStepFields: CommonStepFields{ID: "orchestrate"},
		PredefinedRoutes: []PlanOrchestrationRoute{{RouteID: "analyze", SubAgentStep: nested}},
	}
	err := validateRegularStepExecutionModes([]PlanStepInterface{root})
	if err == nil || !containsAll(err.Error(), "nested-analysis", "message_sequence") {
		t.Fatalf("nested agentic regular rejection = %v", err)
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}

func TestUpsertNewScriptedRegularStepConfig(t *testing.T) {
	configs := []StepConfig{{ID: "fetch-data", AgentConfigs: nil}}
	configs = upsertNewScriptedRegularStepConfig(configs, "fetch-data", "Fetch data")
	if len(configs) != 1 {
		t.Fatalf("expected existing config to be updated, got %d entries", len(configs))
	}
	cfg := configs[0]
	if cfg.Title != "Fetch data" || cfg.AgentConfigs == nil {
		t.Fatalf("missing updated scripted config: %#v", cfg)
	}
	if cfg.AgentConfigs.DeclaredExecutionMode != StepModeScripted {
		t.Fatalf("expected scripted mode, got %q", cfg.AgentConfigs.DeclaredExecutionMode)
	}
	if cfg.AgentConfigs.UseCodeExecutionMode == nil || !*cfg.AgentConfigs.UseCodeExecutionMode {
		t.Fatal("scripted mode did not enable code execution")
	}

	configs = upsertNewScriptedRegularStepConfig(configs, "normalize-data", "Normalize data")
	if len(configs) != 2 || configs[1].ID != "normalize-data" || configs[1].AgentConfigs == nil {
		t.Fatalf("expected a new scripted config entry, got %#v", configs)
	}
}

func TestCollectRegularPlanStepsIncludesNestedTodoRoutes(t *testing.T) {
	regular := &RegularPlanStep{CommonStepFields: CommonStepFields{ID: "fetch-data", Title: "Fetch data"}}
	sequence := &MessageSequencePlanStep{CommonStepFields: CommonStepFields{ID: "analyze-data"}}
	nested := &TodoTaskPlanStep{
		CommonStepFields: CommonStepFields{ID: "nested"},
		PredefinedRoutes: []PlanOrchestrationRoute{
			{RouteID: "fetch-data", SubAgentStep: regular},
			{RouteID: "analyze-data", SubAgentStep: sequence},
		},
	}
	root := &TodoTaskPlanStep{
		CommonStepFields: CommonStepFields{ID: "root"},
		PredefinedRoutes: []PlanOrchestrationRoute{{RouteID: "nested", SubAgentStep: nested}},
	}

	got := collectRegularPlanSteps(root)
	if len(got) != 1 || got[0] != regular {
		t.Fatalf("expected only the nested regular boundary, got %#v", got)
	}
}

func TestTodoTaskRejectsIncompleteMessageSequenceRoute(t *testing.T) {
	step := &TodoTaskPlanStep{
		CommonStepFields: CommonStepFields{
			ID:          "orchestrate",
			Title:       "Orchestrate",
			Description: "Delegate work.",
		},
		NextStepID: "end",
		PredefinedRoutes: []PlanOrchestrationRoute{{
			RouteID:   "analyze",
			RouteName: "Analyze",
			SubAgentStep: &MessageSequencePlanStep{CommonStepFields: CommonStepFields{
				ID:          "analyze",
				Title:       "Analyze",
				Description: "Analyze the evidence.",
			}},
		}},
	}

	if err := validateTodoTaskStepFieldsTyped(step); err == nil {
		t.Fatal("expected an empty message_sequence route to fail validation")
	}
	step.PredefinedRoutes[0].SubAgentStep.(*MessageSequencePlanStep).Items = []MessageSequenceItem{{
		ID:      "analyze",
		Type:    "user_message",
		Message: "Analyze the evidence and save the result.",
	}}
	if err := validateTodoTaskStepFieldsTyped(step); err != nil {
		t.Fatalf("expected a complete message_sequence route to pass validation: %v", err)
	}
}
