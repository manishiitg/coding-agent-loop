package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/manishiitg/mcpagent/events"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
)

// startSessionInternal starts an agent session programmatically (used by bot connector).
// It constructs a QueryRequest from the provided map and invokes handleQuery internally.
// This blocks until the session completes (first turn only — the event filter manages lifecycle).
func (api *StreamingAPI) startSessionInternal(
	ctx context.Context,
	reqMap map[string]interface{},
	sessionID string,
	userID string,
	eventCallback func(event *events.AgentEvent),
) error {
	// Subscribe to events BEFORE starting the session to avoid race conditions
	// where the session errors out before the subscription is set up.
	sub := api.eventStore.Subscribe(sessionID)
	defer api.eventStore.Unsubscribe(sessionID, sub)

	// Marshal the request map to JSON
	body, err := json.Marshal(reqMap)
	if err != nil {
		return fmt.Errorf("failed to marshal query request: %w", err)
	}

	// Create a fake HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", "/api/query", io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return fmt.Errorf("failed to create internal request: %w", err)
	}
	if userID != "" && GetUserFromContext(httpReq.Context()) == nil {
		httpReq = httpReq.WithContext(context.WithValue(httpReq.Context(), UserContextKey, &UserClaims{
			UserID:   userID,
			Username: userID,
		}))
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Session-ID", sessionID)
	if userID != "" {
		httpReq.Header.Set("X-User-ID", userID)
	}

	// Use a ResponseRecorder to capture the response
	recorder := httptest.NewRecorder()

	// Call handleQuery synchronously — but it starts processing async and returns immediately
	api.handleQuery(recorder, httpReq)

	resp := recorder.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("handleQuery returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse the response to get the actual queryID
	var queryResp QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
		if isScheduledSession(sessionID) {
			scheduleLogfWithContext(newServerLogContext("", "", "", userID, "", sessionID), "[BOT_SESSION] Failed to parse handleQuery response: %v", err)
		}
	}

	if isScheduledSession(sessionID) {
		scheduleLogfWithContext(newServerLogContext("", "", "", userID, "", sessionID), "[BOT_SESSION] Internal session started: sessionID=%s queryID=%s", sessionID, queryResp.QueryID)
	}

	if queryResp.Status == queryStatusLiveInputDelivered {
		if isScheduledSession(sessionID) {
			scheduleLogfWithContext(newServerLogContext("", "", "", userID, "", sessionID), "[BOT_SESSION] Internal session delivered as live input; waiting for retained CLI activity/idle")
		}
		return api.waitForLiveInputTurnComplete(ctx, sub, sessionID)
	}

	// Now wait for the session to complete using the already-active subscription
	return waitForEvents(ctx, sub)
}

// sendFollowUpInternal injects a follow-up message into an existing session.
// It reuses the handleQuery path with the same session ID but does NOT block on completion.
// Events flow via EventStore → BotEventFilter → thread automatically.
//
// The reqMap is built by BotConversationManager.buildQueryRequest() so the follow-up agent
// gets the exact same config (servers, skills, delegation mode, API keys, etc.) as the initial session.
func (api *StreamingAPI) sendFollowUpInternal(
	ctx context.Context,
	reqMap map[string]interface{},
	sessionID string,
	userID string,
) error {
	body, err := json.Marshal(reqMap)
	if err != nil {
		return fmt.Errorf("failed to marshal follow-up request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "/api/query", io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return fmt.Errorf("failed to create follow-up request: %w", err)
	}
	if userID != "" && GetUserFromContext(httpReq.Context()) == nil {
		httpReq = httpReq.WithContext(context.WithValue(httpReq.Context(), UserContextKey, &UserClaims{
			UserID:   userID,
			Username: userID,
		}))
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Session-ID", sessionID)
	if userID != "" {
		httpReq.Header.Set("X-User-ID", userID)
	}

	recorder := httptest.NewRecorder()
	api.handleQuery(recorder, httpReq)

	resp := recorder.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("follow-up failed: status %d: %s", resp.StatusCode, string(respBody))
	}

	if isScheduledSession(sessionID) {
		scheduleLogfWithContext(newServerLogContext("", "", "", userID, "", sessionID), "[BOT_SESSION] Follow-up injected into session %s", sessionID)
	}
	return nil
}

// waitForEvents blocks until a completion/error event is received on the subscription.
// This returns after the first completion — the event filter manages extended lifecycle
// (blocking events, plan approval, follow-ups).
func waitForEvents(ctx context.Context, sub *internalevents.Subscriber) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case evt, ok := <-sub.Ch:
			if !ok {
				return nil // channel closed
			}

			switch evt.Type {
			case "agent_end", "conversation_end":
				return nil
			case "agent_error", "conversation_error":
				errMsg := "session failed"
				if evt.Error != "" {
					errMsg = evt.Error
				}
				return fmt.Errorf("%s", errMsg)
			case "unified_completion":
				if evt.Data != nil && evt.Data.Data != nil {
					if uc, ok := evt.Data.Data.(*events.UnifiedCompletionEvent); ok {
						if uc.Status == "error" {
							errMsg := "session failed"
							if uc.Error != "" {
								errMsg = uc.Error
							}
							return fmt.Errorf("%s", errMsg)
						}
					}
				}
				if evt.Error != "" {
					return fmt.Errorf("%s", evt.Error)
				}
				return nil
			}
		}
	}
}

