package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	mcpexecutor "github.com/manishiitg/mcpagent/executor"
)

type trustedPulseSession struct {
	runID string
	token uint64
}

var trustedPulseSessionRegistry = struct {
	sync.RWMutex
	nextToken uint64
	sessions  map[string]trustedPulseSession
}{sessions: map[string]trustedPulseSession{}}

// pulseWorklistRecordMu serializes the short read-then-write window used to
// make a logical Pulse run's Gate decision immutable once fully recorded.
var pulseWorklistRecordMu sync.Mutex

// registerTrustedPulseSession binds a physical workshop session to the logical
// Pulse run it is allowed to update. Recovery sessions use the original run ID.
func registerTrustedPulseSession(sessionID, pulseRunID string) func() {
	sessionID = strings.TrimSpace(sessionID)
	pulseRunID = strings.TrimSpace(pulseRunID)
	if sessionID == "" || pulseRunID == "" {
		return func() {}
	}

	trustedPulseSessionRegistry.Lock()
	trustedPulseSessionRegistry.nextToken++
	token := trustedPulseSessionRegistry.nextToken
	trustedPulseSessionRegistry.sessions[sessionID] = trustedPulseSession{runID: pulseRunID, token: token}
	trustedPulseSessionRegistry.Unlock()

	return func() {
		trustedPulseSessionRegistry.Lock()
		if current, ok := trustedPulseSessionRegistry.sessions[sessionID]; ok && current.token == token {
			delete(trustedPulseSessionRegistry.sessions, sessionID)
		}
		trustedPulseSessionRegistry.Unlock()
	}
}

func validateTrustedPulseToolRunID(ctx context.Context, requestedRunID string) error {
	requestedRunID = strings.TrimSpace(requestedRunID)
	if requestedRunID == "" {
		return fmt.Errorf("pulse_run_id is required")
	}
	sessionID := strings.TrimSpace(mcpexecutor.SessionIDFromContext(ctx))
	if sessionID == "" {
		return fmt.Errorf("Pulse state writes require an active scheduler session")
	}

	trustedPulseSessionRegistry.RLock()
	trusted, ok := trustedPulseSessionRegistry.sessions[sessionID]
	trustedPulseSessionRegistry.RUnlock()
	if !ok {
		return fmt.Errorf("session %q is not authorized to update Pulse state", sessionID)
	}
	if trusted.runID != requestedRunID {
		return fmt.Errorf("pulse_run_id %q does not match this session's logical Pulse run %q", requestedRunID, trusted.runID)
	}
	return nil
}
