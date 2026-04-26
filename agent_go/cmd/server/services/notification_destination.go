package services

// NotificationDestination tells a connector where to send a feedback request.
// Each connector field is optional — when set, the connector uses it; when nil,
// the connector falls back to a per-user preference (looked up via UserID),
// then to its workspace-wide default.
//
// A nil *NotificationDestination is equivalent to "no hints, do whatever your
// configured default is" and preserves the pre-routing behaviour.
type NotificationDestination struct {
	Slack    *SlackDest    // Slack channel/thread hint
	WhatsApp *WhatsAppDest // WhatsApp recipient hint
	UserID   string        // workspace user ID, used to look up per-user preferences
}

// SlackDest is the Slack-specific destination hint. ThreadTS is optional —
// when set, the message is posted as a reply in that thread instead of a
// top-level post in the channel.
type SlackDest struct {
	ChannelID string
	ThreadTS  string
}

// WhatsAppDest is the WhatsApp-specific destination hint. PhoneE164 is the
// recipient phone number in E.164 format (e.g. "+919000000000").
type WhatsAppDest struct {
	PhoneE164 string
}
