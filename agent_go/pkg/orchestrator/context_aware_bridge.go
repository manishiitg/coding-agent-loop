package orchestrator

import (
	"context"
	"fmt"
	"strings"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	orchevents "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
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

// AgentSessionIDKey is re-exported from orchestrator/events for convenience.
// Use events.AgentSessionIDKey to inject agent session ID into context.
// When set, the ContextAwareEventBridge tags events with this correlation ID,
// enabling the frontend to group tool calls under their parent orchestrator_agent_start.
var AgentSessionIDKey = orchevents.AgentSessionIDKey

// orchestratorContext holds a snapshot of orchestrator context for stack operations
type orchestratorContext struct {
	phase     string
	step      int
	stepID    string
	agentName string
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
	// Batch execution context (for batch progress tracking in frontend)
	currentGroupID  string // Current group ID being executed
	currentGroupIdx int    // 0-based index of current group
	totalGroups     int    // Total number of groups in batch
	// Context stack for nested agent execution (e.g., orchestrator -> sub-agent)
	contextStack []orchestratorContext
	mu           sync.RWMutex
	logger       loggerv2.Logger
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

// PushContext saves the current context to the stack and sets a new context
// Use this before executing a sub-agent to preserve the parent context
func (c *ContextAwareEventBridge) PushContext(phase string, step int, stepID string, agentName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Save current context to stack
	c.contextStack = append(c.contextStack, orchestratorContext{
		phase:     c.currentPhase,
		step:      c.currentStep,
		stepID:    c.currentStepID,
		agentName: c.currentAgentName,
	})

	// Set new context
	c.currentPhase = phase
	c.currentStep = step
	c.currentStepID = stepID
	c.currentAgentName = agentName

	c.logger.Info(fmt.Sprintf("📥 Pushed context (stack depth: %d): %s (step %d, ID: %s)", len(c.contextStack), phase, step+1, stepID))
}

// PopContext restores the previous context from the stack
// Use this after a sub-agent completes to restore the parent context
func (c *ContextAwareEventBridge) PopContext() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.contextStack) == 0 {
		c.logger.Warn("⚠️ PopContext called but context stack is empty - no context to restore")
		return
	}

	// Pop the last context from stack
	lastIdx := len(c.contextStack) - 1
	prevContext := c.contextStack[lastIdx]
	c.contextStack = c.contextStack[:lastIdx]

	// Restore previous context
	c.currentPhase = prevContext.phase
	c.currentStep = prevContext.step
	c.currentStepID = prevContext.stepID
	c.currentAgentName = prevContext.agentName

	c.logger.Info(fmt.Sprintf("📤 Popped context (stack depth: %d): restored to %s (step %d, ID: %s)", len(c.contextStack), c.currentPhase, c.currentStep+1, c.currentStepID))
}

// GetCurrentStepID returns the current step ID (for external use)
func (c *ContextAwareEventBridge) GetCurrentStepID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentStepID
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

// SetBatchContext sets the current batch execution context
func (c *ContextAwareEventBridge) SetBatchContext(groupID string, groupIndex int, totalGroups int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentGroupID = groupID
	c.currentGroupIdx = groupIndex
	c.totalGroups = totalGroups

	c.logger.Info(fmt.Sprintf("📦 Set batch context: group %s (%d/%d)", groupID, groupIndex+1, totalGroups))
}

// ClearBatchContext clears the batch execution context
func (c *ContextAwareEventBridge) ClearBatchContext() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentGroupID = ""
	c.currentGroupIdx = 0
	c.totalGroups = 0

	c.logger.Info("🧹 Cleared batch context")
}

// SetCurrentStepID sets the current step ID (simple version - just step ID for all events)
func (c *ContextAwareEventBridge) SetCurrentStepID(stepID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentStepID = stepID

	c.logger.Info(fmt.Sprintf("🎯 Set current step ID: %s", stepID))
}

// ClearCurrentStepID clears the current step ID
func (c *ContextAwareEventBridge) ClearCurrentStepID() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentStepID = ""

	c.logger.Info("🧹 Cleared current step ID")
}

