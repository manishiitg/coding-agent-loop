package server

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
	Metadata          map[string]string     `json:"metadata,omitempty"` // arbitrary key-value pairs (e.g. workshop_mode, lock_code)
	cancel            context.CancelFunc
	mu                sync.RWMutex
	startNotified     bool
	notified          bool
	getHistory        HistoryFunc      // returns last N conversation entries from the running sub-agent
	toolCalls         []ToolCallRecord // tracked tool calls with timing
	activeToolCall    map[string]int   // toolCallID → index in toolCalls (for matching start/end)
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

// NotifyCompletion sends a completion notification for a session
func (r *BackgroundAgentRegistry) NotifyCompletion(sessionID, agentID string) {
	r.mu.RLock()
	ch, ok := r.completionNotifiers[sessionID]
	r.mu.RUnlock()

	if ok {
		// Non-blocking send — if channel is full, completion will be picked up on next poll
		select {
		case ch <- agentID:
		default:
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

// HasRunningAgents returns true if the session has any running agents
func (r *BackgroundAgentRegistry) HasRunningAgents(sessionID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sessionAgents, ok := r.agents[sessionID]
	if !ok {
		return false
	}
	for _, agent := range sessionAgents {
		if agent.GetStatus() == BGAgentRunning {
			return true
		}
	}
	return false
}
