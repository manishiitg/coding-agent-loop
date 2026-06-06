package step_based_workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	baseevents "github.com/manishiitg/mcpagent/events"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
)

type routingPickNotification struct {
	notifier        WorkshopExecutionNotifier
	execID          string
	name            string
	stepID          string
	stepTitle       string
	routingQuestion string
	groupName       string
	completed       bool
}

// executeRoutingStep executes a routing step by:
// 1. (Optional) Executing the step itself if Description is set (execute-then-route mode)
// 2. Reading route_selection.json or using default_route_id to select a route
// 3. Returning the selected route ID for routing (handled by main execution loop)
//
// Returns: (selectedRouteID string, executionResult string, error)
func (hcpo *StepBasedWorkflowOrchestrator) executeRoutingStep(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	progress *StepProgress,
	previousContextFiles []string,
	iteration int,
	execCtx *ExecutionContext,
	allSteps []PlanStepInterface,
	previousExecutionResults []string,
) (string, string, error) {
	routingStep, ok := step.(*RoutingPlanStep)
	if !ok {
		return "", "", fmt.Errorf("step is not a RoutingPlanStep")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔀 Executing routing step %d: %s", stepIndex+1, step.GetTitle()))

	// Validate required fields
	if routingStep.RoutingQuestion == "" {
		return "", "", fmt.Errorf("routing step %d (%s) is missing required routing_question field", stepIndex+1, step.GetTitle())
	}
	if len(routingStep.Routes) < 2 {
		return "", "", fmt.Errorf("routing step %d (%s) must have at least 2 routes, got %d", stepIndex+1, step.GetTitle(), len(routingStep.Routes))
	}

	// Emit step_started event
	routingStepPath := fmt.Sprintf("step-%d", stepIndex+1)
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, routingStepPath, false)

	// Calculate run number
	runNumber := 1
	if progress.RoutingEvaluationCounts != nil {
		totalEvals := 0
		for _, route := range routingStep.Routes {
			key := fmt.Sprintf("%s:%s", step.GetID(), route.RouteID)
			totalEvals += progress.RoutingEvaluationCounts[key]
		}
		runNumber = totalEvals + 1
	}

	var executionResult string

	// Mode check: execute-then-route vs pure routing
	if routingStep.Description != "" {
		// Execute-then-route mode
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing routing step: %s (run %d) - execute-then-route mode", step.GetTitle(), runNumber))
		executionStepPath := fmt.Sprintf("step-%d-routing", stepIndex+1)

		_ = ApplyStepConfigFromFile(ctx, step, hcpo)

		var err error
		executionResult, _, err = hcpo.executeSingleStep(
			ctx,
			step,
			stepIndex,
			executionStepPath,
			1,
			iteration,
			previousContextFiles,
			progress,
			false,
			execCtx,
			allSteps,
			false,
			[]string{},
			nil,
		)
		if err != nil {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute routing step '%s': %v", step.GetTitle(), err), nil)
			return "", "", fmt.Errorf("failed to execute routing step '%s': %w", step.GetTitle(), err)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ Routing step execution completed. Output length: %d chars", len(executionResult)))

		// Check for workflow termination signal
		executionResultUpper := strings.ToUpper(executionResult)
		if strings.Contains(executionResultUpper, "WORKFLOW_END") || strings.Contains(executionResultUpper, "END_WORKFLOW") {
			hcpo.GetLogger().Info(fmt.Sprintf("🏁 Routing step '%s' signaled workflow termination", step.GetTitle()))
			return "", executionResult, fmt.Errorf("WORKFLOW_END: routing step '%s' signaled workflow termination", step.GetTitle())
		}

		// Save execution result to logs
		var validationWorkspacePath string
		if hcpo.selectedRunFolder != "" {
			validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
		} else {
			validationWorkspacePath = hcpo.GetWorkspacePath()
		}
		executionLogsFolderPath := getExecutionFolderPathForLogs(validationWorkspacePath, step.GetID(), executionStepPath)
		executionResultFilePath := fmt.Sprintf("%s/routing-execution.json", executionLogsFolderPath)
		executionResponse := map[string]interface{}{
			"step_index":       stepIndex + 1,
			"step_path":        executionStepPath,
			"routing_step_id":  step.GetID(),
			"execution_result": executionResult,
			"timestamp":        time.Now().Format(time.RFC3339),
		}
		executionJSON, err := json.MarshalIndent(executionResponse, "", "  ")
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal routing execution response: %v", err))
		} else {
			if err := hcpo.WriteWorkspaceFile(ctx, executionResultFilePath, string(executionJSON)); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write routing execution response: %v", err))
			}
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔀 Resolving routing step: %s (run %d) - deterministic file mode", step.GetTitle(), runNumber))
	}

	// Ensure step execution folder exists
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, step.GetID(), routingStepPath)
	if err := hcpo.ensureStepExecutionFolderExists(ctx, stepExecutionPath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure routing step execution folder exists: %v", err))
	}

	selection, err := hcpo.resolveDeterministicRoutingSelection(ctx, routingStep, stepIndex, routingStepPath, allSteps)
	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to resolve routing step %d deterministically: %v", stepIndex+1, err), nil)
		hcpo.EmitOrchestratorAgentError(ctx, "workflow", "routing-step-deterministic", fmt.Sprintf("Resolve routing step: %s", step.GetTitle()), err.Error(), stepIndex, iteration)
		return "", "", fmt.Errorf("failed to resolve routing step deterministically: %w", err)
	}
	routingResponse := selection.routingResponse()
	selectedRouteID := routingResponse.SelectedRouteID

	// Store result on step struct
	routingStep.SelectedRouteID = selectedRouteID
	routingStep.RoutingResponse = routingResponse

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Routing step evaluated deterministically: selected route=%s source=%s", selectedRouteID, selection.SourceKind))

	// Emit routing_evaluated event
	hcpo.emitRoutingEvaluatedEvent(ctx, step, stepIndex, routingStepPath, routingResponse, routingStep.Routes, allSteps)

	// Save evaluation result to logs
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}
	validationFolderPath := getValidationFolderPath(validationWorkspacePath, step.GetID(), routingStepPath)
	routingEvaluationFilePath := fmt.Sprintf("%s/routing-evaluation.json", validationFolderPath)

	routeNextStepIDs := make(map[string]string)
	for _, route := range routingStep.Routes {
		routeNextStepIDs[route.RouteID] = route.NextStepID
	}

	routingEvalResponse := map[string]interface{}{
		"step_index":        stepIndex + 1,
		"step_path":         routingStepPath,
		"routing_step_id":   step.GetID(),
		"routing_question":  routingStep.RoutingQuestion,
		"selected_route_id": selectedRouteID,
		"routing_reasoning": routingResponse.Reasoning,
		"route_selection": map[string]interface{}{
			"source_kind": selection.SourceKind,
			"source_path": selection.SourcePath,
			"raw_value":   selection.RawValue,
		},
		"route_next_steps": routeNextStepIDs,
		"timestamp":        time.Now().Format(time.RFC3339),
	}

	routingJSON, err := json.MarshalIndent(routingEvalResponse, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal routing evaluation response: %v", err))
	} else {
		if err := hcpo.WriteWorkspaceFile(ctx, routingEvaluationFilePath, string(routingJSON)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write routing evaluation response: %v", err))
		}
	}

	// Emit step_finished event
	hcpo.emitStepFinishedEvent(ctx, step, stepIndex, routingStepPath, false)

	return selectedRouteID, executionResult, nil
}

