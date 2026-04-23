package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	_ "modernc.org/sqlite" // pure-Go SQLite driver; registered as "sqlite"
)

// WhatsAppService implements BotConnector on top of whatsmeow — a Go library
// that speaks the multi-device WhatsApp Web protocol. The bot operates
// through a personal WhatsApp account (paired once via QR code); no Meta
// Business API, verified business, or approved templates are required.
//
// Tradeoff: whatsmeow uses the unofficial WhatsApp Web protocol. Meta may ban
// numbers exhibiting bot-like behaviour at scale, so this is best suited to
// personal / internal use, not customer-facing commercial volume.
type WhatsAppService struct {
	dbPath string

	mu        sync.RWMutex
	container *sqlstore.Container
	device    *store.Device
	client    *whatsmeow.Client

	qrMu      sync.RWMutex
	lastQR    string
	qrExpires time.Time

	messageHandler     BotMessageHandler
	interactionHandler BotInteractionHandler
}

// NewWhatsAppService constructs a service that will persist its multi-device
// pairing state in dbPath (a SQLite file local to the agent process).
func NewWhatsAppService(dbPath string) *WhatsAppService {
	return &WhatsAppService{dbPath: dbPath}
}

// Name returns the connector name used in routing and logs.
func (w *WhatsAppService) Name() string { return "whatsapp" }

// IsEnabled reports whether the underlying client has been initialised via
// StartListening. Note: enabled != paired != connected.
func (w *WhatsAppService) IsEnabled() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.client != nil
}

// SupportsThreads returns false: WhatsApp has no Slack-style threads. Every
// message from a JID goes into one ongoing conversation, keyed by ChannelID.
func (w *WhatsAppService) SupportsThreads() bool { return false }

// StartListening opens the sqlite session store, creates the whatsmeow
// client, registers the event handler, and (if already paired) connects to
// WhatsApp. If the session is unpaired, a QR code is requested and exposed
// via GetQR / GetQRImagePNG for the pairing HTTP route to serve; once the
// user scans it the client auto-proceeds to connected state.
func (w *WhatsAppService) StartListening(ctx context.Context) error {
	if w.dbPath == "" {
		return fmt.Errorf("whatsapp: session DB path not configured")
	}
	if err := os.MkdirAll(filepath.Dir(w.dbPath), 0o700); err != nil {
		return fmt.Errorf("whatsapp: mkdir session dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", w.dbPath)
	container, err := sqlstore.New(ctx, "sqlite", dsn, waLog.Noop)
	if err != nil {
		return fmt.Errorf("whatsapp: open sqlstore: %w", err)
	}

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp: get device: %w", err)
	}

	client := whatsmeow.NewClient(device, waLog.Noop)
	client.AddEventHandler(w.handleEvent)

	w.mu.Lock()
	w.container = container
	w.device = device
	w.client = client
	w.mu.Unlock()

	paired := device.ID != nil
	log.Printf("[WHATSAPP] Service started (db=%s, paired=%v)", w.dbPath, paired)

	go w.connectLoop(ctx)
	return nil
}

// connectLoop establishes the initial whatsmeow connection. If the store is
// not yet paired it waits on the QR channel until the user scans; otherwise
// it reconnects with the stored device identity. whatsmeow handles its own
// reconnection on transient drops, so this runs once per start.
func (w *WhatsAppService) connectLoop(ctx context.Context) {
	w.mu.RLock()
	client := w.client
	w.mu.RUnlock()
	if client == nil {
		return
	}

	if client.Store.ID == nil {
		qrChan, err := client.GetQRChannel(ctx)
		if err != nil {
			log.Printf("[WHATSAPP] GetQRChannel failed: %v", err)
			return
		}
		if err := client.Connect(); err != nil {
			log.Printf("[WHATSAPP] Connect (pre-pair) failed: %v", err)
			return
		}
		for evt := range qrChan {
			switch evt.Event {
			case "code":
				w.qrMu.Lock()
				w.lastQR = evt.Code
				w.qrExpires = time.Now().Add(evt.Timeout)
				w.qrMu.Unlock()
				log.Printf("[WHATSAPP] QR code ready for pairing (expires in %s)", evt.Timeout)
			case "success":
				log.Printf("[WHATSAPP] Pairing successful")
				w.qrMu.Lock()
				w.lastQR = ""
				w.qrMu.Unlock()
			case "timeout":
				log.Printf("[WHATSAPP] QR timeout — request a new QR to pair again")
			}
		}
		return
	}

	if err := client.Connect(); err != nil {
		log.Printf("[WHATSAPP] Connect failed: %v", err)
		return
	}
	if id := client.Store.ID; id != nil {
		log.Printf("[WHATSAPP] Connected as %s", id.String())
	}
}

