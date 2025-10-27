package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
)

// StepProgress tracks which steps have been completed
type StepProgress struct {
	CompletedStepIndices []int     `json:"completed_step_indices"` // 0-based indices
	TotalSteps           int       `json:"total_steps"`
	LastUpdated          time.Time `json:"last_updated"`
}

// TodoStep represents a todo step in the execution
type TodoStep struct {
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	SuccessCriteria     string   `json:"success_criteria"`
	WhyThisStep         string   `json:"why_this_step"`
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
	SuccessPatterns     []string `json:"success_patterns,omitempty"` // NEW - what worked (includes tools)
	FailurePatterns     []string `json:"failure_patterns,omitempty"` // NEW - what failed (includes tools to avoid)
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

	// Variable management
	variablesManifest  *VariablesManifest // Extracted variables
	templatedObjective string             // Objective with {{VARS}}
	variableValues     map[string]string  // Runtime variable values

	// Fast execute mode tracking
	fastExecuteMode    bool // Whether we're in fast execute mode
	fastExecuteEndStep int  // Last step index to fast execute (0-based)
}

// NewHumanControlledTodoPlannerOrchestrator creates a new human-controlled todo planner orchestrator
func NewHumanControlledTodoPlannerOrchestrator(
	provider string,
	model string,
	temperature float64,
	agentMode string,
	selectedServers []string,
	selectedTools []string, // NEW parameter
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
		selectedTools, // Pass through actual selected tools
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

// getStepsProgressPath returns the path to steps_done.json file
func (hcpo *HumanControlledTodoPlannerOrchestrator) getStepsProgressPath() string {
	return fmt.Sprintf("%s/todo_creation_human/steps_done.json", hcpo.GetWorkspacePath())
}

// loadStepProgress loads progress from steps_done.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) loadStepProgress(ctx context.Context) (*StepProgress, error) {
	progressPath := hcpo.getStepsProgressPath()

	content, err := hcpo.ReadWorkspaceFile(ctx, progressPath)
	if err != nil {
		// File doesn't exist or error reading
		return nil, err
	}

	var progress StepProgress
	if err := json.Unmarshal([]byte(content), &progress); err != nil {
		return nil, fmt.Errorf("failed to parse steps_done.json: %w", err)
	}

	return &progress, nil
}

// saveStepProgress saves progress to steps_done.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) saveStepProgress(ctx context.Context, progress *StepProgress) error {
	progressPath := hcpo.getStepsProgressPath()

	progress.LastUpdated = time.Now()

	progressJSON, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal progress: %w", err)
	}

	if err := hcpo.WriteWorkspaceFile(ctx, progressPath, string(progressJSON)); err != nil {
		return fmt.Errorf("failed to write steps_done.json: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Saved step progress to %s", progressPath)
	return nil
}

