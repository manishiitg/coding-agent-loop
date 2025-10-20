package todo_execution

import (
	"context"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/llm"

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
	eventBridge orchestrator.EventBridge,
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
) (*TodoExecutionOrchestrator, error) {

	// Create base workflow orchestrator
	baseOrchestrator, err := orchestrator.NewBaseOrchestrator(
		logger,
		tracer,
		eventBridge,
		agents.TodoExecutionAgentType,
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

	// STEP 1: EXECUTION PHASE
	teo.GetLogger().Infof("🔄 Step 1/3: Todo Execution")

	executionResult, err := teo.runExecutionPhase(ctx, runOption)

	if err != nil {
		return "", fmt.Errorf("execution phase failed: %w", err)
	}

	// STEP 2: VALIDATION PHASE
	teo.GetLogger().Infof("🔄 Step 2/3: Todo Validation")

	validationResult, err := teo.runValidationPhase(ctx, executionResult)
	if err != nil {
		teo.GetLogger().Warnf("⚠️ Validation phase failed: %v", err)
		validationResult = "Validation failed: " + err.Error()
	}

	// STEP 3: WORKSPACE UPDATE PHASE
	teo.GetLogger().Infof("🔄 Step 3/3: Workspace Update")

	workspaceResult, err := teo.runWorkspaceUpdatePhase(ctx, executionResult, validationResult)
	if err != nil {
		teo.GetLogger().Warnf("⚠️ Workspace update phase failed: %v", err)
		workspaceResult = "Workspace update failed: " + err.Error()
	}

	// Check completion status
	allTodosCompleted, completionReason, err := teo.checkCompletion(ctx, workspaceResult)
	if err != nil {
		teo.GetLogger().Warnf("⚠️ Completion check failed: %v", err)
		allTodosCompleted = false
		completionReason = "Completion check failed: " + err.Error()
	}

	duration := time.Since(teo.GetStartTime())
	teo.GetLogger().Infof("✅ Multi-agent todo execution completed in %v", duration)

	return teo.formatResults(executionResult, validationResult, workspaceResult, allTodosCompleted, completionReason), nil
}

// runExecutionPhase executes todos using the execution agent
func (teo *TodoExecutionOrchestrator) runExecutionPhase(ctx context.Context, runOption string) (string, error) {
	executionAgent, err := teo.createExecutionAgent("execution", 0, 0)
	if err != nil {
		return "", fmt.Errorf("failed to create execution agent: %w", err)
	}

	// Prepare template variables for Execute method
	templateVars := map[string]string{
		"Objective":     teo.GetObjective(),
		"WorkspacePath": teo.GetWorkspacePath(),
		"RunOption":     runOption,
	}

	executionResult, err := executionAgent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("execution failed: %w", err)
	}

	return executionResult, nil
}

// runValidationPhase validates execution results using the validation agent
func (teo *TodoExecutionOrchestrator) runValidationPhase(ctx context.Context, executionResult string) (string, error) {
	validationAgent, err := teo.createValidationAgent("validation", 1, 0)
	if err != nil {
		return "", fmt.Errorf("failed to create validation agent: %w", err)
	}

	validationTemplateVars := map[string]string{
		"Objective":       teo.GetObjective(),
		"WorkspacePath":   teo.GetWorkspacePath(),
		"ExecutionOutput": executionResult,
	}
	validationResult, err := validationAgent.Execute(ctx, validationTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}

	return validationResult, nil
}

// runWorkspaceUpdatePhase updates workspace using the workspace agent
func (teo *TodoExecutionOrchestrator) runWorkspaceUpdatePhase(ctx context.Context, executionResult, validationResult string) (string, error) {
	workspaceAgent, err := teo.createWorkspaceAgent("workspace", 2, 0)
	if err != nil {
		return "", fmt.Errorf("failed to create workspace agent: %w", err)
	}

	workspaceTemplateVars := map[string]string{
		"Objective":        teo.GetObjective(),
		"WorkspacePath":    teo.GetWorkspacePath(),
		"ExecutionOutput":  executionResult,
		"ValidationOutput": validationResult,
	}
	workspaceResult, err := workspaceAgent.Execute(ctx, workspaceTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("workspace update failed: %w", err)
	}

	return workspaceResult, nil
}

