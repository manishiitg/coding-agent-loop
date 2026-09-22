package server

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestStampCodingAgentDeliveryTimingCorrelatesSubmission(t *testing.T) {
	api := &StreamingAPI{}
	req := httptest.NewRequest("POST", "/api/query", nil)
	req.Header.Set("Idempotency-Key", "submission-123")
	req.Header.Set("X-Client-Submitted-At", "2026-09-22T10:00:00.000Z")
	response := QueryResponse{
		QueryID: "query-1", SessionID: "session-1", MessageID: "message-1",
		Provider: "codex-cli", DeliveryStatus: "sent_to_cli",
		DeliveryTransport: "tmux", DeliverySource: queryDeliverySourceRunningAgent,
	}
	receivedAt := time.Now().Add(-25 * time.Millisecond)

	api.stampCodingAgentDeliveryTiming(req, &response, receivedAt)

	if response.SubmissionID != "submission-123" {
		t.Fatalf("submission id = %q", response.SubmissionID)
	}
	if response.ServerReceivedAt == "" || response.CLIAcceptedAt == "" {
		t.Fatalf("missing timestamps: %#v", response)
	}
	if response.ServerToCLIMs < 20 {
		t.Fatalf("server_to_cli_ms = %f, want >= 20", response.ServerToCLIMs)
	}
}