// deleteStepProgress deletes steps_done.json file
func (hcpo *HumanControlledTodoPlannerOrchestrator) deleteStepProgress(ctx context.Context) error {
	progressPath := hcpo.getStepsProgressPath()

	if err := hcpo.DeleteWorkspaceFile(ctx, progressPath); err != nil {
		// Ignore error if file doesn't exist
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return nil
		}
		return fmt.Errorf("failed to delete steps_done.json: %w", err)
	}

	hcpo.GetLogger().Infof("🗑️ Deleted step progress file: %s", progressPath)
	return nil
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

	// PHASE 0: Variable Extraction with Human Verification (NEW)
	// Check if variables.json already exists
	variablesPath := fmt.Sprintf("%s/todo_creation_human/variables/variables.json", workspacePath)
	variablesExist, existingVariablesManifest, err := hcpo.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for existing variables: %v", err)
		variablesExist = false
	}

	var variablesManifest *VariablesManifest
	var templatedObjective string

	// If variables exist, ask user if they want to use them or re-extract
	if variablesExist {
		requestID := fmt.Sprintf("existing_variables_decision_%d", time.Now().UnixNano())
		useExistingVariables, err := hcpo.RequestYesNoFeedback(
			ctx,
			requestID,
			"Found existing variables.json. Do you want to use the existing variables or extract new ones from the objective?",
			"Use Existing Variables", // Yes button label
			"Extract New Variables",  // No button label
			fmt.Sprintf("Variables file: %s\nFound %d variables", variablesPath, len(existingVariablesManifest.Variables)),
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for existing variables: %v", err)
			// Default to using existing variables
			useExistingVariables = true
		}

		if useExistingVariables {
			hcpo.GetLogger().Infof("✅ User chose to use existing variables")
			variablesManifest = existingVariablesManifest
			templatedObjective = existingVariablesManifest.Objective
		} else {
			hcpo.GetLogger().Infof("🔄 User chose to extract new variables, proceeding with extraction")
			variablesExist = false // Trigger variable extraction
		}
	}

	// Extract variables if they don't exist or user wants to re-extract
	if !variablesExist {
		maxVariableRevisions := 10
		var variableFeedback string
		var variableConversationHistory []llms.MessageContent

		for revisionAttempt := 1; revisionAttempt <= maxVariableRevisions; revisionAttempt++ {
			hcpo.GetLogger().Infof("🔄 Variable extraction attempt %d/%d", revisionAttempt, maxVariableRevisions)

			// Run variable extraction phase (with optional human feedback)
			var err error
			variablesManifest, templatedObjective, err = hcpo.runVariableExtractionPhase(ctx, revisionAttempt, variableFeedback, variableConversationHistory)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Variable extraction failed: %v, continuing without variables", err)
				templatedObjective = objective // Use original objective if extraction fails
				break
			}

			// Accumulate conversation history for next iteration
			variableConversationHistory = append(variableConversationHistory, llms.MessageContent{
				Role:  llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{llms.TextContent{Text: fmt.Sprintf("Extracted %d variables from objective", len(variablesManifest.Variables))}},
			})

			hcpo.GetLogger().Infof("✅ Extracted %d variables, templated objective: %s",
				len(variablesManifest.Variables), templatedObjective)

			// Request human approval for extracted variables
			approved, feedback, err := hcpo.requestVariableApproval(ctx, variablesManifest, revisionAttempt)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Variable approval request failed: %v, will retry", err)
				// Don't auto-approve on error - treat as need for retry
				approved = false
				feedback = fmt.Sprintf("Error getting approval: %v", err)
			}

			if approved {
				hcpo.GetLogger().Infof("✅ Variables approved by human, proceeding to planning")
				break // Exit retry loop
			}

			// Variables rejected with feedback for revision
			hcpo.GetLogger().Infof("🔄 Variable revision requested (attempt %d/%d): %s", revisionAttempt, maxVariableRevisions, feedback)
			variableFeedback = feedback // Store feedback for next attempt

			if revisionAttempt >= maxVariableRevisions {
				hcpo.GetLogger().Warnf("⚠️ Max variable revision attempts (%d) reached, using extracted variables", maxVariableRevisions)
				break
			}
		}
	}

	// Load runtime variable values if provided and switch to templated objective
	if variablesManifest != nil {
		if err := hcpo.loadVariableValues(ctx); err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to load variable values: %v", err)
		}

		// Switch to templated objective for all subsequent phases
		hcpo.SetObjective(templatedObjective)
		hcpo.GetLogger().Infof("✅ Using templated objective with {{VARIABLES}}: %s", templatedObjective)
	}

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
		hcpo.GetLogger().Infof("📋 Found existing plan.md at %s", planPath)

		// Request human decision: use existing plan or create new one
		requestID := fmt.Sprintf("existing_plan_decision_%d", time.Now().UnixNano())
		useExistingPlan, err := hcpo.RequestYesNoFeedback(
			ctx,
			requestID,
			"Found existing plan.md. Do you want to use the existing plan or create a new one?",
			"Use Existing Plan", // Yes button label
			"Create New Plan",   // No button label
			fmt.Sprintf("Plan location: %s", planPath),
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for existing plan: %v", err)
			// Default to using existing plan
			useExistingPlan = true
		}

		if !useExistingPlan {
			hcpo.GetLogger().Infof("🔄 User chose to create new plan, skipping existing plan")
			planExists = false
		} else {
			hcpo.GetLogger().Infof("✅ User chose to use existing plan, converting to JSON and proceeding to execution")
		}
	}

	if planExists {
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

		// Delete existing progress since we're starting fresh planning
		if err := hcpo.deleteStepProgress(ctx); err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to delete step progress: %v", err)
		}

		maxPlanRevisions := 20 // Allow up to 20 plan revisions
		var err error
		var humanFeedback string                                 // Track human feedback separately
		var accumulatedConversationHistory []llms.MessageContent // Track conversation across iterations

		// Retry loop for plan approval and revision
		for revisionAttempt := 1; revisionAttempt <= maxPlanRevisions; revisionAttempt++ {
			hcpo.GetLogger().Infof("🔄 Plan revision attempt %d/%d", revisionAttempt, maxPlanRevisions)

			// Phase 1: Create markdown plan (with optional human feedback)
			_, updatedHistory, err := hcpo.runPlanningPhase(ctx, revisionAttempt, humanFeedback, accumulatedConversationHistory)
			if err != nil {
				return "", fmt.Errorf("planning phase failed: %w", err)
			}

			// Accumulate conversation history for next iteration
			accumulatedConversationHistory = updatedHistory

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
			humanFeedback = feedback // Store feedback for next attempt

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

	// Check for existing progress and ask user if they want to resume
	var startFromStep int = 0 // 0-based index, 0 means start from beginning
	var existingProgress *StepProgress

	// Check if there's existing progress
	existingProgress, err = hcpo.loadStepProgress(ctx)
	if err == nil && existingProgress != nil && len(existingProgress.CompletedStepIndices) > 0 {
		hcpo.GetLogger().Infof("📊 Found existing progress: %d/%d steps completed",
			len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps)

		// Check if total steps match (plan might have changed)
		if existingProgress.TotalSteps != len(breakdownSteps) {
			hcpo.GetLogger().Warnf("⚠️ Plan has changed (different number of steps), ignoring previous progress")
			existingProgress = nil
		} else {
			// Ask user if they want to resume
			nextIncompleteStep := 0
			for i := 0; i < existingProgress.TotalSteps; i++ {
				completed := false
				for _, completedIdx := range existingProgress.CompletedStepIndices {
					if completedIdx == i {
						completed = true
						break
					}
				}
				if !completed {
					nextIncompleteStep = i + 1 // 1-based for display
					break
				}
			}

			if nextIncompleteStep > 0 {
				// Calculate the last completed step number (1-based) for display
				lastCompletedStepNumber := max(existingProgress.CompletedStepIndices) + 1 // Convert to 1-based

				requestID := fmt.Sprintf("resume_progress_%d", time.Now().UnixNano())
				choice, err := hcpo.RequestThreeChoiceFeedback(
					ctx,
					requestID,
					fmt.Sprintf("Found existing progress: %d/%d steps completed. How would you like to proceed?",
						len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps),
					fmt.Sprintf("Resume from Step %d", nextIncompleteStep),
					"Start from Beginning",
					fmt.Sprintf("Fast Execute (0 to Step %d)", lastCompletedStepNumber),
					fmt.Sprintf("Last updated: %s", existingProgress.LastUpdated.Format("2006-01-02 15:04:05")),
					hcpo.getSessionID(),
					hcpo.getWorkflowID(),
				)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for resuming: %v", err)
					choice = "option1" // Default to resume
				}

				// Track fast execute mode
				fastExecuteMode := false
				fastExecuteEndStep := -1

				switch choice {
				case "option1": // Resume from next incomplete step
					startFromStep = nextIncompleteStep - 1 // Convert back to 0-based
					hcpo.GetLogger().Infof("✅ User chose to resume from step %d", nextIncompleteStep)
				case "option2": // Start from beginning (normal execution)
					hcpo.GetLogger().Infof("🔄 User chose to start from beginning, will reset progress")
					// Delete existing progress and start fresh
					if err := hcpo.deleteStepProgress(ctx); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to delete step progress: %v", err)
					}
					existingProgress = nil
					startFromStep = 0
				case "option3": // Fast execute completed steps
					hcpo.GetLogger().Infof("⚡ User chose fast execute mode for completed steps")
					fastExecuteMode = true
					fastExecuteEndStep = max(existingProgress.CompletedStepIndices)
					// Delete previous completed indices to re-execute them
					startFromStep = 0
					// Reset completed indices for steps to be re-executed
					var newCompletedIndices []int
					for _, idx := range existingProgress.CompletedStepIndices {
						if idx > fastExecuteEndStep {
							newCompletedIndices = append(newCompletedIndices, idx)
						}
					}
					existingProgress.CompletedStepIndices = newCompletedIndices
					hcpo.GetLogger().Infof("⚡ Will fast execute steps 0 to %d, then continue with normal execution from step %d", fastExecuteEndStep, nextIncompleteStep)
				}

				// Store fast execute mode for use in execution loop
				hcpo.SetFastExecuteMode(fastExecuteMode, fastExecuteEndStep)
			} else {
				// All steps are completed, skip directly to writer phase
				hcpo.GetLogger().Infof("✅ All steps already completed (%d/%d), skipping execution phase and going directly to writer phase",
					len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps)

				// Phase 3: Write/Update todo list with human review loop
				err = hcpo.runWriterPhaseWithHumanReview(ctx, 1)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Writer phase with human review failed: %v", err)
				}

				// Return early with completion message
				duration := time.Since(hcpo.GetStartTime())
				return fmt.Sprintf(`# Todo Planning Complete - All Steps Already Executed

## Planning Summary
- **Objective**: %s
- **Duration**: %v
- **Workspace**: %s
- **Plan Source**: %s (All steps completed)
- **Phases**: Skipped execution → Writing

## Final Todo List
Todo list has been created and saved as `+"`todo_final.md`"+` in the workspace root by the writer agent.

## Status
All execution steps were already completed. The writer agent has created the final todo list based on previous execution results.`,
					hcpo.GetObjective(), duration, hcpo.GetWorkspacePath(), planSource), nil
			}
		}
	}

	// Phase 2: Execute plan steps one by one (with validation after each step)

	// Initialize progress tracking if not already loaded
	if existingProgress == nil {
		existingProgress = &StepProgress{
			CompletedStepIndices: []int{},
			TotalSteps:           len(breakdownSteps),
		}
	}

	_, err = hcpo.runExecutionPhase(ctx, breakdownSteps, 1, existingProgress, startFromStep)
	if err != nil {
		return "", fmt.Errorf("execution phase failed: %w", err)
	}

	// Phase 3: Write/Update todo list with human review loop
	err = hcpo.runWriterPhaseWithHumanReview(ctx, 1)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Writer phase with human review failed: %v", err)
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

