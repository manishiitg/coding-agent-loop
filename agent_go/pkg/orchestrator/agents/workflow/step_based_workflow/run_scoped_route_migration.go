package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

const workflowContractRunScopedRoutesVersionLabel = "1.0.42"

type runScopedRouteMigrationResult struct {
	Routers   []string `json:"routers"`
	Producers []string `json:"producers"`
	Consumers []string `json:"consumers"`
}

func (r runScopedRouteMigrationResult) noOp() bool { return len(r.Routers) == 0 }

func mutableCommonStepFields(step PlanStepInterface) *CommonStepFields {
	switch typed := step.(type) {
	case *RegularPlanStep:
		return &typed.CommonStepFields
	case *MessageSequencePlanStep:
		return &typed.CommonStepFields
	case *RoutingPlanStep:
		return &typed.CommonStepFields
	case *BranchPlanStep:
		return &typed.CommonStepFields
	case *HumanInputPlanStep:
		return &typed.CommonStepFields
	case *OrchestratorPlanStep:
		return &typed.CommonStepFields
	default:
		return nil
	}
}

func setRouteSourceFile(step routeSwitchStep, value string) {
	switch typed := step.(type) {
	case *RoutingPlanStep:
		typed.RouteSourceFile = value
	case *BranchPlanStep:
		typed.RouteSourceFile = value
	}
}

func addRunScopedRouteDependency(step PlanStepInterface) bool {
	common := mutableCommonStepFields(step)
	if common == nil {
		return false
	}
	for _, dep := range common.ContextDependencies {
		if isRouteSelectionDependency(dep) {
			return false
		}
	}
	common.ContextDependencies = append(common.ContextDependencies, routeSelectionFileName)
	return true
}

func rewriteSharedRouteInstruction(step PlanStepInterface) bool {
	common := mutableCommonStepFields(step)
	if common == nil || !strings.Contains(common.Description, "db/assets/route_selection.json") {
		return false
	}
	common.Description = strings.ReplaceAll(
		common.Description,
		"db/assets/route_selection.json",
		"the current run's declared route_selection.json dependency",
	)
	return true
}

// migrateRunScopedRoutesInPlan repairs only the proven concurrent-run hazard:
// a prior step declares route_selection.json, but a later router reads the
// canonical shared mirror in db/assets. Other route_source_file values remain
// valid shared inputs and are not changed.
func migrateRunScopedRoutesInPlan(plan *PlanningResponse) runScopedRouteMigrationResult {
	result := runScopedRouteMigrationResult{}
	if plan == nil {
		return result
	}

	stepsByID := make(map[string]PlanStepInterface, len(plan.Steps))
	for _, step := range plan.Steps {
		if step != nil {
			stepsByID[step.GetID()] = step
		}
	}

	producerID := ""
	producerSet := map[string]bool{}
	consumerSet := map[string]bool{}
	for _, step := range plan.Steps {
		if step == nil {
			continue
		}

		if router, ok := step.(routeSwitchStep); ok && producerID != "" {
			source := filepath.ToSlash(strings.TrimSpace(router.GetRouteSourceFile()))
			if source == "db/assets/"+routeSelectionFileName {
				setRouteSourceFile(router, "")
				addRunScopedRouteDependency(step)
				result.Routers = append(result.Routers, step.GetID())
				producerSet[producerID] = true
				rewriteSharedRouteInstruction(stepsByID[producerID])

				for _, route := range router.GetRoutes() {
					consumer := stepsByID[strings.TrimSpace(route.NextStepID)]
					if consumer == nil {
						continue
					}
					addRunScopedRouteDependency(consumer)
					rewriteSharedRouteInstruction(consumer)
					consumerSet[consumer.GetID()] = true
				}
			}
		}

		if contextOutputMatchesDependency(step.GetContextOutput().String(), routeSelectionFileName) {
			producerID = step.GetID()
		}
	}

	for id := range producerSet {
		result.Producers = append(result.Producers, id)
	}
	for id := range consumerSet {
		result.Consumers = append(result.Consumers, id)
	}
	sort.Strings(result.Routers)
	sort.Strings(result.Producers)
	sort.Strings(result.Consumers)
	return result
}

func createMigrateRunScopedRoutesExecutor(
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, _ map[string]interface{}) (string, error) {
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("read planning/plan.json: %w", err)
		}
		before, err := json.Marshal(plan)
		if err != nil {
			return "", fmt.Errorf("snapshot plan: %w", err)
		}

		result := migrateRunScopedRoutesInPlan(plan)
		if result.noOp() {
			return `{"status":"no_op","message":"No router points at db/assets/route_selection.json while a prior step produces a run-scoped route_selection.json."}`, nil
		}
		if err := ValidatePlanStructure(plan); err != nil {
			return "", fmt.Errorf("validate migrated plan: %w", err)
		}
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("write migrated planning/plan.json: %w", err)
		}
		after, _ := json.Marshal(plan)
		stepIDs := append(append(append([]string{}, result.Producers...), result.Routers...), result.Consumers...)
		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool: "migrate_run_scoped_routes",
			Reason: fmt.Sprintf(
				"Workflow contract v%s: bind dynamic route decisions and route destinations to the producer's run-scoped route_selection.json so concurrent runs cannot overwrite one another; intentional shared route sources are unchanged.",
				workflowContractRunScopedRoutesVersionLabel,
			),
			StepIDs:        stepIDs,
			BeforeSnapshot: json.RawMessage(before),
			AfterSnapshot:  json.RawMessage(after),
		}, readFile, writeFile, logger)

		payload, _ := json.Marshal(result)
		return fmt.Sprintf(`{"status":"migrated","result":%s}`, payload), nil
	}
}
