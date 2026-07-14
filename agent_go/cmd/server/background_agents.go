package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	agent "github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentwrapper"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	unifiedevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// BackgroundAgentStatus represents the status of a background agent
type BackgroundAgentStatus string

const (
	BGAgentRunning   BackgroundAgentStatus = "running"
	BGAgentCompleted BackgroundAgentStatus = "completed"
	BGAgentFailed    BackgroundAgentStatus = "failed"
	BGAgentCanceled  BackgroundAgentStatus = "canceled"
)

// HistoryEntry represents a single message from the sub-agent's conversation history
type HistoryEntry struct {
	Role string `json:"role"` // "user", "assistant", "tool"
	Text string `json:"text"` // text content (truncated)
}

// HistoryFunc returns the last N entries from a sub-agent's conversation history.
// Set by server.go after the sub-agent wrapper is created.
type HistoryFunc func(lastN int) []HistoryEntry

// ToolCallRecord tracks a single tool call with timing
type ToolCallRecord struct {
	ToolName  string        `json:"tool_name"`
	StartedAt time.Time     `json:"started_at"`
	Duration  time.Duration `json:"duration,omitempty"` // 0 = still running
	Status    string        `json:"status"`             // "running", "completed", "error"
}

// BackgroundAgent represents a background agent running asynchronously
type BackgroundAgent struct {
	ID                   string                `json:"id"`
	ParentExecutionID    string                `json:"parent_execution_id,omitempty"`
	Name                 string                `json:"name"`
	SessionID            string                `json:"session_id"`
	Instruction          string                `json:"instruction"`
	Kind                 string                `json:"kind,omitempty"`
	Status               BackgroundAgentStatus `json:"status"`
	Result               string                `json:"result,omitempty"`
	Error                string                `json:"error,omitempty"`
	CreatedAt            time.Time             `json:"created_at"`
	CompletedAt          *time.Time            `json:"completed_at,omitempty"`
	ReasoningLevel       string                `json:"reasoning_level,omitempty"`
	ModelID              string                `json:"model_id,omitempty"`
	Metadata             map[string]string     `json:"metadata,omitempty"` // arbitrary key-value pairs (e.g. workshop_mode, lock_code)
	cancel               context.CancelFunc
	mu                   sync.RWMutex
	startNotified        bool
	notified             bool
	notificationInFlight bool
	terminalNotified     bool             // a terminal event (background_agent_terminated) has been emitted; prevents duplicates across OnExecutionTerminated / OnExecutionComplete
	getHistory           HistoryFunc      // returns last N conversation entries from the running sub-agent
	toolCalls            []ToolCallRecord // tracked tool calls with timing
	activeToolCall       map[string]int   // toolCallID → index in toolCalls (for matching start/end)
}

func (a *BackgroundAgent) beginCompletionNotification() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.notified || a.notificationInFlight {
		return false
	}
	a.notificationInFlight = true
	return true
}

func (a *BackgroundAgent) finishCompletionNotification(delivered bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notificationInFlight = false
	if delivered {
		a.notified = true
	}
}

// MarkStartNotified records that the main agent has been notified about this
// background agent starting. It returns false when the start notification was
// already sent or queued and consumed.
func (a *BackgroundAgent) MarkStartNotified() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.startNotified {
		return false
	}
	a.startNotified = true
	return true
}

// MarkTerminalNotified records that a terminal event (background_agent_terminated)
// has been emitted for this agent. It returns false when one was already emitted,
// so the OnExecutionTerminated (explicit stop) and OnExecutionComplete (context
// cancel / timeout) paths can each attempt emission without producing duplicates.
func (a *BackgroundAgent) MarkTerminalNotified() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.terminalNotified {
		return false
	}
	a.terminalNotified = true
	return true
}

// SetResult updates the agent result and status atomically
// SetResult marks the agent as completed with the given result.
// If the agent was already canceled (e.g. parent workflow stopped), the status is preserved
// to prevent stale completion notifications from racing with CancelAll.
func (a *BackgroundAgent) SetResult(result string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Status == BGAgentCanceled {
		log.Printf("[BG AGENT] SetResult skipped for canceled agent %s", a.ID)
		return
	}
	a.Result = result
	a.Status = BGAgentCompleted
	now := time.Now()
	a.CompletedAt = &now
}

// SetError updates the agent error and status atomically
// SetError marks the agent as failed with the given error message.
// If the agent was already canceled, the status is preserved to prevent
// stale error notifications from racing with CancelAll.
func (a *BackgroundAgent) SetError(errMsg string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Status == BGAgentCanceled {
		log.Printf("[BG AGENT] SetError skipped for canceled agent %s", a.ID)
		return
	}
	a.Error = errMsg
	a.Status = BGAgentFailed
	now := time.Now()
	a.CompletedAt = &now
}

// SetCanceled marks the agent as canceled
func (a *BackgroundAgent) SetCanceled() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Status = BGAgentCanceled
	now := time.Now()
	a.CompletedAt = &now
}

// RecordToolCallStart records a tool call starting
func (a *BackgroundAgent) RecordToolCallStart(toolCallID, toolName string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activeToolCall == nil {
		a.activeToolCall = make(map[string]int)
	}
	idx := len(a.toolCalls)
	a.toolCalls = append(a.toolCalls, ToolCallRecord{
		ToolName:  toolName,
		StartedAt: time.Now(),
		Status:    "running",
	})
	if toolCallID != "" {
		a.activeToolCall[toolCallID] = idx
	}
}

// RecordToolCallEnd records a tool call completing
func (a *BackgroundAgent) RecordToolCallEnd(toolCallID, toolName string, duration time.Duration, isError bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	status := "completed"
	if isError {
		status = "error"
	}
	// Try to match by toolCallID first, then by name (last running match)
	idx := -1
	if toolCallID != "" {
		if i, ok := a.activeToolCall[toolCallID]; ok {
			idx = i
			delete(a.activeToolCall, toolCallID)
		}
	}
	if idx == -1 {
		// Fallback: find last running tool call with same name
		for i := len(a.toolCalls) - 1; i >= 0; i-- {
			if a.toolCalls[i].ToolName == toolName && a.toolCalls[i].Status == "running" {
				idx = i
				break
			}
		}
	}
	if idx >= 0 && idx < len(a.toolCalls) {
		a.toolCalls[idx].Duration = duration
		a.toolCalls[idx].Status = status
	}
}

// GetRecentToolCalls returns the last N tool call records
func (a *BackgroundAgent) GetRecentToolCalls(lastN int) []ToolCallRecord {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.toolCalls) == 0 {
		return nil
	}
	start := 0
	if lastN > 0 && len(a.toolCalls) > lastN {
		start = len(a.toolCalls) - lastN
	}
	result := make([]ToolCallRecord, len(a.toolCalls)-start)
	copy(result, a.toolCalls[start:])
	return result
}

// SetHistoryFunc sets the function to retrieve conversation history
func (a *BackgroundAgent) SetHistoryFunc(fn HistoryFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.getHistory = fn
}

// GetRecentHistory returns the last N conversation entries (thread-safe)
func (a *BackgroundAgent) GetRecentHistory(lastN int) []HistoryEntry {
	a.mu.RLock()
	fn := a.getHistory
	a.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(lastN)
}

// GetStatus returns the current status (thread-safe)
func (a *BackgroundAgent) GetStatus() BackgroundAgentStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Status
}

// BackgroundAgentSnapshot is a value-type copy of BackgroundAgent without the mutex.
// Used to safely return agent state without copying sync.RWMutex.
type BackgroundAgentSnapshot struct {
	ID                string                `json:"id"`
	ParentExecutionID string                `json:"parent_execution_id,omitempty"`
	Name              string                `json:"name"`
	SessionID         string                `json:"session_id"`
	Instruction       string                `json:"instruction"`
	Kind              string                `json:"kind,omitempty"`
	Status            BackgroundAgentStatus `json:"status"`
	Result            string                `json:"result,omitempty"`
	Error             string                `json:"error,omitempty"`
	CreatedAt         time.Time             `json:"created_at"`
	CompletedAt       *time.Time            `json:"completed_at,omitempty"`
	ReasoningLevel    string                `json:"reasoning_level,omitempty"`
	ModelID           string                `json:"model_id,omitempty"`
	Metadata          map[string]string     `json:"metadata,omitempty"`
}

// GetSnapshot returns a snapshot of the agent state (thread-safe)
func (a *BackgroundAgent) GetSnapshot() BackgroundAgentSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	snap := BackgroundAgentSnapshot{
		ID:                a.ID,
		ParentExecutionID: a.ParentExecutionID,
		Name:              a.Name,
		SessionID:         a.SessionID,
		Instruction:       a.Instruction,
		Kind:              a.Kind,
		Status:            a.Status,
		Result:            a.Result,
		Error:             a.Error,
		CreatedAt:         a.CreatedAt,
		ReasoningLevel:    a.ReasoningLevel,
		ModelID:           a.ModelID,
		Metadata:          a.Metadata,
	}
	if a.CompletedAt != nil {
		t := *a.CompletedAt
		snap.CompletedAt = &t
	}
	return snap
}

// SetMetadata stores arbitrary key-value metadata on the agent (thread-safe).
func (a *BackgroundAgent) SetMetadata(meta map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Metadata = meta
}

// BackgroundAgentRegistry manages background agents across sessions
type BackgroundAgentRegistry struct {
	agents              map[string]map[string]*BackgroundAgent // sessionID → agentID → agent
	mu                  sync.RWMutex
	completionNotifiers map[string]chan string // sessionID → completion channel
	idCounter           atomic.Uint64          // monotonic counter for short agent IDs

	// onDropped is called when NotifyCompletion cannot send because the channel is
	// full. It must re-queue the completion so it is not permanently lost.
	// Set at construction time by StreamingAPI.
	onDropped func(sessionID, agentID string)
}

// NewBackgroundAgentRegistry creates a new registry
func NewBackgroundAgentRegistry() *BackgroundAgentRegistry {
	return &BackgroundAgentRegistry{
		agents:              make(map[string]map[string]*BackgroundAgent),
		completionNotifiers: make(map[string]chan string),
	}
}

// NextID returns the next short agent ID using a prefix derived from the name.
// Takes first 4 alphanumeric lowercase chars from name (e.g. "Research APIs" → "rese-0001").
// Wraps at 9999 back to 0001.
func (r *BackgroundAgentRegistry) NextID(name string) string {
	n := r.idCounter.Add(1)
	short := ((n - 1) % 9999) + 1 // 1..9999

	// Extract up to 4 lowercase alphanumeric chars from name
	prefix := make([]byte, 0, 4)
	for _, c := range strings.ToLower(name) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			prefix = append(prefix, byte(c))
			if len(prefix) == 4 {
				break
			}
		}
	}
	if len(prefix) == 0 {
		prefix = []byte("agent")
	}

	return fmt.Sprintf("%s-%04d", string(prefix), short)
}

// Register adds a background agent to the registry
func (r *BackgroundAgentRegistry) Register(sessionID string, agent *BackgroundAgent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agents[sessionID] == nil {
		r.agents[sessionID] = make(map[string]*BackgroundAgent)
	}
	r.agents[sessionID][agent.ID] = agent
}

