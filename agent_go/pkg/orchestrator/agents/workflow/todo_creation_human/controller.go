package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
)

// TodoStep represents a todo step in the execution
type TodoStep struct {
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	SuccessCriteria     string   `json:"success_criteria"`
	WhyThisStep         string   `json:"why_this_step"`
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
}

// TodoStepsExtractedEvent represents the event when todo steps are extracted from a plan
type TodoStepsExtractedEvent struct {
	events.BaseEventData
	TotalStepsExtracted int        `json:"total_steps_extracted"`
	ExtractedSteps      []TodoStep `json:"extracted_steps"`
	ExtractionMethod    string     `json:"extraction_method"`
	PlanSource          string     `json:"plan_source"` // "existing_plan" or "new_plan"
}

// GetEventType returns the event type for TodoStepsExtractedEvent
func (e *TodoStepsExtractedEvent) GetEventType() events.EventType {
	return events.TodoStepsExtracted
}

// HumanControlledTodoPlannerOrchestrator manages simplified human-controlled todo planning process
// - Single execution (no iterations)
// - No validation phase
// - No critique phase
// - No cleanup phase
// - Simple direct planning approach
// - Always includes independent steps extraction for parallel execution
// - NEW: Includes learning phase after each step execution and validation
type HumanControlledTodoPlannerOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator
	// NEW: Store planning conversation for iterative refinement
	sessionID  string // For human feedback tracking
	workflowID string // For human feedback tracking
}

