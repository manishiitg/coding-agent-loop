package orchestrator

import (
	"context"
	"fmt"
	mcpagent "mcpagent/agent"
	"mcpagent/events"
	loggerv2 "mcpagent/logger/v2"
	"sync"
)

// StepTokenAccumulator defines the interface for accumulating step token usage
type StepTokenAccumulator interface {
	AccumulateStepTokens(phase string, step int, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens int, llmCallCount int, cacheDiscount float64)
}

// ModelTokenAccumulator defines the interface for accumulating model token usage
type ModelTokenAccumulator interface {
	AccumulateModelTokens(modelID, provider string, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens int, llmCallCount int)
}

// TokenPersister defines the interface for persisting token usage to file
type TokenPersister interface {
	PersistTokenUsage(ctx context.Context, iterationFolder string) error
}

// ContextAwareEventBridge wraps an existing AgentEventListener and adds orchestrator context
type ContextAwareEventBridge struct {
	underlyingBridge      mcpagent.AgentEventListener
	tokenAccumulator      StepTokenAccumulator  // Interface for step token tracking
	modelTokenAccumulator ModelTokenAccumulator // Interface for model token tracking
	tokenPersister        TokenPersister        // Interface for persisting token usage
	iterationFolder       string                // Current iteration folder for persistence
	currentPhase          string
	currentStep           int
	currentAgentName      string
	mu                    sync.RWMutex
	logger                loggerv2.Logger
}

// Name implements the EventBridge interface
func (c *ContextAwareEventBridge) Name() string {
	return "context_aware_bridge"
}

// NewContextAwareEventBridge creates a new context-aware event bridge
func NewContextAwareEventBridge(underlyingBridge mcpagent.AgentEventListener, logger loggerv2.Logger) *ContextAwareEventBridge {
	return &ContextAwareEventBridge{
		underlyingBridge: underlyingBridge,
		logger:           logger,
	}
}

// SetTokenAccumulator sets the token accumulator for step token tracking
func (c *ContextAwareEventBridge) SetTokenAccumulator(accumulator StepTokenAccumulator) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokenAccumulator = accumulator
	// If accumulator also implements ModelTokenAccumulator, set it
	if modelAccumulator, ok := accumulator.(ModelTokenAccumulator); ok {
		c.modelTokenAccumulator = modelAccumulator
	}
	// If accumulator also implements TokenPersister, set it
	if persister, ok := accumulator.(TokenPersister); ok {
		c.tokenPersister = persister
	}
}

// SetIterationFolder sets the current iteration folder for token persistence
func (c *ContextAwareEventBridge) SetIterationFolder(iterationFolder string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.iterationFolder = iterationFolder
	c.logger.Debug(fmt.Sprintf("📁 Set iteration folder for token persistence: %s", iterationFolder))
}

// SetOrchestratorContext sets the current orchestrator context
func (c *ContextAwareEventBridge) SetOrchestratorContext(phase string, step int, agentName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentPhase = phase
	c.currentStep = step
	c.currentAgentName = agentName

	c.logger.Info(fmt.Sprintf("🎯 Set orchestrator context: %s (step %d)", phase, step+1))
}

// ClearOrchestratorContext clears the orchestrator context
func (c *ContextAwareEventBridge) ClearOrchestratorContext() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentPhase = ""
	c.currentStep = 0
	c.currentAgentName = ""

	c.logger.Info("🧹 Cleared orchestrator context")
}

