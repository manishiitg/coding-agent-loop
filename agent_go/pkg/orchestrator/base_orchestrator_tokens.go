package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/anthropic"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/azure"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/bedrock"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/claudecode"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/codexcli"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/cursorcli"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/picli"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/vertex"
)

// tokenFileMutex ensures thread-safe access to token_usage.json
// Prevents race conditions when multiple conversations/steps complete concurrently
// Note: TokenUsageEvent is emitted once per conversation (at end) with cumulative totals,
// so there's no duplicate counting, but file writes still need protection from concurrent access
var tokenFileMutex sync.Mutex

// formatTokensM formats raw token count as string with "M" suffix (e.g., "17.016M")
func formatTokensM(tokens int) string {
	if tokens == 0 {
		return "0.000M"
	}
	millions := float64(tokens) / 1_000_000.0
	return fmt.Sprintf("%.3fM", millions)
}

// calculateCostFromTokens calculates the cost for tokens based on model metadata
// Returns cost in USD
func calculateCostFromTokens(tokenCount int, costPer1MTokens float64) float64 {
	if tokenCount <= 0 || costPer1MTokens <= 0 {
		return 0.0
	}
	// Convert from cost per 1M tokens to cost for this token count
	return (float64(tokenCount) / 1_000_000.0) * costPer1MTokens
}