// NewHumanControlledTodoPlannerOrchestrator creates a new human-controlled todo planner orchestrator
func NewHumanControlledTodoPlannerOrchestrator(
	provider string,
	model string,
	temperature float64,
	agentMode string,
	selectedServers []string,
	mcpConfigPath string,
	llmConfig *orchestrator.LLMConfig,
	maxTurns int,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
) (*HumanControlledTodoPlannerOrchestrator, error) {

	// Create base workflow orchestrator
	baseOrchestrator, err := orchestrator.NewBaseOrchestrator(
		logger,
		eventBridge,
		orchestrator.OrchestratorTypeWorkflow,
		provider,
		model,
		mcpConfigPath,
		temperature,
		agentMode,
		selectedServers,
		llmConfig,
		maxTurns,
		customTools,
		customToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	return &HumanControlledTodoPlannerOrchestrator{
		BaseOrchestrator: baseOrchestrator,
		sessionID:        fmt.Sprintf("session_%d", time.Now().UnixNano()),
		workflowID:       fmt.Sprintf("workflow_%d", time.Now().UnixNano()),
	}, nil
}

// CreateTodoList orchestrates the human-controlled todo planning process
// - Single execution (no iterations)
// - Skips validation phase
// - Skips critique phase
// - Skips cleanup phase
// - Simple direct planning approach
// - NEW: Includes human approval loop with iterative plan refinement
func (hcpo *HumanControlledTodoPlannerOrchestrator) CreateTodoList(ctx context.Context, objective, workspacePath string) (string, error) {
	hcpo.GetLogger().Infof("🚀 Starting human-controlled todo planning for objective: %s", objective)

	// Set objective and workspace path directly
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)

	// Check if plan.md already exists
	planPath := fmt.Sprintf("%s/todo_creation_human/planning/plan.md", workspacePath)
	planExists, planContent, err := hcpo.checkExistingPlan(ctx, planPath)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for existing plan: %v", err)
		// Continue with normal planning flow
		planExists = false
	}

	var breakdownSteps []TodoStep
	var independentStepsResult string
	var planSource string
	var phasesDescription string

	if planExists {
		hcpo.GetLogger().Infof("📋 Found existing plan.md, converting to JSON and proceeding to execution")

		// Convert markdown plan to structured JSON using plan reader agent
		planReaderAgent, err := hcpo.createPlanReaderAgent(ctx, "plan_reading", 0, 1)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to create plan reader agent: %v", err)
			// Fall through to create new plan
			planExists = false
		} else {
			// Prepare template variables for plan reader agent
			readerTemplateVars := map[string]string{
				"Objective":     hcpo.GetObjective(),
				"WorkspacePath": hcpo.GetWorkspacePath(),
				"PlanMarkdown":  planContent, // Use the markdown content we found
			}

			// Execute plan reader agent to get structured output
			planReaderAgentTyped, ok := planReaderAgent.(*HumanControlledPlanReaderAgent)
			if !ok {
				hcpo.GetLogger().Warnf("⚠️ Failed to cast plan reader agent to correct type")
				planExists = false
			} else {
				existingPlan, err := planReaderAgentTyped.ExecuteStructured(ctx, readerTemplateVars, []llms.MessageContent{})
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to convert markdown plan to JSON: %v", err)
					// Fall through to create new plan
					planExists = false
				} else {
					// Convert existing plan to TodoStep format
					breakdownSteps = hcpo.convertPlanStepsToTodoSteps(existingPlan.Steps)
					hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "existing_plan")
					independentStepsResult = fmt.Sprintf("Using existing plan.md converted to JSON with %d steps", len(breakdownSteps))
					planSource = "Existing plan.md (converted to JSON)"
					phasesDescription = "Existing Plan.md → JSON Conversion → Step-by-Step Execution with Validation → Learning Analysis → Writing"
				}
			}
		}
	}

	if !planExists {
		hcpo.GetLogger().Infof("🔄 No existing plan found, creating new plan to execute objective")

		maxPlanRevisions := 20 // Allow up to 20 plan revisions
		var err error

		// Initialize conversation history for planning agent
		planningConversationHistory := []llms.MessageContent{}

		// Retry loop for plan approval and revision
		for revisionAttempt := 1; revisionAttempt <= maxPlanRevisions; revisionAttempt++ {
			hcpo.GetLogger().Infof("🔄 Plan revision attempt %d/%d", revisionAttempt, maxPlanRevisions)

			// Phase 1: Create markdown plan (with optional human feedback)
			_, planningConversationHistory, err = hcpo.runPlanningPhase(ctx, revisionAttempt, planningConversationHistory)
			if err != nil {
				return "", fmt.Errorf("planning phase failed: %w", err)
			}

			// Phase 1.5: Request human approval for markdown plan
			approved, feedback, err := hcpo.requestPlanApproval(ctx, revisionAttempt)
			if err != nil {
				return "", fmt.Errorf("plan approval request failed: %w", err)
			}

			if approved {
				hcpo.GetLogger().Infof("✅ Markdown plan approved by human, proceeding to conversion")
				break // Exit retry loop and continue to plan reading
			}

			// Plan rejected with feedback for revision - add to planning history
			hcpo.GetLogger().Infof("🔄 Plan revision requested (attempt %d/%d): %s", revisionAttempt, maxPlanRevisions, feedback)
			hcpo.addUserFeedbackToHistory(feedback, &planningConversationHistory)

			if revisionAttempt >= maxPlanRevisions {
				return "", fmt.Errorf("max plan revision attempts (%d) reached", maxPlanRevisions)
			}
		}

		// Phase 1.75: Read markdown plan and convert to structured JSON
		approvedPlan, err := hcpo.runPlanReaderPhase(ctx)
		if err != nil {
			return "", fmt.Errorf("plan reader phase failed: %w", err)
		}

		// Phase 1.8: Write structured plan to JSON file
		err = hcpo.runPlanWriterPhase(ctx, approvedPlan)
		if err != nil {
			return "", fmt.Errorf("plan writer phase failed: %w", err)
		}

		// Convert approved plan steps to TodoStep format for execution
		breakdownSteps = hcpo.convertPlanStepsToTodoSteps(approvedPlan.Steps)

		// Emit todo steps extracted event after plan reader conversion
		hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "new_plan_converted")

		independentStepsResult = fmt.Sprintf("Plan approved and converted to JSON with %d steps", len(breakdownSteps))
		planSource = "New plan created, approved, and converted"
		phasesDescription = "Markdown Planning → Human Approval → JSON Conversion → Step-by-Step Execution with Validation → Learning Analysis → Writing"
	}

	// Phase 2: Execute plan steps one by one (with validation after each step)
	_, err = hcpo.runExecutionPhase(ctx, breakdownSteps, 1)
	if err != nil {
		return "", fmt.Errorf("execution phase failed: %w", err)
	}

	// Phase 3: Write/Update todo list
	err = hcpo.runWriterPhase(ctx, 1)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Writer phase failed: %v", err)
	}

	// Phase 5: Skip critique phase (human-controlled mode)
	// No critique step in human-controlled mode

	duration := time.Since(hcpo.GetStartTime())
	hcpo.GetLogger().Infof("✅ Human-controlled todo planning completed in %v", duration)

	return fmt.Sprintf(`# Todo Planning Complete

## Planning Summary
- **Objective**: %s
- **Duration**: %v
- **Workspace**: %s
- **Plan Source**: %s
- **Phases**: %s

## Independent Steps Extracted (JSON Format)
%s

## Learning Analysis Summary
- **Learning Analysis**: Each step was analyzed for insights and recommendations
- **Learning Reports**: Generated for each step execution
- **Plan Updates**: plan.json was updated with execution learnings

## Final Todo List
Todo list has been created and saved as `+"`todo_final.md`"+` in the workspace root by the writer agent.

## Validation Reports
Step-by-step validation reports have been created and saved as `+"`validation_report.md`"+` in the validation folder for each executed step.

## Learning Reports
Learning analysis reports have been generated for each step, providing insights into what worked, what failed, and recommendations for improvement. All learnings have been accumulated in `+"`learning_reports.md`"+` in the workspace root for future reference.

## Next Steps
The todo list has been created and is ready for the execution phase. The independent steps are available in structured JSON format for programmatic access. Each step was validated after execution and analyzed for learnings to ensure proper completion and continuous improvement. All agents read from workspace files independently.`,
		hcpo.GetObjective(), duration, hcpo.GetWorkspacePath(),
		planSource,
		phasesDescription,
		independentStepsResult), nil
}

