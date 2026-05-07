package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
)

// =====================================================================
// metrics_snapshot.go — per-run metric snapshotting.
//
// Called inline after eval step outputs are written.
// Reads planning/metrics.json (eval-step sourced metrics only), looks up
// each metric's value in the just-written evaluation_report.json, and
// writes:
//
//   runs/<runFolder>/metrics_snapshot.json   — full per-run snapshot
//   db/metrics_history.jsonl                  — append-only time series
//
// Errors are logged and swallowed so they cannot fail the eval pipeline.
//
// Types are duplicated minimally from cmd/server's auto_improvement_types
// to avoid an import cycle. Kept in sync by hand; the JSON shape on disk
// is the contract between writer and the cmd/server reader.
// =====================================================================

type metricsFileForSnapshot struct {
	Metrics []metricForSnapshot `json:"metrics"`
}

type metricForSnapshot struct {
	ID        string   `json:"id"`
	Direction string   `json:"direction"`
	Mode      string   `json:"mode"`
	Target    *float64 `json:"target,omitempty"`
	Floor     *float64 `json:"floor,omitempty"`
	Ceiling   *float64 `json:"ceiling,omitempty"`
	Source    struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		Field string `json:"field,omitempty"`
	} `json:"source"`
}

type evalReportForSnapshot struct {
	StepScores []evalStepForSnapshot `json:"step_scores"`
}

type evalStepForSnapshot struct {
	StepID        string                 `json:"step_id"`
	Score         int                    `json:"score"`
	MaxScore      int                    `json:"max_score"`
	OutputContent *evalOutputForSnapshot `json:"output_content,omitempty"`
}

type evalOutputForSnapshot struct {
	Content map[string]interface{} `json:"content"`
	IsJSON  bool                   `json:"is_json"`
}

// metricSnapshotRow is the per-metric row written to both the per-run
// snapshot file and db/metrics_history.jsonl. Field tags must match the
// cmd/server MetricSnapshotRow type byte-for-byte — that's what the
// frontend Trajectory chart consumes via /api/workflow/metrics-history.
type metricSnapshotRow struct {
	RunFolder      string   `json:"run_folder"`
	CompletedAt    string   `json:"completed_at"`
	MetricID       string   `json:"metric_id"`
	Value          float64  `json:"value"`
	HasValue       bool     `json:"has_value"`
	ResolveError   string   `json:"resolve_error,omitempty"`
	ThresholdKind  string   `json:"threshold_kind,omitempty"`
	ThresholdValue *float64 `json:"threshold_value,omitempty"`
	Passed         *bool    `json:"passed,omitempty"`
}

