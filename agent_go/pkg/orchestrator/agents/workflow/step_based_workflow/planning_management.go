package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	baseevents "github.com/manishiitg/mcpagent/events"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// EnhancedPlanWithMetadata stores enhanced plan with caching metadata
type EnhancedPlanWithMetadata struct {
	Plan          *PlanningResponse  `json:"plan"`
	LastUpdated   time.Time          `json:"last_updated"`
	LearningFiles []LearningFileInfo `json:"learning_files"`
}

// LearningFileInfo stores information about a learning file for cache comparison
type LearningFileInfo struct {
	Filepath   string    `json:"filepath"`
	ModifiedAt time.Time `json:"modified_at"`
}

// validateRoutingStepTyped validates that a routing step has all required fields
func validateRoutingStepTyped(step PlanStepInterface, stepIndex int) error {
	if routingStep, ok := step.(*RoutingPlanStep); ok {
		if routingStep.ID == "" {
			return fmt.Errorf("routing step at index %d (title: %q) is missing required ID field", stepIndex, step.GetTitle())
		}
		if routingStep.RoutingQuestion == "" {
			return fmt.Errorf("routing step at index %d (title: %q) is missing required routing_question field", stepIndex, step.GetTitle())
		}
		if len(routingStep.Routes) < 2 {
			return fmt.Errorf("routing step at index %d (title: %q) must have at least 2 routes, got %d", stepIndex, step.GetTitle(), len(routingStep.Routes))
		}
		routeIDs := make(map[string]bool)
		for _, route := range routingStep.Routes {
			if route.RouteID == "" {
				return fmt.Errorf("routing step at index %d (title: %q) has a route with empty route_id", stepIndex, step.GetTitle())
			}
			if route.NextStepID == "" {
				return fmt.Errorf("routing step at index %d (title: %q) route %q is missing next_step_id", stepIndex, step.GetTitle(), route.RouteID)
			}
			if routeIDs[route.RouteID] {
				return fmt.Errorf("routing step at index %d (title: %q) has duplicate route_id %q", stepIndex, step.GetTitle(), route.RouteID)
			}
			routeIDs[route.RouteID] = true
		}
		if routingStep.DefaultRouteID != "" && !routeIDs[routingStep.DefaultRouteID] {
			return fmt.Errorf("routing step at index %d (title: %q) has default_route_id %q that doesn't match any route_id", stepIndex, step.GetTitle(), routingStep.DefaultRouteID)
		}
	}
	return nil
}

