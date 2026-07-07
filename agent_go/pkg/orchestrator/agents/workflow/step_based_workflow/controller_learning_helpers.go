package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
)

// Learnings access levels (mirror of knowledgebase_access).
const (
	LearningsAccessRead      = "read"
	LearningsAccessReadWrite = "read-write"
	LearningsAccessNone      = "none"
)

// resolveLearningsAccess returns the effective learnings_access for a step.
// Explicit value wins; empty falls back to auto-migration:
//   - learning_objective non-empty → "read-write" (preserves legacy behavior)
//   - learning_objective empty     → "read"       (new default — all steps see _global/)
//
// An explicit bad value is normalized to "read" with a warning at validation time.
func resolveLearningsAccess(agentConfigs *AgentConfigs) string {
	if agentConfigs == nil {
		return LearningsAccessRead
	}
	v := strings.TrimSpace(agentConfigs.LearningsAccess)
	switch v {
	case LearningsAccessRead, LearningsAccessReadWrite, LearningsAccessNone:
		return v
	case "":
		if strings.TrimSpace(agentConfigs.LearningObjective) != "" {
			return LearningsAccessReadWrite
		}
		return LearningsAccessRead
	default:
		return LearningsAccessRead
	}
}

// canReadLearnings reports whether this step's execution prompt should include
// the global SKILL.md content. Read is the default unless explicitly set to
// "none"; routing steps and evaluation-mode runs always skip to keep their
// prompts lean.
func canReadLearnings(agentConfigs *AgentConfigs, step PlanStepInterface, isEvalMode bool) bool {
	if isEvalMode || (step != nil && isRoutingStep(step)) {
		return false
	}
	return resolveLearningsAccess(agentConfigs) != LearningsAccessNone
}

// canWriteLearnings reports whether the step agent should run its direct
// post-completion learnings turn. Requires learnings_access == "read-write"
// AND a non-empty learning_objective (the extraction target for the writer).
// Routing and eval mode always skip.
func canWriteLearnings(agentConfigs *AgentConfigs, step PlanStepInterface, isEvalMode bool) bool {
	if isEvalMode || (step != nil && isRoutingStep(step)) {
		return false
	}
	if agentConfigs == nil {
		return false
	}
	if resolveLearningsAccess(agentConfigs) != LearningsAccessReadWrite {
		return false
	}
	return strings.TrimSpace(agentConfigs.LearningObjective) != ""
}

// Learnings write methods — mirror of knowledgebase_write_method. Only meaningful
// when canWriteLearnings reports true for the step (access == read-write AND
// objective non-empty AND not eval/routing). lock_learnings is still honored
// separately: locked → no writes regardless of method.
const (
	// LearnWriteMethodAgent — historical "agent" mode (separate post-step
	// learning agent). Retired: kept as a string constant so existing
	// plan.json files that still carry "learnings_write_method": "agent"
	// parse cleanly and are then coerced to direct.
	LearnWriteMethodAgent = "agent"
	// LearnWriteMethodDirect hands learnings writes to the step agent itself
	// via a dedicated post-completion user-message turn. The only supported
	// runtime method.
	LearnWriteMethodDirect = "direct"
)

// resolveLearningsWriteMethod is now a constant — every step uses direct mode.
// The argument is retained for call-site compatibility (and to give grep a
// hint that the field still exists in plan.json) but its value is ignored.
//
// Historically the default was "agent" (a separate post-step learning agent
// analyzed the trace and wrote SKILL.md). That mode was retired: it cost an
// extra LLM turn per step, doubled [AUTO-NOTIFICATION] noise, and direct mode
// produces equivalent SKILL.md content via the step agent's own post-completion
// turn at a fraction of the cost. See controller_execution.go for the trigger.
func resolveLearningsWriteMethod(_ *AgentConfigs) string {
	return LearnWriteMethodDirect
}

// shouldDirectWriteLearnings reports whether the step is configured for
// learnings writes. Since direct is now the only mode, this collapses to
// "is the step access+objective gate satisfied?". Kept as a named helper so
// the call sites still read intuitively.
func shouldDirectWriteLearnings(agentConfigs *AgentConfigs, step PlanStepInterface, isEvalMode bool) bool {
	return canWriteLearnings(agentConfigs, step, isEvalMode)
}

var directLearningsGlobalEmptyForLock = func(hcpo *StepBasedWorkflowOrchestrator, ctx context.Context) (bool, error) {
	return hcpo.isStepLearningsFolderEmpty(ctx, GlobalLearningID, 0, "")
}

func (hcpo *StepBasedWorkflowOrchestrator) shouldSkipDirectLearningsDueToLock(ctx context.Context, agentConfigs *AgentConfigs, stepIndex int) bool {
	if agentConfigs == nil || agentConfigs.LockLearnings == nil || !*agentConfigs.LockLearnings {
		return false
	}
	globalEmpty, emptyErr := directLearningsGlobalEmptyForLock(hcpo, ctx)
	if emptyErr != nil {
		// Can't check — assume empty to allow first-run bootstrap.
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 lock_learnings=true on step %d but _global/ check failed (%v) — allowing direct-learnings turn to bootstrap", stepIndex+1, emptyErr))
		return false
	}
	if globalEmpty {
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 lock_learnings=true on step %d but _global/ is empty — allowing direct-learnings turn to bootstrap initial skill", stepIndex+1))
		return false
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 lock_learnings=true on step %d with existing _global/ content — skipping direct-learnings turn", stepIndex+1))
	return true
}

// findStepInPlan recursively finds a step by ID in the plan structure
// LoadGlobalLearningHistory loads and formats the global workflow-level learning history.
// Returns empty string if no global learnings found or on error.
func (hcpo *StepBasedWorkflowOrchestrator) LoadGlobalLearningHistory(
	ctx context.Context,
) (string, error) {
	globalLearningsPath := hcpo.getLearningsBasePath() + "/" + GlobalLearningID

	// Read learning files from global folder
	learningFiles, err := hcpo.readStepLearningFiles(ctx, globalLearningsPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read global learning files from %s: %v - proceeding without global learnings", globalLearningsPath, err))
		return "", nil
	}

	if len(learningFiles) == 0 {
		return "", nil
	}

	// Format learnings for system prompt
	formattedLearnings, _ := hcpo.formatStepLearningFilesAsHistory(learningFiles)
	hcpo.GetLogger().Info(fmt.Sprintf("✅ Loaded %d global learning file(s) for execution agent system prompt", len(learningFiles)))

	return formattedLearnings, nil
}
