package step_based_workflow

import (
	"reflect"
	"strings"
	"testing"
)

func TestMigrateRunScopedRoutesRepairsRouterAndDestinations(t *testing.T) {
	plan := routeIsolationTestPlan("db/assets/route_selection.json", nil)
	plan.Steps[0].(*RegularPlanStep).Description = "Write route_selection.json and durable db/assets/route_selection.json."
	plan.Steps[2].(*RegularPlanStep).Description = "Read db/assets/route_selection.json before review."
	plan.Steps[3].(*RegularPlanStep).Description = "Read db/assets/route_selection.json before skip."

	result := migrateRunScopedRoutesInPlan(plan)
	if !reflect.DeepEqual(result.Routers, []string{"branch"}) || !reflect.DeepEqual(result.Producers, []string{"gate"}) {
		t.Fatalf("unexpected migration result: %+v", result)
	}
	if !reflect.DeepEqual(result.Consumers, []string{"review", "skip"}) {
		t.Fatalf("unexpected consumers: %+v", result.Consumers)
	}
	branch := plan.Steps[1].(*BranchPlanStep)
	if branch.RouteSourceFile != "" || !reflect.DeepEqual(branch.ContextDependencies, []string{routeSelectionFileName}) {
		t.Fatalf("branch was not migrated to a run-scoped dependency: %+v", branch)
	}
	for _, index := range []int{2, 3} {
		step := plan.Steps[index].(*RegularPlanStep)
		if !reflect.DeepEqual(step.ContextDependencies, []string{routeSelectionFileName}) {
			t.Fatalf("destination %s dependencies = %v", step.ID, step.ContextDependencies)
		}
		if strings.Contains(step.Description, "db/assets/route_selection.json") {
			t.Fatalf("destination %s retained shared path instruction: %s", step.ID, step.Description)
		}
	}
	if strings.Contains(plan.Steps[0].GetDescription(), "db/assets/route_selection.json") {
		t.Fatalf("producer retained shared path instruction: %s", plan.Steps[0].GetDescription())
	}
	if err := ValidatePlanStructure(plan); err != nil {
		t.Fatalf("migrated plan did not validate: %v", err)
	}
}

func TestMigrateRunScopedRoutesLeavesIntentionalSharedSource(t *testing.T) {
	plan := routeIsolationTestPlan("db/assets/manual_route_selection.json", nil)
	before := plan.Steps[1].(*BranchPlanStep).RouteSourceFile
	result := migrateRunScopedRoutesInPlan(plan)
	if !result.noOp() {
		t.Fatalf("intentional shared source was migrated: %+v", result)
	}
	if got := plan.Steps[1].(*BranchPlanStep).RouteSourceFile; got != before {
		t.Fatalf("shared source changed from %q to %q", before, got)
	}
}

func TestMigrateRunScopedRoutesIsIdempotent(t *testing.T) {
	plan := routeIsolationTestPlan("db/assets/route_selection.json", nil)
	first := migrateRunScopedRoutesInPlan(plan)
	if first.noOp() {
		t.Fatal("first migration unexpectedly did nothing")
	}
	second := migrateRunScopedRoutesInPlan(plan)
	if !second.noOp() {
		t.Fatalf("second migration changed the plan again: %+v", second)
	}
}