// StopListening disconnects the client. The session DB is left intact so the
// next StartListening resumes the paired state without re-scanning.
func (w *WhatsAppService) StopListening() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.client != nil {
		w.client.Disconnect()
	}
}

// GetQR returns the most recent pairing QR code string and its expiration.
// Empty code means no pairing flow is active (either already paired, or
// StartListening has not been called).
func (w *WhatsAppService) GetQR() (code string, expires time.Time) {
	w.qrMu.RLock()
	defer w.qrMu.RUnlock()
	return w.lastQR, w.qrExpires
}

// GetQRImagePNG renders the active QR code as a PNG image of the requested
// size in pixels. Returns (nil, nil) if no QR is available; an error only on
// encoding failure.
func (w *WhatsAppService) GetQRImagePNG(size int) ([]byte, error) {
	code, _ := w.GetQR()
	if code == "" {
		return nil, nil
	}
	if size <= 0 {
		size = 384
	}
	return qrcode.Encode(code, qrcode.Medium, size)
}

// IsPaired reports whether a device identity has been stored. Paired implies
// a prior successful QR scan, but not that the client is currently connected
// — use IsConnected for liveness.
func (w *WhatsAppService) IsPaired() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.client != nil && w.client.Store != nil && w.client.Store.ID != nil
}

// IsConnected reports whether the whatsmeow client is live on the WhatsApp
// websocket right now.
func (w *WhatsAppService) IsConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.client != nil && w.client.IsConnected()
}

// OwnJID returns the paired account's own JID (the bot's WhatsApp identity),
// or an empty JID if unpaired.
func (w *WhatsAppService) OwnJID() types.JID {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.client == nil || w.client.Store == nil || w.client.Store.ID == nil {
		return types.JID{}
	}
	return *w.client.Store.ID
}

// handleEvent is the whatsmeow event dispatcher. We currently care about
// inbound messages and log a few connection lifecycle transitions; other
// event types (presence, receipts, history sync) are ignored.
func (w *WhatsAppService) handleEvent(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.Message:
		w.handleIncomingMessage(evt)
	case *events.Connected:
		_ = evt
		log.Printf("[WHATSAPP] Connected")
	case *events.Disconnected:
		log.Printf("[WHATSAPP] Disconnected")
	case *events.LoggedOut:
		log.Printf("[WHATSAPP] Logged out (reason=%v) — session DB is now invalid; delete %s and re-pair", evt.Reason, w.dbPath)
	}
}

// handleIncomingMessage converts a whatsmeow message event into a
// BotIncomingMessage and forwards it to the registered handler. v1: 1:1 DMs
// only — group chats, broadcasts, and status updates are skipped. Also adds
// an "eyes" reaction mirror of the Slack ack UX so the sender sees their
// message was received.
func (w *WhatsAppService) handleIncomingMessage(evt *events.Message) {
	info := evt.Info
	if info.IsFromMe {
		return
	}
	if info.IsGroup || info.Chat.Server == types.BroadcastServer {
		return
	}
	if info.Chat.User == "status" {
		return
	}

	text := extractWhatsAppText(evt.Message)
	if text == "" {
		return
	}

	w.mu.RLock()
	handler := w.messageHandler
	w.mu.RUnlock()
	if handler == nil {
		return
	}

	chatJID := info.Chat.String()
	senderUser := info.Sender.User
	senderName := info.PushName
	log.Printf("[WHATSAPP] Incoming message from %s (%s): %s", senderUser, chatJID, botTruncate(text, 80))

	// Eager ack with 👀 reaction — parity with the Slack UX. Best effort.
	_ = w.sendReaction(context.Background(), chatJID, info.Sender, info.ID, "👀")

	handler(BotIncomingMessage{
		Platform:      "whatsapp",
		UserID:        senderUser,
		UserName:      senderName,
		ChannelID:     chatJID,
		ThreadTS:      "",
		Text:          text,
		MessageTS:     info.ID,
		Timestamp:     info.Timestamp,
		IsThreadReply: false,
		IsMention:     true, // every DM effectively addresses the bot
	})
}

