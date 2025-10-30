package orchestrator

import (
	"context"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"sync"
)

// ContextAwareEventBridge wraps an existing AgentEventListener and adds orchestrator context
type ContextAwareEventBridge struct {
	underlyingBridge mcpagent.AgentEventListener
	currentPhase     string
	currentStep      int
	currentIteration int
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

// SetOrchestratorContext sets the current orchestrator context
func (c *ContextAwareEventBridge) SetOrchestratorContext(phase string, step, iteration int, agentName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentPhase = phase
	c.currentStep = step
	c.currentIteration = iteration
	c.currentAgentName = agentName

	c.logger.Infof("🎯 Set orchestrator context: %s (step %d, iteration %d)", phase, step+1, iteration+1)
}

// ClearOrchestratorContext clears the orchestrator context
func (c *ContextAwareEventBridge) ClearOrchestratorContext() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentPhase = ""
	c.currentStep = 0
	c.currentIteration = 0
	c.currentAgentName = ""

	c.logger.Infof("🧹 Cleared orchestrator context")
}

// HandleEvent implements AgentEventListener interface
func (c *ContextAwareEventBridge) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	c.logger.Debugf("🔍 ContextAwareBridge: Received event %s", event.Type)

	// Copy orchestrator context while holding read lock
	c.mu.RLock()
	currentPhase := c.currentPhase
	currentStep := c.currentStep
	currentIteration := c.currentIteration
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
				baseData.Metadata["orchestrator_iteration"] = currentIteration
				baseData.Metadata["orchestrator_agent_name"] = currentAgentName

				c.logger.Debugf("✅ ContextAwareBridge: Added metadata to event %s, metadata keys count: %d", event.Type, len(baseData.Metadata))
			}
		} else {
			c.logger.Warnf("⚠️ ContextAwareBridge: Event data %T does not have GetBaseEventData method", event.Data)
		}
	}

	// Forward to underlying bridge
	c.logger.Debugf("🔍 ContextAwareBridge: Forwarding event %s to underlying bridge", event.Type)
	err := c.underlyingBridge.HandleEvent(ctx, event)
	if err != nil {
		c.logger.Warnf("⚠️ ContextAwareBridge: Error forwarding event %s: %v", event.Type, err)
	} else {
		c.logger.Debugf("✅ ContextAwareBridge: Successfully forwarded event %s", event.Type)
	}
	return err
}