// snapshotRunMetrics is invoked from MaybeRunAutoEvaluation after eval has
// successfully written evaluation_report.json. runFolder is the same value
// the eval pipeline operated on (e.g., "iteration-0/default-group").
func (hcpo *StepBasedWorkflowOrchestrator) snapshotRunMetrics(ctx context.Context, runFolder string) {
	runFolder = strings.Trim(strings.TrimSpace(runFolder), "/")
	if runFolder == "" {
		return
	}

	// 1. planning/metrics.json — absent means no metrics, no-op.
	metricsRaw, err := hcpo.ReadWorkspaceFile(ctx, "planning/metrics.json")
	if err != nil || strings.TrimSpace(metricsRaw) == "" {
		return
	}
	var mf metricsFileForSnapshot
	if err := json.Unmarshal([]byte(metricsRaw), &mf); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[METRICS_SNAPSHOT] parse planning/metrics.json failed: %v", err))
		return
	}
	if len(mf.Metrics) == 0 {
		return
	}

	// 2. evaluation_report.json — the just-written eval step outputs for this run.
	reportPath := path.Join("evaluation/runs", runFolder, "evaluation_report.json")
	reportRaw, err := hcpo.ReadWorkspaceFile(ctx, reportPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[METRICS_SNAPSHOT] read %s failed: %v", reportPath, err))
		return
	}
	var report evalReportForSnapshot
	if err := json.Unmarshal([]byte(reportRaw), &report); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[METRICS_SNAPSHOT] parse %s failed: %v", reportPath, err))
		return
	}
	stepByID := make(map[string]*evalStepForSnapshot, len(report.StepScores))
	for i := range report.StepScores {
		stepByID[report.StepScores[i].StepID] = &report.StepScores[i]
	}

	// 3. Resolve each metric.
	completedAt := time.Now().UTC().Format(time.RFC3339)
	rows := make([]metricSnapshotRow, 0, len(mf.Metrics))
	for _, m := range mf.Metrics {
		row := metricSnapshotRow{
			RunFolder:   runFolder,
			CompletedAt: completedAt,
			MetricID:    m.ID,
		}
		if m.Source.Type != "eval_step" {
			row.ResolveError = fmt.Sprintf("source type %q not handled by inline snapshot (only eval_step)", m.Source.Type)
			rows = append(rows, row)
			continue
		}
		step, ok := stepByID[m.Source.ID]
		if !ok {
			row.ResolveError = fmt.Sprintf("eval step %q not in evaluation_report.json", m.Source.ID)
			rows = append(rows, row)
			continue
		}
		resolveMetricValueFromStep(&row, step, m.Source.Field)
		applyThreshold(&row, m)
		rows = append(rows, row)
	}

	// 4. Write per-run snapshot file.
	snapshotPath := path.Join("runs", runFolder, "metrics_snapshot.json")
	body, err := json.MarshalIndent(map[string]interface{}{
		"run_folder":   runFolder,
		"completed_at": completedAt,
		"rows":         rows,
	}, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[METRICS_SNAPSHOT] marshal snapshot failed: %v", err))
		return
	}
	if err := hcpo.WriteWorkspaceFile(ctx, snapshotPath, string(body)); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[METRICS_SNAPSHOT] write %s failed: %v", snapshotPath, err))
		// keep going — try the JSONL append even if per-run write failed.
	}

	// 5. Append to db/metrics_history.jsonl (read-modify-write — workspace
	// API has no streaming append).
	historyPath := "db/metrics_history.jsonl"
	existing, _ := hcpo.ReadWorkspaceFile(ctx, historyPath)
	lines := make([]string, 0, len(rows)+8)
	if trimmed := strings.TrimSpace(existing); trimmed != "" {
		for _, line := range strings.Split(trimmed, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				if strings.HasPrefix(line, "[Binary file:") {
					continue
				}
				lines = append(lines, line)
			}
		}
	}
	for _, r := range rows {
		line, err := json.Marshal(r)
		if err != nil {
			continue
		}
		lines = append(lines, string(line))
	}
	if err := hcpo.WriteWorkspaceFile(ctx, historyPath, strings.Join(lines, "\n")+"\n"); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[METRICS_SNAPSHOT] append %s failed: %v", historyPath, err))
		return
	}

	hcpo.GetLogger().Info(fmt.Sprintf("[METRICS_SNAPSHOT] wrote %d metric rows for run %s", len(rows), runFolder))
}

// resolveMetricValueFromStep mutates row with the metric's value (and
// HasValue / ResolveError as appropriate) given an eval step and an
// optional field name.
//
// Field interpretation:
//
//	field != ""        → look up the key in OutputContent.Content (preferred).
//	field == ""        → legacy: percent score for historical scored reports,
//	                     or output_content score/max_score when present.
//	field == "score" / "max_score" can still read historical top-level fields
//	when output_content does not contain those keys.
func resolveMetricValueFromStep(row *metricSnapshotRow, step *evalStepForSnapshot, field string) {
	field = strings.TrimSpace(field)
	if field != "" {
		if resolveStructuredMetricValue(row, step, field) || (field != "score" && field != "max_score") {
			return
		}
		row.ResolveError = ""
	}

	switch field {
	case "":
		if resolvePercentFromStructuredStep(row, step) {
			return
		}
		row.ResolveError = ""
		if step.MaxScore > 0 {
			row.Value = float64(step.Score) / float64(step.MaxScore) * 100.0
			row.HasValue = true
			return
		}
		row.ResolveError = fmt.Sprintf("eval step %q has no final score; set source.field to a numeric key emitted in output_content", step.StepID)
		return
	case "score":
		if step.MaxScore > 0 || step.Score != 0 {
			row.Value = float64(step.Score)
			row.HasValue = true
			return
		}
		row.ResolveError = fmt.Sprintf("eval step %q has no top-level score and output_content.score is missing", step.StepID)
		return
	case "max_score":
		if step.MaxScore > 0 {
			row.Value = float64(step.MaxScore)
			row.HasValue = true
			return
		}
		row.ResolveError = fmt.Sprintf("eval step %q has no top-level max_score and output_content.max_score is missing", step.StepID)
		return
	}
}

