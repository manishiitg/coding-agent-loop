package orchestrator

import (
	"path/filepath"
	"strings"
	"time"
)

type CostScope string

const (
	CostScopeExecution  CostScope = "execution"
	CostScopeEvaluation CostScope = "evaluation"
)

type DailyGroupTokenUsageFile struct {
	Date        string                     `json:"date"`
	GroupFolder string                     `json:"group_folder"`
	UpdatedAt   time.Time                  `json:"updated_at"`
	RunFolders  map[string]*TokenUsageFile `json:"run_folders"`
}

type DailyPhaseTokenUsageFile struct {
	Date       string               `json:"date"`
	UpdatedAt  time.Time            `json:"updated_at"`
	TokenUsage *PhaseTokenUsageFile `json:"token_usage"`
}

func NormalizeCostScopeAndRunFolder(iterationFolder string) (CostScope, string) {
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(iterationFolder)))
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "." {
		return CostScopeExecution, ""
	}

	if strings.HasPrefix(cleaned, "../evaluation/runs/") {
		return CostScopeEvaluation, strings.TrimPrefix(cleaned, "../evaluation/runs/")
	}
	if strings.HasPrefix(cleaned, "evaluation/runs/") {
		return CostScopeEvaluation, strings.TrimPrefix(cleaned, "evaluation/runs/")
	}
	if strings.HasPrefix(cleaned, "runs/") {
		return CostScopeExecution, strings.TrimPrefix(cleaned, "runs/")
	}

	return CostScopeExecution, cleaned
}

