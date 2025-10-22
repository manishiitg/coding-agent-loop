package todo_creation

import (
	"context"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/llm"

	"github.com/tmc/langchaingo/llms"
)

// TodoPlannerOrchestrator manages the multi-agent todo planning process
type TodoPlannerOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator
}

// NewTodoPlannerOrchestrator creates a new multi-agent todo planner orchestrator
func NewTodoPlannerOrchestrator(
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
) (*TodoPlannerOrchestrator, error) {

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

	return &TodoPlannerOrchestrator{
		BaseOrchestrator: baseOrchestrator,
	}, nil
}

// CreateTodoList orchestrates the multi-agent todo planning process with objective achievement checking
func (tpo *TodoPlannerOrchestrator) CreateTodoList(ctx context.Context, objective, workspacePath string) (string, error) {
	tpo.GetLogger().Infof("🚀 Starting multi-agent todo planning for objective: %s", objective)

	// Set objective and workspace path directly
	tpo.SetObjective(objective)
	tpo.SetWorkspacePath(workspacePath)

	// Balanced iterative approach: optimization-focused early iterations, completion-focused later iterations
	maxExecutionIterations := 10
	var finalExecutionResult string
	var finalValidationResult string
	var finalCritiqueResult string
	var planResult string

	for iteration := 1; iteration <= maxExecutionIterations; iteration++ {
		// Determine strategy based on iteration phase
		strategy := tpo.getIterationStrategy(iteration, maxExecutionIterations)
		tpo.GetLogger().Infof("🔄 Iteration %d/%d: %s", iteration, maxExecutionIterations, strategy.Name)

		// Phase 1: Create/Refine plan based on iteration strategy
		// Pass structured execution results to planning phase
		structuredExecutionResult := tpo.structureExecutionResults(finalExecutionResult)
		planResult, err := tpo.runPlanningPhase(ctx, structuredExecutionResult, finalValidationResult, finalCritiqueResult, iteration, maxExecutionIterations, strategy)

		if err != nil {
			return "", fmt.Errorf("planning phase failed: %w", err)
		}

		// Phase 2: Execute based on iteration strategy
		executionResult, err := tpo.runExecutionPhase(ctx, planResult, iteration, strategy)

		if err != nil {
			return "", fmt.Errorf("execution phase failed: %w", err)
		}

		// Phase 3: Validation based on iteration strategy
		validationResult, err := tpo.runValidationPhase(ctx, planResult, iteration, executionResult, strategy)
		if err != nil {
			tpo.GetLogger().Warnf("⚠️ Validation phase failed: %v", err)
			validationResult = "Validation failed: " + err.Error()
		}

		// Phase 4: Write/Update todo list based on iteration strategy
		_, err = tpo.runWriterPhase(ctx, planResult, executionResult, validationResult, finalCritiqueResult, iteration, strategy)
		if err != nil {
			tpo.GetLogger().Warnf("⚠️ Writer phase failed: %v", err)
		}

		// Phase 5: Critique todo list quality
		critiqueResult, err := tpo.runTodoListCritiquePhase(ctx, tpo.GetObjective(), iteration)
		if err != nil {
			tpo.GetLogger().Warnf("⚠️ Todo list critique phase failed: %v", err)
			critiqueResult = "Todo list critique failed: " + err.Error()
		}

		// Store the current results
		finalExecutionResult = executionResult
		finalValidationResult = validationResult
		finalCritiqueResult = critiqueResult

		// Check if we should continue to next iteration or stop using iteration-aware conditional LLM
		objectiveAchieved, reason, err := tpo.checkObjectiveAchievement(ctx, planResult, critiqueResult, iteration, strategy)
		if err != nil {
			tpo.GetLogger().Warnf("⚠️ Failed to check objective achievement: %v", err)
			// Continue execution on error
		} else if objectiveAchieved {
			tpo.GetLogger().Infof("🎯 Objective achieved at iteration %d (%s): %s", iteration, strategy.Name, reason)
			break
		}
	}

	// Phase 6: Cleanup planning workspace
	cleanupResult, err := tpo.runCleanupPhase(ctx)
	if err != nil {
		tpo.GetLogger().Warnf("⚠️ Cleanup phase failed: %v", err)
	}

	duration := time.Since(tpo.GetStartTime())
	tpo.GetLogger().Infof("✅ Multi-agent todo planning completed in %v", duration)

	return fmt.Sprintf(`# Todo Planning Complete

## Planning Summary
- **Objective**: %s
- **Duration**: %v
- **Workspace**: %s
- **Phases**: Comprehensive Planning → Complete Execution → Validation → Writing → Critique → Cleanup

## Plan Created
%s

## Final Execution Results
%s

## Final Validation Results
%s

## Final Critique Results
%s

## Final Todo List
Todo list has been created and saved to the workspace by the writer agent.

## Cleanup Results
%s

## Next Steps
The todo list has been saved to %s/todo_final.md (moved from todo_creation folder) and is ready for the execution phase.`,
		tpo.GetObjective(), duration, tpo.GetWorkspacePath(),
		planResult, finalExecutionResult, finalValidationResult, finalCritiqueResult, cleanupResult, tpo.GetWorkspacePath()), nil
}

