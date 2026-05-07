package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
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
	evaluationPlan, skippedStepScores := hcpo.filterEvaluationPlanForTargetRun(ctx, evaluationPlan, targetRunFolder)
	if len(skippedStepScores) > 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipped %d non-applicable evaluation step(s) for target run %s", len(skippedStepScores), targetRunFolder))
	}
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
			CompletedStepIndices:    []int{},
			TotalSteps:              len(breakdownSteps),
			LastUpdated:             time.Now(),
			BranchSteps:             make(map[int]BranchStepProgress),
			ValidationFailures:      make(map[string]int),
			RoutingEvaluationCounts: make(RoutingEvaluationCount),
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

	// Build the evaluation report directly from individual eval step outputs.
	// There is no final scoring agent: metric extraction reads the structured
	// output_content for each eval step.
	hcpo.GetLogger().Info("📊 Building evaluation report from step outputs")
	report, err := hcpo.runEvaluationReportPhase(ctx, evaluationPlan, targetRunFolder, internalEvalRunFolder, skippedStepScores)
	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Evaluation report build failed: %v", err), nil)
		return "", fmt.Errorf("evaluation report build failed: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Evaluation complete. Captured %d evaluation step output(s)", len(report.StepScores)))

	// Snapshot per-run metric values now that the evaluation_report.json has
	// been published to evaluation/runs/<originalTarget>/. This fires for both
	// workflow-run-triggered eval (via MaybeRunAutoEvaluation) and standalone
	// eval (triggered from the builder), since both flow through here.
	// No-op when planning/metrics.json is absent.
	hcpo.snapshotRunMetrics(ctx, originalTarget)

	return fmt.Sprintf("Evaluation complete. Captured %d evaluation step output(s)", len(report.StepScores)), nil
}

// runEvaluationReportPhase builds the durable evaluation_report.json directly
// from eval step outputs. There is intentionally no final scoring agent in this phase.
func (hcpo *StepBasedWorkflowOrchestrator) runEvaluationReportPhase(ctx context.Context, evaluationPlan *EvaluationPlan, targetRunFolder string, internalEvalRunFolder string, skippedStepScores []*EvaluationStepScore) (*EvaluationReport, error) {
	evalExecutionPath := filepath.Join("evaluation", "runs", internalEvalRunFolder, "execution")
	evalReportFolder := filepath.Join("evaluation", "runs", internalEvalRunFolder)

	report := &EvaluationReport{
		TargetRunFolder: targetRunFolder,
		GeneratedAt:     time.Now().Format(time.RFC3339),
		StepScores:      make([]*EvaluationStepScore, 0, len(evaluationPlan.Steps)+len(skippedStepScores)),
	}

	for _, step := range evaluationPlan.Steps {
		if step == nil {
			continue
		}
		report.StepScores = append(report.StepScores, &EvaluationStepScore{
			StepID:    step.ID,
			Score:     0,
			MaxScore:  0,
			Reasoning: "Final scoring is disabled; this report preserves the eval step output for metrics and review.",
			Evidence:  "Inspect output_content for the eval step's structured verdict and evidence.",
		})
	}
	report.StepScores = append(report.StepScores, skippedStepScores...)
	hcpo.enrichEvaluationReportWithStepOutputs(ctx, report, evalExecutionPath)

	internalReportPath := filepath.Join(evalReportFolder, EvaluationReportFileName)
	publishedReportPath := filepath.Join("evaluation", "runs", targetRunFolder, EvaluationReportFileName)
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
	if err := hcpo.persistEvaluationScoreLedger(ctx, report, targetRunFolder); err != nil {
		return report, fmt.Errorf("failed to persist evaluation report ledger: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📄 Evaluation report saved to internal path: %s", internalReportPath))
	if filepath.ToSlash(internalReportPath) != filepath.ToSlash(publishedReportPath) {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Evaluation report published to target path: %s", publishedReportPath))
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📚 Evaluation report ledger updated for target path: %s", targetRunFolder))
	return report, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) enrichEvaluationReportWithStepOutputs(ctx context.Context, report *EvaluationReport, evalExecutionPath string) {
	if report == nil {
		return
	}
	for _, score := range report.StepScores {
		if score == nil || score.OutputContent != nil || score.Skipped {
			continue
		}
		stepFolder := getArtifactFolderName(score.StepID, "")
		if stepFolder == "" {
			continue
		}
		candidates := []string{
			filepath.Join(evalExecutionPath, stepFolder, "output_content.json"),
			filepath.Join(evalExecutionPath, stepFolder, "context_output.json"),
		}
		for _, candidate := range candidates {
			raw, err := hcpo.ReadWorkspaceFile(ctx, candidate)
			if err != nil || strings.TrimSpace(raw) == "" {
				continue
			}
			score.OutputContent = buildStepOutputContent(candidate, raw)
			break
		}
	}
}

func buildStepOutputContent(filePath string, raw string) *StepOutputContent {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	var decoded interface{}
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		if envelope, ok := decoded.(map[string]interface{}); ok {
			if content, hasContent := envelope["content"]; hasContent {
				if _, hasIsJSON := envelope["is_json"]; hasIsJSON {
					isJSON, _ := envelope["is_json"].(bool)
					return &StepOutputContent{
						FilePath: filePath,
						Content:  content,
						IsJSON:   isJSON,
					}
				}
			}
		}
		return &StepOutputContent{
			FilePath: filePath,
			Content:  decoded,
			IsJSON:   true,
		}
	}

	return &StepOutputContent{
		FilePath: filePath,
		Content:  trimmed,
		IsJSON:   false,
	}
}

