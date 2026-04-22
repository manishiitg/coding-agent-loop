package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// EvaluationScoreDailyFile persists evaluation reports outside the rotating
// evaluation/runs tree so scores survive iteration pruning.
type EvaluationScoreDailyFile struct {
	Date        string                       `json:"date"`
	GroupFolder string                       `json:"group_folder"`
	UpdatedAt   time.Time                    `json:"updated_at"`
	RunFolders  map[string]*EvaluationReport `json:"run_folders"`
}

func evaluationScoreDateKey(ts time.Time) string {
	return ts.UTC().Format("2006-01-02")
}

func evaluationScoreGroupFolder(runFolder string) string {
	cleaned := strings.Trim(filepath.ToSlash(filepath.Clean(strings.TrimSpace(runFolder))), "/")
	if cleaned == "" || cleaned == "." {
		return "__ungrouped__"
	}

	parts := strings.Split(cleaned, "/")
	if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
		return strings.TrimSpace(parts[1])
	}

	return "__ungrouped__"
}

func evaluationScoreGeneratedAt(report *EvaluationReport) time.Time {
	if report != nil {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(report.GeneratedAt)); err == nil {
			return parsed.UTC()
		}
	}
	return time.Now().UTC()
}

func resolveEvaluationScoreDailyPath(workspacePath, runFolder string, ts time.Time) string {
	return filepath.Join(
		workspacePath,
		"scores",
		"evaluation",
		evaluationScoreGroupFolder(runFolder),
		evaluationScoreDateKey(ts)+".json",
	)
}

func (hcpo *StepBasedWorkflowOrchestrator) persistEvaluationScoreLedger(ctx context.Context, report *EvaluationReport, runFolder string) error {
	if report == nil {
		return nil
	}

	persistAt := evaluationScoreGeneratedAt(report)
	ledgerPath := resolveEvaluationScoreDailyPath(hcpo.GetWorkspacePath(), runFolder, persistAt)
	dailyFile := &EvaluationScoreDailyFile{
		Date:        evaluationScoreDateKey(persistAt),
		GroupFolder: evaluationScoreGroupFolder(runFolder),
		UpdatedAt:   time.Now().UTC(),
		RunFolders:  make(map[string]*EvaluationReport),
	}

	if existingContent, err := hcpo.ReadWorkspaceFile(ctx, ledgerPath); err == nil && strings.TrimSpace(existingContent) != "" {
		var existing EvaluationScoreDailyFile
		if err := json.Unmarshal([]byte(existingContent), &existing); err == nil {
			if strings.TrimSpace(existing.Date) == "" {
				existing.Date = dailyFile.Date
			}
			if strings.TrimSpace(existing.GroupFolder) == "" {
				existing.GroupFolder = dailyFile.GroupFolder
			}
			if existing.RunFolders == nil {
				existing.RunFolders = make(map[string]*EvaluationReport)
			}
			dailyFile = &existing
		}
	}

	dailyFile.Date = evaluationScoreDateKey(persistAt)
	dailyFile.GroupFolder = evaluationScoreGroupFolder(runFolder)
	dailyFile.UpdatedAt = time.Now().UTC()
	dailyFile.RunFolders[runFolder] = report

	jsonData, err := json.MarshalIndent(dailyFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal evaluation score ledger: %w", err)
	}
	if err := hcpo.WriteWorkspaceFile(ctx, ledgerPath, string(jsonData)); err != nil {
		return fmt.Errorf("failed to persist evaluation score ledger: %w", err)
	}
	return nil
}