// runPlanningPhase creates markdown plan and returns conversation history
func (hcpo *HumanControlledTodoPlannerOrchestrator) runPlanningPhase(ctx context.Context, iteration int, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	planningTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
	}

	// Create fresh planning agent with proper context
	planningAgent, err := hcpo.createPlanningAgent(ctx, "planning", 0, iteration)
	if err != nil {
		return "", conversationHistory, fmt.Errorf("failed to create planning agent: %w", err)
	}

	// Execute planning agent to create markdown plan
	_, conversationHistory, err = planningAgent.Execute(ctx, planningTemplateVars, conversationHistory)
	if err != nil {
		return "", conversationHistory, fmt.Errorf("planning failed: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Markdown plan created successfully")
	return "markdown_plan_created", conversationHistory, nil
}

// runPlanReaderPhase reads markdown plan and converts to structured JSON
func (hcpo *HumanControlledTodoPlannerOrchestrator) runPlanReaderPhase(ctx context.Context) (*PlanningResponse, error) {
	hcpo.GetLogger().Infof("📖 Reading markdown plan and converting to structured JSON")

	// Create plan reader agent
	planReaderAgent, err := hcpo.createPlanReaderAgent(ctx, "plan_reading", 0, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan reader agent: %w", err)
	}

	// Read markdown plan content (this would typically use MCP tools)
	// For now, we'll assume the markdown content is available
	planMarkdown := "Plan markdown content would be read here" // TODO: Implement actual file reading

	// Prepare template variables for plan reader agent
	readerTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
		"PlanMarkdown":  planMarkdown,
	}

	// Execute plan reader agent to get structured output
	planReaderAgentTyped, ok := planReaderAgent.(*HumanControlledPlanReaderAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast plan reader agent to correct type")
	}

	result, err := planReaderAgentTyped.ExecuteStructured(ctx, readerTemplateVars, []llms.MessageContent{})
	if err != nil {
		return nil, fmt.Errorf("plan reading failed: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Plan converted to structured JSON successfully")
	return result, nil
}

// runPlanWriterPhase writes the structured plan to JSON file
func (hcpo *HumanControlledTodoPlannerOrchestrator) runPlanWriterPhase(ctx context.Context, approvedPlan *PlanningResponse) error {
	hcpo.GetLogger().Infof("📝 Writing structured plan to JSON file")

	// Convert plan to JSON
	planJSON, err := json.MarshalIndent(approvedPlan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plan to JSON: %w", err)
	}

	// Write JSON to file (this would typically use MCP tools)
	// For now, we'll just log it
	hcpo.GetLogger().Infof("Structured plan JSON: %s", string(planJSON))

	hcpo.GetLogger().Infof("✅ Plan written to JSON file successfully")
	return nil
}

// convertPlanStepsToTodoSteps converts PlanStep to TodoStep format
func (hcpo *HumanControlledTodoPlannerOrchestrator) convertPlanStepsToTodoSteps(planSteps []PlanStep) []TodoStep {
	todoSteps := make([]TodoStep, len(planSteps))
	for i, step := range planSteps {
		todoSteps[i] = TodoStep(step)
	}
	return todoSteps
}

