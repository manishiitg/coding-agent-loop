package step_based_workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanValidationRejectsNestedMissingAndDuplicateIDs(t *testing.T) {
	const missingNestedID = `{"steps":[{
		"type":"todo_task","id":"orchestrator","title":"Orchestrator","description":"d",
		"predefined_routes":[{"route_id":"route","route_name":"Route","condition":"always",
			"sub_agent_step":{"type":"regular","title":"Nested","description":"d"}}]
	}]}`
	var missingPlan PlanningResponse
	if err := json.Unmarshal([]byte(missingNestedID), &missingPlan); err != nil {
		t.Fatalf("unmarshal missing-ID plan: %v", err)
	}
	if err := validatePlanStepIDs(missingPlan.Steps); err == nil || !strings.Contains(err.Error(), "sub_agent_step") {
		t.Fatalf("expected nested missing-ID error with route location, got %v", err)
	}

	const duplicateNestedID = `{"steps":[{
		"type":"regular","id":"shared-id","title":"Top","description":"d"
	},{
		"type":"todo_task","id":"orchestrator","title":"Orchestrator","description":"d",
		"predefined_routes":[{"route_id":"route","route_name":"Route","condition":"always",
			"sub_agent_step":{"type":"regular","id":"shared-id","title":"Nested","description":"d"}}]
	}]}`
	var duplicatePlan PlanningResponse
	if err := json.Unmarshal([]byte(duplicateNestedID), &duplicatePlan); err != nil {
		t.Fatalf("unmarshal duplicate-ID plan: %v", err)
	}
	if err := validateStepIDUniqueness(&duplicatePlan); err == nil || !strings.Contains(err.Error(), `duplicate step ID "shared-id"`) {
		t.Fatalf("expected nested duplicate-ID error, got %v", err)
	}
}

func TestParseStepConfigContentRejectsAmbiguousIDs(t *testing.T) {
	for name, content := range map[string]string{
		"duplicate": `{"steps":[{"id":"a"},{"id":"a"}]}`,
		"empty":     `{"steps":[{"id":""}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseStepConfigContent(content); err == nil {
				t.Fatal("expected invalid step config to be rejected")
			}
		})
	}
}

func TestRepairStepConfigsUsesLastEntryWins(t *testing.T) {
	configs := []StepConfig{{ID: "a", Title: "old"}, {ID: ""}, {ID: "b"}, {ID: "a", Title: "new"}}
	repaired, changed := repairStepConfigs(configs)
	if !changed {
		t.Fatal("expected legacy config to require repair")
	}
	if len(repaired) != 2 || repaired[0].ID != "b" || repaired[1].Title != "new" {
		t.Fatalf("unexpected repaired config: %+v", repaired)
	}
}
