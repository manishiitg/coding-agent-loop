package todo_execution

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	todo_creation_human "mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human"

	"github.com/tmc/langchaingo/llms"
)

// TodoExecutionOrchestrator manages the multi-agent todo execution process
type TodoExecutionOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator
}

// NewTodoExecutionOrchestrator creates a new multi-agent todo execution orchestrator
func NewTodoExecutionOrchestrator(
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
		llmConfig, // llmConfig passed from caller
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
	teo.SetWorkspacePath(workspacePath)

	// Parse todo_final.md into structured steps using plan reader agent
	teo.GetLogger().Infof("📖 Parsing todo_final.md into structured steps using plan reader agent")
	todoFinalPath := fmt.Sprintf("%s/todo_final.md", workspacePath)
	content, err := teo.ReadWorkspaceFile(ctx, todoFinalPath)
	if err != nil {
		return "", fmt.Errorf("failed to read todo_final.md: %w", err)
	}

	// Use plan reader agent to parse todo_final.md
	planReaderAgent, err := teo.createPlanReaderAgent(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create plan reader agent: %w", err)
	}

	// Prepare template variables for plan reader agent
	templateVars := map[string]string{
		"Objective":     objective,
		"WorkspacePath": workspacePath,
		"PlanMarkdown":  content,
		"FileType":      "todo_final",
	}

	// Execute plan reader agent to get structured response
	planningResponse, err := planReaderAgent.ExecuteStructured(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("failed to parse todo_final.md with plan reader agent: %w", err)
	}

	// Convert PlanningResponse.Steps to TodoStep array
	steps := make([]todo_creation_human.TodoStep, len(planningResponse.Steps))
	for i, step := range planningResponse.Steps {
		steps[i] = todo_creation_human.TodoStep{
			Title:               step.Title,
			Description:         step.Description,
			SuccessCriteria:     step.SuccessCriteria,
			WhyThisStep:         step.WhyThisStep,
			ContextDependencies: step.ContextDependencies,
			ContextOutput:       step.ContextOutput,
			SuccessPatterns:     step.SuccessPatterns,
			FailurePatterns:     step.FailurePatterns,
		}
	}

	teo.GetLogger().Infof("📋 Parsed %d steps from todo_final.md", len(steps))

	// Execute each step individually with validation feedback loop
	var executionResults []string
	var validationResults []string

	for i, step := range steps {
		teo.GetLogger().Infof("🔄 Executing step %d/%d: %s", i+1, len(steps), step.Title)

		var executionResult string
		var validationResult string
		maxAttempts := 3
		attempt := 1

		for attempt <= maxAttempts {
			teo.GetLogger().Infof("🔄 Attempt %d/%d for step %d", attempt, maxAttempts, i+1)

			// Execute this specific step
			var err error
			executionResult, err = teo.runStepExecutionPhase(ctx, step, i+1, len(steps), runOption, validationResult)
			if err != nil {
				teo.GetLogger().Warnf("⚠️ Step %d execution failed (attempt %d): %v", i+1, attempt, err)
				executionResult = fmt.Sprintf("Step %d execution failed (attempt %d): %v", i+1, attempt, err)
			}

			// Validate this specific step
			validationResponse, err := teo.runStepValidationPhase(ctx, step, i+1, len(steps), executionResult)
			if err != nil {
				teo.GetLogger().Warnf("⚠️ Step %d validation failed (attempt %d): %v", i+1, attempt, err)
				validationResult = fmt.Sprintf("Step %d validation failed (attempt %d): %v", i+1, attempt, err)
				break
			}

			// Check if validation passed
			if validationResponse.IsObjectiveSuccessCriteriaMet {
				teo.GetLogger().Infof("✅ Step %d completed successfully on attempt %d", i+1, attempt)
				validationResult = fmt.Sprintf("Step %d validation passed: %s", i+1, validationResponse.Feedback)
				break
			} else {
				teo.GetLogger().Infof("⚠️ Step %d validation failed on attempt %d: %s", i+1, attempt, validationResponse.Feedback)
				validationResult = validationResponse.Feedback

				if attempt < maxAttempts {
					teo.GetLogger().Infof("🔄 Retrying step %d with feedback: %s", i+1, validationResponse.Feedback)
				} else {
					teo.GetLogger().Warnf("❌ Step %d failed after %d attempts", i+1, maxAttempts)
					validationResult = fmt.Sprintf("Step %d failed after %d attempts. Final feedback: %s", i+1, maxAttempts, validationResponse.Feedback)
				}
			}

			attempt++
		}

		executionResults = append(executionResults, executionResult)
		validationResults = append(validationResults, validationResult)
	}

	duration := time.Since(teo.GetStartTime())
	teo.GetLogger().Infof("✅ Multi-agent todo execution completed in %v", duration)

	return teo.formatStepResults(executionResults, validationResults, len(steps)), nil
}

