package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	orchevents "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
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

// ToolCallEntry represents a captured tool call for logging purposes.
type ToolCallEntry struct {
	ToolCallID  string        `json:"tool_call_id"`
	ToolName    string        `json:"tool_name"`
	Args        string        `json:"args,omitempty"`
	Result      string        `json:"result,omitempty"`
	Error       string        `json:"error,omitempty"`
	Duration    time.Duration `json:"duration,omitempty"`
	StepID      string        `json:"step_id,omitempty"`
	Timestamp   time.Time     `json:"timestamp"`
	StartedAt   time.Time     `json:"started_at,omitempty"`
	CompletedAt time.Time     `json:"completed_at,omitempty"`
}

// LLMCallEntry represents a captured LLM call for timing/debug logging.
type LLMCallEntry struct {
	Turn                  int           `json:"turn,omitempty"`
	ModelID               string        `json:"model_id,omitempty"`
	Status                string        `json:"status"`
	Error                 string        `json:"error,omitempty"`
	StartedAt             time.Time     `json:"started_at"`
	CompletedAt           time.Time     `json:"completed_at,omitempty"`
	Duration              time.Duration `json:"duration,omitempty"`
	FirstResponseAt       time.Time     `json:"first_response_at,omitempty"`
	TimeToFirstResponse   time.Duration `json:"time_to_first_response,omitempty"`
	FirstContentAt        time.Time     `json:"first_content_at,omitempty"`
	TimeToFirstContent    time.Duration `json:"time_to_first_content,omitempty"`
	FirstToolCallAt       time.Time     `json:"first_tool_call_at,omitempty"`
	TimeToFirstToolCall   time.Duration `json:"time_to_first_tool_call,omitempty"`
	ToolCalls             int           `json:"tool_calls,omitempty"`
	PromptTokens          int           `json:"prompt_tokens,omitempty"`
	CompletionTokens      int           `json:"completion_tokens,omitempty"`
	TotalTokens           int           `json:"total_tokens,omitempty"`
	CacheTokens           int           `json:"cache_tokens,omitempty"`
	ReasoningTokens       int           `json:"reasoning_tokens,omitempty"`
	ContextUsagePercent   float64       `json:"context_usage_percent,omitempty"`
	ModelContextWindow    int           `json:"model_context_window,omitempty"`
	FixedThresholdPercent float64       `json:"fixed_threshold_percent,omitempty"`
}

