package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// =====================================================================
// experiment_propose.go — handlers for propose_experiment + propose_metric.
//
// These are the LLM-callable entry points to the framework. The proposer
// (workflow builder in optimizer mode) emits a small typed input; the
// handler does atomic validation + materialization + apply + audit.
//
// Schemas: schemas/auto-improvement.schema.json#$defs/ToolInput_propose_experiment
//          schemas/auto-improvement.schema.json#$defs/ToolInput_propose_metric
// =====================================================================

// ProposeExperimentInput is the LLM-supplied portion of an experiment.
// Mirrors ToolInput_propose_experiment in the schema.
type ProposeExperimentInput struct {
	Hypothesis          string               `json:"hypothesis"`
	TargetMetrics       []string             `json:"target_metrics"`
	ExpectedDirection   ExpectedDirection    `json:"expected_direction"`
	ExpectedMagnitude   float64              `json:"expected_magnitude"`
	InterventionChanges []InterventionChange `json:"intervention_changes"`
	MeasurementRuns     int                  `json:"measurement_runs,omitempty"`
}

// ProposeExperimentOutput is what the tool returns to the proposer LLM.
//
// LinkedSuccessCriteria is the union of all success-criteria entries traced
// by the targeted metrics. UnanchoredMetrics is the subset of target_metrics
// that have empty linked_success_criteria (telemetry-only / auxiliary). The
// proposer is expected to surface both in the rationale so the operator can
// see at a glance whether the experiment moves a user-facing outcome or just
// a telemetry SLO.
type ProposeExperimentOutput struct {
	ExperimentID          string   `json:"experiment_id"`
	Status                string   `json:"status"`
	DecisionsEntryID      string   `json:"decisions_entry_id"`
	LinkedSuccessCriteria []string `json:"linked_success_criteria,omitempty"`
	UnanchoredMetrics     []string `json:"unanchored_metrics,omitempty"`
}