// Get returns a background agent by session and agent ID
func (r *BackgroundAgentRegistry) Get(sessionID, agentID string) *BackgroundAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if sessionAgents, ok := r.agents[sessionID]; ok {
		return sessionAgents[agentID]
	}
	return nil
}

// GetAll returns all background agents for a session
func (r *BackgroundAgentRegistry) GetAll(sessionID string) []*BackgroundAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sessionAgents, ok := r.agents[sessionID]
	if !ok {
		return nil
	}
	agents := make([]*BackgroundAgent, 0, len(sessionAgents))
	for _, agent := range sessionAgents {
		agents = append(agents, agent)
	}
	return agents
}

// CancelAgent cancels a specific background agent
func (r *BackgroundAgentRegistry) CancelAgent(sessionID, agentID string) error {
	r.mu.RLock()
	agent := r.agents[sessionID][agentID]
	r.mu.RUnlock()

	if agent == nil {
		return fmt.Errorf("agent %s not found in session %s", agentID, sessionID)
	}

	status := agent.GetStatus()
	if status != BGAgentRunning {
		return fmt.Errorf("agent %s is not running (status: %s)", agentID, status)
	}

	if agent.cancel != nil {
		agent.cancel()
	}
	agent.SetCanceled()
	return nil
}

// CancelAll cancels all running background agents in a session
func (r *BackgroundAgentRegistry) CancelAll(sessionID string) {
	r.mu.RLock()
	sessionAgents, ok := r.agents[sessionID]
	if !ok {
		r.mu.RUnlock()
		return
	}
	// Copy the slice to avoid holding lock during cancel
	agents := make([]*BackgroundAgent, 0, len(sessionAgents))
	for _, agent := range sessionAgents {
		agents = append(agents, agent)
	}
	r.mu.RUnlock()

	for _, agent := range agents {
		if agent.GetStatus() == BGAgentRunning {
			if agent.cancel != nil {
				agent.cancel()
			}
			agent.SetCanceled()
		}
	}
}

// NotifyCompletion sends a completion notification for a session.
// Holds a write lock for the entire read-check-send sequence to prevent a
// concurrent Cleanup from closing the channel between the channel lookup and
// the send (which would panic with "send on closed channel" — BG-002).
// The non-blocking select is safe under a write lock because the send cannot
// block indefinitely.
func (r *BackgroundAgentRegistry) NotifyCompletion(sessionID, agentID string) {
	r.mu.Lock()
	ch, ok := r.completionNotifiers[sessionID]
	if !ok {
		r.mu.Unlock()
		return
	}
	select {
	case ch <- agentID:
		r.mu.Unlock()
	default:
		// Channel is full. Unlock before invoking the callback to avoid
		// holding the registry lock while the callback acquires pendingMu.
		onDropped := r.onDropped
		r.mu.Unlock()
		log.Printf("[BG AGENT] completion channel full for session %s; invoking onDropped for agent %s", sessionID, agentID)
		if onDropped != nil {
			onDropped(sessionID, agentID)
		}
	}
}

// GetNotificationChannel returns or creates the completion notification channel for a session
func (r *BackgroundAgentRegistry) GetNotificationChannel(sessionID string) chan string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ch, ok := r.completionNotifiers[sessionID]; ok {
		return ch
	}
	ch := make(chan string, 32) // Buffered to prevent blocking
	r.completionNotifiers[sessionID] = ch
	return ch
}

// Cleanup removes all agents and closes channels for a session
func (r *BackgroundAgentRegistry) Cleanup(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, sessionID)
	if ch, ok := r.completionNotifiers[sessionID]; ok {
		close(ch)
		delete(r.completionNotifiers, sessionID)
	}
}

// hasRunningAgentsGracePeriod is how long a recently-completed agent still counts as
// "running". This keeps the frontend builder-idle chip visible briefly after the last
// step finishes so the user has time to notice before the indicator disappears.
const hasRunningAgentsGracePeriod = 8 * time.Second

// HasRunningAgents returns true if the session has any running agents, or if any agent
// completed within the 8-second grace period.
func (r *BackgroundAgentRegistry) HasRunningAgents(sessionID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sessionAgents, ok := r.agents[sessionID]
	if !ok {
		return false
	}
	now := time.Now()
	for _, agent := range sessionAgents {
		if backgroundAgentCountsAsLiveActivity(agent.GetSnapshot(), now) {
			return true
		}
	}
	return false
}