// extractWhatsAppText pulls the plain text body from a whatsmeow message.
// Supports simple conversation messages and extended text (which may include
// link previews or mentions). Returns "" for anything else (images, voice,
// reactions, etc.).
func extractWhatsAppText(m *waProto.Message) string {
	if m == nil {
		return ""
	}
	if m.Conversation != nil {
		return strings.TrimSpace(*m.Conversation)
	}
	if m.ExtendedTextMessage != nil && m.ExtendedTextMessage.Text != nil {
		return strings.TrimSpace(*m.ExtendedTextMessage.Text)
	}
	return ""
}

// SendThreadMessage sends plain text to a WhatsApp chat (1:1 or group). The
// chat is identified by threadID.ChannelID which must be a valid JID string
// (e.g. "491701234567@s.whatsapp.net"). Long messages are split into 4000-
// char chunks, preferring line-boundary cuts. Returns the last sent message
// ID so the caller can reference it for reactions / edits.
func (w *WhatsAppService) SendThreadMessage(ctx context.Context, threadID ThreadID, message string) (string, error) {
	w.mu.RLock()
	client := w.client
	w.mu.RUnlock()
	if client == nil {
		return "", fmt.Errorf("whatsapp: client not initialised")
	}
	jid, err := types.ParseJID(threadID.ChannelID)
	if err != nil {
		return "", fmt.Errorf("whatsapp: parse JID %q: %w", threadID.ChannelID, err)
	}

	parts := splitLongText(message, 4000)
	var lastID string
	for _, part := range parts {
		msg := &waProto.Message{Conversation: protoString(part)}
		resp, err := client.SendMessage(ctx, jid, msg)
		if err != nil {
			return lastID, fmt.Errorf("whatsapp: send: %w", err)
		}
		lastID = resp.ID
	}
	return lastID, nil
}

// SendThreadMessageWithBlocks sends text plus any block content. WhatsApp
// has no block/button primitive, so blocks are flattened into numbered
// options appended to the message body — matching the pattern other
// text-only connectors use.
func (w *WhatsAppService) SendThreadMessageWithBlocks(ctx context.Context, threadID ThreadID, message string, blocks []MessageBlock) (string, error) {
	text := message
	for _, b := range blocks {
		if b.Text != "" {
			text += "\n\n" + b.Text
		}
		for i, btn := range b.Buttons {
			text += fmt.Sprintf("\n%d) %s", i+1, btn.Text)
		}
	}
	return w.SendThreadMessage(ctx, threadID, text)
}

// UpdateMessage is a no-op for WhatsApp for now. whatsmeow supports edits
// via a ProtocolMessage wrapper, but we don't currently rely on editing
// prior bot output for any flow.
func (w *WhatsAppService) UpdateMessage(ctx context.Context, threadID ThreadID, messageID, newText string) error {
	return nil
}

// AddReaction sets a reaction emoji on an inbound message. channelID is the
// chat JID, messageTS is the whatsmeow message ID. Used by the session
// manager to render the 👀 / ⏳ acks mirror-style on the user's original
// message.
func (w *WhatsAppService) AddReaction(ctx context.Context, channelID, messageTS, emoji string) error {
	senderJID, err := types.ParseJID(channelID)
	if err != nil {
		return nil
	}
	return w.sendReaction(ctx, channelID, senderJID, messageTS, emoji)
}

// RemoveReaction clears a previously-set reaction by sending an empty
// reaction payload — WhatsApp's native way to remove a reaction.
func (w *WhatsAppService) RemoveReaction(ctx context.Context, channelID, messageTS, emoji string) error {
	senderJID, err := types.ParseJID(channelID)
	if err != nil {
		return nil
	}
	return w.sendReaction(ctx, channelID, senderJID, messageTS, "")
}

func (w *WhatsAppService) sendReaction(ctx context.Context, channelID string, senderJID types.JID, messageID, emoji string) error {
	if channelID == "" || messageID == "" {
		return nil
	}
	w.mu.RLock()
	client := w.client
	w.mu.RUnlock()
	if client == nil || !client.IsConnected() {
		return nil
	}
	chatJID, err := types.ParseJID(channelID)
	if err != nil {
		return nil
	}
	reaction := client.BuildReaction(chatJID, senderJID, messageID, emoji)
	_, err = client.SendMessage(ctx, chatJID, reaction)
	return err
}

