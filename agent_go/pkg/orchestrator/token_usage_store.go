package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type tokenUsageFileStore struct {
	workspacePath string
	readFile      func(context.Context, string) (string, error)
	writeFile     func(context.Context, string, string) error
	deleteFile    func(context.Context, string) error
	warnf         func(string)
	now           func() time.Time
}

func newBaseOrchestratorTokenUsageStore(bo *BaseOrchestrator) *tokenUsageFileStore {
	return &tokenUsageFileStore{
		workspacePath: bo.GetWorkspacePath(),
		readFile:      bo.ReadWorkspaceFile,
		writeFile:     bo.WriteWorkspaceFile,
		deleteFile:    bo.DeleteWorkspaceFile,
		warnf: func(msg string) {
			bo.GetLogger().Warn(msg)
		},
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *tokenUsageFileStore) parseTokenUsageFile(content string) (*TokenUsageFile, error) {
	var tokenFile TokenUsageFile
	if err := json.Unmarshal([]byte(content), &tokenFile); err != nil {
		return nil, err
	}
	EnsureTokenUsageFilePricing(&tokenFile)
	return &tokenFile, nil
}

func (s *tokenUsageFileStore) parseDailyGroupTokenUsageFile(content string) (*DailyGroupTokenUsageFile, error) {
	var dailyFile DailyGroupTokenUsageFile
	if err := json.Unmarshal([]byte(content), &dailyFile); err != nil {
		return nil, err
	}
	if dailyFile.RunFolders == nil {
		dailyFile.RunFolders = make(map[string]*TokenUsageFile)
	}
	EnsureDailyGroupTokenUsageFilePricing(&dailyFile)
	return &dailyFile, nil
}

func (s *tokenUsageFileStore) parsePhaseTokenUsageFile(content string) (*PhaseTokenUsageFile, error) {
	var tokenFile PhaseTokenUsageFile
	if err := json.Unmarshal([]byte(content), &tokenFile); err != nil {
		return nil, err
	}
	if tokenFile.ByPhaseAndModel == nil {
		tokenFile.ByPhaseAndModel = make(map[string]map[string]*ModelTokenUsage)
	}
	if tokenFile.ByModel == nil {
		tokenFile.ByModel = make(map[string]*ModelTokenUsage)
	}
	EnsurePhaseTokenUsageFilePricing(&tokenFile)
	return &tokenFile, nil
}

func (s *tokenUsageFileStore) parseDailyPhaseTokenUsageFile(content string) (*DailyPhaseTokenUsageFile, error) {
	var dailyFile DailyPhaseTokenUsageFile
	if err := json.Unmarshal([]byte(content), &dailyFile); err != nil {
		return nil, err
	}
	if dailyFile.TokenUsage == nil {
		dailyFile.TokenUsage = &PhaseTokenUsageFile{
			ByPhaseAndModel: make(map[string]map[string]*ModelTokenUsage),
			ByModel:         make(map[string]*ModelTokenUsage),
		}
	}
	if dailyFile.TokenUsage.ByPhaseAndModel == nil {
		dailyFile.TokenUsage.ByPhaseAndModel = make(map[string]map[string]*ModelTokenUsage)
	}
	if dailyFile.TokenUsage.ByModel == nil {
		dailyFile.TokenUsage.ByModel = make(map[string]*ModelTokenUsage)
	}
	EnsureDailyPhaseTokenUsageFilePricing(&dailyFile)
	return &dailyFile, nil
}

func (s *tokenUsageFileStore) legacyRunTokenUsagePath(iterationFolder string) string {
	scope, runFolder := NormalizeCostScopeAndRunFolder(iterationFolder)
	switch scope {
	case CostScopeEvaluation:
		return filepath.Join(s.workspacePath, "evaluation", "runs", runFolder, "token_usage.json")
	default:
		return filepath.Join(s.workspacePath, "runs", runFolder, "token_usage.json")
	}
}

func (s *tokenUsageFileStore) ensureRunMigrated(ctx context.Context, iterationFolder string) {
	scope, runFolder := NormalizeCostScopeAndRunFolder(iterationFolder)
	if runFolder == "" {
		return
	}

	legacyPath := s.legacyRunTokenUsagePath(iterationFolder)
	legacyContent, err := s.readFile(ctx, legacyPath)
	if err != nil || legacyContent == "" {
		return
	}

	legacyFile, err := s.parseTokenUsageFile(legacyContent)
	if err != nil {
		s.warnf(fmt.Sprintf("⚠️ Failed to parse legacy token usage file %s: %v", legacyPath, err))
		return
	}

	migrateAt := legacyFile.CreatedAt
	if migrateAt.IsZero() {
		migrateAt = legacyFile.UpdatedAt
	}
	if migrateAt.IsZero() {
		migrateAt = s.now()
	}

	targetPath := ResolveDailyGroupTokenUsagePath(s.workspacePath, scope, runFolder, migrateAt)
	dailyFile := &DailyGroupTokenUsageFile{
		Date:        CostDateKey(migrateAt),
		GroupFolder: ExtractGroupFolderFromRunFolder(runFolder),
		UpdatedAt:   s.now(),
		RunFolders:  make(map[string]*TokenUsageFile),
	}

	if existingContent, readErr := s.readFile(ctx, targetPath); readErr == nil && existingContent != "" {
		if parsedDaily, parseErr := s.parseDailyGroupTokenUsageFile(existingContent); parseErr == nil {
			dailyFile = parsedDaily
		}
	}

	dailyFile.UpdatedAt = s.now()
	dailyFile.RunFolders[runFolder] = MergeTokenUsageFiles(dailyFile.RunFolders[runFolder], legacyFile)

	jsonData, err := json.MarshalIndent(dailyFile, "", "  ")
	if err != nil {
		s.warnf(fmt.Sprintf("⚠️ Failed to marshal migrated daily token usage for %s: %v", targetPath, err))
		return
	}
	if err := s.writeFile(ctx, targetPath, string(jsonData)); err != nil {
		s.warnf(fmt.Sprintf("⚠️ Failed to write migrated daily token usage for %s: %v", targetPath, err))
		return
	}
	if err := s.deleteFile(ctx, legacyPath); err != nil && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
		s.warnf(fmt.Sprintf("⚠️ Failed to delete legacy token usage file %s after migration: %v", legacyPath, err))
	}
}

