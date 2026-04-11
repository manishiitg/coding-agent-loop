package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
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

// validateDecisionStep validates that a decision step has all required fields
// Returns error if any required field is missing
// DecisionPlanStep is now flattened - fields are directly on the step
func validateDecisionStepTyped(step PlanStepInterface, stepIndex int) error {
	if decisionStep, ok := step.(*DecisionPlanStep); ok {
		if decisionStep.ID == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required ID field", stepIndex, step.GetTitle())
		}
		if decisionStep.Description == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required description field", stepIndex, step.GetTitle())
		}
		if decisionStep.DecisionEvaluationQuestion == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required decision_evaluation_question field", stepIndex, step.GetTitle())
		}
		if decisionStep.IfTrueNextStepID == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required if_true_next_step_id field", stepIndex, step.GetTitle())
		}
		if decisionStep.IfFalseNextStepID == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required if_false_next_step_id field", stepIndex, step.GetTitle())
		}
		// No nested step to validate - DecisionPlanStep is flattened
	}
	return nil
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
		// If description is set, success_criteria must also be set (execute-then-route mode)
		if routingStep.Description != "" && routingStep.SuccessCriteria == "" {
			return fmt.Errorf("routing step at index %d (title: %q) has description but is missing required success_criteria (execute-then-route mode requires both)", stepIndex, step.GetTitle())
		}
	}
	return nil
}