// getModelMetadata retrieves model metadata based on provider and modelID
func getModelMetadata(provider, modelID string) (*llmtypes.ModelMetadata, error) {
	resolvedProvider, resolvedModelID := resolvePricingProviderAndModel(provider, modelID)

	switch resolvedProvider {
	case "openai", "openrouter":
		// Check if it's an OpenRouter model (contains "/")
		if strings.Contains(resolvedModelID, "/") {
			return openai.GetOpenRouterModelMetadata(resolvedModelID)
		}
		return openai.GetOpenAIModelMetadata(resolvedModelID)
	case "anthropic":
		return anthropic.GetAnthropicModelMetadata(resolvedModelID)
	case "vertex":
		// Vertex supports both Gemini and Anthropic models
		if strings.Contains(resolvedModelID, "claude") || strings.Contains(resolvedModelID, "anthropic") {
			return anthropic.GetAnthropicModelMetadata(resolvedModelID)
		}
		return vertex.GetVertexGeminiModelMetadata(resolvedModelID)
	case "bedrock":
		return bedrock.GetBedrockModelMetadata(resolvedModelID)
	case "azure":
		return azure.GetAzureModelMetadata(resolvedModelID)
	case "claude-code":
		return claudecode.NewClaudeCodeInteractiveAdapter(resolvedModelID, nil).GetModelMetadata(resolvedModelID)
	case "codex-cli":
		return codexcli.NewCodexCLIAdapter("", resolvedModelID, nil).GetModelMetadata(resolvedModelID)
	case "cursor-cli":
		return cursorcli.NewCursorCLIAdapter("", resolvedModelID, nil).GetModelMetadata(resolvedModelID)
	case "pi-cli":
		return picli.NewPiCLIAdapter("", resolvedModelID, nil).GetModelMetadata(resolvedModelID)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

func resolvePricingProviderAndModel(provider, modelID string) (string, string) {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	normalizedModelID := strings.TrimSpace(modelID)
	normalizedModelLower := strings.ToLower(normalizedModelID)

	switch normalizedProvider {
	case "anthropic":
		return "anthropic", normalizeAnthropicPricingModel(normalizedModelLower, normalizedModelID)
	case "claude-code":
		return "claude-code", normalizeClaudeCodePricingModel(normalizedModelLower, normalizedModelID)
	case "codex-cli":
		if normalizedModelLower == "" || normalizedModelLower == "auto" || normalizedModelLower == "codex-cli" {
			return "codex-cli", openai.ModelGPT54
		}
		return "codex-cli", normalizedModelID
	case "cursor-cli":
		if normalizedModelLower == "" || normalizedModelLower == "auto" || normalizedModelLower == "cursor-cli" {
			return "cursor-cli", "cursor-cli"
		}
		return "cursor-cli", normalizedModelID
	case "pi-cli":
		if normalizedModelLower == "" || normalizedModelLower == "auto" || normalizedModelLower == "pi-cli" {
			return "pi-cli", "google/gemini-3.5-flash"
		}
		return "pi-cli", normalizedModelID
	default:
		return normalizedProvider, normalizedModelID
	}
}

func normalizeAnthropicPricingModel(modelLower, originalModelID string) string {
	switch modelLower {
	case "opus", "claude-opus":
		return anthropic.ModelClaudeOpus48
	case "sonnet", "claude-sonnet":
		return anthropic.ModelClaudeSonnet46
	case "haiku", "claude-haiku":
		return anthropic.ModelClaudeHaiku45
	}

	modelKey := pricingAliasKey(modelLower)
	switch {
	case strings.Contains(modelKey, "opus-4-8") || strings.Contains(modelKey, "4-8-opus"):
		return anthropic.ModelClaudeOpus48
	case strings.Contains(modelKey, "opus-4-6") || strings.Contains(modelKey, "4-6-opus"):
		return anthropic.ModelClaudeOpus46
	case strings.Contains(modelKey, "opus-4-5") || strings.Contains(modelKey, "4-5-opus"):
		return anthropic.ModelClaudeOpus45
	case strings.Contains(modelKey, "sonnet-4-6") || strings.Contains(modelKey, "4-6-sonnet"):
		return anthropic.ModelClaudeSonnet46
	case strings.Contains(modelKey, "sonnet-4-5") || strings.Contains(modelKey, "4-5-sonnet"):
		return anthropic.ModelClaudeSonnet45
	case strings.Contains(modelKey, "haiku-4-5") || strings.Contains(modelKey, "4-5-haiku"):
		return anthropic.ModelClaudeHaiku45
	default:
		return originalModelID
	}
}

func normalizeClaudeCodePricingModel(modelLower, originalModelID string) string {
	switch modelLower {
	case "", "auto", "claude-code":
		return "claude-opus-4-8"
	case "opus", "claude-opus":
		return "claude-opus-4-8"
	case "sonnet", "claude-sonnet":
		return "claude-sonnet-5"
	case "fable", "claude-fable":
		return "claude-fable-5"
	}

	modelKey := pricingAliasKey(modelLower)
	switch {
	case strings.Contains(modelKey, "fable-5"):
		return "claude-fable-5"
	case strings.Contains(modelKey, "opus-4-8") || strings.Contains(modelKey, "4-8-opus"):
		return "claude-opus-4-8"
	case strings.Contains(modelKey, "opus-4-7") || strings.Contains(modelKey, "4-7-opus"):
		return "claude-opus-4-7"
	case strings.Contains(modelKey, "opus-4-6") || strings.Contains(modelKey, "4-6-opus"):
		return "claude-opus-4-6"
	case strings.Contains(modelKey, "sonnet-5") || strings.Contains(modelKey, "5-sonnet"):
		return "claude-sonnet-5"
	case strings.Contains(modelKey, "sonnet-4-6") || strings.Contains(modelKey, "4-6-sonnet"):
		return "claude-sonnet-4-6"
	default:
		return originalModelID
	}
}

func pricingAliasKey(modelLower string) string {
	modelKey := strings.NewReplacer("_", "-", " ", "-", ".", "-").Replace(modelLower)
	for strings.Contains(modelKey, "--") {
		modelKey = strings.ReplaceAll(modelKey, "--", "-")
	}
	return strings.Trim(modelKey, "-")
}

// CachePricing holds separated cache read and write costs
type CachePricing struct {
	ReadCost  float64 // Cost for cache reads (discounted rate)
	WriteCost float64 // Cost for cache writes (premium rate, 1.25x)
	TotalCost float64 // Total cache cost (read + write)
}

// calculatePricingFromModelData calculates pricing from ModelTokenData using model metadata
// Returns input, output, reasoning, cache costs, total cost, and context window size
// Cache costs are now separated into read (discounted) and write (premium 1.25x) components
func calculatePricingFromModelData(modelData *ModelTokenData) (inputCost, outputCost, reasoningCost, cacheCost, totalCost float64, contextWindow int) {
	if modelData == nil {
		return 0, 0, 0, 0, 0, 0
	}

	// Get model metadata
	metadata, err := getModelMetadata(modelData.Provider, modelData.ModelID)
	if err != nil || metadata == nil {
		// If metadata is not available, return zeros (pricing will be 0)
		return 0, 0, 0, 0, 0, 0
	}

	contextWindow = metadata.ContextWindow

	// Calculate input cost (excluding cached tokens which are charged separately)
	// Input tokens = total input tokens - all cached tokens (both read and write are charged separately)
	totalCacheTokens := modelData.CacheReadTokens + modelData.CacheWriteTokens
	// Fall back to CacheTokens if separate fields are not set (backward compatibility)
	if totalCacheTokens == 0 && modelData.CacheTokens > 0 {
		totalCacheTokens = modelData.CacheTokens
	}
	inputTokens := modelData.InputTokens - totalCacheTokens
	if inputTokens < 0 {
		// Safety check: cache tokens should not exceed input tokens
		// This could indicate a data inconsistency, but we'll clamp to 0 to prevent negative costs
		inputTokens = 0
	}
	if inputTokens > 0 {
		inputCost = calculateCostFromTokens(inputTokens, metadata.InputCostPer1MTokens)
	}

	// Calculate output cost
	if modelData.OutputTokens > 0 {
		outputCost = calculateCostFromTokens(modelData.OutputTokens, metadata.OutputCostPer1MTokens)
	}

	// Calculate reasoning cost
	if modelData.ReasoningTokens > 0 && metadata.ReasoningCostPer1MTokens > 0 {
		reasoningCost = calculateCostFromTokens(modelData.ReasoningTokens, metadata.ReasoningCostPer1MTokens)
	}

	// Calculate cache costs separately for read and write
	// Cache read tokens are charged at discounted rate (CachedInputCostPer1MTokens)
	// Cache write tokens are charged at premium rate (CachedInputCostWritePer1MTokens, typically 1.25x base)
	var cacheReadCost, cacheWriteCost float64

	if modelData.CacheReadTokens > 0 && metadata.CachedInputCostPer1MTokens > 0 {
		cacheReadCost = calculateCostFromTokens(modelData.CacheReadTokens, metadata.CachedInputCostPer1MTokens)
	}

	if modelData.CacheWriteTokens > 0 && metadata.CachedInputCostWritePer1MTokens > 0 {
		cacheWriteCost = calculateCostFromTokens(modelData.CacheWriteTokens, metadata.CachedInputCostWritePer1MTokens)
	}

	// Backward compatibility: if separate cache fields are not set but CacheTokens is,
	// treat all as read tokens (conservative estimate - discounted rate)
	if modelData.CacheReadTokens == 0 && modelData.CacheWriteTokens == 0 && modelData.CacheTokens > 0 {
		if metadata.CachedInputCostPer1MTokens > 0 {
			cacheReadCost = calculateCostFromTokens(modelData.CacheTokens, metadata.CachedInputCostPer1MTokens)
		}
	}

	cacheCost = cacheReadCost + cacheWriteCost

	// Store the separated costs back to modelData for persistence
	modelData.CacheReadCost = cacheReadCost
	modelData.CacheWriteCost = cacheWriteCost
	modelData.CacheCost = cacheCost

	totalCost = inputCost + outputCost + reasoningCost + cacheCost

	return inputCost, outputCost, reasoningCost, cacheCost, totalCost, contextWindow
}

// GetStepTokenUsage reads token usage from file for a specific step (aggregated across all models)
func (bo *BaseOrchestrator) GetStepTokenUsage(phase string, step int, stepID string) *StepTokenUsage {
	if bo.iterationFolder == "" {
		return &StepTokenUsage{}
	}

	ctx := context.Background()
	tokenFile := newBaseOrchestratorTokenUsageStore(bo).readRun(ctx, bo.iterationFolder)

	// Use stepID if available (new format), otherwise fall back to numeric index (old format)
	var stepKey string
	if stepID != "" {
		stepKey = fmt.Sprintf("%s:%s", phase, stepID)
	} else {
		stepKey = fmt.Sprintf("%s:%d", phase, step)
	}
	modelMap, exists := tokenFile.ByStepAndModel[stepKey]
	if !exists || modelMap == nil {
		return &StepTokenUsage{}
	}

	// Aggregate across all models for this step
	result := &StepTokenUsage{}
	var maxContextUsagePercent float64
	for _, modelUsage := range modelMap {
		result.InputTokens += modelUsage.InputTokens
		result.OutputTokens += modelUsage.OutputTokens
		result.CacheTokens += modelUsage.CacheTokens
		result.ReasoningTokens += modelUsage.ReasoningTokens
		result.LLMCallCount += modelUsage.LLMCallCount
		// Aggregate pricing
		result.InputCost += modelUsage.InputCost
		result.OutputCost += modelUsage.OutputCost
		result.ReasoningCost += modelUsage.ReasoningCost
		result.CacheCost += modelUsage.CacheCost
		result.TotalCost += modelUsage.TotalCost
		// Track max context usage percentage
		if modelUsage.ContextUsagePercent > maxContextUsagePercent {
			maxContextUsagePercent = modelUsage.ContextUsagePercent
		}
	}
	result.ContextUsagePercent = maxContextUsagePercent

	return result
}

// EmitStepTokenUsage reads token usage from file and emits a step token usage summary event
// Aggregates tokens across all models used in the step
func (bo *BaseOrchestrator) EmitStepTokenUsage(ctx context.Context, phase string, step int, stepID string, stepTitle string, clearAfterEmit bool) {
	if bo.iterationFolder == "" {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ No iteration folder, cannot read token usage for step %s:%s", phase, stepID))
		return
	}

	// Read token usage from today's cost bucket for the current run folder.
	tokenFile := newBaseOrchestratorTokenUsageStore(bo).readRun(ctx, bo.iterationFolder)
	if tokenFile == nil || len(tokenFile.ByModel) == 0 && len(tokenFile.ByStepAndModel) == 0 {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ No token usage file found for step %s:%s", phase, stepID))
		return
	}

	// Find step data and aggregate across all models
	// Use stepID if available (new format), otherwise fall back to numeric index (old format)
	var stepKey string
	if stepID != "" {
		stepKey = fmt.Sprintf("%s:%s", phase, stepID)
	} else {
		stepKey = fmt.Sprintf("%s:%d", phase, step)
	}
	modelMap, exists := tokenFile.ByStepAndModel[stepKey]
	if !exists || modelMap == nil || len(modelMap) == 0 {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ No token usage data found for step %s (key: %s)", stepTitle, stepKey))
		return
	}

	// Aggregate tokens and pricing across all models for this step
	var inputTokens, outputTokens, cacheTokens, reasoningTokens, llmCallCount int
	var inputCost, outputCost, reasoningCost, cacheCost, totalCost float64
	var maxContextUsagePercent float64
	for _, modelUsage := range modelMap {
		inputTokens += modelUsage.InputTokens
		outputTokens += modelUsage.OutputTokens
		cacheTokens += modelUsage.CacheTokens
		reasoningTokens += modelUsage.ReasoningTokens
		llmCallCount += modelUsage.LLMCallCount
		// Aggregate pricing
		inputCost += modelUsage.InputCost
		outputCost += modelUsage.OutputCost
		reasoningCost += modelUsage.ReasoningCost
		cacheCost += modelUsage.CacheCost
		totalCost += modelUsage.TotalCost
		// Track max context usage percentage
		if modelUsage.ContextUsagePercent > maxContextUsagePercent {
			maxContextUsagePercent = modelUsage.ContextUsagePercent
		}
	}

	// Calculate total for event
	totalTokens := inputTokens + outputTokens

	// Create and emit step token usage event with pricing
	stepTokenEvent := events.NewStepTokenUsageEventWithPricing(
		phase,
		step,
		stepTitle,
		inputTokens,  // prompt_tokens in event
		outputTokens, // completion_tokens in event
		totalTokens,  // total_tokens in event
		cacheTokens,
		reasoningTokens,
		llmCallCount,
		0, // CacheEnabledCallCount not stored in file (could be calculated if needed)
		inputCost,
		outputCost,
		reasoningCost,
		cacheCost,
		totalCost,
		maxContextUsagePercent,
	)

	bo.emitEvent(ctx, events.StepTokenUsage, stepTokenEvent)

	bo.GetLogger().Info(fmt.Sprintf("📊 Emitted step token usage for %s:%s (%s) - Input: %d, Output: %d, Cache: %d, Reasoning: %d, Calls: %d, Cost: $%.4f, Context: %.1f%%",
		phase, stepID, stepTitle, inputTokens, outputTokens, cacheTokens, reasoningTokens, llmCallCount, totalCost, maxContextUsagePercent))
}

// GetModelTokenUsage reads token usage from file for a specific model
func (bo *BaseOrchestrator) GetModelTokenUsage(modelID string) *ModelTokenUsage {
	if bo.iterationFolder == "" {
		return &ModelTokenUsage{}
	}

	ctx := context.Background()
	tokenFile := newBaseOrchestratorTokenUsageStore(bo).readRun(ctx, bo.iterationFolder)

	usage, exists := tokenFile.ByModel[modelID]
	if !exists {
		return &ModelTokenUsage{}
	}

	return usage
}

// GetAllModelTokenUsage reads all model token usage from file
func (bo *BaseOrchestrator) GetAllModelTokenUsage() map[string]*ModelTokenUsage {
	if bo.iterationFolder == "" {
		return make(map[string]*ModelTokenUsage)
	}

	ctx := context.Background()
	tokenFile := newBaseOrchestratorTokenUsageStore(bo).readRun(ctx, bo.iterationFolder)

	return tokenFile.ByModel
}

func (bo *BaseOrchestrator) GetCurrentRunTokenUsageFile() *TokenUsageFile {
	return newBaseOrchestratorTokenUsageStore(bo).readRun(context.Background(), bo.iterationFolder)
}

// GetStepModelTokenUsage reads token usage from file for a specific step and model
func (bo *BaseOrchestrator) GetStepModelTokenUsage(phase string, step int, stepID string, modelID string) *ModelTokenUsage {
	if bo.iterationFolder == "" {
		return &ModelTokenUsage{}
	}

	ctx := context.Background()
	tokenFile := newBaseOrchestratorTokenUsageStore(bo).readRun(ctx, bo.iterationFolder)

	// Use stepID if available (new format), otherwise fall back to numeric index (old format)
	var stepKey string
	if stepID != "" {
		stepKey = fmt.Sprintf("%s:%s", phase, stepID)
	} else {
		stepKey = fmt.Sprintf("%s:%d", phase, step)
	}
	if tokenFile.ByStepAndModel == nil {
		return &ModelTokenUsage{}
	}

	modelMap, exists := tokenFile.ByStepAndModel[stepKey]
	if !exists || modelMap == nil {
		return &ModelTokenUsage{}
	}

	usage, exists := modelMap[modelID]
	if !exists {
		return &ModelTokenUsage{}
	}

	return usage
}

// GetStepModels reads all models used in a specific step from file
func (bo *BaseOrchestrator) GetStepModels(phase string, step int, stepID string) map[string]*ModelTokenUsage {
	if bo.iterationFolder == "" {
		return make(map[string]*ModelTokenUsage)
	}

	ctx := context.Background()
	tokenFile := newBaseOrchestratorTokenUsageStore(bo).readRun(ctx, bo.iterationFolder)

	// Use stepID if available (new format), otherwise fall back to numeric index (old format)
	var stepKey string
	if stepID != "" {
		stepKey = fmt.Sprintf("%s:%s", phase, stepID)
	} else {
		stepKey = fmt.Sprintf("%s:%d", phase, step)
	}
	if tokenFile.ByStepAndModel == nil {
		return make(map[string]*ModelTokenUsage)
	}

	modelMap, exists := tokenFile.ByStepAndModel[stepKey]
	if !exists || modelMap == nil {
		return make(map[string]*ModelTokenUsage)
	}

	// Return a copy to avoid external modifications
	result := make(map[string]*ModelTokenUsage)
	for modelID, usage := range modelMap {
		result[modelID] = &ModelTokenUsage{
			Provider:            usage.Provider,
			InputTokens:         usage.InputTokens,
			OutputTokens:        usage.OutputTokens,
			InputTokensM:        usage.InputTokensM,
			OutputTokensM:       usage.OutputTokensM,
			CacheTokens:         usage.CacheTokens,
			CacheTokensM:        usage.CacheTokensM,
			ReasoningTokens:     usage.ReasoningTokens,
			ReasoningTokensM:    usage.ReasoningTokensM,
			LLMCallCount:        usage.LLMCallCount,
			InputCost:           usage.InputCost,
			OutputCost:          usage.OutputCost,
			ReasoningCost:       usage.ReasoningCost,
			CacheCost:           usage.CacheCost,
			TotalCost:           usage.TotalCost,
			ContextWindowUsage:  usage.ContextWindowUsage,
			ModelContextWindow:  usage.ModelContextWindow,
			ContextUsagePercent: usage.ContextUsagePercent,
		}
	}

	return result
}

// PersistTokenUsage saves token usage into the daily group cost bucket under costs/.
// The daily file is keyed by scope + group + UTC date, and contains one aggregate
// TokenUsageFile per run folder for that date.
func (bo *BaseOrchestrator) PersistTokenUsage(ctx context.Context, iterationFolder string,
	stepTokenData *StepTokenData, modelTokenData *ModelTokenData) error {
	if iterationFolder == "" {
		// Removed verbose logging
		return nil
	}

	// Acquire mutex to prevent race conditions when multiple conversations/steps complete concurrently
	tokenFileMutex.Lock()
	defer tokenFileMutex.Unlock()

	store := newBaseOrchestratorTokenUsageStore(bo)
	store.ensureRunMigrated(ctx, iterationFolder)

	scope, runFolder := NormalizeCostScopeAndRunFolder(iterationFolder)
	if runFolder == "" {
		return nil
	}

	workspacePath := bo.GetWorkspacePath()
	now := time.Now().UTC()
	filePath := ResolveDailyGroupTokenUsagePath(workspacePath, scope, runFolder, now)

	bo.GetLogger().Debug(fmt.Sprintf("💾 Persisting token usage to: %s", filePath))

	// Read the existing daily bucket if it exists.
	dailyFile := &DailyGroupTokenUsageFile{
		Date:        CostDateKey(now),
		GroupFolder: ExtractGroupFolderFromRunFolder(runFolder),
		UpdatedAt:   now,
		RunFolders:  make(map[string]*TokenUsageFile),
	}
	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err == nil && existingContent != "" {
		if parsedDaily, parseErr := store.parseDailyGroupTokenUsageFile(existingContent); parseErr == nil {
			dailyFile = parsedDaily
			dailyFile.Date = CostDateKey(now)
			dailyFile.GroupFolder = ExtractGroupFolderFromRunFolder(runFolder)
			dailyFile.UpdatedAt = now
		} else {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse existing daily token usage file: %v (will create new file)", parseErr))
		}
	} else if err != nil {
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read daily token usage file %s: %v", filePath, err))
		}
	}

	// Start from the existing aggregate for this run folder inside the daily file.
	tokenFile := CloneTokenUsageFile(dailyFile.RunFolders[runFolder])
	if tokenFile == nil {
		tokenFile = &TokenUsageFile{
			CreatedAt:      now,
			ByModel:        make(map[string]*ModelTokenUsage),
			ByStepAndModel: make(map[string]map[string]*ModelTokenUsage),
		}
	}
	if tokenFile.ByModel == nil {
		tokenFile.ByModel = make(map[string]*ModelTokenUsage)
	}
	if tokenFile.ByStepAndModel == nil {
		tokenFile.ByStepAndModel = make(map[string]map[string]*ModelTokenUsage)
	}
	if tokenFile.CreatedAt.IsZero() {
		tokenFile.CreatedAt = now
	}
	tokenFile.UpdatedAt = now

	// Merge new model token data if provided
	if modelTokenData != nil {
		tokenFile.ByModel[modelTokenData.ModelID] = ApplyModelTokenData(tokenFile.ByModel[modelTokenData.ModelID], modelTokenData)
	}

	// Store step+model token data if both stepTokenData and modelTokenData are provided
	if stepTokenData != nil && modelTokenData != nil {
		// Use StepID if available (new format), otherwise fall back to step index (old format)
		var stepKey string
		if stepTokenData.StepID != "" {
			stepKey = fmt.Sprintf("%s:%s", stepTokenData.Phase, stepTokenData.StepID)
		} else {
			stepKey = fmt.Sprintf("%s:%d", stepTokenData.Phase, stepTokenData.Step)
		}
		modelID := modelTokenData.ModelID

		// Initialize step map if it doesn't exist
		if tokenFile.ByStepAndModel[stepKey] == nil {
			tokenFile.ByStepAndModel[stepKey] = make(map[string]*ModelTokenUsage)
		}
		tokenFile.ByStepAndModel[stepKey][modelID] = ApplyModelTokenData(tokenFile.ByStepAndModel[stepKey][modelID], modelTokenData)
	}

	dailyFile.UpdatedAt = now
	dailyFile.RunFolders[runFolder] = tokenFile

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(dailyFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token usage data: %w", err)
	}

	// Write to file
	if err := bo.WriteWorkspaceFile(ctx, filePath, string(jsonData)); err != nil {
		return fmt.Errorf("failed to write token usage file: %w", err)
	}

	bo.GetLogger().Debug("✅ Persisted token usage to file")

	return nil
}

// phaseTokenFileMutex ensures thread-safe access to phase token usage under costs/phase
var phaseTokenFileMutex sync.Mutex

// IsPhaseOnlyAgent checks if a phase is a phase-only agent (not step-based)
// Phase-only agents run independently and don't have step numbers
// This is exported so context_aware_bridge can use it
func IsPhaseOnlyAgent(phase string) bool {
	phaseOnlyPhases := []string{
		"planning",
	}
	for _, p := range phaseOnlyPhases {
		if phase == p {
			return true
		}
	}
	return false
}

// PersistPhaseTokenUsage saves token usage directly to costs/phase/token_usage.json
// It reads existing token data from the file, merges the new token data, and writes back.
// The file is the single source of truth - no in-memory accumulation.
// This is used for phase-only agents (planning, plan-improvement, etc.) that don't have iteration folders.
func (bo *BaseOrchestrator) PersistPhaseTokenUsage(ctx context.Context,
	phaseTokenData *PhaseTokenData, modelTokenData *ModelTokenData) error {
	if phaseTokenData == nil || phaseTokenData.Phase == "" {
		return nil
	}

	// Acquire mutex to prevent race conditions when multiple phase agents complete concurrently
	phaseTokenFileMutex.Lock()
	defer phaseTokenFileMutex.Unlock()

	store := newBaseOrchestratorTokenUsageStore(bo)
	store.ensurePhaseMigrated(ctx)

	// Build file path under the dedicated phase costs folder.
	workspacePath := bo.GetWorkspacePath()
	filePath := ResolvePhaseTokenUsagePath(workspacePath)

	bo.GetLogger().Debug(fmt.Sprintf("💾 Persisting phase token usage to: %s", filePath))

	tokenFile := store.readPhase(ctx)
	now := time.Now()
	ApplyModelTokenDataToPhaseTokenUsageFile(tokenFile, phaseTokenData.Phase, modelTokenData, now)

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(tokenFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal phase token usage data: %w", err)
	}

	// Write to file
	if err := bo.WriteWorkspaceFile(ctx, filePath, string(jsonData)); err != nil {
		return fmt.Errorf("failed to write phase token usage file: %w", err)
	}

	dailyFile := store.readPhaseDaily(ctx, now)
	if dailyFile.TokenUsage == nil {
		dailyFile.TokenUsage = &PhaseTokenUsageFile{}
	}
	dailyFile.Date = CostDateKey(now)
	dailyFile.UpdatedAt = now
	ApplyModelTokenDataToPhaseTokenUsageFile(dailyFile.TokenUsage, phaseTokenData.Phase, modelTokenData, now)

	dailyJSON, err := json.MarshalIndent(dailyFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal daily phase token usage data: %w", err)
	}
	if err := bo.WriteWorkspaceFile(ctx, ResolveDailyPhaseTokenUsagePath(workspacePath, now), string(dailyJSON)); err != nil {
		return fmt.Errorf("failed to write daily phase token usage file: %w", err)
	}

	bo.GetLogger().Debug("✅ Persisted phase token usage to file")

	return nil
}

// GetPhaseTokenUsage reads token usage from file for a specific phase (aggregated across all models)
func (bo *BaseOrchestrator) GetPhaseTokenUsage(phase string) *StepTokenUsage {
	ctx := context.Background()
	store := newBaseOrchestratorTokenUsageStore(bo)
	tokenFile := store.readPhase(ctx)
	if len(tokenFile.ByPhaseAndModel) == 0 && len(tokenFile.ByModel) == 0 {
		return &StepTokenUsage{}
	}

	modelMap, exists := tokenFile.ByPhaseAndModel[phase]
	if !exists || modelMap == nil {
		return &StepTokenUsage{}
	}

	// Aggregate across all models for this phase
	result := &StepTokenUsage{}
	var maxContextUsagePercent float64
	for _, modelUsage := range modelMap {
		result.InputTokens += modelUsage.InputTokens
		result.OutputTokens += modelUsage.OutputTokens
		result.CacheTokens += modelUsage.CacheTokens
		result.ReasoningTokens += modelUsage.ReasoningTokens
		result.LLMCallCount += modelUsage.LLMCallCount
		// Aggregate pricing
		result.InputCost += modelUsage.InputCost
		result.OutputCost += modelUsage.OutputCost
		result.ReasoningCost += modelUsage.ReasoningCost
		result.CacheCost += modelUsage.CacheCost
		result.TotalCost += modelUsage.TotalCost
		// Track max context usage percentage
		if modelUsage.ContextUsagePercent > maxContextUsagePercent {
			maxContextUsagePercent = modelUsage.ContextUsagePercent
		}
	}
	result.ContextUsagePercent = maxContextUsagePercent

	return result
}

// GetPhaseModelTokenUsage reads token usage from file for a specific phase and model
func (bo *BaseOrchestrator) GetPhaseModelTokenUsage(phase string, modelID string) *ModelTokenUsage {
	ctx := context.Background()
	store := newBaseOrchestratorTokenUsageStore(bo)
	tokenFile := store.readPhase(ctx)
	if len(tokenFile.ByPhaseAndModel) == 0 && len(tokenFile.ByModel) == 0 {
		return &ModelTokenUsage{}
	}

	if tokenFile.ByPhaseAndModel == nil {
		return &ModelTokenUsage{}
	}

	modelMap, exists := tokenFile.ByPhaseAndModel[phase]
	if !exists || modelMap == nil {
		return &ModelTokenUsage{}
	}

	usage, exists := modelMap[modelID]
	if !exists {
		return &ModelTokenUsage{}
	}

	return usage
}

// GetAllPhaseTokenUsage reads all phase token usage from file
func (bo *BaseOrchestrator) GetAllPhaseTokenUsage() map[string]map[string]*ModelTokenUsage {
	ctx := context.Background()
	store := newBaseOrchestratorTokenUsageStore(bo)
	tokenFile := store.readPhase(ctx)

	if tokenFile.ByPhaseAndModel == nil {
		return make(map[string]map[string]*ModelTokenUsage)
	}

	// Return a copy to avoid external modifications
	result := make(map[string]map[string]*ModelTokenUsage)
	for phase, modelMap := range tokenFile.ByPhaseAndModel {
		result[phase] = make(map[string]*ModelTokenUsage)
		for modelID, usage := range modelMap {
			result[phase][modelID] = &ModelTokenUsage{
				Provider:            usage.Provider,
				InputTokens:         usage.InputTokens,
				OutputTokens:        usage.OutputTokens,
				InputTokensM:        usage.InputTokensM,
				OutputTokensM:       usage.OutputTokensM,
				CacheTokens:         usage.CacheTokens,
				CacheTokensM:        usage.CacheTokensM,
				ReasoningTokens:     usage.ReasoningTokens,
				ReasoningTokensM:    usage.ReasoningTokensM,
				LLMCallCount:        usage.LLMCallCount,
				InputCost:           usage.InputCost,
				OutputCost:          usage.OutputCost,
				ReasoningCost:       usage.ReasoningCost,
				CacheCost:           usage.CacheCost,
				TotalCost:           usage.TotalCost,
				ContextWindowUsage:  usage.ContextWindowUsage,
				ModelContextWindow:  usage.ModelContextWindow,
				ContextUsagePercent: usage.ContextUsagePercent,
			}
		}
	}

	return result
}