func (s *tokenUsageFileStore) readRun(ctx context.Context, iterationFolder string) *TokenUsageFile {
	if iterationFolder == "" {
		return &TokenUsageFile{
			ByModel:        make(map[string]*ModelTokenUsage),
			ByStepAndModel: make(map[string]map[string]*ModelTokenUsage),
		}
	}

	s.ensureRunMigrated(ctx, iterationFolder)

	scope, runFolder := NormalizeCostScopeAndRunFolder(iterationFolder)
	filePath := ResolveDailyGroupTokenUsagePath(s.workspacePath, scope, runFolder, s.now())
	content, err := s.readFile(ctx, filePath)
	if err != nil || content == "" {
		return &TokenUsageFile{
			ByModel:        make(map[string]*ModelTokenUsage),
			ByStepAndModel: make(map[string]map[string]*ModelTokenUsage),
		}
	}

	dailyFile, err := s.parseDailyGroupTokenUsageFile(content)
	if err != nil {
		return &TokenUsageFile{
			ByModel:        make(map[string]*ModelTokenUsage),
			ByStepAndModel: make(map[string]map[string]*ModelTokenUsage),
		}
	}

	if tokenFile := CloneTokenUsageFile(dailyFile.RunFolders[runFolder]); tokenFile != nil {
		return tokenFile
	}

	return &TokenUsageFile{
		ByModel:        make(map[string]*ModelTokenUsage),
		ByStepAndModel: make(map[string]map[string]*ModelTokenUsage),
	}
}

func (s *tokenUsageFileStore) ensurePhaseMigrated(ctx context.Context) {
	legacyPath := filepath.Join(s.workspacePath, "token_usage.json")
	legacyContent, err := s.readFile(ctx, legacyPath)
	if err != nil || legacyContent == "" {
		return
	}

	targetPath := ResolvePhaseTokenUsagePath(s.workspacePath)
	if existingContent, readErr := s.readFile(ctx, targetPath); readErr == nil && existingContent != "" {
		return
	}

	if err := s.writeFile(ctx, targetPath, legacyContent); err != nil {
		s.warnf(fmt.Sprintf("⚠️ Failed to migrate legacy phase token usage from %s to %s: %v", legacyPath, targetPath, err))
		return
	}
	if err := s.deleteFile(ctx, legacyPath); err != nil && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
		s.warnf(fmt.Sprintf("⚠️ Failed to delete legacy phase token usage file %s after migration: %v", legacyPath, err))
	}
}

func (s *tokenUsageFileStore) readPhase(ctx context.Context) *PhaseTokenUsageFile {
	s.ensurePhaseMigrated(ctx)

	filePath := ResolvePhaseTokenUsagePath(s.workspacePath)
	content, err := s.readFile(ctx, filePath)
	if err != nil || content == "" {
		return &PhaseTokenUsageFile{
			ByPhaseAndModel: make(map[string]map[string]*ModelTokenUsage),
			ByModel:         make(map[string]*ModelTokenUsage),
		}
	}

	tokenFile, err := s.parsePhaseTokenUsageFile(content)
	if err != nil {
		return &PhaseTokenUsageFile{
			ByPhaseAndModel: make(map[string]map[string]*ModelTokenUsage),
			ByModel:         make(map[string]*ModelTokenUsage),
		}
	}
	return tokenFile
}

func (s *tokenUsageFileStore) readPhaseDaily(ctx context.Context, ts time.Time) *DailyPhaseTokenUsageFile {
	filePath := ResolveDailyPhaseTokenUsagePath(s.workspacePath, ts)
	content, err := s.readFile(ctx, filePath)
	if err != nil || content == "" {
		return &DailyPhaseTokenUsageFile{
			Date:      CostDateKey(ts),
			UpdatedAt: s.now(),
			TokenUsage: &PhaseTokenUsageFile{
				ByPhaseAndModel: make(map[string]map[string]*ModelTokenUsage),
				ByModel:         make(map[string]*ModelTokenUsage),
			},
		}
	}

	dailyFile, err := s.parseDailyPhaseTokenUsageFile(content)
	if err != nil {
		return &DailyPhaseTokenUsageFile{
			Date:      CostDateKey(ts),
			UpdatedAt: s.now(),
			TokenUsage: &PhaseTokenUsageFile{
				ByPhaseAndModel: make(map[string]map[string]*ModelTokenUsage),
				ByModel:         make(map[string]*ModelTokenUsage),
			},
		}
	}
	return dailyFile
}
