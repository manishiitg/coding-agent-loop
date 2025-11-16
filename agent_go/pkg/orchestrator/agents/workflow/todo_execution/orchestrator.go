package todo_execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/shared"
	todo_creation_human "mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human"

	"mcp-agent/agent_go/internal/llmtypes"
)

// FlexibleContextOutput handles both string and array formats for context_output
type FlexibleContextOutput string

// UnmarshalJSON implements custom unmarshaling for FlexibleContextOutput
// Handles both string and array formats to prevent parsing errors
func (f *FlexibleContextOutput) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*f = FlexibleContextOutput(str)
		return nil
	}

	// Try to unmarshal as array
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		// Convert array to comma-separated string
		*f = FlexibleContextOutput(strings.Join(arr, ", "))
		return nil
	}

	return json.Unmarshal(data, f)
}

// Use types from todo_creation_human package
// PlanStep and PlanningResponse are from planning_agent.go
// TodoStep is from controller.go

// TodoStepsExtractedEvent represents todo steps extracted from a plan
type TodoStepsExtractedEvent struct {
	events.BaseEventData
	TotalStepsExtracted int                            `json:"total_steps_extracted"`
	ExtractedSteps      []todo_creation_human.TodoStep `json:"extracted_steps"`
	ExtractionMethod    string                         `json:"extraction_method"`
	PlanSource          string                         `json:"plan_source"`
	RunFolder           string                         `json:"run_folder,omitempty"`     // Run folder name for run-specific configs
	WorkspacePath       string                         `json:"workspace_path,omitempty"` // Workspace path for config file operations
}

// GetEventType implements events.EventData interface
func (e *TodoStepsExtractedEvent) GetEventType() events.EventType {
	return events.TodoStepsExtracted
}

// Use types from todo_creation_human package
// ValidationResponse and ValidationFeedback are from validation_agent.go
// VariablesManifest and Variable are from variable_extraction_agent.go
// EnhancedPlanWithMetadata and LearningFileInfo are from controller.go

// TodoExecutionOrchestrator manages the multi-agent todo execution process
type TodoExecutionOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator
	// Variable management
	variablesManifest *todo_creation_human.VariablesManifest // Extracted variables
	variableValues    map[string]string                      // Runtime variable values
}

// NewTodoExecutionOrchestrator creates a new multi-agent todo execution orchestrator
func NewTodoExecutionOrchestrator(
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
	_ observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
) (*TodoExecutionOrchestrator, error) {

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
		llmConfig,     // llmConfig passed from caller
		maxTurns,
		customTools,
		customToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	return &TodoExecutionOrchestrator{
		BaseOrchestrator: baseOrchestrator,
	}, nil
}

