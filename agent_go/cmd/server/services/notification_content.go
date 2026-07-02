package services

// NotificationContent is an optional, typed, per-channel rendering of a single
// notification. It rides alongside the plain `message` string on a
// NotificationDestination so connectors can opt into richer output without the
// shared SendNotification signature changing.
//
// Model: `Text` (and the plain `message` arg) is the common lowest-common-
// denominator every channel can render. A channel with a non-nil slice here
// renders that instead; a channel without one falls back to the plain message.
// Add Slack/WhatsApp slices the same way when those channels grow rich content.
type NotificationContent struct {
	Text  string        // common fallback (mirrors the `message` arg)
	Gmail *GmailContent // Gmail-specific rendering (nil = derive from message)
}

// GmailContent is the Gmail-specific rendering. Every field is optional and
// falls back: Subject → derived from the message's first line; CC → none;
// Body → the plain message; Attachments → none.
type GmailContent struct {
	Subject     string
	CC          []string
	Body        string   // plain-text body (also the fallback when HTMLBody is set)
	HTMLBody    string   // optional rich HTML body; sent as a text/html alternative
	Attachments []string // absolute file paths on the server host (see Gmail raw send)
}

// gmailContentFrom returns the Gmail content slice from a destination, or nil.
func gmailContentFrom(dest *NotificationDestination) *GmailContent {
	if dest != nil && dest.Content != nil {
		return dest.Content.Gmail
	}
	return nil
}
