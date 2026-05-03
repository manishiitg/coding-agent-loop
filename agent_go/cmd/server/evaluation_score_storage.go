package server

import (
	"context"
	"encoding/json"
	"log"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	migratedEvaluationScoreWorkspaces sync.Map
	evaluationScoreMigrationMu        sync.Mutex
)

type evaluationScoreDailyFile struct {
	Date        string                       `json:"date"`
	GroupFolder string                       `json:"group_folder"`
	UpdatedAt   time.Time                    `json:"updated_at"`
	RunFolders  map[string]*EvaluationReport `json:"run_folders"`
}

func ensureWorkspaceEvaluationScoreMigration(ctx context.Context, workspacePath string) error {
	normalized := normalizeWorkspacePath(workspacePath)
	if normalized == "" {
		return nil
	}
	if _, loaded := migratedEvaluationScoreWorkspaces.Load(normalized); loaded {
		return nil
	}

	evaluationScoreMigrationMu.Lock()
	defer evaluationScoreMigrationMu.Unlock()

	if _, loaded := migratedEvaluationScoreWorkspaces.Load(normalized); loaded {
		return nil
	}

	if err := migrateLegacyEvaluationReports(ctx, normalized); err != nil {
		return err
	}

	migratedEvaluationScoreWorkspaces.Store(normalized, struct{}{})
	return nil
}

func evaluationScoreGroupFolder(runFolder string) string {
	cleaned := strings.Trim(pathpkg.Clean(strings.TrimSpace(runFolder)), "/")
	if cleaned == "" || cleaned == "." {
		return "__ungrouped__"
	}

	parts := strings.Split(cleaned, "/")
	if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
		return strings.TrimSpace(parts[1])
	}

	return "__ungrouped__"
}

func evaluationScoreDateKey(ts time.Time) string {
	return ts.UTC().Format("2006-01-02")
}

func evaluationScoreGeneratedAt(report *EvaluationReport) time.Time {
	if report != nil {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(report.GeneratedAt)); err == nil {
			return parsed.UTC()
		}
	}
	return time.Now().UTC()
}

func resolveEvaluationScorePath(workspacePath, runFolder string, ts time.Time) string {
	return workspaceCostPath(
		workspacePath,
		"scores",
		"evaluation",
		evaluationScoreGroupFolder(runFolder),
		evaluationScoreDateKey(ts)+".json",
	)
}

func migrateLegacyEvaluationReports(ctx context.Context, workspacePath string) error {
	root := workspaceCostPath(workspacePath, "evaluation", "runs")
	filePaths, err := listWorkspaceFilesRecursive(ctx, root)
	if err != nil {
		return err
	}

	for _, filePath := range filePaths {
		if pathpkg.Base(filePath) != "evaluation_report.json" {
			continue
		}

		content, exists, err := readFileFromWorkspace(ctx, filePath)
		if err != nil {
			return err
		}
		if !exists || strings.TrimSpace(content) == "" {
			continue
		}

		var report EvaluationReport
		if err := json.Unmarshal([]byte(content), &report); err != nil {
			continue
		}

		runFolder := strings.TrimPrefix(pathpkg.Dir(filePath), root+"/")
		if runFolder == pathpkg.Dir(filePath) || runFolder == "." || strings.TrimSpace(runFolder) == "" {
			continue
		}

		if err := persistEvaluationReportToScoresWithOptions(ctx, workspacePath, runFolder, &report, persistEvaluationReportOptions{
			recordMeasurement: false,
		}); err != nil {
			return err
		}
	}

	return nil
}

func persistEvaluationReportToScores(ctx context.Context, workspacePath, runFolder string, report *EvaluationReport) error {
	return persistEvaluationReportToScoresWithOptions(ctx, workspacePath, runFolder, report, persistEvaluationReportOptions{
		recordMeasurement: true,
	})
}

type persistEvaluationReportOptions struct {
	recordMeasurement bool
}

func persistEvaluationReportToScoresWithOptions(ctx context.Context, workspacePath, runFolder string, report *EvaluationReport, opts persistEvaluationReportOptions) error {
	if report == nil {
		return nil
	}

	persistAt := evaluationScoreGeneratedAt(report)
	targetPath := resolveEvaluationScorePath(workspacePath, runFolder, persistAt)
	dailyFile := &evaluationScoreDailyFile{
		Date:        evaluationScoreDateKey(persistAt),
		GroupFolder: evaluationScoreGroupFolder(runFolder),
		UpdatedAt:   time.Now().UTC(),
		RunFolders:  make(map[string]*EvaluationReport),
	}

	if existingContent, exists, readErr := readFileFromWorkspace(ctx, targetPath); readErr == nil && exists && strings.TrimSpace(existingContent) != "" {
		var existing evaluationScoreDailyFile
		if err := json.Unmarshal([]byte(existingContent), &existing); err == nil {
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
		return err
	}
	if err := writeRawFileToWorkspace(ctx, targetPath, string(jsonData)); err != nil {
		return err
	}

	if opts.recordMeasurement {
		// Auto-improvement framework hook: now that this run's eval scores are
		// persisted (so eval_step source values are queryable) and cost storage
		// has finalized (so telemetry source values are queryable), fan out to
		// any active experiments. Async — we don't want eval persistence to wait
		// on the experiment loop, and a measurement failure must not block the
		// eval pipeline.
		go func(workspace, run string) {
			bgCtx := context.Background()
			if err := RecordMeasurement(bgCtx, workspace, run); err != nil {
				log.Printf("[RECORD_MEASUREMENT] async failed for %s / %s: %v", workspace, run, err)
			}
		}(workspacePath, runFolder)
	}

	return nil
}

func readAllEvaluationReportsFromScores(ctx context.Context, workspacePath string) (map[string]EvaluationReport, error) {
	if err := ensureWorkspaceEvaluationScoreMigration(ctx, workspacePath); err != nil {
		return nil, err
	}

	root := workspaceCostPath(workspacePath, "scores", "evaluation")
	if !evaluationScoresRootExists(ctx, workspacePath) {
		return map[string]EvaluationReport{}, nil
	}

	filePaths, err := listWorkspaceFilesRecursive(ctx, root)
	if err != nil {
		return nil, err
	}

	result := make(map[string]EvaluationReport)
	for _, filePath := range filePaths {
		if !strings.HasSuffix(filePath, ".json") {
			continue
		}

		content, exists, err := readFileFromWorkspace(ctx, filePath)
		if err != nil {
			return nil, err
		}
		if !exists || strings.TrimSpace(content) == "" {
			continue
		}

		var dailyFile evaluationScoreDailyFile
		if err := json.Unmarshal([]byte(content), &dailyFile); err != nil {
			continue
		}

		for runFolder, report := range dailyFile.RunFolders {
			if report == nil {
				continue
			}

			existing, ok := result[runFolder]
			if !ok || evaluationScoreGeneratedAt(report).After(evaluationScoreGeneratedAt(&existing)) {
				result[runFolder] = *report
			}
		}
	}

	return result, nil
}

func evaluationScoresRootExists(ctx context.Context, workspacePath string) bool {
	return workspacePathExists(ctx, filepath.ToSlash(workspaceCostPath(workspacePath, "scores", "evaluation")))
}