// emitRoutingEvaluatedEvent emits a routing_evaluated event
func (hcpo *StepBasedWorkflowOrchestrator) emitRoutingEvaluatedEvent(ctx context.Context, step PlanStepInterface, stepIndex int, stepPath string, routingResponse *RoutingResponse, routes []RoutingRoute, allSteps []PlanStepInterface) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	stepTitle := step.GetTitle()
	if stepTitle == "" {
		stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
	}
	stepId := step.GetID()
	if stepId == "" {
		stepId = fmt.Sprintf("step-%d", stepIndex+1)
	}

	nextStepTypes := routingNextStepTypesByID(allSteps)
	eventRoutes := make([]RoutingRouteEvent, len(routes))
	for i, route := range routes {
		eventRoutes[i] = RoutingRouteEvent{
			RouteID:      route.RouteID,
			RouteName:    route.RouteName,
			Condition:    route.Condition,
			NextStepID:   route.NextStepID,
			NextStepType: nextStepTypes[strings.TrimSpace(route.NextStepID)],
		}
	}

	evaluatedEvent := &RoutingEvaluatedEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
			Component: "orchestrator",
		},
		StepID:    stepId,
		StepIndex: stepIndex,
		StepTitle: stepTitle,
		StepPath:  stepPath,
		RoutingQuestion: func() string {
			if routingStep, ok := step.(*RoutingPlanStep); ok {
				return routingStep.RoutingQuestion
			}
			return ""
		}(),
		RoutingResponse: RoutingResponseEvent{
			SelectedRouteID: routingResponse.SelectedRouteID,
			Reasoning:       routingResponse.Reasoning,
		},
		Routes:        eventRoutes,
		RunFolder:     hcpo.selectedRunFolder,
		WorkspacePath: hcpo.GetWorkspacePath(),
	}

	agentEvent := &baseevents.AgentEvent{
		Type:      events.RoutingEvaluated,
		Timestamp: time.Now(),
		Data:      evaluatedEvent,
	}

	if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit routing evaluated event: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted routing_evaluated event for step %d: %s (route=%s)", stepIndex+1, stepTitle, routingResponse.SelectedRouteID))
	}
}