// HandleEvent implements AgentEventListener interface
func (c *ContextAwareEventBridge) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	// Tag events with correlation ID from context (for parallel agent grouping).
	// This allows the frontend to parent tool calls under their orchestrator_agent_start.
	// ForceCorrelationIDKey takes priority — it survives child agent context overwrites.
	isSubAgent, _ := ctx.Value(orchevents.IsSubAgentContextKey).(bool)
	if isSubAgent && !strings.HasPrefix(event.CorrelationID, "delegation-") {
		if forcedID, ok := ctx.Value(orchevents.ForceCorrelationIDKey).(string); ok && forcedID != "" {
			event.CorrelationID = forcedID
		} else if agentSessionID, ok := ctx.Value(AgentSessionIDKey).(string); ok && agentSessionID != "" {
			event.CorrelationID = agentSessionID
		}
	}

	// Tag events with workshop_step_id in metadata (without changing CorrelationID).
	// This lets the frontend detect "this event belongs to a workshop step execution"
	// for auto-notifications, without breaking EventHierarchy grouping.
	if forcedID, ok := ctx.Value(orchevents.ForceCorrelationIDKey).(string); ok && forcedID != "" && strings.HasPrefix(forcedID, "workshop-") {
		if baseData, ok := event.Data.(interface {
			GetBaseEventData() *events.BaseEventData
		}); ok {
			if bd := baseData.GetBaseEventData(); bd != nil {
				if bd.Metadata == nil {
					bd.Metadata = make(map[string]any)
				}
				bd.Metadata["workshop_step_id"] = forcedID
			}
		}
	}

	// Copy orchestrator and batch context while holding read lock
	c.mu.RLock()
	currentPhase := c.currentPhase
	currentStep := c.currentStep
	currentStepID := c.currentStepID
	currentAgentName := c.currentAgentName
	currentGroupID := c.currentGroupID
	currentGroupIdx := c.currentGroupIdx
	totalGroups := c.totalGroups
	c.mu.RUnlock()

	// Check what context we have
	hasOrchestratorContext := currentPhase != ""
	hasBatchContext := totalGroups > 0
	hasStepID := currentStepID != ""

	// Add context to metadata if we have any context (step ID, batch, or orchestrator)
	if hasOrchestratorContext || hasBatchContext || hasStepID {
		c.logger.Debug(fmt.Sprintf("🔍 ContextAwareBridge: Processing event %s with step %s, batch %s", event.Type, currentStepID, currentGroupID))

		// Add context to metadata
		// We need to check if the event data has a BaseEventData field
		if eventData, ok := event.Data.(interface {
			GetBaseEventData() *events.BaseEventData
		}); ok {
			baseData := eventData.GetBaseEventData()

			// Nil check before accessing Metadata
			if baseData == nil {
				c.logger.Warn(fmt.Sprintf("⚠️ ContextAwareBridge: GetBaseEventData returned nil for event %s", event.Type))
			} else {
				// Build a NEW metadata map instead of modifying in-place.
				// Modifying an existing map while another goroutine may be iterating it
				// during JSON serialization (SSE) causes "concurrent map iteration and map write".
				// By creating a fresh map and assigning it before forwarding the event,
				// the SSE goroutine always sees a fully-populated, immutable snapshot.
				newMeta := make(map[string]any, len(baseData.Metadata)+8)
				for k, v := range baseData.Metadata {
					newMeta[k] = v
				}

				// Add current step ID (simple tracking - which step is running)
				if hasStepID {
					newMeta["current_step_id"] = currentStepID
				}

				// Add batch context (which group is running)
				if hasBatchContext {
					newMeta["batch_group_id"] = currentGroupID
					newMeta["batch_group_index"] = currentGroupIdx
					newMeta["batch_total_groups"] = totalGroups
				}

				// Add full orchestrator context if available (backward compat)
				if hasOrchestratorContext {
					newMeta["orchestrator_phase"] = currentPhase
					newMeta["orchestrator_step"] = currentStep
					newMeta["orchestrator_step_id"] = currentStepID
					newMeta["orchestrator_agent_name"] = currentAgentName
				}

				// Atomically replace the metadata map (all writes done before assignment)
				baseData.Metadata = newMeta

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
								c.logger.Debug("💾 Persisted phase token usage directly to file")
							}
						}()
					} else {
						c.logger.Debug("⚠️ Token persister does not implement PhaseTokenPersister, skipping phase token persistence")
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
							c.logger.Debug("💾 Persisted token usage directly to file")
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