// ExecuteTodos orchestrates the multi-agent todo execution process
func (teo *TodoExecutionOrchestrator) ExecuteTodos(ctx context.Context, objective, workspacePath, runOption string) (string, error) {
	teo.GetLogger().Infof("🚀 Starting multi-agent todo execution for objective: %s", objective)

	// Set objective and workspace path directly
	teo.SetObjective(objective)

	// Resolve selected run folder based on run option (in Go code, not in prompts)
	selectedRunFolder, err := teo.resolveSelectedRunFolder(ctx, workspacePath, runOption)
	if err != nil {
		return "", fmt.Errorf("failed to resolve run folder: %w", err)
	}
	teo.GetLogger().Infof("📁 Selected run folder: %s", selectedRunFolder)

	// Set workspace path to include the run folder
	runWorkspacePath := filepath.Join(workspacePath, "runs", selectedRunFolder)
	teo.SetWorkspacePath(runWorkspacePath)

	// Load variables from variables.json if it exists (optional - variables may not exist)
	// First check runs folder, then fallback to workspace root
	variableValues, err := todo_creation_human.LoadVariableValues(ctx, teo.BaseOrchestrator, workspacePath, runWorkspacePath)
	if err != nil {
		teo.GetLogger().Infof("⚠️ Could not load variables (this is optional): %v", err)
		// Continue execution even if variables don't exist
	} else {
		teo.variableValues = variableValues
		// If variables were loaded, request user review/editing
		if teo.variablesManifest != nil && len(teo.variablesManifest.Variables) > 0 {
			updated, err := teo.requestVariableReview(ctx, runWorkspacePath)
			if err != nil {
				return "", fmt.Errorf("failed to review variables: %w", err)
			}
			if updated {
				teo.GetLogger().Infof("✅ Variables were updated by user")
			}
		}
	}

	// Read plan_learnings.json from todo_creation_human/planning directory
	teo.GetLogger().Infof("📖 Reading plan_learnings.json from todo_creation_human/planning")
	planLearningsPath := filepath.Join(workspacePath, "todo_creation_human", "planning", "plan_learnings.json")
	planContent, err := teo.ReadWorkspaceFile(ctx, planLearningsPath)
	if err != nil {
		return "", fmt.Errorf("failed to read plan_learnings.json: %w", err)
	}

	// Parse EnhancedPlanWithMetadata structure
	var enhancedPlanMetadata todo_creation_human.EnhancedPlanWithMetadata
	if err := json.Unmarshal([]byte(planContent), &enhancedPlanMetadata); err != nil {
		return "", fmt.Errorf("failed to parse plan_learnings.json: %w", err)
	}

	// Extract PlanningResponse from EnhancedPlanWithMetadata
	if enhancedPlanMetadata.Plan == nil {
		return "", fmt.Errorf("plan_learnings.json contains no plan data")
	}
	planningResponse := *enhancedPlanMetadata.Plan // This is *todo_creation_human.PlanningResponse

	// Read step configs from step_config.json (if exists)
	// Check run folder first, then fallback to workspace default
	stepConfigs, err := todo_creation_human.ReadStepConfigs(ctx, teo.BaseOrchestrator, workspacePath, runWorkspacePath)
	if err != nil {
		teo.GetLogger().Warnf("⚠️ Failed to read step_config.json: %v (using defaults for all steps)", err)
		stepConfigs = &todo_creation_human.StepConfigFile{Steps: []todo_creation_human.StepConfig{}}
	}

	// Match configs by step index (0-based)
	matchedConfigs := todo_creation_human.MatchStepConfigs(planningResponse.Steps, stepConfigs)
	teo.GetLogger().Infof("📋 Matched %d/%d step configs from step_config.json", len(matchedConfigs), len(planningResponse.Steps))

	// Convert PlanningResponse.Steps to TodoStep array
	steps := make([]todo_creation_human.TodoStep, len(planningResponse.Steps))
	for i, step := range planningResponse.Steps {
		// Set default max_iterations if not provided and has_loop is true
		maxIterations := step.MaxIterations
		if step.HasLoop && maxIterations == 0 {
			maxIterations = 10 // Default max iterations
		}

		// Get matched config for this step (may be nil if no match)
		var agentConfigs *todo_creation_human.AgentConfigs
		if config, found := matchedConfigs[i]; found {
			agentConfigs = config
		}

		// Resolve variables in step fields
		steps[i] = todo_creation_human.TodoStep{
			Title:                    todo_creation_human.ResolveVariables(step.Title, teo.variableValues),
			Description:              todo_creation_human.ResolveVariables(step.Description, teo.variableValues),
			SuccessCriteria:          todo_creation_human.ResolveVariables(step.SuccessCriteria, teo.variableValues),
			ContextDependencies:      todo_creation_human.ResolveVariablesArray(step.ContextDependencies, teo.variableValues),
			ContextOutput:            todo_creation_human.ResolveVariables(string(step.ContextOutput), teo.variableValues), // Convert FlexibleContextOutput to string
			LearningFilesToReference: step.LearningFilesToReference,
			HasLoop:                  step.HasLoop,
			LoopCondition:            todo_creation_human.ResolveVariables(step.LoopCondition, teo.variableValues),
			MaxIterations:            maxIterations,
			LoopDescription:          todo_creation_human.ResolveVariables(step.LoopDescription, teo.variableValues),
			AgentConfigs:             agentConfigs, // Merged from step_config.json
		}
	}

	teo.GetLogger().Infof("📋 Parsed %d steps from plan_learnings.json", len(steps))

	// Emit todo steps extracted event (so frontend can display the extracted steps)
	// Use the original workspacePath (not runWorkspacePath) so frontend can find todo_creation_human folder
	// Set workspace_path to include todo_creation_human subdirectory for config file access
	workspacePathWithSubdir := filepath.Join(workspacePath, "todo_creation_human")
	todo_creation_human.EmitTodoStepsExtractedEvent(ctx, teo.BaseOrchestrator, steps, "plan_learnings_json", "direct_json", selectedRunFolder, workspacePathWithSubdir)

	// Request human approval for the extracted steps
	approved, feedback, err := teo.requestStepsApproval(ctx, steps, 1)
	if err != nil {
		return "", fmt.Errorf("failed to get approval for extracted steps: %w", err)
	}

	if !approved {
		teo.GetLogger().Infof("⚠️ User did not approve steps. Feedback: %s", feedback)
		return fmt.Sprintf("Steps not approved. Feedback: %s", feedback), nil
	}

	teo.GetLogger().Infof("✅ Steps approved by user, proceeding with execution")

	// Execute each step individually with validation feedback loop
	// For loop steps, iterate until condition is met or max iterations reached

	for i, step := range steps {
		teo.GetLogger().Infof("🔄 Executing step %d/%d: %s", i+1, len(steps), step.Title)

		// Initialize loop state for loop steps
		var loopConditionMet bool
		var loopIterationCount int
		var previousIterationOutput string

		// Main execution loop (either single execution or loop iterations)
		// For non-loop steps, this executes once. For loop steps, it iterates until condition is met.
		for loopIteration := 0; ; loopIteration++ {
			// Initialize loop state on first iteration
			if loopIteration == 0 && step.HasLoop {
				loopConditionMet = false
				loopIterationCount = 0
				previousIterationOutput = ""
				teo.GetLogger().Infof("🔄 Step %d loop starting (max iterations: %d, condition: %s)", i+1, step.MaxIterations, step.LoopCondition)
			}

			// Check loop exit conditions (only for loop steps) - before starting iteration
			if step.HasLoop {
				if loopConditionMet {
					teo.GetLogger().Infof("✅ Step %d loop condition met after %d iterations, exiting loop", i+1, loopIterationCount)
					break // Exit main loop - proceed to next step
				}
				if loopIterationCount >= step.MaxIterations {
					teo.GetLogger().Errorf("❌ Step %d reached max iterations (%d) without meeting loop condition", i+1, step.MaxIterations)
					break // Exit main loop - proceed to next step
				}
				// Increment iteration count at start of iteration
				loopIterationCount++
				if loopIterationCount > 1 {
					teo.GetLogger().Infof("🔄 Step %d loop iteration %d/%d starting", i+1, loopIterationCount, step.MaxIterations)
				}
			}

			var executionResult string
			var validationResult string
			maxAttempts := 3
			attempt := 1

			// Retry loop for execution and validation
			for attempt <= maxAttempts {
				teo.GetLogger().Infof("🔄 Attempt %d/%d for step %d", attempt, maxAttempts, i+1)
				if step.HasLoop {
					teo.GetLogger().Infof("   (Loop iteration %d/%d)", loopIterationCount, step.MaxIterations)
				}

				// Execute this specific step
				// For loop steps, use current iteration count (already incremented)
				// For non-loop steps, use 0
				currentIteration := loopIterationCount
				if !step.HasLoop {
					currentIteration = 0
				}

				var err error
				var conversationHistory []llmtypes.MessageContent
				executionResult, conversationHistory, err = teo.runStepExecutionPhase(ctx, step, i+1, len(steps), selectedRunFolder, runOption, validationResult, previousIterationOutput, currentIteration, step.MaxIterations)
				if err != nil {
					teo.GetLogger().Warnf("⚠️ Step %d execution failed (attempt %d): %v", i+1, attempt, err)
					executionResult = fmt.Sprintf("Step %d execution failed (attempt %d): %v", i+1, attempt, err)
					conversationHistory = nil
				}

				// Validate this specific step
				validationResponse, err := teo.runStepValidationPhase(ctx, step, i+1, len(steps), executionResult, conversationHistory)
				if err != nil {
					teo.GetLogger().Warnf("⚠️ Step %d validation failed (attempt %d): %v", i+1, attempt, err)
					break
				}

				// For loop steps, check loop condition instead of full validation
				if step.HasLoop {
					if validationResponse.LoopConditionMet {
						teo.GetLogger().Infof("✅ Step %d loop condition met on iteration %d: %s", i+1, loopIterationCount, validationResponse.LoopReasoning)
						loopConditionMet = true
						break // Exit retry loop - loop condition met
					} else {
						// Format feedback for logging
						feedbackStr := formatValidationFeedback(validationResponse.Feedback)
						if validationResponse.LoopReasoning != "" {
							feedbackStr = validationResponse.LoopReasoning + "\n" + feedbackStr
						}
						teo.GetLogger().Infof("⚠️ Step %d loop condition not met (iteration %d): %s", i+1, loopIterationCount, feedbackStr)
						previousIterationOutput = executionResult
						// Note: loopIterationCount is incremented at the start of next iteration
						break // Exit retry loop - continue to next loop iteration
					}
				} else {
					// For non-loop steps, check if validation passed
					// Format feedback array as string for logging
					feedbackStr := formatValidationFeedback(validationResponse.Feedback)
					if validationResponse.Reasoning != "" {
						feedbackStr = validationResponse.Reasoning + "\n" + feedbackStr
					}

					if validationResponse.IsSuccessCriteriaMet {
						teo.GetLogger().Infof("✅ Step %d completed successfully on attempt %d: %s", i+1, attempt, feedbackStr)
						break // Exit retry loop - step completed
					} else {
						teo.GetLogger().Infof("⚠️ Step %d validation failed on attempt %d: %s", i+1, attempt, feedbackStr)
						validationResult = feedbackStr

						if attempt < maxAttempts {
							teo.GetLogger().Infof("🔄 Retrying step %d with feedback: %s", i+1, feedbackStr)
						} else {
							teo.GetLogger().Warnf("❌ Step %d failed after %d attempts. Final feedback: %s", i+1, maxAttempts, feedbackStr)
						}
					}
				}

				attempt++
			}

			// If in loop mode and condition not met, continue main loop
			if step.HasLoop && !loopConditionMet {
				if loopIterationCount >= step.MaxIterations {
					break // Exit main loop - max iterations reached
				}
				continue // Continue main loop for next iteration
			}

			// Exit main loop if not in loop mode or loop condition met
			if !step.HasLoop {
				break // Exit main execution loop
			}
			if loopConditionMet {
				break // Exit main execution loop
			}
		}

		// Results are logged and used for validation within the loop; no aggregation needed
	}

	// Note: Learning integration removed - execution agent now auto-discovers learning files and scripts

	duration := time.Since(teo.GetStartTime())
	teo.GetLogger().Infof("✅ Multi-agent todo execution completed in %v", duration)

	return "Execution Completed", nil
}