// validatePlanStepIDs recursively validates that all steps have IDs
// Throws error if any step is missing an ID
func validatePlanStepIDs(steps []PlanStepInterface) error {
	for i, step := range steps {
		if step.GetID() == "" {
			return fmt.Errorf("step at index %d is missing required ID field. Step title: %q", i, step.GetTitle())
		}

		// Validate decision step fields
		if err := validateDecisionStepTyped(step, i); err != nil {
			return err
		}

		// Validate routing step fields
		if err := validateRoutingStepTyped(step, i); err != nil {
			return err
		}

		// Recursively validate branch steps (for conditional steps)
		if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
			if len(conditionalStep.IfTrueSteps) > 0 {
				if err := validateBranchStepIDs(conditionalStep.IfTrueSteps, step.GetTitle(), "true"); err != nil {
					return err
				}
			}
			if len(conditionalStep.IfFalseSteps) > 0 {
				if err := validateBranchStepIDs(conditionalStep.IfFalseSteps, step.GetTitle(), "false"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// validateBranchStepIDs recursively validates that all branch steps have IDs
func validateBranchStepIDs(steps []PlanStepInterface, parentTitle, branchType string) error {
	for i, step := range steps {
		if step.GetID() == "" {
			return fmt.Errorf("branch step at index %d in %s branch of parent %q is missing required ID field. Step title: %q", i, branchType, parentTitle, step.GetTitle())
		}

		// Recursively validate nested branch steps
		if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
			if len(conditionalStep.IfTrueSteps) > 0 {
				if err := validateBranchStepIDs(conditionalStep.IfTrueSteps, step.GetTitle(), "true"); err != nil {
					return err
				}
			}
			if len(conditionalStep.IfFalseSteps) > 0 {
				if err := validateBranchStepIDs(conditionalStep.IfFalseSteps, step.GetTitle(), "false"); err != nil {
					return err
				}
			}
		}
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

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Found existing plan at %s with %d steps", planPath, len(planResponse.Steps)))
	return true, &planResponse, nil
}

func validateLoadedPlanStep(typedStep PlanStepInterface, stepIndex int) error {
	switch step := typedStep.(type) {
	case *RegularPlanStep, *HumanInputPlanStep, *EvaluationStep:
		return nil

	case *DecisionPlanStep:
		if err := validateDecisionStepTyped(step, stepIndex); err != nil {
			return err
		}
		return nil

	case *RoutingPlanStep:
		if err := validateRoutingStepTyped(step, stepIndex); err != nil {
			return err
		}
		return nil

	case *ConditionalPlanStep:
		if len(step.IfTrueSteps) > 0 {
			for i, branchStep := range step.IfTrueSteps {
				if err := validateLoadedPlanStep(branchStep, i); err != nil {
					return fmt.Errorf("conditional if_true_steps[%d]: %w", i, err)
				}
			}
		}
		if len(step.IfFalseSteps) > 0 {
			for i, branchStep := range step.IfFalseSteps {
				if err := validateLoadedPlanStep(branchStep, i); err != nil {
					return fmt.Errorf("conditional if_false_steps[%d]: %w", i, err)
				}
			}
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

	case *ConditionalPlanStep:
		// Conditional step: populate branch steps recursively
		if len(step.IfTrueSteps) > 0 {
			for _, branchStep := range step.IfTrueSteps {
				if err := populateRuntimeFields(branchStep, stepConfigs); err != nil {
					return fmt.Errorf("failed to populate if_true branch step: %w", err)
				}
			}
		}

		if len(step.IfFalseSteps) > 0 {
			for _, branchStep := range step.IfFalseSteps {
				if err := populateRuntimeFields(branchStep, stepConfigs); err != nil {
					return fmt.Errorf("failed to populate if_false branch step: %w", err)
				}
			}
		}

		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		if validationSchemaOverride != nil {
			step.ValidationSchema = validationSchemaOverride
		}
		return nil

	case *DecisionPlanStep:
		// Decision step is now flattened - no nested step to populate
		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		if validationSchemaOverride != nil {
			step.ValidationSchema = validationSchemaOverride
		}
		return nil

	case *HumanInputPlanStep:
		// Human input step: no execution, validation, or learning - just asks question and blocks

		// Human input steps should never have learning or execution agents
		if agentConfigs == nil {
			val := true
			agentConfigs = &AgentConfigs{
				DisableLearning: &val,
			}
		} else {
			// Ensure learning is disabled
			val := true
			if agentConfigs.DisableLearning == nil || !*agentConfigs.DisableLearning {
				disabledConfigs := *agentConfigs
				disabledConfigs.DisableLearning = &val
				agentConfigs = &disabledConfigs
			}
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
	switch step := typedStep.(type) {
	case *ConditionalPlanStep:
		// Populate branch steps recursively
		if len(step.IfTrueSteps) > 0 {
			for _, branchStep := range step.IfTrueSteps {
				if err := populateRuntimeFields(branchStep, stepConfigs); err != nil {
					return nil, fmt.Errorf("failed to populate if_true branch step: %w", err)
				}
			}
		}
		if len(step.IfFalseSteps) > 0 {
			for _, branchStep := range step.IfFalseSteps {
				if err := populateRuntimeFields(branchStep, stepConfigs); err != nil {
					return nil, fmt.Errorf("failed to populate if_false branch step: %w", err)
				}
			}
		}

	case *DecisionPlanStep:
		// Decision step is now flattened - no nested step to populate

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
	return name == "update_regular_step" || name == "update_conditional_step" || name == "update_decision_step" || name == "update_routing_step" || name == "update_human_input_step" || name == "update_todo_task_step" || name == "delete_plan_steps" || name == "add_regular_step" || name == "add_conditional_step" || name == "add_decision_step" || name == "add_routing_step" || name == "add_loop_step" || name == "add_human_input_step" || name == "add_todo_task_step" ||
		name == "convert_step_to_conditional" || name == "add_branch_steps" || name == "update_branch_steps" ||
		name == "delete_branch_steps" || name == "convert_conditional_to_regular" || name == "update_validation_schema" ||
		name == "add_todo_task_route" || name == "update_todo_task_route" || name == "delete_todo_task_route" ||
		name == "migrate_todo_route_ids"
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
				case "update_regular_step", "update_conditional_step", "update_decision_step", "update_routing_step":
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

				case "add_regular_step", "add_conditional_step", "add_decision_step", "add_routing_step", "add_loop_step":
					// Extract id from new step
					if stepID, ok := argsMap["id"].(string); ok && stepID != "" {
						changed.Added = append(changed.Added, stepID)
					}

				case "add_branch_steps":
					// Extract step IDs from branch steps
					if branchType, ok := argsMap["branch_type"].(string); ok {
						var stepsKey string
						if branchType == "true" {
							stepsKey = "if_true_steps"
						} else if branchType == "false" {
							stepsKey = "if_false_steps"
						}
						if stepsKey != "" {
							if stepsRaw, ok := argsMap[stepsKey].([]interface{}); ok {
								for _, stepRaw := range stepsRaw {
									if stepMap, ok := stepRaw.(map[string]interface{}); ok {
										if stepID, ok := stepMap["id"].(string); ok && stepID != "" {
											changed.Added = append(changed.Added, stepID)
										}
									}
								}
							}
						}
					}

				case "update_branch_steps":
					// Extract step IDs from updated branch steps
					if branchType, ok := argsMap["branch_type"].(string); ok {
						var stepsKey string
						if branchType == "true" {
							stepsKey = "if_true_steps"
						} else if branchType == "false" {
							stepsKey = "if_false_steps"
						}
						if stepsKey != "" {
							if stepsRaw, ok := argsMap[stepsKey].([]interface{}); ok {
								for _, stepRaw := range stepsRaw {
									if stepMap, ok := stepRaw.(map[string]interface{}); ok {
										if stepID, ok := stepMap["id"].(string); ok && stepID != "" {
											changed.Updated = append(changed.Updated, stepID)
										}
									}
								}
							}
						}
					}

				case "delete_branch_steps":
					// Extract step IDs from deleted branch steps
					if deletedIDsRaw, ok := argsMap["deleted_step_ids"].([]interface{}); ok {
						for _, idRaw := range deletedIDsRaw {
							if stepID, ok := idRaw.(string); ok && stepID != "" {
								changed.Deleted = append(changed.Deleted, stepID)
							}
						}
					}

				case "convert_step_to_conditional":
					// Extract existing_step_id
					if stepID, ok := argsMap["existing_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}

				case "convert_conditional_to_regular":
					// Extract existing_step_id
					if stepID, ok := argsMap["existing_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
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
