package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

const (
	executionTierPreferenceHigh   = "high"
	executionTierPreferenceMedium = "medium"

	executionTierPromotionThreshold = 3
	executionTierRecoveryThreshold  = 2
)

type adaptiveExecutionTierDecision struct {
	Tier   TierLevel
	Reason string
}

func normalizeExecutionTierPreference(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case executionTierPreferenceMedium:
		return executionTierPreferenceMedium
	case executionTierPreferenceHigh:
		return executionTierPreferenceHigh
	default:
		return executionTierPreferenceHigh
	}
}

func executionTierPreferenceFromLevel(tier TierLevel) string {
	switch tier {
	case TierMedium:
		return executionTierPreferenceMedium
	default:
		return executionTierPreferenceHigh
	}
}

func (hcpo *StepBasedWorkflowOrchestrator) shouldUseAdaptiveExecutionTiering(ctx context.Context, stepConfig *AgentConfigs) bool {
	if hcpo.tierResolver == nil || hcpo.isEvaluationMode {
		return false
	}
	if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		return false
	}
	if stepConfig != nil && NormalizeTierOverride(stepConfig.ExecutionTier) != "" {
		return false
	}
	if stepConfig != nil && stepConfig.DisableTierOptimization != nil && *stepConfig.DisableTierOptimization {
		return false
	}
	if _, ok := ctx.Value(virtualtools.SubAgentLLMContextKey).(*AgentLLMConfig); ok {
		return false
	}
	if _, ok := ctx.Value(virtualtools.PreferredTierContextKey).(int); ok {
		return false
	}
	if _, ok := ctx.Value(WorkshopTierOverrideKey).(int); ok {
		return false
	}
	return true
}

func (hcpo *StepBasedWorkflowOrchestrator) loadLearningMetadataForExecutionTiering(
	ctx context.Context,
	learningPathIdentifier string,
	stepPath string,
) (*LearningMetadata, error) {
	metadataPath := filepath.Join(hcpo.getLearningsBasePath(), learningPathIdentifier, ".learning_metadata.json")
	content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, metadataPath)
	if err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "not found") || strings.Contains(lower, "no such file") {
			return &LearningMetadata{
				StepID:                 learningPathIdentifier,
				StepPath:               stepPath,
				TotalIterations:        0,
				SuccessfulRuns:         0,
				FailureLearningRuns:    0,
				PreferredExecutionTier: executionTierPreferenceHigh,
			}, nil
		}
		return nil, err
	}

	var metadata LearningMetadata
	if err := json.Unmarshal([]byte(content), &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse learning metadata for execution tiering: %w", err)
	}
	if metadata.StepID == "" {
		metadata.StepID = learningPathIdentifier
	}
	if metadata.StepPath == "" {
		metadata.StepPath = stepPath
	}
	if metadata.PreferredExecutionTier == "" {
		metadata.PreferredExecutionTier = executionTierPreferenceHigh
	}
	return &metadata, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) writeLearningMetadataForExecutionTiering(
	ctx context.Context,
	learningPathIdentifier string,
	metadata *LearningMetadata,
) error {
	if metadata == nil {
		return nil
	}
	metadataPath := filepath.Join(hcpo.getLearningsBasePath(), learningPathIdentifier, ".learning_metadata.json")
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal learning metadata for execution tiering: %w", err)
	}
	if err := hcpo.BaseOrchestrator.WriteWorkspaceFile(ctx, metadataPath, string(metadataJSON)); err != nil {
		return fmt.Errorf("failed to write learning metadata for execution tiering: %w", err)
	}
	return nil
}

func resetAdaptiveExecutionTierState(metadata *LearningMetadata, reason string) {
	if metadata == nil {
		return
	}
	metadata.PreferredExecutionTier = executionTierPreferenceHigh
	metadata.MediumSuccessStreak = 0
	metadata.HighSuccessStreakSinceMediumFailure = 0
	metadata.LastMediumFailureAt = ""
	metadata.LastTierDecisionReason = reason
	metadata.LastTierChangeAt = time.Now().Format(time.RFC3339)
}

