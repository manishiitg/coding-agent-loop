package virtualtools

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/cmd/server/services"
)

// HumanFeedbackRequest represents a pending feedback request
type HumanFeedbackRequest struct {
	UniqueID       string
	MessageForUser string
	UserResponse   string
	IsCompleted    bool
	CreatedAt      time.Time
}

// BotSessionCheckerFunc checks if a given session ID belongs to a bot session.
// Set by the server layer to enable skip-delay behavior.
type BotSessionCheckerFunc func(sessionID string) bool

// HumanFeedbackStore manages interactive feedback requests
type HumanFeedbackStore struct {
	requests          map[string]*HumanFeedbackRequest
	waiters           map[string]chan string
	mu                sync.RWMutex
	botSessionChecker BotSessionCheckerFunc
}

// SetBotSessionChecker sets the function to check if a session is a bot session
func (s *HumanFeedbackStore) SetBotSessionChecker(fn BotSessionCheckerFunc) {
	s.botSessionChecker = fn
}

// Global singleton instance
var (
	globalHumanFeedbackStore *HumanFeedbackStore
	humanFeedbackStoreOnce   sync.Once
)

// GetHumanFeedbackStore returns the global singleton instance
func GetHumanFeedbackStore() *HumanFeedbackStore {
	humanFeedbackStoreOnce.Do(func() {
		globalHumanFeedbackStore = &HumanFeedbackStore{
			requests: make(map[string]*HumanFeedbackRequest),
			waiters:  make(map[string]chan string),
		}
	})
	return globalHumanFeedbackStore
}

// CreateRequest creates a new feedback request
func (s *HumanFeedbackStore) CreateRequest(uniqueID, message string) error {
	return s.CreateRequestWithSlack(context.Background(), uniqueID, message, "", nil, nil)
}

// CreateRequestWithoutNotification creates a new feedback request without sending any notifications
// This is used for blocking_human_feedback events that should only appear in the frontend UI
func (s *HumanFeedbackStore) CreateRequestWithoutNotification(uniqueID, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if request already exists
	if existingRequest, exists := s.requests[uniqueID]; exists {
		// If the request is completed, clean it up and allow creating a new one
		if existingRequest.IsCompleted {
			// Clean up completed request
			delete(s.requests, uniqueID)
			if waiter, exists := s.waiters[uniqueID]; exists {
				close(waiter)
				delete(s.waiters, uniqueID)
			}
		} else {
			// Request exists and is still pending - cannot create duplicate
			return fmt.Errorf("feedback request %s already exists and is pending", uniqueID)
		}
	}

	s.requests[uniqueID] = &HumanFeedbackRequest{
		UniqueID:       uniqueID,
		MessageForUser: message,
		IsCompleted:    false,
		CreatedAt:      time.Now(),
	}

	s.waiters[uniqueID] = make(chan string, 1)

	return nil
}

