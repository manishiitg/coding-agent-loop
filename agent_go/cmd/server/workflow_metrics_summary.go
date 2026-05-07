package server

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strings"
)

type WorkflowMetricRunSummary struct {
	Total     int                 `json:"total"`
	WithValue int                 `json:"with_value"`
	Passed    int                 `json:"passed"`
	Failed    int                 `json:"failed"`
	Unknown   int                 `json:"unknown"`
	Rows      []MetricSnapshotRow `json:"rows,omitempty"`
}

type metricSnapshotFile struct {
	RunFolder   string              `json:"run_folder"`
	CompletedAt string              `json:"completed_at"`
	Rows        []MetricSnapshotRow `json:"rows"`
}

func loadWorkflowMetricRunSummary(ctx context.Context, workspacePath, runFolder string) *WorkflowMetricRunSummary {
	workspacePath = strings.Trim(workspacePath, "/")
	runFolder = strings.Trim(strings.TrimSpace(runFolder), "/")
	if workspacePath == "" || runFolder == "" {
		return nil
	}

	rows := readMetricRowsFromRunSnapshot(ctx, workspacePath, runFolder)
	if len(rows) == 0 {
		rows = readMetricRowsFromHistory(ctx, workspacePath, runFolder)
	}
	if len(rows) == 0 {
		return nil
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].MetricID < rows[j].MetricID
	})

	summary := &WorkflowMetricRunSummary{
		Total: len(rows),
		Rows:  rows,
	}
	for _, row := range rows {
		if row.HasValue {
			summary.WithValue++
		}
		if row.Passed == nil {
			summary.Unknown++
			continue
		}
		if *row.Passed {
			summary.Passed++
		} else {
			summary.Failed++
		}
	}
	return summary
}

func readMetricRowsFromRunSnapshot(ctx context.Context, workspacePath, runFolder string) []MetricSnapshotRow {
	snapshotPath := path.Join(workspacePath, "runs", runFolder, "metrics_snapshot.json")
	content, exists, err := readFileFromWorkspace(ctx, snapshotPath)
	if err != nil || !exists || strings.TrimSpace(content) == "" {
		return nil
	}
	var snapshot metricSnapshotFile
	if err := json.Unmarshal([]byte(content), &snapshot); err != nil {
		return nil
	}
	return snapshot.Rows
}

func readMetricRowsFromHistory(ctx context.Context, workspacePath, runFolder string) []MetricSnapshotRow {
	historyPath := path.Join(workspacePath, "db", "metrics_history.jsonl")
	rows, err := readJSONLRecords[MetricSnapshotRow](ctx, historyPath)
	if err != nil || len(rows) == 0 {
		return nil
	}

	var latestCompletedAt string
	for _, row := range rows {
		if row.RunFolder != runFolder {
			continue
		}
		if row.CompletedAt > latestCompletedAt {
			latestCompletedAt = row.CompletedAt
		}
	}
	if latestCompletedAt == "" {
		return nil
	}

	latestRows := make([]MetricSnapshotRow, 0)
	for _, row := range rows {
		if row.RunFolder == runFolder && row.CompletedAt == latestCompletedAt {
			latestRows = append(latestRows, row)
		}
	}
	return latestRows
}