// HandleEvent implements AgentEventListener interface
func (c *ContextAwareEventBridge) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	c.logger.Info(fmt.Sprintf("🔍 ContextAwareBridge: Received event %s (type: %s)", event.Type, event.Type))

	// Copy orchestrator context while holding read lock
	c.mu.RLock()
	currentPhase := c.currentPhase
	currentStep := c.currentStep
	currentAgentName := c.currentAgentName
	c.mu.RUnlock()

	// Early return if no current phase
	if currentPhase == "" {
		c.logger.Debug("🔍 DEBUG: Skipping metadata addition - no currentPhase set")
	} else {
		c.logger.Debug(fmt.Sprintf("🔍 ContextAwareBridge: Processing event %s with phase %s", event.Type, currentPhase))

		// Add orchestrator context to metadata
		// We need to check if the event data has a BaseEventData field
		c.logger.Debug(fmt.Sprintf("🔍 DEBUG: About to check type assertion for event.Data of type %T", event.Data))

		if eventData, ok := event.Data.(interface {
			GetBaseEventData() *events.BaseEventData
		}); ok {
			c.logger.Debug(fmt.Sprintf("🔍 DEBUG: Type assertion succeeded for %T", eventData))
			baseData := eventData.GetBaseEventData()

			// Nil check before accessing Metadata
			if baseData == nil {
				c.logger.Warn(fmt.Sprintf("⚠️ ContextAwareBridge: GetBaseEventData returned nil for event %s", event.Type))
			} else {
				c.logger.Debug(fmt.Sprintf("🔍 DEBUG: Got BaseEventData, metadata present: %t", baseData.Metadata != nil))

				if baseData.Metadata == nil {
					baseData.Metadata = make(map[string]any)
					c.logger.Debug("🔍 DEBUG: Created new metadata map")
				}
				baseData.Metadata["orchestrator_phase"] = currentPhase
				baseData.Metadata["orchestrator_step"] = currentStep
				baseData.Metadata["orchestrator_agent_name"] = currentAgentName

				c.logger.Debug(fmt.Sprintf("✅ ContextAwareBridge: Added metadata to event %s, metadata keys count: %d", event.Type, len(baseData.Metadata)))
			}
		} else {
			c.logger.Warn(fmt.Sprintf("⚠️ ContextAwareBridge: Event data %T does not have GetBaseEventData method", event.Data))
		}
	}

	// Intercept token_usage events and accumulate per step and model
	// Note: Model accumulation works even without phase context
	if event.Type == events.TokenUsage {
		if tokenEvent, ok := event.Data.(*events.TokenUsageEvent); ok {
			c.mu.RLock()
			accumulator := c.tokenAccumulator
			c.mu.RUnlock()

			// Extract cache tokens once (used for both step and model accumulation)
			cacheTokens := extractCacheTokens(tokenEvent)

			// Extract cache discount from token event
			cacheDiscount := 0.0
			if tokenEvent.CacheDiscount > 0 {
				cacheDiscount = tokenEvent.CacheDiscount
			}

			// Accumulate tokens for step (only if we have phase context)
			if accumulator != nil && currentPhase != "" {
				// Accumulate tokens for this step
				accumulator.AccumulateStepTokens(
					currentPhase,
					currentStep,
					tokenEvent.PromptTokens,
					tokenEvent.CompletionTokens,
					tokenEvent.TotalTokens,
					cacheTokens,
					tokenEvent.ReasoningTokens,
					1, // Each token_usage event represents one LLM call
					cacheDiscount,
				)
				c.logger.Debug(fmt.Sprintf("📊 Accumulated tokens for step %s:%d - Total: %d", currentPhase, currentStep, tokenEvent.TotalTokens))
			}

			// Also accumulate tokens per model (works even without phase context)
			c.mu.RLock()
			modelAccumulator := c.modelTokenAccumulator
			c.mu.RUnlock()

			if modelAccumulator != nil && tokenEvent.ModelID != "" {
				// Accumulate tokens for this model
				modelAccumulator.AccumulateModelTokens(
					tokenEvent.ModelID,
					tokenEvent.Provider,
					tokenEvent.PromptTokens,
					tokenEvent.CompletionTokens,
					tokenEvent.TotalTokens,
					cacheTokens,
					tokenEvent.ReasoningTokens,
					1, // Each token_usage event represents one LLM call
				)
				c.logger.Debug(fmt.Sprintf("📊 Accumulated tokens for model %s (%s) - Total: %d", tokenEvent.ModelID, tokenEvent.Provider, tokenEvent.TotalTokens))
			}

			// Persist token usage immediately after accumulation (real-time persistence)
			c.mu.RLock()
			persister := c.tokenPersister
			iterationFolder := c.iterationFolder
			c.mu.RUnlock()

			if persister != nil && iterationFolder != "" {
				// Persist asynchronously to avoid blocking event processing
				go func() {
					if err := persister.PersistTokenUsage(ctx, iterationFolder); err != nil {
						c.logger.Warn(fmt.Sprintf("⚠️ Failed to persist token usage immediately: %v", err))
					} else {
						c.logger.Debug("💾 Persisted token usage immediately after event")
					}
				}()
			}
		}
	}

	// Forward to underlying bridge
	c.logger.Info(fmt.Sprintf("🔍 ContextAwareBridge: Forwarding event %s to underlying bridge", event.Type))
	if c.underlyingBridge == nil {
		c.logger.Error(fmt.Sprintf("❌ ContextAwareBridge: Underlying bridge is nil, cannot forward event %s", event.Type), nil)
		return fmt.Errorf("underlying bridge is nil")
	}
	err := c.underlyingBridge.HandleEvent(ctx, event)
	if err != nil {
		c.logger.Warn(fmt.Sprintf("⚠️ ContextAwareBridge: Error forwarding event %s: %w", event.Type, err))
	} else {
		c.logger.Info(fmt.Sprintf("✅ ContextAwareBridge: Successfully forwarded event %s", event.Type))
	}
	return err
}
