package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"

	unifiedevents "github.com/manishiitg/mcpagent/events"
)

// startSessionInternal starts an agent session programmatically (used by bot connector).
// It constructs a QueryRequest from the provided map and invokes handleQuery internally.
// This blocks until the session completes.
func (api *StreamingAPI) startSessionInternal(
	ctx context.Context,
	reqMap map[string]interface{},
	sessionID string,
	userID string,
	eventCallback func(event *unifiedevents.AgentEvent),
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
		log.Printf("[BOT_SESSION] Failed to parse handleQuery response: %v", err)
	}

	log.Printf("[BOT_SESSION] Internal session started: sessionID=%s queryID=%s", sessionID, queryResp.QueryID)

	// Now wait for the session to complete by polling the event store
	return api.waitForSessionCompletion(ctx, sessionID)
}

// waitForSessionCompletion blocks until the session status changes to completed/error
func (api *StreamingAPI) waitForSessionCompletion(ctx context.Context, sessionID string) error {
	// Subscribe to events for this session
	sub := api.eventStore.Subscribe(sessionID, "advanced")
	defer api.eventStore.Unsubscribe(sessionID, sub)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case evt, ok := <-sub.Ch:
			if !ok {
				return nil // channel closed
			}

			// Check for completion events
			switch evt.Type {
			case "agent_end", "conversation_end":
				return nil
			case "agent_error", "conversation_error":
				errMsg := "session failed"
				if evt.Error != "" {
					errMsg = evt.Error
				}
				return fmt.Errorf("%s", errMsg)
			}
		}
	}
}
