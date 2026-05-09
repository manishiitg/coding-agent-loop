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
// metric_propose.go — handler for the propose_metric tool.
//
// New metrics are added through the active metrics list. Existing metrics can
// be amended only with an explicit amend_existing payload; the prior active
// definition is copied to metrics.json::archive first.
//
// Schemas: schemas/auto-improvement.schema.json#$defs/ToolInput_propose_metric
// =====================================================================

// ProposeMetricInput is the LLM-supplied portion of a metric proposal.
type ProposeMetricInput struct {
	ID        string              `json:"id,omitempty"`
	Label     string              `json:"label,omitempty"`
	Role      MetricRole          `json:"role,omitempty"`
	Category  string              `json:"category,omitempty"`
	Unit      string              `json:"unit,omitempty"`
	Direction MetricDirection     `json:"direction,omitempty"`
	Mode      MetricMode          `json:"mode,omitempty"`
	Target    *float64            `json:"target,omitempty"`
	Floor     *float64            `json:"floor,omitempty"`
	Ceiling   *float64            `json:"ceiling,omitempty"`
	Source    MetricSource        `json:"source,omitempty"`
	Parent    string              `json:"parent,omitempty"`
	Amend     *AmendMetricRequest `json:"amend_existing,omitempty"`
	// SuccessCriteria should quote or summarize the soul.md success criterion
	// this metric measures, so metric rows stay anchored to user outcomes.
	SuccessCriteria string `json:"success_criteria,omitempty"`
}

