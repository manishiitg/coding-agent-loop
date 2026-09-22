package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	maxClientChatTelemetryBody   = 64 << 10
	maxClientChatTelemetryEvents = 100
)

type clientChatTelemetryBatch struct {
	PageID string                     `json:"page_id"`
	Events []clientChatTelemetryEvent `json:"events"`
}

type clientChatTelemetryEvent struct {
	Sequence        int64   `json:"sequence"`
	Phase           string  `json:"phase"`
	SessionID       string  `json:"session_id"`
	EventID         string  `json:"event_id,omitempty"`
	EventType       string  `json:"event_type,omitempty"`
	Transport       string  `json:"transport,omitempty"`
	TabID           string  `json:"tab_id,omitempty"`
	ClientTime      string  `json:"client_time"`
	PerformanceMS   float64 `json:"performance_ms"`
	ServerEventTime string  `json:"server_event_time,omitempty"`
}

var allowedClientChatTelemetryPhases = map[string]bool{
	"sse_received":     true,
	"poll_received":    true,
	"catchup_received": true,
	"processed":        true,
	"painted":          true,
}

func boundedTelemetryField(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}

// handleClientChatTelemetry closes the browser-observability gap in chat
// delivery. The payload is deliberately a fixed, content-free schema: message
// text, tool arguments, and arbitrary metadata are never accepted or logged.
func (api *StreamingAPI) handleClientChatTelemetry(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxClientChatTelemetryBody))
	if err != nil {
		http.Error(w, "telemetry payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	var batch clientChatTelemetryBatch
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&batch); err != nil {
		http.Error(w, "invalid telemetry payload", http.StatusBadRequest)
		return
	}
	if len(batch.Events) == 0 || len(batch.Events) > maxClientChatTelemetryEvents {
		http.Error(w, "invalid telemetry event count", http.StatusBadRequest)
		return
	}

	pageID := boundedTelemetryField(batch.PageID, 80)
	serverTime := time.Now().UTC().Format(time.RFC3339Nano)
	for _, event := range batch.Events {
		if !allowedClientChatTelemetryPhases[event.Phase] {
			http.Error(w, "invalid telemetry phase", http.StatusBadRequest)
			return
		}
		entry := map[string]interface{}{
			"server_time":       serverTime,
			"page_id":           pageID,
			"sequence":          event.Sequence,
			"phase":             event.Phase,
			"session_id":        boundedTelemetryField(event.SessionID, 160),
			"event_id":          boundedTelemetryField(event.EventID, 200),
			"event_type":        boundedTelemetryField(event.EventType, 80),
			"transport":         boundedTelemetryField(event.Transport, 32),
			"tab_id":            boundedTelemetryField(event.TabID, 160),
			"client_time":       boundedTelemetryField(event.ClientTime, 40),
			"performance_ms":    event.PerformanceMS,
			"server_event_time": boundedTelemetryField(event.ServerEventTime, 40),
		}
		encoded, _ := json.Marshal(entry)
		logfWithContext(api.httpRequestLogContext(r), "[CLIENT_CHAT_TIMELINE] %s", encoded)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprint(w, `{"accepted":true}`)
}
