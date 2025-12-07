package orchestrator

import (
	"context"
	"fmt"
	"mcp-agent/agent_go/internal/utils"
	mcpagent "mcpagent/agent"
	"mcpagent/events"
	"sync"
)

// TokenPersister defines the interface for persisting token usage to file
type TokenPersister interface {
	PersistTokenUsage(ctx context.Context, iterationFolder string, stepTokenData *StepTokenData, modelTokenData *ModelTokenData) error
}

// ContextAwareEventBridge wraps an existing AgentEventListener and adds orchestrator context
type ContextAwareEventBridge struct {
	underlyingBridge mcpagent.AgentEventListener
	tokenPersister   TokenPersister // Interface for persisting token usage
	iterationFolder  string         // Current iteration folder for persistence
	currentPhase     string
	currentStep      int
	currentAgentName string
	mu               sync.RWMutex
	logger           utils.ExtendedLogger
}

// Name implements the EventBridge interface
func (c *ContextAwareEventBridge) Name() string {
	return "context_aware_bridge"
}

// NewContextAwareEventBridge creates a new context-aware event bridge
func NewContextAwareEventBridge(underlyingBridge mcpagent.AgentEventListener, logger utils.ExtendedLogger) *ContextAwareEventBridge {
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
	c.logger.Debugf("📁 Set iteration folder for token persistence: %s", iterationFolder)
}

// SetOrchestratorContext sets the current orchestrator context
func (c *ContextAwareEventBridge) SetOrchestratorContext(phase string, step int, agentName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentPhase = phase
	c.currentStep = step
	c.currentAgentName = agentName

	c.logger.Infof("🎯 Set orchestrator context: %s (step %d)", phase, step+1)
}

// ClearOrchestratorContext clears the orchestrator context
func (c *ContextAwareEventBridge) ClearOrchestratorContext() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentPhase = ""
	c.currentStep = 0
	c.currentAgentName = ""

	c.logger.Infof("🧹 Cleared orchestrator context")
}

// HandleEvent implements AgentEventListener interface
func (c *ContextAwareEventBridge) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	c.logger.Infof("🔍 ContextAwareBridge: Received event %s (type: %s)", event.Type, event.Type)

	// Copy orchestrator context while holding read lock
	c.mu.RLock()
	currentPhase := c.currentPhase
	currentStep := c.currentStep
	currentAgentName := c.currentAgentName
	c.mu.RUnlock()

	// Early return if no current phase
	if currentPhase == "" {
		c.logger.Debugf("🔍 DEBUG: Skipping metadata addition - no currentPhase set")
	} else {
		c.logger.Debugf("🔍 ContextAwareBridge: Processing event %s with phase %s", event.Type, currentPhase)

		// Add orchestrator context to metadata
		// We need to check if the event data has a BaseEventData field
		c.logger.Debugf("🔍 DEBUG: About to check type assertion for event.Data of type %T", event.Data)

		if eventData, ok := event.Data.(interface {
			GetBaseEventData() *events.BaseEventData
		}); ok {
			c.logger.Debugf("🔍 DEBUG: Type assertion succeeded for %T", eventData)
			baseData := eventData.GetBaseEventData()

			// Nil check before accessing Metadata
			if baseData == nil {
				c.logger.Warnf("⚠️ ContextAwareBridge: GetBaseEventData returned nil for event %s", event.Type)
			} else {
				c.logger.Debugf("🔍 DEBUG: Got BaseEventData, metadata present: %t", baseData.Metadata != nil)

				if baseData.Metadata == nil {
					baseData.Metadata = make(map[string]any)
					c.logger.Debugf("🔍 DEBUG: Created new metadata map")
				}
				baseData.Metadata["orchestrator_phase"] = currentPhase
				baseData.Metadata["orchestrator_step"] = currentStep
				baseData.Metadata["orchestrator_agent_name"] = currentAgentName

				c.logger.Debugf("✅ ContextAwareBridge: Added metadata to event %s, metadata keys count: %d", event.Type, len(baseData.Metadata))
			}
		} else {
			c.logger.Warnf("⚠️ ContextAwareBridge: Event data %T does not have GetBaseEventData method", event.Data)
		}
	}

	// Intercept token_usage events and persist directly to file (no in-memory accumulation)
	if event.Type == events.TokenUsage {
		if tokenEvent, ok := event.Data.(*events.TokenUsageEvent); ok {
			// Extract cache tokens
			cacheTokens := extractCacheTokens(tokenEvent)

			// Prepare token data for persistence
			var stepTokenData *StepTokenData
			var modelTokenData *ModelTokenData

			// Prepare step token data if we have phase context
			if currentPhase != "" {
				stepTokenData = &StepTokenData{
					Phase:           currentPhase,
					Step:            currentStep,
					InputTokens:     tokenEvent.PromptTokens,     // input tokens
					OutputTokens:    tokenEvent.CompletionTokens, // output tokens
					CacheTokens:     cacheTokens,
					ReasoningTokens: tokenEvent.ReasoningTokens,
					LLMCallCount:    1, // Each token_usage event represents one LLM call
				}
			}

			// Prepare model token data
			if tokenEvent.ModelID != "" {
				modelTokenData = &ModelTokenData{
					ModelID:         tokenEvent.ModelID,
					Provider:        tokenEvent.Provider,
					InputTokens:     tokenEvent.PromptTokens,     // input tokens
					OutputTokens:    tokenEvent.CompletionTokens, // output tokens
					CacheTokens:     cacheTokens,
					ReasoningTokens: tokenEvent.ReasoningTokens,
					LLMCallCount:    1, // Each token_usage event represents one LLM call
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
						c.logger.Warnf("⚠️ Failed to persist token usage: %v", err)
					} else {
						c.logger.Debugf("💾 Persisted token usage directly to file")
					}
				}()
			}
		}
	}

	// Forward to underlying bridge
	c.logger.Infof("🔍 ContextAwareBridge: Forwarding event %s to underlying bridge", event.Type)
	if c.underlyingBridge == nil {
		c.logger.Errorf("❌ ContextAwareBridge: Underlying bridge is nil, cannot forward event %s", event.Type)
		return fmt.Errorf("underlying bridge is nil")
	}
	err := c.underlyingBridge.HandleEvent(ctx, event)
	if err != nil {
		c.logger.Warnf("⚠️ ContextAwareBridge: Error forwarding event %s: %w", event.Type, err)
	} else {
		c.logger.Infof("✅ ContextAwareBridge: Successfully forwarded event %s", event.Type)
	}
	return err
}