// AmendMetricRequest must be present when propose_metric is used to change an
// existing metric definition in place under the same metric id.
type AmendMetricRequest struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// ProposeMetricOutput is what the tool returns to the proposer LLM.
//
// Warnings are non-blocking advisories — most commonly that the eval
// step's most recent run does not currently emit the structured-output
// field this metric will read, so the metric will return NO VALUE on the
// next snapshot until the eval Python is updated. Surfaced so the agent
// can either update the eval in the same session or accept that the metric
// won't resolve until then.
type ProposeMetricOutput struct {
	MetricID        string   `json:"metric_id,omitempty"`
	Status          string   `json:"status"` // "added" | "amended"
	Version         int      `json:"version,omitempty"`
	ArchivedVersion int      `json:"archived_version,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

// ProposeMetric is the system entrypoint for defining or amending a metric.
// Amending requires amend_existing so the agent has to supply an audit reason.
//
// Trigger is the originating slash command name (used in the structured
// improve.md decision entry).
func ProposeMetric(ctx context.Context, workspacePath, trigger string, input ProposeMetricInput) (*ProposeMetricOutput, error) {
	// Soul precondition: an objective and success_criteria must exist before
	// metrics are defined against them. Without that anchor, metrics are
	// arbitrary and the framework has no north star to measure against.
	if err := RequireSoulPreconditions(ctx, workspacePath); err != nil {
		return nil, err
	}

	candidate := Metric{
		ID:              strings.TrimSpace(input.ID),
		Label:           input.Label,
		Role:            input.Role,
		Category:        strings.TrimSpace(input.Category),
		Unit:            input.Unit,
		Direction:       input.Direction,
		Mode:            input.Mode,
		Target:          input.Target,
		Floor:           input.Floor,
		Ceiling:         input.Ceiling,
		Source:          input.Source,
		SuccessCriteria: strings.TrimSpace(input.SuccessCriteria),
		Parent:          strings.TrimSpace(input.Parent),
	}
	if err := ValidateMetric(&candidate); err != nil {
		return nil, fmt.Errorf("invalid metric: %w", err)
	}

	// Cross-file validation: a metric is just an eval value extracted in a
	// specific format, so the metric/eval contract has to hold at write time.
	// Hard errors block creation; warnings flow through to the caller so the
	// agent can decide.
	if err := checkEvalStepReferenceExists(ctx, workspacePath, &candidate.Source); err != nil {
		return nil, err
	}
	warnings := dryResolveWarnings(ctx, workspacePath, &candidate.Source)

	file, exists, err := ReadMetricsFile(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	if !exists || file == nil {
		file = &MetricsFile{Metrics: []Metric{}}
	}

	if input.Amend != nil {
		targetID := strings.TrimSpace(input.Amend.ID)
		reason := strings.TrimSpace(input.Amend.Reason)
		if targetID == "" {
			return nil, fmt.Errorf("amend_existing.id is required")
		}
		if reason == "" {
			return nil, fmt.Errorf("amend_existing.reason is required")
		}
		if targetID != candidate.ID {
			return nil, fmt.Errorf("amend_existing.id (%q) must match id (%q); to replace with a new metric id, call retire_metric then propose_metric", targetID, candidate.ID)
		}
		idx := -1
		for i := range file.Metrics {
			if file.Metrics[i].ID == candidate.ID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("amend_existing requested for metric id %q, but it is not active in planning/metrics.json", candidate.ID)
		}
		prior := file.Metrics[idx]
		priorVersion := metricVersion(prior)
		prior.Version = priorVersion
		candidate.Version = priorVersion + 1
		file.Archive = append(file.Archive, MetricArchiveEntry{
			ID:             prior.ID,
			Version:        priorVersion,
			ArchivedAt:     nowUTC(),
			ArchivedReason: reason,
			Definition:     prior,
		})
		file.Metrics[idx] = candidate

		if err := ValidateMetricsFile(file); err != nil {
			return nil, fmt.Errorf("metrics.json validation: %w", err)
		}
		if err := WriteMetricsFile(ctx, workspacePath, file); err != nil {
			return nil, fmt.Errorf("write metrics.json: %w", err)
		}

		dec := DecisionEntry{
			Source:         DecisionSourceAgent,
			Trigger:        trigger,
			Rationale:      fmt.Sprintf("metric amended: %s v%d -> v%d - %s", candidate.ID, priorVersion, candidate.Version, reason),
			AppliedChanges: []string{"planning/metrics.json"},
			TargetMetrics:  []string{candidate.ID},
		}
		if _, err := AppendDecisionEntry(ctx, workspacePath, dec); err != nil {
			return nil, fmt.Errorf("append improve.md decision: %w", err)
		}

		return &ProposeMetricOutput{
			MetricID:        candidate.ID,
			Status:          "amended",
			Version:         candidate.Version,
			ArchivedVersion: priorVersion,
			Warnings:        warnings,
		}, nil
	}

	for _, m := range file.Metrics {
		if m.ID == candidate.ID {
			return nil, fmt.Errorf("metric id %q already exists; to change its definition, call propose_metric with amend_existing: {\"id\": %q, \"reason\": \"...\"}; to intentionally start a separate series, use a different id", candidate.ID, candidate.ID)
		}
	}
	candidate.Version = metricVersion(candidate)
	file.Metrics = append(file.Metrics, candidate)

	if err := ValidateMetricsFile(file); err != nil {
		return nil, fmt.Errorf("metrics.json validation: %w", err)
	}
	if err := WriteMetricsFile(ctx, workspacePath, file); err != nil {
		return nil, fmt.Errorf("write metrics.json: %w", err)
	}

	dec := DecisionEntry{
		Source:         DecisionSourceAgent,
		Trigger:        trigger,
		Rationale:      fmt.Sprintf("metric added: %s", candidate.ID),
		AppliedChanges: []string{"planning/metrics.json"},
		TargetMetrics:  []string{candidate.ID},
	}
	if _, err := AppendDecisionEntry(ctx, workspacePath, dec); err != nil {
		return nil, fmt.Errorf("append improve.md decision: %w", err)
	}

	return &ProposeMetricOutput{
		MetricID: candidate.ID,
		Status:   "added",
		Version:  candidate.Version,
		Warnings: warnings,
	}, nil
}

func metricVersion(m Metric) int {
	if m.Version > 0 {
		return m.Version
	}
	return 1
}

// checkEvalStepReferenceExists returns a hard error when source.type=eval_step
// and source.id does not match any step in evaluation/evaluation_plan.json.
// No-op for telemetry source. The eval plan being absent is also a no-op
// (validating against a missing file would block any metric creation in
// workflows that haven't authored eval yet — same reason ValidateMetricsFile
// doesn't require the eval plan to exist).
func checkEvalStepReferenceExists(ctx context.Context, workspacePath string, src *MetricSource) error {
	if src == nil || src.Type != MetricSourceEvalStep {
		return nil
	}
	stepID := strings.TrimSpace(src.ID)
	if stepID == "" {
		return nil // ValidateMetric already enforces this
	}
	planPath := path.Join(strings.Trim(workspacePath, "/"), "evaluation", "evaluation_plan.json")
	content, exists, err := readFileFromWorkspace(ctx, planPath)
	if err != nil {
		return nil // can't read → don't block; metric creation is the wrong place to surface IO issues
	}
	if !exists || strings.TrimSpace(content) == "" {
		return nil // no eval plan yet — accept the metric and let the agent author the eval next
	}
	type evalPlanStubStep struct {
		ID string `json:"id"`
	}
	type evalPlanStub struct {
		Steps     []evalPlanStubStep `json:"steps"`
		EvalSteps []evalPlanStubStep `json:"eval_steps"`
	}
	var plan evalPlanStub
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil // unparseable plan; not the metric tool's job to validate
	}
	all := append([]evalPlanStubStep{}, plan.Steps...)
	all = append(all, plan.EvalSteps...)
	for _, s := range all {
		if strings.TrimSpace(s.ID) == stepID {
			return nil
		}
	}
	available := make([]string, 0, len(all))
	for _, s := range all {
		if id := strings.TrimSpace(s.ID); id != "" {
			available = append(available, id)
		}
	}
	sort.Strings(available)
	if len(available) == 0 {
		return fmt.Errorf("source.id %q not found — evaluation/evaluation_plan.json has no steps yet", stepID)
	}
	return fmt.Errorf("source.id %q does not exist in evaluation/evaluation_plan.json. Available step ids: [%s]", stepID, strings.Join(available, ", "))
}

// dryResolveWarnings returns non-blocking warnings about the metric/eval
// contract. Specifically, when source.type=eval_step + source.field is a
// structured-output key (not "" / "score" / "max_score"), this checks the
// most recent published evaluation_report.json for the targeted step and
// warns if that step's output does NOT currently include the named field.
//
// The metric is still accepted — the eval Python may simply need to be
// updated to emit the field. Surfacing the warning at creation time is
// strictly better than waiting for the next run's resolve_error.
func dryResolveWarnings(ctx context.Context, workspacePath string, src *MetricSource) []string {
	if src == nil || src.Type != MetricSourceEvalStep {
		return nil
	}
	field := strings.TrimSpace(src.Field)
	switch field {
	case "", "score", "max_score":
		return nil // top-level shortcuts — always resolve cleanly against any eval report
	}
	stepID := strings.TrimSpace(src.ID)
	if stepID == "" {
		return nil
	}

	reports, err := readAllEvaluationReportsFromScores(ctx, workspacePath)
	if err != nil || len(reports) == 0 {
		return nil
	}
	// Pick the most recent report by GeneratedAt (lexicographic ISO ordering).
	var latestKey string
	var latestReport EvaluationReport
	for k, r := range reports {
		if latestKey == "" || r.GeneratedAt > latestReport.GeneratedAt {
			latestKey = k
			latestReport = r
		}
	}
	for _, step := range latestReport.StepScores {
		if step.StepID != stepID {
			continue
		}
		if step.OutputContent == nil || !step.OutputContent.IsJSON {
			return []string{
				fmt.Sprintf("Eval step %q (latest report: %s) emits no structured JSON output. The metric's field=%q will return NO VALUE until the eval Python is updated to emit a JSON object containing %q. Alternative: change field to \"\" (percent score) or \"score\" (raw score).", stepID, latestKey, field, field),
			}
		}
		obj, ok := step.OutputContent.Content.(map[string]interface{})
		if !ok {
			return []string{
				fmt.Sprintf("Eval step %q output is %T, not an object — field=%q lookups won't resolve.", stepID, step.OutputContent.Content, field),
			}
		}
		if _, present := obj[field]; !present {
			keys := make([]string, 0, len(obj))
			for k := range obj {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return []string{
				fmt.Sprintf("Eval step %q (latest report: %s) does not currently emit field %q. Available fields in its output: [%s]. Either update the eval Python to emit %q, or change the metric's field to one of the available keys / a top-level shortcut.", stepID, latestKey, field, strings.Join(keys, ", "), field),
			}
		}
		return nil // field present — clean
	}
	// Step exists in the plan (we already verified) but no report yet.
	return []string{
		fmt.Sprintf("No published eval report found for step %q yet. Cannot dry-resolve field=%q until the next eval run produces a report.", stepID, field),
	}
}

// =====================================================================
// retire_metric — soft delete that moves a metric from active to archive.
// =====================================================================

// RetireMetricInput is the tool payload for retire_metric.
type RetireMetricInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// RetireMetricOutput is what the tool returns to the proposer LLM.
type RetireMetricOutput struct {
	MetricID        string `json:"metric_id"`
	Status          string `json:"status"` // "retired"
	ArchivedVersion int    `json:"archived_version,omitempty"`
}

// RetireMetric removes a metric from metrics.json::metrics[]. The metric stops
// being collected on subsequent runs. Existing rows in db/metrics_history.jsonl
// that reference the id are kept as-is — the improve.md decision entry created here
// is the audit trail for what those historical values represented.
//
// Reason is required so the improve.md decision entry traces why the metric was removed.
func RetireMetric(ctx context.Context, workspacePath, trigger string, input RetireMetricInput) (*RetireMetricOutput, error) {
	if strings.TrimSpace(input.ID) == "" {
		return nil, fmt.Errorf("retire_metric: id is required")
	}
	if strings.TrimSpace(input.Reason) == "" {
		return nil, fmt.Errorf("retire_metric: reason is required (audit trail)")
	}

	file, exists, err := ReadMetricsFile(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	if !exists || file == nil {
		return nil, fmt.Errorf("retire_metric: planning/metrics.json not found")
	}

	idx := -1
	for i := range file.Metrics {
		if file.Metrics[i].ID == input.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("retire_metric: metric id %q not found in active metrics", input.ID)
	}
	prior := file.Metrics[idx]
	priorVersion := metricVersion(prior)
	prior.Version = priorVersion

	file.Archive = append(file.Archive, MetricArchiveEntry{
		ID:             prior.ID,
		Version:        priorVersion,
		ArchivedAt:     nowUTC(),
		ArchivedReason: strings.TrimSpace(input.Reason),
		Definition:     prior,
	})

	// Remove from active.
	file.Metrics = append(file.Metrics[:idx], file.Metrics[idx+1:]...)

	if err := ValidateMetricsFile(file); err != nil {
		return nil, fmt.Errorf("metrics.json validation: %w", err)
	}
	if err := WriteMetricsFile(ctx, workspacePath, file); err != nil {
		return nil, fmt.Errorf("write metrics.json: %w", err)
	}

	dec := DecisionEntry{
		Source:         DecisionSourceAgent,
		Trigger:        trigger,
		Rationale:      fmt.Sprintf("metric retired: %s — %s", prior.ID, input.Reason),
		AppliedChanges: []string{"planning/metrics.json"},
		TargetMetrics:  []string{prior.ID},
	}
	if _, err := AppendDecisionEntry(ctx, workspacePath, dec); err != nil {
		return nil, fmt.Errorf("append improve.md decision: %w", err)
	}

	return &RetireMetricOutput{
		MetricID:        prior.ID,
		Status:          "retired",
		ArchivedVersion: priorVersion,
	}, nil
}
