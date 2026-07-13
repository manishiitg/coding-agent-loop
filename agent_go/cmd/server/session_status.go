package server

import "strings"

// Consolidated session "live status" logic — the single source of truth for
// whether a session is busy / idle / stopped (and steerable). Previously this
// determination was reimplemented in several places (the execution-tree summary,
// the polling/SSE endpoints, and the scheduler's idle-wait), which let them
// disagree — e.g. the scheduler treated a session as done while the UI still
// showed it busy. Everything now derives status from the functions here.

// SessionDisplayStatus is a session's consolidated live status.
type SessionDisplayStatus struct {
	Status                     string // sessionExecutionDisplay{Busy,Idle,Stopped}
	CanSteer                   bool
	HasRunningBackgroundAgents bool
}

type sessionLifecycleStatus string

const legacyCanceledBritishStatus = "cancel" + "led"

const (
	sessionLifecycleRunning   sessionLifecycleStatus = "running"
	sessionLifecycleCompleted sessionLifecycleStatus = "completed"
	sessionLifecycleFailed    sessionLifecycleStatus = "failed"
	sessionLifecycleStopped   sessionLifecycleStatus = "stopped"
	sessionLifecycleInactive  sessionLifecycleStatus = "inactive"
	sessionLifecycleUnknown   sessionLifecycleStatus = "unknown"
)

// normalizeSessionLifecycleStatus gives internal state decisions one stable
// vocabulary while preserving the legacy wire value in existing API fields.
func normalizeSessionLifecycleStatus(status string) sessionLifecycleStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "active", "busy", "paused", "waiting":
		return sessionLifecycleRunning
	case "completed", "complete", "success", "succeeded", "done":
		return sessionLifecycleCompleted
	case "error", "failed", "failure":
		return sessionLifecycleFailed
	case "stopped", "canceled", legacyCanceledBritishStatus, "dismissed":
		return sessionLifecycleStopped
	case "inactive", "idle":
		return sessionLifecycleInactive
	default:
		return sessionLifecycleUnknown
	}
}

// deriveSessionDisplayStatus maps raw running/terminal signals to the
// busy/idle/stopped label. This is the ONE place that decision is made.
func deriveSessionDisplayStatus(hasRunningWork, isStopped bool) string {
	switch {
	case hasRunningWork:
		return sessionExecutionDisplayBusy
	case isStopped:
		return sessionExecutionDisplayStopped
	default:
		return sessionExecutionDisplayIdle
	}
}

// isStoppedSessionStatus reports whether a session-level status string means the
// session has finished/halted (vs. actively idle and able to continue).
func isStoppedSessionStatus(status string) bool {
	switch normalizeSessionLifecycleStatus(status) {
	case sessionLifecycleCompleted, sessionLifecycleFailed, sessionLifecycleStopped, sessionLifecycleInactive:
		return true
	default:
		return false
	}
}

// sessionHasRunningWork reports whether a session currently has any live work:
// a busy foreground turn, running background agents, a running tracked
// execution, or a steerable (active-turn / busy-tmux) foreground agent. This is
// the lightweight signal set used by callers that don't build a full execution
// tree (scheduler, polling).
func (api *StreamingAPI) sessionHasRunningWork(sessionID string, hasRunningBackgroundAgents, canSteer bool) bool {
	return api.isSessionBusy(sessionID) ||
		api.hasActiveTurnCancel(sessionID) ||
		hasRunningBackgroundAgents ||
		api.hasRunningTrackedExecutionForSession(sessionID) ||
		canSteer
}

// sessionDisplayStatus computes a session's live status WITHOUT building the
// full execution tree, gathering the same running/terminal signals the tree
// uses — lightweight enough for the scheduler to poll. Single reusable entry
// point for the scheduler, polling, and SSE.
func (api *StreamingAPI) sessionDisplayStatus(sessionID string) SessionDisplayStatus {
	sessionStatus := ""
	if s, ok := api.getActiveSession(sessionID); ok && s != nil {
		sessionStatus = s.Status
	}
	hasBg := api.bgAgentRegistry != nil && api.bgAgentRegistry.HasRunningAgents(sessionID)
	canSteer := api.canSteerSession(sessionID)
	result := SessionDisplayStatus{
		Status:                     deriveSessionDisplayStatus(api.sessionHasRunningWork(sessionID, hasBg, canSteer), isStoppedSessionStatus(sessionStatus)),
		CanSteer:                   canSteer,
		HasRunningBackgroundAgents: hasBg,
	}
	api.observeRuntimeSnapshot(sessionID, &result)
	return result
}

// sessionIsBusy is a convenience wrapper: true when the session's consolidated
// status is "busy". Use this instead of ad-hoc isSessionBusy + HasRunningAgents
// checks so callers agree with the UI's notion of busy.
func (api *StreamingAPI) sessionIsBusy(sessionID string) bool {
	return api.sessionDisplayStatus(sessionID).Status == sessionExecutionDisplayBusy
}
