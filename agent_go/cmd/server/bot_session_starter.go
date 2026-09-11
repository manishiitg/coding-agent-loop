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

	"github.com/manishiitg/mcpagent/events"
)

// internalBotRequestContext gives bot-originated turns the same authenticated
// identity as an HTTP turn from the paired account. The user directory remains
// authoritative for permissions: we deliberately persist only the user ID on
// the connector, then resolve the current username/email/role for every turn so
// a promotion or demotion takes effect without re-pairing WhatsApp.
func internalBotRequestContext(ctx context.Context, userID string) context.Context {
	if userID == "" || GetUserFromContext(ctx) != nil {
		return ctx
	}
	claims := &UserClaims{UserID: userID, Username: userID}
	if record := directoryUserFor(userID, "", ""); record != nil {
		claims.Username = record.Username
		claims.Email = record.Email
	}
	return context.WithValue(ctx, UserContextKey, claims)
}

// startSessionInternal starts an agent session programmatically (used by bot connector).
// It constructs a QueryRequest from the provided map and invokes handleQuery internally.
// This blocks until the exact query execution and all descendants complete.
func (api *StreamingAPI) startSessionInternal(
	ctx context.Context,
	reqMap map[string]interface{},
	sessionID string,
	userID string,
	eventCallback func(event *events.AgentEvent),
) error {
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
	httpReq = httpReq.WithContext(internalBotRequestContext(httpReq.Context(), userID))

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
			scheduleLogfWithContext(newServerLogContext("", "", "", userID, "", sessionID), "[BOT_SESSION] Internal session delivered as live input; waiting for exact execution %s", queryResp.QueryID)
		}
	}
	if strings.TrimSpace(queryResp.QueryID) == "" {
		return fmt.Errorf("handleQuery did not return a query execution id")
	}
	return api.waitForConversationTurnTree(ctx, sessionID, queryResp.QueryID, schedulerWorkshopMaxInactivity)
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
	httpReq = httpReq.WithContext(internalBotRequestContext(httpReq.Context(), userID))

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