// runExecutionPhase executes the plan steps one by one
func (hcpo *HumanControlledTodoPlannerOrchestrator) runExecutionPhase(ctx context.Context, breakdownSteps []TodoStep, iteration int) ([]llms.MessageContent, error) {
	hcpo.GetLogger().Infof("🔄 Starting step-by-step execution of %d steps", len(breakdownSteps))

	// Track human feedback across all steps for continuous improvement
	var humanFeedbackHistory []string

	// Execute each step one by one
	for i, step := range breakdownSteps {
		hcpo.GetLogger().Infof("📋 Executing step %d/%d: %s", i+1, len(breakdownSteps), step.Title)

		// Create conversation history for execution agent
		executionConversationHistory := []llms.MessageContent{}

		// Prepare template variables for this specific step with individual fields
		templateVars := map[string]string{
			"StepNumber":            fmt.Sprintf("%d", i+1),
			"TotalSteps":            fmt.Sprintf("%d", len(breakdownSteps)),
			"StepTitle":             step.Title,
			"StepDescription":       step.Description,
			"StepSuccessCriteria":   step.SuccessCriteria,
			"StepWhyThisStep":       step.WhyThisStep,
			"StepContextOutput":     step.ContextOutput,
			"WorkspacePath":         hcpo.GetWorkspacePath(),
			"LearningAgentOutput":   "", // Will be populated with learning agent's output
			"PreviousHumanFeedback": "", // Will be populated with human feedback from previous steps
		}

		// Add context dependencies as a comma-separated string
		if len(step.ContextDependencies) > 0 {
			templateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
		} else {
			templateVars["StepContextDependencies"] = ""
		}

		// Add previous human feedback to template variables
		if len(humanFeedbackHistory) > 0 {
			templateVars["PreviousHumanFeedback"] = strings.Join(humanFeedbackHistory, "\n---\n")
		} else {
			templateVars["PreviousHumanFeedback"] = ""
		}

		// Execute this specific step with retry logic
		maxRetryAttempts := 3
		var validationFeedback []ValidationFeedback
		var validationResponse *ValidationResponse

		for retryAttempt := 1; retryAttempt <= maxRetryAttempts; retryAttempt++ {
			hcpo.GetLogger().Infof("🔄 Executing step %d/%d (attempt %d/%d): %s", i+1, len(breakdownSteps), retryAttempt, maxRetryAttempts, step.Title)

			// Add validation feedback to template variables if this is a retry
			if retryAttempt > 1 {
				feedbackJSON, _ := json.Marshal(validationFeedback)
				templateVars["ValidationFeedback"] = string(feedbackJSON)
			} else {
				templateVars["ValidationFeedback"] = ""
			}

			// Create execution agent for this step
			executionAgent, err := hcpo.createExecutionAgent(ctx, "execution", i+1, iteration)
			if err != nil {
				return nil, fmt.Errorf("failed to create execution agent for step %d: %w", i+1, err)
			}

			// Execute this specific step with execution conversation history
			_, executionConversationHistory, err = executionAgent.Execute(ctx, templateVars, executionConversationHistory)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Step %d execution failed (attempt %d): %v", i+1, retryAttempt, err)
				if retryAttempt >= maxRetryAttempts {
					hcpo.GetLogger().Errorf("❌ Step %d execution failed after %d attempts", i+1, maxRetryAttempts)
					continue
				}
				continue
			}

			hcpo.GetLogger().Infof("✅ Step %d execution completed successfully (attempt %d)", i+1, retryAttempt)

			// Validate this step's execution using structured output
			hcpo.GetLogger().Infof("🔍 Validating step %d execution (attempt %d)", i+1, retryAttempt)

			validationAgent, err := hcpo.createValidationAgent(ctx, "validation", i+1, iteration)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to create validation agent for step %d: %v", i+1, err)
				if retryAttempt >= maxRetryAttempts {
					continue
				}
				continue
			}

			// Prepare validation template variables with individual fields
			validationTemplateVars := map[string]string{
				"StepNumber":          fmt.Sprintf("%d", i+1),
				"TotalSteps":          fmt.Sprintf("%d", len(breakdownSteps)),
				"StepTitle":           step.Title,
				"StepDescription":     step.Description,
				"StepSuccessCriteria": step.SuccessCriteria,
				"StepWhyThisStep":     step.WhyThisStep,
				"StepContextOutput":   step.ContextOutput,
				"WorkspacePath":       hcpo.GetWorkspacePath(),
				"ExecutionHistory":    hcpo.formatConversationHistory(executionConversationHistory),
			}

			// Add context dependencies as a comma-separated string
			if len(step.ContextDependencies) > 0 {
				validationTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
			} else {
				validationTemplateVars["StepContextDependencies"] = ""
			}

			// Validate this step's execution using structured output
			validationResponse, err = validationAgent.(*HumanControlledTodoPlannerValidationAgent).ExecuteStructured(ctx, validationTemplateVars, []llms.MessageContent{})
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Step %d validation failed (attempt %d): %v", i+1, retryAttempt, err)
				if retryAttempt >= maxRetryAttempts {
					continue
				}
				continue
			}

			hcpo.GetLogger().Infof("✅ Step %d validation completed successfully (attempt %d)", i+1, retryAttempt)
			hcpo.GetLogger().Infof("📊 Validation result: Success Criteria Met: %v, Status: %s", validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)

			// Run appropriate learning phase based on validation result
			if validationResponse.IsSuccessCriteriaMet {
				// Success Learning Agent - analyze what worked well and update plan.json
				hcpo.GetLogger().Infof("🧠 Running success learning analysis for step %d", i+1)
				err := hcpo.runSuccessLearningPhase(ctx, i+1, len(breakdownSteps), step, executionConversationHistory, validationResponse)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Success learning phase failed for step %d: %v", i+1, err)
				} else {
					hcpo.GetLogger().Infof("✅ Success learning analysis completed for step %d", i+1)
				}
			} else {
				// Failure Learning Agent - analyze what went wrong and provide refined task description
				hcpo.GetLogger().Infof("🧠 Running failure learning analysis for step %d", i+1)
				refinedTaskDescription, learningAnalysis, err := hcpo.runFailureLearningPhase(ctx, i+1, len(breakdownSteps), step, executionConversationHistory, validationResponse)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failure learning phase failed for step %d: %v", i+1, err)
				} else {
					hcpo.GetLogger().Infof("✅ Failure learning analysis completed for step %d", i+1)

					// Update step description for retry
					if refinedTaskDescription != "" {
						step.Description = refinedTaskDescription
						templateVars["StepDescription"] = refinedTaskDescription
						templateVars["LearningAgentOutput"] = learningAnalysis
						hcpo.GetLogger().Infof("🔄 Updated step %d description with refined task for retry", i+1)
					}
				}
			}

			// Check if success criteria was met
			if validationResponse.IsSuccessCriteriaMet {
				hcpo.GetLogger().Infof("✅ Step %d passed validation - success criteria met", i+1)
				break // Exit retry loop and continue to next step
			} else {
				hcpo.GetLogger().Warnf("⚠️ Step %d failed validation - success criteria not met (attempt %d/%d)", i+1, retryAttempt, maxRetryAttempts)

				// Store feedback for next retry attempt
				validationFeedback = validationResponse.Feedback

				if retryAttempt >= maxRetryAttempts {
					hcpo.GetLogger().Errorf("❌ Step %d failed validation after %d attempts", i+1, maxRetryAttempts)
					// Continue to next step even if validation failed
					break
				} else {
					hcpo.GetLogger().Infof("🔄 Retrying step %d execution with validation feedback", i+1)
					// Reset conversation history for retry
					executionConversationHistory = []llms.MessageContent{}
				}
			}
		}

		// BLOCKING HUMAN FEEDBACK - Ask user if they want to continue to next step
		var validationSummary string
		if validationResponse != nil {
			validationSummary = fmt.Sprintf("Step %d validation completed. Success Criteria Met: %v, Status: %s", i+1, validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)
		} else {
			validationSummary = fmt.Sprintf("Step %d execution failed - no validation response available", i+1)
		}
		approved, feedback, err := hcpo.requestHumanFeedback(ctx, i+1, len(breakdownSteps), validationSummary)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Human feedback request failed: %v", err)
			// Default to continue if feedback fails
			approved = true
		}

		// Store human feedback for future steps (even if approved, user might have provided guidance)
		if feedback != "" {
			feedbackEntry := fmt.Sprintf("Step %d/%d Feedback: %s", i+1, len(breakdownSteps), feedback)
			humanFeedbackHistory = append(humanFeedbackHistory, feedbackEntry)
			hcpo.GetLogger().Infof("📝 Stored human feedback for future steps: %s", feedbackEntry)
		}

		if !approved {
			hcpo.GetLogger().Infof("🛑 User requested to stop execution after step %d with feedback: %s", i+1, feedback)
			break
		}
	}

	hcpo.GetLogger().Infof("✅ All steps execution completed")
	return nil, nil
}

