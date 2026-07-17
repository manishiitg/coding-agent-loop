package server

import (
	"log"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
)

// reconcileUnexpectedTerminalExit propagates an unexpected provider-process
// failure to the exact runtime owner. Terminal snapshots are presentation; the
// owning execution/session is the source of truth that advances or stops work.
// Exact IDs and generation timestamps prevent an old pane from overwriting a
// newer execution that happens to reuse the same logical session.
func (api *StreamingAPI) reconcileUnexpectedTerminalExit(snapshot terminals.Snapshot, reason string) bool {
	if api == nil || !snapshot.Active {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "coding CLI terminal exited unexpectedly"
	}

	if codingAgentSnapshotIsMainAgent(snapshot) {
		active, ok := api.getActiveSession(snapshot.SessionID)
		if !ok || active == nil || !strings.EqualFold(strings.TrimSpace(active.Status), "running") {
			log.Printf("[TERMINAL RECONCILE] skipped main terminal=%s session=%s: no running owner", snapshot.TerminalID, snapshot.SessionID)
			return false
		}
		if !snapshot.CreatedAt.IsZero() && !active.CreatedAt.IsZero() && active.CreatedAt.After(snapshot.CreatedAt) {
			log.Printf("[TERMINAL RECONCILE] skipped stale main terminal=%s session=%s terminal_created=%s owner_created=%s",
				snapshot.TerminalID, snapshot.SessionID, snapshot.CreatedAt.Format(time.RFC3339Nano), active.CreatedAt.Format(time.RFC3339Nano))
			return false
		}
		log.Printf("[TERMINAL RECONCILE] failing main session=%s terminal=%s reason=%q", snapshot.SessionID, snapshot.TerminalID, reason)
		api.updateSessionStatus(snapshot.SessionID, "error")
		api.cancelSessionRuntimeWork(snapshot.SessionID, reason, runtimePhaseFailed)
		return true
	}

	candidates := uniqueTerminalOwnerIDs(snapshot.ExecutionID, snapshot.OwnerID)
	reconciled := false
	for _, executionID := range candidates {
		if api.failTrackedExecutionOwnedByTerminal(snapshot, executionID, reason) {
			reconciled = true
		}
		if api.bgAgentRegistry != nil {
			if agent := api.bgAgentRegistry.Get(snapshot.SessionID, executionID); agent != nil &&
				(snapshot.CreatedAt.IsZero() || agent.CreatedAt.IsZero() || !agent.CreatedAt.After(snapshot.CreatedAt)) &&
				agent.FailAndCancel(reason) {
				reconciled = true
			}
		}
	}
	if reconciled {
		log.Printf("[TERMINAL RECONCILE] failed child session=%s terminal=%s execution_candidates=%v reason=%q",
			snapshot.SessionID, snapshot.TerminalID, candidates, reason)
	} else {
		log.Printf("[TERMINAL RECONCILE] no current child owner matched session=%s terminal=%s execution_candidates=%v",
			snapshot.SessionID, snapshot.TerminalID, candidates)
	}
	return reconciled
}

func (api *StreamingAPI) failTrackedExecutionOwnedByTerminal(snapshot terminals.Snapshot, executionID, reason string) bool {
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return false
	}
	api.trackedWorkflowExecutionsMux.Lock()
	exec := api.trackedWorkflowExecutions[executionID]
	if exec == nil || exec.Status != trackedExecutionStatusRunning || exec.SessionID != snapshot.SessionID ||
		(!snapshot.CreatedAt.IsZero() && !exec.StartedAt.IsZero() && exec.StartedAt.After(snapshot.CreatedAt)) {
		api.trackedWorkflowExecutionsMux.Unlock()
		return false
	}
	now := time.Now().UTC()
	exec.Status = trackedExecutionStatusFailed
	exec.CompletedAt = &now
	exec.LastError = reason
	sessionID := exec.SessionID
	api.pruneTrackedExecutionsLocked(now)
	api.trackedWorkflowExecutionsMux.Unlock()
	api.observeRuntimeSnapshot(sessionID)
	return true
}

func uniqueTerminalOwnerIDs(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