func backgroundAgentCountsAsLiveActivity(snap BackgroundAgentSnapshot, now time.Time) bool {
	switch snap.Status {
	case BGAgentRunning:
		return true
	case BGAgentCompleted, BGAgentFailed:
		return snap.CompletedAt != nil && now.Sub(*snap.CompletedAt) < hasRunningAgentsGracePeriod
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Background-agent execution + lifecycle logic (relocated verbatim from
// server.go to sit alongside the BackgroundAgent types above).
// ---------------------------------------------------------------------------

// executeBackgroundDelegatedTask spawns a background goroutine for async delegation
func (api *StreamingAPI) executeBackgroundDelegatedTask(
	ctx context.Context, parentReq QueryRequest, sessionID, name, instruction string,
) (string, error) {
	agentID := api.bgAgentRegistry.NextID(name)
	bgCtx, bgCancel := context.WithCancel(context.Background())

	// Carry the delegation spec across the context boundary (bgCtx derives from
	// Background(), not from ctx, so nothing is inherited automatically).
	// Depth is reset to 0 because background sub-agents don't have the delegate
	// tool, so they can never create further sub-agents.
	// BackgroundAgentID is set to this agent so delegation_start can link back.
	bgSpec := virtualtools.SubAgentSpecFromContext(ctx)
	parentExecutionID := bgSpec.BackgroundAgentID
	bgSpec.Depth = 0
	bgSpec.BackgroundAgentID = agentID
	bgCtx = virtualtools.WithSubAgentSpec(bgCtx, bgSpec)

	// Propagate per-user Chats folder to background sub-agents so shell commands
	// resolve to the right user's workspace folder.
	if cf, ok := ctx.Value(virtualtools.ChatsFolderKey).(string); ok && cf != "" {
		bgCtx = context.WithValue(bgCtx, virtualtools.ChatsFolderKey, cf)
	}
	// Pass user ID for per-user OAuth
	if userID, ok := ctx.Value(common.UserIDKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, common.UserIDKey, userID)
		log.Printf("[USER_ID_DEBUGGING] Background agent: copied UserIDKey=%q to bgCtx", userID)
	}
	if dest, ok := ctx.Value(virtualtools.BotNotificationDestinationKey).(*services.NotificationDestination); ok && dest != nil {
		bgCtx = context.WithValue(bgCtx, virtualtools.BotNotificationDestinationKey, dest)
	}

	bgAgent := &BackgroundAgent{
		ID:                agentID,
		ParentExecutionID: parentExecutionID,
		Name:              name,
		SessionID:         sessionID,
		Instruction:       instruction,
		Kind:              "delegation",
		Status:            BGAgentRunning,
		CreatedAt:         time.Now(),
		cancel:            bgCancel,
	}
	api.bgAgentRegistry.Register(sessionID, bgAgent)

	// Inject tool event callback so executeDelegatedTask's observer tracks timing on bgAgent
	bgCtx = context.WithValue(bgCtx, virtualtools.ToolEventCallbackKey, events.ToolEventCallback(
		func(toolCallID, toolName, eventType string, duration time.Duration) {
			switch eventType {
			case "start":
				bgAgent.RecordToolCallStart(toolCallID, toolName)
			case "end":
				bgAgent.RecordToolCallEnd(toolCallID, toolName, duration, false)
			case "error":
				bgAgent.RecordToolCallEnd(toolCallID, toolName, duration, true)
			}
		},
	))

	// Emit background_agent_started event
	api.emitBackgroundAgentEvent(sessionID, agentID, "background_agent_started", map[string]interface{}{
		"agent_id":    agentID,
		"name":        name,
		"instruction": truncateForToolResponse(instruction, 200),
	})
	api.notifyBackgroundAgentStarted(sessionID, agentID)

	// Start the background completion loop for this session if not already running
	api.completionLoopStartedMu.Lock()
	if !api.completionLoopStarted[sessionID] {
		api.completionLoopStarted[sessionID] = true
		go api.backgroundCompletionLoop(sessionID)
	}
	api.completionLoopStartedMu.Unlock()

	go func() {
		defer bgCancel()
		result, err := api.executeDelegatedTask(bgCtx, parentReq, sessionID, instruction, func(wrapper *agent.LLMAgentWrapper) {
			// Attach history func so query_agent can read the sub-agent's live conversation
			bgAgent.SetHistoryFunc(func(lastN int) []HistoryEntry {
				history := wrapper.GetHistory()
				start := 0
				if lastN > 0 && len(history) > lastN {
					start = len(history) - lastN
				}
				var entries []HistoryEntry
				for _, msg := range history[start:] {
					role := string(msg.Role)
					var parts []string
					for _, part := range msg.Parts {
						switch p := part.(type) {
						case llmtypes.TextContent:
							if p.Text != "" {
								parts = append(parts, p.Text)
							}
						case llmtypes.ToolCall:
							name := ""
							args := ""
							if p.FunctionCall != nil {
								name = p.FunctionCall.Name
								args = p.FunctionCall.Arguments
							}
							parts = append(parts, fmt.Sprintf("[tool_call: %s(%s)]", name, args))
						case *llmtypes.ToolCall:
							name := ""
							args := ""
							if p != nil && p.FunctionCall != nil {
								name = p.FunctionCall.Name
								args = p.FunctionCall.Arguments
							}
							parts = append(parts, fmt.Sprintf("[tool_call: %s(%s)]", name, args))
						case llmtypes.ToolCallResponse:
							parts = append(parts, fmt.Sprintf("[tool_result: %s] %s", p.Name, p.Content))
						case *llmtypes.ToolCallResponse:
							if p != nil {
								parts = append(parts, fmt.Sprintf("[tool_result: %s] %s", p.Name, p.Content))
							}
						}
					}
					if len(parts) > 0 {
						entries = append(entries, HistoryEntry{
							Role: role,
							Text: strings.Join(parts, "\n"),
						})
					}
				}
				return entries
			})
		})

		now := time.Now()
		duration := now.Sub(bgAgent.CreatedAt)

		if err != nil {
			bgAgent.SetError(err.Error())
			api.emitBackgroundAgentEvent(sessionID, agentID, "background_agent_completed", map[string]interface{}{
				"agent_id": agentID,
				"name":     name,
				"status":   "failed",
				"error":    err.Error(),
				"duration": duration.Truncate(time.Second).String(),
			})
			log.Printf("[BG AGENT] Agent '%s' (ID: %s) failed after %s: %v", name, agentID, duration, err)
		} else {
			bgAgent.SetResult(result)
			api.emitBackgroundAgentEvent(sessionID, agentID, "background_agent_completed", map[string]interface{}{
				"agent_id": agentID,
				"name":     name,
				"status":   "completed",
				"result":   truncateForToolResponse(result, 500),
				"duration": duration.Truncate(time.Second).String(),
			})
			log.Printf("[BG AGENT] Agent '%s' (ID: %s) completed in %s", name, agentID, duration)
		}

		// Signal completion to the notification loop
		api.bgAgentRegistry.NotifyCompletion(sessionID, agentID)
	}()

	return agentID, nil
}

// emitBackgroundAgentEvent emits a background agent event to the event store
func (api *StreamingAPI) emitBackgroundAgentEvent(sessionID, agentID, eventType string, data map[string]interface{}) {
	if api == nil || api.eventStore == nil {
		return
	}
	if data == nil {
		data = make(map[string]interface{})
	}
	now := time.Now()
	data["timestamp"] = now.Format(time.RFC3339)
	if _, exists := data["parent_execution_id"]; !exists && api.bgAgentRegistry != nil && agentID != "" {
		if agent := api.bgAgentRegistry.Get(sessionID, agentID); agent != nil {
			if parentID := strings.TrimSpace(agent.GetSnapshot().ParentExecutionID); parentID != "" {
				data["parent_execution_id"] = parentID
			}
		}
	}

	eventID := fmt.Sprintf("%s_%s_%s", sessionID, eventType, agentID)
	if agentID == "" {
		eventID = fmt.Sprintf("%s_%s_%d", sessionID, eventType, now.UnixNano())
	} else if eventType == "synthetic_turn_ready" {
		if status, ok := data["status"].(string); ok && strings.TrimSpace(status) != "" {
			eventID = fmt.Sprintf("%s_%s_%s_%s", sessionID, eventType, strings.TrimSpace(status), agentID)
		}
	}

	event := events.Event{
		ID:        eventID,
		Type:      eventType,
		Timestamp: now,
		SessionID: sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:      unifiedevents.EventType(eventType),
			Timestamp: now,
			SessionID: sessionID,
			Component: "background-agent",
			Data:      events.NewGenericEventData(eventType, data),
		},
	}
	api.eventStore.AddEvent(sessionID, event)
}

// isSessionBusy returns whether the session is currently processing a user turn
func (api *StreamingAPI) isSessionBusy(sessionID string) bool {
	api.sessionBusyMu.RLock()
	defer api.sessionBusyMu.RUnlock()
	return api.sessionBusy[sessionID]
}

const autoNotificationStaleBusyAfter = 15 * time.Second

// setSessionBusy sets the busy state for a session
func (api *StreamingAPI) setSessionBusy(sessionID string, busy bool) {
	api.sessionBusyMu.Lock()
	if api.sessionBusy == nil {
		api.sessionBusy = make(map[string]bool)
	}
	if api.sessionBusySince == nil {
		api.sessionBusySince = make(map[string]time.Time)
	}
	if busy {
		if !api.sessionBusy[sessionID] {
			api.sessionBusySince[sessionID] = time.Now()
		}
	} else {
		delete(api.sessionBusySince, sessionID)
	}
	api.sessionBusy[sessionID] = busy
	api.sessionBusyMu.Unlock()
	api.observeRuntimeSnapshot(sessionID, nil)
}

func (api *StreamingAPI) hasActiveTurnCancel(sessionID string) bool {
	api.agentCancelMux.RLock()
	defer api.agentCancelMux.RUnlock()
	_, ok := api.agentCancelFuncs[sessionID]
	return ok
}

// clearStaleBusyIfNeeded atomically checks whether the busy flag is stale and,
// if so, clears it. It holds sessionBusyMu.Lock() for the entire read-and-clear
// sequence so two concurrent callers cannot both pass the staleness check and
// then both clear it (isSessionBusyForAutoNotification TOCTOU fix).
// Returns true when the flag was stale and has been cleared.
func (api *StreamingAPI) clearStaleBusyIfNeeded(sessionID string) bool {
	api.sessionBusyMu.Lock()
	defer api.sessionBusyMu.Unlock()
	if !api.sessionBusy[sessionID] {
		return false // already cleared or never set
	}
	since := api.sessionBusySince[sessionID]
	if since.IsZero() || time.Since(since) < autoNotificationStaleBusyAfter {
		return false // not stale yet
	}
	// Stale: clear atomically under the write lock.
	api.sessionBusy[sessionID] = false
	delete(api.sessionBusySince, sessionID)
	return true
}

// isSessionBusyForAutoNotification is intentionally narrower than isSessionBusy.
// Auto-notifications must be serialized behind real user/synthetic turns, but a
// stale busy flag should not permanently strand workflow step start/completion
// notifications. If the busy flag has no active cancel function behind it and
// has aged out, clear it so the synthetic turn can resume the provider session.
func (api *StreamingAPI) isSessionBusyForAutoNotification(sessionID string) bool {
	if !api.isSessionBusy(sessionID) {
		return false
	}
	if api.isSyntheticTurn(sessionID) || api.hasActiveTurnCancel(sessionID) {
		return true
	}
	if api.terminalStore != nil && api.terminalStore.SessionHasBusyCodingTmux(sessionID) {
		return true
	}

	if api.clearStaleBusyIfNeeded(sessionID) {
		log.Printf("[BG AGENT] Session %s busy flag looks stale; clearing so queued auto-notification can resume main agent", sessionID)
		return false
	}
	return true
}

// isSessionStoppedOrInactive returns true when a session has been explicitly stopped
// or aged out, in which case background completions must not trigger synthetic turns.
func (api *StreamingAPI) isSessionStoppedOrInactive(sessionID string) bool {
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()
	session, exists := api.activeSessions[sessionID]
	if !exists {
		return false
	}
	return session.Status == "stopped" || session.Status == "inactive"
}

// autoNotificationSessionUnreachable decides whether a background-completion
// auto-notification must be dropped. It is the auto-notification-specific guard:
// a session is unreachable ONLY when it was explicitly stopped, or it is idle
// ("inactive") with no agent left in memory to wake. A merely-idle session
// (marked "inactive" by the 10-minute cleanup) whose agent is still resident is
// NOT unreachable — a pending completion should REACTIVATE the main agent rather
// than be lost. In that case we clear the inactive mark (back to "running", which
// also refreshes LastActivity) so the synthetic turn can resume it. Without this,
// any background step that outlived the 10-minute idle window had its completion
// silently dropped even though the main agent was alive, stalling the workflow.
func (api *StreamingAPI) autoNotificationSessionUnreachable(sessionID string) bool {
	if api.isSessionMarkedStopped(sessionID) {
		return true
	}
	api.activeSessionsMux.RLock()
	status := ""
	if session, exists := api.activeSessions[sessionID]; exists {
		status = session.Status
	}
	api.activeSessionsMux.RUnlock()

	switch status {
	case "stopped":
		return true
	case "inactive":
		api.sessionAgentsMux.RLock()
		_, hasAgent := api.sessionAgents[sessionID]
		api.sessionAgentsMux.RUnlock()
		if !hasAgent {
			return true // agent already evicted (e.g. after restart) — nothing to wake here
		}
		// Idle but alive: reactivate so the completion notification can resume it.
		api.updateSessionStatus(sessionID, "running")
		log.Printf("[BG AGENT] Session %s was inactive but its agent is live; reactivating so the auto-notification can resume it", sessionID)
		return false
	default:
		return false
	}
}

// markSessionStopped records that this session must not spawn more work. User
// stops are the common case; fatal runtime cancellation also uses this guard
// while preserving an error lifecycle status.
func (api *StreamingAPI) markSessionStopped(sessionID string) {
	api.stoppedSessionsMu.Lock()
	api.stoppedSessions[sessionID] = true
	api.stoppedSessionsMu.Unlock()
}

// clearSessionStopped removes the stopped guard so the session can accept new queries.
// Called when a NEW user message explicitly reactivates the session (not by racing goroutines).
func (api *StreamingAPI) clearSessionStopped(sessionID string) {
	api.stoppedSessionsMu.Lock()
	delete(api.stoppedSessions, sessionID)
	api.stoppedSessionsMu.Unlock()
}

// isSessionMarkedStopped returns true while the session has a hard cancellation
// guard and no new user message has explicitly reactivated it.
func (api *StreamingAPI) isSessionMarkedStopped(sessionID string) bool {
	api.stoppedSessionsMu.RLock()
	defer api.stoppedSessionsMu.RUnlock()
	return api.stoppedSessions[sessionID]
}

func (api *StreamingAPI) markSessionTurnInterrupted(sessionID string) {
	api.interruptedTurnsMu.Lock()
	if api.interruptedTurns == nil {
		api.interruptedTurns = make(map[string]bool)
	}
	api.interruptedTurns[sessionID] = true
	api.interruptedTurnsMu.Unlock()
}

func (api *StreamingAPI) consumeSessionTurnInterrupted(sessionID string) bool {
	api.interruptedTurnsMu.Lock()
	defer api.interruptedTurnsMu.Unlock()
	if !api.interruptedTurns[sessionID] {
		return false
	}
	delete(api.interruptedTurns, sessionID)
	return true
}

// setSyntheticTurn marks a session as running an auto-notification synthetic turn.
// The frontend uses this to avoid blocking user input during background agent notifications.
func (api *StreamingAPI) setSyntheticTurn(sessionID string, synthetic bool) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()
	if session, exists := api.activeSessions[sessionID]; exists {
		session.IsSyntheticTurn = synthetic
	}
}

// isSyntheticTurn returns true if the session is currently running a synthetic (auto-notification) turn.
func (api *StreamingAPI) isSyntheticTurn(sessionID string) bool {
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()
	if session, exists := api.activeSessions[sessionID]; exists {
		return session.IsSyntheticTurn
	}
	return false
}

func (api *StreamingAPI) notifyBackgroundAgentStarted(sessionID, agentID string) {
	sessionID = strings.TrimSpace(sessionID)
	agentID = strings.TrimSpace(agentID)
	if sessionID == "" || agentID == "" || api == nil {
		return
	}
	if api.autoNotificationSessionUnreachable(sessionID) {
		return
	}

	// Interactive app sessions already receive the background_agent_started
	// event emitted by the notifier. Starting a synthetic LLM turn merely to
	// acknowledge that event resumes and resets the retained coding-CLI pane,
	// which makes the user's terminal appear to restart. Keep synthetic start
	// turns only for bot sessions, where a model turn is required to send the
	// acknowledgement back through the external chat channel. Completion
	// notifications remain unchanged and still reach the main agent.
	if !strings.HasPrefix(sessionID, "bot-") {
		if agent := api.bgAgentRegistry.Get(sessionID, agentID); agent != nil {
			agent.MarkStartNotified()
		}
		log.Printf("[BG AGENT] Recorded UI-only background start for agent %s in session %s", agentID, sessionID)
		return
	}

	api.autoNotificationMu.Lock()
	defer api.autoNotificationMu.Unlock()
	if api.isSessionBusyForAutoNotification(sessionID) {
		api.queuePendingStartNotification(sessionID, agentID)
		api.schedulePendingStartNotificationRetry(sessionID)
		log.Printf("[BG AGENT] Session %s busy, queued start notification for agent %s", sessionID, agentID)
		return
	}
	api.processBatchedBackgroundAgentStartsLocked(sessionID, []string{agentID})
}

