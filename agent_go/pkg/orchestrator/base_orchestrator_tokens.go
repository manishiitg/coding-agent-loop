package orchestrator

import (
	"context"
	"fmt"

	"mcpagent/events"
)

// AccumulateStepTokens accumulates token usage for a specific step
func (bo *BaseOrchestrator) AccumulateStepTokens(phase string, step int, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens int, llmCallCount int, cacheDiscount float64) {
	bo.stepTokenMutex.Lock()
	defer bo.stepTokenMutex.Unlock()

	key := fmt.Sprintf("%s:%d", phase, step)
	usage, exists := bo.stepTokenAccumulator[key]
	if !exists {
		usage = &StepTokenUsage{}
		bo.stepTokenAccumulator[key] = usage
	}

	usage.PromptTokens += promptTokens
	usage.CompletionTokens += completionTokens
	usage.TotalTokens += totalTokens
	usage.CacheTokens += cacheTokens
	usage.ReasoningTokens += reasoningTokens
	usage.LLMCallCount += llmCallCount
	if cacheTokens > 0 {
		usage.CacheEnabledCallCount++
	}
	usage.CacheDiscountSum += cacheDiscount
}

// GetStepTokenUsage retrieves accumulated token usage for a specific step
func (bo *BaseOrchestrator) GetStepTokenUsage(phase string, step int) *StepTokenUsage {
	bo.stepTokenMutex.RLock()
	defer bo.stepTokenMutex.RUnlock()

	key := fmt.Sprintf("%s:%d", phase, step)
	usage, exists := bo.stepTokenAccumulator[key]
	if !exists {
		return &StepTokenUsage{} // Return zero values if step not found
	}

	// Return a copy to avoid race conditions
	return &StepTokenUsage{
		PromptTokens:          usage.PromptTokens,
		CompletionTokens:      usage.CompletionTokens,
		TotalTokens:           usage.TotalTokens,
		CacheTokens:           usage.CacheTokens,
		ReasoningTokens:       usage.ReasoningTokens,
		LLMCallCount:          usage.LLMCallCount,
		CacheEnabledCallCount: usage.CacheEnabledCallCount,
		CacheDiscountSum:      usage.CacheDiscountSum,
	}
}

// EmitStepTokenUsage emits a step token usage summary event and optionally clears the accumulated data
func (bo *BaseOrchestrator) EmitStepTokenUsage(ctx context.Context, phase string, step int, stepTitle string, clearAfterEmit bool) {
	bo.stepTokenMutex.Lock()
	defer bo.stepTokenMutex.Unlock()

	key := fmt.Sprintf("%s:%d", phase, step)
	usage, exists := bo.stepTokenAccumulator[key]
	if !exists {
		bo.GetLogger().Warnf("⚠️ No token usage data found for step %s:%d", phase, step)
		return
	}

	// Create and emit step token usage event
	stepTokenEvent := events.NewStepTokenUsageEvent(
		phase,
		step,
		stepTitle,
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
		usage.CacheTokens,
		usage.ReasoningTokens,
		usage.LLMCallCount,
		usage.CacheEnabledCallCount,
	)

	bo.emitEvent(ctx, events.StepTokenUsage, stepTokenEvent)

	bo.GetLogger().Infof("📊 Emitted step token usage for %s:%d - Total: %d tokens (Prompt: %d, Completion: %d, Cache: %d, Reasoning: %d, Calls: %d)",
		phase, step, usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens, usage.CacheTokens, usage.ReasoningTokens, usage.LLMCallCount)

	// Clear accumulated data if requested
	if clearAfterEmit {
		delete(bo.stepTokenAccumulator, key)
	}
}