func (hcpo *StepBasedWorkflowOrchestrator) decideAdaptiveExecutionTier(
	ctx context.Context,
	learningPathIdentifier string,
	stepPath string,
	hasLearnings bool,
	currentDescriptionHash string,
	currentLearningHash string,
) (adaptiveExecutionTierDecision, error) {
	decision := adaptiveExecutionTierDecision{
		Tier:   TierHigh,
		Reason: "high (default)",
	}

	metadata, err := hcpo.loadLearningMetadataForExecutionTiering(ctx, learningPathIdentifier, stepPath)
	if err != nil {
		return decision, err
	}

	dirty := false
	metadata.PreferredExecutionTier = normalizeExecutionTierPreference(metadata.PreferredExecutionTier)

	if !hasLearnings {
		if metadata.PreferredExecutionTier != executionTierPreferenceHigh || metadata.MediumSuccessStreak > 0 || metadata.HighSuccessStreakSinceMediumFailure > 0 {
			resetAdaptiveExecutionTierState(metadata, "no learnings available — using Tier 1 (High)")
			dirty = true
		}
		if currentDescriptionHash != "" && metadata.LastDescriptionHash != currentDescriptionHash {
			metadata.LastDescriptionHash = currentDescriptionHash
			dirty = true
		}
		if dirty {
			if err := hcpo.writeLearningMetadataForExecutionTiering(ctx, learningPathIdentifier, metadata); err != nil {
				return decision, err
			}
		}
		decision.Reason = "high (no learnings available)"
		return decision, nil
	}

	if currentDescriptionHash != "" && metadata.LastDescriptionHash != "" && metadata.LastDescriptionHash != currentDescriptionHash {
		resetAdaptiveExecutionTierState(metadata, fmt.Sprintf("description changed (%s -> %s) — reverting to Tier 1 (High)", metadata.LastDescriptionHash[:8], currentDescriptionHash[:8]))
		dirty = true
	}
	if currentLearningHash != "" && metadata.LearningContentHash != "" && metadata.LearningContentHash != currentLearningHash {
		oldHash := metadata.LearningContentHash
		if len(oldHash) > 8 {
			oldHash = oldHash[:8]
		}
		newHash := currentLearningHash
		if len(newHash) > 8 {
			newHash = newHash[:8]
		}
		resetAdaptiveExecutionTierState(metadata, fmt.Sprintf("learning content changed (%s -> %s) — reverting to Tier 1 (High)", oldHash, newHash))
		dirty = true
	}
	if currentDescriptionHash != "" && metadata.LastDescriptionHash != currentDescriptionHash {
		metadata.LastDescriptionHash = currentDescriptionHash
		dirty = true
	}
	if currentLearningHash != "" && metadata.LearningContentHash != currentLearningHash {
		metadata.LearningContentHash = currentLearningHash
		dirty = true
	}

	switch metadata.PreferredExecutionTier {
	case executionTierPreferenceMedium:
		decision.Tier = TierMedium
		if metadata.LastTierDecisionReason != "" {
			decision.Reason = metadata.LastTierDecisionReason
		} else {
			decision.Reason = "medium (stable learnings + successful history)"
		}
	default:
		if metadata.LastMediumFailureAt != "" {
			if metadata.HighSuccessStreakSinceMediumFailure >= executionTierRecoveryThreshold {
				metadata.PreferredExecutionTier = executionTierPreferenceMedium
				metadata.HighSuccessStreakSinceMediumFailure = 0
				metadata.LastTierDecisionReason = fmt.Sprintf("promoting back to Tier 2 (Medium) after %d high-tier recovery success(es)", executionTierRecoveryThreshold)
				metadata.LastTierChangeAt = time.Now().Format(time.RFC3339)
				dirty = true
				decision.Tier = TierMedium
				decision.Reason = metadata.LastTierDecisionReason
			} else {
				decision.Reason = fmt.Sprintf("high (recovering after medium failure: %d/%d high-tier success(es))", metadata.HighSuccessStreakSinceMediumFailure, executionTierRecoveryThreshold)
			}
		} else if metadata.DescriptionHashRuns >= executionTierPromotionThreshold {
			metadata.PreferredExecutionTier = executionTierPreferenceMedium
			metadata.LastTierDecisionReason = fmt.Sprintf("promoting to Tier 2 (Medium) after %d stable successful run(s)", metadata.DescriptionHashRuns)
			metadata.LastTierChangeAt = time.Now().Format(time.RFC3339)
			dirty = true
			decision.Tier = TierMedium
			decision.Reason = metadata.LastTierDecisionReason
		} else {
			decision.Reason = fmt.Sprintf("high (waiting for stable successes: %d/%d on current description)", metadata.DescriptionHashRuns, executionTierPromotionThreshold)
		}
	}

	if dirty {
		if err := hcpo.writeLearningMetadataForExecutionTiering(ctx, learningPathIdentifier, metadata); err != nil {
			return decision, err
		}
	}

	return decision, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) recordAdaptiveExecutionTierSuccess(
	ctx context.Context,
	learningPathIdentifier string,
	stepPath string,
	usedTier TierLevel,
	currentDescriptionHash string,
	currentLearningHash string,
) error {
	metadata, err := hcpo.loadLearningMetadataForExecutionTiering(ctx, learningPathIdentifier, stepPath)
	if err != nil {
		return err
	}

	metadata.LastExecutionTier = executionTierPreferenceFromLevel(usedTier)
	if currentDescriptionHash != "" {
		metadata.LastDescriptionHash = currentDescriptionHash
	}
	if currentLearningHash != "" {
		metadata.LearningContentHash = currentLearningHash
	}

	switch usedTier {
	case TierMedium:
		if metadata.PreferredExecutionTier != executionTierPreferenceMedium {
			metadata.PreferredExecutionTier = executionTierPreferenceMedium
			metadata.LastTierChangeAt = time.Now().Format(time.RFC3339)
		}
		metadata.MediumSuccessStreak++
		metadata.HighSuccessStreakSinceMediumFailure = 0
		metadata.LastMediumFailureAt = ""
		metadata.LastTierDecisionReason = fmt.Sprintf("medium tier succeeded (%d consecutive medium success(es))", metadata.MediumSuccessStreak)
	default:
		if metadata.PreferredExecutionTier == "" {
			metadata.PreferredExecutionTier = executionTierPreferenceHigh
		}
		if metadata.LastMediumFailureAt != "" {
			metadata.HighSuccessStreakSinceMediumFailure++
			metadata.LastTierDecisionReason = fmt.Sprintf("high-tier recovery success %d/%d after medium failure", metadata.HighSuccessStreakSinceMediumFailure, executionTierRecoveryThreshold)
		}
	}

	return hcpo.writeLearningMetadataForExecutionTiering(ctx, learningPathIdentifier, metadata)
}

