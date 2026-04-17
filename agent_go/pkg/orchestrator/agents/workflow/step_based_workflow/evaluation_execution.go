package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	orchevents "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
)

// ExecuteEvaluationOnly runs only the evaluation execution phase
func (hcpo *StepBasedWorkflowOrchestrator) ExecuteEvaluationOnly(ctx context.Context, objective, workspacePath, targetRunFolder string) (string, error) {
	hcpo.GetLogger().Info("🚀 Starting evaluation execution")

	// Set objective and workspace path
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)
	// Fallback: resolve objective from soul/soul.md if caller passed empty. See
	// CreateTodoList for the same pattern — keeps learning-agent CurrentObjective populated.
	if strings.TrimSpace(hcpo.GetObjective()) == "" {
		if resolved, _ := hcpo.ResolveWorkflowObjective(ctx); resolved != "" {
			hcpo.SetObjective(resolved)
		}
	}

	// Check if evaluation_plan.json exists
	// Note: evaluation_plan.json is stored in evaluation/ directory (not planning/) per documentation
	evalPlanPath := "evaluation/evaluation_plan.json"
	planExists, evaluationPlan, err := hcpo.checkExistingEvaluationPlan(ctx, evalPlanPath)
	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to check evaluation plan: %v", err), nil)
		return "", fmt.Errorf("failed to check for existing evaluation plan: %w", err)
	}
	if !planExists {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Evaluation plan not found at %s", evalPlanPath), nil)
		return "", fmt.Errorf("evaluation_plan.json not found at %s - evaluation planning must be run first", evalPlanPath)
	}

	// Use a special run folder for evaluation results.
	// Like report generation, evaluation should execute inside the workshop-style
	// iteration-0 sandbox while still reading artifacts from the requested target run.
	// Eval always runs against the workflow's current iteration-0. Historical
	// re-scoring is intentionally not supported — workflow + eval rotate together
	// in resolveRunFolderWithOptions, so evaluation/runs/iteration-N is paired
	// with runs/iteration-N by construction. We preserve any group suffix the
	// caller passed (e.g. "iteration-19/manishiitg" -> "iteration-0/manishiitg")
	// since multi-group runs share an iteration but split per-group inside it.
	if targetRunFolder == "" {
		targetRunFolder = "iteration-0"
	}
	originalTarget := targetRunFolder
	targetRunFolder = workshopInternalRunFolderForTarget(targetRunFolder)
	if targetRunFolder != "iteration-0" && !strings.HasPrefix(targetRunFolder, "iteration-0/") {
		// workshopInternalRunFolderForTarget always returns iteration-0[/group],
		// so this is defense-in-depth in case the helper changes.
		targetRunFolder = "iteration-0"
	}
	if targetRunFolder != originalTarget {
		hcpo.GetLogger().Info(fmt.Sprintf("📍 Eval target normalized: %q -> %q (eval always runs against current run)", originalTarget, targetRunFolder))
	}
	internalEvalRunFolder := targetRunFolder

	// We use ".." to step out of the standard "runs/" folder that the orchestrator assumes,
	// and point to "evaluation/runs/<internalEvalRunFolder>".
	hcpo.selectedRunFolder = filepath.Join("..", "evaluation", "runs", internalEvalRunFolder)

	// Set iteration folder for token persistence - this ensures token_usage.json goes to
	// evaluation/runs/<internalEvalRunFolder>/
	hcpo.SetIterationFolder(hcpo.selectedRunFolder)

	// Load runtime variable values if available (needed for variable resolution in evaluation steps)
	variableValues, err := LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load variable values: %v", err))
		hcpo.variableValues = make(map[string]string)
	} else {
		hcpo.variableValues = variableValues
	}

	// Inject TARGET_RUN_PATH so evaluation steps can find the artifacts they need to check
	// targetRunFolder is e.g. "iteration-1" or "iteration-26/atul"
	// Absolute path so eval steps can use it directly in shell commands
	docsRoot := GetPromptDocsRoot()
	targetRunPath := filepath.Join(docsRoot, hcpo.GetWorkspacePath(), "runs", targetRunFolder, "execution")
	hcpo.variableValues["TARGET_RUN_PATH"] = targetRunPath

	// Convert evaluation steps to PlanStepInterface
	breakdownSteps := evaluationPlan.ToPlanSteps()

	// Set evaluation mode flag — controls step_config.json lookup (evaluation/step_config.json)
	// and learning-phase skipping for eval steps. Learnings themselves share the learnings/ namespace
	// with execution steps; cross-plan step-ID uniqueness is enforced separately.
	hcpo.isEvaluationMode = true

	// Configure evaluation steps: apply configs from step_config.json, disable validation, enable learning
	// Validation is disabled (steps auto-approve), learning is enabled to capture insights from evaluation runs
	for i, step := range breakdownSteps {
		// Apply configuration from evaluation/step_config.json if it exists
		if err := ApplyStepConfigFromFile(ctx, step, hcpo); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to apply step config for step %d: %v", i+1, err))
		}

		if evalStep, ok := step.(*EvaluationStep); ok {
			// Keep learning enabled (we want to learn from evaluation runs)
			// DisableLearning is not set (nil = enabled by default)
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Evaluation step %d (%s): learning enabled", i+1, evalStep.Title))
		}
	}

	// Initialize or load progress
	progress, err := hcpo.loadStepProgress(ctx)
	if err != nil {
		hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No existing evaluation progress file found, initializing fresh progress: %v", err))
		progress = &StepProgress{
			CompletedStepIndices:     []int{},
			TotalSteps:               len(breakdownSteps),
			LastUpdated:              time.Now(),
			BranchSteps:              make(map[int]BranchStepProgress),
			ValidationFailures:       make(map[string]int),
			DecisionEvaluationCounts: make(DecisionEvaluationCount),
		}
	}

	// Build execution context with human input skipped for automated evaluation
	hcpo.SetSkipHumanInput(true) // Evaluation runs are always automated - no human feedback prompts
	execCtx := hcpo.buildExecutionContext()
	execCtx.SkipHumanInput = true // Ensure execution context also has this set

	// Run execution phase
	err = hcpo.runExecutionPhase(ctx, breakdownSteps, 1, progress, 0, execCtx)
	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Evaluation execution failed: %v", err), nil)
		return "", fmt.Errorf("evaluation execution phase failed: %w", err)
	}

	hcpo.GetLogger().Info("✅ Evaluation execution completed successfully")

	// Run scoring phase after all evaluation steps complete
	hcpo.GetLogger().Info("📊 Starting evaluation scoring phase")
	report, err := hcpo.runEvaluationScoringPhase(ctx, evaluationPlan, targetRunFolder, internalEvalRunFolder)
	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Evaluation scoring failed: %v", err), nil)
		return "", fmt.Errorf("evaluation scoring phase failed: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Evaluation complete. Total Score: %d/%d (%.1f%%)",
		report.TotalScore, report.MaxPossibleScore, report.ScorePercentage))

	return fmt.Sprintf("Evaluation complete. Score: %d/%d (%.1f%%)", report.TotalScore, report.MaxPossibleScore, report.ScorePercentage), nil
}

