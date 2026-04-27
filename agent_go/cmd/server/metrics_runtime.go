package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// =====================================================================
// metrics_runtime.go — read/validate/write <workflow>/metrics.json and
// resolve metric values from their declared sources.
//
// Schemas: schemas/auto-improvement.schema.json#$defs/MetricsFile
// =====================================================================

var metricIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
var metricLagPattern = regexp.MustCompile(`^\d+[smhdw]$`)

// workflowMetricsPath returns the canonical path to <workflow>/metrics.json.
func workflowMetricsPath(workspacePath string) string {
	return path.Join(strings.Trim(workspacePath, "/"), "metrics.json")
}

// ReadMetricsFile loads <workflow>/metrics.json. Returns (file, true, nil) when
// present. (nil, false, nil) when the file does not exist (no metrics yet).
func ReadMetricsFile(ctx context.Context, workspacePath string) (*MetricsFile, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, workflowMetricsPath(workspacePath))
	if err != nil {
		return nil, false, err
	}
	if !exists || strings.TrimSpace(content) == "" {
		return nil, false, nil
	}
	var file MetricsFile
	if err := json.Unmarshal([]byte(content), &file); err != nil {
		return nil, true, fmt.Errorf("parse metrics.json: %w", err)
	}
	return &file, true, nil
}

// WriteMetricsFile validates and persists <workflow>/metrics.json atomically.
func WriteMetricsFile(ctx context.Context, workspacePath string, file *MetricsFile) error {
	if file == nil {
		return fmt.Errorf("metrics file is nil")
	}
	if err := ValidateMetricsFile(file); err != nil {
		return err
	}
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metrics.json: %w", err)
	}
	return writeFileToWorkspace(ctx, workflowMetricsPath(workspacePath), string(body))
}