// ProposeExperiment is the system entrypoint for opening a new experiment.
// See docs/workflow/auto_improvement_framework.md for design background.
//
// Caller must supply the originating slash command in `trigger` (e.g.
// "improve-eval", "capture-context"); this is what gets recorded as the
// experiment's intervention.trigger and the decision-log trigger.
func ProposeExperiment(ctx context.Context, workspacePath, trigger string, input ProposeExperimentInput) (*ProposeExperimentOutput, error) {
	// 1. Validate inputs.
	if err := validateProposeInput(&input); err != nil {
		return nil, err
	}

	// Load metrics file once for validation + baseline.
	metricsFile, _, err := ReadMetricsFile(ctx, workspacePath)
	if err != nil {
		return nil, fmt.Errorf("read metrics: %w", err)
	}
	if metricsFile == nil {
		return nil, fmt.Errorf("workflow has no metrics.json — define metrics before proposing experiments")
	}
	for _, mid := range input.TargetMetrics {
		if FindMetric(metricsFile, mid) == nil {
			return nil, fmt.Errorf("target_metrics: %q not found in metrics.json", mid)
		}
		// Direction consistency: if expected_direction is concrete, must match metric direction.
		m := FindMetric(metricsFile, mid)
		if input.ExpectedDirection == DirectionIncrease && m.Direction == LowerBetter {
			return nil, fmt.Errorf("metric %q has direction=lower_better but expected_direction=increase", mid)
		}
		if input.ExpectedDirection == DirectionDecrease && m.Direction == HigherBetter {
			return nil, fmt.Errorf("metric %q has direction=higher_better but expected_direction=decrease", mid)
		}
	}

	cfg, err := ReadExperimentsConfig(ctx, workspacePath)
	if err != nil {
		return nil, err
	}

	// Path allow-list.
	if err := validateInterventionPaths(input.InterventionChanges, cfg); err != nil {
		return nil, err
	}

	measurementRuns := input.MeasurementRuns
	if measurementRuns == 0 {
		measurementRuns = cfg.DefaultMeasurementRuns
	}
	if cfg.MinRuns > 0 && measurementRuns < cfg.MinRuns {
		return nil, fmt.Errorf("measurement_runs %d < min_runs %d", measurementRuns, cfg.MinRuns)
	}
	if cfg.MaxRuns > 0 && measurementRuns > cfg.MaxRuns {
		return nil, fmt.Errorf("measurement_runs %d > max_runs %d", measurementRuns, cfg.MaxRuns)
	}

	// 2. Preconditions.
	active, err := ReadActiveExperiments(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	if cfg.MaxConcurrentExperiments > 0 && len(active) >= cfg.MaxConcurrentExperiments {
		return nil, fmt.Errorf("max concurrent experiments reached (%d)", cfg.MaxConcurrentExperiments)
	}
	for _, e := range active {
		if e.Status == ExpStatusMeasuring || e.Status == ExpStatusEvaluating || e.Status == ExpStatusAwaitingApproval {
			if metricsOverlap(e.TargetMetrics, input.TargetMetrics) {
				return nil, fmt.Errorf("active experiment %s already targets one of these metrics; conclude or abort it first", e.ID)
			}
		}
	}
	for _, p := range cfg.PinnedHypotheses {
		if strings.EqualFold(strings.TrimSpace(p.Text), strings.TrimSpace(input.Hypothesis)) {
			return nil, fmt.Errorf("hypothesis is pinned (forbidden): %s", p.Reason)
		}
	}

	// 3. Read baseline.
	baseline, err := computeBaseline(ctx, workspacePath, metricsFile, input.TargetMetrics, cfg.BaselineWindow)
	if err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}

	// 4. Capture world_state snapshot.
	worldStart := captureWorldState(ctx)

	// Allocate experiment id BEFORE diff capture so the diff path is stable.
	experimentID, err := nextExperimentID(ctx, workspacePath)
	if err != nil {
		return nil, err
	}

	// 5. Capture revertable_diff (pre-state) — read each path's current
	// content so we can restore it later. Persist the patch as a JSON
	// envelope (we don't depend on git here).
	diff, err := captureRevertableDiff(ctx, workspacePath, input.InterventionChanges)
	if err != nil {
		return nil, fmt.Errorf("capture diff: %w", err)
	}
	diffPath := experimentDiffPath(workspacePath, experimentID)
	diffBody, _ := json.MarshalIndent(diff, "", "  ")
	if err := writeFileToWorkspace(ctx, diffPath, string(diffBody)); err != nil {
		return nil, fmt.Errorf("persist diff: %w", err)
	}

	// 6. Apply intervention. On failure, attempt revert and abort.
	appliedPaths, applyErr := applyIntervention(ctx, workspacePath, input.InterventionChanges)
	if applyErr != nil {
		_ = applyRevertFromDiff(ctx, workspacePath, diff)
		return nil, fmt.Errorf("apply intervention: %w", applyErr)
	}

	// 7. Determine status based on oversight mode.
	manifest, _, mErr := ReadWorkflowManifest(ctx, workspacePath)
	oversight := OversightSupervised
	if mErr == nil && manifest != nil && manifest.OversightMode != "" {
		oversight = manifest.OversightMode
	}
	highRisk := isHighRiskIntervention(input.InterventionChanges, cfg)
	status := ExpStatusMeasuring
	switch oversight {
	case OversightManual:
		status = ExpStatusAwaitingApproval
	case OversightSupervised:
		if highRisk {
			status = ExpStatusAwaitingApproval
		}
	case OversightAutonomous:
		// stays measuring
	}

	// 8. Materialize the experiment record.
	rec := ExperimentRecord{
		ID:                experimentID,
		Status:            status,
		Hypothesis:        input.Hypothesis,
		TargetMetrics:     input.TargetMetrics,
		ExpectedDirection: input.ExpectedDirection,
		ExpectedMagnitude: input.ExpectedMagnitude,
		Baseline:          baseline,
		Intervention: ExperimentIntervention{
			Trigger:            trigger,
			AppliedChanges:     appliedPaths,
			RevertableDiffPath: diffPath,
		},
		Measurement: ExperimentMeasurement{
			TargetRuns:    measurementRuns,
			CompletedRuns: 0,
			Values:        map[string][]float64{},
		},
		WorldState: ExperimentWorldState{
			StartedAt: worldStart,
		},
		StartedAt: nowUTC(),
		Approvals: ExperimentApprovals{},
	}

	if err := UpsertActiveExperiment(ctx, workspacePath, rec); err != nil {
		// Roll back the apply if we failed to record.
		_ = applyRevertFromDiff(ctx, workspacePath, diff)
		return nil, fmt.Errorf("persist active experiment: %w", err)
	}

	// 9. Audit. Append a decision record cross-linking the experiment.
	dec := DecisionEntry{
		Source:             DecisionSourceAgent,
		Trigger:            trigger,
		Rationale:          fmt.Sprintf("opened experiment: %s", truncate(input.Hypothesis, 120)),
		AppliedChanges:     appliedPaths,
		TargetMetrics:      input.TargetMetrics,
		LinkedExperimentID: experimentID,
	}
	persistedDec, err := AppendDecisionEntry(ctx, workspacePath, dec)
	if err != nil {
		return nil, fmt.Errorf("append decision: %w", err)
	}
	rec.LinkedDecisions = append(rec.LinkedDecisions, persistedDec.ID)
	if err := UpsertActiveExperiment(ctx, workspacePath, rec); err != nil {
		return nil, err
	}

	linked, unanchored := summarizeMetricLinks(metricsFile, input.TargetMetrics)

	return &ProposeExperimentOutput{
		ExperimentID:          experimentID,
		Status:                string(status),
		DecisionsEntryID:      persistedDec.ID,
		LinkedSuccessCriteria: linked,
		UnanchoredMetrics:     unanchored,
	}, nil
}

