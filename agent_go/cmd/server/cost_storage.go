package server

import (
	"context"
	"encoding/json"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

// normalizeWorkspacePath cleans and normalizes a workspace path for comparison.
func normalizeWorkspacePath(path string) string {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(cleaned))
}

var migratedCostWorkspaces sync.Map

func ensureWorkspaceCostMigration(ctx context.Context, workspacePath string) error {
	normalized := normalizeWorkspacePath(workspacePath)
	if normalized == "" {
		return nil
	}
	if _, loaded := migratedCostWorkspaces.Load(normalized); loaded {
		return nil
	}

	if err := migrateLegacyCostFiles(ctx, normalized); err != nil {
		return err
	}

	migratedCostWorkspaces.Store(normalized, struct{}{})
	return nil
}

func workspaceCostPath(workspacePath string, parts ...string) string {
	all := []string{normalizeWorkspacePath(workspacePath)}
	all = append(all, parts...)
	return pathpkg.Clean(pathpkg.Join(all...))
}

func migrateLegacyCostFiles(ctx context.Context, workspacePath string) error {
	if err := migrateLegacyScopedTokenUsage(ctx, workspacePath, workspaceCostPath(workspacePath, "runs"), orchestrator.CostScopeExecution); err != nil {
		return err
	}
	if err := migrateLegacyScopedTokenUsage(ctx, workspacePath, workspaceCostPath(workspacePath, "evaluation", "runs"), orchestrator.CostScopeEvaluation); err != nil {
		return err
	}
	if err := migrateLegacyPhaseTokenUsage(ctx, workspacePath); err != nil {
		return err
	}
	return nil
}

func listWorkspaceFilesRecursive(ctx context.Context, folderPath string) ([]string, error) {
	listing, _, err := listWorkspaceFolder(ctx, folderPath, 100)
	if err != nil {
		return nil, err
	}
	var filePaths []string
	collectWorkspaceFilePaths(listing, &filePaths)
	return filePaths, nil
}