// runEvaluationScoringPhase collects all eval step outputs and runs a single scoring agent
// that scores all steps at once with holistic analysis.
func (hcpo *StepBasedWorkflowOrchestrator) runEvaluationScoringPhase(ctx context.Context, evaluationPlan *EvaluationPlan, targetRunFolder string, internalEvalRunFolder string) (*EvaluationReport, error) {
	evalExecutionPath := filepath.Join("evaluation", "runs", internalEvalRunFolder, "execution")

	// Collect all step outputs
	var stepInputs []EvaluationStepInput
	for i, step := range evaluationPlan.Steps {
		legacyStepPath := fmt.Sprintf("step-%d", i+1)

		hcpo.GetLogger().Info(fmt.Sprintf("📂 Reading output for step %d: %s", i+1, step.Title))

		executionOutput, err := hcpo.readStepExecutionOutput(ctx, evalExecutionPath, step.ID, legacyStepPath)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Could not read execution output for step %d: %v", i+1, err))
			executionOutput = fmt.Sprintf("Error reading output: %v", err)
		}

		stepInputs = append(stepInputs, EvaluationStepInput{
			ID:              step.ID,
			Title:           step.Title,
			Description:     step.Description,
			SuccessCriteria: step.SuccessCriteria,
			ExecutionOutput: executionOutput,
		})
	}

	// Run single scoring agent with all steps
	hcpo.GetLogger().Info(fmt.Sprintf("📊 Running scoring agent for all %d evaluation steps", len(stepInputs)))

	// Inject correlation ID so scoring agent events are tagged for frontend auto-notifications
	scoringSessionID := fmt.Sprintf("workshop-eval-scoring-%s-%d", targetRunFolder, time.Now().UnixNano())
	scoringCtx := context.WithValue(ctx, orchevents.AgentSessionIDKey, scoringSessionID)
	scoringCtx = context.WithValue(scoringCtx, orchevents.ForceCorrelationIDKey, scoringSessionID)
	scoringCtx = context.WithValue(scoringCtx, orchevents.IsSubAgentContextKey, true)

	report, err := hcpo.scoreAllSteps(scoringCtx, evaluationPlan, stepInputs, targetRunFolder)
	if err != nil {
		return nil, fmt.Errorf("scoring agent failed: %w", err)
	}

	// Save report
	internalReportPath := filepath.Join("evaluation", "runs", internalEvalRunFolder, "evaluation_report.json")
	publishedReportPath := filepath.Join("evaluation", "runs", targetRunFolder, "evaluation_report.json")
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return report, fmt.Errorf("failed to marshal evaluation report: %w", err)
	}

	if err := hcpo.WriteWorkspaceFile(ctx, internalReportPath, string(reportJSON)); err != nil {
		return report, fmt.Errorf("failed to write internal evaluation report: %w", err)
	}
	if filepath.ToSlash(internalReportPath) != filepath.ToSlash(publishedReportPath) {
		if err := hcpo.WriteWorkspaceFile(ctx, publishedReportPath, string(reportJSON)); err != nil {
			return report, fmt.Errorf("failed to publish evaluation report: %w", err)
		}
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📄 Evaluation report saved to internal path: %s", internalReportPath))
	if filepath.ToSlash(internalReportPath) != filepath.ToSlash(publishedReportPath) {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Evaluation report published to target path: %s", publishedReportPath))
	}
	return report, nil
}