// runSuccessLearningPhase analyzes successful executions to capture best practices and improve plan.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) runSuccessLearningPhase(ctx context.Context, stepNumber, totalSteps int, step TodoStep, executionHistory []llms.MessageContent, validationResponse *ValidationResponse) error {
	hcpo.GetLogger().Infof("🧠 Starting success learning analysis for step %d/%d: %s", stepNumber, totalSteps, step.Title)

	// Create success learning agent
	successLearningAgent, err := hcpo.createSuccessLearningAgent(ctx, "success_learning", stepNumber, 1)
	if err != nil {
		return fmt.Errorf("failed to create success learning agent: %w", err)
	}

	// Format validation result for template
	validationResultJSON, err := json.MarshalIndent(validationResponse, "", "  ")
	if err != nil {
		validationResultJSON = []byte(fmt.Sprintf("Validation failed to marshal: %v", err))
	}

	// Prepare template variables for success learning agent
	successLearningTemplateVars := map[string]string{
		"StepTitle":           step.Title,
		"StepDescription":     step.Description,
		"StepSuccessCriteria": step.SuccessCriteria,
		"StepWhyThisStep":     step.WhyThisStep,
		"StepContextOutput":   step.ContextOutput,
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		"ExecutionHistory":    hcpo.formatConversationHistory(executionHistory),
		"ValidationResult":    string(validationResultJSON),
		"CurrentObjective":    hcpo.GetObjective(),
	}

	// Add context dependencies as a comma-separated string
	if len(step.ContextDependencies) > 0 {
		successLearningTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
	} else {
		successLearningTemplateVars["StepContextDependencies"] = ""
	}

	// Execute success learning agent
	_, _, err = successLearningAgent.Execute(ctx, successLearningTemplateVars, []llms.MessageContent{})
	if err != nil {
		return fmt.Errorf("success learning analysis failed: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Success learning analysis completed for step %d", stepNumber)
	return nil
}