// runStepExecutionPhase executes a single step using the execution agent
func (teo *TodoExecutionOrchestrator) runStepExecutionPhase(ctx context.Context, step todo_creation_human.TodoStep, stepNumber, totalSteps int, selectedRunFolder, runOption, previousFeedback, previousIterationOutput string, currentIteration, maxIterations int) (string, []llmtypes.MessageContent, error) {
	executionAgent, err := teo.createExecutionAgent(ctx, step.Title, stepNumber, 0)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create execution agent: %w", err)
	}

	// LearningAgentOutput is now empty - execution agent auto-discovers learning files and scripts

	// Prepare template variables for this specific step
	// Map to format expected by HumanControlledTodoPlannerExecutionAgent
	// Execution agent workspace path is the execution subdirectory (folder guard validates against this)
	executionWorkspacePath := filepath.Join(teo.GetWorkspacePath(), "execution")
	templateVars := map[string]string{
		"StepTitle":               step.Title,
		"StepDescription":         step.Description,
		"StepSuccessCriteria":     step.SuccessCriteria,
		"StepContextDependencies": strings.Join(step.ContextDependencies, ", "),
		"StepContextOutput":       step.ContextOutput,
		"WorkspacePath":           executionWorkspacePath, // Execution subdirectory (folder guard validates against this)
		"ValidationFeedback":      previousFeedback,       // Map PreviousFeedback to ValidationFeedback
		"PreviousIterationOutput": previousIterationOutput,
		"LearningAgentOutput":     "", // Execution agent auto-discovers learning files and scripts
		"HasLoop":                 fmt.Sprintf("%v", step.HasLoop),
		"LoopCondition":           step.LoopCondition,
		"LoopDescription":         step.LoopDescription,
		"CurrentIteration":        fmt.Sprintf("%d", currentIteration),
		"MaxIterations":           fmt.Sprintf("%d", maxIterations),
	}

	// Add variable names and values if variables exist
	if variableNames := todo_creation_human.FormatVariableNames(teo.variablesManifest); variableNames != "" {
		templateVars["VariableNames"] = variableNames
	}
	if variableValues := todo_creation_human.FormatVariableValues(teo.variablesManifest, teo.variableValues); variableValues != "" {
		templateVars["VariableValues"] = variableValues
	}

	executionResult, conversationHistory, err := executionAgent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", nil, fmt.Errorf("step %d execution failed: %w", stepNumber, err)
	}

	// Store execution result with conversation history
	// Format: "EXECUTION_RESULT:|<result>|CONVERSATION_HISTORY:|<history_json>"
	// This allows validation agent to access both the result and the full conversation
	return executionResult, conversationHistory, nil
}

