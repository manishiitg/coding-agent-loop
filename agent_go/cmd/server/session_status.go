package server

import "strings"

// Consolidated session status compatibility helpers. RuntimeCoordinator is the
// authoritative lifecycle read model; these helpers expose its values through
// legacy fields while clients migrate to RuntimeSnapshot.

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

// sessionDisplayStatus computes a session's live status WITHOUT building the
// full execution tree, gathering the same running/terminal signals the tree
// uses — lightweight enough for the scheduler to poll. Single reusable entry
// point for the scheduler, polling, and SSE.
func (api *StreamingAPI) sessionDisplayStatus(sessionID string) SessionDisplayStatus {
	snapshot, ok := api.authoritativeRuntimeSnapshot(sessionID)
	if !ok {
		snapshot = api.collectRuntimeSnapshot(sessionID)
	}
	return sessionDisplayStatusFromRuntime(snapshot)
}

func sessionDisplayStatusFromRuntime(snapshot RuntimeSnapshot) SessionDisplayStatus {
	return SessionDisplayStatus{
		Status:                     runtimePhaseDisplayStatus(snapshot.Phase),
		CanSteer:                   snapshot.ForegroundTurn.CanSteer,
		HasRunningBackgroundAgents: snapshot.BackgroundLive,
	}
}

// sessionIsBusy is a convenience wrapper: true when the session's consolidated
// status is "busy". Use this instead of ad-hoc isSessionBusy + HasRunningAgents
// checks so callers agree with the UI's notion of busy.
func (api *StreamingAPI) sessionIsBusy(sessionID string) bool {
	return api.sessionDisplayStatus(sessionID).Status == sessionExecutionDisplayBusy
}