// ValidateMetricsFile checks structural integrity of metrics.json.
func ValidateMetricsFile(file *MetricsFile) error {
	if file == nil {
		return fmt.Errorf("file is nil")
	}
	seen := make(map[string]struct{}, len(file.Metrics))
	for i := range file.Metrics {
		if err := ValidateMetric(&file.Metrics[i]); err != nil {
			return fmt.Errorf("metrics[%d]: %w", i, err)
		}
		id := file.Metrics[i].ID
		if _, dup := seen[id]; dup {
			return fmt.Errorf("metrics: duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}
	// Parent links (used for grouping only) must resolve within the same file.
	for _, m := range file.Metrics {
		if m.Parent == "" {
			continue
		}
		if _, ok := seen[m.Parent]; !ok {
			return fmt.Errorf("metric %q: parent %q not found", m.ID, m.Parent)
		}
	}
	return nil
}

// ValidateMetric validates a single metric definition.
func ValidateMetric(m *Metric) error {
	if m == nil {
		return fmt.Errorf("metric is nil")
	}
	if !metricIDPattern.MatchString(m.ID) {
		return fmt.Errorf("id %q does not match kebab.dot pattern", m.ID)
	}
	if strings.TrimSpace(m.Unit) == "" {
		return fmt.Errorf("unit is required")
	}
	switch m.Direction {
	case HigherBetter, LowerBetter:
	default:
		return fmt.Errorf("invalid direction %q", m.Direction)
	}
	switch m.Mode {
	case MetricModeTarget:
		if m.Target == nil {
			return fmt.Errorf("mode=target requires target")
		}
	case MetricModeSLO:
		if m.Direction == HigherBetter && m.Floor == nil {
			return fmt.Errorf("mode=slo with higher_better requires floor")
		}
		if m.Direction == LowerBetter && m.Ceiling == nil {
			return fmt.Errorf("mode=slo with lower_better requires ceiling")
		}
	default:
		return fmt.Errorf("invalid mode %q", m.Mode)
	}
	if err := validateMetricSource(&m.Source); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	if m.EvaluableAtLag != "" && !metricLagPattern.MatchString(m.EvaluableAtLag) {
		return fmt.Errorf("evaluable_at_lag %q must match ^\\d+[smhdw]$", m.EvaluableAtLag)
	}
	return nil
}

func validateMetricSource(s *MetricSource) error {
	switch s.Type {
	case MetricSourceEvalStep:
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("eval_step source requires id")
		}
	case MetricSourceTelemetry:
		if strings.TrimSpace(s.Field) == "" {
			return fmt.Errorf("telemetry source requires field")
		}
	case MetricSourceExternal:
		if strings.TrimSpace(s.Field) == "" {
			return fmt.Errorf("external source requires field")
		}
	case MetricSourceDelayedGroundTruth:
		if strings.TrimSpace(s.JoinedVia) == "" {
			return fmt.Errorf("delayed_ground_truth source requires joined_via")
		}
	case MetricSourceLineage, MetricSourceSchemaCheck:
		// no required fields beyond type
	default:
		return fmt.Errorf("invalid source.type %q", s.Type)
	}
	return nil
}

// FindMetric returns the metric with the given id, or nil.
func FindMetric(file *MetricsFile, id string) *Metric {
	if file == nil {
		return nil
	}
	for i := range file.Metrics {
		if file.Metrics[i].ID == id {
			return &file.Metrics[i]
		}
	}
	return nil
}

// ResolveMetricValue extracts the metric's value for a given run from its
// declared source. Returns (value, true) when a value is available now,
// (0, false) when the value is not yet available (e.g., delayed ground truth
// or external feed has not delivered).
//
// Sources implemented in Phase 2: eval_step, telemetry. Others land later.
func ResolveMetricValue(ctx context.Context, workspacePath, runFolder string, m *Metric) (float64, bool, error) {
	if m == nil {
		return 0, false, fmt.Errorf("metric is nil")
	}
	switch m.Source.Type {
	case MetricSourceEvalStep:
		return resolveFromEvalStep(ctx, workspacePath, runFolder, m.Source.ID)
	case MetricSourceTelemetry:
		return resolveFromTelemetry(ctx, workspacePath, runFolder, m.Source.Field)
	case MetricSourceExternal, MetricSourceDelayedGroundTruth:
		// Not yet available; the experiment loop must enqueue these for later.
		return 0, false, nil
	case MetricSourceLineage, MetricSourceSchemaCheck:
		// Phase-2 stub; lineage/schema sources land with the data-pipeline workflows.
		return 0, false, nil
	default:
		return 0, false, fmt.Errorf("unsupported metric source type %q", m.Source.Type)
	}
}

// resolveFromEvalStep reads the named eval step's score/percent from this run's
// evaluation report (already persisted to /scores/evaluation/).
func resolveFromEvalStep(ctx context.Context, workspacePath, runFolder, stepID string) (float64, bool, error) {
	reports, err := readAllEvaluationReportsFromScores(ctx, workspacePath)
	if err != nil {
		return 0, false, err
	}
	report, ok := reports[runFolder]
	if !ok {
		return 0, false, nil
	}
	for _, step := range report.StepScores {
		if step.StepID != stepID {
			continue
		}
		if step.MaxScore <= 0 {
			return float64(step.Score), true, nil
		}
		// Return as percent (0-100) so units stay consistent with Metric.Unit "percent".
		return (float64(step.Score) / float64(step.MaxScore)) * 100.0, true, nil
	}
	return 0, false, nil
}

// resolveFromTelemetry reads the named field out of this run's telemetry
// payload. The telemetry layout today is the cost storage; we read the per-run
// total cost when field == "run.total_cost_usd". Other fields land as needed.
func resolveFromTelemetry(ctx context.Context, workspacePath, runFolder, field string) (float64, bool, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return 0, false, fmt.Errorf("telemetry source field is empty")
	}
	switch field {
	case "run.total_cost_usd":
		v, ok, err := readRunTotalCostUSD(ctx, workspacePath, runFolder)
		if err != nil {
			return 0, false, err
		}
		return v, ok, nil
	default:
		// Unknown telemetry field: not an error, just unavailable.
		return 0, false, nil
	}
}

// readRunTotalCostUSD scans the per-run cost files for a single number that
// represents the run's total spend. Best-effort; if storage layout doesn't
// match, returns (0, false, nil) so callers can fall back gracefully.
func readRunTotalCostUSD(ctx context.Context, workspacePath, runFolder string) (float64, bool, error) {
	// Cost storage path: workflow/runs/<iter>/<group>/costs/total_cost.json (best effort).
	candidate := path.Join(strings.Trim(workspacePath, "/"), "runs", runFolder, "costs", "total_cost.json")
	content, exists, err := readFileFromWorkspace(ctx, candidate)
	if err != nil {
		return 0, false, err
	}
	if !exists || strings.TrimSpace(content) == "" {
		return 0, false, nil
	}
	var payload struct {
		TotalUSD float64 `json:"total_usd"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return 0, false, nil
	}
	return payload.TotalUSD, true, nil
}
