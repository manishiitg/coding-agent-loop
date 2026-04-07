package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

var migratedCostWorkspaces sync.Map

func ensureWorkspaceCostMigration(ctx context.Context, workspacePath string) error {
	normalized := normalizeWorkspacePathForPresetMatch(workspacePath)
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

func workspaceAbsPath(workspacePath string) string {
	return filepath.Join(getWorkspaceDocsAbsPath(), filepath.FromSlash(filepath.Clean(workspacePath)))
}

func migrateLegacyCostFiles(ctx context.Context, workspacePath string) error {
	absWorkspacePath := workspaceAbsPath(workspacePath)
	if err := migrateLegacyScopedTokenUsage(absWorkspacePath, workspacePath, filepath.Join(absWorkspacePath, "runs"), orchestrator.CostScopeExecution); err != nil {
		return err
	}
	if err := migrateLegacyScopedTokenUsage(absWorkspacePath, workspacePath, filepath.Join(absWorkspacePath, "evaluation", "runs"), orchestrator.CostScopeEvaluation); err != nil {
		return err
	}
	if err := migrateLegacyPhaseTokenUsage(absWorkspacePath, workspacePath); err != nil {
		return err
	}
	return nil
}

func migrateLegacyScopedTokenUsage(absWorkspacePath, workspacePath, legacyRoot string, scope orchestrator.CostScope) error {
	info, err := os.Stat(legacyRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(legacyRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || d.Name() != "token_usage.json" {
			return nil
		}

		runFolder, err := filepath.Rel(legacyRoot, filepath.Dir(path))
		if err != nil {
			return err
		}
		runFolder = filepath.ToSlash(runFolder)
		if runFolder == "." || runFolder == "" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var tokenFile orchestrator.TokenUsageFile
		if err := json.Unmarshal(content, &tokenFile); err != nil {
			return nil
		}

		ts := tokenFile.CreatedAt
		if ts.IsZero() {
			ts = tokenFile.UpdatedAt
		}
		if ts.IsZero() {
			if fileInfo, statErr := os.Stat(path); statErr == nil {
				ts = fileInfo.ModTime().UTC()
			}
		}
		if ts.IsZero() {
			ts = time.Now().UTC()
		}

		targetAbsPath := filepath.Join(
			absWorkspacePath,
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

		if existingContent, readErr := os.ReadFile(targetAbsPath); readErr == nil && len(existingContent) > 0 {
			if err := json.Unmarshal(existingContent, dailyFile); err == nil && dailyFile.RunFolders == nil {
				dailyFile.RunFolders = make(map[string]*orchestrator.TokenUsageFile)
			}
		}

		dailyFile.RunFolders[runFolder] = orchestrator.MergeTokenUsageFiles(dailyFile.RunFolders[runFolder], &tokenFile)
		dailyFile.UpdatedAt = time.Now().UTC()

		if err := os.MkdirAll(filepath.Dir(targetAbsPath), 0o755); err != nil {
			return err
		}

		jsonData, err := json.MarshalIndent(dailyFile, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(targetAbsPath, jsonData, 0o644); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}

		return nil
	})
}

func migrateLegacyPhaseTokenUsage(absWorkspacePath, workspacePath string) error {
	legacyPath := filepath.Join(absWorkspacePath, "token_usage.json")
	content, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	targetAbsPath := filepath.Join(absWorkspacePath, "costs", "phase", "token_usage.json")
	if err := os.MkdirAll(filepath.Dir(targetAbsPath), 0o755); err != nil {
		return err
	}

	var legacyTokenFile orchestrator.PhaseTokenUsageFile
	if err := json.Unmarshal(content, &legacyTokenFile); err != nil {
		return nil
	}
	orchestrator.EnsurePhaseTokenUsageFilePricing(&legacyTokenFile)

	if existingContent, readErr := os.ReadFile(targetAbsPath); readErr == nil && len(existingContent) > 0 {
		var targetTokenFile orchestrator.PhaseTokenUsageFile
		if err := json.Unmarshal(existingContent, &targetTokenFile); err == nil {
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
			if err := os.WriteFile(targetAbsPath, mergedJSON, 0o644); err != nil {
				return err
			}
			if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}
	}

	if err := os.WriteFile(targetAbsPath, content, 0o644); err != nil {
		return err
	}
	if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readPhaseTokenUsageFromCosts(ctx context.Context, workspacePath string) (*orchestrator.PhaseTokenUsageFile, error) {
	if err := ensureWorkspaceCostMigration(ctx, workspacePath); err != nil {
		return nil, err
	}

	phasePath := filepath.Join(workspaceAbsPath(workspacePath), "costs", "phase", "token_usage.json")
	content, err := os.ReadFile(phasePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var tokenFile orchestrator.PhaseTokenUsageFile
	if err := json.Unmarshal(content, &tokenFile); err != nil {
		return nil, err
	}
	orchestrator.EnsurePhaseTokenUsageFilePricing(&tokenFile)
	return &tokenFile, nil
}

func readAllRunTokenUsageFromCosts(ctx context.Context, workspacePath string, scope orchestrator.CostScope) (map[string]*orchestrator.TokenUsageFile, error) {
	if err := ensureWorkspaceCostMigration(ctx, workspacePath); err != nil {
		return nil, err
	}

	root := filepath.Join(workspaceAbsPath(workspacePath), "costs", string(scope))
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return map[string]*orchestrator.TokenUsageFile{}, nil
	}

	result := make(map[string]*orchestrator.TokenUsageFile)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var dailyFile orchestrator.DailyGroupTokenUsageFile
		if err := json.Unmarshal(content, &dailyFile); err != nil {
			return nil
		}
		orchestrator.EnsureDailyGroupTokenUsageFilePricing(&dailyFile)

		for storedRunFolder, tokenFile := range dailyFile.RunFolders {
			result[storedRunFolder] = orchestrator.MergeTokenUsageFiles(result[storedRunFolder], tokenFile)
		}

		return nil
	})
	if err != nil {
		return nil, err
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

	root := filepath.Join(workspaceAbsPath(workspacePath), "costs", "phase", "daily")
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []workflowPhaseDailyCostEntry{}, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return []workflowPhaseDailyCostEntry{}, nil
	}

	entries := make([]workflowPhaseDailyCostEntry, 0)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var dailyFile orchestrator.DailyPhaseTokenUsageFile
		if err := json.Unmarshal(content, &dailyFile); err != nil {
			return nil
		}
		orchestrator.EnsureDailyPhaseTokenUsageFilePricing(&dailyFile)
		if dailyFile.TokenUsage == nil {
			return nil
		}

		date := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if strings.TrimSpace(dailyFile.Date) != "" {
			date = dailyFile.Date
		}

		entries = append(entries, workflowPhaseDailyCostEntry{
			Date:       date,
			TokenUsage: dailyFile.TokenUsage,
		})
		return nil
	})
	if err != nil {
		return nil, err
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