// runStepValidationPhase validates a single step's execution using the validation agent
func (teo *TodoExecutionOrchestrator) runStepValidationPhase(ctx context.Context, step todo_creation_human.TodoStep, stepNumber, totalSteps int, executionResult string, conversationHistory []llmtypes.MessageContent) (*todo_creation_human.ValidationResponse, error) {
	validationAgent, err := teo.createValidationAgent(ctx, step.Title, stepNumber, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create validation agent: %w", err)
	}

	// Cast to HumanControlledTodoPlannerValidationAgent to access ExecuteStructured method
	todoValidationAgent, ok := validationAgent.(*todo_creation_human.HumanControlledTodoPlannerValidationAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast validation agent to HumanControlledTodoPlannerValidationAgent")
	}

	// Format conversation history as string for template variable
	conversationHistoryStr := shared.FormatConversationHistory(conversationHistory)

	// Prepare template variables for this specific step
	// Map to format expected by HumanControlledTodoPlannerValidationAgent
	templateVars := map[string]string{
		"StepTitle":               step.Title,
		"StepDescription":         step.Description,
		"StepSuccessCriteria":     step.SuccessCriteria,
		"StepContextDependencies": strings.Join(step.ContextDependencies, ", "),
		"StepContextOutput":       step.ContextOutput,
		"WorkspacePath":           teo.GetWorkspacePath(), // This now includes runs/{folder}
		"ExecutionHistory":        conversationHistoryStr, // Map ExecutionOutput to ExecutionHistory
		"LoopCondition":           step.LoopCondition,     // For loop steps: condition to check
	}

	validationResponse, _, err := todoValidationAgent.ExecuteStructured(ctx, templateVars, conversationHistory)
	if err != nil {
		return nil, fmt.Errorf("step %d validation failed: %w", stepNumber, err)
	}

	// Return validation response directly (using types from todo_creation_human)
	return validationResponse, nil
}

