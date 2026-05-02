package server

import (
	"context"
	"fmt"
	"strings"
)

// =====================================================================
// metric_propose.go — handler for the propose_metric tool.
//
// Append-only by id. To change a metric's meaning, retire the old one
// (retire_metric) and create a new one with a different id. There is no
// in-place amend or version-archive machinery — that complexity was
// removed in the metric-model simplification.
//
// Schemas: schemas/auto-improvement.schema.json#$defs/ToolInput_propose_metric
// =====================================================================

// ProposeMetricInput is the LLM-supplied portion of a metric proposal.
type ProposeMetricInput struct {
	ID        string          `json:"id,omitempty"`
	Label     string          `json:"label,omitempty"`
	Unit      string          `json:"unit,omitempty"`
	Direction MetricDirection `json:"direction,omitempty"`
	Mode      MetricMode      `json:"mode,omitempty"`
	Target    *float64        `json:"target,omitempty"`
	Floor     *float64        `json:"floor,omitempty"`
	Ceiling   *float64        `json:"ceiling,omitempty"`
	Source    MetricSource    `json:"source,omitempty"`
}

// ProposeMetricOutput is what the tool returns to the proposer LLM.
type ProposeMetricOutput struct {
	MetricID string `json:"metric_id,omitempty"`
	Status   string `json:"status"` // "added"
}

// ProposeMetric is the system entrypoint for defining a new metric. To
// change an existing metric's meaning, call retire_metric on the old one
// and ProposeMetric for a fresh metric with a new id.
//
// Trigger is the originating slash command name (used in the audit decision
// entry).
func ProposeMetric(ctx context.Context, workspacePath, trigger string, input ProposeMetricInput) (*ProposeMetricOutput, error) {
	// Soul precondition: an objective and success_criteria must exist before
	// metrics are defined against them. Without that anchor, metrics are
	// arbitrary and the framework has no north star to verdict against.
	if err := RequireSoulPreconditions(ctx, workspacePath); err != nil {
		return nil, err
	}

	candidate := Metric{
		ID:        strings.TrimSpace(input.ID),
		Label:     input.Label,
		Unit:      input.Unit,
		Direction: input.Direction,
		Mode:      input.Mode,
		Target:    input.Target,
		Floor:     input.Floor,
		Ceiling:   input.Ceiling,
		Source:    input.Source,
	}
	if err := ValidateMetric(&candidate); err != nil {
		return nil, fmt.Errorf("invalid metric: %w", err)
	}

	file, exists, err := ReadMetricsFile(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	if !exists || file == nil {
		file = &MetricsFile{Metrics: []Metric{}}
	}

	for _, m := range file.Metrics {
		if m.ID == candidate.ID {
			return nil, fmt.Errorf("metric id %q already exists; retire it first if you want to redefine", candidate.ID)
		}
	}
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
		return nil, fmt.Errorf("append decision: %w", err)
	}

	return &ProposeMetricOutput{
		MetricID: candidate.ID,
		Status:   "added",
	}, nil
}

// trimAndDedupe drops empty entries and duplicates while preserving order.
func trimAndDedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		t := strings.TrimSpace(s)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
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
	MetricID string `json:"metric_id"`
	Status   string `json:"status"` // "retired"
}

// RetireMetric removes a metric from metrics.json::metrics[]. The metric stops
// being collected on subsequent runs. Existing rows in db/metrics_history.jsonl
// that reference the id are kept as-is — the decision-log entry created here
// is the audit trail for what those historical values represented.
//
// Reason is required so the decision entry traces why the metric was removed.
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
		return nil, fmt.Errorf("append decision: %w", err)
	}

	return &RetireMetricOutput{
		MetricID: prior.ID,
		Status:   "retired",
	}, nil
}
