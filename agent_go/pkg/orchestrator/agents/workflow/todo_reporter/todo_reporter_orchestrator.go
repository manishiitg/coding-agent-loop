package todo_reporter

import (
	"context"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_optimization"
	"mcp-agent/agent_go/pkg/orchestrator/llm"

	"github.com/tmc/langchaingo/llms"
)

// TodoReporterOrchestrator manages the multi-agent report generation process
type TodoReporterOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator
}

// NewTodoReporterOrchestrator creates a new multi-agent report generation orchestrator
func NewTodoReporterOrchestrator(
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
) (*TodoReporterOrchestrator, error) {
	baseOrchestrator, err := orchestrator.NewBaseOrchestrator(
		logger,
		tracer,
		eventBridge,
		agents.TodoReporterAgentType,
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

	return &TodoReporterOrchestrator{
		BaseOrchestrator: baseOrchestrator,
	}, nil
}

// ExecuteReportGeneration orchestrates the iterative report generation process
func (tro *TodoReporterOrchestrator) ExecuteReportGeneration(ctx context.Context, objective, workspacePath string) (string, error) {
	tro.GetLogger().Infof("📊 Starting iterative report generation for objective: %s", objective)

	// Set objective and workspace path directly
	tro.SetObjective(objective)
	tro.SetWorkspacePath(workspacePath)

	maxIterations := 3 // Configurable max report refinement iterations
	var finalReportResult string

	// Iterative report refinement loop with critique feedback
	var previousCritiqueResult string
	for iteration := 1; iteration <= maxIterations; iteration++ {
		tro.GetLogger().Infof("📊 Report generation iteration %d/%d", iteration, maxIterations)

		// Step 1: Generate report (with previous critique feedback for iterations 2+)
		var reportResult string
		var err error

		if iteration == 1 {
			// First iteration - no previous critique available
			reportResult, err = tro.runReportGenerationPhase(ctx, objective, "", iteration)
		} else {
			// Subsequent iterations - use previous critique result for improvement
			reportResult, err = tro.runReportGenerationPhase(ctx, objective, previousCritiqueResult, iteration)
		}

		if err != nil {
			return "", fmt.Errorf("report generation iteration %d failed: %w", iteration, err)
		}

		// Step 2: Critique the generated report
		reportPrompt := fmt.Sprintf("Critique the generated report for iteration %d. Focus on accuracy, completeness, and alignment with the objective.", iteration)
		critiqueResult, err := tro.runCritiquePhase(ctx, objective, reportResult, reportPrompt, iteration)

		if err != nil {
			return "", fmt.Errorf("report critique iteration %d failed: %w", iteration, err)
		}

		// Step 3: Check if more improvement is needed
		needsMoreWork, _, err := tro.checkImprovementNeeded(ctx, critiqueResult)
		if err != nil {
			tro.GetLogger().Warnf("⚠️ Report improvement check failed: %v", err)
			needsMoreWork = true
		}

		// Store the current report result
		finalReportResult = reportResult

		// If critique is satisfied, exit the loop
		if !needsMoreWork {
			break
		}

		// Store critique result for next iteration
		previousCritiqueResult = critiqueResult
	}

	duration := time.Since(tro.GetStartTime())
	tro.GetLogger().Infof("✅ Iterative report generation completed in %v", duration)

	return finalReportResult, nil
}

// runReportGenerationPhase runs a single report generation iteration using the proper agent pattern
func (tro *TodoReporterOrchestrator) runReportGenerationPhase(ctx context.Context, objective, previousCritiqueResult string, iteration int) (string, error) {
	// Create ReportGenerationAgent for report generation
	reportAgent, err := tro.createReportAgent("report_generation", 0, iteration)
	if err != nil {
		return "", fmt.Errorf("failed to create report generation agent: %w", err)
	}

	// Prepare template variables with critique feedback
	templateVars := map[string]string{
		"Objective":        objective,
		"WorkspacePath":    tro.GetWorkspacePath(),
		"CritiqueFeedback": previousCritiqueResult,
	}

	// Execute report generation using the ReportGenerationAgent
	reportResult, err := reportAgent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("report generation execution failed: %v", err)
	}

	return reportResult, nil
}

