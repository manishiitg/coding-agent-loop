package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

const routeSelectionFileName = "route_selection.json"

type deterministicRoutingSelection struct {
	SelectedRouteID string
	Reasoning       string
	SourcePath      string
	SourceKind      string
	RawValue        string
}

func (s *deterministicRoutingSelection) routingResponse() *RoutingResponse {
	if s == nil {
		return nil
	}
	return &RoutingResponse{
		SelectedRouteID: s.SelectedRouteID,
		Reasoning:       s.Reasoning,
	}
}

func parseRouteSelectionPayload(content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("route selection file is empty")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return "", fmt.Errorf("route selection file must be valid JSON: %w", err)
	}

	for _, key := range []string{"select_route", "route_id", "selected_route_id"} {
		raw, ok := payload[key]
		if !ok {
			continue
		}
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("%s must be a string", key)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return "", fmt.Errorf("%s must not be empty", key)
		}
		return value, nil
	}

	return "", fmt.Errorf("route selection file must contain select_route, route_id, or selected_route_id")
}

func resolveRouteSelectionValue(routes []RoutingRoute, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("route selection value is empty")
	}

	for _, route := range routes {
		if value == strings.TrimSpace(route.RouteID) {
			return route.RouteID, nil
		}
	}

	var nextStepMatches []RoutingRoute
	for _, route := range routes {
		if value == strings.TrimSpace(route.NextStepID) {
			nextStepMatches = append(nextStepMatches, route)
		}
	}
	if len(nextStepMatches) == 1 {
		return nextStepMatches[0].RouteID, nil
	}
	if len(nextStepMatches) > 1 {
		routeIDs := make([]string, 0, len(nextStepMatches))
		for _, route := range nextStepMatches {
			routeIDs = append(routeIDs, route.RouteID)
		}
		return "", fmt.Errorf("route selection value %q matches multiple next_step_id routes: %s", value, strings.Join(routeIDs, ", "))
	}

	available := make([]string, 0, len(routes))
	for _, route := range routes {
		available = append(available, fmt.Sprintf("%s -> %s", route.RouteID, route.NextStepID))
	}
	return "", fmt.Errorf("route selection value %q does not match any route_id or unique next_step_id (available: %s)", value, strings.Join(available, ", "))
}

func isWorkspaceNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "not found") ||
		strings.Contains(errText, "no such file") ||
		strings.Contains(errText, "does not exist")
}

func (hcpo *StepBasedWorkflowOrchestrator) routingStepExecutionPath(step *RoutingPlanStep, stepIndex int) string {
	stepPath := fmt.Sprintf("step-%d", stepIndex+1)
	return hcpo.routingStepExecutionPathForStepPath(step, stepIndex, stepPath)
}

func (hcpo *StepBasedWorkflowOrchestrator) routingStepExecutionPathForStepPath(step *RoutingPlanStep, stepIndex int, stepPath string) string {
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	return getExecutionFolderPath(executionWorkspacePath, step.GetID(), stepPath)
}

func (hcpo *StepBasedWorkflowOrchestrator) seedRouteSelectionsForRun(ctx context.Context, steps []PlanStepInterface) error {
	if hcpo.executionOptions == nil || len(hcpo.executionOptions.RouteSelections) == 0 {
		return nil
	}
	if hcpo.selectedRunFolder == "" {
		return fmt.Errorf("run folder not resolved - cannot seed route selections")
	}

	routingSteps := make(map[string]struct {
		step  *RoutingPlanStep
		index int
	})
	for i, step := range steps {
		routingStep, ok := step.(*RoutingPlanStep)
		if !ok {
			continue
		}
		stepID := strings.TrimSpace(routingStep.GetID())
		if stepID == "" {
			continue
		}
		routingSteps[stepID] = struct {
			step  *RoutingPlanStep
			index int
		}{step: routingStep, index: i}
	}

	for stepID, rawValue := range hcpo.executionOptions.RouteSelections {
		stepID = strings.TrimSpace(stepID)
		rawValue = strings.TrimSpace(rawValue)
		routingStepInfo, ok := routingSteps[stepID]
		if !ok {
			return fmt.Errorf("route_selections references unknown routing step %q", stepID)
		}
		selectedRouteID, err := resolveRouteSelectionValue(routingStepInfo.step.Routes, rawValue)
		if err != nil {
			return fmt.Errorf("invalid route_selections[%q]: %w", stepID, err)
		}

		stepExecutionPath := hcpo.routingStepExecutionPath(routingStepInfo.step, routingStepInfo.index)
		if err := hcpo.ensureStepExecutionFolderExists(ctx, stepExecutionPath); err != nil {
			return fmt.Errorf("failed to create routing step folder for %q: %w", stepID, err)
		}

		payload := map[string]string{"select_route": selectedRouteID}
		content, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal route selection for %q: %w", stepID, err)
		}
		routeFilePath := filepath.Join(stepExecutionPath, routeSelectionFileName)
		if err := hcpo.WriteWorkspaceFile(ctx, routeFilePath, string(content)); err != nil {
			return fmt.Errorf("failed to seed route selection for %q: %w", stepID, err)
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔀 Seeded deterministic route selection for %s: %s -> %s", stepID, rawValue, selectedRouteID))
	}

	return nil
}