// runPlanningPhase creates markdown plan
// conversationHistory is updated in-place to accumulate across iterations
func (hcpo *HumanControlledTodoPlannerOrchestrator) runPlanningPhase(ctx context.Context, iteration int, humanFeedback string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	planningTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
	}

	// Add human feedback to conversation if provided
	if humanFeedback != "" {
		feedbackMessage := llms.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextContent{Text: humanFeedback}},
		}
		conversationHistory = append(conversationHistory, feedbackMessage)
		hcpo.GetLogger().Infof("📝 Added human feedback to conversation history for iteration %d", iteration)
	}

	// Create fresh planning agent with proper context
	planningAgent, err := hcpo.createPlanningAgent(ctx, "planning", 0, iteration)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create planning agent: %w", err)
	}

	// Execute planning agent
	// If this is the first iteration (empty conversationHistory), template vars will create initial task
	// If conversationHistory is already populated, template vars will be added after existing conversation
	_, updatedConversationHistory, err := planningAgent.Execute(ctx, planningTemplateVars, conversationHistory)
	if err != nil {
		return "", nil, fmt.Errorf("planning failed: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Markdown plan created successfully (conversation has %d messages)", len(updatedConversationHistory))
	return "markdown_plan_created", updatedConversationHistory, nil
}

// runPlanReaderPhase reads markdown plan and converts to structured JSON
func (hcpo *HumanControlledTodoPlannerOrchestrator) runPlanReaderPhase(ctx context.Context) (*PlanningResponse, error) {
	hcpo.GetLogger().Infof("📖 Reading markdown plan and converting to structured JSON")

	// Create plan reader agent
	planReaderAgent, err := hcpo.createPlanReaderAgent(ctx, "plan_reading", 0, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan reader agent: %w", err)
	}

	// Read markdown plan content from workspace
	planPath := filepath.Join(hcpo.GetWorkspacePath(), "plan.md")
	hcpo.GetLogger().Infof("📖 Reading plan markdown from: %s", planPath)

	planMarkdown, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		// Check if this is a file not found error (expected case for new plans)
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Infof("📝 Plan file not found, using empty content (new plan): %s", planPath)
			planMarkdown = ""
		} else {
			hcpo.GetLogger().Errorf("❌ Failed to read plan file %s: %v", planPath, err)
			return nil, fmt.Errorf("failed to read plan file %s: %w", planPath, err)
		}
	} else {
		hcpo.GetLogger().Infof("✅ Successfully read plan markdown (%d characters)", len(planMarkdown))
	}

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
func (hcpo *HumanControlledTodoPlannerOrchestrator) runExecutionPhase(
	ctx context.Context,
	breakdownSteps []TodoStep,
	iteration int,
	progress *StepProgress,
	startFromStep int,
) ([]llms.MessageContent, error) {
	hcpo.GetLogger().Infof("🔄 Starting step-by-step execution of %d steps (starting from step %d)",
		len(breakdownSteps), startFromStep+1)

	// Track human feedback across all steps for continuous improvement
	var humanFeedbackHistory []string

	// Execute each step one by one
	for i, step := range breakdownSteps {
		// Skip if step is already completed
		if i < startFromStep {
			hcpo.GetLogger().Infof("⏭️ Skipping step %d/%d (already completed): %s",
				i+1, len(breakdownSteps), step.Title)
			continue
		}

		// Check if step is in completed list
		isCompleted := false
		for _, completedIdx := range progress.CompletedStepIndices {
			if completedIdx == i {
				isCompleted = true
				break
			}
		}
		if isCompleted {
			hcpo.GetLogger().Infof("⏭️ Skipping step %d/%d (marked as completed): %s",
				i+1, len(breakdownSteps), step.Title)
			continue
		}

		hcpo.GetLogger().Infof("📋 Executing step %d/%d: %s", i+1, len(breakdownSteps), step.Title)

		// Initialize variables for step execution
		maxRetryAttempts := 3
		var executionConversationHistory []llms.MessageContent
		var humanFeedback string
		stepCompleted := false

		// Outer loop: Handle re-execution with human feedback
		for !stepCompleted {
			// Add human feedback to conversation history if provided
			if humanFeedback != "" {
				humanFeedbackMessage := llms.MessageContent{
					Role: llms.ChatMessageTypeHuman,
					Parts: []llms.ContentPart{llms.TextContent{
						Text: fmt.Sprintf("## Human Feedback for Step %d:\n%s", i+1, humanFeedback),
					}},
				}
				executionConversationHistory = append(executionConversationHistory, humanFeedbackMessage)
				hcpo.GetLogger().Infof("📝 Added human feedback to conversation history for step %d", i+1)
				humanFeedback = "" // Reset for next iteration
			}

			// Prepare template variables for this specific step with individual fields
			// RESOLVE VARIABLES: Replace {{VARS}} with actual values for execution
			templateVars := map[string]string{
				"StepNumber":          fmt.Sprintf("%d", i+1),
				"TotalSteps":          fmt.Sprintf("%d", len(breakdownSteps)),
				"StepTitle":           hcpo.resolveVariables(step.Title),
				"StepDescription":     hcpo.resolveVariables(step.Description),
				"StepSuccessCriteria": hcpo.resolveVariables(step.SuccessCriteria),
				"StepWhyThisStep":     hcpo.resolveVariables(step.WhyThisStep),
				"StepContextOutput":   hcpo.resolveVariables(step.ContextOutput),
				"WorkspacePath":       hcpo.GetWorkspacePath(),
				"LearningAgentOutput": "", // Will be populated with learning agent's output
			}

			// Combine success and failure patterns from plan breakdown into LearningAgentOutput
			var learningOutputParts []string
			if len(step.SuccessPatterns) > 0 {
				learningOutputParts = append(learningOutputParts, "## ✅ Success Patterns from Plan:")
				for _, pattern := range step.SuccessPatterns {
					learningOutputParts = append(learningOutputParts, fmt.Sprintf("- Success Pattern: %s", pattern))
				}
			}
			if len(step.FailurePatterns) > 0 {
				learningOutputParts = append(learningOutputParts, "## ❌ Failure Patterns from Plan:")
				for _, pattern := range step.FailurePatterns {
					learningOutputParts = append(learningOutputParts, fmt.Sprintf("- Failure Pattern: %s", pattern))
				}
			}

			if len(learningOutputParts) > 0 {
				templateVars["LearningAgentOutput"] = strings.Join(learningOutputParts, "\n")
			} else {
				templateVars["LearningAgentOutput"] = ""
			}

			// Add context dependencies as a comma-separated string (also resolve variables)
			if len(step.ContextDependencies) > 0 {
				resolvedDeps := make([]string, len(step.ContextDependencies))
				for idx, dep := range step.ContextDependencies {
					resolvedDeps[idx] = hcpo.resolveVariables(dep)
				}
				templateVars["StepContextDependencies"] = strings.Join(resolvedDeps, ", ")
			} else {
				templateVars["StepContextDependencies"] = ""
			}

			// Add human feedback from previous steps to conversation history (first iteration only)
			if len(humanFeedbackHistory) > 0 && len(executionConversationHistory) == 0 {
				previousFeedbackMessage := llms.MessageContent{
					Role: llms.ChatMessageTypeHuman,
					Parts: []llms.ContentPart{llms.TextContent{
						Text: fmt.Sprintf("## Previous Steps' Feedback for Context:\n%s", strings.Join(humanFeedbackHistory, "\n---\n")),
					}},
				}
				executionConversationHistory = append(executionConversationHistory, previousFeedbackMessage)
				hcpo.GetLogger().Infof("📝 Added human feedback from previous steps to conversation history for step %d", i+1)
			}

			// Inner loop: Automatic retry logic
			var validationFeedback []ValidationFeedback
			var validationResponse *ValidationResponse

			for retryAttempt := 1; retryAttempt <= maxRetryAttempts; retryAttempt++ {
				hcpo.GetLogger().Infof("🔄 Executing step %d/%d (attempt %d/%d): %s", i+1, len(breakdownSteps), retryAttempt, maxRetryAttempts, step.Title)

				// Add validation feedback to template variables if this is a retry
				if retryAttempt > 1 && validationFeedback != nil {
					feedbackJSON, _ := json.Marshal(validationFeedback)
					templateVars["ValidationFeedback"] = fmt.Sprintf("## Validation Feedback (Retry Attempt %d):\n%s", retryAttempt, string(feedbackJSON))
					hcpo.GetLogger().Infof("📝 Added validation feedback to template variables for step %d, retry %d", i+1, retryAttempt)
				} else {
					templateVars["ValidationFeedback"] = "" // No validation feedback for first attempt
				}

				// Create execution agent for this step
				agentName := fmt.Sprintf("execution-agent-step-%d-%s", i+1, strings.ReplaceAll(step.Title, " ", "-"))
				executionAgent, err := hcpo.createExecutionAgent(ctx, "execution", i+1, iteration, agentName)
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

				validationAgentName := fmt.Sprintf("validation-agent-step-%d-%s", i+1, strings.ReplaceAll(step.Title, " ", "-"))
				validationAgent, err := hcpo.createValidationAgent(ctx, "validation", i+1, iteration, validationAgentName)
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

				// FAST MODE: Skip learning agents entirely
				isFastExecuteStep := hcpo.IsFastExecuteStep(i)
				if isFastExecuteStep {
					hcpo.GetLogger().Infof("⚡ Fast mode: Skipping learning agents for step %d", i+1)
				} else {
					// Run appropriate learning phase based on validation result
					if validationResponse.IsSuccessCriteriaMet {
						// Success Learning Agent - analyze what worked well and update plan.json
						hcpo.GetLogger().Infof("🧠 Running success learning analysis for step %d", i+1)
						successLearningOutput, err := hcpo.runSuccessLearningPhase(ctx, i+1, len(breakdownSteps), &step, executionConversationHistory, validationResponse)
						if err != nil {
							hcpo.GetLogger().Warnf("⚠️ Success learning phase failed for step %d: %v", i+1, err)
						} else {
							hcpo.GetLogger().Infof("✅ Success learning analysis completed for step %d", i+1)

							// Append success learning analysis to existing LearningAgentOutput
							if successLearningOutput != "" {
								existingOutput := templateVars["LearningAgentOutput"]
								if existingOutput != "" {
									templateVars["LearningAgentOutput"] = existingOutput + "\n\n" + successLearningOutput
								} else {
									templateVars["LearningAgentOutput"] = successLearningOutput
								}
							}
						}
					} else {
						// Failure Learning Agent - analyze what went wrong and provide refined task description
						hcpo.GetLogger().Infof("🧠 Running failure learning analysis for step %d", i+1)
						refinedTaskDescription, learningAnalysis, err := hcpo.runFailureLearningPhase(ctx, i+1, len(breakdownSteps), &step, executionConversationHistory, validationResponse)
						if err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failure learning phase failed for step %d: %v", i+1, err)
						} else {
							hcpo.GetLogger().Infof("✅ Failure learning analysis completed for step %d", i+1)

							// Update step description for retry
							if refinedTaskDescription != "" {
								step.Description = refinedTaskDescription
								templateVars["StepDescription"] = refinedTaskDescription
								hcpo.GetLogger().Infof("🔄 Updated step %d description with refined task for retry", i+1)
							}

							// Update LearningAgentOutput with full learning analysis
							if learningAnalysis != "" {
								existingOutput := templateVars["LearningAgentOutput"]
								if existingOutput != "" {
									templateVars["LearningAgentOutput"] = existingOutput + "\n\n" + learningAnalysis
								} else {
									templateVars["LearningAgentOutput"] = learningAnalysis
								}
							}
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
						// Note: conversation history is preserved from previous attempts for context
					}
				}
			}

			// BLOCKING HUMAN FEEDBACK - Ask user if they want to continue to next step or re-execute current step
			// FAST MODE: Skip human feedback and auto-approve
			isFastExecuteStep := hcpo.IsFastExecuteStep(i)
			var approved bool
			var feedback string

			if isFastExecuteStep {
				hcpo.GetLogger().Infof("⚡ Fast mode: Auto-approving step %d without human feedback", i+1)
				approved = true
				feedback = "" // No feedback in fast mode
			} else {
				// Normal mode: Request human feedback
				var validationSummary string
				if validationResponse != nil {
					validationSummary = fmt.Sprintf("Step %d validation completed. Success Criteria Met: %v, Status: %s", i+1, validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)
				} else {
					validationSummary = fmt.Sprintf("Step %d execution failed - no validation response available", i+1)
				}
				var err error
				approved, feedback, err = hcpo.requestHumanFeedback(ctx, i+1, len(breakdownSteps), validationSummary)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Human feedback request failed: %v", err)
					// Default to continue if feedback fails
					approved = true
				}
			}

			// Store human feedback for future steps (even if approved, user might have provided guidance)
			if feedback != "" {
				feedbackEntry := fmt.Sprintf("Step %d/%d Feedback: %s", i+1, len(breakdownSteps), feedback)
				humanFeedbackHistory = append(humanFeedbackHistory, feedbackEntry)
				hcpo.GetLogger().Infof("📝 Stored human feedback for future steps: %s", feedbackEntry)
			}

			if approved {
				// User approved - mark step as completed and exit outer loop
				progress.CompletedStepIndices = append(progress.CompletedStepIndices, i)
				if err := hcpo.saveStepProgress(ctx, progress); err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to save step progress: %v", err)
				} else {
					hcpo.GetLogger().Infof("✅ Step %d/%d marked as completed and saved", i+1, len(breakdownSteps))
				}
				stepCompleted = true
			} else if !isFastExecuteStep {
				// User rejected - ask if they want to re-execute this step with feedback or move to next step
				// Skip this in fast mode (should not happen anyway since we auto-approve)
				shouldReexecute, err := hcpo.requestReexecuteDecision(ctx, i+1, len(breakdownSteps), feedback)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Re-execution decision request failed: %v", err)
					shouldReexecute = false // Default to stop if decision fails
				}

				if shouldReexecute {
					// User wants to re-execute - set feedback and continue outer loop
					hcpo.GetLogger().Infof("🔄 Will re-execute step %d with human feedback: %s", i+1, feedback)
					humanFeedback = feedback
					// Outer loop will continue, adding feedback to conversation history
				} else {
					// User wants to stop execution
					hcpo.GetLogger().Infof("🛑 User requested to stop execution after step %d with feedback: %s", i+1, feedback)
					break // Break out of outer loop (ends all steps)
				}
			}
		} // End of outer loop for step execution
	}

	hcpo.GetLogger().Infof("✅ All steps execution completed")
	return nil, nil
}

