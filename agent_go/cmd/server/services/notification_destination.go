package services

// NotificationDestination tells a connector where to send a feedback request.
// Each connector field is optional — when set, the connector uses it; when nil,
// the connector falls back to a per-user preference (looked up via UserID),
// then to its workspace-wide default.
//
// A nil *NotificationDestination is equivalent to "no hints, do whatever your
// configured default is" and preserves the pre-routing behavior.
type NotificationDestination struct {
	Slack        *SlackDest           // Slack bot channel/thread hint
	SlackWebhook *SlackWebhookDest    // workflow-scoped one-way Incoming Webhook
	WhatsApp     *WhatsAppDest        // WhatsApp recipient hint
	Gmail        *GmailDest           // Gmail recipient hint
	UserID       string               // workspace user ID, used to look up per-user preferences
	Content      *NotificationContent // optional typed per-channel content (nil = plain message only)

	// ExcludeChannels lists account-level connector names ("gmail", "slack",
	// "whatsapp") to skip for this notification. Sourced from the workflow's
	// workflow.json notifications.exclude_channels so a per-workflow preference
	// suppresses an inherited account-level channel without touching the
	// account-wide configuration. Matched case-insensitively.
	ExcludeChannels []string
}

// SlackWebhookDest is a workflow-scoped, one-way Slack Incoming Webhook.
// SecretName is safe to report in configuration. URL is resolved from the
// encrypted secret store only at run time and must never be logged or persisted.
type SlackWebhookDest struct {
	SecretName string
	URL        string
}

// GmailDest is the Gmail-specific destination hint. Email is the recipient
// address; the sending account is the single shared identity gws is
// authenticated as.
type GmailDest struct {
	Email string

	// BlockedRecipients is a per-notification denylist unioned with the
	// account-wide GmailConfig.BlockedRecipients at send time. It lets a
	// per-workflow notification preference (soul/soul.md ## Notifications, passed
	// through by notify_user) reject additional recipients for this workflow
	// without editing the account-wide config. Never widens the allow-set — it
	// can only block more, never unblock a globally-blocked address.
	BlockedRecipients []string
}

// SlackDest is the Slack-specific destination hint. ThreadTS is optional —
// when set, the message is posted as a reply in that thread instead of a
// top-level post in the channel.
type SlackDest struct {
	ChannelID string
	ThreadTS  string
}

// WhatsAppDest is the WhatsApp-specific destination hint. ChannelID is a
// WhatsApp JID (e.g. "919000000000@s.whatsapp.net" or a group JID) used when
// replying to an active bot chat. PhoneE164 is the recipient phone number in
// E.164 format (e.g. "+919000000000").
type WhatsAppDest struct {
	ChannelID string
	PhoneE164 string
}
