package todo_execution

import (
	"context"
	"encoding/json"
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

// PlanStep represents a step in the execution plan
type PlanStep struct {
	Title               string                `json:"title"`
	Description         string                `json:"description"`
	SuccessCriteria     string                `json:"success_criteria"`
	WhyThisStep         string                `json:"why_this_step"`
	ContextDependencies []string              `json:"context_dependencies"`
	ContextOutput       FlexibleContextOutput `json:"context_output"`
	SuccessPatterns     []string              `json:"success_patterns,omitempty"`
	FailurePatterns     []string              `json:"failure_patterns,omitempty"`
	HasLoop             bool                  `json:"has_loop,omitempty"`
	LoopCondition       string                `json:"loop_condition,omitempty"`
	MaxIterations       int                   `json:"max_iterations,omitempty"`
	LoopDescription     string                `json:"loop_description,omitempty"`
}

// PlanningResponse represents the structured response from plan reading
type PlanningResponse struct {
	Steps []PlanStep `json:"steps"`
}

// TodoStep represents a todo step for execution
type TodoStep struct {
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	SuccessCriteria     string   `json:"success_criteria"`
	WhyThisStep         string   `json:"why_this_step"`
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
	SuccessPatterns     []string `json:"success_patterns,omitempty"`
	FailurePatterns     []string `json:"failure_patterns,omitempty"`
	HasLoop             bool     `json:"has_loop,omitempty"`
	LoopCondition       string   `json:"loop_condition,omitempty"`
	MaxIterations       int      `json:"max_iterations,omitempty"`
	LoopDescription     string   `json:"loop_description,omitempty"`
}

// TodoStepsExtractedEvent represents todo steps extracted from a plan
type TodoStepsExtractedEvent struct {
	events.BaseEventData
	TotalStepsExtracted int        `json:"total_steps_extracted"`
	ExtractedSteps      []TodoStep `json:"extracted_steps"`
	ExtractionMethod    string     `json:"extraction_method"`
	PlanSource          string     `json:"plan_source"`
}

// GetEventType implements events.EventData interface
func (e *TodoStepsExtractedEvent) GetEventType() events.EventType {
	return events.TodoStepsExtracted
}

// Variable represents a single variable definition
type Variable struct {
	Name        string `json:"name"`        // e.g., "AWS_ACCOUNT_ID"
	Value       string `json:"value"`       // Original value from objective
	Description string `json:"description"` // e.g., "AWS account number for deployment"
}

// VariablesManifest contains all extracted variables
type VariablesManifest struct {
	Objective      string     `json:"objective"` // Templated objective with {{VARS}}
	Variables      []Variable `json:"variables"` // List of variables
	ExtractionDate string     `json:"extraction_date"`
}

// TodoExecutionOrchestrator manages the multi-agent todo execution process
type TodoExecutionOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator
	// Variable management
	variablesManifest *VariablesManifest // Extracted variables
	variableValues    map[string]string  // Runtime variable values
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
	if err := teo.loadVariableValues(ctx, workspacePath); err != nil {
		teo.GetLogger().Infof("⚠️ Could not load variables (this is optional): %v", err)
		// Continue execution even if variables don't exist
	}

	// Read todo_final.json directly from workspace root
	teo.GetLogger().Infof("📖 Reading todo_final.json from workspace root")
	todoFinalJSONPath := filepath.Join(workspacePath, "todo_final.json")
	planContent, err := teo.ReadWorkspaceFile(ctx, todoFinalJSONPath)
	if err != nil {
		return "", fmt.Errorf("failed to read todo_final.json: %w", err)
	}

	// Parse JSON directly
	var planningResponse PlanningResponse
	if err := json.Unmarshal([]byte(planContent), &planningResponse); err != nil {
		return "", fmt.Errorf("failed to parse todo_final.json: %w", err)
	}

	// Convert PlanningResponse.Steps to TodoStep array
	steps := make([]TodoStep, len(planningResponse.Steps))
	for i, step := range planningResponse.Steps {
		// Set default max_iterations if not provided and has_loop is true
		maxIterations := step.MaxIterations
		if step.HasLoop && maxIterations == 0 {
			maxIterations = 10 // Default max iterations
		}

		// Resolve variables in step fields
		steps[i] = TodoStep{
			Title:               teo.resolveVariables(step.Title),
			Description:         teo.resolveVariables(step.Description),
			SuccessCriteria:     teo.resolveVariables(step.SuccessCriteria),
			WhyThisStep:         teo.resolveVariables(step.WhyThisStep),
			ContextDependencies: teo.resolveVariablesArray(step.ContextDependencies),
			ContextOutput:       teo.resolveVariables(string(step.ContextOutput)), // Convert FlexibleContextOutput to string
			SuccessPatterns:     step.SuccessPatterns,
			FailurePatterns:     step.FailurePatterns,
			HasLoop:             step.HasLoop,
			LoopCondition:       teo.resolveVariables(step.LoopCondition),
			MaxIterations:       maxIterations,
			LoopDescription:     teo.resolveVariables(step.LoopDescription),
		}
	}

	teo.GetLogger().Infof("📋 Parsed %d steps from todo_final.json", len(steps))

	// Emit todo steps extracted event (so frontend can display the extracted steps)
	teo.emitTodoStepsExtractedEvent(ctx, steps, "todo_final_json")

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

	duration := time.Since(teo.GetStartTime())
	teo.GetLogger().Infof("✅ Multi-agent todo execution completed in %v", duration)

	return "Execution Completed", nil
}

