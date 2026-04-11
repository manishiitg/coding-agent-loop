package step_based_workflow

import (
	"encoding/json"
	"fmt"
)

// resolvePlanOrphanStepRefs materializes orphan_step_ref references into concrete
// route sub-agent steps for runtime use while keeping plan.json reference-based.
func resolvePlanOrphanStepRefs(plan *PlanningResponse) error {
	if plan == nil {
		return nil
	}

	orphanByID := make(map[string]PlanStepInterface, len(plan.OrphanSteps))
	for _, step := range plan.OrphanSteps {
		if step == nil {
			continue
		}
		if _, exists := orphanByID[step.GetID()]; exists {
			return fmt.Errorf("duplicate orphan step id %q", step.GetID())
		}
		orphanByID[step.GetID()] = step
	}

	if len(orphanByID) == 0 {
		return nil
	}

	validOrchestratorIDs := make(map[string]bool)
	for _, step := range plan.Steps {
		collectTodoTaskStepIDs(step, validOrchestratorIDs)
	}
	for _, step := range plan.OrphanSteps {
		collectTodoTaskStepIDs(step, validOrchestratorIDs)
	}

	for _, step := range plan.OrphanSteps {
		sharing := step.GetCommonFields().SharedWith
		if sharing == nil {
			continue
		}
		for _, orchestratorID := range sharing.OrchestratorIDs {
			if !validOrchestratorIDs[orchestratorID] {
				return fmt.Errorf("orphan step %q declares shared_with.orchestrator_ids entry %q, but no todo_task step with that ID exists in the plan", step.GetID(), orchestratorID)
			}
		}
	}

	for _, step := range plan.Steps {
		if err := resolveOrphanRefsInStep(step, orphanByID, nil); err != nil {
			return err
		}
	}
	for _, step := range plan.OrphanSteps {
		if err := resolveOrphanRefsInStep(step, orphanByID, []string{step.GetID()}); err != nil {
			return err
		}
	}

	return nil
}

func collectTodoTaskStepIDs(step PlanStepInterface, ids map[string]bool) {
	if step == nil {
		return
	}

	switch s := step.(type) {
	case *ConditionalPlanStep:
		for _, branchStep := range s.IfTrueSteps {
			collectTodoTaskStepIDs(branchStep, ids)
		}
		for _, branchStep := range s.IfFalseSteps {
			collectTodoTaskStepIDs(branchStep, ids)
		}
	case *TodoTaskPlanStep:
		if s.GetID() != "" {
			ids[s.GetID()] = true
		}
		for _, route := range s.PredefinedRoutes {
			if route.SubAgentStep != nil {
				collectTodoTaskStepIDs(route.SubAgentStep, ids)
			}
		}
	}
}

func resolveOrphanRefsInStep(step PlanStepInterface, orphanByID map[string]PlanStepInterface, orphanChain []string) error {
	if step == nil {
		return nil
	}

	switch s := step.(type) {
	case *ConditionalPlanStep:
		for _, branchStep := range s.IfTrueSteps {
			if err := resolveOrphanRefsInStep(branchStep, orphanByID, orphanChain); err != nil {
				return err
			}
		}
		for _, branchStep := range s.IfFalseSteps {
			if err := resolveOrphanRefsInStep(branchStep, orphanByID, orphanChain); err != nil {
				return err
			}
		}
	case *TodoTaskPlanStep:
		for i := range s.PredefinedRoutes {
			route := &s.PredefinedRoutes[i]
			if route.OrphanStepRef != "" {
				if route.SubAgentStep != nil {
					return fmt.Errorf("todo_task step %q route %q cannot define both orphan_step_ref and sub_agent_step", s.GetID(), route.RouteID)
				}
				if containsString(orphanChain, route.OrphanStepRef) {
					return fmt.Errorf("orphan step reference cycle detected while resolving %q via todo_task step %q route %q", route.OrphanStepRef, s.GetID(), route.RouteID)
				}

				sourceStep, ok := orphanByID[route.OrphanStepRef]
				if !ok {
					return fmt.Errorf("todo_task step %q route %q references orphan_step_ref %q, but no orphan step with that ID exists", s.GetID(), route.RouteID, route.OrphanStepRef)
				}
				if !orphanStepSharedWithOrchestrator(sourceStep, s.GetID()) {
					return fmt.Errorf("todo_task step %q route %q references orphan step %q, but that orphan step is not shared with this orchestrator", s.GetID(), route.RouteID, route.OrphanStepRef)
				}

				clonedStep, err := clonePlanStep(sourceStep)
				if err != nil {
					return fmt.Errorf("failed to clone orphan step %q for todo_task step %q route %q: %w", route.OrphanStepRef, s.GetID(), route.RouteID, err)
				}
				if err := setStepIdentity(clonedStep, route.RouteID, route.RouteName); err != nil {
					return err
				}
				route.SubAgentStep = clonedStep
			}

			if route.SubAgentStep != nil {
				nextChain := orphanChain
				if route.OrphanStepRef != "" {
					nextChain = append(append([]string{}, orphanChain...), route.OrphanStepRef)
				}
				if err := resolveOrphanRefsInStep(route.SubAgentStep, orphanByID, nextChain); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func orphanStepSharedWithOrchestrator(step PlanStepInterface, orchestratorID string) bool {
	if step == nil || orchestratorID == "" {
		return false
	}

	sharing := step.GetCommonFields().SharedWith
	if sharing == nil {
		return false
	}
	for _, allowedID := range sharing.OrchestratorIDs {
		if allowedID == orchestratorID {
			return true
		}
	}
	return false
}

func clonePlanStep(step PlanStepInterface) (PlanStepInterface, error) {
	if step == nil {
		return nil, nil
	}

	stepJSON, err := json.Marshal(step)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal step clone: %w", err)
	}
	clonedStep, err := unmarshalStepFromJSON(stepJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal step clone: %w", err)
	}
	return clonedStep, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