// formatStepResults removed; returning simple completion message instead

// Agent creation methods
func (teo *TodoExecutionOrchestrator) createExecutionAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from learnings (read-only) and execution (via writePaths), writes only to execution
	baseWorkspacePath := teo.GetWorkspacePath()
	executionWorkspacePath := filepath.Join(baseWorkspacePath, "execution")
	learningsPath := filepath.Join(baseWorkspacePath, "learnings")

	// Only specify learnings in readPaths - execution is automatically readable since it's in writePaths
	readPaths := []string{learningsPath}
	writePaths := []string{executionWorkspacePath}
	teo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	teo.GetLogger().Infof("🔒 Setting folder guard - Read paths: %v, Write paths: %v (execution automatically readable via writePaths)", readPaths, writePaths)

	// Reuse the execution agent from todo_creation_human
	agent, err := teo.CreateAndSetupStandardAgent(
		ctx,
		"todo_execution",
		phase,
		step,
		iteration,
		teo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return todo_creation_human.NewHumanControlledTodoPlannerExecutionAgent(config, logger, tracer, eventBridge)
		},
		teo.WorkspaceTools,
		teo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (teo *TodoExecutionOrchestrator) createValidationAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from execution (read-only) and validation (via writePaths), writes only to validation
	baseWorkspacePath := teo.GetWorkspacePath()
	executionPath := filepath.Join(baseWorkspacePath, "execution")
	validationPath := filepath.Join(baseWorkspacePath, "validation")

	// Only specify execution in readPaths - validation is automatically readable since it's in writePaths
	readPaths := []string{executionPath}
	writePaths := []string{validationPath}
	teo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	teo.GetLogger().Infof("🔒 Setting folder guard for validation agent - Read paths: %v, Write paths: %v (validation automatically readable via writePaths)", readPaths, writePaths)

	// Reuse the validation agent from todo_creation_human
	agent, err := teo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"validation-agent",
		phase,
		step,
		iteration,
		teo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // No MCP servers - validation agent only uses workspace tools to read/write files
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return todo_creation_human.NewHumanControlledTodoPlannerValidationAgent(config, logger, tracer, eventBridge)
		},
		teo.WorkspaceTools,
		teo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// Execute implements the Orchestrator interface
func (teo *TodoExecutionOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	// Validate workspace path is provided
	if workspacePath == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	// Extract run option from options
	runOption := "create_new_runs_always" // default
	if ro, ok := options["RunOption"].(string); ok && ro != "" {
		runOption = ro
	}

	// Call the existing ExecuteTodos method
	return teo.ExecuteTodos(ctx, objective, workspacePath, runOption)
}

// GetType returns the orchestrator type
func (teo *TodoExecutionOrchestrator) GetType() string {
	return "todo_execution"
}

