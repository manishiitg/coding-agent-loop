package services

import (
	"encoding/json"
	"errors"
	"fmt"
)

// BotSubmissionError retains the durable receipt for reconciliation without
// forwarding the HTTP response body into a channel conversation.
type BotSubmissionError struct {
	StatusCode     int
	SubmissionID   string
	DeliveryStatus string
}

func (e *BotSubmissionError) Error() string {
	return fmt.Sprintf("bot submission failed: status %d, delivery %s, submission %s", e.StatusCode, e.DeliveryStatus, e.SubmissionID)
}

func NewBotSubmissionError(status int, body []byte) error {
	var receipt struct {
		SubmissionID   string `json:"submission_id"`
		DeliveryStatus string `json:"delivery_status"`
	}
	_ = json.Unmarshal(body, &receipt)
	return &BotSubmissionError{StatusCode: status, SubmissionID: receipt.SubmissionID, DeliveryStatus: receipt.DeliveryStatus}
}

func botFollowUpFailureMessage(err error) string {
	var submission *BotSubmissionError
	if errors.As(err, &submission) && submission.DeliveryStatus == "delivery_uncertain" {
		return "Your message was saved, but I couldn't confirm it reached the agent. Please don't resend it yet; its delivery needs to be checked to avoid running it twice."
	}
	return "I couldn't deliver your message to the agent. Check the execution logs for the cause."
}