func (hcpo *StepBasedWorkflowOrchestrator) recordAdaptiveExecutionTierFailure(
	ctx context.Context,
	learningPathIdentifier string,
	stepPath string,
	failedTier TierLevel,
	currentDescriptionHash string,
	currentLearningHash string,
	reason string,
) error {
	if failedTier != TierMedium {
		return nil
	}

	metadata, err := hcpo.loadLearningMetadataForExecutionTiering(ctx, learningPathIdentifier, stepPath)
	if err != nil {
		return err
	}

	metadata.PreferredExecutionTier = executionTierPreferenceHigh
	metadata.LastExecutionTier = executionTierPreferenceFromLevel(failedTier)
	metadata.MediumSuccessStreak = 0
	metadata.HighSuccessStreakSinceMediumFailure = 0
	metadata.LastMediumFailureAt = time.Now().Format(time.RFC3339)
	metadata.LastTierChangeAt = metadata.LastMediumFailureAt
	metadata.LastTierDecisionReason = fmt.Sprintf("medium tier failed: %s — reverting to Tier 1 (High)", reason)
	if currentDescriptionHash != "" {
		metadata.LastDescriptionHash = currentDescriptionHash
	}
	if currentLearningHash != "" {
		metadata.LearningContentHash = currentLearningHash
	}

	return hcpo.writeLearningMetadataForExecutionTiering(ctx, learningPathIdentifier, metadata)
}