// resolveSelectedRunFolder determines which run folder to use based on the run option
func (teo *TodoExecutionOrchestrator) resolveSelectedRunFolder(ctx context.Context, workspacePath, runOption string) (string, error) {
	runsPath := filepath.Join(workspacePath, "runs")

	// Get current date for dated folders
	today := time.Now().Format("2006-01-02")

	switch runOption {
	case "use_same_run":
		// Check if runs directory exists
		exists, _ := teo.workspaceFileExists(ctx, runsPath)
		if !exists {
			// Create initial run folder
			selectedFolder := "initial"
			if err := teo.createRunFolderStructure(ctx, filepath.Join(runsPath, selectedFolder)); err != nil {
				return "", err
			}
			return selectedFolder, nil
		}

		// List existing run folders
		existingFolders, err := teo.listRunFolders(ctx, runsPath)
		if err != nil || len(existingFolders) == 0 {
			// Create initial folder if none exist
			selectedFolder := "initial"
			if err := teo.createRunFolderStructure(ctx, filepath.Join(runsPath, selectedFolder)); err != nil {
				return "", err
			}
			return selectedFolder, nil
		}

		// Return the latest folder (alphabetically sorted, so latest date/name)
		sort.Strings(existingFolders)
		return existingFolders[len(existingFolders)-1], nil

	case "create_new_runs_always":
		// Always create a new dated folder with incremental number
		counter := 1
		for {
			selectedFolder := fmt.Sprintf("%s-iteration-%d", today, counter)
			fullPath := filepath.Join(runsPath, selectedFolder)

			exists, _ := teo.workspaceFileExists(ctx, fullPath)
			if !exists {
				if err := teo.createRunFolderStructure(ctx, fullPath); err != nil {
					return "", err
				}
				return selectedFolder, nil
			}
			counter++
		}

	case "create_new_run_once_daily":
		// Check if today's folder exists
		prefix := today + "-"
		existingFolders, _ := teo.listRunFolders(ctx, runsPath)

		// Look for today's folder
		for _, folder := range existingFolders {
			if strings.HasPrefix(folder, prefix) {
				teo.GetLogger().Infof("📁 Using existing today's run folder: %s", folder)
				return folder, nil
			}
		}

		// Create new folder for today
		selectedFolder := fmt.Sprintf("%s-initial", today)
		fullPath := filepath.Join(runsPath, selectedFolder)
		if err := teo.createRunFolderStructure(ctx, fullPath); err != nil {
			return "", err
		}
		return selectedFolder, nil

	default:
		return "", fmt.Errorf("unknown run option: %s", runOption)
	}
}

// workspaceFileExists checks if a file or directory exists in the workspace
func (teo *TodoExecutionOrchestrator) workspaceFileExists(ctx context.Context, path string) (bool, error) {
	// Try to list the directory to check if it exists
	_, err := teo.ReadWorkspaceFile(ctx, filepath.Join(path, ".keep"))
	if err == nil {
		return true, nil
	}

	// Try to read the directory itself by listing parent
	parent := filepath.Dir(path)
	filename := filepath.Base(path)

	// List files in parent directory
	files, err := teo.listWorkspaceFiles(ctx, parent)
	if err != nil {
		return false, err
	}

	for _, file := range files {
		if file == filename || strings.HasPrefix(file, filename) {
			return true, nil
		}
	}

	return false, nil
}

// listWorkspaceFiles lists files in a directory (helper for workspaceFileExists)
func (teo *TodoExecutionOrchestrator) listWorkspaceFiles(ctx context.Context, path string) ([]string, error) {
	// This is a simplified version - in production, you'd use actual workspace tools
	// For now, return empty list to trigger folder creation
	return []string{}, nil
}

// listRunFolders lists existing run folder names
func (teo *TodoExecutionOrchestrator) listRunFolders(ctx context.Context, runsPath string) ([]string, error) {
	// This would typically use workspace tools to list directories
	// For now, return empty to trigger creation
	return []string{}, nil
}

// createRunFolderStructure creates the basic structure for a run folder
func (teo *TodoExecutionOrchestrator) createRunFolderStructure(ctx context.Context, runPath string) error {
	// Create .keep file to ensure directory is created
	keepFile := filepath.Join(runPath, ".keep")
	if err := teo.WriteWorkspaceFile(ctx, keepFile, "# This file ensures the run folder exists"); err != nil {
		return fmt.Errorf("failed to create run folder: %w", err)
	}

	// The actual folder creation will happen when files are written
	teo.GetLogger().Infof("✅ Created run folder structure: %s", runPath)
	return nil
}

// conversation history formatting moved to shared.FormatConversationHistory

// formatValidationFeedback formats a ValidationFeedback array as a string for logging
func formatValidationFeedback(feedback []todo_creation_human.ValidationFeedback) string {
	if len(feedback) == 0 {
		return "No feedback provided"
	}

	var parts []string
	for _, f := range feedback {
		parts = append(parts, fmt.Sprintf("[%s] %s: %s", f.Severity, f.Type, f.Description))
	}
	return strings.Join(parts, "\n")
}

// Event emission method removed - now using public method from todo_creation_human package:
// - EmitTodoStepsExtractedEvent

// Variable management methods removed - now using public methods from todo_creation_human package:
// - ResolveVariables
// - ResolveVariablesArray
// - FormatVariableNames
// - FormatVariableValues
// - LoadVariableValues