func routingNextStepTypesByID(steps []PlanStepInterface) map[string]string {
	nextStepTypes := make(map[string]string, len(steps))
	for _, step := range steps {
		if step == nil {
			continue
		}
		stepID := strings.TrimSpace(step.GetID())
		if stepID == "" {
			continue
		}
		nextStepTypes[stepID] = string(step.StepType())
	}
	return nextStepTypes
}

func (hcpo *StepBasedWorkflowOrchestrator) startRoutingPickNotification(ctx context.Context, pc *virtualtools.ParentChatContext, routingStep *RoutingPlanStep) *routingPickNotification {
	notifier := hcpo.routingDecisionNotifier
	if notifier == nil {
		notifier = hcpo.workshopExecutionNotifier
	}
	if notifier == nil || pc == nil || routingStep == nil {
		return nil
	}

	stepID := strings.TrimSpace(routingStep.GetID())
	if stepID == "" {
		stepID = "routing"
	}
	stepTitle := strings.TrimSpace(routingStep.GetTitle())
	if stepTitle == "" {
		stepTitle = stepID
	}

	parentExecutionID := strings.TrimSpace(pc.AgentID)
	if parentExecutionID == "" {
		parentExecutionID = currentWorkshopParentExecutionID(ctx)
	}

	execID := fmt.Sprintf("routing-pick-%s-%d", routingPickIDPart(stepID), time.Now().UnixNano())
	name := fmt.Sprintf("routing pick: %s", stepTitle)
	notifier.OnExecutionStart(WorkshopExecutionStart{
		ID:                execID,
		ParentExecutionID: parentExecutionID,
		Name:              name,
		Kind:              "workflow_routing_pick",
	})

	return &routingPickNotification{
		notifier:        notifier,
		execID:          execID,
		name:            name,
		stepID:          stepID,
		stepTitle:       stepTitle,
		routingQuestion: routingStep.RoutingQuestion,
		groupName:       strings.TrimSpace(pc.GroupName),
	}
}

func routingPickIDPart(s string) string {
	return workflowSafeIDPart(s, "routing")
}

