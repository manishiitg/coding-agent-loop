package orchestrator

import (
	"context"
	"fmt"
	mcpagent "mcpagent/agent"
	"mcpagent/events"
	loggerv2 "mcpagent/logger/v2"
	"sync"
)

// TokenPersister defines the interface for persisting token usage to file
type TokenPersister interface {
	PersistTokenUsage(ctx context.Context, iterationFolder string, stepTokenData *StepTokenData, modelTokenData *ModelTokenData) error
}

// PhaseTokenPersister defines the interface for persisting phase token usage to file
type PhaseTokenPersister interface {
	PersistPhaseTokenUsage(ctx context.Context, phaseTokenData *PhaseTokenData, modelTokenData *ModelTokenData) error
}

// ContextAwareEventBridge wraps an existing AgentEventListener and adds orchestrator context
type ContextAwareEventBridge struct {
	underlyingBridge mcpagent.AgentEventListener
	tokenPersister   TokenPersister // Interface for persisting token usage
	iterationFolder  string         // Current iteration folder for persistence
	currentPhase     string
	currentStep      int    // Step index (deprecated, kept for backward compat)
	currentStepID    string // Step ID (e.g., "fetch-data", "process-results")
	currentAgentName string
	mu               sync.RWMutex
	logger           loggerv2.Logger
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

// SetTokenPersister sets the token persister (no longer using accumulators)
func (c *ContextAwareEventBridge) SetTokenPersister(persister TokenPersister) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokenPersister = persister
}

// SetIterationFolder sets the current iteration folder for token persistence
func (c *ContextAwareEventBridge) SetIterationFolder(iterationFolder string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.iterationFolder = iterationFolder
	c.logger.Debug(fmt.Sprintf("📁 Set iteration folder for token persistence: %s", iterationFolder))
}

// SetOrchestratorContext sets the current orchestrator context
func (c *ContextAwareEventBridge) SetOrchestratorContext(phase string, step int, stepID string, agentName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentPhase = phase
	c.currentStep = step
	c.currentStepID = stepID
	c.currentAgentName = agentName

	c.logger.Info(fmt.Sprintf("🎯 Set orchestrator context: %s (step %d, ID: %s)", phase, step+1, stepID))
}

// ClearOrchestratorContext clears the orchestrator context
func (c *ContextAwareEventBridge) ClearOrchestratorContext() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentPhase = ""
	c.currentStep = 0
	c.currentStepID = ""
	c.currentAgentName = ""

	c.logger.Info("🧹 Cleared orchestrator context")
}

