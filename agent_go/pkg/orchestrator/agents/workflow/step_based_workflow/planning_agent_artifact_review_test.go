package step_based_workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestClearDescriptionReviewedAfterPlanUpdate(t *testing.T) {
	ctx := context.Background()
	files := map[string]string{
		"planning/step_config.json": `{
  "steps": [
    {
      "id": "step-a",
      "agent_configs": {
        "description_reviewed": true,
        "review_notes": "old review"
      }
    }
  ]
}`,
	}

	readFile := func(_ context.Context, path string) (string, error) {
		return files[path], nil
	}
	writeFile := func(_ context.Context, path, content string) error {
		files[path] = content
		return nil
	}

	cleared, err := clearDescriptionReviewedAfterPlanUpdate(ctx, "", "step-a", []PlanFieldChange{{
		StepID: "step-a",
		Field:  "description",
	}}, readFile, writeFile)
	if err != nil {
		t.Fatalf("clearDescriptionReviewedAfterPlanUpdate returned error: %v", err)
	}
	if !cleared {
		t.Fatalf("expected description_reviewed to be cleared")
	}

	var out StepConfigFile
	if err := json.Unmarshal([]byte(files["planning/step_config.json"]), &out); err != nil {
		t.Fatalf("updated step_config.json is invalid JSON: %v", err)
	}
	if got := out.Steps[0].AgentConfigs.DescriptionReviewed; got != nil {
		t.Fatalf("description_reviewed = %v, want nil", *got)
	}
	if got := out.Steps[0].AgentConfigs.ReviewNotes; got != "old review" {
		t.Fatalf("review_notes = %q, want preserved review notes", got)
	}
}

func TestClearDescriptionReviewedAfterPlanUpdateSkipsTitleOnly(t *testing.T) {
	ctx := context.Background()
	writeCalled := false
	files := map[string]string{
		"planning/step_config.json": `{"steps":[{"id":"step-a","agent_configs":{"description_reviewed":true}}]}`,
	}

	cleared, err := clearDescriptionReviewedAfterPlanUpdate(ctx, "", "step-a", []PlanFieldChange{{
		StepID: "step-a",
		Field:  "title",
	}}, func(_ context.Context, path string) (string, error) {
		return files[path], nil
	}, func(_ context.Context, path, content string) error {
		writeCalled = true
		files[path] = content
		return nil
	})
	if err != nil {
		t.Fatalf("clearDescriptionReviewedAfterPlanUpdate returned error: %v", err)
	}
	if cleared {
		t.Fatalf("title-only changes should not clear description_reviewed")
	}
	if writeCalled {
		t.Fatalf("title-only changes should not write step_config.json")
	}
}

func TestArtifactReviewNotices(t *testing.T) {
	updateNotice := buildPlanStepDependentArtifactReviewNotice("step-a", []PlanFieldChange{{
		StepID: "step-a",
		Field:  "description",
	}}, true)
	for _, want := range []string{
		"Dependent artifact review required",
		"validation_schema",
		"learnings/step-a",
		"db/README.md",
		"knowledgebase_access",
		"reports/report_plan.json",
		"review-artifact-drift",
	} {
		if !strings.Contains(updateNotice, want) {
			t.Fatalf("update notice missing %q:\n%s", want, updateNotice)
		}
	}

	addNotice := buildAddedStepArtifactSetupNotice("new-step", "regular")
	for _, want := range []string{"New step artifact setup required", "planning/step_config.json", "learnings/new-step/main.py"} {
		if !strings.Contains(addNotice, want) {
			t.Fatalf("add notice missing %q:\n%s", want, addNotice)
		}
	}

	deleteNotice := buildDeletedStepArtifactCleanupNotice([]string{"old-step"}, []string{"old-step"})
	for _, want := range []string{"Deleted step artifact cleanup required", "Removed matching planning/step_config.json entries", "learnings/<step-id>"} {
		if !strings.Contains(deleteNotice, want) {
			t.Fatalf("delete notice missing %q:\n%s", want, deleteNotice)
		}
	}

	routeNotice := buildTodoTaskRouteArtifactReviewNotice("parent", "route-a", "deleted", true)
	for _, want := range []string{"Todo route artifact review required", "description_reviewed", "learnings/route-a"} {
		if !strings.Contains(routeNotice, want) {
			t.Fatalf("route notice missing %q:\n%s", want, routeNotice)
		}
	}
}

func TestUpdateSingleStepTracksArtifactRelevantFields(t *testing.T) {
	plan := &PlanningResponse{Steps: []PlanStepInterface{
		&HumanInputPlanStep{
			CommonStepFields: CommonStepFields{
				ID:    "ask-user",
				Title: "Ask user",
			},
			Question:   "Old question?",
			NextStepID: "step-old",
		},
		&RoutingPlanStep{
			CommonStepFields: CommonStepFields{
				ID:    "route",
				Title: "Route",
			},
			RoutingQuestion: "Old route?",
			Routes: []RoutingRoute{
				{RouteID: "a", RouteName: "A", Condition: "old", NextStepID: "end"},
				{RouteID: "b", RouteName: "B", Condition: "old", NextStepID: "end"},
			},
		},
	}}

	var humanChanges []PlanFieldChange
	if _, _, err := updateSingleStep(plan, PartialPlanStep{
		ExistingStepID: "ask-user",
		Question:       "New question?",
		ResponseType:   "multiple_choice",
		Options:        []string{"A", "B"},
		NextStepID:     "step-new",
	}, &humanChanges); err != nil {
		t.Fatalf("updateSingleStep human input returned error: %v", err)
	}
	for _, want := range []string{"question", "response_type", "options", "next_step_id"} {
		if !hasPlanFieldChange(humanChanges, want) {
			t.Fatalf("human field changes missing %q: %#v", want, humanChanges)
		}
	}

	var routingChanges []PlanFieldChange
	if _, _, err := updateSingleStep(plan, PartialPlanStep{
		ExistingStepID:   "route",
		RoutingQuestion:  "New route?",
		DefaultRouteID:   "b",
		Routes:           []RoutingRoute{{RouteID: "a", RouteName: "A", Condition: "new", NextStepID: "end"}, {RouteID: "b", RouteName: "B", Condition: "new", NextStepID: "end"}},
		ContextOutput:    "route-output.json",
		ValidationSchema: nil,
	}, &routingChanges); err != nil {
		t.Fatalf("updateSingleStep routing returned error: %v", err)
	}
	for _, want := range []string{"routing_question", "routes", "default_route_id", "context_output"} {
		if !hasPlanFieldChange(routingChanges, want) {
			t.Fatalf("routing field changes missing %q: %#v", want, routingChanges)
		}
	}
}

func hasPlanFieldChange(changes []PlanFieldChange, field string) bool {
	for _, change := range changes {
		if change.Field == field {
			return true
		}
	}
	return false
}