func resolveStructuredMetricValue(row *metricSnapshotRow, step *evalStepForSnapshot, field string) bool {
	// Structured-output field lookup.
	if step.OutputContent == nil {
		row.ResolveError = fmt.Sprintf("eval step %q has no structured output (field=%q). Update the eval step to emit a structured JSON output containing %q.", step.StepID, field, field)
		return false
	}
	if !step.OutputContent.IsJSON || step.OutputContent.Content == nil {
		row.ResolveError = fmt.Sprintf("eval step %q output is not JSON object (field=%q)", step.StepID, field)
		return false
	}
	raw, present := step.OutputContent.Content[field]
	if !present {
		row.ResolveError = fmt.Sprintf("field %q not present in eval step %q output", field, step.StepID)
		return false
	}
	switch v := raw.(type) {
	case float64:
		row.Value = v
		row.HasValue = true
	case bool:
		if v {
			row.Value = 1
		} else {
			row.Value = 0
		}
		row.HasValue = true
	case nil:
		// missing → leave HasValue=false, no error
	case string:
		row.ResolveError = fmt.Sprintf("field %q is string %q, not numeric", field, v)
	default:
		row.ResolveError = fmt.Sprintf("field %q is %T, not numeric", field, raw)
	}
	return row.HasValue || row.ResolveError != ""
}

func resolvePercentFromStructuredStep(row *metricSnapshotRow, step *evalStepForSnapshot) bool {
	scoreRow := &metricSnapshotRow{}
	if !resolveStructuredMetricValue(scoreRow, step, "score") || !scoreRow.HasValue {
		return false
	}
	maxRow := &metricSnapshotRow{}
	if resolveStructuredMetricValue(maxRow, step, "max_score") && maxRow.HasValue && maxRow.Value > 0 {
		row.Value = scoreRow.Value / maxRow.Value * 100.0
	} else {
		row.Value = scoreRow.Value
	}
	row.HasValue = true
	return true
}

// applyThreshold sets row.ThresholdKind / ThresholdValue / Passed based on
// the metric's mode + direction. Leaves them unset when no applicable
// threshold is configured (target without target value, slo without
// matching floor/ceiling for the direction).
func applyThreshold(row *metricSnapshotRow, m metricForSnapshot) {
	switch m.Mode {
	case "target":
		if m.Target == nil {
			return
		}
		row.ThresholdKind = "target"
		row.ThresholdValue = m.Target
		if row.HasValue {
			var p bool
			switch m.Direction {
			case "higher_better":
				p = row.Value >= *m.Target
			case "lower_better":
				p = row.Value <= *m.Target
			default:
				return
			}
			row.Passed = &p
		}
	case "slo":
		switch m.Direction {
		case "higher_better":
			if m.Floor == nil {
				return
			}
			row.ThresholdKind = "floor"
			row.ThresholdValue = m.Floor
			if row.HasValue {
				p := row.Value >= *m.Floor
				row.Passed = &p
			}
		case "lower_better":
			if m.Ceiling == nil {
				return
			}
			row.ThresholdKind = "ceiling"
			row.ThresholdValue = m.Ceiling
			if row.HasValue {
				p := row.Value <= *m.Ceiling
				row.Passed = &p
			}
		}
	}
}
