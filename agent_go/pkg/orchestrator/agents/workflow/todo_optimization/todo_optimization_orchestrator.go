package todo_optimization

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
	"mcp-agent/agent_go/pkg/orchestrator/llm"

	"github.com/tmc/langchaingo/llms"
)

// TodoOptimizationOrchestrator manages the multi-agent optimization process
type TodoOptimizationOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator
}

// NewTodoOptimizationOrchestrator creates a new multi-agent optimization orchestrator
func NewTodoOptimizationOrchestrator(
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
) (*TodoOptimizationOrchestrator, error) {
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

	return &TodoOptimizationOrchestrator{
		BaseOrchestrator: baseOrchestrator,
	}, nil
}

// ExecuteRefinement orchestrates the iterative refinement process
func (too *TodoOptimizationOrchestrator) ExecuteRefinement(ctx context.Context, objective, workspacePath string) (string, error) {
	too.GetLogger().Infof("🔄 Starting iterative refinement for objective: %s", objective)

	// Set objective and workspace path directly
	too.SetObjective(objective)
	too.SetWorkspacePath(workspacePath)

	maxIterations := 3 // Configurable max refinement iterations
	var finalRefinementResult string

	// Iterative refinement loop
	var previousCritiqueResult string
	for iteration := 1; iteration <= maxIterations; iteration++ {
		too.GetLogger().Infof("🔄 Iteration %d/%d: Refining todo list...", iteration, maxIterations)

		// Step 1: Refine the todo list (use previous critique result for improvement)
		refinementResult, err := too.runRefinementPhase(ctx, objective, previousCritiqueResult, iteration)

		if err != nil {
			return "", fmt.Errorf("refinement iteration %d failed: %w", iteration, err)
		}

		// Step 2: Critique the refined output
		refinementPrompt := fmt.Sprintf("Critique the refined todo list output for iteration %d. Focus on factual accuracy, completeness, and alignment with the objective.", iteration)
		critiqueResult, err := too.runCritiquePhase(ctx, objective, refinementResult, refinementPrompt, iteration, previousCritiqueResult)

		if err != nil {
			return "", fmt.Errorf("critique iteration %d failed: %w", iteration, err)
		}

		// Step 3: Check if there's room for more improvement
		needsMoreImprovement, _, err := too.checkImprovementNeeded(ctx, critiqueResult)
		if err != nil {
			too.GetLogger().Warnf("⚠️ Improvement check failed: %v", err)
			needsMoreImprovement = true
		}

		// Store the current refinement result
		finalRefinementResult = refinementResult

		// If critique is satisfied, exit the loop
		if !needsMoreImprovement {
			break
		}

		// Store critique result for next iteration
		previousCritiqueResult = critiqueResult
	}

	duration := time.Since(too.GetStartTime())
	too.GetLogger().Infof("✅ Iterative refinement completed in %v", duration)

	return finalRefinementResult, nil
}

// runRefinementPhase runs a single refinement iteration using the proper agent pattern
func (too *TodoOptimizationOrchestrator) runRefinementPhase(ctx context.Context, objective, previousCritiqueResult string, iteration int) (string, error) {
	// Create TodoRefinePlannerAgent for refinement
	refineAgent, err := too.createRefineAgent(ctx, "refinement", 0, iteration)
	if err != nil {
		return "", fmt.Errorf("failed to create refine agent: %w", err)
	}

	// Prepare template variables with iteration context and previous critique
	templateVars := map[string]string{
		"Objective":        objective,
		"WorkspacePath":    too.GetWorkspacePath(),
		"Iteration":        fmt.Sprintf("%d", iteration),
		"CritiqueFeedback": previousCritiqueResult,
	}

	// Execute refinement using the TodoRefinePlannerAgent
	refinementResult, _, err := refineAgent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("refinement execution failed: %v", err)
	}

	return refinementResult, nil
}