func (api *StreamingAPI) queuePendingStartNotification(sessionID, agentID string) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return
	}
	api.pendingStartMu.Lock()
	defer api.pendingStartMu.Unlock()
	if api.pendingStartNotifications == nil {
		api.pendingStartNotifications = make(map[string][]string)
	}
	for _, existing := range api.pendingStartNotifications[sessionID] {
		if existing == agentID {
			return
		}
	}
	api.pendingStartNotifications[sessionID] = append(api.pendingStartNotifications[sessionID], agentID)
}

func (api *StreamingAPI) queuePendingStartNotifications(sessionID string, agentIDs []string) {
	for _, agentID := range agentIDs {
		api.queuePendingStartNotification(sessionID, agentID)
	}
}

func (api *StreamingAPI) drainPendingStartNotifications(sessionID string) []string {
	api.pendingStartMu.Lock()
	defer api.pendingStartMu.Unlock()
	pending := api.pendingStartNotifications[sessionID]
	delete(api.pendingStartNotifications, sessionID)
	return pending
}

func (api *StreamingAPI) filterUnsentStartNotifications(sessionID string, agentIDs []string) []string {
	if len(agentIDs) == 0 || api.bgAgentRegistry == nil {
		return nil
	}
	filtered := make([]string, 0, len(agentIDs))
	seen := make(map[string]struct{}, len(agentIDs))
	for _, agentID := range agentIDs {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			continue
		}
		if _, ok := seen[agentID]; ok {
			continue
		}
		seen[agentID] = struct{}{}
		agent := api.bgAgentRegistry.Get(sessionID, agentID)
		if agent == nil {
			continue
		}
		snap := agent.GetSnapshot()
		agent.mu.RLock()
		alreadySent := agent.startNotified
		completionNotified := agent.notified
		agent.mu.RUnlock()
		if !alreadySent && !completionNotified && !isTerminalBackgroundAgentStatus(snap.Status) {
			filtered = append(filtered, agentID)
		}
	}
	return filtered
}

func isTerminalBackgroundAgentStatus(status BackgroundAgentStatus) bool {
	return status == BGAgentCompleted || status == BGAgentFailed || status == BGAgentCanceled
}

func (api *StreamingAPI) schedulePendingStartNotificationRetry(sessionID string) {
	// Singleton guard: at most one retry timer per session (mirrors completionRetryScheduled).
	api.pendingStartMu.Lock()
	if api.startNotificationRetryScheduled == nil {
		api.startNotificationRetryScheduled = make(map[string]bool)
	}
	if api.startNotificationRetryScheduled[sessionID] {
		api.pendingStartMu.Unlock()
		return
	}
	api.startNotificationRetryScheduled[sessionID] = true
	api.pendingStartMu.Unlock()

	time.AfterFunc(5*time.Second, func() {
		api.pendingStartMu.Lock()
		delete(api.startNotificationRetryScheduled, sessionID)
		api.pendingStartMu.Unlock()

		if api.autoNotificationSessionUnreachable(sessionID) {
			return
		}
		if api.isSessionBusyForAutoNotification(sessionID) {
			api.schedulePendingStartNotificationRetry(sessionID)
			return
		}
		pending := api.filterUnsentStartNotifications(sessionID, api.drainPendingStartNotifications(sessionID))
		if len(pending) == 0 {
			return
		}
		api.processBatchedBackgroundAgentStarts(sessionID, pending)
	})
}

func (api *StreamingAPI) drainPendingAutoNotificationsAfterTurn(sessionID string) {
	pendingStarts := api.filterUnsentStartNotifications(sessionID, api.drainPendingStartNotifications(sessionID))
	pendingCompletions := api.drainPendingCompletions(sessionID)

	if len(pendingStarts) > 0 && len(pendingCompletions) > 0 {
		// Both pending at once (e.g. a parallel step completed while another
		// step was starting). Fire completions first — they carry actual results
		// the main agent needs — then starts. Called synchronously: executeSyntheticTurn
		// sets sessionBusy=true before returning, preventing a concurrent
		// StreamWithEvents from being spawned before this one finishes (timing-gap fix).
		// Re-queue starts for the completion turn's own post-turn drain.
		api.queuePendingStartNotifications(sessionID, pendingStarts)
		api.schedulePendingStartNotificationRetry(sessionID)
		api.processBatchedBackgroundAgentCompletions(sessionID, pendingCompletions)
		return
	}
	if len(pendingStarts) > 0 {
		api.processBatchedBackgroundAgentStarts(sessionID, pendingStarts)
		return
	}
	if len(pendingCompletions) > 0 {
		api.processBatchedBackgroundAgentCompletions(sessionID, pendingCompletions)
	}
}

// queuePendingCompletion adds a completed agent ID to the pending queue
func (api *StreamingAPI) queuePendingCompletion(sessionID, agentID string) {
	api.queuePendingCompletions(sessionID, []string{agentID})
}

func (api *StreamingAPI) queuePendingCompletions(sessionID string, agentIDs []string) {
	api.pendingMu.Lock()
	defer api.pendingMu.Unlock()
	if len(agentIDs) == 0 {
		return
	}
	if api.pendingCompletions == nil {
		api.pendingCompletions = make(map[string][]string)
	}
	seen := make(map[string]struct{}, len(api.pendingCompletions[sessionID])+len(agentIDs))
	for _, existing := range api.pendingCompletions[sessionID] {
		seen[existing] = struct{}{}
	}
	for _, agentID := range agentIDs {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			continue
		}
		if _, ok := seen[agentID]; ok {
			continue
		}
		api.pendingCompletions[sessionID] = append(api.pendingCompletions[sessionID], agentID)
		seen[agentID] = struct{}{}
	}
}

// drainPendingCompletions returns and clears all pending completion agent IDs
func (api *StreamingAPI) drainPendingCompletions(sessionID string) []string {
	api.pendingMu.Lock()
	defer api.pendingMu.Unlock()
	pending := api.pendingCompletions[sessionID]
	delete(api.pendingCompletions, sessionID)
	return pending
}

// schedulePendingCompletionRetry is the backstop that guarantees queued or
// dropped background-agent completions are eventually delivered even if no
// further user/synthetic turn fires drainPendingAutoNotificationsAfterTurn. It
// runs at most one timer per session (guarded by completionRetryScheduled);
// when the session next looks idle it re-sweeps the registry for any terminal-
// but-unnotified agent, then drains. Trigger it whenever a completion is queued
// because the session was busy.
func (api *StreamingAPI) schedulePendingCompletionRetry(sessionID string) {
	api.pendingMu.Lock()
	if api.completionRetryScheduled == nil {
		api.completionRetryScheduled = make(map[string]bool)
	}
	if api.completionRetryScheduled[sessionID] {
		api.pendingMu.Unlock()
		return
	}
	api.completionRetryScheduled[sessionID] = true
	api.pendingMu.Unlock()

	time.AfterFunc(5*time.Second, func() {
		api.pendingMu.Lock()
		delete(api.completionRetryScheduled, sessionID)
		api.pendingMu.Unlock()

		if api.autoNotificationSessionUnreachable(sessionID) {
			// Explicitly stopped, or inactive with no resident agent left to wake:
			// log a warning so the discard is observable rather than silent.
			api.pendingMu.RLock()
			nPending := len(api.pendingCompletions[sessionID])
			api.pendingMu.RUnlock()
			if nPending > 0 {
				log.Printf("[BG AGENT] WARNING: session %s unreachable with %d pending completion(s) — discarding", sessionID, nPending)
			}
			return
		}
		if api.isSessionBusyForAutoNotification(sessionID) {
			if api.canSteerSession(sessionID) {
				api.requeueUnnotifiedCompletions(sessionID)
				pending := api.drainPendingCompletions(sessionID)
				remaining := make([]string, 0, len(pending))
				for _, agentID := range pending {
					if api.steerBackgroundAgentCompletion(sessionID, agentID) {
						continue
					}
					remaining = append(remaining, agentID)
				}
				if len(remaining) > 0 {
					api.queuePendingCompletions(sessionID, remaining)
					api.schedulePendingCompletionRetry(sessionID)
				}
				return
			}
			// Still busy — re-arm and check again later.
			api.schedulePendingCompletionRetry(sessionID)
			return
		}
		// Recover both completions queued while busy AND any that a full
		// notification channel dropped, then deliver in one batch.
		api.requeueUnnotifiedCompletions(sessionID)
		pending := api.drainPendingCompletions(sessionID)
		if len(pending) == 0 {
			return
		}
		api.processBatchedBackgroundAgentCompletions(sessionID, pending)
	})
}

// requeueUnnotifiedCompletions sweeps the registry for agents whose execution
// finished (completed/failed) but whose synthetic [AUTO-NOTIFICATION] turn was
// never emitted (notified == false), and queues them for delivery. This is the
// safety net behind NotifyCompletion's best-effort channel send: a dropped or
// missed send cannot strand a completion permanently.
func (api *StreamingAPI) requeueUnnotifiedCompletions(sessionID string) {
	for _, agent := range api.bgAgentRegistry.GetAll(sessionID) {
		if agent == nil {
			continue
		}
		snap := agent.GetSnapshot()
		if snap.Status != BGAgentCompleted && snap.Status != BGAgentFailed {
			continue
		}
		agent.mu.RLock()
		notifiedOrInFlight := agent.notified || agent.notificationInFlight
		agent.mu.RUnlock()
		if notifiedOrInFlight {
			continue
		}
		api.queuePendingCompletion(sessionID, snap.ID)
	}
}

// backgroundCompletionLoop listens for background agent completions and triggers synthetic turns
func (api *StreamingAPI) backgroundCompletionLoop(sessionID string) {
	ch := api.bgAgentRegistry.GetNotificationChannel(sessionID)
	log.Printf("[BG AGENT] Started completion loop for session %s", sessionID)
	defer func() {
		api.completionLoopStartedMu.Lock()
		delete(api.completionLoopStarted, sessionID)
		api.completionLoopStartedMu.Unlock()
		log.Printf("[BG AGENT] Completion loop ended for session %s", sessionID)
	}()

	for agentID := range ch {
		if api.autoNotificationSessionUnreachable(sessionID) {
			log.Printf("[BG AGENT] Session %s is unreachable, dropping completion for agent %s", sessionID, agentID)
			continue
		}
		if api.isSessionBusyForAutoNotification(sessionID) {
			// Session is busy. A CLI coding agent can still receive the completion
			// mid-turn via live steering — prefer that, since the busy session may
			// be running the very workflow whose completion it is waiting on and so
			// may never reach the idle window a synthetic turn needs.
			if api.canSteerSession(sessionID) && api.steerBackgroundAgentCompletion(sessionID, agentID) {
				continue
			}
			// Not steerable (or steer failed) — queue the completion and arm the
			// retry backstop so it still drains even if no further turn fires the
			// post-turn drain.
			api.queuePendingCompletion(sessionID, agentID)
			api.schedulePendingCompletionRetry(sessionID)
			log.Printf("[BG AGENT] Session %s busy, queued completion for agent %s", sessionID, agentID)
		} else {
			api.processBackgroundAgentCompletion(sessionID, agentID)
		}
	}
}

