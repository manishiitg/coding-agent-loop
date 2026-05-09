package server

import (
	"context"
	"encoding/json"
	"log"
	"path"
	"strings"
	"time"
)

// =====================================================================
// metrics_snapshot.go — post-run metric snapshotter wired into the
// step-based workflow orchestrator via SetRunCompletedHook.
//
// On each successful run, reads <workspace>/planning/metrics.json,
// resolves every metric's value for that run via ResolveMetricValue,
// and writes the results to two places:
//
//   <workspace>/runs/<runFolder>/metrics_snapshot.json   (per-run, full)
//   <workspace>/db/metrics_history.jsonl                 (cross-run series)
//
// All errors are swallowed (logged via stderr). A snapshot failure must
// never fail an otherwise-successful workflow run.
// =====================================================================

// MetricSnapshotRow captures one metric's value for one run. Multiple rows
// per run (one per metric) are produced; identical row format goes into both
// the per-run snapshot file and the cross-run history JSONL.
type MetricSnapshotRow struct {
	RunFolder      string   `json:"run_folder"`
	CompletedAt    string   `json:"completed_at"`
	MetricID       string   `json:"metric_id"`
	MetricVersion  int      `json:"metric_version,omitempty"`
	Value          float64  `json:"value"`
	HasValue       bool     `json:"has_value"`
	ResolveError   string   `json:"resolve_error,omitempty"`
	ThresholdKind  string   `json:"threshold_kind,omitempty"`
	ThresholdValue *float64 `json:"threshold_value,omitempty"`
	Passed         *bool    `json:"passed,omitempty"`
}

// SnapshotRunMetrics is the RunCompletedHook implementation. Wired in by
// the server when constructing a StepBasedWorkflowOrchestrator (see
// pkg/orchestrator/types/workflow_orchestrator.go).
//
// Signature matches step_based_workflow.RunCompletedHook so it can be passed
// in directly: SetRunCompletedHook(server.SnapshotRunMetrics).
func SnapshotRunMetrics(ctx context.Context, workspacePath, runFolder, status string) {
	if status != "completed" {
		return
	}
	workspacePath = strings.TrimSpace(workspacePath)
	runFolder = strings.Trim(strings.TrimSpace(runFolder), "/")
	if workspacePath == "" || runFolder == "" {
		return
	}

	file, ok, err := ReadMetricsFile(ctx, workspacePath)
	if err != nil {
		log.Printf("[METRICS_SNAPSHOT] read metrics.json failed for %s: %v", workspacePath, err)
		return
	}
	if !ok || file == nil || len(file.Metrics) == 0 {
		return // no metrics defined for this workflow → no-op
	}

	completedAt := time.Now().UTC().Format(time.RFC3339)
	rows := make([]MetricSnapshotRow, 0, len(file.Metrics))
	for i := range file.Metrics {
		m := &file.Metrics[i]
		row := MetricSnapshotRow{
			RunFolder:     runFolder,
			CompletedAt:   completedAt,
			MetricID:      m.ID,
			MetricVersion: metricVersion(*m),
		}
		v, hasVal, rerr := ResolveMetricValue(ctx, workspacePath, runFolder, m)
		if rerr != nil {
			row.ResolveError = rerr.Error()
		} else {
			row.Value = v
			row.HasValue = hasVal
		}
		row.ThresholdKind, row.ThresholdValue = thresholdFor(m)
		row.Passed = passVerdict(m, v, hasVal)
		rows = append(rows, row)
	}

	writePerRunSnapshot(ctx, workspacePath, runFolder, completedAt, rows)
	appendToCrossRunHistory(ctx, workspacePath, rows)
}

// writePerRunSnapshot writes the full snapshot to runs/<runFolder>/metrics_snapshot.json.
// Failure is logged but not propagated.
func writePerRunSnapshot(ctx context.Context, workspacePath, runFolder, completedAt string, rows []MetricSnapshotRow) {
	snapshot := map[string]interface{}{
		"run_folder":   runFolder,
		"completed_at": completedAt,
		"rows":         rows,
	}
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		log.Printf("[METRICS_SNAPSHOT] marshal snapshot for %s failed: %v", runFolder, err)
		return
	}
	snapshotPath := path.Join(workspacePath, "runs", runFolder, "metrics_snapshot.json")
	if err := writeFileToWorkspace(ctx, snapshotPath, string(body)); err != nil {
		log.Printf("[METRICS_SNAPSHOT] write %s failed: %v", snapshotPath, err)
	}
}

// appendToCrossRunHistory appends one JSONL row per metric to db/metrics_history.jsonl.
// Uses appendJSONLRecord (read-modify-write atomic) — same pattern as decisions.
func appendToCrossRunHistory(ctx context.Context, workspacePath string, rows []MetricSnapshotRow) {
	historyPath := path.Join(workspacePath, "db", "metrics_history.jsonl")
	for i := range rows {
		if _, err := appendJSONLRecord(ctx, historyPath, rows[i]); err != nil {
			log.Printf("[METRICS_SNAPSHOT] append %s failed at row %d (metric=%s): %v",
				historyPath, i, rows[i].MetricID, err)
			return // bail rest — likely a write-permission issue, no point retrying
		}
	}
}

// thresholdFor returns the human-readable threshold kind ("target", "floor",
// "ceiling") and its value, based on the metric's mode and direction.
// Returns ("", nil) when no threshold applies (mode mismatch, or required
// field unset).
func thresholdFor(m *Metric) (string, *float64) {
	if m == nil {
		return "", nil
	}
	switch m.Mode {
	case MetricModeTarget:
		return "target", m.Target
	case MetricModeSLO:
		switch m.Direction {
		case HigherBetter:
			if m.Floor != nil {
				return "floor", m.Floor
			}
		case LowerBetter:
			if m.Ceiling != nil {
				return "ceiling", m.Ceiling
			}
		}
	}
	return "", nil
}

// passVerdict computes pass/fail against the metric's threshold. Returns nil
// (unknown) when there is no value or no applicable threshold — distinct from
// false (failed). Callers should treat nil as "not evaluable this run".
func passVerdict(m *Metric, value float64, hasValue bool) *bool {
	if !hasValue || m == nil {
		return nil
	}
	var passed bool
	switch m.Mode {
	case MetricModeTarget:
		if m.Target == nil {
			return nil
		}
		switch m.Direction {
		case HigherBetter:
			passed = value >= *m.Target
		case LowerBetter:
			passed = value <= *m.Target
		default:
			return nil
		}
	case MetricModeSLO:
		switch m.Direction {
		case HigherBetter:
			if m.Floor == nil {
				return nil
			}
			passed = value >= *m.Floor
		case LowerBetter:
			if m.Ceiling == nil {
				return nil
			}
			passed = value <= *m.Ceiling
		default:
			return nil
		}
	default:
		return nil
	}
	return &passed
}