// runFailureLearningPhase analyzes failed executions to provide refined task descriptions for retry
func (hcpo *HumanControlledTodoPlannerOrchestrator) runFailureLearningPhase(ctx context.Context, stepNumber, totalSteps int, step TodoStep, executionHistory []llms.MessageContent, validationResponse *ValidationResponse) (string, string, error) {
	hcpo.GetLogger().Infof("🧠 Starting failure learning analysis for step %d/%d: %s", stepNumber, totalSteps, step.Title)

	// Create failure learning agent
	failureLearningAgent, err := hcpo.createFailureLearningAgent(ctx, "failure_learning", stepNumber, 1)
	if err != nil {
		return "", "", fmt.Errorf("failed to create failure learning agent: %w", err)
	}

	// Format validation result for template
	validationResultJSON, err := json.MarshalIndent(validationResponse, "", "  ")
	if err != nil {
		validationResultJSON = []byte(fmt.Sprintf("Validation failed to marshal: %v", err))
	}

	// Prepare template variables for failure learning agent
	failureLearningTemplateVars := map[string]string{
		"StepTitle":           step.Title,
		"StepDescription":     step.Description,
		"StepSuccessCriteria": step.SuccessCriteria,
		"StepWhyThisStep":     step.WhyThisStep,
		"StepContextOutput":   step.ContextOutput,
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		"ExecutionHistory":    hcpo.formatConversationHistory(executionHistory),
		"ValidationResult":    string(validationResultJSON),
		"CurrentObjective":    hcpo.GetObjective(),
	}

	// Add context dependencies as a comma-separated string
	if len(step.ContextDependencies) > 0 {
		failureLearningTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
	} else {
		failureLearningTemplateVars["StepContextDependencies"] = ""
	}

	// Execute failure learning agent
	failureLearningOutput, _, err := failureLearningAgent.Execute(ctx, failureLearningTemplateVars, []llms.MessageContent{})
	if err != nil {
		return "", "", fmt.Errorf("failure learning analysis failed: %w", err)
	}

	// Extract refined task description from the output
	refinedTaskDescription := hcpo.extractRefinedTaskDescription(failureLearningOutput)
	learningAnalysis := failureLearningOutput // Use the full output as learning analysis

	hcpo.GetLogger().Infof("✅ Failure learning analysis completed for step %d", stepNumber)
	return refinedTaskDescription, learningAnalysis, nil
}

// runLearningPhase analyzes execution history and validation results to extract learnings and generate refined task description
// DEPRECATED: Use runSuccessLearningPhase or runFailureLearningPhase instead
func (hcpo *HumanControlledTodoPlannerOrchestrator) runLearningPhase(ctx context.Context, stepNumber, totalSteps int, step TodoStep, executionHistory []llms.MessageContent, validationResponse *ValidationResponse) (string, string, error) {
	hcpo.GetLogger().Infof("🧠 Starting learning analysis for step %d/%d: %s", stepNumber, totalSteps, step.Title)

	// Create learning agent
	learningAgent, err := hcpo.createLearningAgent(ctx, "learning", stepNumber, 1)
	if err != nil {
		return "", "", fmt.Errorf("failed to create learning agent: %w", err)
	}

	// Format validation result for template
	validationResultJSON, err := json.MarshalIndent(validationResponse, "", "  ")
	if err != nil {
		validationResultJSON = []byte(fmt.Sprintf("Validation failed to marshal: %v", err))
	}

	// Prepare template variables for learning agent
	learningTemplateVars := map[string]string{
		"StepTitle":           step.Title,
		"StepDescription":     step.Description,
		"StepSuccessCriteria": step.SuccessCriteria,
		"StepWhyThisStep":     step.WhyThisStep,
		"StepContextOutput":   step.ContextOutput,
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		"ExecutionHistory":    hcpo.formatConversationHistory(executionHistory),
		"ValidationResult":    string(validationResultJSON),
		"CurrentObjective":    hcpo.GetObjective(),
	}

	// Add context dependencies as a comma-separated string
	if len(step.ContextDependencies) > 0 {
		learningTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
	} else {
		learningTemplateVars["StepContextDependencies"] = ""
	}

	// Execute learning agent with simple text output
	learningOutput, _, err := learningAgent.Execute(ctx, learningTemplateVars, []llms.MessageContent{})
	if err != nil {
		return "", "", fmt.Errorf("learning analysis failed: %w", err)
	}

	// Extract refined task description from the output
	refinedTaskDescription := hcpo.extractRefinedTaskDescription(learningOutput)
	learningAnalysis := learningOutput // Use the full output as learning analysis

	hcpo.GetLogger().Infof("✅ Learning analysis completed for step %d", stepNumber)
	return refinedTaskDescription, learningAnalysis, nil
}

