package virtualtools

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// HumanFeedbackRequest represents a pending feedback request
type HumanFeedbackRequest struct {
	UniqueID       string
	MessageForUser string
	UserResponse   string
	IsCompleted    bool
	CreatedAt      time.Time
}

// HumanFeedbackStore manages interactive feedback requests
type HumanFeedbackStore struct {
	requests map[string]*HumanFeedbackRequest
	waiters  map[string]chan string
	mu       sync.RWMutex
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