func (api *StreamingAPI) processBatchedBackgroundAgentStarts(sessionID string, agentIDs []string) {
	api.autoNotificationMu.Lock()
	defer api.autoNotificationMu.Unlock()
	if api.autoNotificationSessionUnreachable(sessionID) {
		return
	}
	if api.isSessionBusyForAutoNotification(sessionID) {
		api.queuePendingStartNotifications(sessionID, agentIDs)
		api.schedulePendingStartNotificationRetry(sessionID)
		return
	}
	api.processBatchedBackgroundAgentStartsLocked(sessionID, agentIDs)
}

func (api *StreamingAPI) processBatchedBackgroundAgentStartsLocked(sessionID string, agentIDs []string) {
	if len(agentIDs) == 0 || api.bgAgentRegistry == nil {
		return
	}

	var parts []string
	var emittedIDs []string
	var agentRefs []*BackgroundAgent
	for _, agentID := range agentIDs {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			continue
		}
		agent := api.bgAgentRegistry.Get(sessionID, agentID)
		if agent == nil {
			continue
		}
		if !agent.MarkStartNotified() {
			continue
		}
		snap := agent.GetSnapshot()
		if isTerminalBackgroundAgentStatus(snap.Status) {
			agent.mu.Lock()
			agent.startNotified = false
			agent.mu.Unlock()
			continue
		}
		parts = append(parts, backgroundAgentStartNotificationPart(snap))
		emittedIDs = append(emittedIDs, agentID)
		agentRefs = append(agentRefs, agent)
	}
	if len(parts) == 0 {
		return
	}

	syntheticMsg := buildBackgroundAgentStartSyntheticMessage(sessionID, parts)
	if strings.HasPrefix(sessionID, "bot-") {
		syntheticMsg += "\n\n---\nReply with ONE short status line (target <=150 characters) that says the background work started. Do not ask the user a follow-up question."
	}

	for _, agentID := range emittedIDs {
		api.emitBackgroundAgentEvent(sessionID, agentID, "synthetic_turn_ready", map[string]interface{}{
			"message":  "Background work started. The main agent will be notified.",
			"agent_id": agentID,
			"status":   "started",
		})
	}
	if !api.executeSyntheticTurn(sessionID, syntheticMsg) && !api.autoNotificationSessionUnreachable(sessionID) {
		for _, agent := range agentRefs {
			agent.mu.Lock()
			agent.startNotified = false
			agent.mu.Unlock()
		}
		api.queuePendingStartNotifications(sessionID, emittedIDs)
		api.schedulePendingStartNotificationRetry(sessionID)
	}
}

func backgroundAgentStartNotificationPart(snap BackgroundAgentSnapshot) string {
	label := backgroundAgentStartLabel(snap)
	contextInfo := backgroundAgentStartContext(snap)
	name := strings.TrimSpace(snap.Name)
	if label == "Step" {
		name = strings.TrimPrefix(name, "Step -> ")
	}
	if name == "" {
		name = label
	}
	return fmt.Sprintf("- %s: %s%s", label, name, contextInfo)
}

func buildBackgroundAgentStartSyntheticMessage(_ string, parts []string) string {
	// Keep the message compact so cursor-cli's tmux paste-compression heuristic
	// renders it inline rather than as "[Pasted text +N lines]".
	// Completion will arrive as a separate AUTO-NOTIFICATION; the agent may call
	// query_step to inspect live progress in the meantime.
	const trailer = "Ack only. No tools; wait."
	if len(parts) == 1 {
		return fmt.Sprintf("[AUTO-NOTIFICATION] Started: %s\n%s", strings.TrimPrefix(parts[0], "- "), trailer)
	}
	return fmt.Sprintf("[AUTO-NOTIFICATION] Started:\n%s\n%s", strings.Join(parts, "\n"), trailer)
}

func backgroundAgentStartLabel(snap BackgroundAgentSnapshot) string {
	kind := strings.TrimSpace(snap.Kind)
	if snap.Metadata != nil {
		if executionType := strings.TrimSpace(snap.Metadata["execution_type"]); executionType == "message-sequence-item" {
			return "Message sequence item"
		}
		if stepID := strings.TrimSpace(snap.Metadata["step_id"]); stepID != "" {
			return "Step"
		}
		if typ := strings.TrimSpace(snap.Metadata["type"]); typ == "workflow_run" {
			return "Run"
		}
	}
	switch {
	case strings.Contains(kind, "sub_agent"):
		return "Sub-agent"
	case strings.Contains(kind, "delegation"):
		return "Background sub-agent"
	case strings.Contains(kind, "message_sequence_item"):
		return "Message sequence item"
	case strings.Contains(kind, "workflow"):
		return "Run"
	case strings.Contains(kind, "route"):
		return "Routing task"
	default:
		return "Background agent"
	}
}

func backgroundAgentStartContext(snap BackgroundAgentSnapshot) string {
	if snap.Metadata == nil {
		return ""
	}
	var fields []string
	if workflowPath := strings.TrimSpace(snap.Metadata["workflow_path"]); workflowPath != "" {
		fields = append(fields, "space="+autoNotificationDisplayPath(workflowPath))
	}
	if groupName := strings.TrimSpace(snap.Metadata["group_name"]); groupName != "" {
		fields = append(fields, "group="+groupName)
	}
	if stepID := strings.TrimSpace(snap.Metadata["step_id"]); stepID != "" {
		fields = append(fields, "step="+stepID)
	}
	if itemID := strings.TrimSpace(snap.Metadata["item_id"]); itemID != "" {
		itemContext := "item=" + itemID
		if itemType := strings.TrimSpace(snap.Metadata["item_type"]); itemType != "" {
			itemContext += "/" + itemType
		}
		fields = append(fields, itemContext)
	}
	if len(fields) == 0 {
		return ""
	}
	return " [" + strings.Join(fields, ", ") + "]"
}

func autoNotificationDisplayPath(value string) string {
	path := strings.TrimSpace(value)
	path = strings.TrimPrefix(path, "Workflow/")
	path = strings.TrimPrefix(path, "workflow/")
	path = strings.TrimPrefix(path, "/Workflow/")
	path = strings.TrimPrefix(path, "/workflow/")
	return path
}

// processBatchedBackgroundAgentCompletions builds a single [AUTO-NOTIFICATION] message for one or more
// completed agents and fires ONE synthetic turn. Subsequent drained completions are chained via
// the synthetic turn's own defer, avoiding concurrent StreamWithEvents calls.
func (api *StreamingAPI) processBatchedBackgroundAgentCompletions(sessionID string, agentIDs []string) {
	if len(agentIDs) == 0 {
		return
	}
	if api.autoNotificationSessionUnreachable(sessionID) {
		log.Printf("[BG AGENT] Session %s is stopped/inactive, skipping %d batched completion(s)", sessionID, len(agentIDs))
		return
	}

	// Single completion: use the normal individual path (simpler message).
	if len(agentIDs) == 1 {
		api.processBackgroundAgentCompletion(sessionID, agentIDs[0])
		return
	}

	// Multiple completions: build a batched [AUTO-NOTIFICATION] message.
	// agentRefs tracks the BackgroundAgent pointers for agents we include in the
	// batch so we can mark them notified=true only after the synthetic turn
	// actually dispatches (notified-before-executeSyntheticTurn fix).
	var parts []string
	var emittedIDs []string
	var agentRefs []*BackgroundAgent
	var batchWorkflowRunDirective string // set once if any completed part is a workflow run
	for _, agentID := range agentIDs {
		agent := api.bgAgentRegistry.Get(sessionID, agentID)
		if agent == nil {
			continue
		}

		// Snapshot and canceled check BEFORE setting notified=true
		// (bg-agent-notified-before-canceled-check fix: match single-agent path).
		snap := agent.GetSnapshot()
		if snap.Status == BGAgentCanceled {
			continue
		}

		if !agent.beginCompletionNotification() {
			continue
		}

		var resultText string
		if snap.Status == BGAgentCompleted {
			resultText = compactScheduledAutoNotificationResult(sessionID, snap, snap.Result)
		} else if snap.Status == BGAgentFailed {
			resultText = "Error: " + compactScheduledAutoNotificationResult(sessionID, snap, snap.Error)
		} else {
			resultText = fmt.Sprintf("Status: %s", snap.Status)
		}
		workshopMode := ""
		isLockCode := false
		isLockLearnings := false
		lockCodeConsecutiveFailures := 0
		lockCodeNeedsReview := false
		if snap.Metadata != nil {
			workshopMode = snap.Metadata["workshop_mode"]
			isLockCode = snap.Metadata["lock_code"] == "true"
			isLockLearnings = snap.Metadata["lock_learnings"] == "true"
			if v := snap.Metadata["lock_code_consecutive_failures"]; v != "" {
				if n, perr := strconv.Atoi(v); perr == nil {
					lockCodeConsecutiveFailures = n
				}
			}
			lockCodeNeedsReview = snap.Metadata["lock_code_needs_review"] == "true"
		}
		actionHint := buildWorkshopActionHint(workshopMode, isLockCode, isLockLearnings, lockCodeConsecutiveFailures, lockCodeNeedsReview, snap.Status == BGAgentFailed)
		batchContext := autoNotificationBracketContext(snap.Metadata)
		parts = append(parts, fmt.Sprintf("- **%s**%s: %s\n  Result: %s%s", strings.TrimSpace(snap.Name), batchContext, snap.Status, resultText, actionHint))
		if batchWorkflowRunDirective == "" {
			batchWorkflowRunDirective = workflowRunCompletionDirective(snap)
		}
		emittedIDs = append(emittedIDs, agentID)
		agentRefs = append(agentRefs, agent)
	}

	if len(parts) == 0 {
		return
	}

	syntheticMsg := fmt.Sprintf("[AUTO-NOTIFICATION] Multiple step completions:\n%s%s", strings.Join(parts, "\n"), batchWorkflowRunDirective)
	if strings.HasPrefix(sessionID, "bot-") {
		syntheticMsg += botAutoNotificationProgressDirective(sessionID, api.isFinalBotAutoNotification(sessionID))
	}

	// Emit synthetic_turn_ready event for each agent
	for _, agentID := range emittedIDs {
		api.emitBackgroundAgentEvent(sessionID, agentID, "synthetic_turn_ready", map[string]interface{}{
			"message":  "Background agents completed. The main agent will process the results.",
			"agent_id": agentID,
			"status":   "completed",
		})
	}

	// Mark notified=true only for agents whose turn was actually dispatched.
	dispatched := api.executeSyntheticTurn(sessionID, syntheticMsg)
	for _, a := range agentRefs {
		a.finishCompletionNotification(dispatched)
	}
	if !dispatched && !api.autoNotificationSessionUnreachable(sessionID) {
		// Dispatch failed but the session is still reachable. Leave every batched
		// agent notified=false and arm the retry backstop so they are redelivered
		// rather than dropped (no-stored-agent / stream-error drop fix).
		for _, agentID := range emittedIDs {
			api.queuePendingCompletion(sessionID, agentID)
		}
		api.schedulePendingCompletionRetry(sessionID)
		log.Printf("[BG AGENT] Batched synthetic turn for session %s did not dispatch %d agent(s) — queued for retry", sessionID, len(emittedIDs))
	}
}