func (n *routingPickNotification) complete(route RoutingRoute, answer string) {
	if n == nil || n.completed || n.notifier == nil {
		return
	}
	n.completed = true

	routeName := strings.TrimSpace(route.RouteName)
	if routeName == "" {
		routeName = route.RouteID
	}
	result := fmt.Sprintf("Routing pick completed for step %q. Selected route %q (%s). Next step: %s.", n.stepTitle, route.RouteID, routeName, route.NextStepID)
	if answer = strings.TrimSpace(answer); answer != "" {
		result += fmt.Sprintf("\nAnswer submitted: %s", answer)
	}

	meta := n.metadata(route.RouteID)
	n.notifier.OnExecutionComplete(n.execID, n.name, result, meta, nil)
}

func (n *routingPickNotification) fail(message string, err error) {
	if n == nil || n.completed || n.notifier == nil {
		return
	}
	n.completed = true
	if err == nil {
		err = errors.New(message)
	}
	result := strings.TrimSpace(message)
	if result == "" {
		result = err.Error()
	}
	meta := n.metadata("")
	n.notifier.OnExecutionComplete(n.execID, n.name, result, meta, err)
}

func (n *routingPickNotification) metadata(selectedRouteID string) map[string]string {
	meta := map[string]string{
		"execution_type":   "routing-pick",
		"step_id":          n.stepID,
		"routing_question": n.routingQuestion,
	}
	if selectedRouteID != "" {
		meta["selected_route_id"] = selectedRouteID
	}
	if n.groupName != "" {
		meta["group_name"] = n.groupName
	}
	return meta
}