// GetThreadHistory returns an empty slice. Unlike Slack's conversations API,
// WhatsApp does not expose server-side history via the user-device protocol,
// so continuity comes from our own chat history persistence.
func (w *WhatsAppService) GetThreadHistory(ctx context.Context, threadID ThreadID) ([]ThreadMessage, error) {
	return nil, nil
}

// GetChannelName resolves a chat JID to a display name: contact pushName for
// a DM or group subject for a group. Returns "" on any lookup failure so
// callers can fall back to the JID itself.
func (w *WhatsAppService) GetChannelName(ctx context.Context, channelID string) string {
	w.mu.RLock()
	client := w.client
	w.mu.RUnlock()
	if client == nil || channelID == "" {
		return ""
	}
	jid, err := types.ParseJID(channelID)
	if err != nil {
		return ""
	}
	if jid.Server == types.GroupServer {
		info, err := client.GetGroupInfo(ctx, jid)
		if err == nil && info != nil {
			return info.GroupName.Name
		}
		return ""
	}
	if client.Store != nil && client.Store.Contacts != nil {
		info, err := client.Store.Contacts.GetContact(ctx, jid)
		if err == nil {
			if info.PushName != "" {
				return info.PushName
			}
			if info.FullName != "" {
				return info.FullName
			}
		}
	}
	return ""
}

// SetMessageHandler registers the callback invoked on every inbound text
// message. Called once during bot manager setup.
func (w *WhatsAppService) SetMessageHandler(handler BotMessageHandler) {
	w.mu.Lock()
	w.messageHandler = handler
	w.mu.Unlock()
}

// SetInteractionHandler is a stub today — WhatsApp interactive messages
// (buttons, list pickers) could route through here later, but v1 only does
// plain text in and plain text out.
func (w *WhatsAppService) SetInteractionHandler(handler BotInteractionHandler) {
	w.mu.Lock()
	w.interactionHandler = handler
	w.mu.Unlock()
}

// GetFormatter returns a WhatsApp-specific formatter that does the minimum
// transformation needed (double asterisks → single, for bold).
func (w *WhatsAppService) GetFormatter() MessageFormatter {
	return &WhatsAppFormatter{}
}

// SendNotification satisfies NotificationConnector. A notification becomes a
// DM to the paired account's own number; any button options are flattened
// into numbered choices the user replies with.
func (w *WhatsAppService) SendNotification(ctx context.Context, uniqueID, message, contextMsg string, opts *ButtonOptions) (string, error) {
	ownJID := w.OwnJID()
	if ownJID.IsEmpty() {
		return "", fmt.Errorf("whatsapp: not paired — cannot send notification")
	}
	body := message
	if contextMsg != "" {
		body = body + "\n\n" + contextMsg
	}
	if opts != nil {
		for i, label := range opts.Options {
			body += fmt.Sprintf("\n%d) %s", i+1, label)
		}
	}
	return w.SendThreadMessage(ctx, ThreadID{Platform: "whatsapp", ChannelID: ownJID.String()}, body)
}

// WhatsAppFormatter maps standard markdown onto WhatsApp's native syntax.
// WhatsApp uses *bold*, _italic_, ~strike~, and ```code``` already — very
// close to markdown — so only **bold** → *bold* needs translating.
type WhatsAppFormatter struct{}

func (f *WhatsAppFormatter) FormatMessage(markdown string) string {
	return strings.ReplaceAll(markdown, "**", "*")
}

func (f *WhatsAppFormatter) MaxMessageLength() int { return 4000 }

func (f *WhatsAppFormatter) SplitLongMessage(text string) []string {
	return splitLongText(text, f.MaxMessageLength())
}

// splitLongText breaks s into chunks no longer than maxLen bytes, preferring
// newline boundaries over hard cuts in the middle of a word.
func splitLongText(s string, maxLen int) []string {
	if maxLen <= 0 || len(s) <= maxLen {
		return []string{s}
	}
	var out []string
	for len(s) > maxLen {
		cut := strings.LastIndex(s[:maxLen], "\n")
		if cut <= 0 {
			cut = maxLen
		}
		out = append(out, s[:cut])
		s = strings.TrimLeft(s[cut:], "\n")
	}
	if len(s) > 0 {
		out = append(out, s)
	}
	return out
}

func protoString(s string) *string { return &s }