// summarizeMetricLinks returns the union of linked_success_criteria across
// the targeted metrics, plus the list of metric ids that have no link
// (telemetry / auxiliary). Unanchored metrics are not blocked — surfaced for
// the operator and the agent's rationale so the decision is explicit.
func summarizeMetricLinks(metricsFile *MetricsFile, targetMetrics []string) (linked, unanchored []string) {
	if metricsFile == nil {
		return nil, nil
	}
	seen := map[string]struct{}{}
	for _, mid := range targetMetrics {
		m := FindMetric(metricsFile, mid)
		if m == nil {
			continue
		}
		if len(m.LinkedSuccessCriteria) == 0 {
			unanchored = append(unanchored, mid)
			continue
		}
		for _, sc := range m.LinkedSuccessCriteria {
			if _, ok := seen[sc]; ok {
				continue
			}
			seen[sc] = struct{}{}
			linked = append(linked, sc)
		}
	}
	return linked, unanchored
}

// ---- validation ------------------------------------------------------------

func validateProposeInput(in *ProposeExperimentInput) error {
	if strings.TrimSpace(in.Hypothesis) == "" {
		return fmt.Errorf("hypothesis is required")
	}
	if len(in.Hypothesis) > 200 {
		return fmt.Errorf("hypothesis exceeds 200 chars (got %d)", len(in.Hypothesis))
	}
	if len(in.TargetMetrics) == 0 {
		return fmt.Errorf("target_metrics is required (non-empty)")
	}
	switch in.ExpectedDirection {
	case DirectionIncrease, DirectionDecrease, DirectionMaintain:
	default:
		return fmt.Errorf("invalid expected_direction %q", in.ExpectedDirection)
	}
	if in.ExpectedMagnitude <= 0 {
		return fmt.Errorf("expected_magnitude must be > 0")
	}
	if len(in.InterventionChanges) == 0 {
		return fmt.Errorf("intervention_changes is required (non-empty)")
	}
	for i, c := range in.InterventionChanges {
		if strings.TrimSpace(c.Path) == "" {
			return fmt.Errorf("intervention_changes[%d].path is required", i)
		}
		switch c.Operation {
		case OpAppend, OpReplace, OpPatch, OpCreate:
		default:
			return fmt.Errorf("intervention_changes[%d].operation invalid: %q", i, c.Operation)
		}
	}
	return nil
}