// createConditionalLLM creates a conditional LLM on-demand with todo execution-specific configuration
func (teo *TodoExecutionOrchestrator) createConditionalLLM() (*llm.ConditionalLLM, error) {
	// Create config for conditional LLM using todo execution-specific settings
	conditionalConfig := &agents.OrchestratorAgentConfig{
		Provider:      teo.GetProvider(),
		Model:         teo.GetModel(),
		Temperature:   teo.GetTemperature(),
		ServerNames:   teo.GetSelectedServers(),
		MCPConfigPath: teo.GetMCPConfigPath(),
	}

	// Create conditional LLM with todo execution-specific context
	conditionalLLM, err := llm.CreateConditionalLLMWithEventBridge(conditionalConfig, teo.GetContextAwareBridge(), teo.GetLogger(), teo.GetTracer())
	if err != nil {
		return nil, fmt.Errorf("failed to create conditional LLM: %w", err)
	}

	return conditionalLLM, nil
}

// checkCompletion uses conditional logic to determine if all todos are completed
func (teo *TodoExecutionOrchestrator) checkCompletion(ctx context.Context, workspaceResult string) (bool, string, error) {
	// Create conditional LLM on-demand
	conditionalLLM, err := teo.createConditionalLLM()
	if err != nil {
		teo.GetLogger().Errorf("❌ Failed to create conditional LLM: %v", err)
		return false, "Failed to create conditional LLM: " + err.Error(), err
	}

	// Prepare context and question for true/false decision
	context := fmt.Sprintf("Workspace Update Agent Report:\n%s", workspaceResult)
	question := "Are all todos completed? Look for completion status, remaining todos, and overall project status in the workspace update report."

	// Use conditional LLM to make the decision
	result, err := conditionalLLM.Decide(ctx, context, question, 0, 0)
	if err != nil {
		teo.GetLogger().Errorf("❌ Conditional LLM decision failed: %v", err)
		return false, "Conditional decision failed: " + err.Error(), err
	}

	return result.GetResult(), result.Reason, nil
}

// formatResults formats the execution results into a comprehensive report
func (teo *TodoExecutionOrchestrator) formatResults(executionResult, validationResult, workspaceResult string, allCompleted bool, completionReason string) string {
	status := "IN PROGRESS"
	if allCompleted {
		status = "COMPLETED"
	}

	return fmt.Sprintf(`# Todo Execution Report

## Execution Summary
- **Objective**: %s
- **Status**: %s
- **Duration**: %v
- **Workspace**: %s
- **Completion Reason**: %s

## Execution Results
%s

## Validation Results
%s

## Workspace Update Results
%s

## Next Steps
%s

## Evidence Files
- **Runs Folder**: %s/runs/
- **Execution Logs**: Available in runs folder
- **Results**: Available in runs folder
- **Evidence**: Available in runs folder

Focus on executing as many incomplete todos as possible effectively and providing comprehensive results.`,
		teo.GetObjective(), status, time.Since(teo.GetStartTime()), teo.GetWorkspacePath(), completionReason,
		executionResult, validationResult, workspaceResult,
		func() string {
			if allCompleted {
				return "All todos completed successfully!"
			}
			return "Continue with next execution cycle for remaining todos."
		}(), teo.GetWorkspacePath())
}

// Agent creation methods
func (teo *TodoExecutionOrchestrator) createExecutionAgent(phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := teo.CreateAndSetupStandardAgent(
		"todo_execution",
		"execution-agent",
		phase,
		step,
		iteration,
		teo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) agents.OrchestratorAgent {
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

func (teo *TodoExecutionOrchestrator) createValidationAgent(phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := teo.CreateAndSetupStandardAgent(
		"todo_validation",
		"validation-agent",
		phase,
		step,
		iteration,
		teo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) agents.OrchestratorAgent {
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

func (teo *TodoExecutionOrchestrator) createWorkspaceAgent(phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := teo.CreateAndSetupStandardAgent(
		"workspace_update",
		"workspace-agent",
		phase,
		step,
		iteration,
		teo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) agents.OrchestratorAgent {
			return NewWorkspaceUpdateAgent(config, logger, tracer, eventBridge)
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
	if ro, ok := options["runOption"].(string); ok && ro != "" {
		runOption = ro
	}

	// Call the existing ExecuteTodos method
	return teo.ExecuteTodos(ctx, objective, workspacePath, runOption)
}

// GetType returns the orchestrator type
func (teo *TodoExecutionOrchestrator) GetType() string {
	return "todo_execution"
}