// buildWorkshopActionHint returns a mode-specific instruction appended to AUTO-NOTIFICATION messages
// so the agent knows what to do next. Most success/failure cases are handled by the system prompt;
// this function only adds extra guidance for cases where the engine has silently degraded behavior
// the orchestrator wouldn't otherwise know about — most notably fast-path failures on locked steps,
// where the fix loop is disabled and the step gets exactly one shot at running the saved main.py.
func buildWorkshopActionHint(workshopMode string, isLockCode, isLockLearnings bool, lockCodeConsecutiveFailures int, lockCodeNeedsReview, failed bool) string {
	if !failed {
		return ""
	}
	// Pattern hint shared by both locked-step branches: a streak of locked failures is
	// strong evidence the lock itself is wrong (script no longer matches the site/API),
	// not that each individual run is independently environmental.
	streakHint := ""
	if lockCodeNeedsReview || lockCodeConsecutiveFailures >= 3 {
		streakHint = fmt.Sprintf(
			"\n\n**Pattern signal:** the locked main.py has now failed %d times in a row "+
				"(`script_metadata.json.lock_code_stats.consecutive_failures=%d`, `needs_review=%v`). "+
				"At this point the lock is likely wrong — a single environmental failure is plausible, "+
				"three in a row usually means the saved script no longer matches the site/API. "+
				"Strongly consider clearing `lock_code` and patching the script rather than treating "+
				"this as one more transient failure.",
			lockCodeConsecutiveFailures, lockCodeConsecutiveFailures, lockCodeNeedsReview)
	}
	if isLockCode && isLockLearnings {
		return "\n\n[LOCKED STEP FAILED] This step is locked " +
			"(`lock_code=true`, `lock_learnings=true`) and ran on the fast path, " +
			"so only the saved main.py executed — no fix loop, no LLM repair attempt. " +
			"Investigate the failure: read the run folder " +
			"(`step_*_status.json`, `scripted_fast_path.json`, screenshots, downloaded files) " +
			"and decide between two recovery paths:\n" +
			"  1. **Fix main.py** — if there's a real bug in the script (these accumulate over time as " +
			"sites and APIs change), clear `lock_code` via `update_step_config` and update the script. " +
			"Use `review_step_code` or rewrite directly based on what you find.\n" +
			"  2. **Re-run with `fast_path_only=false`** — calls `execute_step` again with the fast path " +
			"disabled so the full agentic path engages. The LLM will drive tools directly, can repair " +
			"the run live, and (if `lock_code` is cleared) save an updated main.py back to learnings. " +
			"Good first move when you're not sure whether it's a script bug or environmental.\n" +
			"If after inspection it's clearly environmental (bad creds, MFA prompt, captcha) and the " +
			"script is fine, surface that to the user instead of touching the code." +
			streakHint
	}
	if isLockCode {
		return "\n\n[CODE-LOCKED FAILURE] `lock_code=true` so the fix loop is disabled and the saved " +
			"main.py is frozen. Inspect the run folder, then either clear `lock_code` and fix the " +
			"script, or re-run with `fast_path_only=false` to engage agentic mode for this run." +
			streakHint
	}
	return ""
}

// processBackgroundAgentCompletion injects a synthetic message and triggers a new main agent turn
func (api *StreamingAPI) processBackgroundAgentCompletion(sessionID, agentID string) {
	if api.autoNotificationSessionUnreachable(sessionID) {
		log.Printf("[BG AGENT] Session %s is stopped/inactive, skipping completion for agent %s", sessionID, agentID)
		return
	}
	agent := api.bgAgentRegistry.Get(sessionID, agentID)
	if agent == nil {
		log.Printf("[BG AGENT] Warning: agent %s not found for completion processing", agentID)
		return
	}

	// Snapshot once; reused below to avoid a second lock acquisition that could
	// observe inconsistent state (LOW bug fix: single-item-two-snapshot).
	snap := agent.GetSnapshot()
	if snap.Status == BGAgentCanceled {
		log.Printf("[BG AGENT] Agent %s for session %s was canceled, suppressing synthetic turn", agentID, sessionID)
		return
	}

	// Claim this completion across both direct and retry loops. The claim is
	// released if dispatch fails so the retry backstop can deliver it later.
	if !agent.beginCompletionNotification() {
		return
	}

	syntheticMsg := api.buildAutoNotificationMessage(sessionID, snap)

	// NOTE: Don't inject syntheticMsg into conversation history here.
	// handleQuery will add it via StreamWithEvents when the synthetic turn runs.

	// Emit synthetic_turn_ready event so frontend shows amber banner before the turn fires
	statusLabel := "completed"
	if snap.Status == BGAgentFailed {
		statusLabel = "failed"
	}
	api.emitBackgroundAgentEvent(sessionID, agentID, "synthetic_turn_ready", map[string]interface{}{
		"message":  fmt.Sprintf("Background agent '%s' %s. The main agent will process the results.", snap.Name, statusLabel),
		"agent_id": snap.ID,
		"name":     snap.Name,
		"status":   string(snap.Status),
	})

	// Trigger a synthetic turn using the stored QueryRequest.
	// Set notified=true only when the turn was actually dispatched.
	dispatched := api.executeSyntheticTurn(sessionID, syntheticMsg)
	agent.finishCompletionNotification(dispatched)
	if !dispatched && !api.autoNotificationSessionUnreachable(sessionID) {
		// Dispatch failed but the session is still reachable (no stored agent yet,
		// or StreamWithEvents errored). Leave notified=false and arm the retry
		// backstop so requeueUnnotifiedCompletions redelivers this completion
		// instead of dropping it (no-stored-agent / stream-error drop fix).
		api.queuePendingCompletion(sessionID, agentID)
		api.schedulePendingCompletionRetry(sessionID)
		log.Printf("[BG AGENT] Synthetic turn for session %s did not dispatch agent %s — queued for retry", sessionID, agentID)
	}
}

// workflowRunBackupDirective returns the directive that backs up an interactive
// workflow run/step after it completes, or "" when this completion is not a
// workflow run. This is the interactive arm of post-run backup: for scheduled
// runs the Pulse pass (scheduler.go runPostRunMonitor, step 4) owns backup.
// Both arms share ONE backup contract — same default (zero-config local git),
// same source-hash skip — so a run backed up by one is recognized as current by
// the other (no double push). Keep this text in sync with Pulse's backup step.
func workflowRunBackupDirective(snap BackgroundAgentSnapshot) string {
	if snap.Status != BGAgentCompleted || snap.Metadata == nil {
		return ""
	}
	if snap.Kind != "workflow_run_tool" && snap.Metadata["type"] != "workflow_run" {
		return ""
	}
	return "\n\nThe run is complete - now back up this workflow. Call get_reference_doc(kind=\"backup-strategy\"), read workflow.json.backup, and use it as the backup contract. Perform backup and all Git commands directly in this parent workflow turn. Never delegate them through run_in_background, call_generic_agent, a reviewer, or another sub-agent: delegated agents intentionally cannot write the workflow .git directory. If backup is enabled, perform the configured destinations (git/github, object store, HuggingFace, etc.). If backup is missing or disabled, do not silently skip: set it up with the zero-config local-git default (a local git repo needs no credentials) and back up. Skip the push only when backup/status.json shows the current source is already backed up (unchanged source hash) — i.e. a Pulse pass or an earlier turn already captured this state. Always write backup/status.json with state, last attempt/success timestamps, destination results, errors, and the current source hash; do not write operational backup status into workflow.json."
}

func workflowRunGoalAlignmentDirective(snap BackgroundAgentSnapshot) string {
	if snap.Status != BGAgentCompleted || snap.Metadata == nil {
		return ""
	}
	if snap.Kind != "workflow_run_tool" && snap.Metadata["type"] != "workflow_run" {
		return ""
	}

	workflowPath := strings.TrimSpace(snap.Metadata["workflow_path"])
	groupName := strings.TrimSpace(snap.Metadata["group_name"])
	stepID := strings.TrimSpace(snap.Metadata["step_id"])
	runEvidencePath := "the latest run folder"
	if workflowPath != "" && groupName != "" {
		runEvidencePath = fmt.Sprintf("`%s/runs/iteration-0/%s/`", workflowPath, groupName)
		if stepID != "" {
			runEvidencePath = fmt.Sprintf("`%s/runs/iteration-0/%s/execution/%s/`", workflowPath, groupName, stepID)
		}
	}
	workflowRef := strings.TrimSpace(workflowPath)
	if workflowRef == "" {
		workflowRef = strings.TrimSpace(snap.Name)
	}
	if workflowRef == "" {
		workflowRef = "this workflow run"
	}
	stepNote := ""
	if stepID != "" {
		stepNote = fmt.Sprintf(" This was a single-step run for `%s`, so distinguish step evidence from full-workflow evidence.", stepID)
	}

	return fmt.Sprintf("\n\nAfter backup, do org goal alignment for this run. If `pulse/goals.html` exists, call get_reference_doc(kind=\"org-goals\"), read `pulse/goals.html`, and compare `%s` against any goals whose contributing workflows name this workflow. Use concrete evidence from %s, `builder/improve.html`, `reports/`, and `db/db.sqlite`. In your reply include a short `Org goal alignment` section: goal, status (`on-track`, `at-risk`, `off-track`, or `unknown`), evidence path, gap, and next action.%s If no goal names this workflow, classify it as supporting/maintenance or unaligned. Update `pulse/goals.html` only when this run provides concrete new evidence that changes the scorecard; load get_reference_doc(kind=\"org-html\") first and preserve goal history. Do not invent proxy metrics.", workflowRef, runEvidencePath, stepNote)
}

func workflowRunCompletionDirective(snap BackgroundAgentSnapshot) string {
	return workflowRunBackupDirective(snap) + workflowRunGoalAlignmentDirective(snap)
}

