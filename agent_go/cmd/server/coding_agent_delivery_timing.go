package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// stampCodingAgentDeliveryTiming is the common measurement point for every
// coding-agent provider and every retained delivery transport. It deliberately
// records IDs and timings only; user message content is never logged.
func (api *StreamingAPI) stampCodingAgentDeliveryTiming(r *http.Request, response *QueryResponse, requestReceivedAt time.Time) {
	if r == nil || response == nil {
		return
	}
	acceptedAt := time.Now()
	response.SubmissionID = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	response.ServerReceivedAt = requestReceivedAt.UTC().Format(time.RFC3339Nano)
	response.CLIAcceptedAt = acceptedAt.UTC().Format(time.RFC3339Nano)
	response.ServerToCLIMs = float64(acceptedAt.Sub(requestReceivedAt).Microseconds()) / 1000

	entry := map[string]interface{}{
		"phase":               "cli_accepted",
		"submission_id":       response.SubmissionID,
		"session_id":          response.SessionID,
		"query_id":            response.QueryID,
		"message_id":          response.MessageID,
		"provider":            response.Provider,
		"delivery_transport":  response.DeliveryTransport,
		"delivery_source":     response.DeliverySource,
		"delivery_status":     response.DeliveryStatus,
		"client_submitted_at": boundedTelemetryField(r.Header.Get("X-Client-Submitted-At"), 40),
		"server_received_at":  response.ServerReceivedAt,
		"cli_accepted_at":     response.CLIAcceptedAt,
		"server_to_cli_ms":    response.ServerToCLIMs,
	}
	encoded, _ := json.Marshal(entry)
	logfWithContext(api.httpRequestLogContext(r), "[CODING_AGENT_DELIVERY] %s", encoded)
}
