package step_based_workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// ExecuteEvaluationOnly runs only the evaluation execution phase
func (hcpo *StepBasedWorkflowOrchestrator) ExecuteEvaluationOnly(ctx context.Context, objective, workspacePath, targetRunFolder string) (string, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("🚀 Starting evaluation execution for objective: %s", objective))

	// Set objective and workspace path
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)

	// Check if evaluation_plan.json exists
	evalPlanPath := "evaluation/evaluation_plan.json"
	planExists, evaluationPlan, err := hcpo.checkExistingEvaluationPlan(ctx, evalPlanPath)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing evaluation plan: %w", err)
	}
	if !planExists {
		return "", fmt.Errorf("evaluation_plan.json not found at %s - evaluation planning must be run first", evalPlanPath)
	}

	// Use a special run folder for evaluation results
	// This will cause outputs to go to evaluation/runs/<targetRunFolder>/execution/ and progress to evaluation/runs/<targetRunFolder>/steps_done.json
	if targetRunFolder == "" {
		return "", fmt.Errorf("targetRunFolder is required for evaluation execution")
	}
	
	// We use ".." to step out of the standard "runs/" folder that the orchestrator assumes,
	// and point to "evaluation/runs/<targetRunFolder>"
	hcpo.selectedRunFolder = filepath.Join("..", "evaluation", "runs", targetRunFolder)
	hcpo.GetLogger().Info(fmt.Sprintf("📁 Using evaluation folder for execution results: evaluation/runs/%s", targetRunFolder))

	// Load runtime variable values if available (needed for variable resolution in evaluation steps)
	variableValues, err := LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load variable values for evaluation: %v", err))
		hcpo.variableValues = make(map[string]string)
	} else {
		hcpo.variableValues = variableValues
	}

	// Inject TARGET_RUN_PATH so evaluation steps can find the artifacts they need to check
	// targetRunFolder is e.g. "iteration-1"
	// artifacts are in workspace/runs/iteration-1/execution
	targetRunPath := filepath.Join(hcpo.GetWorkspacePath(), "runs", targetRunFolder, "execution")
	hcpo.variableValues["TARGET_RUN_PATH"] = targetRunPath
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Injected TARGET_RUN_PATH variable for evaluation: %s", targetRunPath))

	// Convert evaluation steps to PlanStepInterface
	breakdownSteps := evaluationPlan.ToPlanSteps()

	// Initialize or load progress
	progress, err := hcpo.loadStepProgress(ctx)
	if err != nil {
		hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No existing evaluation progress found, starting fresh"))
		progress = &StepProgress{
			CompletedStepIndices:     []int{},
			TotalSteps:               len(breakdownSteps),
			LastUpdated:              time.Now(),
			BranchSteps:              make(map[int]BranchStepProgress),
			ValidationFailures:       make(map[string]int),
			DecisionEvaluationCounts: make(DecisionEvaluationCount),
		}
	}

	// Build execution context (skip human input by default for automated evaluation?)
	// For now, follow orchestrator settings
	execCtx := hcpo.buildExecutionContext()

	// Run execution phase
	err = hcpo.runExecutionPhase(ctx, breakdownSteps, 1, progress, 0, execCtx)
	if err != nil {
		return "", fmt.Errorf("evaluation execution phase failed: %w", err)
	}

	return "Evaluation execution complete.", nil
}
