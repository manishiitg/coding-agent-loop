package virtualtools

import (
	"strings"
	"sync"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

var sessionNotificationDestinations = struct {
	sync.RWMutex
	values map[string]*services.NotificationDestination
}{values: make(map[string]*services.NotificationDestination)}

// RegisterSessionNotificationDestination records backend-only notification
// fields for a chat/workflow session. It merges non-empty fields so a stale
// continuation request cannot erase an explicit same-session config update.
func RegisterSessionNotificationDestination(sessionID string, dest *services.NotificationDestination) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || notificationDestinationEmpty(dest) {
		return
	}
	sessionNotificationDestinations.Lock()
	defer sessionNotificationDestinations.Unlock()
	current := cloneNotificationDestination(sessionNotificationDestinations.values[sessionID])
	if current == nil {
		current = &services.NotificationDestination{}
	}
	incoming := cloneNotificationDestination(dest)
	if incoming.UserID != "" {
		current.UserID = incoming.UserID
	}
	if incoming.Slack != nil {
		current.Slack = incoming.Slack
	}
	if incoming.SlackWebhook != nil {
		current.SlackWebhook = incoming.SlackWebhook
	}
	if incoming.WhatsApp != nil {
		current.WhatsApp = incoming.WhatsApp
	}
	if incoming.Gmail != nil {
		current.Gmail = incoming.Gmail
	}
	sessionNotificationDestinations.values[sessionID] = current
}

// UpdateSessionSlackWebhook applies a workflow webhook change immediately to
// subsequent notify_user calls in the same coding-agent session. The URL stays
// in backend memory and is never returned to the model.
func UpdateSessionSlackWebhook(sessionID string, webhook *services.SlackWebhookDest) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	sessionNotificationDestinations.Lock()
	defer sessionNotificationDestinations.Unlock()
	dest := cloneNotificationDestination(sessionNotificationDestinations.values[sessionID])
	if dest == nil {
		dest = &services.NotificationDestination{}
	}
	if webhook == nil {
		dest.SlackWebhook = nil
	} else {
		dest.SlackWebhook = &services.SlackWebhookDest{
			SecretName: webhook.SecretName,
			URL:        webhook.URL,
		}
	}
	sessionNotificationDestinations.values[sessionID] = dest
}

// DeleteSessionNotificationDestination releases state when the owning runtime
// session is evicted.
func DeleteSessionNotificationDestination(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	sessionNotificationDestinations.Lock()
	delete(sessionNotificationDestinations.values, sessionID)
	sessionNotificationDestinations.Unlock()
}

func sessionNotificationDestination(sessionID string) *services.NotificationDestination {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	sessionNotificationDestinations.RLock()
	dest := cloneNotificationDestination(sessionNotificationDestinations.values[sessionID])
	sessionNotificationDestinations.RUnlock()
	return dest
}