// CreateRequestWithSlack creates a new feedback request and sends a notification
// after 2 minutes if no response arrives via the in-app UI first.
//
// dest is an optional destination hint passed through to the notification
// fanout — connectors use it to override their workspace-wide default
// (per-user prefs and per-request hints both flow through this). Pass nil
// for the legacy "use whatever the connectors are configured for" behaviour.
func (s *HumanFeedbackStore) CreateRequestWithSlack(ctx context.Context, uniqueID, message, contextMsg string, buttonOptions *services.ButtonOptions, dest *services.NotificationDestination) error {
	// First register the request (without notifications)
	if err := s.CreateRequestWithoutNotification(uniqueID, message); err != nil {
		return err
	}

	// Start delayed notification: wait 2 minutes, then check if user responded
	// If no response, send notification via connectors.
	// For bot sessions, send immediately (the thread IS the primary interface).
	go func() {
		// Check if this is a bot session (extract sessionID from uniqueID pattern)
		isBotSession := false
		if s.botSessionChecker != nil {
			// uniqueID often contains the sessionID — check if any active bot session matches
			isBotSession = s.botSessionChecker(uniqueID)
		}

		if !isBotSession {
			// Standard delay: wait 2 minutes before sending external notification
			time.Sleep(2 * time.Minute)
		} else {
			log.Printf("[HUMAN_FEEDBACK_STORE] Bot session detected for %s, sending notification immediately", uniqueID)
		}

		// Check if user has already responded
		s.mu.RLock()
		request, exists := s.requests[uniqueID]
		hasResponded := exists && request != nil && request.IsCompleted
		s.mu.RUnlock()

		if !exists {
			log.Printf("[HUMAN_FEEDBACK_STORE] Request %s no longer exists, skipping delayed notification", uniqueID)
			return
		}

		if hasResponded {
			log.Printf("[HUMAN_FEEDBACK_STORE] User already responded to %s, skipping delayed Slack notification", uniqueID)
			return
		}

		// User hasn't responded after 2 minutes, send Slack notification
		log.Printf("[HUMAN_FEEDBACK_STORE] No response after 2 minutes for %s, sending Slack notification", uniqueID)
		notificationManager := services.GetNotificationManager()
		if notificationManager == nil {
			log.Printf("[HUMAN_FEEDBACK_STORE] Notification manager not available")
			return
		}

		if buttonOptions != nil {
			log.Printf("[HUMAN_FEEDBACK_STORE] Sending delayed notification - buttonOptions: YesNoOnly=%v, YesLabel=%s, NoLabel=%s, Options=%v",
				buttonOptions.YesNoOnly, buttonOptions.YesLabel, buttonOptions.NoLabel, buttonOptions.Options)
		}

		// Send notification via notification manager (async, non-blocking)
		// This will send to all enabled connectors (Slack, WhatsApp)
		if err := notificationManager.SendNotification(context.Background(), uniqueID, message, contextMsg, buttonOptions, dest); err != nil {
			// Log error but don't fail - this is a reminder notification
			log.Printf("[HUMAN_FEEDBACK_STORE] Failed to send delayed notification: %v", err)
		} else {
			log.Printf("[HUMAN_FEEDBACK_STORE] ✅ Delayed Slack notification sent for %s", uniqueID)
		}
	}()

	return nil
}

// GetResponse gets a user response for a feedback request (if available)
func (s *HumanFeedbackStore) GetResponse(uniqueID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	request, exists := s.requests[uniqueID]
	if !exists {
		return "", false
	}

	if !request.IsCompleted || request.UserResponse == "" {
		return "", false
	}

	return request.UserResponse, true
}

// SubmitResponse submits a user response to a feedback request
func (s *HumanFeedbackStore) SubmitResponse(uniqueID, response string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	request, exists := s.requests[uniqueID]
	if !exists {
		return fmt.Errorf("feedback request %s not found", uniqueID)
	}

	if request.IsCompleted {
		return fmt.Errorf("feedback request %s already completed", uniqueID)
	}

	request.UserResponse = response
	request.IsCompleted = true

	// Signal waiter
	if waiter, exists := s.waiters[uniqueID]; exists {
		select {
		case waiter <- response:
		default:
		}
	}

	return nil
}

// WaitForResponse blocks until user responds or timeout occurs
func (s *HumanFeedbackStore) WaitForResponse(uniqueID string, timeout time.Duration) (string, error) {
	s.mu.RLock()
	waiter, exists := s.waiters[uniqueID]
	s.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("feedback request %s not found", uniqueID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case response := <-waiter:
		return response, nil
	case <-ctx.Done():
		return "", fmt.Errorf("timeout waiting for feedback: %w", ctx.Err())
	}
}

// Cleanup removes old requests (optional cleanup)
func (s *HumanFeedbackStore) Cleanup(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for uniqueID, request := range s.requests {
		if request.CreatedAt.Before(cutoff) {
			delete(s.requests, uniqueID)
			if waiter, exists := s.waiters[uniqueID]; exists {
				close(waiter)
				delete(s.waiters, uniqueID)
			}
		}
	}
}
