package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	baseevents "github.com/manishiitg/mcpagent/events"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for planning management - panics at startup if invalid
var planningUpdateValidationErrorTemplate = MustRegisterTemplate("planningUpdateValidationError",
	`Review the existing plan, fix the following validation issues, and then update the plan based on the objective and my feedback: {{.ValidationErr}}. Always use the human_feedback tool first to confirm any changes with me.`)

var planningCreateUserMessageTemplate = MustRegisterTemplate("planningCreateUserMessage",
	`Objective: {{.Objective}}

Generate a comprehensive structured plan to achieve this objective.`)

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
		if decisionStep.SuccessCriteria == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required success_criteria field", stepIndex, step.GetTitle())
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

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Found existing plan at %s with %d steps", planPath, len(planResponse.Steps)))
	return true, &planResponse, nil
}

// requestPlanApproval requests human approval for the generated plan
// Returns: (approved bool, feedback string, error)
func (hcpo *StepBasedWorkflowOrchestrator) requestPlanApproval(
	ctx context.Context,
	revisionAttempt int,
) (bool, string, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("⏸️ Requesting human approval for plan (attempt %d)", revisionAttempt))

	// Generate unique request ID
	requestID := fmt.Sprintf("plan_approval_%d_%d", time.Now().UnixNano(), revisionAttempt)

	// Use common human feedback function
	return hcpo.RequestHumanFeedback(
		ctx,
		requestID,
		"Please review the plan and provide approval or feedback",
		"", // No additional context for plan approval
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
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

	case *OrchestrationPlanStep:
		// Orchestration step: populate inner OrchestrationStep recursively
		if step.OrchestrationStep != nil {
			if err := populateRuntimeFields(step.OrchestrationStep, stepConfigs); err != nil {
				return fmt.Errorf("failed to populate orchestration inner step: %w", err)
			}
		}

		// Populate sub-agent steps in routes recursively
		for i := range step.OrchestrationRoutes {
			route := &step.OrchestrationRoutes[i]
			if route.SubAgentStep != nil {
				if err := populateRuntimeFields(route.SubAgentStep, stepConfigs); err != nil {
					return fmt.Errorf("failed to populate sub-agent step for route '%s': %w", route.RouteID, err)
				}
				}
		}

		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		// OrchestrationPlanStep delegates ValidationSchema to its inner OrchestrationStep,
		// which gets populated via recursive populateRuntimeFields above
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
		// Todo task step: populate inner TodoTaskStep recursively
		if step.TodoTaskStep != nil {
			if err := populateRuntimeFields(step.TodoTaskStep, stepConfigs); err != nil {
				return fmt.Errorf("failed to populate todo task inner step: %w", err)
			}
		}

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
		// TodoTaskPlanStep delegates ValidationSchema to its inner TodoTaskStep,
		// which gets populated via recursive populateRuntimeFields above
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

	case *OrchestrationPlanStep:
		// Populate inner OrchestrationStep
		if step.OrchestrationStep != nil {
			if err := populateRuntimeFields(step.OrchestrationStep, stepConfigs); err != nil {
				return nil, fmt.Errorf("failed to populate orchestration inner step: %w", err)
			}
		}
		// Populate sub-agent steps in routes
		for _, route := range step.OrchestrationRoutes {
			if route.SubAgentStep != nil {
				if err := populateRuntimeFields(route.SubAgentStep, stepConfigs); err != nil {
					return nil, fmt.Errorf("failed to populate sub-agent step: %w", err)
				}
			}
		}

	case *EvaluationStep:
		// No nested steps for evaluation steps currently

	case *RoutingPlanStep:
		// Routing step is flattened - no nested steps to populate
	}

	// Return the step with populated runtime fields
	return typedStep, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) populateStepsRuntimeFields(ctx context.Context, planSteps []PlanStepInterface) ([]PlanStepInterface, error) {
	// Read step configs from step_config.json
	stepConfigs, err := hcpo.ReadStepConfigs(ctx)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_config.json: %v (using defaults for all steps)", err))
		stepConfigs = []StepConfig{}
	}

	// Read global step overrides from step_override.json (applied after per-step configs)
	stepOverrides, err := hcpo.ReadStepOverrides(ctx)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_override.json: %v (skipping global overrides)", err))
		stepOverrides = nil
	}
	if stepOverrides != nil {
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Loaded global step overrides from step_override.json"))
	}

	// Log available config IDs for debugging
	if len(stepConfigs) > 0 {
		configIDs := make([]string, 0, len(stepConfigs))
		for _, config := range stepConfigs {
			if config.ID != "" {
				configIDs = append(configIDs, config.ID)
			}
		}
	} else {
	}

	// Match configs by step index (0-based)
	matchedConfigs, err := MatchStepConfigs(planSteps, stepConfigs)
	if err != nil {
		return nil, fmt.Errorf("failed to match step configs: %w", err)
	}

	todoSteps := make([]PlanStepInterface, len(planSteps))
	for i, step := range planSteps {
		// Step is already PlanStepInterface, no conversion needed

		// Get matched config for this step (may be nil if no match)
		var agentConfigs *AgentConfigs
		if config, found := matchedConfigs[i]; found {
			agentConfigs = config
			// Log code execution mode for debugging
			if agentConfigs.UseCodeExecutionMode != nil {
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("📋 Step '%s' (ID: %s) matched config - use_code_execution_mode: nil (will use preset default)", step.GetTitle(), step.GetID()))
			}
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("⚠️ Step '%s' (ID: %s) has NO config match in step_config.json - will use preset defaults", step.GetTitle(), step.GetID()))
		}

		// Populate runtime fields (this properly handles inner steps for decision/orchestration)
		todoStep, err := populateStepRuntimeFields(step, stepConfigs)
		if err != nil {
			return nil, fmt.Errorf("failed to populate runtime fields for step %d (title: %q, ID: %s): %w", i, step.GetTitle(), step.GetID(), err)
		}

		// Merge matched configs with existing configs (if any)
		// This preserves any configs set during conversion and merges in step_config.json configs
		if agentConfigs != nil {
			existingConfigs := getAgentConfigs(todoStep)
			if existingConfigs == nil {
				// Set AgentConfigs on the step if it doesn't exist
				switch s := todoStep.(type) {
				case *RegularPlanStep:
					s.AgentConfigs = agentConfigs
				case *ConditionalPlanStep:
					s.AgentConfigs = agentConfigs
				case *DecisionPlanStep:
					s.AgentConfigs = agentConfigs
				case *OrchestrationPlanStep:
					s.AgentConfigs = agentConfigs
				case *EvaluationStep:
					s.AgentConfigs = agentConfigs
				case *RoutingPlanStep:
					s.AgentConfigs = agentConfigs
				}
			} else {
				// Merge configs from step_config.json into existing configs
				MergeAgentConfigFields(existingConfigs, agentConfigs, step.GetID(), hcpo.GetLogger())
			}

			// Note: Inner orchestration steps should NOT inherit config from wrapper steps
			// Each step (wrapper and inner) loads its own config by its own ID only
			// The inner step will get its config when ApplyStepConfigFromFile is called for it during execution
		}

		// Apply global overrides from step_override.json (highest priority - wins over per-step config)
		if stepOverrides != nil {
			existingConfigs := getAgentConfigs(todoStep)
			if existingConfigs == nil {
				// Set AgentConfigs on the step with override values
				switch s := todoStep.(type) {
				case *RegularPlanStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				case *ConditionalPlanStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				case *DecisionPlanStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				case *OrchestrationPlanStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				case *EvaluationStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				case *RoutingPlanStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				}
			} else {
				MergeAgentConfigFields(existingConfigs, stepOverrides, step.GetID(), hcpo.GetLogger())
			}
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Applied global overrides for step '%s' (ID: %s)", step.GetTitle(), step.GetID()))
		}

		// Log config matching results for nested steps
		switch s := todoStep.(type) {
		case *DecisionPlanStep:
			// Decision step is now flattened - configs are directly on the step
			innerConfigs := getAgentConfigs(s)
			if innerConfigs != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Decision step '%s' (ID: %s) matched config from step_config.json", s.GetTitle(), s.GetID()))
			}
		case *OrchestrationPlanStep:
			if s.OrchestrationStep != nil {
				innerConfigs := getAgentConfigs(s.OrchestrationStep)
				if innerConfigs != nil {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Orchestration step '%s' (ID: %s) matched config from step_config.json", s.OrchestrationStep.GetTitle(), s.OrchestrationStep.GetID()))
				}
			}
		case *ConditionalPlanStep:
			for _, branchStep := range s.IfTrueSteps {
				branchConfigs := getAgentConfigs(branchStep)
				if branchConfigs != nil {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Branch step '%s' (ID: %s) matched config from step_config.json", branchStep.GetTitle(), branchStep.GetID()))
				}
			}
			for _, branchStep := range s.IfFalseSteps {
				branchConfigs := getAgentConfigs(branchStep)
				if branchConfigs != nil {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Branch step '%s' (ID: %s) matched config from step_config.json", branchStep.GetTitle(), branchStep.GetID()))
				}
			}
		}

		todoSteps[i] = todoStep
	}
	return todoSteps, nil
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
	return name == "update_regular_step" || name == "update_conditional_step" || name == "update_decision_step" || name == "update_routing_step" || name == "update_orchestration_step" || name == "update_human_input_step" || name == "update_todo_task_step" || name == "delete_plan_steps" || name == "add_regular_step" || name == "add_conditional_step" || name == "add_decision_step" || name == "add_routing_step" || name == "add_orchestration_step" || name == "add_loop_step" || name == "add_human_input_step" || name == "add_todo_task_step" ||
		name == "convert_step_to_conditional" || name == "add_branch_steps" || name == "update_branch_steps" ||
		name == "delete_branch_steps" || name == "convert_conditional_to_regular" || name == "update_validation_schema" || name == "update_success_criteria" ||
		name == "add_orchestration_route" || name == "update_orchestration_route" || name == "delete_orchestration_route" ||
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
				case "update_regular_step", "update_conditional_step", "update_decision_step", "update_routing_step", "update_orchestration_step":
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

				case "add_regular_step", "add_conditional_step", "add_decision_step", "add_routing_step", "add_orchestration_step", "add_loop_step":
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

				case "update_validation_schema", "update_success_criteria":
					// Extract existing_step_id from updated step
					if stepID, ok := argsMap["existing_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}

				case "add_orchestration_route":
					// Extract parent_step_id (the orchestration step that contains the route)
					if stepID, ok := argsMap["parent_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}
					// Extract sub_agent_step.id from new_route (sub-agents have their own step IDs)
					// When a new route is added, the sub-agent step is effectively "added" to the plan
					if newRouteRaw, ok := argsMap["new_route"].(map[string]interface{}); ok {
						if subAgentStepRaw, ok := newRouteRaw["sub_agent_step"].(map[string]interface{}); ok {
							if subAgentStepID, ok := subAgentStepRaw["id"].(string); ok && subAgentStepID != "" {
								changed.Added = append(changed.Added, subAgentStepID)
							}
						}
					}

				case "update_orchestration_route":
					// Extract parent_step_id (the orchestration step that contains the route)
					if stepID, ok := argsMap["parent_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}
					// Extract sub_agent_step.id from sub_agent_step parameter (if provided)
					// When a route's sub-agent is updated, the sub-agent step is effectively "updated"
					if subAgentStepRaw, ok := argsMap["sub_agent_step"].(map[string]interface{}); ok {
						if subAgentStepID, ok := subAgentStepRaw["id"].(string); ok && subAgentStepID != "" {
							changed.Updated = append(changed.Updated, subAgentStepID)
						}
					}

				case "delete_orchestration_route":
					// Extract parent_step_id (the orchestration step that contains the route)
					if stepID, ok := argsMap["parent_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}
					// Note: We can't extract the deleted sub-agent step ID from the tool call arguments alone
					// because delete_orchestration_route only provides parent_step_id and deleted_route_id
					// The sub-agent step ID would need to be read from the plan.json file, which is complex
					// For now, we track the parent step as updated, which will refresh the frontend
					// The frontend will see the deleted route (and its sub-agent node) when it refreshes the plan
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
	// Convert orchestration routes to use PlanStepInterface directly
	planSteps := make([]PlanStepInterface, len(plan.Steps))
	for i, step := range plan.Steps {
		// For orchestration steps, we need to convert routes to use PlanStepInterface
		if orchestrationStep, ok := step.(*OrchestrationPlanStep); ok {
			// Create a copy with updated routes
			orchestrationRoutes := make([]PlanOrchestrationRoute, len(orchestrationStep.OrchestrationRoutes))
			copy(orchestrationRoutes, orchestrationStep.OrchestrationRoutes)
			// Routes already use PlanStepInterface, so no conversion needed
			planSteps[i] = step
		} else {
			planSteps[i] = step
		}
	}

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