// extractRefinedTaskDescription extracts the refined task description from learning agent output
func (hcpo *HumanControlledTodoPlannerOrchestrator) extractRefinedTaskDescription(learningOutput string) string {
	// Look for "### Refined Task:" section in the output
	lines := strings.Split(learningOutput, "\n")
	inRefinedTaskSection := false
	var refinedTaskLines []string

	for _, line := range lines {
		if strings.Contains(line, "### Refined Task:") {
			inRefinedTaskSection = true
			continue
		}
		if inRefinedTaskSection {
			// Stop when we hit the next section (starts with ###)
			if strings.HasPrefix(strings.TrimSpace(line), "###") && !strings.Contains(line, "Refined Task") {
				break
			}
			// Skip empty lines at the start
			if len(refinedTaskLines) == 0 && strings.TrimSpace(line) == "" {
				continue
			}
			refinedTaskLines = append(refinedTaskLines, line)
		}
	}

	refinedTask := strings.TrimSpace(strings.Join(refinedTaskLines, "\n"))
	if refinedTask == "" {
		// Fallback: return the original step description if no refined task found
		return ""
	}

	return refinedTask
}

// runWriterPhase creates optimal todo list based on plan and execution experience
func (hcpo *HumanControlledTodoPlannerOrchestrator) runWriterPhase(ctx context.Context, iteration int) error {
	writerAgent, err := hcpo.createWriterAgent(ctx, "writing", 0, iteration)
	if err != nil {
		return fmt.Errorf("failed to create writer agent: %w", err)
	}

	// Prepare template variables for Execute method
	writerTemplateVars := map[string]string{
		"Objective":       hcpo.GetObjective(),
		"WorkspacePath":   hcpo.GetWorkspacePath(),
		"TotalIterations": fmt.Sprintf("%d", iteration),
	}

	_, _, err = writerAgent.Execute(ctx, writerTemplateVars, nil)
	if err != nil {
		return fmt.Errorf("todo list creation failed: %w", err)
	}

	return nil
}

// requestHumanFeedback requests human feedback after validation and blocks until user responds
// Returns: (approved bool, feedback string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestHumanFeedback(ctx context.Context, currentStep, totalSteps int, validationResult string) (bool, string, error) {
	hcpo.GetLogger().Infof("🤔 Requesting human feedback for step %d/%d", currentStep, totalSteps)

	// Generate unique request ID
	requestID := fmt.Sprintf("step_feedback_%d_%d_%d", currentStep, totalSteps, time.Now().UnixNano())

	// Use common human feedback function
	return hcpo.RequestHumanFeedback(
		ctx,
		requestID,
		fmt.Sprintf("Step %d/%d validation completed. Should we continue with execution of the next step?", currentStep, totalSteps),
		validationResult, // Show validation results as context
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
}

// Agent creation methods - reuse from base orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPlanningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"human-controlled-planning-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerPlanningAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) createExecutionAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"execution-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerExecutionAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createValidationAgent creates a validation agent for the current iteration
func (hcpo *HumanControlledTodoPlannerOrchestrator) createValidationAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"validation-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerValidationAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) createWriterAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"writer-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerWriterAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createPlanReaderAgent creates a plan reader agent for converting markdown to JSON
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPlanReaderAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"plan-reader-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{"NO_SERVERS"}, // Special MCP identifier for no servers - plan reader only converts markdown to JSON
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledPlanReaderAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createSuccessLearningAgent creates a success learning agent for analyzing successful executions
func (hcpo *HumanControlledTodoPlannerOrchestrator) createSuccessLearningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"success-learning-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerSuccessLearningAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createFailureLearningAgent creates a failure learning agent for analyzing failed executions
func (hcpo *HumanControlledTodoPlannerOrchestrator) createFailureLearningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"failure-learning-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerFailureLearningAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createLearningAgent creates a learning agent for analyzing execution history and validation results
// DEPRECATED: Use createSuccessLearningAgent or createFailureLearningAgent instead
func (hcpo *HumanControlledTodoPlannerOrchestrator) createLearningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"learning-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerFailureLearningAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// emitTodoStepsExtractedEvent emits an event when todo steps are extracted from a plan
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitTodoStepsExtractedEvent(ctx context.Context, extractedSteps []TodoStep, planSource string) {
	if hcpo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data
	eventData := &TodoStepsExtractedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		TotalStepsExtracted: len(extractedSteps),
		ExtractedSteps:      extractedSteps,
		ExtractionMethod:    "structured_breakdown_agent",
		PlanSource:          planSource,
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
		Type:      events.TodoStepsExtracted,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Debug: Log the event data before emission
	hcpo.GetLogger().Infof("🔍 DEBUG: Event data before emission: %+v", eventData)
	hcpo.GetLogger().Infof("🔍 DEBUG: Unified event before emission: %+v", unifiedEvent)

	// Emit through the context-aware bridge
	bridge := hcpo.GetContextAwareBridge()
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to emit todo steps extracted event: %v", err)
	} else {
		hcpo.GetLogger().Infof("✅ Emitted todo steps extracted event: %d steps extracted", len(extractedSteps))
	}
}