// runStepExecutionPhase executes a single step using the execution agent
func (teo *TodoExecutionOrchestrator) runStepExecutionPhase(ctx context.Context, step TodoStep, stepNumber, totalSteps int, selectedRunFolder, runOption, previousFeedback, previousIterationOutput string, currentIteration, maxIterations int) (string, []llmtypes.MessageContent, error) {
	executionAgent, err := teo.createExecutionAgent(ctx, step.Title, stepNumber, 0)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create execution agent: %w", err)
	}

	// Prepare template variables for this specific step
	templateVars := map[string]string{
		"StepNumber":              fmt.Sprintf("%d", stepNumber),
		"TotalSteps":              fmt.Sprintf("%d", totalSteps),
		"StepTitle":               step.Title,
		"StepDescription":         step.Description,
		"StepSuccessCriteria":     step.SuccessCriteria,
		"StepContextDependencies": strings.Join(step.ContextDependencies, ", "),
		"StepContextOutput":       step.ContextOutput,
		"StepSuccessPatterns":     strings.Join(step.SuccessPatterns, "\n- "),
		"StepFailurePatterns":     strings.Join(step.FailurePatterns, "\n- "),
		"WorkspacePath":           teo.GetWorkspacePath(), // This now includes runs/{folder}
		"RunOption":               runOption,
		"PreviousFeedback":        previousFeedback,
		"PreviousIterationOutput": previousIterationOutput,
		"HasLoop":                 fmt.Sprintf("%v", step.HasLoop),
		"LoopCondition":           step.LoopCondition,
		"LoopDescription":         step.LoopDescription,
		"CurrentIteration":        fmt.Sprintf("%d", currentIteration),
		"MaxIterations":           fmt.Sprintf("%d", maxIterations),
	}

	// Add variable names and values if variables exist
	if variableNames := teo.formatVariableNames(); variableNames != "" {
		templateVars["VariableNames"] = variableNames
	}
	if variableValues := teo.formatVariableValues(); variableValues != "" {
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
func (teo *TodoExecutionOrchestrator) runStepValidationPhase(ctx context.Context, step TodoStep, stepNumber, totalSteps int, executionResult string, conversationHistory []llmtypes.MessageContent) (*ValidationResponse, error) {
	validationAgent, err := teo.createValidationAgent(ctx, step.Title, stepNumber, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create validation agent: %w", err)
	}

	// Cast to TodoValidationAgent to access ExecuteStructured method
	todoValidationAgent, ok := validationAgent.(*TodoValidationAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast validation agent to TodoValidationAgent")
	}

	// Format conversation history as string for template variable
	conversationHistoryStr := shared.FormatConversationHistory(conversationHistory)

	// Prepare template variables for this specific step
	templateVars := map[string]string{
		"StepNumber":          fmt.Sprintf("%d", stepNumber),
		"TotalSteps":          fmt.Sprintf("%d", totalSteps),
		"StepTitle":           step.Title,
		"StepDescription":     step.Description,
		"StepSuccessCriteria": step.SuccessCriteria,
		"WorkspacePath":       teo.GetWorkspacePath(), // This now includes runs/{folder}
		"ExecutionOutput":     conversationHistoryStr, // Pass conversation history instead of just result
		"LoopCondition":       step.LoopCondition,     // For loop steps: condition to check
	}

	validationResponse, _, err := todoValidationAgent.ExecuteStructured(ctx, templateVars, conversationHistory)
	if err != nil {
		return nil, fmt.Errorf("step %d validation failed: %w", stepNumber, err)
	}

	return validationResponse, nil
}

// formatStepResults removed; returning simple completion message instead

// Agent creation methods
func (teo *TodoExecutionOrchestrator) createExecutionAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := teo.CreateAndSetupStandardAgent(
		ctx,
		"todo_execution",
		phase,
		step,
		iteration,
		teo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewTodoExecutionAgent(config, logger, tracer, eventBridge)
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
	// Use combined standardized agent creation and setup with no MCP servers
	// Validation agent only reads execution outputs and writes validation reports using workspace tools
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
			return NewTodoValidationAgent(config, logger, tracer, eventBridge)
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
func formatValidationFeedback(feedback []ValidationFeedback) string {
	if len(feedback) == 0 {
		return "No feedback provided"
	}

	var parts []string
	for _, f := range feedback {
		parts = append(parts, fmt.Sprintf("[%s] %s: %s", f.Severity, f.Type, f.Description))
	}
	return strings.Join(parts, "\n")
}

// emitTodoStepsExtractedEvent emits an event when todo steps are extracted from todo_final.json
func (teo *TodoExecutionOrchestrator) emitTodoStepsExtractedEvent(ctx context.Context, extractedSteps []TodoStep, planSource string) {
	if teo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data
	eventData := &TodoStepsExtractedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		TotalStepsExtracted: len(extractedSteps),
		ExtractedSteps:      extractedSteps,
		ExtractionMethod:    "direct_json",
		PlanSource:          planSource,
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
		Type:      events.TodoStepsExtracted,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through the context-aware bridge
	bridge := teo.GetContextAwareBridge()
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		teo.GetLogger().Warnf("⚠️ Failed to emit todo steps extracted event: %w", err)
	} else {
		teo.GetLogger().Infof("✅ Emitted todo steps extracted event: %d steps extracted", len(extractedSteps))
	}
}

// loadVariableValues loads runtime variable values from variables.json
func (teo *TodoExecutionOrchestrator) loadVariableValues(ctx context.Context, workspacePath string) error {
	// Load variable values from variables.json
	variablesPath := filepath.Join(workspacePath, "variables", "variables.json")
	variablesContent, err := teo.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		return fmt.Errorf("variables.json not found (this is optional): %w", err)
	}

	// Parse variables.json to get current values
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		return fmt.Errorf("failed to parse variables.json: %w", err)
	}

	// Store manifest and load values into the variableValues map
	teo.variablesManifest = &manifest
	teo.variableValues = make(map[string]string)
	for _, variable := range manifest.Variables {
		teo.variableValues[variable.Name] = variable.Value
	}

	teo.GetLogger().Infof("✅ Loaded variable values from variables.json: %d variables", len(teo.variableValues))
	return nil
}