// HandleEvent implements AgentEventListener interface
func (c *ContextAwareEventBridge) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	// Copy orchestrator context while holding read lock
	c.mu.RLock()
	currentPhase := c.currentPhase
	currentStep := c.currentStep
	currentStepID := c.currentStepID
	currentAgentName := c.currentAgentName
	c.mu.RUnlock()

	// Early return if no current phase
	if currentPhase != "" {
		c.logger.Debug(fmt.Sprintf("🔍 ContextAwareBridge: Processing event %s with phase %s", event.Type, currentPhase))

		// Add orchestrator context to metadata
		// We need to check if the event data has a BaseEventData field
		if eventData, ok := event.Data.(interface {
			GetBaseEventData() *events.BaseEventData
		}); ok {
			baseData := eventData.GetBaseEventData()

			// Nil check before accessing Metadata
			if baseData == nil {
				c.logger.Warn(fmt.Sprintf("⚠️ ContextAwareBridge: GetBaseEventData returned nil for event %s", event.Type))
			} else {
				if baseData.Metadata == nil {
					baseData.Metadata = make(map[string]any)
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

	// Intercept token_usage events and persist directly to file (no in-memory accumulation)
	if event.Type == events.TokenUsage {
		if tokenEvent, ok := event.Data.(*events.TokenUsageEvent); ok {
			// Extract cache tokens (separated into read and write for accurate pricing)
			// Cache reads are discounted, cache writes are premium (1.25x base rate)
			cacheTokensSeparate := extractCacheTokensSeparate(tokenEvent)

			// Extract LLM call count from event (cumulative for conversation end, 1 for single calls)
			llmCallCount := extractLLMCallCount(tokenEvent)

			// Prepare model token data
			var modelTokenData *ModelTokenData
			if tokenEvent.ModelID != "" {
				modelTokenData = &ModelTokenData{
					ModelID:          tokenEvent.ModelID,
					Provider:         tokenEvent.Provider,
					InputTokens:      tokenEvent.PromptTokens,     // input tokens
					OutputTokens:     tokenEvent.CompletionTokens, // output tokens
					CacheTokens:      cacheTokensSeparate.Total,   // total cache (backward compat)
					CacheReadTokens:  cacheTokensSeparate.ReadTokens,  // cache reads (discounted)
					CacheWriteTokens: cacheTokensSeparate.WriteTokens, // cache writes (premium 1.25x)
					ReasoningTokens:  tokenEvent.ReasoningTokens,
					LLMCallCount:     llmCallCount, // Extract actual call count from event
				}
			}

			// Check if this is a phase-only agent (step == 0 and phase is a phase-only agent)
			isPhaseOnly := currentPhase != "" && currentStep == 0 && IsPhaseOnlyAgent(currentPhase)

			if isPhaseOnly {
				// Phase-only agent: persist to main workspace folder (phase-level tracking)
				var phaseTokenData *PhaseTokenData
				if currentPhase != "" {
					phaseTokenData = &PhaseTokenData{
						Phase:            currentPhase,
						InputTokens:      tokenEvent.PromptTokens,     // input tokens
						OutputTokens:     tokenEvent.CompletionTokens, // output tokens
						CacheTokens:      cacheTokensSeparate.Total,   // total cache (backward compat)
						CacheReadTokens:  cacheTokensSeparate.ReadTokens,  // cache reads (discounted)
						CacheWriteTokens: cacheTokensSeparate.WriteTokens, // cache writes (premium 1.25x)
						ReasoningTokens:  tokenEvent.ReasoningTokens,
						LLMCallCount:     llmCallCount, // Extract actual call count from event
					}
				}

				// Persist phase token usage directly to file (real-time persistence, no accumulation)
				c.mu.RLock()
				persister := c.tokenPersister
				c.mu.RUnlock()

				if persister != nil {
					// Check if persister implements PhaseTokenPersister interface
					if phasePersister, ok := persister.(PhaseTokenPersister); ok {
						// Persist asynchronously to avoid blocking event processing
						go func() {
							if err := phasePersister.PersistPhaseTokenUsage(ctx, phaseTokenData, modelTokenData); err != nil {
								c.logger.Warn(fmt.Sprintf("⚠️ Failed to persist phase token usage: %v", err))
							} else {
								c.logger.Debug(fmt.Sprintf("💾 Persisted phase token usage directly to file"))
							}
						}()
					} else {
						c.logger.Debug(fmt.Sprintf("⚠️ Token persister does not implement PhaseTokenPersister, skipping phase token persistence"))
					}
				}
			} else {
				// Step-based agent: persist to iteration folder (existing behavior)
				var stepTokenData *StepTokenData
				if currentPhase != "" {
					stepTokenData = &StepTokenData{
						Phase:            currentPhase,
						Step:             currentStep,
						StepID:           currentStepID, // Use step ID instead of index
						InputTokens:      tokenEvent.PromptTokens,     // input tokens
						OutputTokens:     tokenEvent.CompletionTokens, // output tokens
						CacheTokens:      cacheTokensSeparate.Total,   // total cache (backward compat)
						CacheReadTokens:  cacheTokensSeparate.ReadTokens,  // cache reads (discounted)
						CacheWriteTokens: cacheTokensSeparate.WriteTokens, // cache writes (premium 1.25x)
						ReasoningTokens:  tokenEvent.ReasoningTokens,
						LLMCallCount:     llmCallCount, // Extract actual call count from event
					}
				}

				// Persist token usage directly to file (real-time persistence, no accumulation)
				c.mu.RLock()
				persister := c.tokenPersister
				iterationFolder := c.iterationFolder
				c.mu.RUnlock()

				if persister != nil && iterationFolder != "" {
					// Persist asynchronously to avoid blocking event processing
					go func() {
						if err := persister.PersistTokenUsage(ctx, iterationFolder, stepTokenData, modelTokenData); err != nil {
							c.logger.Warn(fmt.Sprintf("⚠️ Failed to persist token usage: %v", err))
						} else {
							c.logger.Debug(fmt.Sprintf("💾 Persisted token usage directly to file"))
						}
					}()
				}
			}
		}
	}

	if c.underlyingBridge == nil {
		c.logger.Error(fmt.Sprintf("❌ ContextAwareBridge: Underlying bridge is nil, cannot forward event %s", event.Type), nil)
		return fmt.Errorf("underlying bridge is nil")
	}
	err := c.underlyingBridge.HandleEvent(ctx, event)
	if err != nil {
		c.logger.Warn(fmt.Sprintf("⚠️ ContextAwareBridge: Error forwarding event %s: %v", event.Type, err))
	}
	return err
}