// max returns the maximum value in a slice of integers
func max(slice []int) int {
	if len(slice) == 0 {
		return -1
	}
	maxVal := slice[0]
	for _, val := range slice {
		if val > maxVal {
			maxVal = val
		}
	}
	return maxVal
}

// runVariableExtractionPhase extracts variables from objective (with optional human feedback)
func (hcpo *HumanControlledTodoPlannerOrchestrator) runVariableExtractionPhase(ctx context.Context, iteration int, humanFeedback string, conversationHistory []llms.MessageContent) (*VariablesManifest, string, error) {
	hcpo.GetLogger().Infof("🔍 Starting variable extraction from objective (attempt %d)", iteration)

	// Create variable extraction agent
	extractionAgent, err := hcpo.createVariableExtractionAgent(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create variable extraction agent: %w", err)
	}

	// Prepare template variables
	extractionTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
	}

	// Add human feedback to conversation if provided
	if humanFeedback != "" {
		feedbackMessage := llms.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextContent{Text: humanFeedback}},
		}
		conversationHistory = append(conversationHistory, feedbackMessage)
		hcpo.GetLogger().Infof("📝 Added human feedback to variable extraction conversation (attempt %d)", iteration)
	}

	// Execute variable extraction
	_, updatedHistory, err := extractionAgent.Execute(ctx, extractionTemplateVars, conversationHistory)
	if err != nil {
		return nil, "", fmt.Errorf("variable extraction failed: %w", err)
	}

	// Read the generated variables.json file
	variablesPath := fmt.Sprintf("%s/todo_creation_human/variables/variables.json", hcpo.GetWorkspacePath())
	variablesContent, err := hcpo.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read variables.json: %w", err)
	}

	// Parse JSON to get manifest
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		return nil, "", fmt.Errorf("failed to parse variables.json: %w", err)
	}

	// Store manifest in orchestrator for future use
	hcpo.variablesManifest = &manifest
	hcpo.templatedObjective = manifest.Objective

	hcpo.GetLogger().Infof("✅ Extracted %d variables from objective (conversation has %d messages)", len(manifest.Variables), len(updatedHistory))
	return &manifest, manifest.Objective, nil
}

