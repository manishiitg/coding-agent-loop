package step_based_workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

func routingStepWithTargets(targets ...string) *RoutingPlanStep {
	routes := make([]RoutingRoute, 0, len(targets))
	for index, target := range targets {
		routes = append(routes, RoutingRoute{
			RouteID:    "route-" + string(rune('a'+index)),
			RouteName:  "Route " + string(rune('A'+index)),
			Condition:  "condition",
			NextStepID: target,
		})
	}
	return &RoutingPlanStep{
		Type: StepTypeRouting,
		CommonStepFields: CommonStepFields{
			ID:    "router",
			Title: "Router",
		},
		RoutingQuestion: "Which route?",
		Routes:          routes,
		DefaultRouteID:  "route-a",
	}
}

func TestValidatePlanStructureReportsEveryDanglingReference(t *testing.T) {
	plan := &PlanningResponse{Steps: []PlanStepInterface{
		routingStepWithTargets("missing-a", "missing-b"),
		&HumanInputPlanStep{
			Type: StepTypeHumanInput,
			CommonStepFields: CommonStepFields{
				ID:    "approval",
				Title: "Approval",
			},
			Question:        "Approve?",
			ResponseType:    "yesno",
			NextStepID:      "end",
			IfYesNextStepID: "missing-c",
			IfNoNextStepID:  "end",
		},
		&RegularPlanStep{
			Type:             StepTypeRegular,
			CommonStepFields: CommonStepFields{ID: "scripted"},
			NextStepID:       "missing-d",
		},
	}}

	err := ValidatePlanStructure(plan)
	if err == nil {
		t.Fatal("expected graph validation error")
	}
	var validationErr *PlanValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error type = %T, want *PlanValidationError", err)
	}
	for _, want := range []string{
		"PLAN_GRAPH_INVALID: 4 next-step reference(s)",
		`route "route-a".next_step_id in step "router" points to missing step "missing-a"`,
		`route "route-b".next_step_id in step "router" points to missing step "missing-b"`,
		`if_yes_next_step_id in step "approval" points to missing step "missing-c"`,
		`next_step_id in step "scripted" points to missing step "missing-d"`,
		"No changes were saved",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error missing %q:\n%s", want, err)
		}
	}
}

func TestDeletePlanStepsRejectsReferencedTargetWithoutWriting(t *testing.T) {
	oldPlan := &PlanningResponse{Steps: []PlanStepInterface{
		routingStepWithTargets("target", "target"),
		regularStep("target"),
	}}
	planJSON, err := json.Marshal(oldPlan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	writes := 0
	readFile := func(context.Context, string) (string, error) {
		return string(planJSON), nil
	}
	writeFile := func(context.Context, string, string) error {
		writes++
		return nil
	}
	executor := createDeletePlanStepsExecutor("workflow", loggerv2.NewNoop(), readFile, writeFile, nil, nil)

	_, err = executor(context.Background(), map[string]interface{}{
		"deleted_step_ids": []interface{}{"target"},
		"reason":           "remove obsolete target",
	})
	if err == nil {
		t.Fatal("expected referenced-step deletion to be rejected")
	}
	for _, want := range []string{"step deletion rejected", "PLAN_GRAPH_INVALID: 2", "route-a", "route-b", "left unchanged"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("delete error missing %q:\n%s", want, err)
		}
	}
	if writes != 0 {
		t.Fatalf("write callback called %d times; rejected deletion must be atomic", writes)
	}
}

func TestUpdateRoutingStepCanRepairPreviouslyDanglingGraph(t *testing.T) {
	brokenPlan := &PlanningResponse{Steps: []PlanStepInterface{
		routingStepWithTargets("already-deleted", "end"),
	}}
	planJSON, err := json.Marshal(brokenPlan)
	if err != nil {
		t.Fatalf("marshal broken plan: %v", err)
	}
	var writtenPlan string
	readFile := func(_ context.Context, path string) (string, error) {
		if strings.HasSuffix(path, "step_config.json") {
			return "[]", nil
		}
		if strings.Contains(path, "evaluation/") {
			return "", errors.New("not found")
		}
		return string(planJSON), nil
	}
	writeFile := func(_ context.Context, path, content string) error {
		if strings.HasSuffix(path, "plan.json") {
			writtenPlan = content
		}
		return nil
	}
	executor := createUpdateRoutingStepExecutor("workflow", loggerv2.NewNoop(), readFile, writeFile, nil)

	result, err := executor(context.Background(), map[string]interface{}{
		"existing_step_id": "router",
		"routes": []interface{}{
			map[string]interface{}{"route_id": "route-a", "route_name": "Route A", "condition": "condition", "next_step_id": "end"},
			map[string]interface{}{"route_id": "route-b", "route_name": "Route B", "condition": "condition", "next_step_id": "end"},
		},
		"reason": "repair route left dangling by an older deletion",
	})
	if err != nil {
		t.Fatalf("repair update failed: %v", err)
	}
	if !strings.Contains(result, "Successfully updated routing step") {
		t.Fatalf("unexpected update result: %s", result)
	}
	if writtenPlan == "" {
		t.Fatal("repair did not persist the now-valid plan")
	}
	var repaired PlanningResponse
	if err := json.Unmarshal([]byte(writtenPlan), &repaired); err != nil {
		t.Fatalf("decode repaired plan: %v", err)
	}
	if err := ValidatePlanStructure(&repaired); err != nil {
		t.Fatalf("persisted repair is invalid: %v", err)
	}
}
