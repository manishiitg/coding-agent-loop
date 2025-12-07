package services

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// ButtonOptions represents button configuration for interactive notifications
type ButtonOptions struct {
	YesNoOnly bool     // If true, show only Yes/No buttons
	YesLabel  string   // Custom label for Yes button (default: "Approve")
	NoLabel   string   // Custom label for No button (default: "Reject")
	Options   []string // Array of option labels for multiple choice (renders as buttons)
}

// NotificationConnector is the interface that all notification connectors must implement
// This allows for multiple connectors (Slack, Gmail, WhatsApp, etc.) to be registered
type NotificationConnector interface {
	// Name returns the name of the connector (e.g., "slack", "gmail", "whatsapp")
	Name() string

	// IsEnabled checks if the connector is enabled and ready to send notifications
	IsEnabled() bool

	// SendNotification sends a notification to the connector
	// uniqueID: unique identifier for the feedback request
	// message: the message/question to send
	// contextMsg: additional context information
	// buttonOptions: optional button configuration (nil if no buttons)
	// Returns: message identifier (e.g., Slack timestamp) and error
	SendNotification(ctx context.Context, uniqueID string, message string, contextMsg string, buttonOptions *ButtonOptions) (string, error)
}

// FeedbackResponseFunc is a function type for submitting feedback responses (to avoid import cycle)
type FeedbackResponseFunc func(uniqueID string, response string) error

// NotificationManager manages all notification connectors
// It sends notifications to all enabled connectors when a feedback request is created
// It also receives notifications from connectors and updates the feedback store
type NotificationManager struct {
	connectors             map[string]NotificationConnector
	submitFeedbackResponse FeedbackResponseFunc
	mu                     sync.RWMutex
}

var (
	globalNotificationManager *NotificationManager
	notificationManagerOnce   sync.Once
)

// GetNotificationManager returns the global notification manager instance
func GetNotificationManager() *NotificationManager {
	notificationManagerOnce.Do(func() {
		globalNotificationManager = &NotificationManager{
			connectors: make(map[string]NotificationConnector),
		}
	})
	return globalNotificationManager
}

// RegisterConnector registers a notification connector
func (nm *NotificationManager) RegisterConnector(connector NotificationConnector) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	name := connector.Name()
	nm.connectors[name] = connector
	log.Printf("[NOTIFICATION_MANAGER] Registered connector: %s", name)
}

// UnregisterConnector removes a notification connector
func (nm *NotificationManager) UnregisterConnector(name string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	delete(nm.connectors, name)
	log.Printf("[NOTIFICATION_MANAGER] Unregistered connector: %s", name)
}

// SendNotification sends a notification to all enabled connectors
// This is called by the feedback store when a new request is created
func (nm *NotificationManager) SendNotification(ctx context.Context, uniqueID string, message string, contextMsg string, buttonOptions *ButtonOptions) error {
	nm.mu.RLock()
	connectors := make([]NotificationConnector, 0, len(nm.connectors))
	for _, connector := range nm.connectors {
		if connector.IsEnabled() {
			connectors = append(connectors, connector)
		}
	}
	nm.mu.RUnlock()

	if len(connectors) == 0 {
		log.Printf("[NOTIFICATION_MANAGER] No enabled connectors available for notification")
		return nil // Not an error, just no connectors enabled
	}

	// Send to all enabled connectors (async, non-blocking)
	for _, connector := range connectors {
		go func(conn NotificationConnector) {
			msgID, err := conn.SendNotification(ctx, uniqueID, message, contextMsg, buttonOptions)
			if err != nil {
				log.Printf("[NOTIFICATION_MANAGER] Failed to send notification via %s: %v", conn.Name(), err)
			} else {
				log.Printf("[NOTIFICATION_MANAGER] ✅ Notification sent via %s (msgID: %s)", conn.Name(), msgID)
			}
		}(connector)
	}

	return nil
}

// GetConnector returns a specific connector by name
func (nm *NotificationManager) GetConnector(name string) (NotificationConnector, error) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	connector, exists := nm.connectors[name]
	if !exists {
		return nil, fmt.Errorf("connector %s not found", name)
	}

	return connector, nil
}

// ListConnectors returns a list of all registered connector names
func (nm *NotificationManager) ListConnectors() []string {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	names := make([]string, 0, len(nm.connectors))
	for name := range nm.connectors {
		names = append(names, name)
	}

	return names
}

// SetFeedbackResponseFunc sets the feedback store function for submitting responses
// This is called from server initialization to connect to the feedback store
func (nm *NotificationManager) SetFeedbackResponseFunc(fn FeedbackResponseFunc) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.submitFeedbackResponse = fn
	log.Printf("[NOTIFICATION_MANAGER] Feedback response function set")
}

// ReceiveNotification receives a notification/reply from a connector
// This is called by connectors when they receive a reply (e.g., Slack thread reply)
// The notification manager then updates the human feedback store
func (nm *NotificationManager) ReceiveNotification(uniqueID string, response string, connectorName string) error {
	nm.mu.RLock()
	submitFn := nm.submitFeedbackResponse
	nm.mu.RUnlock()

	if submitFn == nil {
		log.Printf("[NOTIFICATION_MANAGER] ⚠️  Feedback store not available, cannot submit response from %s", connectorName)
		return fmt.Errorf("feedback store not available")
	}

	if err := submitFn(uniqueID, response); err != nil {
		log.Printf("[NOTIFICATION_MANAGER] ❌ Failed to submit feedback from %s for unique_id %s: %v", connectorName, uniqueID, err)
		return fmt.Errorf("failed to submit feedback: %w", err)
	}

	log.Printf("[NOTIFICATION_MANAGER] ✅ Successfully received notification from %s for unique_id: %s", connectorName, uniqueID)
	return nil
}
