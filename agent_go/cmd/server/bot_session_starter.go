package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/manishiitg/mcpagent/events"

	internalevents "mcp-agent-builder-go/agent_go/internal/events"
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