// runStepExecutionPhase executes a single step using the execution agent
func (teo *TodoExecutionOrchestrator) runStepExecutionPhase(ctx context.Context, step todo_creation_human.TodoStep, stepNumber, totalSteps int, runOption, previousFeedback string) (string, error) {
	executionAgent, err := teo.createExecutionAgent(ctx, step.Title, stepNumber, 0)
	if err != nil {
		return "", fmt.Errorf("failed to create execution agent: %w", err)
	}

	// Prepare template variables for this specific step
	templateVars := map[string]string{
		"StepNumber":              fmt.Sprintf("%d", stepNumber),
		"TotalSteps":              fmt.Sprintf("%d", totalSteps),
		"StepTitle":               step.Title,
		"StepDescription":         step.Description,
		"StepSuccessCriteria":     step.SuccessCriteria,
		"StepWhyThisStep":         step.WhyThisStep,
		"StepContextDependencies": strings.Join(step.ContextDependencies, ", "),
		"StepContextOutput":       step.ContextOutput,
		"StepSuccessPatterns":     strings.Join(step.SuccessPatterns, "\n- "),
		"StepFailurePatterns":     strings.Join(step.FailurePatterns, "\n- "),
		"WorkspacePath":           teo.GetWorkspacePath(),
		"RunOption":               runOption,
		"PreviousFeedback":        previousFeedback,
	}

	executionResult, _, err := executionAgent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("step %d execution failed: %w", stepNumber, err)
	}

	return executionResult, nil
}

// runStepValidationPhase validates a single step's execution using the validation agent
func (teo *TodoExecutionOrchestrator) runStepValidationPhase(ctx context.Context, step todo_creation_human.TodoStep, stepNumber, totalSteps int, executionResult string) (*ValidationResponse, error) {
	validationAgent, err := teo.createValidationAgent(ctx, step.Title, stepNumber, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create validation agent: %w", err)
	}

	// Cast to TodoValidationAgent to access ExecuteStructured method
	todoValidationAgent, ok := validationAgent.(*TodoValidationAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast validation agent to TodoValidationAgent")
	}

	// Prepare template variables for this specific step
	templateVars := map[string]string{
		"StepNumber":          fmt.Sprintf("%d", stepNumber),
		"TotalSteps":          fmt.Sprintf("%d", totalSteps),
		"StepTitle":           step.Title,
		"StepDescription":     step.Description,
		"StepSuccessCriteria": step.SuccessCriteria,
		"WorkspacePath":       teo.GetWorkspacePath(),
		"ExecutionOutput":     executionResult,
	}

	validationResponse, err := todoValidationAgent.ExecuteStructured(ctx, templateVars, nil)
	if err != nil {
		return nil, fmt.Errorf("step %d validation failed: %w", stepNumber, err)
	}

	return validationResponse, nil
}

// formatStepResults formats the step-by-step execution results
func (teo *TodoExecutionOrchestrator) formatStepResults(executionResults, validationResults []string, totalSteps int) string {
	return fmt.Sprintf(`# Todo Execution Report

## Execution Summary
- **Objective**: %s
- **Duration**: %v
- **Workspace**: %s
- **Total Steps**: %d
- **Steps Executed**: %d

## Step-by-Step Results

%s

## Overall Status
All steps have been executed and validated individually. Each step was processed with its specific Success Patterns and Failure Patterns from the structured todo_final.md format.

## Evidence Files
- **Runs Folder**: %s/runs/
- **Execution Logs**: Available in runs folder
- **Results**: Available in runs folder
- **Evidence**: Available in runs folder

Focus on executing each step effectively using proven approaches and avoiding failed patterns.`,
		teo.GetObjective(), time.Since(teo.GetStartTime()), teo.GetWorkspacePath(), totalSteps, len(executionResults),
		func() string {
			var result strings.Builder
			for i := 0; i < len(executionResults); i++ {
				result.WriteString(fmt.Sprintf("### Step %d\n", i+1))
				result.WriteString(fmt.Sprintf("**Execution**: %s\n\n", executionResults[i]))
				if i < len(validationResults) {
					result.WriteString(fmt.Sprintf("**Validation**: %s\n\n", validationResults[i]))
				}
			}
			return result.String()
		}(), teo.GetWorkspacePath())
}

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
	// Use combined standardized agent creation and setup
	agent, err := teo.CreateAndSetupStandardAgent(
		ctx,
		"validation-agent",
		phase,
		step,
		iteration,
		teo.GetMaxTurns(),
		agents.OutputFormatStructured,
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

// createPlanReaderAgent creates a plan reader agent for parsing todo_final.md
func (teo *TodoExecutionOrchestrator) createPlanReaderAgent(ctx context.Context) (*todo_creation_human.HumanControlledPlanReaderAgent, error) {
	config := &agents.OrchestratorAgentConfig{
		Provider:    teo.GetProvider(),
		Model:       teo.GetModel(),
		Temperature: teo.GetTemperature(),
	}

	agent := todo_creation_human.NewHumanControlledPlanReaderAgent(config, teo.GetLogger(), teo.GetTracer(), teo.GetContextAwareBridge())
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