// filterEvaluationPlanForTargetRun removes eval steps that are explicitly scoped
// to a route absent from the selected workflow run.
func (hcpo *StepBasedWorkflowOrchestrator) filterEvaluationPlanForTargetRun(ctx context.Context, evaluationPlan *EvaluationPlan, targetRunFolder string) (*EvaluationPlan, []*EvaluationStepScore) {
	if evaluationPlan == nil || len(evaluationPlan.Steps) == 0 {
		return evaluationPlan, nil
	}

	active := make([]*EvaluationStep, 0, len(evaluationPlan.Steps))
	skipped := make([]*EvaluationStepScore, 0)
	for _, step := range evaluationPlan.Steps {
		if step == nil {
			continue
		}
		if applies, reason := hcpo.evaluationStepAppliesToTargetRun(ctx, step, targetRunFolder); !applies {
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping evaluation step %s: %s", step.ID, reason))
			skipped = append(skipped, &EvaluationStepScore{
				StepID:    step.ID,
				Score:     0,
				MaxScore:  0,
				Reasoning: fmt.Sprintf("Skipped because this evaluation step is not applicable to target run %s. %s", targetRunFolder, reason),
				Evidence:  "Route gating marked this eval step as not applicable.",
				Skipped:   true,
			})
			continue
		}
		active = append(active, step)
	}

	return &EvaluationPlan{Steps: active}, skipped
}

func (hcpo *StepBasedWorkflowOrchestrator) evaluationStepAppliesToTargetRun(ctx context.Context, step *EvaluationStep, targetRunFolder string) (bool, string) {
	for _, gate := range step.AppliesToRoutes {
		if strings.TrimSpace(gate.RoutingStepID) == "" || len(gate.RouteIDs) == 0 {
			continue
		}
		selectedRouteID, ok := hcpo.readSelectedRouteForTargetRun(ctx, targetRunFolder, gate.RoutingStepID)
		if !ok {
			// Missing route logs are not enough to skip safely; older runs may not have them.
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Could not read routing selection for step %s in run %s; running eval step %s conservatively", gate.RoutingStepID, targetRunFolder, step.ID))
			continue
		}
		if !stringInList(selectedRouteID, gate.RouteIDs) {
			return false, fmt.Sprintf("routing step %q selected route %q, not one of %v", gate.RoutingStepID, selectedRouteID, gate.RouteIDs)
		}
	}

	return true, ""
}

func (hcpo *StepBasedWorkflowOrchestrator) readSelectedRouteForTargetRun(ctx context.Context, targetRunFolder string, routingStepID string) (string, bool) {
	path := filepath.Join("runs", targetRunFolder, "logs", routingStepID, "routing-evaluation.json")
	content, err := hcpo.ReadWorkspaceFile(ctx, path)
	if err != nil || strings.TrimSpace(content) == "" {
		return "", false
	}
	var payload struct {
		SelectedRouteID string `json:"selected_route_id"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse %s: %v", path, err))
		return "", false
	}
	if payload.SelectedRouteID == "" {
		return "", false
	}
	return payload.SelectedRouteID, true
}

func stringInList(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.EqualFold(value, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
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

	// Call the same evaluation process as manual evaluation. ExecuteEvaluationOnly
	// also snapshots per-run metric values at its tail, so any caller (this auto
	// path, builder-triggered eval-only) gets the snapshot for free.
	_, err = hcpo.ExecuteEvaluationOnly(ctx, hcpo.GetObjective(), hcpo.GetWorkspacePath(), targetRunFolder)
	if err != nil {
		return fmt.Errorf("auto-evaluation failed: %w", err)
	}

	return nil
}