func validateInterventionPaths(changes []InterventionChange, cfg ExperimentsConfig) error {
	for _, c := range changes {
		clean := strings.TrimPrefix(strings.TrimSpace(c.Path), "/")
		// Forbidden first.
		for _, fp := range cfg.ForbiddenInterventionPaths {
			if pathMatches(clean, fp) {
				return fmt.Errorf("intervention_changes: path %q is forbidden", clean)
			}
		}
		// Allowed.
		ok := false
		for _, ap := range cfg.AllowedInterventionPaths {
			if pathMatches(clean, ap) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("intervention_changes: path %q is not in allowed_intervention_paths", clean)
		}
	}
	return nil
}

// pathMatches matches a clean path against a pattern. Pattern ending with "/"
// is treated as a directory prefix; otherwise exact match.
func pathMatches(clean, pattern string) bool {
	pat := strings.TrimPrefix(pattern, "/")
	if strings.HasSuffix(pat, "/") {
		return strings.HasPrefix(clean, pat)
	}
	return clean == pat
}

func metricsOverlap(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, x := range a {
		set[x] = struct{}{}
	}
	for _, y := range b {
		if _, ok := set[y]; ok {
			return true
		}
	}
	return false
}

func isHighRiskIntervention(changes []InterventionChange, cfg ExperimentsConfig) bool {
	for _, c := range changes {
		clean := strings.TrimPrefix(strings.TrimSpace(c.Path), "/")
		// Hard rule: changing eval plan structure or metrics.json is high risk.
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

// ---- baseline + world_state -----------------------------------------------

func computeBaseline(ctx context.Context, workspacePath string, metricsFile *MetricsFile, metricIDs []string, window int) (ExperimentBaseline, error) {
	if window <= 0 {
		window = 5
	}
	bl := ExperimentBaseline{
		Window: fmt.Sprintf("last_%d_runs", window),
		Values: map[string][]float64{},
		Mean:   map[string]float64{},
		Std:    map[string]float64{},
	}

	runs, err := listRecentRunFolders(ctx, workspacePath, window*2) // grab extra so we can pick up enough that have eval data
	if err != nil {
		// Baseline failure is non-fatal; flag insufficient.
		bl.Insufficient = true
		return bl, nil
	}

	for _, mid := range metricIDs {
		m := FindMetric(metricsFile, mid)
		if m == nil {
			continue
		}
		var vals []float64
		for _, r := range runs {
			if len(vals) >= window {
				break
			}
			v, ok, _ := ResolveMetricValue(ctx, workspacePath, r, m)
			if !ok {
				continue
			}
			vals = append(vals, v)
		}
		if len(vals) == 0 {
			bl.Insufficient = true
			continue
		}
		bl.Values[mid] = vals
		bl.Mean[mid] = mean(vals)
		bl.Std[mid] = stddev(vals, bl.Mean[mid])
	}
	return bl, nil
}

func mean(vs []float64) float64 {
	if len(vs) == 0 {
		return 0
	}
	s := 0.0
	for _, v := range vs {
		s += v
	}
	return s / float64(len(vs))
}

func stddev(vs []float64, mu float64) float64 {
	if len(vs) < 2 {
		return 0
	}
	s := 0.0
	for _, v := range vs {
		d := v - mu
		s += d * d
	}
	return sqrt(s / float64(len(vs)-1))
}

// sqrt is a tiny in-package sqrt to avoid importing math just for this; uses
// Newton's method for ~1e-12 precision.
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 50; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// listRecentRunFolders returns up to maxN run-folder names sorted by recency
// (most recent first). Best-effort; uses the existing eval scores file index
// as a proxy for "we have data for this run".
func listRecentRunFolders(ctx context.Context, workspacePath string, maxN int) ([]string, error) {
	reports, err := readAllEvaluationReportsFromScores(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	type rf struct {
		Folder      string
		GeneratedAt string
	}
	all := make([]rf, 0, len(reports))
	for k, v := range reports {
		all = append(all, rf{Folder: k, GeneratedAt: v.GeneratedAt})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].GeneratedAt > all[j].GeneratedAt
	})
	out := make([]string, 0, len(all))
	for i, r := range all {
		if i >= maxN {
			break
		}
		out = append(out, r.Folder)
	}
	return out, nil
}

// captureWorldState snapshots model versions, MCP versions, etc. The detailed
// fields can be wired in later as the runtime exposes them; for v1 we capture
// just the timestamp so drift detection can proceed even without rich state.
func captureWorldState(ctx context.Context) *WorldStateSnapshot {
	return &WorldStateSnapshot{CapturedAt: nowUTC()}
}

// ---- intervention apply / revert ------------------------------------------

// RevertableDiff is what we persist to experiments/diffs/<id>.patch.
// Stored as JSON for simplicity (the workspace API doesn't expose git).
type RevertableDiff struct {
	Files []RevertableDiffEntry `json:"files"`
}

type RevertableDiffEntry struct {
	Path        string `json:"path"`
	HadBefore   bool   `json:"had_before"`
	BeforeBody  string `json:"before_body,omitempty"`
}

func captureRevertableDiff(ctx context.Context, workspacePath string, changes []InterventionChange) (RevertableDiff, error) {
	diff := RevertableDiff{Files: make([]RevertableDiffEntry, 0, len(changes))}
	seen := make(map[string]struct{})
	for _, c := range changes {
		full := joinWorkspacePath(workspacePath, c.Path)
		if _, ok := seen[full]; ok {
			continue
		}
		seen[full] = struct{}{}
		body, exists, err := readFileFromWorkspace(ctx, full)
		if err != nil {
			return diff, fmt.Errorf("read %s: %w", full, err)
		}
		entry := RevertableDiffEntry{Path: full, HadBefore: exists}
		if exists {
			entry.BeforeBody = body
		}
		diff.Files = append(diff.Files, entry)
	}
	return diff, nil
}

func applyIntervention(ctx context.Context, workspacePath string, changes []InterventionChange) ([]string, error) {
	applied := make([]string, 0, len(changes))
	for _, c := range changes {
		full := joinWorkspacePath(workspacePath, c.Path)
		current, exists, err := readFileFromWorkspace(ctx, full)
		if err != nil {
			return applied, err
		}
		var newBody string
		switch c.Operation {
		case OpAppend:
			if !exists {
				newBody = c.Content
			} else {
				if !strings.HasSuffix(current, "\n") && current != "" {
					current += "\n"
				}
				newBody = current + c.Content
			}
		case OpReplace, OpCreate:
			newBody = c.Content
		case OpPatch:
			// Phase-2 stub: we don't implement unified-diff apply yet. Treat
			// patch as full-content replace and let the LLM send the resulting
			// content. Future: use a unified-diff library.
			newBody = c.Content
		default:
			return applied, fmt.Errorf("unsupported operation %q", c.Operation)
		}
		if err := writeFileToWorkspace(ctx, full, newBody); err != nil {
			return applied, fmt.Errorf("write %s: %w", full, err)
		}
		applied = append(applied, c.Path)
	}
	return applied, nil
}

// applyRevertFromDiff restores files to their pre-experiment state. Used by:
//   - propose_experiment internal rollback on failed atomic commit
//   - conclude_experiment with verdict=reverted
func applyRevertFromDiff(ctx context.Context, workspacePath string, diff RevertableDiff) error {
	for _, f := range diff.Files {
		if !f.HadBefore {
			// File didn't exist before; best-effort: write empty content. The
			// workspace API does not expose Delete; this is the closest we can
			// get without infrastructure churn.
			if err := writeFileToWorkspace(ctx, f.Path, ""); err != nil {
				return err
			}
			continue
		}
		if err := writeFileToWorkspace(ctx, f.Path, f.BeforeBody); err != nil {
			return err
		}
	}
	return nil
}

// joinWorkspacePath joins a workflow workspace path with a workflow-relative
// path segment, treating leading "/" defensively.
func joinWorkspacePath(workspacePath, rel string) string {
	return path.Join(strings.Trim(workspacePath, "/"), strings.TrimPrefix(strings.TrimSpace(rel), "/"))
}