// runPlanningPhase creates or refines the step-wise plan based on iteration strategy
func (tpo *TodoPlannerOrchestrator) runPlanningPhase(ctx context.Context, previousExecutionResult, previousValidationResult, previousCritiqueResult string, iteration int, maxIterations int, strategy IterationStrategy) (string, error) {
	if iteration == 1 {
		planningTemplateVars := map[string]string{
			"Objective":     tpo.GetObjective(),
			"WorkspacePath": tpo.GetWorkspacePath(),
			"Strategy":      strategy.Name,
			"Focus":         strategy.Focus,
			"Iteration":     fmt.Sprintf("%d", iteration),
			"MaxIterations": fmt.Sprintf("%d", maxIterations),
		}

		// Create fresh planning agent with proper context
		planningAgent, err := tpo.createPlanningAgent(ctx, "planning", 0, iteration)
		if err != nil {
			return "", fmt.Errorf("failed to create planning agent: %w", err)
		}

		planResult, _, err := planningAgent.Execute(ctx, planningTemplateVars, nil)
		if err != nil {
			return "", fmt.Errorf("planning failed: %w", err)
		}
		return planResult, nil
	} else {
		planningTemplateVars := map[string]string{
			"Objective":     tpo.GetObjective(),
			"WorkspacePath": tpo.GetWorkspacePath(),
			"Strategy":      strategy.Name,
			"Focus":         strategy.Focus,
			"Iteration":     fmt.Sprintf("%d", iteration),
			"MaxIterations": fmt.Sprintf("%d", maxIterations),
		}

		// Create fresh planning agent with proper context
		planningAgent, err := tpo.createPlanningAgent(ctx, "planning", 0, iteration)
		if err != nil {
			return "", fmt.Errorf("failed to create planning agent: %w", err)
		}

		planResult, _, err := planningAgent.Execute(ctx, planningTemplateVars, nil)
		if err != nil {
			return "", fmt.Errorf("plan refinement failed: %w", err)
		}
		return planResult, nil
	}
}

// runExecutionPhase executes the plan for the current iteration based on strategy
func (tpo *TodoPlannerOrchestrator) runExecutionPhase(ctx context.Context, plan string, iteration int, strategy IterationStrategy) (string, error) {
	executionAgent, err := tpo.createExecutionAgent(ctx, "execution", 0, iteration)
	if err != nil {
		return "", fmt.Errorf("failed to create execution agent: %w", err)
	}

	// Prepare template variables for Execute method
	// Execution agent is tactical - just needs the plan and workspace
	templateVars := map[string]string{
		"Plan":          plan,
		"WorkspacePath": tpo.GetWorkspacePath(),
	}

	executionResult, _, err := executionAgent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("execution failed: %w", err)
	}

	return executionResult, nil
}

// runValidationPhase validates the execution results for the current iteration based on strategy
func (tpo *TodoPlannerOrchestrator) runValidationPhase(ctx context.Context, plan string, iteration int, executionResult string, strategy IterationStrategy) (string, error) {
	validationAgent, err := tpo.createValidationAgent(ctx, "validation", 0, iteration)
	if err != nil {
		return "", fmt.Errorf("failed to create validation agent: %w", err)
	}

	// Validation agent is tactical - just validates execution results with evidence
	validationTemplateVars := map[string]string{
		"ExecutionResult": executionResult,
		"WorkspacePath":   tpo.GetWorkspacePath(),
		"Iteration":       fmt.Sprintf("%d", iteration),
	}

	validationResult, _, err := validationAgent.Execute(ctx, validationTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}

	return validationResult, nil
}