// resolveVariables replaces {{VARIABLE}} placeholders with actual values
func (teo *TodoExecutionOrchestrator) resolveVariables(text string) string {
	if teo.variableValues == nil {
		return text // No variables to resolve
	}

	resolved := text
	for varName, varValue := range teo.variableValues {
		placeholder := fmt.Sprintf("{{%s}}", varName)
		resolved = strings.ReplaceAll(resolved, placeholder, varValue)
	}
	return resolved
}

// resolveVariablesArray resolves variables in an array of strings
func (teo *TodoExecutionOrchestrator) resolveVariablesArray(arr []string) []string {
	if teo.variableValues == nil {
		return arr // No variables to resolve
	}

	resolved := make([]string, len(arr))
	for i, item := range arr {
		resolved[i] = teo.resolveVariables(item)
	}
	return resolved
}

// formatVariableNames formats the variables manifest into a human-readable string for agent prompts
func (teo *TodoExecutionOrchestrator) formatVariableNames() string {
	if teo.variablesManifest == nil || len(teo.variablesManifest.Variables) == 0 {
		return "" // No variables to format
	}

	var builder strings.Builder
	builder.WriteString("\n")
	for _, variable := range teo.variablesManifest.Variables {
		builder.WriteString(fmt.Sprintf("- {{%s}} - %s\n", variable.Name, variable.Description))
	}
	return builder.String()
}

// formatVariableValues formats the variables manifest with their actual values for agent prompts
func (teo *TodoExecutionOrchestrator) formatVariableValues() string {
	if teo.variablesManifest == nil || len(teo.variablesManifest.Variables) == 0 {
		return "" // No variables to format
	}

	var builder strings.Builder
	builder.WriteString("\n")
	for _, variable := range teo.variablesManifest.Variables {
		// Get the actual resolved value from variableValues map if available
		actualValue := variable.Value
		if teo.variableValues != nil {
			if resolvedValue, exists := teo.variableValues[variable.Name]; exists {
				actualValue = resolvedValue
			}
		}
		builder.WriteString(fmt.Sprintf("- {{%s}} = %s - %s\n", variable.Name, actualValue, variable.Description))
	}
	return builder.String()
}

// requestStepsApproval requests human approval for extracted steps before execution
// Returns: (approved bool, feedback string, error)
func (teo *TodoExecutionOrchestrator) requestStepsApproval(ctx context.Context, steps []TodoStep, revisionAttempt int) (bool, string, error) {
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