// validatePlanStepIDs recursively validates that all steps have IDs
// Throws error if any step is missing an ID
func validatePlanStepIDsAtPath(steps []PlanStepInterface, pathPrefix string) error {
	for i, step := range steps {
		thisLoc := fmt.Sprintf("%s[%d]", pathPrefix, i)
		if step.GetID() == "" {
			return fmt.Errorf("step at %s is missing required ID field. Step title: %q", thisLoc, step.GetTitle())
		}

		// Validate routing step fields
		if err := validateRoutingStepTyped(step, i); err != nil {
			return err
		}
		if todo, ok := step.(*TodoTaskPlanStep); ok {
			for routeIndex, route := range todo.PredefinedRoutes {
				if route.SubAgentStep == nil {
					continue
				}
				routePath := fmt.Sprintf("%s.predefined_routes[%d].sub_agent_step", thisLoc, routeIndex)
				if err := validatePlanStepIDsAtPath([]PlanStepInterface{route.SubAgentStep}, routePath); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validatePlanStepIDs(steps []PlanStepInterface) error {
	return validatePlanStepIDsAtPath(steps, "steps")
}

// collectStepIDsRecursive walks steps (and any nested branch steps) and records
// each ID against the location where it was first seen. Returns a duplicate error
// on the first collision it encounters. Empty IDs are skipped — presence is the
// job of validatePlanStepIDs.
func collectStepIDsRecursive(steps []PlanStepInterface, pathPrefix string, seen map[string]string) error {
	for i, step := range steps {
		id := step.GetID()
		thisLoc := fmt.Sprintf("%s[%d] (title: %q)", pathPrefix, i, step.GetTitle())
		if id != "" {
			if prev, dup := seen[id]; dup {
				return fmt.Errorf("duplicate step ID %q: first at %s, again at %s", id, prev, thisLoc)
			}
			seen[id] = thisLoc
		}
		if todo, ok := step.(*TodoTaskPlanStep); ok {
			for routeIndex, route := range todo.PredefinedRoutes {
				if route.SubAgentStep == nil {
					continue
				}
				routePath := fmt.Sprintf("%s.predefined_routes[%d].sub_agent_step", thisLoc, routeIndex)
				if err := collectStepIDsRecursive([]PlanStepInterface{route.SubAgentStep}, routePath, seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// validateStepIDUniqueness enforces that every step ID is unique across the plan —
// main steps, orphan steps, and nested branch steps share the same namespace
// because step IDs are used as learnings/{stepID}/ folder names and collisions
// silently clobber saved scripts and metadata.
func validateStepIDUniqueness(plan *PlanningResponse) error {
	if plan == nil {
		return nil
	}
	seen := make(map[string]string)
	if err := collectStepIDsRecursive(plan.Steps, "steps", seen); err != nil {
		return err
	}
	if err := collectStepIDsRecursive(plan.OrphanSteps, "orphan_steps", seen); err != nil {
		return err
	}
	return nil
}

// checkExistingPlan checks if a plan.json file already exists in the workspace and returns the parsed plan if found
// Uses the generic ReadWorkspaceFile function from base orchestrator
func (hcpo *StepBasedWorkflowOrchestrator) checkExistingPlan(ctx context.Context, planPath string) (bool, *PlanningResponse, error) {

	// Use the generic ReadWorkspaceFile function from base orchestrator
	planContent, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing plan: %w", err)
	}

	// Parse JSON content to PlanningResponse
	var planResponse PlanningResponse
	if err := json.Unmarshal([]byte(planContent), &planResponse); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse existing plan.json: %v", err))
		return false, nil, fmt.Errorf("failed to parse plan.json: %w", err)
	}
	if err := resolvePlanOrphanStepRefs(&planResponse); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to resolve orphan step references in existing plan.json: %v", err))
		return false, nil, fmt.Errorf("failed to resolve orphan step references in plan.json: %w", err)
	}
	// checkExistingPlan was previously a parse-only path: it returned
	// the plan without invoking the existing validator chain that the
	// fresh-plan write path (writePlanToFile) and the canonical load
	// path (loadPlanFromFile) both call. This let duplicate step IDs,
	// dangling routing.next_step_id, and ambiguous conditional
	// branch+next_step_id combinations through to LLM execution —
	// every per-step artifact then collides under the colliding ID
	// and writes silently clobber prior content. Re-enabling the
	// validator at this seam catches the violations at plan load.
	if err := validateLoadedPlanStructure(&planResponse); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Existing plan failed validation: %v", err))
		return false, nil, fmt.Errorf("existing plan.json failed validation: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Found existing plan at %s with %d steps", planPath, len(planResponse.Steps)))
	return true, &planResponse, nil
}

func validateLoadedPlanStep(typedStep PlanStepInterface, stepIndex int) error {
	switch step := typedStep.(type) {
	case *RegularPlanStep, *EvaluationStep:
		return nil

	case *HumanInputPlanStep:
		return validateHumanInputStepFieldsTyped(step)

	case *MessageSequencePlanStep:
		return validateMessageSequenceStepFieldsTyped(step)

	case *RoutingPlanStep:
		if err := validateRoutingStepTyped(step, stepIndex); err != nil {
			return err
		}
		return nil

	case *TodoTaskPlanStep:
		if err := validateTodoTaskStepFieldsTyped(step); err != nil {
			return err
		}
		for i, route := range step.PredefinedRoutes {
			if route.SubAgentStep != nil {
				if err := validateLoadedPlanStep(route.SubAgentStep, i); err != nil {
					return fmt.Errorf("predefined_route[%d] (route_id: %s): %w", i, route.RouteID, err)
				}
			}
		}
		return nil

	default:
		return fmt.Errorf("unsupported step type %T during loaded plan validation", typedStep)
	}
}

func validateLoadedPlanStructure(plan *PlanningResponse) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if err := validatePlanStepIDs(plan.Steps); err != nil {
		return err
	}
	if err := validateStepIDUniqueness(plan); err != nil {
		return err
	}
	for i, step := range plan.Steps {
		if err := validateLoadedPlanStep(step, i); err != nil {
			return fmt.Errorf("steps[%d] (id=%s): %w", i, step.GetID(), err)
		}
	}
	for i, step := range plan.OrphanSteps {
		if err := validateLoadedPlanStep(step, i); err != nil {
			return fmt.Errorf("orphan_steps[%d] (id=%s): %w", i, step.GetID(), err)
		}
	}
	if err := validateNextStepIDReferences(plan); err != nil {
		return err
	}
	return nil
}

// nextStepIDSentinelEnd is the literal value that any next_step_id may
// take to mean "end the workflow here" — it is NOT required to match a
// real step ID in the plan.
const nextStepIDSentinelEnd = "end"

// collectKnownStepIDs walks the plan tree (main steps, nested branch
// steps inside conditionals, orphan steps, sub-agent steps inside
// todo_task predefined routes) and returns the set of every step ID
// the plan declares. The set is the legal universe for any
// next_step_id reference; anything outside it (other than the "end"
// sentinel) is a dangling reference that would surface at runtime
// only after the LLM had already been billed for the routing
// decision.
func collectKnownStepIDs(plan *PlanningResponse) map[string]struct{} {
	out := make(map[string]struct{})
	if plan == nil {
		return out
	}
	var walk func(steps []PlanStepInterface)
	walk = func(steps []PlanStepInterface) {
		for _, step := range steps {
			if id := step.GetID(); id != "" {
				out[id] = struct{}{}
			}
			switch s := step.(type) {
			case *TodoTaskPlanStep:
				for _, route := range s.PredefinedRoutes {
					if route.SubAgentStep != nil {
						walk([]PlanStepInterface{route.SubAgentStep})
					}
				}
			}
		}
	}
	walk(plan.Steps)
	walk(plan.OrphanSteps)
	return out
}

// validateNextStepIDReferences enforces that every next_step_id
// emitted by routing routes and conditional branches references a
// step that actually exists in the plan (or is the "end" sentinel).
// Without this, a typo in plan.json silently goes to LLM execution;
// the routing/conditional LLM call gets billed; the workflow then
// dies trying to look up the missing successor with a runtime error
// that is hard to attribute back to the original mistake.
func validateNextStepIDReferences(plan *PlanningResponse) error {
	known := collectKnownStepIDs(plan)
	ref := func(stepID, fieldDesc, nextID string) error {
		nextID = strings.TrimSpace(nextID)
		if nextID == "" || nextID == nextStepIDSentinelEnd {
			return nil
		}
		if _, ok := known[nextID]; !ok {
			return fmt.Errorf("%s in step %q points to next_step_id=%q which is not a known step ID in the plan (use \"end\" to terminate the workflow, or fix the reference)", fieldDesc, stepID, nextID)
		}
		return nil
	}
	var walk func(steps []PlanStepInterface) error
	walk = func(steps []PlanStepInterface) error {
		for _, step := range steps {
			switch s := step.(type) {
			case *RoutingPlanStep:
				for _, route := range s.Routes {
					if err := ref(s.GetID(), fmt.Sprintf("route %q.next_step_id", route.RouteID), route.NextStepID); err != nil {
						return err
					}
				}
			case *TodoTaskPlanStep:
				if err := ref(s.GetID(), "next_step_id", s.NextStepID); err != nil {
					return err
				}
				for _, route := range s.PredefinedRoutes {
					if route.SubAgentStep != nil {
						if err := walk([]PlanStepInterface{route.SubAgentStep}); err != nil {
							return err
						}
					}
				}
			case *MessageSequencePlanStep:
				if err := ref(s.GetID(), "next_step_id", s.NextStepID); err != nil {
					return err
				}
			case *HumanInputPlanStep:
				if err := ref(s.GetID(), "next_step_id", s.NextStepID); err != nil {
					return err
				}
				if err := ref(s.GetID(), "if_yes_next_step_id", s.IfYesNextStepID); err != nil {
					return err
				}
				if err := ref(s.GetID(), "if_no_next_step_id", s.IfNoNextStepID); err != nil {
					return err
				}
				for key, target := range s.OptionRoutes {
					if err := ref(s.GetID(), fmt.Sprintf("option_routes[%q]", key), target); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if err := walk(plan.Steps); err != nil {
		return err
	}
	return walk(plan.OrphanSteps)
}

// validateHumanInputStepFieldsTyped enforces that a human_input step's
// conditional-routing configuration is internally consistent. The runtime
// (resolveHumanInputNextStep) never fails on a misconfigured step — an
// unmatched option falls back to the default next_step_id and then to the
// next sequential step — so a bad config doesn't error at execution, it
// routes somewhere the builder didn't intend. Catch the unambiguous
// misconfigurations at plan time instead.
func validateHumanInputStepFieldsTyped(step *HumanInputPlanStep) error {
	if strings.TrimSpace(step.Question) == "" {
		return fmt.Errorf("human_input step (title: %q, ID: %s) is missing required question field", step.Title, step.ID)
	}
	responseType := strings.TrimSpace(step.ResponseType)
	switch responseType {
	case "", "text", "yesno", "multiple_choice":
	default:
		return fmt.Errorf("human_input step %q has unsupported response_type %q (use text, yesno, or multiple_choice)", step.ID, responseType)
	}
	if (step.IfYesNextStepID != "" || step.IfNoNextStepID != "") && responseType != "yesno" {
		return fmt.Errorf("human_input step %q sets if_yes_next_step_id/if_no_next_step_id but response_type is %q — those fields only apply to response_type \"yesno\"", step.ID, responseType)
	}
	if len(step.OptionRoutes) > 0 && responseType != "multiple_choice" {
		return fmt.Errorf("human_input step %q sets option_routes but response_type is %q — option_routes only applies to response_type \"multiple_choice\"", step.ID, responseType)
	}
	if responseType == "multiple_choice" {
		if len(step.Options) == 0 {
			return fmt.Errorf("human_input step %q has response_type \"multiple_choice\" but no options", step.ID)
		}
		optionValues := make(map[string]bool, len(step.Options))
		for _, opt := range step.Options {
			optionValues[opt] = true
		}
		for key := range step.OptionRoutes {
			if idx, err := strconv.Atoi(key); err == nil {
				if idx < 0 || idx >= len(step.Options) {
					return fmt.Errorf("human_input step %q option_routes key %q is out of range — the step has %d option(s) (indices 0-%d)", step.ID, key, len(step.Options), len(step.Options)-1)
				}
				continue
			}
			if !optionValues[key] {
				return fmt.Errorf("human_input step %q option_routes key %q matches neither an option index nor an option value", step.ID, key)
			}
		}
		// A partial map is legal when the default next_step_id catches the
		// rest; without one, picking an unmapped option silently continues to
		// the next sequential step — almost always a missing mapping rather
		// than a deliberate choice.
		if len(step.OptionRoutes) > 0 && strings.TrimSpace(step.NextStepID) == "" {
			for i, opt := range step.Options {
				if _, byIdx := step.OptionRoutes[strconv.Itoa(i)]; byIdx {
					continue
				}
				if _, byVal := step.OptionRoutes[opt]; byVal {
					continue
				}
				return fmt.Errorf("human_input step %q option %d (%q) has no option_routes entry and the step has no default next_step_id — selecting it would silently continue to the next sequential step; map the option or set next_step_id as the fallback", step.ID, i, opt)
			}
		}
	}
	return nil
}

// populateRuntimeFields populates runtime fields (AgentConfigs, etc.) on plan steps in-place
// This maintains type safety by working directly with plan step types
func populateRuntimeFields(typedStep PlanStepInterface, stepConfigs []StepConfig) error {
	// Match config by step ID
	var agentConfigs *AgentConfigs
	var validationSchemaOverride *ValidationSchema
	stepID := typedStep.GetID()
	if stepID == "" {
		return fmt.Errorf("step is missing required ID field. Step title: %q", typedStep.GetTitle())
	} else if stepConfigs != nil {
		agentConfigs = MatchStepConfigByID(stepID, stepConfigs)
		// Check for validation schema override in step_config.json
		for i := range stepConfigs {
			if stepConfigs[i].ID == stepID && stepConfigs[i].ValidationSchema != nil {
				validationSchemaOverride = stepConfigs[i].ValidationSchema
				break
			}
		}
	}

	// Use type switch to handle different step types
	switch step := typedStep.(type) {
	case *RegularPlanStep:
		// Regular step (may have loops)
		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		if validationSchemaOverride != nil {
			step.ValidationSchema = validationSchemaOverride
		}
		return nil

	case *HumanInputPlanStep:
		// Human input step: no execution, validation, or learning - just asks question and blocks.
		// Force learnings_access="none" + empty objective so this step is never included
		// in the read or write path regardless of defaults. Under the learnings_access
		// split, the default is "read" (step sees _global/ SKILL.md) — which is
		// meaningless for a human-input step since it has no LLM prompt.
		if agentConfigs == nil {
			agentConfigs = &AgentConfigs{LearningsAccess: LearningsAccessNone}
		} else if strings.TrimSpace(agentConfigs.LearningObjective) != "" || agentConfigs.LearningsAccess != LearningsAccessNone {
			cleared := *agentConfigs
			cleared.LearningObjective = ""
			cleared.LearningsAccess = LearningsAccessNone
			agentConfigs = &cleared
		}

		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		if validationSchemaOverride != nil {
			step.ValidationSchema = validationSchemaOverride
		}
		return nil

	case *EvaluationStep:
		// Evaluation step
		step.AgentConfigs = agentConfigs
		if validationSchemaOverride != nil {
			step.PreValidation = validationSchemaOverride
		}
		return nil

	case *RoutingPlanStep:
		// Routing step: similar to decision step - evaluates a question and routes to one of N next steps
		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		if validationSchemaOverride != nil {
			step.ValidationSchema = validationSchemaOverride
		}
		return nil

	case *MessageSequencePlanStep:
		step.AgentConfigs = agentConfigs
		if validationSchemaOverride != nil {
			step.ValidationSchema = validationSchemaOverride
		}
		return nil

	case *TodoTaskPlanStep:
		// Populate sub-agent steps in predefined routes recursively
		for i := range step.PredefinedRoutes {
			route := &step.PredefinedRoutes[i]
			if route.SubAgentStep != nil {
				if err := populateRuntimeFields(route.SubAgentStep, stepConfigs); err != nil {
					return fmt.Errorf("failed to populate sub-agent step for route '%s': %w", route.RouteID, err)
				}
			}
		}

		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		if validationSchemaOverride != nil {
			step.ValidationSchema = validationSchemaOverride
		}
		return nil

	default:
		return fmt.Errorf("unknown step type: %T", typedStep)
	}
}

// populateStepRuntimeFields populates runtime fields on a PlanStepInterface and returns it
// This function populates AgentConfigs and other runtime fields from step_config.json
// For execution, use plan steps directly with populated runtime fields
func populateStepRuntimeFields(typedStep PlanStepInterface, stepConfigs []StepConfig) (PlanStepInterface, error) {
	// Populate runtime fields on the plan step
	if err := populateRuntimeFields(typedStep, stepConfigs); err != nil {
		return nil, err
	}

	// Recursively populate runtime fields for nested steps
	switch typedStep.(type) {
	case *EvaluationStep:
		// No nested steps for evaluation steps currently

	case *RoutingPlanStep:
		// Routing step is flattened - no nested steps to populate
	}

	// Return the step with populated runtime fields
	return typedStep, nil
}

// EmitTodoStepsExtractedEvent emits an event when todo steps are extracted from a plan
// Public method that accepts BaseOrchestrator and other parameters
func EmitTodoStepsExtractedEvent(ctx context.Context, bo *orchestrator.BaseOrchestrator, extractedSteps []PlanStepInterface, planSource, extractionMethod, runFolder, workspacePath string) {
	EmitTodoStepsExtractedEventWithMetadata(ctx, bo, extractedSteps, planSource, extractionMethod, runFolder, workspacePath, nil)
}

// EmitTodoStepsExtractedEventWithMetadata emits an event when todo steps are extracted from a plan with optional metadata
// Metadata can include changed_step_ids and deleted_step_ids for granular event handling
func EmitTodoStepsExtractedEventWithMetadata(ctx context.Context, bo *orchestrator.BaseOrchestrator, extractedSteps []PlanStepInterface, planSource, extractionMethod, runFolder, workspacePath string, metadata map[string]interface{}) {
	if bo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data with metadata
	baseEventData := baseevents.BaseEventData{
		Timestamp: time.Now(),
	}
	if metadata != nil {
		baseEventData.Metadata = metadata
	}

	eventData := &TodoStepsExtractedEvent{
		BaseEventData:       baseEventData,
		TotalStepsExtracted: len(extractedSteps),
		ExtractedSteps:      extractedSteps,
		ExtractionMethod:    extractionMethod,
		PlanSource:          planSource,
		WorkspacePath:       workspacePath,
		RunFolder:           runFolder,
	}

	// Create unified event wrapper
	unifiedEvent := &baseevents.AgentEvent{
		Type:      events.TodoStepsExtracted,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through the context-aware bridge
	bridge := bo.GetContextAwareBridge()
	if bridge == nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ [EmitTodoStepsExtractedEventWithMetadata] ContextAwareBridge is nil, cannot emit event"))
		return
	}
	bo.GetLogger().Info(fmt.Sprintf("📤 [EmitTodoStepsExtractedEventWithMetadata] About to emit event through bridge (bridge type: %T, metadata keys: %v)", bridge, getMetadataKeys(metadata)))
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ [EmitTodoStepsExtractedEventWithMetadata] Failed to emit todo steps extracted event: %v", err))
	} else {
		bo.GetLogger().Info(fmt.Sprintf("✅ [EmitTodoStepsExtractedEventWithMetadata] Successfully emitted todo steps extracted event: %d steps extracted", len(extractedSteps)))
	}
}

// getMetadataKeys returns a slice of metadata keys for logging
func getMetadataKeys(metadata map[string]interface{}) []string {
	if metadata == nil {
		return []string{}
	}
	keys := make([]string, 0, len(metadata))
	for k := range metadata {
		keys = append(keys, k)
	}
	return keys
}

// IsPlanModificationTool checks if a tool name is a plan modification tool
func IsPlanModificationTool(name string) bool {
	return name == "update_regular_step" || name == "update_routing_step" || name == "update_human_input_step" || name == "update_todo_task_step" || name == "update_message_sequence_step" || name == "delete_plan_steps" || name == "add_regular_step" || name == "add_routing_step" || name == "add_human_input_step" || name == "add_todo_task_step" || name == "add_message_sequence_step" ||
		name == "update_validation_schema" ||
		name == "add_todo_task_route" || name == "update_todo_task_route" || name == "delete_todo_task_route"
}

// IsStepConfigModificationTool checks if a tool name is a step_config modification tool
func IsStepConfigModificationTool(name string) bool {
	return name == "update_step_config_tools"
}

// ExtractToolCallsFromMessages scans messages for tool calls and returns the tool names that were called
// This is a public version of extractToolCallsFromMessages for use by other agents
func ExtractToolCallsFromMessages(messages []llmtypes.MessageContent) []string {
	toolNames := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != llmtypes.ChatMessageTypeAI {
			continue
		}
		for _, part := range msg.Parts {
			if toolCall, ok := part.(llmtypes.ToolCall); ok {
				if toolCall.FunctionCall != nil {
					toolNames[toolCall.FunctionCall.Name] = true
				}
			}
		}
	}
	result := make([]string, 0, len(toolNames))
	for name := range toolNames {
		result = append(result, name)
	}
	return result
}

// ChangedStepIDs contains step IDs that were added, updated, or deleted
type ChangedStepIDs struct {
	Added   []string
	Updated []string
	Deleted []string
}

// ExtractChangedStepIDsFromMessages extracts which specific step IDs were changed from plan modification tool calls
// Returns changed step IDs grouped by operation type (added, updated, deleted)
func ExtractChangedStepIDsFromMessages(messages []llmtypes.MessageContent) ChangedStepIDs {
	changed := ChangedStepIDs{
		Added:   []string{},
		Updated: []string{},
		Deleted: []string{},
	}

	for _, msg := range messages {
		if msg.Role != llmtypes.ChatMessageTypeAI {
			continue
		}
		for _, part := range msg.Parts {
			if toolCall, ok := part.(llmtypes.ToolCall); ok {
				if toolCall.FunctionCall == nil {
					continue
				}

				toolName := toolCall.FunctionCall.Name
				args := toolCall.FunctionCall.Arguments

				// Parse arguments JSON
				var argsMap map[string]interface{}
				if err := json.Unmarshal([]byte(args), &argsMap); err != nil {
					continue
				}

				switch toolName {
				case "update_regular_step", "update_routing_step":
					// Extract existing_step_id from updated step
					if stepID, ok := argsMap["existing_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}

				case "delete_plan_steps":
					// Extract deleted_step_ids array
					if deletedIDsRaw, ok := argsMap["deleted_step_ids"].([]interface{}); ok {
						for _, idRaw := range deletedIDsRaw {
							if stepID, ok := idRaw.(string); ok && stepID != "" {
								changed.Deleted = append(changed.Deleted, stepID)
							}
						}
					}

				case "add_regular_step", "add_routing_step":
					// Extract id from new step
					if stepID, ok := argsMap["id"].(string); ok && stepID != "" {
						changed.Added = append(changed.Added, stepID)
					}

				case "update_validation_schema":
					// Extract existing_step_id from updated step
					if stepID, ok := argsMap["existing_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}

				}
			}
		}
	}

	// Remove duplicates
	changed.Added = removeDuplicates(changed.Added)
	changed.Updated = removeDuplicates(changed.Updated)
	changed.Deleted = removeDuplicates(changed.Deleted)

	return changed
}

// removeDuplicates removes duplicate strings from a slice
func removeDuplicates(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, item := range slice {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

// CheckAndEmitPlanUpdateEvent checks if plan/step_config modification tools were called
// and emits todo_steps_extracted event if so. This helper can be used by any agent
// that modifies plan.json or step_config.json to ensure the frontend is notified.
//
// Parameters:
//   - ctx: context for the operation
//   - bo: BaseOrchestrator for event emission and logging
//   - conversationHistory: messages from the agent execution to check for tool calls
//   - workspacePath: workspace path for reading plan.json
//   - readFile: function to read files from workspace
func CheckAndEmitPlanUpdateEvent(
	ctx context.Context,
	bo *orchestrator.BaseOrchestrator,
	conversationHistory []llmtypes.MessageContent,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
) {
	if bo == nil {
		// Log at info level so we can see if this is the issue
		return
	}

	// Extract tool calls from conversation history
	toolCalls := ExtractToolCallsFromMessages(conversationHistory)
	bo.GetLogger().Info(fmt.Sprintf("🔍 [CheckAndEmitPlanUpdateEvent] Extracted %d tool calls: %v", len(toolCalls), toolCalls))

	// Check if any plan or step_config modification tool was called
	needsEvent := false
	for _, name := range toolCalls {
		if IsPlanModificationTool(name) || IsStepConfigModificationTool(name) {
			needsEvent = true
			break
		}
	}

	if !needsEvent {
		return
	}

	// Extract changed step IDs from tool call arguments (granular event data)
	changedStepIDs := ExtractChangedStepIDsFromMessages(conversationHistory)
	if len(changedStepIDs.Deleted) > 0 {
		bo.GetLogger().Info(fmt.Sprintf("   Deleted: %v", changedStepIDs.Deleted))
	}

	// Read current plan
	plan, err := readPlanFromFile(ctx, workspacePath, readFile)
	if err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read plan for event emission: %v", err))
		return
	}

	if plan == nil || len(plan.Steps) == 0 {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Plan is empty, skipping event emission"))
		return
	}

	// Use plan steps directly for the event (no conversion needed)
	// The frontend will merge step_config.json when it receives the event and refreshes
	planSteps := make([]PlanStepInterface, len(plan.Steps))
	copy(planSteps, plan.Steps)

	// Prepare metadata with changed step IDs for granular event handling
	// Combine added and updated into a single "changed_step_ids" array (frontend treats both as "changed")
	metadata := make(map[string]interface{})
	changedStepIDsCombined := make([]string, 0, len(changedStepIDs.Added)+len(changedStepIDs.Updated))
	changedStepIDsCombined = append(changedStepIDsCombined, changedStepIDs.Added...)
	changedStepIDsCombined = append(changedStepIDsCombined, changedStepIDs.Updated...)
	if len(changedStepIDsCombined) > 0 {
		metadata["changed_step_ids"] = changedStepIDsCombined
	}
	if len(changedStepIDs.Deleted) > 0 {
		metadata["deleted_step_ids"] = changedStepIDs.Deleted
	}

	// Emit the event with metadata
	EmitTodoStepsExtractedEventWithMetadata(ctx, bo, planSteps, "updated_plan", "agent_tool_modification", "", workspacePath, metadata)
	bo.GetLogger().Info(fmt.Sprintf("✅ Emitted plan update event: %d steps (changed: %d added, %d updated, %d deleted)",
		len(planSteps), len(changedStepIDs.Added), len(changedStepIDs.Updated), len(changedStepIDs.Deleted)))
}