// runCritiquePhase runs a single critique iteration using the proper agent pattern
func (tro *TodoReporterOrchestrator) runCritiquePhase(ctx context.Context, objective, inputData, inputPrompt string, iteration int) (string, error) {
	// Create DataCritiqueAgent for critique
	critiqueAgent, err := tro.createCritiqueAgent("critique", 0, iteration)
	if err != nil {
		return "", fmt.Errorf("failed to create critique agent: %w", err)
	}

	// Prepare template variables
	templateVars := map[string]string{
		"objective":          objective,
		"input_data":         inputData,
		"input_prompt":       inputPrompt,
		"refinement_history": "No refinement history available for first iteration",
		"iteration":          fmt.Sprintf("%d", iteration),
	}

	// Execute critique using the DataCritiqueAgent
	critiqueResult, err := critiqueAgent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("critique execution failed: %v", err)
	}

	return critiqueResult, nil
}

// createConditionalLLM creates a conditional LLM on-demand with todo reporter-specific configuration
func (tro *TodoReporterOrchestrator) createConditionalLLM() (*llm.ConditionalLLM, error) {
	// Create config for conditional LLM using todo reporter-specific settings
	conditionalConfig := &agents.OrchestratorAgentConfig{
		Provider:      tro.GetProvider(),
		Model:         tro.GetModel(),
		Temperature:   tro.GetTemperature(),
		ServerNames:   tro.GetSelectedServers(),
		MCPConfigPath: tro.GetMCPConfigPath(),
	}

	// Create conditional LLM with todo reporter-specific context
	conditionalLLM, err := llm.CreateConditionalLLMWithEventBridge(conditionalConfig, tro.GetContextAwareBridge(), tro.GetLogger(), tro.GetTracer())
	if err != nil {
		return nil, fmt.Errorf("failed to create conditional LLM: %w", err)
	}

	return conditionalLLM, nil
}

// checkImprovementNeeded uses conditional logic to determine if there's room for more improvement
func (tro *TodoReporterOrchestrator) checkImprovementNeeded(ctx context.Context, critiqueResult string) (bool, string, error) {
	// Create conditional LLM on-demand
	conditionalLLM, err := tro.createConditionalLLM()
	if err != nil {
		tro.GetLogger().Errorf("❌ Failed to create conditional LLM: %v", err)
		return false, "Failed to create conditional LLM: " + err.Error(), err
	}

	// Use the improvement check prompt - focus on specific decision criteria
	context := critiqueResult
	question := `Analyze the critique report and determine if another report refinement iteration is needed.

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
		tro.GetLogger().Errorf("❌ Improvement check decision failed: %v", err)
		return false, "Improvement check failed: " + err.Error(), err
	}

	return result.GetResult(), result.Reason, nil
}

// Agent creation methods
func (tro *TodoReporterOrchestrator) createReportAgent(phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := tro.CreateAndSetupStandardAgent(
		"todo_reporter_report",
		"report-agent",
		phase,
		step,
		iteration,
		tro.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) agents.OrchestratorAgent {
			return todo_optimization.NewReportGenerationAgent(config, logger, tracer, eventBridge)
		},
		tro.WorkspaceTools,
		tro.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (tro *TodoReporterOrchestrator) createCritiqueAgent(phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := tro.CreateAndSetupStandardAgent(
		"todo_reporter_critique",
		"critique-agent",
		phase,
		step,
		iteration,
		tro.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) agents.OrchestratorAgent {
			return todo_optimization.NewDataCritiqueAgent(config, logger, tracer, eventBridge)
		},
		tro.WorkspaceTools,
		tro.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// Execute implements the Orchestrator interface
func (tro *TodoReporterOrchestrator) Execute(ctx context.Context, objective string, options map[string]interface{}) (string, error) {
	// Extract workspace path from options
	workspacePath := ""
	if wp, ok := options["workspacePath"].(string); ok && wp != "" {
		workspacePath = wp
	}

	// Call the existing ExecuteReportGeneration method
	return tro.ExecuteReportGeneration(ctx, objective, workspacePath)
}

// GetType returns the orchestrator type
func (tro *TodoReporterOrchestrator) GetType() string {
	return "todo_reporter"
}