// TimingCaptureSnapshot is a per-attempt timing snapshot used by workflow logs.
type TimingCaptureSnapshot struct {
	ToolCalls []ToolCallEntry `json:"tool_calls,omitempty"`
	LLMCalls  []LLMCallEntry  `json:"llm_calls,omitempty"`
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
	currentGroupName string // Current group name being executed
	currentGroupIdx  int    // 0-based index of current group
	totalGroups      int    // Total number of groups in batch
	// Context stack for nested agent execution (e.g., orchestrator -> sub-agent)
	contextStack []orchestratorContext
	// Timing collector — captures tool and LLM timing events for workspace logging
	toolCalls       map[string]*ToolCallEntry // keyed by ToolCallID
	toolCallOrder   []string                  // insertion order
	llmCalls        []*LLMCallEntry
	activeLLMCalls  []*LLMCallEntry
	toolCallCapture bool // whether to capture tool calls
	mu              sync.RWMutex
	logger          loggerv2.Logger
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

// SetLogger updates the logger used by the bridge so workflow/group scoping can
// follow the active orchestrator execution context.
func (c *ContextAwareEventBridge) SetLogger(logger loggerv2.Logger) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logger = logger
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
func (c *ContextAwareEventBridge) SetBatchContext(groupName string, groupIndex int, totalGroups int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentGroupName = groupName
	c.currentGroupIdx = groupIndex
	c.totalGroups = totalGroups

	c.logger.Info(fmt.Sprintf("📦 Set batch context: group %s (%d/%d)", groupName, groupIndex+1, totalGroups))
}

// ClearBatchContext clears the batch execution context
func (c *ContextAwareEventBridge) ClearBatchContext() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentGroupName = ""
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
				newMeta := make(map[string]any, len(bd.Metadata)+1)
				for k, v := range bd.Metadata {
					newMeta[k] = v
				}
				newMeta["workshop_step_id"] = forcedID
				bd.Metadata = newMeta
			}
		}
	}

	// Copy orchestrator and batch context while holding read lock
	c.mu.RLock()
	currentPhase := c.currentPhase
	currentStep := c.currentStep
	currentStepID := c.currentStepID
	currentAgentName := c.currentAgentName
	currentGroupName := c.currentGroupName
	currentGroupIdx := c.currentGroupIdx
	totalGroups := c.totalGroups
	c.mu.RUnlock()

	// Check what context we have
	hasOrchestratorContext := currentPhase != ""
	hasBatchContext := totalGroups > 0
	hasStepID := currentStepID != ""

	// Add context to metadata if we have any context (step ID, batch, or orchestrator)
	if hasOrchestratorContext || hasBatchContext || hasStepID {
		c.logger.Debug(fmt.Sprintf("🔍 ContextAwareBridge: Processing event %s with step %s, batch %s", event.Type, currentStepID, currentGroupName))

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
					newMeta["batch_group_name"] = currentGroupName
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
					InputTokens:      tokenEvent.PromptTokens,         // input tokens
					OutputTokens:     tokenEvent.CompletionTokens,     // output tokens
					CacheTokens:      cacheTokensSeparate.Total,       // total cache (backward compat)
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
						InputTokens:      tokenEvent.PromptTokens,         // input tokens
						OutputTokens:     tokenEvent.CompletionTokens,     // output tokens
						CacheTokens:      cacheTokensSeparate.Total,       // total cache (backward compat)
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
						StepID:           currentStepID,                   // Use step ID instead of index
						InputTokens:      tokenEvent.PromptTokens,         // input tokens
						OutputTokens:     tokenEvent.CompletionTokens,     // output tokens
						CacheTokens:      cacheTokensSeparate.Total,       // total cache (backward compat)
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

	// Capture tool call events when capture is enabled
	c.mu.RLock()
	capture := c.toolCallCapture
	c.mu.RUnlock()
	if capture && event.Data != nil {
		c.captureToolCallEvent(event)
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

// StartTimingCapture enables per-attempt timing capture. Call DrainTimingCapture to retrieve and reset.
func (c *ContextAwareEventBridge) StartTimingCapture() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolCallCapture = true
	c.toolCalls = make(map[string]*ToolCallEntry)
	c.toolCallOrder = nil
	c.llmCalls = nil
	c.activeLLMCalls = nil
}

// StartToolCallCapture enables tool call capture. Call DrainToolCalls to retrieve and reset.
// Deprecated: use StartTimingCapture for tool + LLM timing.
func (c *ContextAwareEventBridge) StartToolCallCapture() {
	c.StartTimingCapture()
}

// DrainTimingCapture returns all captured timing data in order and resets the collector.
func (c *ContextAwareEventBridge) DrainTimingCapture() TimingCaptureSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolCallCapture = false

	result := TimingCaptureSnapshot{
		ToolCalls: make([]ToolCallEntry, 0, len(c.toolCallOrder)),
		LLMCalls:  make([]LLMCallEntry, 0, len(c.llmCalls)),
	}
	for _, id := range c.toolCallOrder {
		if tc, ok := c.toolCalls[id]; ok {
			result.ToolCalls = append(result.ToolCalls, *tc)
		}
	}
	for _, llmCall := range c.llmCalls {
		if llmCall != nil {
			result.LLMCalls = append(result.LLMCalls, *llmCall)
		}
	}

	c.toolCalls = nil
	c.toolCallOrder = nil
	c.llmCalls = nil
	c.activeLLMCalls = nil
	return result
}

// DrainToolCalls returns all captured tool calls in order and resets the collector.
// Deprecated: use DrainTimingCapture for tool + LLM timing.
func (c *ContextAwareEventBridge) DrainToolCalls() []ToolCallEntry {
	return c.DrainTimingCapture().ToolCalls
}

