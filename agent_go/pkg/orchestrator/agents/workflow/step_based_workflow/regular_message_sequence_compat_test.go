package step_based_workflow

import (
	"encoding/json"
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

func TestScriptedToolSchemasExposeNextStepID(t *testing.T) {
	for name, schema := range map[string]string{
		"add_scripted_step":    getAddRegularStepSchema(),
		"update_scripted_step": getUpdateRegularStepSchema(),
	} {
		var decoded struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal([]byte(schema), &decoded); err != nil {
			t.Fatalf("%s schema is invalid JSON: %v", name, err)
		}
		if _, ok := decoded.Properties["next_step_id"]; !ok {
			t.Fatalf("%s schema does not expose next_step_id", name)
		}
	}
}

func TestScriptedStepNextStepIDRoundTripAndUpdate(t *testing.T) {
	original := &RegularPlanStep{
		Type:             StepTypeRegular,
		CommonStepFields: CommonStepFields{ID: "script-a"},
		NextStepID:       "script-b",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RegularPlanStep
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.NextStepID != "script-b" {
		t.Fatalf("round-trip next_step_id = %q", decoded.NextStepID)
	}

	updated := mergePartialStepUpdate(original, PartialPlanStep{NextStepID: "shared"}).(*RegularPlanStep)
	if updated.NextStepID != "shared" {
		t.Fatalf("updated next_step_id = %q, want shared", updated.NextStepID)
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

func TestNonScriptedRegularStepNormalizesToMessageSequence(t *testing.T) {
	validation := &ValidationSchema{}
	config := &AgentConfigs{DeclaredExecutionMode: StepModeAgentic, DBAccess: "read"}
	regular := &RegularPlanStep{
		Type: StepTypeRegular,
		CommonStepFields: CommonStepFields{
			ID:                  "analyze-results",
			Title:               "Analyze results",
			Description:         "Analyze the saved evidence and explain the result.",
			ContextDependencies: []string{"fetch.json"},
			ContextOutput:       FlexibleContextOutput("analysis.json"),
			ValidationSchema:    validation,
		},
		NextStepID:   "shared",
		AgentConfigs: config,
	}

	if !shouldNormalizeRegularStepToMessageSequence(regular) {
		t.Fatal("expected a non-scripted regular step to use message-sequence normalization")
	}
	sequence := normalizeRegularStepToMessageSequence(regular)
	if sequence == nil || sequence.StepType() != StepTypeMessageSeq {
		t.Fatalf("expected message_sequence normalization, got %#v", sequence)
	}
	if sequence.ID != regular.ID || sequence.Title != regular.Title || sequence.Description != regular.Description {
		t.Fatalf("normalization changed step identity or instructions: %#v", sequence)
	}
	if sequence.ContextOutput != regular.ContextOutput || sequence.ValidationSchema != validation || sequence.AgentConfigs != config {
		t.Fatal("normalization did not preserve output, validation, or agent configuration")
	}
	if sequence.NextStepID != "shared" {
		t.Fatalf("normalization lost next_step_id: %q", sequence.NextStepID)
	}
	if len(sequence.Items) != 1 || sequence.Items[0].ID != normalizedRegularSequenceItemID || sequence.Items[0].Type != "user_message" {
		t.Fatalf("expected one normalized work turn, got %#v", sequence.Items)
	}
}

func TestScriptedRegularStepDoesNotNormalizeToMessageSequence(t *testing.T) {
	regular := &RegularPlanStep{
		CommonStepFields: CommonStepFields{ID: "fetch-data"},
		AgentConfigs:     &AgentConfigs{DeclaredExecutionMode: StepModeScripted},
	}
	if shouldNormalizeRegularStepToMessageSequence(regular) {
		t.Fatal("scripted regular step must retain the saved-script execution path")
	}
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