// readStepExecutionOutput reads all relevant output files from evaluation step execution
// It looks in multiple locations since execution outputs are stored in logs folders
func (hcpo *StepBasedWorkflowOrchestrator) readStepExecutionOutput(ctx context.Context, evalExecutionPath string, stepID string, legacyStepPath string) (string, error) {
	var outputs []string
	folderCandidates := []string{getArtifactFolderName(stepID, legacyStepPath)}
	if legacyStepPath != "" && legacyStepPath != folderCandidates[0] {
		folderCandidates = append(folderCandidates, legacyStepPath)
	}

	seenOutputs := make(map[string]bool)
	appendOutput := func(label string, content string) {
		if strings.TrimSpace(content) == "" {
			return
		}
		entry := fmt.Sprintf("=== %s ===\n%s", label, content)
		if seenOutputs[entry] {
			return
		}
		seenOutputs[entry] = true
		outputs = append(outputs, entry)
	}

	for _, stepFolder := range folderCandidates {
		executionStepPath := filepath.Join(evalExecutionPath, stepFolder)

		hcpo.GetLogger().Info(fmt.Sprintf("📂 Looking for step output files in: %s", executionStepPath))

		// Read step output files from execution folder (the actual results to score)
		execFiles, listErr := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, executionStepPath)
		if listErr == nil && len(execFiles) > 0 {
			for _, filename := range execFiles {
				if strings.HasSuffix(filename, ".json") || strings.HasSuffix(filename, ".md") {
					filePath := filepath.Join(executionStepPath, filename)
					content, err := hcpo.ReadWorkspaceFile(ctx, filePath)
					if err == nil && content != "" {
						appendOutput(filename, content)
					}
				}
			}
		} else {
			stepOutputFiles := []string{
				"final_verification_report.json",
				"verification_report.json",
				"verification_summary.json",
				"verification_report.md",
				"output.json",
				"result.json",
				"summary.json",
				"step_done.json",
				"context_output.json",
			}
			for _, filename := range stepOutputFiles {
				filePath := filepath.Join(executionStepPath, filename)
				content, err := hcpo.ReadWorkspaceFile(ctx, filePath)
				if err == nil && content != "" {
					appendOutput(filename, content)
				}
			}
		}
	}

	if len(outputs) == 0 {
		return "No execution output files found. The evaluation step may not have run or produced no output.", nil
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📄 Found %d output file(s) for evaluation scoring", len(outputs)))
	return strings.Join(outputs, "\n\n"), nil
}