// evaluateRoutingViaBuilderChat attempts to resolve a routing step by asking
// the parent builder chat session (set up via run_workflow) to pick a route.
// Returns (response, true) on success; (nil, false) to fall through to the
// LLM-based evaluator. Never fatal — any failure falls through.
func (hcpo *StepBasedWorkflowOrchestrator) evaluateRoutingViaBuilderChat(
	ctx context.Context,
	routingStep *RoutingPlanStep,
	executionResult string,
	conditionContext string,
) (*RoutingResponse, bool) {
	sessionID := hcpo.getSessionID()
	pc := virtualtools.GetParentChat(sessionID)
	if pc == nil || pc.SessionID == "" || !virtualtools.HasChatInjector() {
		return nil, false
	}

	requestID := fmt.Sprintf("routing_step_%s_%d", routingStep.GetID(), time.Now().UnixNano())
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	if err := feedbackStore.CreateRequestWithoutNotification(requestID, routingStep.RoutingQuestion); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create routing feedback request: %v (falling back to LLM)", err))
		return nil, false
	}
	notification := hcpo.startRoutingPickNotification(ctx, pc, routingStep)

	var msg strings.Builder
	msg.WriteString("[WORKFLOW_ROUTING] The workflow you launched has reached a routing step. ")
	msg.WriteString("Pick which route to take based on the context below. ")
	msg.WriteString("If the choice is clear from the conversation so far, answer directly by calling submit_human_answer with the route_id. ")
	msg.WriteString("Otherwise, ask the user.\n\n")
	if pc.WorkflowPath != "" {
		msg.WriteString(fmt.Sprintf("Workflow: %s\n", pc.WorkflowPath))
	}
	if pc.GroupName != "" {
		msg.WriteString(fmt.Sprintf("Group: %s\n", pc.GroupName))
	}
	msg.WriteString(fmt.Sprintf("Request ID: %s\n", requestID))
	msg.WriteString(fmt.Sprintf("Routing question: %s\n\n", routingStep.RoutingQuestion))
	if strings.TrimSpace(conditionContext) != "" {
		msg.WriteString("Context (from prior steps):\n")
		msg.WriteString(conditionContext)
		if !strings.HasSuffix(conditionContext, "\n") {
			msg.WriteString("\n")
		}
		msg.WriteString("\n")
	}
	if strings.TrimSpace(executionResult) != "" {
		msg.WriteString("Execution output of this routing step:\n")
		msg.WriteString(executionResult)
		msg.WriteString("\n\n")
	}
	msg.WriteString("Available routes:\n")
	for i, route := range routingStep.Routes {
		msg.WriteString(fmt.Sprintf("  %d. route_id=%q  name=%q\n     Condition: %s\n     Next step: %s\n",
			i+1, route.RouteID, route.RouteName, route.Condition, route.NextStepID))
	}
	msg.WriteString("\nSubmit the answer as the exact route_id (or the route name). ")
	if routingStep.DefaultRouteID != "" {
		msg.WriteString(fmt.Sprintf("If unsure, the default route is %q.\n", routingStep.DefaultRouteID))
	}

	if err := virtualtools.InjectChatMessage(ctx, pc.SessionID, pc.UserID, msg.String()); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to inject routing question into parent chat %s: %v (falling back to LLM)", pc.SessionID, err))
		if notification != nil {
			notification.fail("Routing pick could not be sent to the parent builder chat; falling back to LLM routing.", err)
		}
		return nil, false
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📨 Routed routing decision to parent chat %s (request=%s, step=%s)", pc.SessionID, requestID, routingStep.GetID()))

	response, err := feedbackStore.WaitForResponse(requestID, 10*time.Minute)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Timeout/error waiting for routing answer from builder: %v (falling back to LLM)", err))
		if notification != nil {
			notification.fail("Routing pick timed out waiting for the parent builder chat; falling back to LLM routing.", err)
		}
		return nil, false
	}

	// Match the builder's answer to a route (by route_id, route name, or
	// option index — accept a few forms so the builder isn't brittle).
	trimmed := strings.TrimSpace(response)
	for _, route := range routingStep.Routes {
		if strings.EqualFold(route.RouteID, trimmed) || strings.EqualFold(route.RouteName, trimmed) {
			if notification != nil {
				notification.complete(route, response)
			}
			return &RoutingResponse{
				SelectedRouteID: route.RouteID,
				Reasoning:       fmt.Sprintf("Selected by builder chat: %s", response),
			}, true
		}
	}
	// Accept "option0"/"option1" as zero-based compatibility forms.
	lowerTrimmed := strings.ToLower(trimmed)
	if strings.HasPrefix(lowerTrimmed, "option") {
		indexStr := strings.TrimPrefix(lowerTrimmed, "option")
		if idx := parseNonNegativeInt(indexStr); idx >= 0 && idx < len(routingStep.Routes) {
			route := routingStep.Routes[idx]
			if notification != nil {
				notification.complete(route, response)
			}
			return &RoutingResponse{
				SelectedRouteID: route.RouteID,
				Reasoning:       fmt.Sprintf("Selected by builder chat (option index %d): %s", idx, route.RouteID),
			}, true
		}
	}
	// Plain numeric answers refer to the 1-based list shown to the builder.
	if displayIdx := parseNonNegativeInt(trimmed); displayIdx >= 1 && displayIdx <= len(routingStep.Routes) {
		idx := displayIdx - 1
		route := routingStep.Routes[idx]
		if notification != nil {
			notification.complete(route, response)
		}
		return &RoutingResponse{
			SelectedRouteID: route.RouteID,
			Reasoning:       fmt.Sprintf("Selected by builder chat (display index %d): %s", displayIdx, route.RouteID),
		}, true
	}
	if idx := parseNonNegativeInt(trimmed); idx == 0 && len(routingStep.Routes) > 0 {
		route := routingStep.Routes[0]
		if notification != nil {
			notification.complete(route, response)
		}
		return &RoutingResponse{
			SelectedRouteID: route.RouteID,
			Reasoning:       fmt.Sprintf("Selected by builder chat (zero-based index %d): %s", idx, route.RouteID),
		}, true
	}
	hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Builder's routing answer %q did not match any route; falling back to LLM", trimmed))
	if notification != nil {
		notification.fail(fmt.Sprintf("Routing pick answer %q did not match any available route; falling back to LLM routing.", trimmed), nil)
	}
	return nil, false
}

func parseNonNegativeInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return -1
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}
