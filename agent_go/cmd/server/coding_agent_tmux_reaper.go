package server

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/internal/terminals"
)

const (
	defaultCodingAgentTmuxOrphanIdleTimeout = 3 * time.Hour
	envCodingAgentTmuxOrphanIdleSeconds     = "MCP_CODING_AGENT_TMUX_ORPHAN_IDLE_TIMEOUT_SECONDS"
)

type codingAgentTmuxCleanupCandidate struct {
	snapshot terminals.Snapshot
	reason   string
}

type codingAgentTmuxSessionState struct {
	status string
}

func codingAgentTmuxOrphanIdleTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(envCodingAgentTmuxOrphanIdleSeconds))
	if raw == "" {
		return defaultCodingAgentTmuxOrphanIdleTimeout
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultCodingAgentTmuxOrphanIdleTimeout
	}
	return time.Duration(seconds) * time.Second
}

func (api *StreamingAPI) cleanupStaleCodingAgentTmuxSessions(now time.Time) int {
	if api == nil || api.terminalStore == nil {
		return 0
	}
	if now.IsZero() {
		now = time.Now()
	}
	candidates := api.staleCodingAgentTmuxCleanupCandidates(now, codingAgentTmuxOrphanIdleTimeout())
	closed := 0
	for _, candidate := range candidates {
		snapshot := candidate.snapshot
		tmuxSession := strings.TrimSpace(snapshot.TmuxSession)
		if tmuxSession == "" {
			continue
		}
		reason := "stale coding-agent tmux cleanup"
		if candidate.reason != "" {
			reason += ": " + candidate.reason
		}
		closeCodingAgentTmuxSessionByName(tmuxSession, reason)
		if _, ok := api.terminalStore.MarkStale(snapshot.TerminalID); ok {
			closed++
			log.Printf("[TMUX_REAPER] Closed stale coding-agent tmux session %q terminal=%q owner=%q session=%q reason=%s",
				tmuxSession, snapshot.TerminalID, snapshot.OwnerID, snapshot.SessionID, candidate.reason)
		}
	}
	return closed
}

func (api *StreamingAPI) staleCodingAgentTmuxCleanupCandidates(now time.Time, idleTimeout time.Duration) []codingAgentTmuxCleanupCandidate {
	if api == nil || api.terminalStore == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	if idleTimeout <= 0 {
		idleTimeout = defaultCodingAgentTmuxOrphanIdleTimeout
	}

	sessionStates := api.codingAgentTmuxActiveSessionStates()
	snapshots := api.terminalStore.List("")
	candidates := make([]codingAgentTmuxCleanupCandidate, 0)
	for _, snapshot := range snapshots {
		tmuxSession := strings.TrimSpace(snapshot.TmuxSession)
		if tmuxSession == "" || !isCodingAgentTmuxSessionName(tmuxSession) {
			continue
		}

		state, sessionExists := sessionStates[snapshot.SessionID]
		switch {
		case !sessionExists:
			candidates = append(candidates, codingAgentTmuxCleanupCandidate{
				snapshot: snapshot,
				reason:   "owner session missing",
			})
		case codingAgentTmuxSessionStatusClosesImmediately(state.status):
			candidates = append(candidates, codingAgentTmuxCleanupCandidate{
				snapshot: snapshot,
				reason:   "owner session " + state.status,
			})
		case snapshot.ClosesAt != nil && !now.Before(*snapshot.ClosesAt):
			candidates = append(candidates, codingAgentTmuxCleanupCandidate{
				snapshot: snapshot,
				reason:   "retention elapsed",
			})
		case codingAgentTmuxSessionStatusUsesIdleBackstop(state.status) &&
			codingAgentTmuxSnapshotIdleFor(snapshot, now, idleTimeout):
			candidates = append(candidates, codingAgentTmuxCleanupCandidate{
				snapshot: snapshot,
				reason:   "idle backstop elapsed",
			})
		}
	}
	return candidates
}

func (api *StreamingAPI) codingAgentTmuxActiveSessionStates() map[string]codingAgentTmuxSessionState {
	states := make(map[string]codingAgentTmuxSessionState)
	if api == nil {
		return states
	}
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()
	for sessionID, session := range api.activeSessions {
		if session == nil {
			continue
		}
		states[sessionID] = codingAgentTmuxSessionState{status: strings.TrimSpace(session.Status)}
	}
	return states
}

func codingAgentTmuxSessionStatusClosesImmediately(status string) bool {
	switch strings.TrimSpace(status) {
	case "stopped", "error", "dismissed":
		return true
	default:
		return false
	}
}

func codingAgentTmuxSessionStatusUsesIdleBackstop(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "inactive":
		return true
	default:
		return false
	}
}

func codingAgentTmuxSnapshotIdleFor(snapshot terminals.Snapshot, now time.Time, idleTimeout time.Duration) bool {
	lastUpdate := snapshot.UpdatedAt
	if lastUpdate.IsZero() {
		lastUpdate = snapshot.CreatedAt
	}
	if lastUpdate.IsZero() {
		return false
	}
	return !now.Before(lastUpdate.Add(idleTimeout))
}

func isCodingAgentTmuxSessionName(tmuxSession string) bool {
	name := strings.TrimSpace(tmuxSession)
	return strings.HasPrefix(name, "mlp-agy-cli") ||
		strings.HasPrefix(name, "mlp-claude-code") ||
		strings.HasPrefix(name, "mlp-codex-cli") ||
		strings.HasPrefix(name, "mlp-cursor-cli") ||
		strings.HasPrefix(name, "mlp-gemini-cli") ||
		strings.HasPrefix(name, "mlp-pi-cli")
}

func closeCodingAgentTmuxSessionByName(tmuxSession, reason string) {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return
	}
	_ = gracefulCloseCodingCLITmuxByName(tmuxSession, reason)
	ctx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
	defer cancel()
	if err := runTerminalTmuxCommand(ctx, "", "kill-session", "-t", tmuxSession); err != nil && !isMissingTmuxTargetError(err) {
		log.Printf("[TMUX_REAPER] kill-session %q failed: %v", tmuxSession, err)
	}
}