func migrateLegacyScopedTokenUsage(ctx context.Context, workspacePath, legacyRoot string, scope orchestrator.CostScope) error {
	filePaths, err := listWorkspaceFilesRecursive(ctx, legacyRoot)
	if err != nil {
		return err
	}

	for _, filePath := range filePaths {
		if pathpkg.Base(filePath) != "token_usage.json" {
			continue
		}

		runFolder := strings.TrimPrefix(pathpkg.Dir(filePath), legacyRoot+"/")
		if runFolder == pathpkg.Dir(filePath) || runFolder == "." || runFolder == "" {
			continue
		}

		content, exists, err := readFileFromWorkspace(ctx, filePath)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}

		var tokenFile orchestrator.TokenUsageFile
		if err := json.Unmarshal([]byte(content), &tokenFile); err != nil {
			continue
		}

		ts := tokenFile.CreatedAt
		if ts.IsZero() {
			ts = tokenFile.UpdatedAt
		}
		if ts.IsZero() {
			ts = time.Now().UTC()
		}

		targetPath := workspaceCostPath(
			workspacePath,
			"costs",
			string(scope),
			orchestrator.ExtractGroupFolderFromRunFolder(runFolder),
			orchestrator.CostDateKey(ts)+".json",
		)

		dailyFile := &orchestrator.DailyGroupTokenUsageFile{
			Date:        orchestrator.CostDateKey(ts),
			GroupFolder: orchestrator.ExtractGroupFolderFromRunFolder(runFolder),
			UpdatedAt:   time.Now().UTC(),
			RunFolders:  make(map[string]*orchestrator.TokenUsageFile),
		}

		if existingContent, exists, readErr := readFileFromWorkspace(ctx, targetPath); readErr == nil && exists && existingContent != "" {
			if err := json.Unmarshal([]byte(existingContent), dailyFile); err == nil && dailyFile.RunFolders == nil {
				dailyFile.RunFolders = make(map[string]*orchestrator.TokenUsageFile)
			}
		}

		dailyFile.RunFolders[runFolder] = orchestrator.MergeTokenUsageFiles(dailyFile.RunFolders[runFolder], &tokenFile)
		dailyFile.UpdatedAt = time.Now().UTC()

		jsonData, err := json.MarshalIndent(dailyFile, "", "  ")
		if err != nil {
			return err
		}
		if err := writeRawFileToWorkspace(ctx, targetPath, string(jsonData)); err != nil {
			return err
		}
		if err := deleteWorkspaceFile(ctx, filePath); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyPhaseTokenUsage(ctx context.Context, workspacePath string) error {
	legacyPath := workspaceCostPath(workspacePath, "token_usage.json")
	content, exists, err := readFileFromWorkspace(ctx, legacyPath)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	targetPath := workspaceCostPath(workspacePath, "costs", "phase", "token_usage.json")

	var legacyTokenFile orchestrator.PhaseTokenUsageFile
	if err := json.Unmarshal([]byte(content), &legacyTokenFile); err != nil {
		return nil
	}
	orchestrator.EnsurePhaseTokenUsageFilePricing(&legacyTokenFile)

	if existingContent, exists, readErr := readFileFromWorkspace(ctx, targetPath); readErr == nil && exists && existingContent != "" {
		var targetTokenFile orchestrator.PhaseTokenUsageFile
		if err := json.Unmarshal([]byte(existingContent), &targetTokenFile); err == nil {
			orchestrator.EnsurePhaseTokenUsageFilePricing(&targetTokenFile)

			if targetTokenFile.ByModel == nil {
				targetTokenFile.ByModel = make(map[string]*orchestrator.ModelTokenUsage)
			}
			if targetTokenFile.ByPhaseAndModel == nil {
				targetTokenFile.ByPhaseAndModel = make(map[string]map[string]*orchestrator.ModelTokenUsage)
			}
			if legacyTokenFile.ByModel == nil {
				legacyTokenFile.ByModel = make(map[string]*orchestrator.ModelTokenUsage)
			}
			if legacyTokenFile.ByPhaseAndModel == nil {
				legacyTokenFile.ByPhaseAndModel = make(map[string]map[string]*orchestrator.ModelTokenUsage)
			}

			if targetTokenFile.CreatedAt.IsZero() || (!legacyTokenFile.CreatedAt.IsZero() && legacyTokenFile.CreatedAt.Before(targetTokenFile.CreatedAt)) {
				targetTokenFile.CreatedAt = legacyTokenFile.CreatedAt
			}
			if legacyTokenFile.UpdatedAt.After(targetTokenFile.UpdatedAt) {
				targetTokenFile.UpdatedAt = legacyTokenFile.UpdatedAt
			}

			for modelID, usage := range legacyTokenFile.ByModel {
				targetTokenFile.ByModel[modelID] = orchestrator.MergeModelTokenUsage(targetTokenFile.ByModel[modelID], usage)
			}
			for phaseID, modelMap := range legacyTokenFile.ByPhaseAndModel {
				if targetTokenFile.ByPhaseAndModel[phaseID] == nil {
					targetTokenFile.ByPhaseAndModel[phaseID] = make(map[string]*orchestrator.ModelTokenUsage)
				}
				for modelID, usage := range modelMap {
					targetTokenFile.ByPhaseAndModel[phaseID][modelID] = orchestrator.MergeModelTokenUsage(targetTokenFile.ByPhaseAndModel[phaseID][modelID], usage)
				}
			}

			mergedJSON, err := json.MarshalIndent(targetTokenFile, "", "  ")
			if err != nil {
				return err
			}
			if err := writeRawFileToWorkspace(ctx, targetPath, string(mergedJSON)); err != nil {
				return err
			}
			if err := deleteWorkspaceFile(ctx, legacyPath); err != nil {
				return err
			}
			return nil
		}
	}

	if err := writeRawFileToWorkspace(ctx, targetPath, content); err != nil {
		return err
	}
	if err := deleteWorkspaceFile(ctx, legacyPath); err != nil {
		return err
	}
	return nil
}

func readPhaseTokenUsageFromCosts(ctx context.Context, workspacePath string) (*orchestrator.PhaseTokenUsageFile, error) {
	if err := ensureWorkspaceCostMigration(ctx, workspacePath); err != nil {
		return nil, err
	}

	phasePath := workspaceCostPath(workspacePath, "costs", "phase", "token_usage.json")
	content, exists, err := readFileFromWorkspace(ctx, phasePath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	var tokenFile orchestrator.PhaseTokenUsageFile
	if err := json.Unmarshal([]byte(content), &tokenFile); err != nil {
		return nil, err
	}
	orchestrator.EnsurePhaseTokenUsageFilePricing(&tokenFile)
	return &tokenFile, nil
}

func readAllRunTokenUsageFromCosts(ctx context.Context, workspacePath string, scope orchestrator.CostScope) (map[string]*orchestrator.TokenUsageFile, error) {
	if err := ensureWorkspaceCostMigration(ctx, workspacePath); err != nil {
		return nil, err
	}

	root := workspaceCostPath(workspacePath, "costs", string(scope))

	result := make(map[string]*orchestrator.TokenUsageFile)
	filePaths, err := listWorkspaceFilesRecursive(ctx, root)
	if err != nil {
		return nil, err
	}
	for _, filePath := range filePaths {
		if !strings.HasSuffix(filePath, ".json") {
			continue
		}

		content, exists, err := readFileFromWorkspace(ctx, filePath)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}

		var dailyFile orchestrator.DailyGroupTokenUsageFile
		if err := json.Unmarshal([]byte(content), &dailyFile); err != nil {
			continue
		}
		orchestrator.EnsureDailyGroupTokenUsageFilePricing(&dailyFile)

		for storedRunFolder, tokenFile := range dailyFile.RunFolders {
			result[storedRunFolder] = orchestrator.MergeTokenUsageFiles(result[storedRunFolder], tokenFile)
		}
	}

	return result, nil
}

type workflowRunCostEntry struct {
	RunFolder            string                       `json:"run_folder"`
	TokenUsage           *orchestrator.TokenUsageFile `json:"token_usage,omitempty"`
	EvaluationTokenUsage *orchestrator.TokenUsageFile `json:"evaluation_token_usage,omitempty"`
}

type workflowPhaseDailyCostEntry struct {
	Date       string                            `json:"date"`
	TokenUsage *orchestrator.PhaseTokenUsageFile `json:"token_usage,omitempty"`
}

func buildWorkflowRunCostEntries(executionCosts, evaluationCosts map[string]*orchestrator.TokenUsageFile) []workflowRunCostEntry {
	runFolderSet := make(map[string]struct{})
	for runFolder := range executionCosts {
		runFolderSet[runFolder] = struct{}{}
	}
	for runFolder := range evaluationCosts {
		runFolderSet[runFolder] = struct{}{}
	}

	entries := make([]workflowRunCostEntry, 0, len(runFolderSet))
	for runFolder := range runFolderSet {
		entries = append(entries, workflowRunCostEntry{
			RunFolder:            runFolder,
			TokenUsage:           executionCosts[runFolder],
			EvaluationTokenUsage: evaluationCosts[runFolder],
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		iTime := workflowRunCostEntryTime(entries[i])
		jTime := workflowRunCostEntryTime(entries[j])
		if !iTime.Equal(jTime) {
			return jTime.Before(iTime)
		}
		return entries[j].RunFolder < entries[i].RunFolder
	})

	return entries
}

func workflowRunCostEntryTime(entry workflowRunCostEntry) time.Time {
	if entry.TokenUsage != nil {
		if !entry.TokenUsage.UpdatedAt.IsZero() {
			return entry.TokenUsage.UpdatedAt
		}
		if !entry.TokenUsage.CreatedAt.IsZero() {
			return entry.TokenUsage.CreatedAt
		}
	}
	if entry.EvaluationTokenUsage != nil {
		if !entry.EvaluationTokenUsage.UpdatedAt.IsZero() {
			return entry.EvaluationTokenUsage.UpdatedAt
		}
		if !entry.EvaluationTokenUsage.CreatedAt.IsZero() {
			return entry.EvaluationTokenUsage.CreatedAt
		}
	}
	return time.Time{}
}

func readAllPhaseTokenUsageFromCosts(ctx context.Context, workspacePath string) ([]workflowPhaseDailyCostEntry, error) {
	if err := ensureWorkspaceCostMigration(ctx, workspacePath); err != nil {
		return nil, err
	}

	root := workspaceCostPath(workspacePath, "costs", "phase", "daily")

	entries := make([]workflowPhaseDailyCostEntry, 0)
	filePaths, err := listWorkspaceFilesRecursive(ctx, root)
	if err != nil {
		return nil, err
	}
	for _, filePath := range filePaths {
		if !strings.HasSuffix(filePath, ".json") {
			continue
		}

		content, exists, err := readFileFromWorkspace(ctx, filePath)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}

		var dailyFile orchestrator.DailyPhaseTokenUsageFile
		if err := json.Unmarshal([]byte(content), &dailyFile); err != nil {
			continue
		}
		orchestrator.EnsureDailyPhaseTokenUsageFilePricing(&dailyFile)
		if dailyFile.TokenUsage == nil {
			continue
		}

		date := strings.TrimSuffix(pathpkg.Base(filePath), pathpkg.Ext(filePath))
		if strings.TrimSpace(dailyFile.Date) != "" {
			date = dailyFile.Date
		}

		entries = append(entries, workflowPhaseDailyCostEntry{
			Date:       date,
			TokenUsage: dailyFile.TokenUsage,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Date != entries[j].Date {
			return entries[i].Date > entries[j].Date
		}
		iUpdated := time.Time{}
		jUpdated := time.Time{}
		if entries[i].TokenUsage != nil {
			iUpdated = entries[i].TokenUsage.UpdatedAt
		}
		if entries[j].TokenUsage != nil {
			jUpdated = entries[j].TokenUsage.UpdatedAt
		}
		return jUpdated.Before(iUpdated)
	})

	return entries, nil
}
