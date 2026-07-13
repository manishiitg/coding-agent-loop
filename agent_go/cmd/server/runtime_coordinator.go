package server

import (
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

// RuntimePhase is the coordinator's normalized lifecycle vocabulary. Phase 1
// observes and compares this state only; existing stores remain authoritative.
type RuntimePhase string

const (
	runtimePhaseStarting  RuntimePhase = "starting"
	runtimePhaseRunning   RuntimePhase = "running"
	runtimePhaseWaiting   RuntimePhase = "waiting"
	runtimePhaseIdle      RuntimePhase = "idle"
	runtimePhaseCompleted RuntimePhase = "completed"
	runtimePhaseFailed    RuntimePhase = "failed"
	runtimePhaseCanceled  RuntimePhase = "canceled"
)

type RuntimeForegroundSnapshot struct {
	Busy      bool `json:"busy"`
	HasCancel bool `json:"has_cancel"`
	CanSteer  bool `json:"can_steer"`
	Synthetic bool `json:"synthetic"`
}

type RuntimeChildSnapshot struct {
	ExecutionID string     `json:"execution_id"`
	Kind        string     `json:"kind,omitempty"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type RuntimeBackgroundSnapshot struct {
	AgentID     string     `json:"agent_id"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type RuntimeTerminalSnapshot struct {
	TerminalID  string    `json:"terminal_id"`
	ExecutionID string    `json:"execution_id,omitempty"`
	State       string    `json:"state"`
	Active      bool      `json:"active"`
	HasTmux     bool      `json:"has_tmux"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// RuntimeSnapshot is an immutable, revisioned observer view assembled from the
// existing runtime stores. Callers always receive deep copies.
type RuntimeSnapshot struct {
	SessionID        string                      `json:"session_id"`
	Generation       uint64                      `json:"generation"`
	Revision         uint64                      `json:"revision"`
	Phase            RuntimePhase                `json:"phase"`
	Reason           string                      `json:"reason,omitempty"`
	RawSessionStatus string                      `json:"raw_session_status,omitempty"`
	ForegroundTurn   RuntimeForegroundSnapshot   `json:"foreground_turn"`
	ChildExecutions  []RuntimeChildSnapshot      `json:"child_executions,omitempty"`
	BackgroundAgents []RuntimeBackgroundSnapshot `json:"background_agents,omitempty"`
	BackgroundLive   bool                        `json:"background_live"`
	Terminals        []RuntimeTerminalSnapshot   `json:"terminals,omitempty"`
	TerminalBusy     bool                        `json:"terminal_busy"`
	WaitingForUser   bool                        `json:"waiting_for_user"`
	WaitingMessage   string                      `json:"waiting_message,omitempty"`
	LastProgressAt   time.Time                   `json:"last_progress_at"`
	StartedAt        time.Time                   `json:"started_at"`
	CompletedAt      *time.Time                  `json:"completed_at,omitempty"`
	ObservedAt       time.Time                   `json:"observed_at"`
}

type runtimeCoordinatorRecord struct {
	snapshot              RuntimeSnapshot
	lastMismatchSignature string
}

// RuntimeCoordinator stores observer snapshots. It deliberately has no
// transition APIs that mutate the existing runtime stores in Phase 1.
type RuntimeCoordinator struct {
	mu           sync.RWMutex
	records      map[string]runtimeCoordinatorRecord
	nextRevision uint64
}

func NewRuntimeCoordinator() *RuntimeCoordinator {
	return &RuntimeCoordinator{records: make(map[string]runtimeCoordinatorRecord)}
}

func (c *RuntimeCoordinator) Observe(candidate RuntimeSnapshot) (RuntimeSnapshot, bool) {
	if c == nil || strings.TrimSpace(candidate.SessionID) == "" {
		return RuntimeSnapshot{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	previous, exists := c.records[candidate.SessionID]
	candidate.Generation = runtimeSnapshotGeneration(previous.snapshot, candidate, exists)
	if exists && runtimeSnapshotsSemanticallyEqual(previous.snapshot, candidate) {
		return cloneRuntimeSnapshot(previous.snapshot), false
	}

	c.nextRevision++
	candidate.Revision = c.nextRevision
	candidate.ObservedAt = time.Now().UTC()
	previous.snapshot = cloneRuntimeSnapshot(candidate)
	c.records[candidate.SessionID] = previous
	return cloneRuntimeSnapshot(candidate), true
}

func (c *RuntimeCoordinator) Snapshot(sessionID string) (RuntimeSnapshot, bool) {
	if c == nil {
		return RuntimeSnapshot{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	record, ok := c.records[sessionID]
	if !ok {
		return RuntimeSnapshot{}, false
	}
	return cloneRuntimeSnapshot(record.snapshot), true
}

func (c *RuntimeCoordinator) CompareLegacy(snapshot RuntimeSnapshot, legacy SessionDisplayStatus) {
	if c == nil || snapshot.SessionID == "" {
		return
	}
	observerDisplay := runtimePhaseDisplayStatus(snapshot.Phase)
	signature := ""
	if observerDisplay != legacy.Status || snapshot.ForegroundTurn.CanSteer != legacy.CanSteer ||
		snapshot.BackgroundLive != legacy.HasRunningBackgroundAgents {
		signature = fmt.Sprintf("phase=%s display=%s legacy=%s steer=%t/%t bg=%t/%t",
			snapshot.Phase, observerDisplay, legacy.Status,
			snapshot.ForegroundTurn.CanSteer, legacy.CanSteer,
			snapshot.BackgroundLive, legacy.HasRunningBackgroundAgents)
	}

	c.mu.Lock()
	record, ok := c.records[snapshot.SessionID]
	if !ok || record.snapshot.Revision != snapshot.Revision || record.lastMismatchSignature == signature {
		c.mu.Unlock()
		return
	}
	record.lastMismatchSignature = signature
	c.records[snapshot.SessionID] = record
	c.mu.Unlock()

	if signature != "" {
		log.Printf("[RUNTIME_OBSERVER] mismatch session=%s generation=%d revision=%d %s reason=%q",
			snapshot.SessionID, snapshot.Generation, snapshot.Revision, signature, snapshot.Reason)
	}
}

func runtimeSnapshotGeneration(previous, candidate RuntimeSnapshot, exists bool) uint64 {
	if !exists {
		return 1
	}
	generation := previous.Generation
	if generation == 0 {
		generation = 1
	}
	newStart := !previous.StartedAt.IsZero() && !candidate.StartedAt.IsZero() &&
		!previous.StartedAt.Equal(candidate.StartedAt)
	restartedAfterQuiescence := (runtimePhaseIsTerminal(previous.Phase) || previous.Phase == runtimePhaseIdle) &&
		(candidate.Phase == runtimePhaseStarting || candidate.Phase == runtimePhaseRunning)
	if newStart || restartedAfterQuiescence {
		generation++
	}
	return generation
}

func runtimeSnapshotsSemanticallyEqual(a, b RuntimeSnapshot) bool {
	a.Revision, b.Revision = 0, 0
	a.ObservedAt, b.ObservedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(a, b)
}

func cloneRuntimeSnapshot(snapshot RuntimeSnapshot) RuntimeSnapshot {
	copy := snapshot
	copy.ChildExecutions = append([]RuntimeChildSnapshot(nil), snapshot.ChildExecutions...)
	copy.BackgroundAgents = append([]RuntimeBackgroundSnapshot(nil), snapshot.BackgroundAgents...)
	copy.Terminals = append([]RuntimeTerminalSnapshot(nil), snapshot.Terminals...)
	if snapshot.CompletedAt != nil {
		completedAt := *snapshot.CompletedAt
		copy.CompletedAt = &completedAt
	}
	for i := range copy.ChildExecutions {
		if snapshot.ChildExecutions[i].CompletedAt != nil {
			completedAt := *snapshot.ChildExecutions[i].CompletedAt
			copy.ChildExecutions[i].CompletedAt = &completedAt
		}
	}
	for i := range copy.BackgroundAgents {
		if snapshot.BackgroundAgents[i].CompletedAt != nil {
			completedAt := *snapshot.BackgroundAgents[i].CompletedAt
			copy.BackgroundAgents[i].CompletedAt = &completedAt
		}
	}
	return copy
}

func runtimePhaseDisplayStatus(phase RuntimePhase) string {
	switch phase {
	case runtimePhaseStarting, runtimePhaseRunning:
		return sessionExecutionDisplayBusy
	case runtimePhaseCompleted, runtimePhaseFailed, runtimePhaseCanceled:
		return sessionExecutionDisplayStopped
	default:
		return sessionExecutionDisplayIdle
	}
}

func runtimePhaseIsTerminal(phase RuntimePhase) bool {
	return phase == runtimePhaseCompleted || phase == runtimePhaseFailed || phase == runtimePhaseCanceled
}

func sortRuntimeSnapshot(snapshot *RuntimeSnapshot) {
	sort.Slice(snapshot.ChildExecutions, func(i, j int) bool {
		return snapshot.ChildExecutions[i].ExecutionID < snapshot.ChildExecutions[j].ExecutionID
	})
	sort.Slice(snapshot.BackgroundAgents, func(i, j int) bool {
		return snapshot.BackgroundAgents[i].AgentID < snapshot.BackgroundAgents[j].AgentID
	})
	sort.Slice(snapshot.Terminals, func(i, j int) bool {
		return snapshot.Terminals[i].TerminalID < snapshot.Terminals[j].TerminalID
	})
}

func (api *StreamingAPI) observeRuntimeSnapshot(sessionID string, legacy *SessionDisplayStatus) (RuntimeSnapshot, bool) {
	if api == nil || api.runtimeCoordinator == nil || strings.TrimSpace(sessionID) == "" {
		return RuntimeSnapshot{}, false
	}
	snapshot, changed := api.runtimeCoordinator.Observe(api.collectRuntimeSnapshot(sessionID))
	if legacy != nil {
		api.runtimeCoordinator.CompareLegacy(snapshot, *legacy)
	}
	return snapshot, changed
}

func (api *StreamingAPI) collectRuntimeSnapshot(sessionID string) RuntimeSnapshot {
	snapshot := RuntimeSnapshot{SessionID: strings.TrimSpace(sessionID)}
	now := time.Now().UTC()

	if active, ok := api.getActiveSession(sessionID); ok && active != nil {
		snapshot.RawSessionStatus = active.Status
		snapshot.StartedAt = active.CreatedAt
		snapshot.LastProgressAt = active.LastActivity
		snapshot.WaitingForUser = active.NeedsUserInput
		snapshot.WaitingMessage = active.WaitingMessage
		snapshot.ForegroundTurn.Synthetic = active.IsSyntheticTurn
	}

	snapshot.ForegroundTurn.Busy = api.isSessionBusy(sessionID)
	snapshot.ForegroundTurn.HasCancel = api.hasActiveTurnCancel(sessionID)
	snapshot.ForegroundTurn.CanSteer = api.canSteerSession(sessionID)

	for _, execution := range api.trackedExecutionsForSession(sessionID) {
		if execution == nil {
			continue
		}
		snapshot.ChildExecutions = append(snapshot.ChildExecutions, RuntimeChildSnapshot{
			ExecutionID: execution.ExecutionID,
			Kind:        execution.Kind,
			Status:      execution.Status,
			StartedAt:   execution.StartedAt,
			CompletedAt: cloneRuntimeTime(execution.CompletedAt),
		})
		snapshot.StartedAt = earliestRuntimeTime(snapshot.StartedAt, execution.StartedAt)
		snapshot.LastProgressAt = latestRuntimeTime(snapshot.LastProgressAt, execution.StartedAt)
		if execution.CompletedAt != nil {
			snapshot.LastProgressAt = latestRuntimeTime(snapshot.LastProgressAt, *execution.CompletedAt)
		}
	}

	if api.bgAgentRegistry != nil {
		snapshot.BackgroundLive = api.bgAgentRegistry.HasRunningAgents(sessionID)
		for _, backgroundAgent := range api.bgAgentRegistry.GetAll(sessionID) {
			if backgroundAgent == nil {
				continue
			}
			agent := backgroundAgent.GetSnapshot()
			snapshot.BackgroundAgents = append(snapshot.BackgroundAgents, RuntimeBackgroundSnapshot{
				AgentID:     agent.ID,
				Status:      string(agent.Status),
				CreatedAt:   agent.CreatedAt,
				CompletedAt: cloneRuntimeTime(agent.CompletedAt),
			})
			snapshot.StartedAt = earliestRuntimeTime(snapshot.StartedAt, agent.CreatedAt)
			snapshot.LastProgressAt = latestRuntimeTime(snapshot.LastProgressAt, agent.CreatedAt)
			if agent.CompletedAt != nil {
				snapshot.LastProgressAt = latestRuntimeTime(snapshot.LastProgressAt, *agent.CompletedAt)
			}
		}
	}

	if api.terminalStore != nil {
		snapshot.TerminalBusy = api.terminalStore.SessionHasBusyCodingTmux(sessionID)
		for _, terminal := range api.terminalStore.ListMetadata(sessionID) {
			snapshot.Terminals = append(snapshot.Terminals, RuntimeTerminalSnapshot{
				TerminalID:  terminal.TerminalID,
				ExecutionID: terminal.ExecutionID,
				State:       terminal.State,
				Active:      terminal.Active,
				HasTmux:     strings.TrimSpace(terminal.TmuxSession) != "",
				UpdatedAt:   terminal.UpdatedAt,
			})
			snapshot.StartedAt = earliestRuntimeTime(snapshot.StartedAt, terminal.CreatedAt)
			snapshot.LastProgressAt = latestRuntimeTime(snapshot.LastProgressAt, terminal.UpdatedAt)
		}
	}

	snapshot.Phase, snapshot.Reason = deriveRuntimePhase(snapshot, now)
	if runtimePhaseIsTerminal(snapshot.Phase) {
		completedAt := snapshot.LastProgressAt
		if completedAt.IsZero() {
			completedAt = now
		}
		snapshot.CompletedAt = &completedAt
	}
	sortRuntimeSnapshot(&snapshot)
	return snapshot
}

func deriveRuntimePhase(snapshot RuntimeSnapshot, now time.Time) (RuntimePhase, string) {
	if snapshot.WaitingForUser {
		return runtimePhaseWaiting, "session requires user input"
	}
	if snapshot.ForegroundTurn.Busy || snapshot.ForegroundTurn.HasCancel || snapshot.ForegroundTurn.CanSteer {
		return runtimePhaseRunning, "foreground turn is active"
	}
	for _, execution := range snapshot.ChildExecutions {
		if execution.Status == trackedExecutionStatusRunning {
			return runtimePhaseRunning, "tracked child execution is active"
		}
	}
	if snapshot.BackgroundLive {
		return runtimePhaseRunning, "background agent is active or in completion grace"
	}
	if snapshot.TerminalBusy {
		return runtimePhaseRunning, "coding-agent terminal is busy"
	}

	switch normalizeSessionLifecycleStatus(snapshot.RawSessionStatus) {
	case sessionLifecycleFailed:
		return runtimePhaseFailed, "session reported failure"
	case sessionLifecycleStopped:
		return runtimePhaseCanceled, "session was stopped"
	case sessionLifecycleCompleted:
		return runtimePhaseCompleted, "session completed"
	case sessionLifecycleRunning:
		if !snapshot.StartedAt.IsZero() && now.Sub(snapshot.StartedAt) < 10*time.Second {
			return runtimePhaseStarting, "session started and runtime work is not registered yet"
		}
		return runtimePhaseIdle, "session reports running but no live work is registered"
	default:
		return runtimePhaseIdle, "no live runtime work is registered"
	}
}

func cloneRuntimeTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func earliestRuntimeTime(current, candidate time.Time) time.Time {
	if candidate.IsZero() || (!current.IsZero() && !candidate.Before(current)) {
		return current
	}
	return candidate
}

func latestRuntimeTime(current, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate
	}
	return current
}