func (c *ContextAwareEventBridge) captureToolCallEvent(event *events.AgentEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.toolCalls == nil && c.llmCalls == nil {
		return
	}

	eventTime := event.Timestamp
	if eventTime.IsZero() {
		eventTime = time.Now()
	}

	switch d := event.Data.(type) {
	case *events.LLMGenerationStartEvent:
		entry := &LLMCallEntry{
			Turn:      d.Turn,
			ModelID:   d.ModelID,
			Status:    "running",
			StartedAt: eventTime,
		}
		c.llmCalls = append(c.llmCalls, entry)
		c.activeLLMCalls = append(c.activeLLMCalls, entry)
	case *events.StreamingChunkEvent:
		if len(c.activeLLMCalls) == 0 || d.Content == "" {
			return
		}
		current := c.activeLLMCalls[0]
		if current.FirstResponseAt.IsZero() {
			current.FirstResponseAt = eventTime
			current.TimeToFirstResponse = eventTime.Sub(current.StartedAt)
		}
		if current.FirstContentAt.IsZero() {
			current.FirstContentAt = eventTime
			current.TimeToFirstContent = eventTime.Sub(current.StartedAt)
		}
	case *events.ToolCallStartEvent:
		if _, exists := c.toolCalls[d.ToolCallID]; !exists {
			c.toolCallOrder = append(c.toolCallOrder, d.ToolCallID)
		}
		c.toolCalls[d.ToolCallID] = &ToolCallEntry{
			ToolCallID: d.ToolCallID,
			ToolName:   d.ToolName,
			Args:       d.ToolParams.Arguments,
			StepID:     c.currentStepID,
			Timestamp:  eventTime,
			StartedAt:  eventTime,
		}
		if len(c.activeLLMCalls) > 0 {
			current := c.activeLLMCalls[0]
			if current.FirstResponseAt.IsZero() {
				current.FirstResponseAt = eventTime
				current.TimeToFirstResponse = eventTime.Sub(current.StartedAt)
			}
			if current.FirstToolCallAt.IsZero() {
				current.FirstToolCallAt = eventTime
				current.TimeToFirstToolCall = eventTime.Sub(current.StartedAt)
			}
		}
	case *events.ToolCallEndEvent:
		if tc, ok := c.toolCalls[d.ToolCallID]; ok {
			tc.Result = d.Result
			tc.Duration = d.Duration
			tc.CompletedAt = eventTime
		} else {
			c.toolCallOrder = append(c.toolCallOrder, d.ToolCallID)
			c.toolCalls[d.ToolCallID] = &ToolCallEntry{
				ToolCallID:  d.ToolCallID,
				ToolName:    d.ToolName,
				Result:      d.Result,
				Duration:    d.Duration,
				StepID:      c.currentStepID,
				Timestamp:   eventTime,
				StartedAt:   eventTime.Add(-d.Duration),
				CompletedAt: eventTime,
			}
		}
	case *events.ToolCallErrorEvent:
		if tc, ok := c.toolCalls[d.ToolCallID]; ok {
			tc.Error = d.Error
			tc.Duration = d.Duration
			tc.CompletedAt = eventTime
		} else {
			c.toolCallOrder = append(c.toolCallOrder, d.ToolCallID)
			c.toolCalls[d.ToolCallID] = &ToolCallEntry{
				ToolCallID:  d.ToolCallID,
				ToolName:    d.ToolName,
				Error:       d.Error,
				Duration:    d.Duration,
				StepID:      c.currentStepID,
				Timestamp:   eventTime,
				StartedAt:   eventTime.Add(-d.Duration),
				CompletedAt: eventTime,
			}
		}
	case *events.LLMGenerationEndEvent:
		entry := c.consumeActiveLLMCall()
		if entry == nil {
			entry = &LLMCallEntry{
				Status:    "success",
				StartedAt: eventTime.Add(-d.Duration),
			}
			c.llmCalls = append(c.llmCalls, entry)
		}
		entry.Status = "success"
		if d.Turn != 0 {
			entry.Turn = d.Turn
		}
		entry.CompletedAt = eventTime
		entry.Duration = d.Duration
		entry.ToolCalls = d.ToolCalls
		entry.PromptTokens = d.UsageMetrics.PromptTokens
		entry.CompletionTokens = d.UsageMetrics.CompletionTokens
		entry.TotalTokens = d.UsageMetrics.TotalTokens
		entry.CacheTokens = d.UsageMetrics.CacheTokens
		entry.ReasoningTokens = d.UsageMetrics.ReasoningTokens
		if entry.ModelID == "" {
			entry.ModelID = extractStringMetadata(d.Metadata, "resolved_model", "model_id", "gemini_model", "claude_code_model")
		}
		entry.ContextUsagePercent = extractFloatMetadata(d.Metadata, "context_usage_percent")
		entry.FixedThresholdPercent = extractFloatMetadata(d.Metadata, "fixed_threshold_percent")
		entry.ModelContextWindow = extractIntMetadata(d.Metadata, "model_context_window")
		c.finalizeLLMEntryTiming(entry)
	case *events.LLMGenerationErrorEvent:
		entry := c.consumeActiveLLMCall()
		if entry == nil {
			entry = &LLMCallEntry{
				StartedAt: eventTime.Add(-d.Duration),
			}
			c.llmCalls = append(c.llmCalls, entry)
		}
		entry.Status = "error"
		if d.Turn != 0 {
			entry.Turn = d.Turn
		}
		if entry.ModelID == "" {
			entry.ModelID = d.ModelID
		}
		entry.Error = d.Error
		entry.CompletedAt = eventTime
		entry.Duration = d.Duration
		c.finalizeLLMEntryTiming(entry)
	case *events.ContextCancelledEvent:
		entry := c.consumeActiveLLMCall()
		if entry == nil {
			return
		}
		entry.Status = "canceled"
		if d.Turn != 0 {
			entry.Turn = d.Turn
		}
		entry.Error = d.Reason
		entry.CompletedAt = eventTime
		entry.Duration = d.Duration
		c.finalizeLLMEntryTiming(entry)
	}
}

