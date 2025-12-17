package todo_creation_human

import (
	"context"
	"fmt"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// runLearningConsolidationPhase consolidates new learning content with existing learnings
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// newLearningFilePath: Path to the new learning file created by extraction agent
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority)
func (hcpo *HumanControlledTodoPlannerOrchestrator) runLearningConsolidationPhase(ctx context.Context, stepIndex int, stepPath string, learningPathIdentifier string, step *TodoStep, newLearningFilePath string, isCodeExecutionMode bool) error {
	// Use step-specific learning detail level, default to "exact" if not set
	learningDetailLevel := "exact" // default
	if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = step.AgentConfigs.LearningDetailLevel
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Using step-specific learning detail level: '%s'", learningDetailLevel))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📝 No step-specific learning detail level set, using default: 'exact'"))
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔗 Starting learning consolidation for %s: %s", learningPathIdentifier, step.Title))

	// Create consolidation agent
	// Resolve variables in step title before using in agent name
	resolvedTitle := ResolveVariables(step.Title, hcpo.variableValues)
	sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
	// Include learning mode in agent name (exact or general)
	learningMode := "general"
	if learningDetailLevel == "exact" {
		learningMode = "exact"
	}
	consolidationAgentName := fmt.Sprintf("%s-consolidation-%s-%s", learningPathIdentifier, sanitizedTitle, learningMode)
	consolidationAgent, err := hcpo.createLearningConsolidationAgent(ctx, "consolidation", learningPathIdentifier, consolidationAgentName, step.AgentConfigs, isCodeExecutionMode)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to create learning consolidation agent: %w", err), nil)
	}

	// Prepare template variables for consolidation agent
	consolidationTemplateVars := map[string]string{
		"StepTitle":           step.Title,
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		"StepNumber":          learningPathIdentifier,
		"NewLearningFilePath": newLearningFilePath,
		"LearningDetailLevel": learningDetailLevel,
		"IsCodeExecutionMode": fmt.Sprintf("%v", isCodeExecutionMode),
	}

	// Add step-specific paths (always enabled)
	// Calculate run workspace path - learnings are at the same level as execution/, not inside it
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	consolidationTemplateVars["StepExecutionPath"] = runWorkspacePath

	// Add variable names if available
	if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
		consolidationTemplateVars["VariableNames"] = variableNames
	}

	// Read the new learning file content before consolidation (it will be deleted by consolidation agent)
	newLearningContent := ""
	if newLearningFilePath != "" {
		content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, newLearningFilePath)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read new learning file for metadata: %v (continuing without it)", err))
			// Continue even if we can't read it - it might have been deleted already or doesn't exist
		} else {
			newLearningContent = content
		}
	}

	// Execute consolidation agent and capture output
	consolidationOutput, _, err := consolidationAgent.Execute(ctx, consolidationTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("learning consolidation failed: %w", err), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Learning consolidation completed for %s (detail level: %s)", learningPathIdentifier, learningDetailLevel))

	// Update consolidation metadata with the output and new learning content
	if err := hcpo.updateConsolidationMetadata(ctx, stepIndex, stepPath, learningPathIdentifier, consolidationOutput, newLearningContent); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update consolidation metadata: %v", err))
		// Don't fail the consolidation if metadata update fails
	}

	return nil
}