// scoreAllSteps runs a single scoring agent that scores all evaluation steps at once.
func (hcpo *StepBasedWorkflowOrchestrator) scoreAllSteps(ctx context.Context, evaluationPlan *EvaluationPlan, stepInputs []EvaluationStepInput, targetRunFolder string) (*EvaluationReport, error) {
	report, err := hcpo.createEvaluationScoringAgent(ctx, "evaluation-scoring", evaluationPlan, stepInputs)
	if err != nil {
		return nil, err
	}

	report.TargetRunFolder = targetRunFolder
	report.GeneratedAt = time.Now().Format(time.RFC3339)
	report.MaxPossibleScore = len(evaluationPlan.Steps) * 10

	// Fill in any steps that the scoring agent missed
	scoredStepIDs := make(map[string]bool)
	for _, s := range report.StepScores {
		scoredStepIDs[s.StepID] = true
	}
	for _, step := range evaluationPlan.Steps {
		if !scoredStepIDs[step.ID] {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Scoring agent did not score step %s — adding zero score", step.ID))
			report.StepScores = append(report.StepScores, &EvaluationStepScore{
				StepID:          step.ID,
				StepTitle:       step.Title,
				Score:           0,
				MaxScore:        10,
				Reasoning:       "Scoring agent did not provide a score for this step",
				SuccessCriteria: step.SuccessCriteria,
			})
		}
	}

	// Calculate totals
	report.TotalScore = 0
	for _, s := range report.StepScores {
		report.TotalScore += s.Score
	}
	if report.MaxPossibleScore > 0 {
		report.ScorePercentage = float64(report.TotalScore) / float64(report.MaxPossibleScore) * 100
	}

	return report, nil
}

// ============================================================================
// AUTO-EVALUATION (runs automatically after normal execution if evaluation_plan.json exists)
// ============================================================================

// MaybeRunAutoEvaluation checks if evaluation_plan.json exists and automatically runs
// the full evaluation process (same as manual evaluation) after normal execution completes.
func (hcpo *StepBasedWorkflowOrchestrator) MaybeRunAutoEvaluation(ctx context.Context) error {
	// Check if evaluation_plan.json exists
	evalPlanPath := "evaluation/evaluation_plan.json"
	planExists, _, err := hcpo.checkExistingEvaluationPlan(ctx, evalPlanPath)
	if err != nil {
		return fmt.Errorf("failed to check evaluation plan: %w", err)
	}
	if !planExists {
		hcpo.GetLogger().Info("ℹ️ No evaluation_plan.json found - skipping auto-evaluation")
		return nil // Not an error - just no evaluation plan defined
	}

	// Get the target run folder from the current execution
	targetRunFolder := hcpo.selectedRunFolder
	if targetRunFolder == "" {
		hcpo.GetLogger().Warn("⚠️ No run folder set - skipping auto-evaluation")
		return nil
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📊 Starting auto-evaluation for run folder: %s", targetRunFolder))

	// Call the same evaluation process as manual evaluation
	// This executes evaluation steps and scores them
	_, err = hcpo.ExecuteEvaluationOnly(ctx, hcpo.GetObjective(), hcpo.GetWorkspacePath(), targetRunFolder)
	if err != nil {
		return fmt.Errorf("auto-evaluation failed: %w", err)
	}

	return nil
}
