package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// =====================================================================
// experiment_conclude.go — handler for the conclude_experiment tool +
// apply_revert + archive_experiment.
//
// The verdict is system-computed (in compute_verdict). The evaluator LLM
// only narrates it (rationale) and may, rarely, override it with reason.
//
// Schemas: schemas/auto-improvement.schema.json#$defs/ToolInput_conclude_experiment
// =====================================================================

// VerdictOverride is the optional override block on conclude_experiment.
type VerdictOverride struct {
	To     Verdict `json:"to"`
	Reason string  `json:"reason"`
}

// ConcludeExperimentInput is the LLM-supplied portion (or user-supplied via
// manual_conclude). Mirrors ToolInput_conclude_experiment in the schema.
type ConcludeExperimentInput struct {
	ExperimentID    string           `json:"experiment_id"`
	Rationale       string           `json:"rationale"`
	OverrideVerdict *VerdictOverride `json:"override_verdict,omitempty"`
}

// ConcludeExperimentOutput is what the tool returns to the evaluator LLM.
type ConcludeExperimentOutput struct {
	FinalVerdict Verdict `json:"final_verdict"`
	Archived     bool    `json:"archived"`
}

// ConcludeExperiment commits the rationale on an experiment whose verdict has
// been system-computed, runs the post-verdict actions (archive / revert /
// extend), and writes audit entries.
//
// Caller specifies who is concluding via `actor` (e.g. "evaluator-agent",
// "user:alice"). For manual_conclude, the caller may pass an override.
func ConcludeExperiment(ctx context.Context, workspacePath, actor string, input ConcludeExperimentInput) (*ConcludeExperimentOutput, error) {
	if strings.TrimSpace(input.ExperimentID) == "" {
		return nil, fmt.Errorf("experiment_id is required")
	}
	if strings.TrimSpace(input.Rationale) == "" {
		return nil, fmt.Errorf("rationale is required")
	}
	if len(input.Rationale) > 500 {
		return nil, fmt.Errorf("rationale exceeds 500 chars (got %d)", len(input.Rationale))
	}
	if input.OverrideVerdict != nil {
		switch input.OverrideVerdict.To {
		case VerdictKept, VerdictReverted, VerdictInconclusive, VerdictExtend:
		default:
			return nil, fmt.Errorf("invalid override_verdict.to %q", input.OverrideVerdict.To)
		}
		if strings.TrimSpace(input.OverrideVerdict.Reason) == "" {
			return nil, fmt.Errorf("override_verdict.reason is required")
		}
	}

	file, exists, err := ReadActiveFile(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	if !exists || file == nil {
		return nil, fmt.Errorf("no active experiments file")
	}
	rec := FindActiveExperiment(file, input.ExperimentID)
	if rec == nil {
		return nil, fmt.Errorf("experiment %s not found in active.json", input.ExperimentID)
	}

	// Allow conclusion either when status is "evaluating" (system computed
	// the verdict already) or when an explicit override is supplied (manual
	// conclude path can short-circuit a stuck experiment).
	if rec.Status != ExpStatusEvaluating && input.OverrideVerdict == nil {
		return nil, fmt.Errorf("experiment %s is in status %q; verdict not yet computed", rec.ID, rec.Status)
	}

	// Ensure conclusion exists (override path may set it for the first time).
	if rec.Conclusion == nil {
		rec.Conclusion = &ExperimentConclusion{}
	}
	rec.Conclusion.Rationale = input.Rationale
	if input.OverrideVerdict != nil {
		rec.Conclusion.Verdict = input.OverrideVerdict.To
		rec.Conclusion.VerdictOverridden = true
		rec.Conclusion.OverrideReason = input.OverrideVerdict.Reason
	}

	// Approval gate.
	manifest, _, _ := ReadWorkflowManifest(ctx, workspacePath)
	cfg, _ := ReadExperimentsConfig(ctx, workspacePath)
	oversight := OversightSupervised
	if manifest != nil && manifest.OversightMode != "" {
		oversight = manifest.OversightMode
	}
	highRisk := isHighRiskInterventionPaths(rec.Intervention.AppliedChanges, cfg)
	requiresApproval := false
	switch oversight {
	case OversightManual:
		requiresApproval = true
	case OversightSupervised:
		if rec.Conclusion.Verdict == VerdictKept && highRisk {
			requiresApproval = true
		}
	}
	if requiresApproval {
		rec.Status = ExpStatusAwaitingConclusionApproval
		if err := UpsertActiveExperiment(ctx, workspacePath, *rec); err != nil {
			return nil, err
		}
		return &ConcludeExperimentOutput{FinalVerdict: rec.Conclusion.Verdict, Archived: false}, nil
	}

	// Apply verdict.
	final, archived, err := applyConclusion(ctx, workspacePath, rec)
	if err != nil {
		return nil, err
	}

	// Audit decision entry.
	dec := DecisionEntry{
		Source:             actorToSource(actor),
		Trigger:            "conclude-experiment",
		Rationale:          fmt.Sprintf("verdict=%s actor=%s", final, actor),
		AppliedChanges:     []string{},
		LinkedExperimentID: rec.ID,
		TargetMetrics:      rec.TargetMetrics,
	}
	if rec.Conclusion.VerdictOverridden {
		dec.Rationale += fmt.Sprintf(" override_reason=%q", rec.Conclusion.OverrideReason)
	}
	if _, err := AppendDecisionEntry(ctx, workspacePath, dec); err != nil {
		return nil, err
	}

	return &ConcludeExperimentOutput{FinalVerdict: final, Archived: archived}, nil
}

// applyConclusion executes the verdict's effect:
//   kept         -> archive (intervention stays)
//   reverted     -> apply_revert + archive
//   inconclusive -> archive (intervention stays; user can manually revert)
//   extend       -> bump target_runs by config default; status back to measuring
func applyConclusion(ctx context.Context, workspacePath string, rec *ExperimentRecord) (Verdict, bool, error) {
	if rec.Conclusion == nil {
		return VerdictInconclusive, false, fmt.Errorf("no conclusion to apply")
	}
	v := rec.Conclusion.Verdict

	cfg, _ := ReadExperimentsConfig(ctx, workspacePath)

	switch v {
	case VerdictExtend:
		bump := cfg.DefaultMeasurementRuns
		if bump <= 0 {
			bump = 5
		}
		rec.Measurement.TargetRuns += bump
		rec.Status = ExpStatusMeasuring
		// Clear conclusion's verdict so the next compute_verdict will re-render it.
		rec.Conclusion.Verdict = ""
		if err := UpsertActiveExperiment(ctx, workspacePath, *rec); err != nil {
			return v, false, err
		}
		return v, false, nil

	case VerdictReverted:
		if err := ApplyRevertByExperimentID(ctx, workspacePath, rec.ID); err != nil {
			return v, false, fmt.Errorf("apply_revert: %w", err)
		}
		fallthrough
	case VerdictKept, VerdictInconclusive:
		// Stamp concluded_at and world_state.concluded_at (if not already).
		rec.ConcludedAt = nowUTC()
		if rec.WorldState.ConcludedAt == nil {
			rec.WorldState.ConcludedAt = &WorldStateSnapshot{CapturedAt: nowUTC()}
		}
		rec.Status = ExpStatusConcluded

		if err := AppendHistoryRecord(ctx, workspacePath, *rec); err != nil {
			return v, false, fmt.Errorf("append history: %w", err)
		}
		if err := RemoveActiveExperiment(ctx, workspacePath, rec.ID); err != nil {
			return v, true, fmt.Errorf("remove from active: %w", err)
		}
		return v, true, nil
	}
	return v, false, fmt.Errorf("unhandled verdict %q", v)
}

// ApplyRevertByExperimentID reads the persisted revertable_diff for the
// experiment and restores files to their pre-experiment state. Also writes a
// decision entry. Used by:
//   - applyConclusion when verdict=reverted
//   - the user-side abort_experiment endpoint (Phase 5)
func ApplyRevertByExperimentID(ctx context.Context, workspacePath, experimentID string) error {
	diffPath := experimentDiffPath(workspacePath, experimentID)
	content, exists, err := readFileFromWorkspace(ctx, diffPath)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("revertable_diff not found at %s", diffPath)
	}
	var diff RevertableDiff
	if err := json.Unmarshal([]byte(content), &diff); err != nil {
		return fmt.Errorf("parse revertable_diff: %w", err)
	}
	if err := applyRevertFromDiff(ctx, workspacePath, diff); err != nil {
		return err
	}
	dec := DecisionEntry{
		Source:             DecisionSourceSystem,
		Trigger:            "apply_revert",
		Rationale:          fmt.Sprintf("reverted experiment %s", experimentID),
		AppliedChanges:     diffPathsApplied(diff),
		LinkedExperimentID: experimentID,
	}
	_, err = AppendDecisionEntry(ctx, workspacePath, dec)
	return err
}

func diffPathsApplied(diff RevertableDiff) []string {
	out := make([]string, 0, len(diff.Files))
	for _, f := range diff.Files {
		out = append(out, f.Path)
	}
	return out
}

// isHighRiskInterventionPaths is the variant used at conclude time, when we
// have only the applied paths (not the original InterventionChange list).
func isHighRiskInterventionPaths(paths []string, cfg ExperimentsConfig) bool {
	for _, p := range paths {
		clean := strings.TrimPrefix(strings.TrimSpace(p), "/")
		if clean == "evaluation/evaluation_plan.json" || clean == "planning/metrics.json" {
			return true
		}
		for _, hr := range cfg.HighRiskPaths {
			if pathMatches(clean, hr) {
				return true
			}
		}
	}
	return false
}

func actorToSource(actor string) DecisionSource {
	a := strings.ToLower(strings.TrimSpace(actor))
	switch {
	case strings.HasPrefix(a, "user"):
		return DecisionSourceUser
	case a == "system" || strings.HasPrefix(a, "system:"):
		return DecisionSourceSystem
	default:
		return DecisionSourceAgent
	}
}