// runWriterPhase creates optimal todo list based on plan and execution experience using strategy
func (tpo *TodoPlannerOrchestrator) runWriterPhase(ctx context.Context, planResult, executionResult, validationResult, critiqueResult string, iteration int, strategy IterationStrategy) (string, error) {
	writerAgent, err := tpo.createWriterAgent(ctx, "writing", 0, iteration)
	if err != nil {
		return "", fmt.Errorf("failed to create writer agent: %w", err)
	}

	// Prepare template variables for Execute method
	// Writer is strategic - needs to synthesize from all iterations
	writerTemplateVars := map[string]string{
		"Objective":        tpo.GetObjective(),
		"PlanResult":       planResult,
		"ExecutionResult":  executionResult,
		"ValidationResult": validationResult,
		"CritiqueResult":   critiqueResult,
		"WorkspacePath":    tpo.GetWorkspacePath(),
		"TotalIterations":  fmt.Sprintf("%d", iteration),
	}

	todoListResult, _, err := writerAgent.Execute(ctx, writerTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("todo list creation failed: %w", err)
	}

	return todoListResult, nil
}

// runCleanupPhase cleans up the planning workspace
func (tpo *TodoPlannerOrchestrator) runCleanupPhase(ctx context.Context) (string, error) {
	cleanupAgent, err := tpo.createCleanupAgent(ctx, "cleanup", 0, 0)
	if err != nil {
		return "", fmt.Errorf("failed to create cleanup agent: %w", err)
	}

	cleanupTemplateVars := map[string]string{
		"WorkspacePath": tpo.GetWorkspacePath(),
	}

	cleanupResult, _, err := cleanupAgent.Execute(ctx, cleanupTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("cleanup failed: %w", err)
	}

	return cleanupResult, nil
}

// runTodoListCritiquePhase critiques the todo list quality and reproducibility
func (tpo *TodoPlannerOrchestrator) runTodoListCritiquePhase(ctx context.Context, objective string, iteration int) (string, error) {
	critiqueAgent, err := tpo.createCritiqueAgent(ctx, "critique", 0, iteration)
	if err != nil {
		return "", fmt.Errorf("failed to create critique agent: %w", err)
	}

	// Prepare template variables for TodoPlannerCritiqueAgent
	templateVars := map[string]string{
		"Objective":     objective,
		"Iteration":     fmt.Sprintf("%d", iteration),
		"WorkspacePath": tpo.GetWorkspacePath(),
	}

	// Execute todo list critique
	critiqueResult, _, err := critiqueAgent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("todo list critique failed: %w", err)
	}

	return critiqueResult, nil
}

// createConditionalLLM creates a conditional LLM on-demand with todo planner-specific configuration
func (tpo *TodoPlannerOrchestrator) createConditionalLLM() (*llm.ConditionalLLM, error) {
	// Create config for conditional LLM using todo planner-specific settings
	conditionalConfig := &agents.OrchestratorAgentConfig{
		Provider:      tpo.GetProvider(),
		Model:         tpo.GetModel(),
		Temperature:   tpo.GetTemperature(),
		ServerNames:   tpo.GetSelectedServers(),
		MCPConfigPath: tpo.GetMCPConfigPath(),
	}

	// Create conditional LLM with todo planner-specific context
	conditionalLLM, err := llm.CreateConditionalLLMWithEventBridge(conditionalConfig, tpo.GetContextAwareBridge(), tpo.GetLogger(), tpo.GetTracer())
	if err != nil {
		return nil, fmt.Errorf("failed to create conditional LLM: %w", err)
	}

	return conditionalLLM, nil
}

