package services

import (
	"fmt"
	"strings"
	"testing"
)

func TestBotFollowUpFailureMessagePreservesUncertainReceipt(t *testing.T) {
	err := NewBotSubmissionError(409, []byte(`{"delivery_status":"delivery_uncertain","submission_id":"receipt-123","message":"internal implementation details"}`))
	receipt := err.(*BotSubmissionError)
	if receipt.SubmissionID != "receipt-123" {
		t.Fatal("lost receipt needed for reconciliation")
	}
	message := botFollowUpFailureMessage(fmt.Errorf("wrapped: %w", err))
	if !strings.Contains(message, "saved") || !strings.Contains(message, "don't resend") {
		t.Fatalf("missing durable delivery guidance: %s", message)
	}
	if strings.Contains(message, "receipt-123") || strings.Contains(message, "implementation") {
		t.Fatalf("exposed internal response: %s", message)
	}
}

func TestBotFollowUpFailureMessageDoesNotExposeUnknownErrors(t *testing.T) {
	for _, err := range []error{fmt.Errorf("secret credentials"), NewBotSubmissionError(500, []byte("secret credentials"))} {
		message := botFollowUpFailureMessage(err)
		if strings.Contains(message, "secret") || strings.Contains(message, "saved") {
			t.Fatalf("unsafe or misleading message: %s", message)
		}
	}
}