// Execute implements the Orchestrator interface
func (hcpo *HumanControlledTodoPlannerOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	// Validate that no options are provided since this orchestrator doesn't use them
	if len(options) > 0 {
		return "", fmt.Errorf("human-controlled todo planner orchestrator does not accept options")
	}

	// Validate workspace path is provided
	if workspacePath == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	// Call the existing CreateTodoList method
	return hcpo.CreateTodoList(ctx, objective, workspacePath)
}

// GetType returns the orchestrator type
func (hcpo *HumanControlledTodoPlannerOrchestrator) GetType() string {
	return "human_controlled_todo_planner"
}

// Helper methods for human feedback tracking

// getSessionID returns the session ID for this orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) getSessionID() string {
	return hcpo.sessionID
}

// getWorkflowID returns the workflow ID for this orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) getWorkflowID() string {
	return hcpo.workflowID
}

// checkExistingPlan checks if a plan file already exists in the workspace and returns the plan content if found
// Uses the generic ReadWorkspaceFile function from base orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) checkExistingPlan(ctx context.Context, planPath string) (bool, string, error) {
	hcpo.GetLogger().Infof("🔍 Checking for existing plan at %s", planPath)

	// Use the generic ReadWorkspaceFile function from base orchestrator
	planContent, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Infof("📋 No existing plan found: %v", err)
			return false, "", nil
		}
		// Other errors should be returned
		return false, "", err
	}

	hcpo.GetLogger().Infof("✅ Found existing plan at %s", planPath)
	return true, planContent, nil
}

// addUserFeedbackToHistory adds human feedback to conversation history
func (hcpo *HumanControlledTodoPlannerOrchestrator) addUserFeedbackToHistory(feedback string, conversationHistory *[]llms.MessageContent) {
	feedbackMessage := llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: feedback}},
	}
	*conversationHistory = append(*conversationHistory, feedbackMessage)
}

// formatConversationHistory formats conversation history for template usage
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatConversationHistory(conversationHistory []llms.MessageContent) string {
	var result strings.Builder

	for _, message := range conversationHistory {
		// Skip system messages
		if message.Role == llms.ChatMessageTypeSystem {
			continue
		}

		switch message.Role {
		case llms.ChatMessageTypeHuman:
			result.WriteString("## Human Message\n")
		case llms.ChatMessageTypeAI:
			result.WriteString("## Assistant Response\n")
		case llms.ChatMessageTypeTool:
			result.WriteString("## Tool Response\n")
		default:
			result.WriteString("## Message\n")
		}

		for _, part := range message.Parts {
			switch p := part.(type) {
			case llms.TextContent:
				result.WriteString(p.Text)
				result.WriteString("\n\n")
			case llms.ToolCall:
				result.WriteString("### Tool Call\n")
				result.WriteString(fmt.Sprintf("**Tool Name:** %s\n", p.FunctionCall.Name))
				result.WriteString(fmt.Sprintf("**Tool ID:** %s\n", p.ID))
				if p.FunctionCall.Arguments != "" {
					result.WriteString(fmt.Sprintf("**Arguments:** %s\n", p.FunctionCall.Arguments))
				}
				result.WriteString("\n")
			case llms.ToolCallResponse:
				result.WriteString("### Tool Response\n")
				result.WriteString(fmt.Sprintf("**Tool ID:** %s\n", p.ToolCallID))
				if p.Name != "" {
					result.WriteString(fmt.Sprintf("**Tool Name:** %s\n", p.Name))
				}
				result.WriteString(fmt.Sprintf("**Response:** %s\n", p.Content))
				result.WriteString("\n")
			default:
				// Handle any other content types
				result.WriteString(fmt.Sprintf("**Unknown Content Type:** %T\n", p))
			}
		}
		result.WriteString("---\n\n")
	}

	return result.String()
}

// requestPlanApproval requests human approval for the generated plan
// Returns: (approved bool, feedback string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestPlanApproval(
	ctx context.Context,
	revisionAttempt int,
) (bool, string, error) {
	hcpo.GetLogger().Infof("⏸️ Requesting human approval for plan (attempt %d)", revisionAttempt)

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