var liveInputTurnPollInterval = time.Second
var liveInputTurnNoBusyStableAfter = 2 * time.Minute

const liveInputTurnIdleConsecutiveChecks = 2
const liveInputTurnMaxRefreshErrors = 3

// waitForLiveInputTurnComplete handles retained coding-CLI turns delivered via
// live input. That delivery path does not create a new server-owned foreground
// goroutine and therefore may never emit the completion event that waitForEvents
// normally relies on. Use the terminal pane as the fallback source of truth:
// wait until activity is observed, then require consecutive idle checks.
func (api *StreamingAPI) waitForLiveInputTurnComplete(ctx context.Context, sub *internalevents.Subscriber, sessionID string) error {
	ticker := time.NewTicker(liveInputTurnPollInterval)
	defer ticker.Stop()

	lastFingerprint := api.terminalFingerprint(sessionID)
	startedAt := time.Now()
	lastChangedAt := startedAt
	sawBusy := api.liveInputTurnLooksBusy(sessionID)
	sawChange := false
	consecutiveIdleChecks := 0
	consecutiveRefreshErrors := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt, ok := <-subscriberEvents(sub):
			if !ok {
				return nil
			}
			if done, err := completionEventResult(evt); done {
				return err
			}
		case <-ticker.C:
			if err := api.refreshSessionTmuxSnapshotsForIdleCheck(ctx, sessionID); err != nil {
				consecutiveIdleChecks = 0
				consecutiveRefreshErrors++
				if consecutiveRefreshErrors >= liveInputTurnMaxRefreshErrors {
					return err
				}
				continue
			}
			consecutiveRefreshErrors = 0

			now := time.Now()
			fingerprint := api.terminalFingerprint(sessionID)
			if fingerprint != "" && fingerprint != lastFingerprint {
				sawChange = true
				lastChangedAt = now
				lastFingerprint = fingerprint
			}

			if api.liveInputTurnLooksBusy(sessionID) {
				sawBusy = true
				consecutiveIdleChecks = 0
				continue
			}

			if sawBusy {
				consecutiveIdleChecks++
				if consecutiveIdleChecks >= liveInputTurnIdleConsecutiveChecks {
					return nil
				}
				continue
			}

			// Some CLI panes do not expose a reliable busy string for every model.
			// As a fallback, allow completion only after the pane changed and then
			// remained idle/stable for a longer grace period. This prevents the
			// original race where the initial prompt echo counted as completion in
			// the first two short idle polls.
			if sawChange && now.Sub(lastChangedAt) >= liveInputTurnNoBusyStableAfter && now.Sub(startedAt) >= liveInputTurnNoBusyStableAfter {
				consecutiveIdleChecks++
				if consecutiveIdleChecks >= liveInputTurnIdleConsecutiveChecks {
					return nil
				}
				continue
			}

			consecutiveIdleChecks = 0
		}
	}
}

func (api *StreamingAPI) liveInputTurnLooksBusy(sessionID string) bool {
	if api == nil {
		return false
	}
	return api.sessionIsBusy(sessionID) ||
		(api.terminalStore != nil && api.terminalStore.SessionHasBusyCodingTmux(sessionID))
}

func subscriberEvents(sub *internalevents.Subscriber) <-chan internalevents.Event {
	if sub == nil {
		return nil
	}
	return sub.Ch
}

func completionEventResult(evt internalevents.Event) (bool, error) {
	switch evt.Type {
	case "agent_end", "conversation_end":
		return true, nil
	case "agent_error", "conversation_error":
		errMsg := "session failed"
		if evt.Error != "" {
			errMsg = evt.Error
		}
		return true, fmt.Errorf("%s", errMsg)
	case "unified_completion":
		if evt.Data != nil && evt.Data.Data != nil {
			if uc, ok := evt.Data.Data.(*events.UnifiedCompletionEvent); ok {
				if uc.Status == "error" {
					errMsg := "session failed"
					if uc.Error != "" {
						errMsg = uc.Error
					}
					return true, fmt.Errorf("%s", errMsg)
				}
			}
		}
		if evt.Error != "" {
			return true, fmt.Errorf("%s", evt.Error)
		}
		return true, nil
	default:
		return false, nil
	}
}

func (api *StreamingAPI) terminalFingerprint(sessionID string) string {
	if api == nil || api.terminalStore == nil {
		return ""
	}
	var b strings.Builder
	for _, snapshot := range api.terminalStore.ListRaw(sessionID) {
		if strings.TrimSpace(snapshot.TmuxSession) == "" {
			continue
		}
		b.WriteString(snapshot.TerminalID)
		b.WriteByte('|')
		b.WriteString(snapshot.TmuxSession)
		b.WriteByte('|')
		b.WriteString(snapshot.State)
		b.WriteByte('|')
		b.WriteString(snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano))
		b.WriteByte('|')
		b.WriteString(snapshot.Content)
		b.WriteByte('\n')
	}
	return b.String()
}