// requestVariableReview requests human review and optional editing of variables
// Returns: (updated bool, error) - true if variables were updated, false if approved as-is
func (teo *TodoExecutionOrchestrator) requestVariableReview(ctx context.Context, runWorkspacePath string) (bool, error) {
	teo.GetLogger().Infof("⏸️ Requesting human review for %d variables", len(teo.variablesManifest.Variables))

	// Format variables for display
	var variablesSummary strings.Builder
	variablesSummary.WriteString(fmt.Sprintf("**Current Variables (%d):**\n\n", len(teo.variablesManifest.Variables)))

	for _, variable := range teo.variablesManifest.Variables {
		variablesSummary.WriteString(fmt.Sprintf("- **{{%s}}**: %s\n", variable.Name, variable.Description))
		variablesSummary.WriteString(fmt.Sprintf("  - Current Value: `%s`\n", variable.Value))
		variablesSummary.WriteString("\n")
	}

	variablesSummary.WriteString("\n**Instructions:**\n")
	variablesSummary.WriteString("- Click **Approve** to use these variables as-is\n")
	variablesSummary.WriteString("- Or provide new variable values in the text field below\n")
	variablesSummary.WriteString("- Format: `VARIABLE_NAME=new_value` (one per line)\n")
	variablesSummary.WriteString("- Example:\n")
	variablesSummary.WriteString("  ```\n")
	variablesSummary.WriteString("  AWS_ACCOUNT_ID=999999999999\n")
	variablesSummary.WriteString("  GITHUB_REPO_URL=https://github.com/new/repo\n")
	variablesSummary.WriteString("  ```\n")

	// Generate unique request ID
	requestID := fmt.Sprintf("variable_review_%d", time.Now().UnixNano())

	// Request human feedback
	approved, feedback, err := teo.RequestHumanFeedback(
		ctx,
		requestID,
		"Review the variables below. Approve to use them as-is, or provide updated values in the text field.",
		variablesSummary.String(),
		"todo_execution_session",
		teo.GetObjective(),
	)
	if err != nil {
		return false, fmt.Errorf("failed to request variable review: %w", err)
	}

	// If approved without feedback, use variables as-is
	if approved || strings.TrimSpace(feedback) == "" {
		teo.GetLogger().Infof("✅ Variables approved as-is")
		return false, nil
	}

	// User provided feedback - check if it contains variable updates
	feedback = strings.TrimSpace(feedback)
	if feedback == "" || feedback == "Approve" {
		teo.GetLogger().Infof("✅ Variables approved as-is")
		return false, nil
	}

	// User provided feedback - try to extract variables and update
	teo.GetLogger().Infof("🔄 User provided variable updates: %s", feedback)

	// Run variable extraction phase with user feedback
	updatedManifest, err := teo.runVariableExtractionPhase(ctx, feedback, runWorkspacePath)
	if err != nil {
		return false, fmt.Errorf("failed to update variables: %w", err)
	}

	// Update variables in orchestrator
	teo.variablesManifest = updatedManifest
	teo.variableValues = make(map[string]string)
	for _, variable := range updatedManifest.Variables {
		teo.variableValues[variable.Name] = variable.Value
	}

	// Save updated variables to runs folder
	if err := teo.saveVariableValues(ctx, runWorkspacePath); err != nil {
		teo.GetLogger().Warnf("⚠️ Failed to save updated variables: %v", err)
		// Continue anyway - variables are in memory
	}

	teo.GetLogger().Infof("✅ Variables updated: %d variables", len(updatedManifest.Variables))
	return true, nil
}

// runVariableExtractionPhase runs the variable extraction agent with user feedback to update variables
func (teo *TodoExecutionOrchestrator) runVariableExtractionPhase(ctx context.Context, userFeedback string, runWorkspacePath string) (*todo_creation_human.VariablesManifest, error) {
	teo.GetLogger().Infof("🔍 Running variable extraction phase with user feedback")

	// Create variable extraction agent
	extractionAgent, err := teo.createVariableExtractionAgent(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create variable extraction agent: %w", err)
	}

	// Prepare template variables - use the current objective and run workspace path
	extractionTemplateVars := map[string]string{
		"Objective":     teo.GetObjective(),
		"WorkspacePath": runWorkspacePath,
	}

	// Use user feedback as the user message
	// The feedback should contain variable updates like "AWS_ACCOUNT_ID=999999"
	userMessage := fmt.Sprintf("Update variables based on the following user feedback:\n\n%s\n\nExtract and update variables from the objective, incorporating the user's changes. Call submit_variable_extraction_response tool with the structured output.", userFeedback)

	// Execute variable extraction using structured output via tool
	extractionAgentTyped, ok := extractionAgent.(*todo_creation_human.VariableExtractionAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast variable extraction agent to correct type")
	}

	// Start with empty conversation history
	var conversationHistory []llmtypes.MessageContent
	manifest, updatedHistory, err := extractionAgentTyped.ExecuteStructured(ctx, extractionTemplateVars, conversationHistory, userMessage)
	if err != nil {
		// Check if this is a non-structured response error
		if agents.IsNonStructuredResponseError(err) {
			var nonStructuredErr *agents.NonStructuredResponseError
			if errors.As(err, &nonStructuredErr) {
				teo.GetLogger().Warnf("⚠️ Variable extraction agent returned text response instead of structured output: %s", nonStructuredErr.TextResponse)
				return nil, fmt.Errorf("variable extraction agent returned text response: %s", nonStructuredErr.TextResponse)
			}
		}
		return nil, fmt.Errorf("variable extraction failed: %w", err)
	}

	teo.GetLogger().Infof("✅ Variable extraction completed: %d variables extracted (conversation has %d messages)", len(manifest.Variables), len(updatedHistory))

	// Return manifest directly (using types from todo_creation_human)
	return manifest, nil
}