func (hcpo *StepBasedWorkflowOrchestrator) resolveDeterministicRoutingSelection(
	ctx context.Context,
	routingStep *RoutingPlanStep,
	stepIndex int,
	routingStepPath string,
	allSteps []PlanStepInterface,
) (*deterministicRoutingSelection, error) {
	if routingStep == nil {
		return nil, fmt.Errorf("routing step is nil")
	}

	for _, ownRouteFilePath := range hcpo.routingStepOwnRouteFileCandidates(routingStep, stepIndex, routingStepPath) {
		if selection, found, err := hcpo.readDeterministicRoutingSource(ctx, routingStep, ownRouteFilePath, "routing step output"); err != nil {
			return nil, err
		} else if found {
			return selection, nil
		}
	}

	if source := strings.TrimSpace(routingStep.RouteSourceFile); source != "" {
		for _, candidate := range hcpo.resolveRouteSourceCandidates(ctx, source, stepIndex, routingStepPath, allSteps) {
			if selection, found, err := hcpo.readDeterministicRoutingSource(ctx, routingStep, candidate, "route_source_file"); err != nil {
				return nil, err
			} else if found {
				return selection, nil
			}
		}
		return nil, fmt.Errorf("routing step %q route_source_file %q was not found", routingStep.GetID(), source)
	}

	for _, dep := range routingStep.GetContextDependencies() {
		dep = strings.TrimSpace(dep)
		if !isRouteSelectionDependency(dep) {
			continue
		}
		for _, candidate := range hcpo.resolveRouteSourceCandidates(ctx, dep, stepIndex, routingStepPath, allSteps) {
			if selection, found, err := hcpo.readDeterministicRoutingSource(ctx, routingStep, candidate, "context_dependencies"); err != nil {
				return nil, err
			} else if found {
				return selection, nil
			}
		}
	}

	if defaultRouteID := strings.TrimSpace(routingStep.DefaultRouteID); defaultRouteID != "" {
		selectedRouteID, err := resolveRouteSelectionValue(routingStep.Routes, defaultRouteID)
		if err != nil {
			return nil, fmt.Errorf("invalid default_route_id for routing step %q: %w", routingStep.GetID(), err)
		}
		return &deterministicRoutingSelection{
			SelectedRouteID: selectedRouteID,
			Reasoning:       fmt.Sprintf("No %s found; using default_route_id %q.", routeSelectionFileName, selectedRouteID),
			SourceKind:      "default_route_id",
			RawValue:        defaultRouteID,
		}, nil
	}

	return nil, fmt.Errorf("routing step %q requires %s or default_route_id", routingStep.GetID(), routeSelectionFileName)
}

func (hcpo *StepBasedWorkflowOrchestrator) routingStepOwnRouteFileCandidates(routingStep *RoutingPlanStep, stepIndex int, routingStepPath string) []string {
	if routingStepPath == "" {
		routingStepPath = fmt.Sprintf("step-%d", stepIndex+1)
	}

	candidates := []string{
		filepath.Join(hcpo.routingStepExecutionPathForStepPath(routingStep, stepIndex, routingStepPath), routeSelectionFileName),
	}

	if strings.TrimSpace(routingStep.Description) != "" {
		executeStepPath := fmt.Sprintf("step-%d-routing", stepIndex+1)
		executeRouteFilePath := filepath.Join(hcpo.routingStepExecutionPathForStepPath(routingStep, stepIndex, executeStepPath), routeSelectionFileName)
		if executeRouteFilePath != candidates[0] {
			candidates = append(candidates, executeRouteFilePath)
		}
	}

	return candidates
}

func (hcpo *StepBasedWorkflowOrchestrator) resolveRouteSourceCandidates(
	ctx context.Context,
	source string,
	stepIndex int,
	routingStepPath string,
	allSteps []PlanStepInterface,
) []string {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	if filepath.IsAbs(source) || strings.Contains(source, "/") {
		return []string{source}
	}

	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	docsRoot := GetPromptDocsRoot()
	resolved := hcpo.resolveDependencyPathsWithWorkspace(ctx, []string{source}, stepIndex, routingStepPath, allSteps, executionWorkspacePath, docsRoot, hcpo.variableValues)
	if len(resolved) == 0 {
		return []string{source}
	}
	return resolved
}

func (hcpo *StepBasedWorkflowOrchestrator) readDeterministicRoutingSource(
	ctx context.Context,
	routingStep *RoutingPlanStep,
	sourcePath string,
	sourceKind string,
) (*deterministicRoutingSelection, bool, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return nil, false, nil
	}

	content, err := hcpo.ReadWorkspaceFile(ctx, sourcePath)
	if err != nil {
		if isWorkspaceNotFoundErr(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read route selection source %q: %w", sourcePath, err)
	}

	rawValue, err := parseRouteSelectionPayload(content)
	if err != nil {
		return nil, true, fmt.Errorf("invalid route selection source %q: %w", sourcePath, err)
	}
	selectedRouteID, err := resolveRouteSelectionValue(routingStep.Routes, rawValue)
	if err != nil {
		return nil, true, fmt.Errorf("invalid route selection source %q: %w", sourcePath, err)
	}

	return &deterministicRoutingSelection{
		SelectedRouteID: selectedRouteID,
		Reasoning:       fmt.Sprintf("Selected deterministically from %s (%s): %s.", sourceKind, sourcePath, rawValue),
		SourcePath:      sourcePath,
		SourceKind:      sourceKind,
		RawValue:        rawValue,
	}, true, nil
}

func isRouteSelectionDependency(dep string) bool {
	dep = strings.TrimSpace(dep)
	if dep == "" {
		return false
	}
	return filepath.Base(dep) == routeSelectionFileName
}