// buildAutoNotificationMessage formats the [AUTO-NOTIFICATION] user message for a
// finished background agent. It is pure formatting (no dedup / no side effects) so
// both the synthetic-turn path (idle session) and the live-steer path (busy
// steerable CLI agent) emit byte-identical text.
func (api *StreamingAPI) buildAutoNotificationMessage(sessionID string, snap BackgroundAgentSnapshot) string {
	var resultText string
	if snap.Status == BGAgentCompleted {
		resultText = compactScheduledAutoNotificationResult(sessionID, snap, snap.Result)
	} else if snap.Status == BGAgentFailed {
		resultText = "Error: " + compactScheduledAutoNotificationResult(sessionID, snap, snap.Error)
	} else {
		resultText = fmt.Sprintf("Status: %s", snap.Status)
	}

	// Append mode-specific action hint so the agent knows what to do next.
	workshopMode := ""
	isLockCode := false
	isLockLearnings := false
	lockCodeConsecutiveFailures := 0
	lockCodeNeedsReview := false
	if snap.Metadata != nil {
		workshopMode = snap.Metadata["workshop_mode"]
		isLockCode = snap.Metadata["lock_code"] == "true"
		isLockLearnings = snap.Metadata["lock_learnings"] == "true"
		if v := snap.Metadata["lock_code_consecutive_failures"]; v != "" {
			if n, perr := strconv.Atoi(v); perr == nil {
				lockCodeConsecutiveFailures = n
			}
		}
		lockCodeNeedsReview = snap.Metadata["lock_code_needs_review"] == "true"
	}
	isFailed := snap.Status == BGAgentFailed
	actionHint := buildWorkshopActionHint(workshopMode, isLockCode, isLockLearnings, lockCodeConsecutiveFailures, lockCodeNeedsReview, isFailed)

	// Iteration and group go inline alongside id/status to keep the header
	// to a single line — cursor-cli's tmux paste-compression collapses any
	// multi-line user-message into a "[Pasted text +N lines]" placeholder,
	// which hides the actual notification text from the operator.
	contextInfo := autoNotificationInlineContext(snap.Metadata)
	syntheticMsg := fmt.Sprintf(
		"[AUTO-NOTIFICATION] Agent '%s' completed — status=%s%s.\nResult: %s%s%s",
		strings.TrimSpace(snap.Name), snap.Status, contextInfo, resultText, actionHint, workflowRunCompletionDirective(snap))

	// Bot connector sessions (slack / whatsapp / discord / telegram / etc.): the
	// builder's reply is forwarded verbatim to a chat thread, so a faithful echo
	// of the full sub-agent result blows up the conversation. Append a brevity
	// directive so the builder still ingests the full result above (full context
	// for its own reasoning) but replies to the user with a single short status
	// line. Web / desktop sessions intentionally keep the verbose progressive
	// update — that long reply renders fine in a side panel, not in chat.
	// Session ID format is `bot-<platform>--<uuid>` (see newBotSessionID).
	if strings.HasPrefix(sessionID, "bot-") {
		syntheticMsg += botAutoNotificationProgressDirective(sessionID, api.isFinalBotAutoNotification(sessionID))
	}

	return syntheticMsg
}

const scheduledAutoNotificationResultMaxRunes = 4000

func compactScheduledAutoNotificationResult(sessionID string, snap BackgroundAgentSnapshot, result string) string {
	if !isScheduledSession(sessionID) {
		return result
	}
	runes := []rune(result)
	if len(runes) <= scheduledAutoNotificationResultMaxRunes {
		return result
	}

	stepID := ""
	if snap.Metadata != nil {
		stepID = strings.TrimSpace(snap.Metadata["step_id"])
	}
	inspectHint := fmt.Sprintf("Inspect execution %q or its persisted run artifacts for the complete result.", snap.ID)
	if stepID != "" {
		inspectHint = fmt.Sprintf("Use query_step(step_id=%q, execution_id=%q) or inspect its persisted run artifacts for the complete result.", stepID, snap.ID)
	}
	// Large coding-agent results are often full terminal/tool transcripts rather
	// than final prose. Pasting their prefix into the parent CLI exposes escaped
	// JSON, partial TUI frames, and wrapped command arguments. Keep the parent
	// pane readable and point it at the authoritative execution instead.
	return fmt.Sprintf(
		"Detailed result omitted from this scheduled notification because it exceeds %d characters. %s",
		scheduledAutoNotificationResultMaxRunes,
		inspectHint,
	)
}

func autoNotificationInlineContext(meta map[string]string) string {
	if meta == nil {
		return ""
	}
	var fields []string
	if iter := strings.TrimSpace(meta["iteration"]); iter != "" {
		fields = append(fields, "iter="+iter)
	}
	if groupName := strings.TrimSpace(meta["group_name"]); groupName != "" {
		fields = append(fields, "group="+groupName)
	}
	if stepID := strings.TrimSpace(meta["step_id"]); stepID != "" {
		fields = append(fields, "step="+stepID)
	}
	if itemID := strings.TrimSpace(meta["item_id"]); itemID != "" {
		itemContext := "item=" + itemID
		if itemType := strings.TrimSpace(meta["item_type"]); itemType != "" {
			itemContext += "/" + itemType
		}
		fields = append(fields, itemContext)
	}
	if len(fields) == 0 {
		return ""
	}
	return ", " + strings.Join(fields, ", ")
}

func autoNotificationBracketContext(meta map[string]string) string {
	inline := strings.TrimPrefix(autoNotificationInlineContext(meta), ", ")
	if inline == "" {
		return ""
	}
	return " [" + inline + "]"
}

// steerBackgroundAgentCompletion delivers a finished background agent's
// [AUTO-NOTIFICATION] to a busy-but-steerable CLI coding agent by injecting it
// into the turn that is already running (the same path live user chat takes via
// handleLiveInputMessage), instead of starting a fresh synthetic turn.
//
// This exists because the synthetic-turn path (processBackgroundAgentCompletion ->
// executeSyntheticTurn) can only fire when the session is idle. For a CLI coding
// agent the session is frequently busy — often running the very workflow whose
// completion it is waiting on — so the synthetic turn never gets an idle window,
// the notification queues, the session goes stale, and the completion is dropped.
// A steerable agent can always receive the message mid-turn, so prefer that.
//
// Returns true when the message was handed to the running agent (caller should
// NOT queue). Returns false on any failure so the caller falls back to the
// existing queue + drain-on-idle backstop.
func (api *StreamingAPI) steerBackgroundAgentCompletion(sessionID, agentID string) bool {
	if api.autoNotificationSessionUnreachable(sessionID) {
		return false
	}

	api.runningAgentsMux.RLock()
	runningAgent, exists := api.runningAgents[sessionID]
	api.runningAgentsMux.RUnlock()
	if !exists || runningAgent == nil {
		return false
	}

	if api.bgAgentRegistry == nil {
		return false
	}
	agent := api.bgAgentRegistry.Get(sessionID, agentID)
	if agent == nil {
		return false
	}

	snap := agent.GetSnapshot()
	if snap.Status != BGAgentCompleted && snap.Status != BGAgentFailed {
		return false
	}
	if shouldDeferBackgroundCompletionToSyntheticTurn(snap) {
		log.Printf("[BG AGENT] Deferring plain delegation completion for agent %s in session %s to a separate synthetic turn", agentID, sessionID)
		return false
	}

	// Atomically claim delivery. Multiple completion/retry loops can reach this
	// function at once; checking notified and setting it only after I/O allowed
	// duplicate [AUTO-NOTIFICATION] messages into the same tmux pane.
	if !agent.beginCompletionNotification() {
		return true // already delivered or another goroutine owns this delivery
	}
	delivered := false
	defer func() { agent.finishCompletionNotification(delivered) }()

	msg := api.buildAutoNotificationMessage(sessionID, snap)

	inputCtx, cancel := context.WithTimeout(context.Background(), liveCodingAgentInputTimeout)
	defer cancel()
	delivery, err := runningAgent.DeliverUserMessage(inputCtx, mcpagent.UserMessageDeliveryRequest{
		SessionID: sessionID,
		Message:   msg,
		Intent:    mcpagent.UserMessageDeliveryIntentLiveInput,
	})
	if err != nil {
		log.Printf("[BG AGENT] Live steer delivery failed for session %s agent %s: %v — falling back to queue", sessionID, agentID, err)
		return false
	}

	provider := string(delivery.Provider)
	if provider == "" {
		provider = string(runningAgent.GetProvider())
	}
	deliveryStatus := string(delivery.DeliveryStatus)
	if deliveryStatus == "" {
		deliveryStatus = string(mcpagent.UserMessageDeliveryStatusQueuedForInjection)
	}

	// steer-bg-agent-completion-queued-injection-loss fix:
	// Only mark notified=true when the message was definitively sent to the CLI
	// (SentToCLI). For QueuedForInjection the foreground turn may exit before
	// injecting it, which would orphan the notification permanently. Fall back
	// to the queue path so the completion is reliably re-delivered.
	if delivery.DeliveryStatus != mcpagent.UserMessageDeliveryStatusSentToCLI {
		log.Printf("[BG AGENT] Steer for agent %s in session %s returned status=%s — falling back to queue", agentID, sessionID, deliveryStatus)
		api.recordLiveCodingAgentUserMessage(sessionID, msg, provider, newSteerMessageID(), deliveryStatus)
		return false
	}

	// Commit the dedup only after a confirmed SentToCLI hand-off.
	delivered = true

	api.recordLiveCodingAgentUserMessage(sessionID, msg, provider, newSteerMessageID(), deliveryStatus)
	api.emitBackgroundAgentEvent(sessionID, agentID, "auto_notification_steered", map[string]interface{}{
		"agent_id": snap.ID,
		"name":     snap.Name,
		"status":   string(snap.Status),
		"provider": provider,
	})
	log.Printf("[BG AGENT] Steered completion for agent %s into busy session %s (provider=%s status=%s)", agentID, sessionID, provider, deliveryStatus)
	return true
}

func shouldDeferBackgroundCompletionToSyntheticTurn(snap BackgroundAgentSnapshot) bool {
	return strings.EqualFold(strings.TrimSpace(snap.Kind), "delegation")
}

func (api *StreamingAPI) isFinalBotAutoNotification(sessionID string) bool {
	if api.botManager == nil || !strings.HasPrefix(sessionID, "bot-") {
		return false
	}
	// Registrations are usually removed before the synthetic turn is injected.
	// Treat 0 or 1 remaining mirrored sessions as the terminal notification so
	// we stop adding the progress-only bot directive and let Run mode respond
	// from its normal prompt/context.
	return api.botManager.PendingWorkflowCount(sessionID) <= 1
}

func botAutoNotificationProgressDirective(sessionID string, final bool) string {
	if final {
		return ""
	}
	switch botPlatformFromSessionID(sessionID) {
	case "slack":
		return "\n\n---\nSlack progress update. Reply with one <=150-char mrkdwn line: \"Step update (<name>): <status>\". Use the agent/completion name. Do not start with \"Status: completed\" or quote/summarize Result."
	case "whatsapp":
		return "\n\n---\nWhatsApp progress update. Reply with one <=150-char plain-text line: \"Step update (<name>): <status>\". Use the agent/completion name. Do not start with \"Status: completed\" or quote/summarize Result."
	default:
		return "\n\n---\nBot progress update. Reply with one <=150-char line: \"Step update (<name>): <status>\". Use the agent/completion name. Do not start with \"Status: completed\" or quote/summarize Result."
	}
}

func botPlatformFromSessionID(sessionID string) string {
	rest := strings.TrimPrefix(strings.TrimSpace(sessionID), "bot-")
	if rest == sessionID {
		return ""
	}
	platform, _, ok := strings.Cut(rest, "--")
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(platform))
}