// createVariableExtractionAgent creates the variable extraction agent
func (teo *TodoExecutionOrchestrator) createVariableExtractionAgent(ctx context.Context) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from workspace (read-only), writes only to variables
	baseWorkspacePath := teo.GetWorkspacePath()
	variablesPath := filepath.Join(baseWorkspacePath, "variables")

	// Read from base workspace (to understand objective), write only to variables folder
	// Note: Using base workspace as read path allows reading from root, but we restrict writes to variables/
	readPaths := []string{baseWorkspacePath}
	writePaths := []string{variablesPath}
	teo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	teo.GetLogger().Infof("🔒 Setting folder guard for variable extraction agent - Read paths: %v, Write paths: %v (variables automatically readable via writePaths)", readPaths, writePaths)

	// Use combined standardized agent creation and setup with no MCP servers
	// Variable extraction agent only uses workspace tools to read/write files
	agent, err := teo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"variable-extraction-agent",
		"variable_extraction",
		0, // No step number
		0, // No iteration
		teo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // No MCP servers - variable extraction agent only uses workspace tools
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return todo_creation_human.NewVariableExtractionAgent(config, logger, tracer, eventBridge)
		},
		teo.WorkspaceTools,
		teo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// saveVariableValues saves the current variable values to variables.json in the runs folder
func (teo *TodoExecutionOrchestrator) saveVariableValues(ctx context.Context, runWorkspacePath string) error {
	if teo.variablesManifest == nil {
		return fmt.Errorf("no variables manifest to save")
	}

	// Update extraction date to current time
	teo.variablesManifest.ExtractionDate = time.Now().Format(time.RFC3339)

	// Marshal to JSON
	variablesJSON, err := json.MarshalIndent(teo.variablesManifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal variables manifest to JSON: %w", err)
	}

	// Save to runs folder
	variablesPath := filepath.Join(runWorkspacePath, "variables", "variables.json")
	if err := teo.WriteWorkspaceFile(ctx, variablesPath, string(variablesJSON)); err != nil {
		return fmt.Errorf("failed to save variables.json to runs folder: %w", err)
	}

	teo.GetLogger().Infof("💾 Saved variables.json to runs folder: %s", variablesPath)
	return nil
}

// requestStepsApproval requests human approval for extracted steps before execution
// Returns: (approved bool, feedback string, error)
func (teo *TodoExecutionOrchestrator) requestStepsApproval(ctx context.Context, steps []todo_creation_human.TodoStep, revisionAttempt int) (bool, string, error) {
	teo.GetLogger().Infof("⏸️ Requesting human approval for %d extracted steps (revision attempt %d)", len(steps), revisionAttempt)

	// Generate unique request ID
	requestID := fmt.Sprintf("steps_approval_%d_%d", revisionAttempt, time.Now().UnixNano())

	// Request human approval using base orchestrator method
	// Simple question without detailed context (details are in the event)
	var question string
	if revisionAttempt == 1 {
		question = fmt.Sprintf("Review the %d extracted steps and approve to proceed with execution, or provide feedback for revision.", len(steps))
	} else {
		question = fmt.Sprintf("Review the revised steps (attempt %d). Approve to proceed or provide additional feedback.", revisionAttempt)
	}

	return teo.RequestHumanFeedback(
		ctx,
		requestID,
		question,
		"Steps have been extracted and displayed above. Can we proceed with execution?", // Simple context
		"todo_execution_session",
		teo.GetObjective(),
	)
}

// Step config types removed - now using types from todo_creation_human package:
// - StepConfig
// - StepConfigFile
// Step config methods removed - now using public methods from todo_creation_human package:
// - ReadStepConfigs
// - MatchStepConfigs