// checkObjectiveAchievement uses conditional LLM to determine if the objective was achieved based on iteration strategy
func (tpo *TodoPlannerOrchestrator) checkObjectiveAchievement(ctx context.Context, planResult, critiqueResult string, iteration int, strategy IterationStrategy) (bool, string, error) {
	// Create conditional LLM on-demand
	conditionalLLM, err := tpo.createConditionalLLM()
	if err != nil {
		tpo.GetLogger().Errorf("❌ Failed to create conditional LLM: %v", err)
		return false, "Failed to create conditional LLM: " + err.Error(), err
	}

	// Prepare context for objective achievement assessment based on iteration strategy
	assessmentContext := fmt.Sprintf(`Objective: %s

Plan:
%s

Critique Results:
%s

Iteration Strategy: %s
Focus: %s
Iteration: %d

Assessment Context:
- This is a balanced todo creation workflow that adapts strategy based on iteration phase
- Early iterations (1-3): Focus on discovering optimal methods and approaches
- Middle iterations (4-6): Focus on validating and refining optimal methods
- Later iterations (7-10): Focus on completing the objective using proven optimal methods
- Current strategy: %s
- Current focus: %s`,
		tpo.GetObjective(), planResult, critiqueResult, strategy.Name, strategy.Focus, iteration, strategy.Name, strategy.Focus)

	question := fmt.Sprintf(`Based on the plan, critique analysis, and current iteration strategy (%s), %s

Consider:
1. **Strategy Alignment**: Are we achieving the goals of the current iteration strategy?
2. **Method Discovery**: Have we discovered optimal methods for critical steps?
3. **Method Validation**: Have we validated that our methods work consistently?
4. **Objective Completion**: Have we completed the objective using optimal methods?
5. **Quality Assessment**: Is the todo list comprehensive, clear, and reproducible?

%s`,
		strategy.Name, strategy.StoppingCriteria, strategy.StoppingCriteria)

	// Use conditional LLM to make the decision
	result, err := conditionalLLM.Decide(ctx, assessmentContext, question, 0, 0)

	if err != nil {
		tpo.GetLogger().Errorf("❌ Conditional LLM decision failed: %v", err)
		return false, "Conditional decision failed: " + err.Error(), err
	}

	return result.GetResult(), result.Reason, nil
}

// Agent creation methods
func (tpo *TodoPlannerOrchestrator) createPlanningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Create fresh agent for each execution with proper context
	agent, err := tpo.CreateAndSetupStandardAgent(
		ctx,
		"planning-agent",
		phase,
		step,
		iteration,
		tpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewTodoPlannerPlanningAgent(config, logger, tracer, eventBridge)
		},
		tpo.WorkspaceTools,
		tpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (tpo *TodoPlannerOrchestrator) createExecutionAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Create fresh agent for each execution with proper context
	agent, err := tpo.CreateAndSetupStandardAgent(
		ctx,
		"execution-agent",
		phase,
		step,
		iteration,
		tpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewTodoPlannerExecutionAgent(config, logger, tracer, eventBridge)
		},
		tpo.WorkspaceTools,
		tpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (tpo *TodoPlannerOrchestrator) createValidationAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Create fresh agent for each execution with proper context
	agent, err := tpo.CreateAndSetupStandardAgent(
		ctx,
		"validation-agent",
		phase,
		step,
		iteration,
		tpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewTodoPlannerValidationAgent(config, logger, tracer, eventBridge)
		},
		tpo.WorkspaceTools,
		tpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (tpo *TodoPlannerOrchestrator) createWriterAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Create fresh agent for each execution with proper context
	agent, err := tpo.CreateAndSetupStandardAgent(
		ctx,
		"writer-agent",
		phase,
		step,
		iteration,
		tpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewTodoPlannerWriterAgent(config, logger, tracer, eventBridge)
		},
		tpo.WorkspaceTools,
		tpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (tpo *TodoPlannerOrchestrator) createCleanupAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Create fresh agent for each execution with proper context
	agent, err := tpo.CreateAndSetupStandardAgent(
		ctx,
		"cleanup-agent",
		phase,
		step,
		iteration,
		tpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewTodoPlannerCleanupAgent(config, logger, tracer, eventBridge)
		},
		tpo.WorkspaceTools,
		tpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (tpo *TodoPlannerOrchestrator) createCritiqueAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Create fresh agent for each execution with proper context
	agent, err := tpo.CreateAndSetupStandardAgent(
		ctx,
		"critique-agent",
		phase,
		step,
		iteration,
		tpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewTodoPlannerCritiqueAgent(config, logger, tracer, eventBridge)
		},
		tpo.WorkspaceTools,
		tpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// structureExecutionResults processes execution results to extract optimized steps
