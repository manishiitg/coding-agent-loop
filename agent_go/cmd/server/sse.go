package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"mcp-agent-builder-go/agent_go/internal/events"

	"github.com/gorilla/mux"
)

// sseEventMessage mirrors GetEventsResponse for SSE "event" messages.
type sseEventMessage struct {
	Events                     []events.Event `json:"events"`
	SessionStatus              string         `json:"session_status,omitempty"`
	DisplayStatus              string         `json:"display_status,omitempty"`
	LastProcessedIndex         int            `json:"last_processed_index"`
	HasRunningBackgroundAgents bool           `json:"has_running_background_agents,omitempty"`
	IsSyntheticTurn            bool           `json:"is_synthetic_turn,omitempty"` // True when running auto-notification turn (frontend should not block input)
	CanSteer                   bool           `json:"can_steer,omitempty"`
}

// sseStatusMessage is sent on the "status" SSE event.
type sseStatusMessage struct {
	SessionStatus              string `json:"session_status,omitempty"`
	DisplayStatus              string `json:"display_status,omitempty"`
	HasRunningBackgroundAgents bool   `json:"has_running_background_agents,omitempty"`
	IsSyntheticTurn            bool   `json:"is_synthetic_turn,omitempty"`
	CanSteer                   bool   `json:"can_steer,omitempty"`
}

// handleSSEStream serves a Server-Sent Events stream of session events.
//
// Query params:
//   - session_id (path)  — required
//   - since              — last event index the client has seen
//   - event_mode         — "advanced" | "tiny" | "micro" (default "micro")
//
// The handler subscribes to the EventStore first, then backfills catch-up
// events so no events are lost between subscription and the initial fetch.
func (api *StreamingAPI) handleSSEStream(w http.ResponseWriter, r *http.Request) {
	// 1. Validate streaming support
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// 2. Extract parameters
	vars := mux.Vars(r)
	sessionID := vars["session_id"]
	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Auth / ownership check
	sessionStatus, deny := api.getSessionStatusString(r, sessionID)
	if deny {
		http.Error(w, "Session not found or access denied", http.StatusNotFound)
		return
	}

	sinceIndex := -1
	// Support both ?since= and the standard Last-Event-ID header (EventSource auto-reconnect)
	sinceStr := r.URL.Query().Get("since")
	if sinceStr == "" {
		sinceStr = r.Header.Get("Last-Event-ID")
	}
	if sinceStr != "" {
		parsed, err := strconv.Atoi(sinceStr)
		if err != nil {
			http.Error(w, "since / Last-Event-ID must be a valid integer", http.StatusBadRequest)
			return
		}
		sinceIndex = parsed
	}

	// 3. Disable write timeout for SSE connections — they are long-lived streams.
	// The server's global WriteTimeout (30s) would kill the connection prematurely.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.Printf("[SSE] Warning: could not disable write deadline: %v", err)
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	flusher.Flush()

	// 4. Subscribe FIRST so no events are lost, then backfill
	sub := api.eventStore.Subscribe(sessionID)
	defer api.eventStore.Unsubscribe(sessionID, sub)
	log.Printf("[SSE] Subscribed to session %s (since=%d)", sessionID, sinceIndex)

	ctx := r.Context()

	// Backfill: send catch-up events the client hasn't seen yet
	if sinceIndex >= 0 {
		result := api.eventStore.GetEvents(sessionID, events.GetEventsOptions{
			SinceIndex:       sinceIndex,
			IncludeStreaming: true,
		})
		if len(result.Events) > 0 {
			runtimeStatus := api.sessionDisplayStatus(sessionID)
			msg := sseEventMessage{
				Events:                     result.Events,
				SessionStatus:              sessionStatus,
				DisplayStatus:              runtimeStatus.Status,
				LastProcessedIndex:         result.LastProcessedIndex,
				HasRunningBackgroundAgents: runtimeStatus.HasRunningBackgroundAgents,
				IsSyntheticTurn:            api.isSyntheticTurn(sessionID),
				CanSteer:                   runtimeStatus.CanSteer,
			}
			if err := writeSSEEvent(w, "event", result.LastProcessedIndex, msg); err != nil {
				return
			}
			sinceIndex = result.LastProcessedIndex
			flusher.Flush()
		}
	}

	// 5. Event loop
	statusTicker := time.NewTicker(2 * time.Second)
	defer statusTicker.Stop()
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	// Track the running last index for SSE id field
	lastIndex := sinceIndex

	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			return

		case event, ok := <-sub.Ch:
			if !ok {
				// Channel closed (unsubscribed)
				return
			}

			// Skip events already covered by the backfill
			if event.Data != nil && event.Data.EventIndex > 0 && event.Data.EventIndex <= lastIndex {
				continue
			}

			// Determine the event index to use as SSE id
			eventIndex := lastIndex + 1
			if event.Data != nil && event.Data.EventIndex > 0 {
				eventIndex = event.Data.EventIndex
			}
			if eventIndex > lastIndex {
				lastIndex = eventIndex
			}

			currentStatus, _ := api.getSessionStatusString(r, sessionID)
			if currentStatus == "" {
				currentStatus = sessionStatus
			}

			runtimeStatus := api.sessionDisplayStatus(sessionID)
			msg := sseEventMessage{
				Events:                     []events.Event{event},
				SessionStatus:              currentStatus,
				DisplayStatus:              runtimeStatus.Status,
				LastProcessedIndex:         lastIndex,
				HasRunningBackgroundAgents: runtimeStatus.HasRunningBackgroundAgents,
				IsSyntheticTurn:            api.isSyntheticTurn(sessionID),
				CanSteer:                   runtimeStatus.CanSteer,
			}
			if err := writeSSEEvent(w, "event", lastIndex, msg); err != nil {
				return
			}
			flusher.Flush()

		case <-statusTicker.C:
			currentStatus, _ := api.getSessionStatusString(r, sessionID)
			if currentStatus == "" {
				currentStatus = sessionStatus
			} else {
				sessionStatus = currentStatus // update cached status
			}
			runtimeStatus := api.sessionDisplayStatus(sessionID)
			msg := sseStatusMessage{
				SessionStatus:              currentStatus,
				DisplayStatus:              runtimeStatus.Status,
				HasRunningBackgroundAgents: runtimeStatus.HasRunningBackgroundAgents,
				IsSyntheticTurn:            api.isSyntheticTurn(sessionID),
				CanSteer:                   runtimeStatus.CanSteer,
			}
			if err := writeSSEEvent(w, "status", -1, msg); err != nil {
				return
			}
			flusher.Flush()

		case <-heartbeatTicker.C:
			// SSE comment heartbeat to keep connection alive
			if _, err := fmt.Fprintf(w, ": heartbeat %s\n\n", time.Now().Format(time.RFC3339)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEEvent writes a single SSE message to the writer.
// If id >= 0, an "id:" line is included (allows EventSource auto-reconnect).
func writeSSEEvent(w http.ResponseWriter, eventType string, id int, data interface{}) (writeErr error) {
	// Recover from concurrent map access panics during JSON marshaling.
	// The primary fix is in ContextAwareBridge (new map per event), but this
	// acts as a safety net to prevent the whole server from crashing.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[SSE] WARNING: recovered from panic during event serialization: %v", r)
			writeErr = fmt.Errorf("serialization panic: %v", r)
		}
	}()

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if id >= 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventType); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", jsonBytes); err != nil {
		return err
	}
	return nil
}