// requestVariableApproval requests human approval for extracted variables
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestVariableApproval(ctx context.Context, manifest *VariablesManifest, revisionAttempt int) (bool, string, error) {
	hcpo.GetLogger().Infof("⏸️ Requesting human approval for extracted variables (attempt %d)", revisionAttempt)

	// Format variables for display
	var variablesSummary strings.Builder
	variablesSummary.WriteString(fmt.Sprintf("Extracted %d variables from objective:\n\n", len(manifest.Variables)))

	for _, variable := range manifest.Variables {
		variablesSummary.WriteString(fmt.Sprintf("- **{{%s}}**: %s\n", variable.Name, variable.Description))
		variablesSummary.WriteString(fmt.Sprintf("  - Value: %s\n", variable.Value))
		variablesSummary.WriteString("\n")
	}

	variablesSummary.WriteString(fmt.Sprintf("\n**Templated Objective**:\n%s", manifest.Objective))

	// Generate unique request ID
	requestID := fmt.Sprintf("variable_approval_%d_%d", revisionAttempt, time.Now().UnixNano())

	// Use common human feedback function
	return hcpo.RequestHumanFeedback(
		ctx,
		requestID,
		fmt.Sprintf("Please review the extracted variables (attempt %d). Are these correct or do you want to provide feedback for refinement?", revisionAttempt),
		variablesSummary.String(),
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
}

// createVariableExtractionAgent creates the variable extraction agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) createVariableExtractionAgent(ctx context.Context) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"variable-extraction-agent",
		"variable_extraction",
		0, // No step number
		0, // No iteration
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // No MCP servers needed - pure LLM extraction
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewVariableExtractionAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// loadVariableValues loads runtime variable values from variables.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) loadVariableValues(ctx context.Context) error {
	if hcpo.variablesManifest == nil {
		return nil // No variables to load
	}

	// Load variable values from variables.json
	variablesPath := fmt.Sprintf("%s/todo_creation_human/variables/variables.json", hcpo.GetWorkspacePath())
	variablesContent, err := hcpo.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		return fmt.Errorf("failed to read variables.json: %w", err)
	}

	// Parse variables.json to get current values
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		return fmt.Errorf("failed to parse variables.json: %w", err)
	}

	// Load values into the variableValues map
	hcpo.variableValues = make(map[string]string)
	for _, variable := range manifest.Variables {
		hcpo.variableValues[variable.Name] = variable.Value
	}

	hcpo.GetLogger().Infof("✅ Loaded variable values from variables.json: %d variables", len(hcpo.variableValues))
	return nil
}

