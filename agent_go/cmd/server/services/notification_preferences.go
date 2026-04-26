package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// NotificationPreference is the per-user override for where workflow questions
// should land. A user may set a preferred Slack channel, a WhatsApp recipient,
// or both. Empty fields fall back to the connector's workspace-wide default.
// The Disabled flags let a user opt out of one connector entirely (e.g. only
// receive Slack pings, never WhatsApp).
type NotificationPreference struct {
	SlackChannelID   string `json:"slack_channel_id,omitempty"`
	SlackDisabled    bool   `json:"slack_disabled,omitempty"`
	WhatsAppPhone    string `json:"whatsapp_phone,omitempty"`    // E.164 (e.g. "+919000000000")
	WhatsAppDisabled bool   `json:"whatsapp_disabled,omitempty"`
}

// notificationPreferences is the on-disk shape: { user_id: NotificationPreference }
type notificationPreferences struct {
	Users map[string]*NotificationPreference `json:"users"`
}

const notificationPrefsFilePath = "config/notification-preferences.json"

var (
	notifPrefsMu    sync.RWMutex
	notifPrefsCache *notificationPreferences
)

// loadNotificationPreferences reads the prefs file from the workspace, caching
// the result. On first read or after a write, the cache is repopulated.
func loadNotificationPreferences(ctx context.Context) (*notificationPreferences, error) {
	notifPrefsMu.RLock()
	cached := notifPrefsCache
	notifPrefsMu.RUnlock()
	if cached != nil {
		return cached, nil
	}

	data, exists, err := readWorkspaceFile(ctx, workspaceAPIURL(), notificationPrefsFilePath)
	if err != nil {
		return nil, fmt.Errorf("read notification prefs: %w", err)
	}
	prefs := &notificationPreferences{Users: map[string]*NotificationPreference{}}
	if exists && len(data) > 0 {
		if err := json.Unmarshal([]byte(data), prefs); err != nil {
			return nil, fmt.Errorf("parse notification prefs: %w", err)
		}
		if prefs.Users == nil {
			prefs.Users = map[string]*NotificationPreference{}
		}
	}

	notifPrefsMu.Lock()
	notifPrefsCache = prefs
	notifPrefsMu.Unlock()
	return prefs, nil
}

// getNotificationPreferences returns the cached preference for a user, or nil
// if none is set or the prefs file can't be read. Connector resolvers call
// this on the hot path, so failures are silent — they just fall through to
// the workspace default.
func getNotificationPreferences(userID string) *NotificationPreference {
	if userID == "" {
		return nil
	}
	prefs, err := loadNotificationPreferences(context.Background())
	if err != nil || prefs == nil {
		return nil
	}
	notifPrefsMu.RLock()
	defer notifPrefsMu.RUnlock()
	return prefs.Users[userID]
}

// SetNotificationPreference upserts the preference for a user and persists it.
// Pass a zero-valued NotificationPreference (or nil) to clear the user's prefs.
func SetNotificationPreference(ctx context.Context, userID string, pref *NotificationPreference) error {
	if userID == "" {
		return fmt.Errorf("user_id is required")
	}

	prefs, err := loadNotificationPreferences(ctx)
	if err != nil {
		return err
	}

	notifPrefsMu.Lock()
	if pref == nil || (*pref == NotificationPreference{}) {
		delete(prefs.Users, userID)
	} else {
		prefs.Users[userID] = pref
	}
	out, marshalErr := json.MarshalIndent(prefs, "", "  ")
	notifPrefsMu.Unlock()
	if marshalErr != nil {
		return fmt.Errorf("marshal notification prefs: %w", marshalErr)
	}

	if err := writeWorkspaceFile(ctx, workspaceAPIURL(), notificationPrefsFilePath, string(out)); err != nil {
		return fmt.Errorf("write notification prefs: %w", err)
	}

	notifPrefsMu.Lock()
	notifPrefsCache = nil // invalidate so next read re-loads
	notifPrefsMu.Unlock()
	return nil
}

// GetNotificationPreference is the public read accessor used by the API layer.
func GetNotificationPreference(userID string) *NotificationPreference {
	return getNotificationPreferences(userID)
}