func (c *ContextAwareEventBridge) consumeActiveLLMCall() *LLMCallEntry {
	if len(c.activeLLMCalls) == 0 {
		return nil
	}
	entry := c.activeLLMCalls[0]
	c.activeLLMCalls = c.activeLLMCalls[1:]
	return entry
}

func (c *ContextAwareEventBridge) finalizeLLMEntryTiming(entry *LLMCallEntry) {
	if entry == nil {
		return
	}
	if entry.CompletedAt.IsZero() {
		entry.CompletedAt = entry.StartedAt
	}
	if entry.Duration <= 0 && !entry.StartedAt.IsZero() && !entry.CompletedAt.IsZero() {
		entry.Duration = entry.CompletedAt.Sub(entry.StartedAt)
	}
	if entry.TimeToFirstResponse <= 0 && !entry.FirstResponseAt.IsZero() && !entry.StartedAt.IsZero() {
		entry.TimeToFirstResponse = entry.FirstResponseAt.Sub(entry.StartedAt)
	}
	if entry.TimeToFirstContent <= 0 && !entry.FirstContentAt.IsZero() && !entry.StartedAt.IsZero() {
		entry.TimeToFirstContent = entry.FirstContentAt.Sub(entry.StartedAt)
	}
	if entry.TimeToFirstToolCall <= 0 && !entry.FirstToolCallAt.IsZero() && !entry.StartedAt.IsZero() {
		entry.TimeToFirstToolCall = entry.FirstToolCallAt.Sub(entry.StartedAt)
	}
	if entry.FirstResponseAt.IsZero() && !entry.CompletedAt.IsZero() && (entry.ToolCalls > 0 || entry.Duration > 0) {
		entry.FirstResponseAt = entry.CompletedAt
		entry.TimeToFirstResponse = entry.CompletedAt.Sub(entry.StartedAt)
	}
	if entry.FirstContentAt.IsZero() && !entry.CompletedAt.IsZero() && entry.CompletionTokens > 0 {
		entry.FirstContentAt = entry.CompletedAt
		entry.TimeToFirstContent = entry.CompletedAt.Sub(entry.StartedAt)
	}
	if entry.FirstToolCallAt.IsZero() && !entry.CompletedAt.IsZero() && entry.ToolCalls > 0 {
		entry.FirstToolCallAt = entry.CompletedAt
		entry.TimeToFirstToolCall = entry.CompletedAt.Sub(entry.StartedAt)
	}
}

func extractStringMetadata(meta map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := meta[key]; ok {
			if asString, ok := value.(string); ok && strings.TrimSpace(asString) != "" {
				return asString
			}
		}
	}
	return ""
}

func extractFloatMetadata(meta map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := meta[key]; ok {
			switch v := value.(type) {
			case float64:
				return v
			case float32:
				return float64(v)
			case int:
				return float64(v)
			case int64:
				return float64(v)
			}
		}
	}
	return 0
}

func extractIntMetadata(meta map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if value, ok := meta[key]; ok {
			switch v := value.(type) {
			case int:
				return v
			case int64:
				return int(v)
			case float64:
				return int(v)
			case float32:
				return int(v)
			}
		}
	}
	return 0
}