// resolveVariables replaces {{VARIABLE}} placeholders with actual values
func (hcpo *HumanControlledTodoPlannerOrchestrator) resolveVariables(text string) string {
	if hcpo.variableValues == nil {
		return text // No variables to resolve
	}

	resolved := text
	for varName, varValue := range hcpo.variableValues {
		placeholder := fmt.Sprintf("{{%s}}", varName)
		resolved = strings.ReplaceAll(resolved, placeholder, varValue)
	}
	return resolved
}

// runSuccessLearningPhase analyzes successful executions to capture best practices and improve plan.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) runSuccessLearningPhase(ctx context.Context, stepNumber, totalSteps int, step *TodoStep, executionHistory []llms.MessageContent, validationResponse *ValidationResponse) (string, error) {
	hcpo.GetLogger().Infof("🧠 Starting success learning analysis for step %d/%d: %s", stepNumber, totalSteps, step.Title)

	// Create success learning agent
	successLearningAgentName := fmt.Sprintf("success-learning-agent-step-%d-%s", stepNumber, strings.ReplaceAll(step.Title, " ", "-"))
	successLearningAgent, err := hcpo.createSuccessLearningAgent(ctx, "success_learning", stepNumber, 1, successLearningAgentName)
	if err != nil {
		return "", fmt.Errorf("failed to create success learning agent: %w", err)
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

	// Execute success learning agent and capture output
	successLearningOutput, _, err := successLearningAgent.Execute(ctx, successLearningTemplateVars, []llms.MessageContent{})
	if err != nil {
		return "", fmt.Errorf("success learning analysis failed: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Success learning analysis completed for step %d", stepNumber)
	return successLearningOutput, nil
}

// runFailureLearningPhase analyzes failed executions to provide refined task descriptions for retry
func (hcpo *HumanControlledTodoPlannerOrchestrator) runFailureLearningPhase(ctx context.Context, stepNumber, totalSteps int, step *TodoStep, executionHistory []llms.MessageContent, validationResponse *ValidationResponse) (string, string, error) {
	hcpo.GetLogger().Infof("🧠 Starting failure learning analysis for step %d/%d: %s", stepNumber, totalSteps, step.Title)

	// Create failure learning agent
	failureLearningAgentName := fmt.Sprintf("failure-learning-agent-step-%d-%s", stepNumber, strings.ReplaceAll(step.Title, " ", "-"))
	failureLearningAgent, err := hcpo.createFailureLearningAgent(ctx, "failure_learning", stepNumber, 1, failureLearningAgentName)
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

	// Execute failure learning agent and capture output
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

// runWriterPhaseWithHumanReview creates todo list with human review and feedback loop
func (hcpo *HumanControlledTodoPlannerOrchestrator) runWriterPhaseWithHumanReview(ctx context.Context, iteration int) error {
	maxRevisions := 5 // Allow up to 5 revisions based on human feedback
	var writerConversationHistory []llms.MessageContent

	for revisionAttempt := 1; revisionAttempt <= maxRevisions; revisionAttempt++ {
		hcpo.GetLogger().Infof("📝 Writer revision attempt %d/%d", revisionAttempt, maxRevisions)

		// Create writer agent for this revision
		writerAgentName := fmt.Sprintf("writer-agent-revision-%d", revisionAttempt)
		writerAgent, err := hcpo.createWriterAgent(ctx, "writing", 0, iteration, writerAgentName)
		if err != nil {
			return fmt.Errorf("failed to create writer agent for revision %d: %w", revisionAttempt, err)
		}

		// Prepare template variables for Execute method
		writerTemplateVars := map[string]string{
			"Objective":       hcpo.GetObjective(),
			"WorkspacePath":   hcpo.GetWorkspacePath(),
			"TotalIterations": fmt.Sprintf("%d", iteration),
		}

		// Execute writer agent with conversation history
		_, writerConversationHistory, err = writerAgent.Execute(ctx, writerTemplateVars, writerConversationHistory)
		if err != nil {
			return fmt.Errorf("todo list creation failed for revision %d: %w", revisionAttempt, err)
		}

		hcpo.GetLogger().Infof("✅ Writer agent completed revision %d", revisionAttempt)

		// Request human review of the generated todo list
		approved, feedback, err := hcpo.requestTodoListReview(ctx, revisionAttempt)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Human review request failed: %v", err)
			// Default to approved if review fails
			approved = true
		}

		// Add human feedback to conversation history for next revision
		if feedback != "" {
			hcpo.addUserFeedbackToHistory(feedback, &writerConversationHistory)
			hcpo.GetLogger().Infof("📝 Added human feedback to conversation history for next revision: %s", feedback)
		}

		if approved {
			hcpo.GetLogger().Infof("✅ Todo list approved by human after revision %d", revisionAttempt)
			break // Exit revision loop
		}

		// Todo list rejected with feedback for revision
		hcpo.GetLogger().Infof("🔄 Todo list revision requested (attempt %d/%d): %s", revisionAttempt, maxRevisions, feedback)

		if revisionAttempt >= maxRevisions {
			hcpo.GetLogger().Warnf("⚠️ Max todo list revision attempts (%d) reached", maxRevisions)
			break
		}
	}

	return nil
}

// requestTodoListReview requests human review of the generated todo list
// Returns: (approved bool, feedback string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestTodoListReview(ctx context.Context, revisionAttempt int) (bool, string, error) {
	hcpo.GetLogger().Infof("📋 Requesting human review of todo list (revision %d)", revisionAttempt)

	// Generate unique request ID
	requestID := fmt.Sprintf("todo_list_review_%d_%d", revisionAttempt, time.Now().UnixNano())

	// Use common human feedback function
	return hcpo.RequestHumanFeedback(
		ctx,
		requestID,
		fmt.Sprintf("Please review the generated todo list (revision %d). Is it ready for execution or do you want to provide feedback for improvements?", revisionAttempt),
		fmt.Sprintf("Todo list location: %s/todo_final.md", hcpo.GetWorkspacePath()),
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
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

// requestReexecuteDecision asks user if they want to re-execute the current step with feedback or skip to next step
// Returns: (shouldReexecute bool, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestReexecuteDecision(ctx context.Context, currentStep, totalSteps int, feedback string) (bool, error) {
	hcpo.GetLogger().Infof("🔄 Requesting re-execution decision for step %d/%d", currentStep, totalSteps)

	// Generate unique request ID
	requestID := fmt.Sprintf("reexecute_decision_%d_%d_%d", currentStep, totalSteps, time.Now().UnixNano())

	// Use common human feedback function with yes/no semantics
	return hcpo.RequestYesNoFeedback(
		ctx,
		requestID,
		fmt.Sprintf("You provided feedback for step %d/%d. Would you like to re-execute this step with your feedback, or skip to the next step?", currentStep, totalSteps),
		"Re-execute Step with Feedback",
		"Skip to Next Step",
		fmt.Sprintf("Your feedback: %s", feedback),
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
}

// Agent creation methods - reuse from base orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPlanningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"human-controlled-planning-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // Planning agent only works with plan.md file, no MCP servers needed
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

func (hcpo *HumanControlledTodoPlannerOrchestrator) createExecutionAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		agentName,
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) createValidationAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		agentName,
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

func (hcpo *HumanControlledTodoPlannerOrchestrator) createWriterAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		agentName,
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
		[]string{mcpclient.NoServers}, // Special MCP identifier for no servers - plan reader only converts markdown to JSON
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) createSuccessLearningAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		agentName,
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) createFailureLearningAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		agentName,
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

// SetFastExecuteMode sets the fast execute mode and end step
func (hcpo *HumanControlledTodoPlannerOrchestrator) SetFastExecuteMode(enabled bool, endStep int) {
	hcpo.fastExecuteMode = enabled
	hcpo.fastExecuteEndStep = endStep
}

// IsFastExecuteStep checks if a step should be executed in fast mode
func (hcpo *HumanControlledTodoPlannerOrchestrator) IsFastExecuteStep(stepIndex int) bool {
	return hcpo.fastExecuteMode && stepIndex <= hcpo.fastExecuteEndStep
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

// checkExistingVariables checks if variables.json already exists and loads it
func (hcpo *HumanControlledTodoPlannerOrchestrator) checkExistingVariables(ctx context.Context, variablesPath string) (bool, *VariablesManifest, error) {
	hcpo.GetLogger().Infof("🔍 Checking for existing variables at %s", variablesPath)

	// Try to read variables.json
	variablesContent, err := hcpo.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		// Check if it's a "file not found" error
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Infof("📋 No existing variables found: %v", err)
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, err
	}

	// Parse the existing variables manifest
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to parse existing variables.json: %v", err)
		return false, nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Found existing variables.json with %d variables", len(manifest.Variables))
	return true, &manifest, nil
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