// runCritiquePhase runs a single critique iteration using the proper agent pattern
func (too *TodoOptimizationOrchestrator) runCritiquePhase(ctx context.Context, objective, inputData, inputPrompt string, iteration int, previousCritiqueResult string) (string, error) {
	// Create DataCritiqueAgent for critique
	critiqueAgent, err := too.createCritiqueAgent(ctx, "critique", 1, iteration)
	if err != nil {
		return "", fmt.Errorf("failed to create critique agent: %w", err)
	}

	// Prepare template variables
	refinementHistory := "No refinement history available for first iteration"
	if iteration > 1 && previousCritiqueResult != "" {
		refinementHistory = strings.TrimSpace(previousCritiqueResult)
	}

	templateVars := map[string]string{
		"Objective":         objective,
		"InputData":         inputData,
		"InputPrompt":       inputPrompt,
		"RefinementHistory": refinementHistory,
		"Iteration":         fmt.Sprintf("%d", iteration),
		"WorkspacePath":     too.GetWorkspacePath(),
	}

	// Execute critique using the DataCritiqueAgent
	critiqueResult, _, err := critiqueAgent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("critique execution failed: %v", err)
	}

	return critiqueResult, nil
}

// createConditionalLLM creates a conditional LLM on-demand with todo optimization-specific configuration
func (too *TodoOptimizationOrchestrator) createConditionalLLM() (*llm.ConditionalLLM, error) {
	// Create config for conditional LLM using todo optimization-specific settings
	conditionalConfig := &agents.OrchestratorAgentConfig{
		Provider:      too.GetProvider(),
		Model:         too.GetModel(),
		Temperature:   too.GetTemperature(),
		ServerNames:   too.GetSelectedServers(),
		MCPConfigPath: too.GetMCPConfigPath(),
	}

	// Create conditional LLM with todo optimization-specific context
	conditionalLLM, err := llm.CreateConditionalLLMWithEventBridge(conditionalConfig, too.GetContextAwareBridge(), too.GetLogger(), too.GetTracer())
	if err != nil {
		return nil, fmt.Errorf("failed to create conditional LLM: %w", err)
	}

	return conditionalLLM, nil
}

// checkImprovementNeeded uses conditional logic to determine if there's room for more improvement
func (too *TodoOptimizationOrchestrator) checkImprovementNeeded(ctx context.Context, critiqueResult string) (bool, string, error) {
	// Create conditional LLM on-demand
	conditionalLLM, err := too.createConditionalLLM()
	if err != nil {
		too.GetLogger().Errorf("❌ Failed to create conditional LLM: %v", err)
		return false, "Failed to create conditional LLM: " + err.Error(), err
	}

	// Use the improvement check prompt - focus on specific decision criteria
	context := critiqueResult
	question := `Analyze the critique report and determine if another refinement iteration is needed.

Focus on these critical criteria:
- FACTUAL ERRORS: Major factual inaccuracies that need correction
- ANALYTICAL GAPS: Significant missing analysis or weak reasoning
- PROMPT MISALIGNMENT: Major deviations from the required task
- QUALITY ISSUES: Issues that would significantly impact the objective achievement

If the critique identifies ANY of these critical issues that would benefit from another refinement iteration, return true. Otherwise, return false.
`

	// Use conditional LLM to make the decision
	result, err := conditionalLLM.Decide(ctx, context, question, 0, 0)
	if err != nil {
		too.GetLogger().Errorf("❌ Improvement check decision failed: %v", err)
		return false, "Improvement check failed: " + err.Error(), err
	}

	return result.GetResult(), result.Reason, nil
}

// Agent creation methods
func (too *TodoOptimizationOrchestrator) createRefineAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := too.CreateAndSetupStandardAgent(
		ctx,
		"refine-agent",
		phase,
		step,
		iteration,
		too.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewTodoRefinePlannerAgent(config, logger, tracer, eventBridge)
		},
		too.WorkspaceTools,
		too.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (too *TodoOptimizationOrchestrator) createCritiqueAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := too.CreateAndSetupStandardAgent(
		ctx,
		"critique-agent",
		phase,
		step,
		iteration,
		too.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewDataCritiqueAgent(config, logger, tracer, eventBridge)
		},
		too.WorkspaceTools,
		too.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// Execute implements the Orchestrator interface
func (too *TodoOptimizationOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	// Validate that no options are provided since this orchestrator doesn't use them
	if len(options) > 0 {
		return "", fmt.Errorf("todo optimization orchestrator does not accept options")
	}

	// Validate workspace path is provided
	if workspacePath == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	// Call the existing ExecuteRefinement method
	return too.ExecuteRefinement(ctx, objective, workspacePath)
}