func (tpo *TodoPlannerOrchestrator) structureExecutionResults(executionResult string) string {
	if executionResult == "" {
		return ""
	}

	// Add structured header to help planning agent parse optimized steps
	structuredResult := fmt.Sprintf(`# STRUCTURED EXECUTION RESULTS FOR PLANNING REFINEMENT

## RAW EXECUTION RESULTS
%s

## OPTIMIZATION PARSING INSTRUCTIONS
The planning agent should extract the following from the execution results above:
1. Steps marked as OPTIMIZED - these have optimal methods and should NOT be re-planned
2. Steps marked as COMPLETED - these are done and should be skipped
3. Steps marked as IN_PROGRESS - these are being optimized and should continue
4. Steps marked as PENDING - these need optimization in the next iteration

## KEY SECTIONS TO PARSE
- "OPTIMIZED STEPS SUMMARY" - Contains steps with optimal methods
- "Step Optimization Status" - Shows which steps are optimized
- "Step Optimization Details" - Contains optimal methods and commands
- "Accomplished Steps" - Shows completed steps with evidence

Focus on preserving optimized methods and only refining steps that need improvement.`, executionResult)

	return structuredResult
}

// IterationStrategy defines the approach for each iteration phase
type IterationStrategy struct {
	Name             string
	Focus            string
	PlanningPhase    string
	ExecutionPhase   string
	ValidationPhase  string
	WriterPhase      string
	StoppingCriteria string
}

// getIterationStrategy determines the strategy based on current iteration
func (tpo *TodoPlannerOrchestrator) getIterationStrategy(iteration, maxIterations int) IterationStrategy {
	// Early iterations (1-3): Optimization & Method Discovery
	if iteration <= 3 {
		return IterationStrategy{
			Name:             "Optimization & Method Discovery",
			Focus:            "Find the best possible methods and approaches",
			PlanningPhase:    "Creating exploration plan to discover optimal methods",
			ExecutionPhase:   "Exploring and discovering optimal methods for each step",
			ValidationPhase:  "Validating discovered methods and approaches",
			WriterPhase:      "Creating todo list based on discovered optimal methods",
			StoppingCriteria: "Have we discovered the best possible methods for all critical steps?",
		}
	}

	// Middle iterations (4-6): Refinement & Validation
	if iteration <= 6 {
		return IterationStrategy{
			Name:             "Refinement & Validation",
			Focus:            "Refine the best methods and validate they work consistently",
			PlanningPhase:    "Refining plan based on proven optimal methods",
			ExecutionPhase:   "Testing and validating optimal methods",
			ValidationPhase:  "Validating method reliability and reproducibility",
			WriterPhase:      "Updating todo list with validated optimal methods",
			StoppingCriteria: "Have we validated that our optimal methods work consistently?",
		}
	}

	// Later iterations (7-10): Completion & Execution
	return IterationStrategy{
		Name:             "Completion & Execution",
		Focus:            "Complete the remaining steps using proven optimal methods",
		PlanningPhase:    "Finalizing plan with proven optimal methods",
		ExecutionPhase:   "Executing remaining steps using proven optimal methods",
		ValidationPhase:  "Validating completion of remaining steps",
		WriterPhase:      "Finalizing todo list with proven optimal methods",
		StoppingCriteria: "Have we completed the objective using optimal methods?",
	}
}

// Execute implements the Orchestrator interface
func (tpo *TodoPlannerOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	// Validate that no options are provided since this orchestrator doesn't use them
	if len(options) > 0 {
		return "", fmt.Errorf("todo planner orchestrator does not accept options")
	}

	// Validate workspace path is provided
	if workspacePath == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	// Call the existing CreateTodoList method
	return tpo.CreateTodoList(ctx, objective, workspacePath)
}

// GetType returns the orchestrator type
func (tpo *TodoPlannerOrchestrator) GetType() string {
	return "todo_planner"
}