// executeSyntheticTurn drives the stored agent directly with a synthetic message.
// Instead of creating an internal HTTP request and re-building the entire agent/tools/history,
// it reuses the agent stored after the last plan-mode turn via StreamWithEvents().
// This is called synchronously from processBackgroundAgentCompletion — it sets session busy
// before spawning the goroutine, preventing concurrent synthetic turns.
// Returns true when the synthetic turn was successfully dispatched (goroutine spawned),
// false when the session has no stored agent or is unreachable.
func (api *StreamingAPI) executeSyntheticTurn(sessionID, syntheticMsg string) bool {
	if api.autoNotificationSessionUnreachable(sessionID) {
		log.Printf("[BG AGENT] Session %s is stopped/inactive, suppressing synthetic turn", sessionID)
		return false
	}
	if api.botManager != nil {
		api.botManager.PrepareSyntheticTurn(sessionID)
	}
	// Get stored agent for this session
	api.sessionAgentsMux.RLock()
	llmAgent, ok := api.sessionAgents[sessionID]
	api.sessionAgentsMux.RUnlock()

	if !ok || llmAgent == nil {
		log.Printf("[BG AGENT] No stored agent for session %s, cannot trigger synthetic turn", sessionID)
		return false
	}

	// Synthetic turns share the same full-turn lane as user-created turns. This
	// prevents an old completion turn and a resumed user turn from concurrently
	// mutating conversation history, running-agent maps, and terminal state.
	releaseInputLane := api.lockSessionInputLane(sessionID)
	if api.autoNotificationSessionUnreachable(sessionID) {
		releaseInputLane()
		return false
	}

	// Get stored query request for user ID context
	api.lastQueryMu.RLock()
	req, hasReq := api.lastQueryRequests[sessionID]
	api.lastQueryMu.RUnlock()

	// Set session busy synchronously BEFORE spawning goroutine
	// This prevents concurrent synthetic turns from the completion listener
	api.setSessionBusy(sessionID, true)

	// Mark as synthetic turn so frontend doesn't block user input
	api.setSyntheticTurn(sessionID, true)

	// Update session status to running
	api.updateSessionStatus(sessionID, "running")

	// Create cancellable context for this synthetic turn
	agentCtx, agentCancel := context.WithCancel(context.Background())

	// Inject user ID into context
	if hasReq && req.userID != "" {
		agentCtx = context.WithValue(agentCtx, common.UserIDKey, req.userID)
	}
	if hasReq {
		if dest := notificationDestinationFromQuery(req, req.userID); dest != nil {
			agentCtx = context.WithValue(agentCtx, virtualtools.BotNotificationDestinationKey, dest)
		}
	}

	// Store cancel function so handleStopSession can cancel this turn
	api.agentCancelMux.Lock()
	api.agentCancelFuncs[sessionID] = agentCancel
	api.agentCancelMux.Unlock()

	log.Printf("[BG AGENT] Executing synthetic turn for session %s via stored agent", sessionID)

	// Start the stream SYNCHRONOUSLY so the bool we return reflects whether the
	// turn actually dispatched. Previously StreamWithEvents ran inside the goroutine
	// and we returned true on spawn; a stream error there left the caller having
	// already committed notified=true, so the completion was permanently lost
	// (notified-only-after-stream-start fix). On error we undo the busy/synthetic
	// setup and return false; the caller leaves notified=false and arms the retry
	// backstop so requeueUnnotifiedCompletions redelivers it.
	textChan, err := llmAgent.StreamWithEvents(agentCtx, syntheticMsg)
	if err != nil {
		log.Printf("[BG AGENT] StreamWithEvents error for synthetic turn on session %s: %v", sessionID, err)
		agentCancel()
		api.agentCancelMux.Lock()
		delete(api.agentCancelFuncs, sessionID)
		api.agentCancelMux.Unlock()
		api.setSyntheticTurn(sessionID, false)
		api.setSessionBusy(sessionID, false)
		api.updateSessionStatus(sessionID, "error")
		releaseInputLane()
		return false
	}

	go func() {
		defer func() {
			// Clean up cancel function
			api.agentCancelMux.Lock()
			delete(api.agentCancelFuncs, sessionID)
			api.agentCancelMux.Unlock()

			// Clear synthetic turn flag
			api.setSyntheticTurn(sessionID, false)

			// Clear session busy first so any later work sees the session as idle.
			api.setSessionBusy(sessionID, false)
			releaseInputLane()

			// If the session was explicitly stopped while this synthetic turn was running,
			// do not chain any queued completions. That would re-enter the stopped session.
			if api.autoNotificationSessionUnreachable(sessionID) {
				log.Printf("[BG AGENT] Session %s stopped/inactive after synthetic turn, skipping pending completion drain", sessionID)
				return
			}

			// Drain queued auto-notifications only for still-active sessions (batched
			// to avoid concurrent StreamWithEvents calls).
			api.drainPendingAutoNotificationsAfterTurn(sessionID)
		}()

		// Stream already started above; events flow through already-attached
		// EventObservers (in-memory + DB). Consume text chunks and save history.
		for range textChan {
			api.conversationMux.Lock()
			api.conversationHistory[sessionID] = llmAgent.GetHistory()
			api.conversationMux.Unlock()
		}

		// A stopped/canceled synthetic turn must not "complete" afterward, otherwise
		// it can resurrect the stored agent and reopen Playwright after Esc/stop.
		if agentCtx.Err() != nil || api.isSessionStoppedOrInactive(sessionID) {
			log.Printf("[BG AGENT] Synthetic turn aborted for session %s after stream end (ctx_err=%v stopped=%v)",
				sessionID, agentCtx.Err(), api.isSessionStoppedOrInactive(sessionID))
			return
		}

		// Final save of conversation history
		finalHistory := llmAgent.GetHistory()
		api.conversationMux.Lock()
		api.conversationHistory[sessionID] = finalHistory
		api.conversationMux.Unlock()
		log.Printf("[BG AGENT] Synthetic turn completed for session %s, history: %d messages", sessionID, len(finalHistory))

		// Persist conversation to builder/conversation/YYYY-MM-DD/ on disk (same as handleQuery defer)
		// Without this, auto-notification responses are only in memory and lost on restart.
		api.sessionWorkspaceMu.RLock()
		workflowPhaseFolder, hasFolderForSession := api.sessionWorkspaceFolders[sessionID]
		api.sessionWorkspaceMu.RUnlock()
		persistedHistory := cleanChatHistoryForPersistence(finalHistory)
		if hasFolderForSession && workflowPhaseFolder != "" && len(persistedHistory) > 0 {
			phaseID := ""
			if hasReq {
				phaseID = strings.TrimSpace(req.PhaseID)
			}
			logPath := workflowBuilderConversationLogPath(workflowPhaseFolder, sessionID, time.Now())
			var existing struct {
				PhaseID      string                   `json:"phase_id"`
				WorkshopMode string                   `json:"workshop_mode,omitempty"`
				Runtime      *ChatHistoryAgentRuntime `json:"runtime,omitempty"`
			}
			if existingContent, exists, err := readFileFromWorkspace(context.Background(), logPath); err == nil && exists {
				if json.Unmarshal([]byte(existingContent), &existing) == nil {
					if phaseID == "" {
						phaseID = strings.TrimSpace(existing.PhaseID)
					}
				} else {
					log.Printf("[BG AGENT] Failed to parse existing builder conversation metadata for %s", logPath)
				}
			} else if err != nil {
				log.Printf("[BG AGENT] Failed to read existing builder conversation metadata for %s: %v", logPath, err)
			}
			if phaseID == "" {
				phaseID = "workflow-builder"
			}
			chatRuntime := existing.Runtime
			if chatRuntime == nil {
				if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
					chatRuntime = api.captureChatHistoryAgentRuntime(sessionID, "", "", workflowPhaseFolder, underlyingAgent)
				}
			}
			workshopMode := strings.TrimSpace(existing.WorkshopMode)
			if chatRuntime != nil && chatRuntime.WorkshopMode != "" {
				workshopMode = chatRuntime.WorkshopMode
			}
			if chatRuntime != nil && chatRuntime.WorkshopMode == "" && workshopMode != "" {
				chatRuntime.WorkshopMode = workshopMode
			}
			currentUserID := "default"
			if hasReq && strings.TrimSpace(req.userID) != "" {
				currentUserID = strings.TrimSpace(req.userID)
			}
			restoredConversationPath := ""
			restoredConversationSessionID := ""
			if hasReq {
				restoredConversationPath = strings.TrimSpace(req.RestoredConversationPath)
				restoredConversationSessionID = strings.TrimSpace(req.RestoredConversationSessionID)
			}
			providerForPersist := ""
			if chatRuntime != nil {
				providerForPersist = chatRuntime.Provider
			}
			persistSessionID := sessionID
			persistedHistoryForDisk := persistedHistory
			if target, ok, err := api.resolveRestoredCodingConversationPersistTarget(
				currentUserID,
				sessionID,
				restoredConversationPath,
				restoredConversationSessionID,
				workflowPhaseFolder,
				providerForPersist,
				workshopMode,
			); err != nil {
				log.Printf("[BG AGENT] Failed to resolve restored coding-agent persistence target for %s: %v", sessionID, err)
			} else if ok && target != nil {
				persistSessionID = target.SessionID
				logPath = target.ConversationPath
				persistedHistoryForDisk = mergeRestoredChatHistory(target.History, persistedHistory)
				log.Printf("[BG AGENT] Continuing restored coding-agent conversation current_session=%s persisted_session=%s path=%s merged_messages=%d",
					sessionID, persistSessionID, logPath, len(persistedHistoryForDisk))
			}
			var uiEvents []events.Event
			if api.eventStore != nil {
				uiEvents = trimChatHistoryUIEvents(api.eventStore.GetAllEventsRaw(sessionID))
			}
			convData := map[string]interface{}{
				"session_id":           persistSessionID,
				"phase_id":             phaseID,
				"conversation_history": persistedHistoryForDisk,
				"updated_at":           time.Now().Format(time.RFC3339),
			}
			if workshopMode != "" {
				convData["workshop_mode"] = workshopMode
			}
			if chatRuntime != nil {
				convData["runtime"] = chatRuntime
			}
			if terminalSnapshots := api.captureChatHistoryTerminalSnapshots(sessionID, chatRuntime); len(terminalSnapshots) > 0 {
				convData["terminal_snapshots"] = terminalSnapshots
			}
			if len(uiEvents) > 0 {
				convData["ui_events"] = uiEvents
			}
			if convJSON, err := json.MarshalIndent(convData, "", "  "); err == nil {
				if err := writeRawFileToWorkspace(context.Background(), logPath, string(convJSON)); err != nil {
					log.Printf("[BG AGENT] Failed to persist builder conversation after synthetic turn: %v", err)
				} else {
					log.Printf("[BG AGENT] Persisted builder conversation after synthetic turn (%d messages) to %s", len(finalHistory), logPath)
				}
			}
		}

		if api.botManager != nil && strings.HasPrefix(sessionID, "bot-") {
			finalText := latestAssistantTextFromHistory(finalHistory)
			api.botManager.SendSyntheticTurnFinalIfNeeded(sessionID, finalText)
		}

		// Update stored agent (it now has the latest history from this turn)
		api.sessionAgentsMux.Lock()
		api.sessionAgents[sessionID] = llmAgent
		api.sessionAgentsMux.Unlock()

		// Update session status to completed
		api.updateSessionStatus(sessionID, "completed")
	}()
	return true
}