func ExtractGroupFolderFromRunFolder(runFolder string) string {
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

func CostDateKey(ts time.Time) string {
	return ts.UTC().Format("2006-01-02")
}

func ResolveDailyGroupTokenUsagePath(workspacePath string, scope CostScope, runFolder string, ts time.Time) string {
	return filepath.Join(
		workspacePath,
		"costs",
		string(scope),
		ExtractGroupFolderFromRunFolder(runFolder),
		CostDateKey(ts)+".json",
	)
}

func ResolvePhaseTokenUsagePath(workspacePath string) string {
	return filepath.Join(workspacePath, "costs", "phase", "token_usage.json")
}

func ResolveDailyPhaseTokenUsagePath(workspacePath string, ts time.Time) string {
	return filepath.Join(workspacePath, "costs", "phase", "daily", CostDateKey(ts)+".json")
}

func CloneModelTokenUsage(src *ModelTokenUsage) *ModelTokenUsage {
	if src == nil {
		return nil
	}
	return &ModelTokenUsage{
		Provider:            src.Provider,
		InputTokens:         src.InputTokens,
		OutputTokens:        src.OutputTokens,
		InputTokensM:        src.InputTokensM,
		OutputTokensM:       src.OutputTokensM,
		CacheTokens:         src.CacheTokens,
		CacheTokensM:        src.CacheTokensM,
		CacheReadTokens:     src.CacheReadTokens,
		CacheReadTokensM:    src.CacheReadTokensM,
		CacheWriteTokens:    src.CacheWriteTokens,
		CacheWriteTokensM:   src.CacheWriteTokensM,
		ReasoningTokens:     src.ReasoningTokens,
		ReasoningTokensM:    src.ReasoningTokensM,
		LLMCallCount:        src.LLMCallCount,
		InputCost:           src.InputCost,
		OutputCost:          src.OutputCost,
		ReasoningCost:       src.ReasoningCost,
		CacheCost:           src.CacheCost,
		CacheReadCost:       src.CacheReadCost,
		CacheWriteCost:      src.CacheWriteCost,
		TotalCost:           src.TotalCost,
		ContextWindowUsage:  src.ContextWindowUsage,
		ModelContextWindow:  src.ModelContextWindow,
		ContextUsagePercent: src.ContextUsagePercent,
	}
}

func buildModelTokenUsage(modelTokenData *ModelTokenData) *ModelTokenUsage {
	if modelTokenData == nil {
		return nil
	}

	inputCost, outputCost, reasoningCost, cacheCost, totalCost, contextWindow := calculatePricingFromModelData(modelTokenData)
	totalTokens := modelTokenData.InputTokens + modelTokenData.OutputTokens
	var contextUsagePercent float64
	if contextWindow > 0 {
		contextUsagePercent = (float64(totalTokens) / float64(contextWindow)) * 100.0
		if contextUsagePercent > 100.0 {
			contextUsagePercent = 100.0
		}
	}

	return &ModelTokenUsage{
		Provider:            modelTokenData.Provider,
		InputTokens:         modelTokenData.InputTokens,
		OutputTokens:        modelTokenData.OutputTokens,
		InputTokensM:        formatTokensM(modelTokenData.InputTokens),
		OutputTokensM:       formatTokensM(modelTokenData.OutputTokens),
		CacheTokens:         modelTokenData.CacheTokens,
		CacheTokensM:        formatTokensM(modelTokenData.CacheTokens),
		CacheReadTokens:     modelTokenData.CacheReadTokens,
		CacheReadTokensM:    formatTokensM(modelTokenData.CacheReadTokens),
		CacheWriteTokens:    modelTokenData.CacheWriteTokens,
		CacheWriteTokensM:   formatTokensM(modelTokenData.CacheWriteTokens),
		ReasoningTokens:     modelTokenData.ReasoningTokens,
		ReasoningTokensM:    formatTokensM(modelTokenData.ReasoningTokens),
		LLMCallCount:        modelTokenData.LLMCallCount,
		InputCost:           inputCost,
		OutputCost:          outputCost,
		ReasoningCost:       reasoningCost,
		CacheCost:           cacheCost,
		CacheReadCost:       modelTokenData.CacheReadCost,
		CacheWriteCost:      modelTokenData.CacheWriteCost,
		TotalCost:           totalCost,
		ContextWindowUsage:  totalTokens,
		ModelContextWindow:  contextWindow,
		ContextUsagePercent: contextUsagePercent,
	}
}

func buildModelTokenDataFromUsage(modelID string, usage *ModelTokenUsage) *ModelTokenData {
	if usage == nil {
		return nil
	}

	return &ModelTokenData{
		ModelID:          modelID,
		Provider:         usage.Provider,
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheTokens:      usage.CacheTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		LLMCallCount:     usage.LLMCallCount,
	}
}

func EnsureModelTokenUsagePricing(modelID string, usage *ModelTokenUsage) {
	if usage == nil {
		return
	}

	modelTokenData := buildModelTokenDataFromUsage(modelID, usage)
	inputCost, outputCost, reasoningCost, cacheCost, totalCost, contextWindow := calculatePricingFromModelData(modelTokenData)

	if inputCost > 0 || outputCost > 0 || reasoningCost > 0 || cacheCost > 0 || totalCost > 0 {
		usage.InputCost = inputCost
		usage.OutputCost = outputCost
		usage.ReasoningCost = reasoningCost
		usage.CacheCost = cacheCost
		usage.CacheReadCost = modelTokenData.CacheReadCost
		usage.CacheWriteCost = modelTokenData.CacheWriteCost
		usage.TotalCost = totalCost
	}

	if usage.ModelContextWindow == 0 && contextWindow > 0 {
		usage.ModelContextWindow = contextWindow
	}
	if usage.ContextWindowUsage == 0 {
		usage.ContextWindowUsage = usage.InputTokens + usage.OutputTokens
	}
	if usage.ContextUsagePercent == 0 && usage.ModelContextWindow > 0 && usage.LLMCallCount <= 1 {
		usage.ContextUsagePercent = (float64(usage.ContextWindowUsage) / float64(usage.ModelContextWindow)) * 100.0
		if usage.ContextUsagePercent > 100.0 {
			usage.ContextUsagePercent = 100.0
		}
	}
}

func EnsureTokenUsageFilePricing(tokenFile *TokenUsageFile) {
	if tokenFile == nil {
		return
	}
	EnsureTokenUsageFileInitialized(tokenFile)

	for modelID, usage := range tokenFile.ByModel {
		EnsureModelTokenUsagePricing(modelID, usage)
	}
	for _, modelMap := range tokenFile.ByStepAndModel {
		for modelID, usage := range modelMap {
			EnsureModelTokenUsagePricing(modelID, usage)
		}
	}
}

func EnsureTokenUsageFileInitialized(tokenFile *TokenUsageFile) {
	if tokenFile == nil {
		return
	}
	if tokenFile.ByModel == nil {
		tokenFile.ByModel = make(map[string]*ModelTokenUsage)
	}
	if tokenFile.ByStepAndModel == nil {
		tokenFile.ByStepAndModel = make(map[string]map[string]*ModelTokenUsage)
	}
	if tokenFile.ByTool == nil {
		tokenFile.ByTool = make(map[string]*ToolCostUsage)
	}
	if tokenFile.ByStepAndTool == nil {
		tokenFile.ByStepAndTool = make(map[string]map[string]*ToolCostUsage)
	}
}

func EnsureDailyGroupTokenUsageFilePricing(dailyFile *DailyGroupTokenUsageFile) {
	if dailyFile == nil {
		return
	}
	for _, tokenFile := range dailyFile.RunFolders {
		EnsureTokenUsageFilePricing(tokenFile)
	}
}

func EnsurePhaseTokenUsageFilePricing(tokenFile *PhaseTokenUsageFile) {
	if tokenFile == nil {
		return
	}
	for modelID, usage := range tokenFile.ByModel {
		EnsureModelTokenUsagePricing(modelID, usage)
	}
	for _, modelMap := range tokenFile.ByPhaseAndModel {
		for modelID, usage := range modelMap {
			EnsureModelTokenUsagePricing(modelID, usage)
		}
	}
}

func EnsureDailyPhaseTokenUsageFilePricing(dailyFile *DailyPhaseTokenUsageFile) {
	if dailyFile == nil || dailyFile.TokenUsage == nil {
		return
	}
	EnsurePhaseTokenUsageFilePricing(dailyFile.TokenUsage)
}

func EnsurePhaseTokenUsageFileInitialized(tokenFile *PhaseTokenUsageFile) {
	if tokenFile == nil {
		return
	}
	if tokenFile.ByPhaseAndModel == nil {
		tokenFile.ByPhaseAndModel = make(map[string]map[string]*ModelTokenUsage)
	}
	if tokenFile.ByModel == nil {
		tokenFile.ByModel = make(map[string]*ModelTokenUsage)
	}
}

func ApplyModelTokenData(dst *ModelTokenUsage, modelTokenData *ModelTokenData) *ModelTokenUsage {
	usage := buildModelTokenUsage(modelTokenData)
	if usage == nil {
		return dst
	}
	if dst == nil {
		return usage
	}

	dst.InputTokens += usage.InputTokens
	dst.OutputTokens += usage.OutputTokens
	dst.CacheTokens += usage.CacheTokens
	dst.CacheReadTokens += usage.CacheReadTokens
	dst.CacheWriteTokens += usage.CacheWriteTokens
	dst.ReasoningTokens += usage.ReasoningTokens
	dst.LLMCallCount += usage.LLMCallCount
	dst.InputTokensM = formatTokensM(dst.InputTokens)
	dst.OutputTokensM = formatTokensM(dst.OutputTokens)
	dst.CacheTokensM = formatTokensM(dst.CacheTokens)
	dst.CacheReadTokensM = formatTokensM(dst.CacheReadTokens)
	dst.CacheWriteTokensM = formatTokensM(dst.CacheWriteTokens)
	dst.ReasoningTokensM = formatTokensM(dst.ReasoningTokens)
	dst.InputCost += usage.InputCost
	dst.OutputCost += usage.OutputCost
	dst.ReasoningCost += usage.ReasoningCost
	dst.CacheCost += usage.CacheCost
	dst.CacheReadCost += usage.CacheReadCost
	dst.CacheWriteCost += usage.CacheWriteCost
	dst.TotalCost += usage.TotalCost
	if usage.Provider != "" {
		dst.Provider = usage.Provider
	}
	if usage.ModelContextWindow > 0 {
		dst.ModelContextWindow = usage.ModelContextWindow
		if modelTokenData.LLMCallCount <= 1 {
			dst.ContextWindowUsage = usage.ContextWindowUsage
			dst.ContextUsagePercent = usage.ContextUsagePercent
		}
	}

	return dst
}

func CloneTokenUsageFile(src *TokenUsageFile) *TokenUsageFile {
	if src == nil {
		return nil
	}

	clone := &TokenUsageFile{
		CreatedAt:      src.CreatedAt,
		UpdatedAt:      src.UpdatedAt,
		ByModel:        make(map[string]*ModelTokenUsage),
		ByStepAndModel: make(map[string]map[string]*ModelTokenUsage),
		ByTool:         make(map[string]*ToolCostUsage),
		ByStepAndTool:  make(map[string]map[string]*ToolCostUsage),
	}

	for modelID, usage := range src.ByModel {
		clone.ByModel[modelID] = CloneModelTokenUsage(usage)
	}

	for stepKey, modelMap := range src.ByStepAndModel {
		clone.ByStepAndModel[stepKey] = make(map[string]*ModelTokenUsage)
		for modelID, usage := range modelMap {
			clone.ByStepAndModel[stepKey][modelID] = CloneModelTokenUsage(usage)
		}
	}

	for toolKey, usage := range src.ByTool {
		clone.ByTool[toolKey] = CloneToolCostUsage(usage)
	}

	for stepKey, toolMap := range src.ByStepAndTool {
		clone.ByStepAndTool[stepKey] = make(map[string]*ToolCostUsage)
		for toolKey, usage := range toolMap {
			clone.ByStepAndTool[stepKey][toolKey] = CloneToolCostUsage(usage)
		}
	}

	return clone
}

func CloneToolCostUsage(src *ToolCostUsage) *ToolCostUsage {
	if src == nil {
		return nil
	}
	clone := &ToolCostUsage{
		ToolName:    src.ToolName,
		Capability:  src.Capability,
		Provider:    src.Provider,
		ModelID:     src.ModelID,
		Unit:        src.Unit,
		Quantity:    src.Quantity,
		Count:       src.Count,
		TotalCost:   src.TotalCost,
		Estimated:   src.Estimated,
		OutputPaths: append([]string(nil), src.OutputPaths...),
		CreatedAt:   src.CreatedAt,
		UpdatedAt:   src.UpdatedAt,
	}
	if src.Metadata != nil {
		clone.Metadata = make(map[string]interface{}, len(src.Metadata))
		for k, v := range src.Metadata {
			clone.Metadata[k] = v
		}
	}
	return clone
}

func MergeModelTokenUsage(dst, src *ModelTokenUsage) *ModelTokenUsage {
	if src == nil {
		return dst
	}
	if dst == nil {
		return CloneModelTokenUsage(src)
	}

	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CacheTokens += src.CacheTokens
	dst.CacheReadTokens += src.CacheReadTokens
	dst.CacheWriteTokens += src.CacheWriteTokens
	dst.ReasoningTokens += src.ReasoningTokens
	dst.LLMCallCount += src.LLMCallCount
	dst.InputTokensM = formatTokensM(dst.InputTokens)
	dst.OutputTokensM = formatTokensM(dst.OutputTokens)
	dst.CacheTokensM = formatTokensM(dst.CacheTokens)
	dst.CacheReadTokensM = formatTokensM(dst.CacheReadTokens)
	dst.CacheWriteTokensM = formatTokensM(dst.CacheWriteTokens)
	dst.ReasoningTokensM = formatTokensM(dst.ReasoningTokens)
	dst.InputCost += src.InputCost
	dst.OutputCost += src.OutputCost
	dst.ReasoningCost += src.ReasoningCost
	dst.CacheCost += src.CacheCost
	dst.CacheReadCost += src.CacheReadCost
	dst.CacheWriteCost += src.CacheWriteCost
	dst.TotalCost += src.TotalCost
	if src.Provider != "" {
		dst.Provider = src.Provider
	}
	if src.ModelContextWindow > 0 {
		dst.ModelContextWindow = src.ModelContextWindow
	}
	if src.ContextWindowUsage > 0 {
		dst.ContextWindowUsage = src.ContextWindowUsage
	}
	if src.ContextUsagePercent > dst.ContextUsagePercent {
		dst.ContextUsagePercent = src.ContextUsagePercent
	}

	return dst
}

func MergeTokenUsageFiles(dst, src *TokenUsageFile) *TokenUsageFile {
	if src == nil {
		return dst
	}
	if dst == nil {
		return CloneTokenUsageFile(src)
	}

	if dst.CreatedAt.IsZero() || (!src.CreatedAt.IsZero() && src.CreatedAt.Before(dst.CreatedAt)) {
		dst.CreatedAt = src.CreatedAt
	}
	if src.UpdatedAt.After(dst.UpdatedAt) {
		dst.UpdatedAt = src.UpdatedAt
	}
	if dst.ByModel == nil {
		dst.ByModel = make(map[string]*ModelTokenUsage)
	}
	if dst.ByStepAndModel == nil {
		dst.ByStepAndModel = make(map[string]map[string]*ModelTokenUsage)
	}
	if dst.ByTool == nil {
		dst.ByTool = make(map[string]*ToolCostUsage)
	}
	if dst.ByStepAndTool == nil {
		dst.ByStepAndTool = make(map[string]map[string]*ToolCostUsage)
	}

	for modelID, usage := range src.ByModel {
		dst.ByModel[modelID] = MergeModelTokenUsage(dst.ByModel[modelID], usage)
	}

	for stepKey, modelMap := range src.ByStepAndModel {
		if dst.ByStepAndModel[stepKey] == nil {
			dst.ByStepAndModel[stepKey] = make(map[string]*ModelTokenUsage)
		}
		for modelID, usage := range modelMap {
			dst.ByStepAndModel[stepKey][modelID] = MergeModelTokenUsage(dst.ByStepAndModel[stepKey][modelID], usage)
		}
	}

	for toolKey, usage := range src.ByTool {
		dst.ByTool[toolKey] = MergeToolCostUsage(dst.ByTool[toolKey], usage)
	}

	for stepKey, toolMap := range src.ByStepAndTool {
		if dst.ByStepAndTool[stepKey] == nil {
			dst.ByStepAndTool[stepKey] = make(map[string]*ToolCostUsage)
		}
		for toolKey, usage := range toolMap {
			dst.ByStepAndTool[stepKey][toolKey] = MergeToolCostUsage(dst.ByStepAndTool[stepKey][toolKey], usage)
		}
	}

	return dst
}

func MergeToolCostUsage(dst, src *ToolCostUsage) *ToolCostUsage {
	if src == nil {
		return dst
	}
	if dst == nil {
		return CloneToolCostUsage(src)
	}

	dst.Quantity += src.Quantity
	dst.Count += src.Count
	dst.TotalCost += src.TotalCost
	dst.OutputPaths = append(dst.OutputPaths, src.OutputPaths...)
	if src.ToolName != "" {
		dst.ToolName = src.ToolName
	}
	if src.Capability != "" {
		dst.Capability = src.Capability
	}
	if src.Provider != "" {
		dst.Provider = src.Provider
	}
	if src.ModelID != "" {
		dst.ModelID = src.ModelID
	}
	if src.Unit != "" {
		dst.Unit = src.Unit
	}
	dst.Estimated = dst.Estimated || src.Estimated
	if dst.CreatedAt.IsZero() || (!src.CreatedAt.IsZero() && src.CreatedAt.Before(dst.CreatedAt)) {
		dst.CreatedAt = src.CreatedAt
	}
	if src.UpdatedAt.After(dst.UpdatedAt) {
		dst.UpdatedAt = src.UpdatedAt
	}
	if src.Metadata != nil {
		if dst.Metadata == nil {
			dst.Metadata = make(map[string]interface{}, len(src.Metadata))
		}
		for k, v := range src.Metadata {
			dst.Metadata[k] = v
		}
	}
	return dst
}

func TokenUsageLLMCostUSD(tokenFile *TokenUsageFile) float64 {
	if tokenFile == nil {
		return 0
	}
	var total float64
	for _, usage := range tokenFile.ByModel {
		if usage != nil {
			total += usage.TotalCost
		}
	}
	return total
}

func TokenUsageToolCostUSD(tokenFile *TokenUsageFile) float64 {
	if tokenFile == nil {
		return 0
	}
	var total float64
	for _, usage := range tokenFile.ByTool {
		if usage != nil {
			total += usage.TotalCost
		}
	}
	return total
}

func TokenUsageTotalCostUSD(tokenFile *TokenUsageFile) float64 {
	return TokenUsageLLMCostUSD(tokenFile) + TokenUsageToolCostUSD(tokenFile)
}

func ApplyToolCostUsage(tokenFile *TokenUsageFile, stepKey, aggregateKey string, usage *ToolCostUsage, now time.Time) {
	if tokenFile == nil || usage == nil || usage.TotalCost <= 0 {
		return
	}
	EnsureTokenUsageFileInitialized(tokenFile)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if tokenFile.CreatedAt.IsZero() {
		tokenFile.CreatedAt = now
	}
	tokenFile.UpdatedAt = now
	if usage.CreatedAt.IsZero() {
		usage.CreatedAt = now
	}
	if usage.UpdatedAt.IsZero() {
		usage.UpdatedAt = now
	}
	if aggregateKey == "" {
		aggregateKey = usage.ToolName
		if usage.Provider != "" {
			aggregateKey += ":" + usage.Provider
		}
		if usage.ModelID != "" {
			aggregateKey += ":" + usage.ModelID
		}
	}
	tokenFile.ByTool[aggregateKey] = MergeToolCostUsage(tokenFile.ByTool[aggregateKey], usage)
	if stepKey != "" {
		if tokenFile.ByStepAndTool[stepKey] == nil {
			tokenFile.ByStepAndTool[stepKey] = make(map[string]*ToolCostUsage)
		}
		tokenFile.ByStepAndTool[stepKey][aggregateKey] = MergeToolCostUsage(tokenFile.ByStepAndTool[stepKey][aggregateKey], usage)
	}
}

func ClonePhaseTokenUsageFile(src *PhaseTokenUsageFile) *PhaseTokenUsageFile {
	if src == nil {
		return nil
	}

	clone := &PhaseTokenUsageFile{
		CreatedAt:       src.CreatedAt,
		UpdatedAt:       src.UpdatedAt,
		ByPhaseAndModel: make(map[string]map[string]*ModelTokenUsage),
		ByModel:         make(map[string]*ModelTokenUsage),
	}

	for modelID, usage := range src.ByModel {
		clone.ByModel[modelID] = CloneModelTokenUsage(usage)
	}

	for phaseID, modelMap := range src.ByPhaseAndModel {
		clone.ByPhaseAndModel[phaseID] = make(map[string]*ModelTokenUsage)
		for modelID, usage := range modelMap {
			clone.ByPhaseAndModel[phaseID][modelID] = CloneModelTokenUsage(usage)
		}
	}

	return clone
}

func MergePhaseTokenUsageFiles(dst, src *PhaseTokenUsageFile) *PhaseTokenUsageFile {
	if src == nil {
		return dst
	}
	if dst == nil {
		return ClonePhaseTokenUsageFile(src)
	}

	if dst.CreatedAt.IsZero() || (!src.CreatedAt.IsZero() && src.CreatedAt.Before(dst.CreatedAt)) {
		dst.CreatedAt = src.CreatedAt
	}
	if src.UpdatedAt.After(dst.UpdatedAt) {
		dst.UpdatedAt = src.UpdatedAt
	}
	if dst.ByModel == nil {
		dst.ByModel = make(map[string]*ModelTokenUsage)
	}
	if dst.ByPhaseAndModel == nil {
		dst.ByPhaseAndModel = make(map[string]map[string]*ModelTokenUsage)
	}

	for modelID, usage := range src.ByModel {
		dst.ByModel[modelID] = MergeModelTokenUsage(dst.ByModel[modelID], usage)
	}

	for phaseID, modelMap := range src.ByPhaseAndModel {
		if dst.ByPhaseAndModel[phaseID] == nil {
			dst.ByPhaseAndModel[phaseID] = make(map[string]*ModelTokenUsage)
		}
		for modelID, usage := range modelMap {
			dst.ByPhaseAndModel[phaseID][modelID] = MergeModelTokenUsage(dst.ByPhaseAndModel[phaseID][modelID], usage)
		}
	}

	return dst
}

func ApplyModelTokenDataToPhaseTokenUsageFile(tokenFile *PhaseTokenUsageFile, phaseKey string, modelTokenData *ModelTokenData, now time.Time) {
	if tokenFile == nil || modelTokenData == nil || strings.TrimSpace(phaseKey) == "" {
		return
	}

	EnsurePhaseTokenUsageFileInitialized(tokenFile)
	if tokenFile.CreatedAt.IsZero() {
		tokenFile.CreatedAt = now
	}
	tokenFile.UpdatedAt = now

	modelID := modelTokenData.ModelID
	tokenFile.ByModel[modelID] = ApplyModelTokenData(tokenFile.ByModel[modelID], modelTokenData)
	if tokenFile.ByPhaseAndModel[phaseKey] == nil {
		tokenFile.ByPhaseAndModel[phaseKey] = make(map[string]*ModelTokenUsage)
	}
	tokenFile.ByPhaseAndModel[phaseKey][modelID] = ApplyModelTokenData(tokenFile.ByPhaseAndModel[phaseKey][modelID], modelTokenData)
}

func ApplyModelUsageToPhaseTokenUsageFile(tokenFile *PhaseTokenUsageFile, phaseKey, modelID string, usage *ModelTokenUsage, now time.Time) {
	if tokenFile == nil || usage == nil || strings.TrimSpace(phaseKey) == "" || strings.TrimSpace(modelID) == "" {
		return
	}

	EnsurePhaseTokenUsageFileInitialized(tokenFile)
	if tokenFile.CreatedAt.IsZero() {
		tokenFile.CreatedAt = now
	}
	tokenFile.UpdatedAt = now

	tokenFile.ByModel[modelID] = MergeModelTokenUsage(tokenFile.ByModel[modelID], usage)
	if tokenFile.ByPhaseAndModel[phaseKey] == nil {
		tokenFile.ByPhaseAndModel[phaseKey] = make(map[string]*ModelTokenUsage)
	}
	tokenFile.ByPhaseAndModel[phaseKey][modelID] = MergeModelTokenUsage(tokenFile.ByPhaseAndModel[phaseKey][modelID], usage)
}
