package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/enginedetect"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	_ "modernc.org/sqlite" // pure-Go sqlite driver, registered as "sqlite"
)

// whatsappPairingTimeout bounds how long one pairing attempt (QR generation +
// waiting for the phone to scan it) stays alive before giving up. A fresh
// attempt (and a fresh QR) is started the next time the frontend asks.
const whatsappPairingTimeout = 30 * time.Second

// waBot is a REAL WhatsApp connection via whatsmeow (the unofficial WhatsApp
// Web multi-device protocol — the same mechanism as scanning a QR to link a
// device in the real WhatsApp app). Deliberately single-account: this app is
// one family, so there is exactly one bot instance, no per-user routing.
//
// Safety boundary: the bot only ever acts in the paired account's own
// "Message Yourself" chat (see isSelfChat) — never a real contact or group.
// Without that restriction, Quill would start replying to whoever else the
// linked phone talks to, which would be a serious safety problem for a
// family app. This mirrors AgentWorks' whatsapp_service.go pattern, stripped
// of its multi-tenant/routing-table machinery this app doesn't need.
type waBot struct {
	mu     sync.RWMutex
	client *whatsmeow.Client
	dbPath string
	// bgCtx is a long-lived context for the connection/pairing goroutine —
	// deliberately NOT derived from any HTTP request's context. A request's
	// context is canceled the instant that response is written, which would
	// silently kill an in-flight whatsmeow Connect()/GetQRChannel() call if it
	// were used here instead (this was a real bug caught in testing: the
	// pairing goroutine died silently on every request with no log at all).
	bgCtx context.Context

	pairingMu sync.Mutex
	qrMu      sync.RWMutex
	lastQR    string
	qrExpires time.Time
}

var whatsAppBot = &waBot{}

func whatsAppSessionPath() string {
	return filepath.Join(familyDataDir(), "whatsapp", "session.db")
}

// initWhatsAppBot loads (or creates) the local device store and builds the
// whatsmeow client. It does NOT connect to WhatsApp's servers — that only
// happens lazily, the first time the parent opens the WhatsApp settings
// section, via EnsureConnecting. Called once at server startup so IsPaired()
// reflects real state immediately.
func initWhatsAppBot(ctx context.Context) error {
	dbPath := whatsAppSessionPath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return fmt.Errorf("whatsapp: mkdir session dir: %w", err)
	}

	store.SetOSInfo("SparkQuill", [3]uint32{1, 0, 0})
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", dbPath)
	container, err := sqlstore.New(ctx, "sqlite", dsn, waLog.Noop)
	if err != nil {
		return fmt.Errorf("whatsapp: open session store: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp: load device: %w", err)
	}
	client := whatsmeow.NewClient(device, waLog.Noop)
	client.AddEventHandler(whatsAppBot.handleEvent)

	whatsAppBot.mu.Lock()
	whatsAppBot.client = client
	whatsAppBot.dbPath = dbPath
	whatsAppBot.bgCtx = context.Background()
	whatsAppBot.mu.Unlock()
	return nil
}

// EnsureConnecting starts (or resumes) the connection in the background —
// idempotent and safe to call on every status/pair poll. If already paired,
// this simply keeps/gets the live connection up so incoming messages arrive.
// If not paired, it runs one QR-pairing attempt (bounded by
// whatsappPairingTimeout); the next poll starts a fresh attempt if it lapsed.
func (w *waBot) EnsureConnecting(_ context.Context) {
	w.mu.RLock()
	client := w.client
	bgCtx := w.bgCtx
	w.mu.RUnlock()
	if client == nil {
		return
	}
	if client.IsConnected() {
		return
	}
	if !w.pairingMu.TryLock() {
		return // an attempt is already in flight
	}
	go func() {
		defer w.pairingMu.Unlock()
		w.connectOnce(bgCtx, client)
	}()
}

func (w *waBot) connectOnce(ctx context.Context, client *whatsmeow.Client) {
	log.Printf("[whatsapp] connectOnce starting (paired=%v)", client.Store.ID != nil)
	if client.Store.ID != nil {
		if err := client.Connect(); err != nil {
			log.Printf("[whatsapp] connect failed: %v", err)
		} else if id := client.Store.ID; id != nil {
			log.Printf("[whatsapp] connected as %s", id.String())
		}
		return
	}

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		log.Printf("[whatsapp] GetQRChannel failed: %v", err)
		return
	}
	connectDone := make(chan error, 1)
	go func() { connectDone <- client.Connect() }()
	select {
	case err := <-connectDone:
		if err != nil {
			log.Printf("[whatsapp] connect (pre-pair) failed: %v", err)
			return
		}
	case <-time.After(whatsappPairingTimeout):
		client.Disconnect()
		return
	case <-ctx.Done():
		return
	}

	timeout := time.NewTimer(whatsappPairingTimeout)
	defer timeout.Stop()
	for {
		select {
		case evt, ok := <-qrChan:
			if !ok {
				return
			}
			switch evt.Event {
			case "code":
				w.qrMu.Lock()
				w.lastQR = evt.Code
				w.qrExpires = time.Now().Add(evt.Timeout)
				w.qrMu.Unlock()
				log.Printf("[whatsapp] QR ready (expires in %s)", evt.Timeout)
			case "success":
				log.Printf("[whatsapp] paired successfully")
				w.qrMu.Lock()
				w.lastQR = ""
				w.qrExpires = time.Time{}
				w.qrMu.Unlock()
				return
			case "timeout":
				w.qrMu.Lock()
				w.lastQR = ""
				w.qrExpires = time.Time{}
				w.qrMu.Unlock()
				return
			}
		case <-timeout.C:
			client.Disconnect()
			return
		case <-ctx.Done():
			return
		}
	}
}

func (w *waBot) IsPaired() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.client != nil && w.client.Store != nil && w.client.Store.ID != nil
}

func (w *waBot) IsConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.client != nil && w.client.IsConnected()
}

func (w *waBot) OwnJID() types.JID {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.client == nil || w.client.Store == nil || w.client.Store.ID == nil {
		return types.JID{}
	}
	return *w.client.Store.ID
}

// OwnLID returns the paired account's own LID identity (the "@lid" JID modern
// WhatsApp uses alongside the phone-number JID). Self-chat messages arrive on
// the LID identity, so isSelfChat must match it too — see the LID handling
// AgentWorks' whatsapp_service.go also does.
func (w *waBot) OwnLID() types.JID {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.client == nil || w.client.Store == nil {
		return types.JID{}
	}
	return w.client.Store.LID
}

func (w *waBot) GetQR() (code string, expires time.Time) {
	w.qrMu.RLock()
	defer w.qrMu.RUnlock()
	if w.lastQR != "" && !w.qrExpires.IsZero() && time.Now().After(w.qrExpires) {
		return "", time.Time{}
	}
	return w.lastQR, w.qrExpires
}

func (w *waBot) GetQRImagePNG(size int) ([]byte, error) {
	code, _ := w.GetQR()
	if code == "" {
		return nil, nil
	}
	if size <= 0 {
		size = 320
	}
	return qrcode.Encode(code, qrcode.Medium, size)
}

// isSelfChat is the safety boundary described on waBot: true only for the
// paired account's own "Message Yourself" chat. Modern WhatsApp routes the
// self-chat through the account's LID identity (chat server "lid"), so match
// BOTH the classic phone-number JID and the LID — otherwise self-chat
// messages arrive as "@lid" and get silently rejected.
func (w *waBot) isSelfChat(chat types.JID) bool {
	if chat.User == "" {
		return false
	}
	if own := w.OwnJID(); !own.IsEmpty() && chat.Server == types.DefaultUserServer && chat.User == own.User {
		return true
	}
	if lid := w.OwnLID(); !lid.IsEmpty() && chat.Server == types.HiddenUserServer && chat.User == lid.User {
		return true
	}
	return false
}

func (w *waBot) Unpair(ctx context.Context) error {
	w.mu.Lock()
	client := w.client
	dbPath := w.dbPath
	w.client = nil
	w.mu.Unlock()

	if client != nil {
		_ = client.Logout(ctx)
		client.Disconnect()
	}

	w.qrMu.Lock()
	w.lastQR = ""
	w.qrExpires = time.Time{}
	w.qrMu.Unlock()

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("whatsapp: remove %s: %w", dbPath+suffix, err)
		}
	}
	return initWhatsAppBot(ctx)
}

// SendToSelf pushes a message into the paired account's own "Message
// Yourself" chat proactively — used by Pulse (pulse.go) when the parent's
// most recently active conversation is the real WhatsApp thread, so an
// unsolicited check-in actually reaches them instead of silently sitting in
// a chat history file nobody's looking at.
func (w *waBot) SendToSelf(ctx context.Context, text string) error {
	w.mu.RLock()
	client := w.client
	w.mu.RUnlock()
	if client == nil || !w.IsPaired() {
		return fmt.Errorf("whatsapp not paired")
	}
	own := w.OwnJID()
	if own.IsEmpty() {
		return fmt.Errorf("whatsapp own JID unknown")
	}
	msg := &waProto.Message{Conversation: &text}
	_, err := client.SendMessage(ctx, own, msg)
	return err
}

// react adds (or, with emoji "", clears) a whatsmeow emoji reaction on an
// incoming message — the "got it / working on it" acknowledgement, since an
// agent turn can take a minute or two and there'd otherwise be no sign the
// message was received. Mirrors AgentWorks' WhatsApp reaction ack. Best-effort.
func (w *waBot) react(chat, sender types.JID, msgID types.MessageID, emoji string) {
	w.mu.RLock()
	client := w.client
	w.mu.RUnlock()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reaction := client.BuildReaction(chat, sender, msgID, emoji)
	if _, err := client.SendMessage(ctx, chat, reaction); err != nil {
		log.Printf("[whatsapp] reaction failed: %v", err)
	}
}

// --- incoming messages -------------------------------------------------

func (w *waBot) handleEvent(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.Message:
		w.handleIncomingMessage(evt)
	case *events.LoggedOut:
		log.Printf("[whatsapp] logged out (reason=%v) — session invalid, re-pair required", evt.Reason)
	}
}

// ingestWhatsAppMedia downloads an image/document attachment from an incoming
// WhatsApp message into shared/inbox and reports whether one was saved.
// Deliberately does NOT tell the model about it or the path — only text
// messages go to the agent; the file just lands in the inbox and the
// process-file skill's own "check shared/inbox/ before every reply" habit
// picks it up naturally on whatever the next real turn is, the same as any
// other inbox arrival. Best-effort: a failed download just drops the
// attachment (any accompanying text still gets handled normally).
func (w *waBot) ingestWhatsAppMedia(evt *events.Message) bool {
	m := evt.Message
	if m == nil {
		return false
	}
	var dl whatsmeow.DownloadableMessage
	var name string
	switch {
	case m.ImageMessage != nil:
		dl = m.ImageMessage
		name = "wa-" + evt.Info.ID + extForMime(m.ImageMessage.GetMimetype(), ".jpg")
	case m.DocumentMessage != nil:
		dl = m.DocumentMessage
		name = strings.TrimSpace(m.DocumentMessage.GetFileName())
		if name == "" {
			name = "wa-" + evt.Info.ID + extForMime(m.DocumentMessage.GetMimetype(), ".bin")
		}
	default:
		return false
	}

	w.mu.RLock()
	client := w.client
	w.mu.RUnlock()
	if client == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, err := client.Download(ctx, dl)
	if err != nil {
		log.Printf("[whatsapp] media download failed: %v", err)
		return false
	}

	name = sanitizeInboxName(name)
	relDir := filepath.Join("shared", "inbox")
	absDir := filepath.Join(familyDataDir(), "workspace", relDir)
	if err := os.MkdirAll(absDir, 0o700); err != nil {
		log.Printf("[whatsapp] inbox mkdir failed: %v", err)
		return false
	}
	if err := os.WriteFile(filepath.Join(absDir, name), data, 0o600); err != nil {
		log.Printf("[whatsapp] media save failed: %v", err)
		return false
	}
	relPath := filepath.ToSlash(filepath.Join(relDir, name))
	log.Printf("[whatsapp] saved attachment to %s (%d bytes)", relPath, len(data))
	return true
}

// extForMime maps a media mimetype to a file extension, falling back to def.
func extForMime(mime, def string) string {
	switch {
	case strings.Contains(mime, "jpeg"), strings.Contains(mime, "jpg"):
		return ".jpg"
	case strings.Contains(mime, "png"):
		return ".png"
	case strings.Contains(mime, "webp"):
		return ".webp"
	case strings.Contains(mime, "gif"):
		return ".gif"
	case strings.Contains(mime, "pdf"):
		return ".pdf"
	default:
		return def
	}
}

// sanitizeInboxName strips path separators and dodgy characters from an
// attachment filename so it can't escape shared/inbox.
func sanitizeInboxName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.NewReplacer("/", "_", "\\", "_", "..", "_", " ", "_").Replace(name)
	if name == "" || name == "." {
		name = "wa-attachment"
	}
	return name
}

func extractWhatsAppMessageText(m *waProto.Message) string {
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

func (w *waBot) handleIncomingMessage(evt *events.Message) {
	info := evt.Info
	if info.IsFromMe && !w.isSelfChat(info.Chat) {
		return // outgoing message to a real contact/group — never act on these
	}
	if info.IsGroup || info.Chat.Server == types.BroadcastServer {
		return
	}
	if !w.isSelfChat(info.Chat) {
		return // a real contact DMed the linked account — never reply as Quill
	}
	text := extractWhatsAppMessageText(evt.Message)

	// An image or document attachment: save it straight into shared/inbox and
	// stop there — only text messages ever reach the agent. A bare attachment
	// with no caption just gets acknowledged (👀) below and never starts a
	// turn; the file sits in the inbox until the next real text message, at
	// which point the process-file skill's own "check shared/inbox/ before
	// every reply" habit picks it up like any other inbox arrival.
	gotMedia := w.ingestWhatsAppMedia(evt)

	if strings.TrimSpace(text) == "" {
		if gotMedia {
			w.react(info.Chat, info.Sender, info.ID, "👀")
		}
		return
	}

	// Eager acknowledgement: 👀 on the parent's message the instant we accept it,
	// so they see it was received while the (possibly 1-2 min) turn runs. If the
	// turn runs long, layer on ⏳ ("still working"). Cleared when the reply is
	// sent; swapped to ⚠️ if the turn fails. Mirrors AgentWorks' reaction ack.
	w.react(info.Chat, info.Sender, info.ID, "👀")
	longRunDone := make(chan struct{})
	go func() {
		select {
		case <-longRunDone:
		case <-time.After(12 * time.Second):
			w.react(info.Chat, info.Sender, info.ID, "⏳")
		}
	}()

	reply, err := w.runTurn(text)
	close(longRunDone)
	if err != nil {
		w.react(info.Chat, info.Sender, info.ID, "⚠️")
		log.Printf("[whatsapp] turn failed: %v", err)
		return
	}
	w.react(info.Chat, info.Sender, info.ID, "") // clear the ack — the reply is the completion signal

	w.mu.RLock()
	client := w.client
	w.mu.RUnlock()
	if client == nil {
		return
	}
	msg := &waProto.Message{Conversation: &reply}
	sendCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := client.SendMessage(sendCtx, info.Chat, msg); err != nil {
		log.Printf("[whatsapp] send failed: %v", err)
	}
}

// runTurn runs one real agent turn for a message received over the actual
// linked WhatsApp account — the same agentic runtime as the in-app WhatsApp
// simulator (handleWhatsAppMessage), just triggered by a whatsmeow event
// instead of an HTTP request from the frontend.
func (w *waBot) runTurn(text string) (string, error) {
	agentTurnMu.Lock()
	defer agentTurnMu.Unlock()

	stateMu.Lock()
	s := loadState()
	stateMu.Unlock()
	if s.Engine == "" {
		return "", fmt.Errorf("no learning engine selected")
	}
	provider, ok := engineToProvider(s.Engine)
	if !ok {
		return "", fmt.Errorf("engine %q has no provider mapping", s.Engine)
	}

	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
	defer cancel()

	// WhatsApp joins the SINGLE parent conversation (same file + same warm tmux
	// session as the web chat and Pulse) so Quill has one unified memory across
	// every channel — a message on WhatsApp continues the same thread as the web.
	convID := parentConversationID
	workDir := filepath.Join(familyDataDir(), "workspace")

	// Build the full history like the web chat does. In resume mode Ask sends
	// only the newest message (the coding agent restores prior context from its
	// warm tmux, or its `--resume` session store after a restart via the loaded
	// SessionHandle) — so the older messages here are for the persisted transcript
	// / UI, not re-sent to the model. WhatsApp has no frontend to supply the
	// thread, so we load it from the persisted file (the UI's source of truth).
	existing, _ := loadStoredConversation("parent", convID)
	prior := existing.Messages
	history := make([]agentsession.Message, 0, len(prior)+1)
	for _, m := range prior {
		if m.Role == "user" || m.Role == "assistant" {
			history = append(history, agentsession.Message{Role: m.Role, Text: m.Text})
		}
	}
	// Per-turn WhatsApp formatting hint sent to the model but NOT persisted (so
	// the stored/visible message stays clean). Because the tmux session is shared
	// with the web chat, the base system prompt may be the web one; this keeps
	// replies phone-appropriate regardless.
	history = append(history, agentsession.Message{Role: "user", Text: text + "\n\n(Replying over WhatsApp on the phone — keep it short and plain text: no markdown, headings, or file paths.)"})

	persistFull := func(reply string) {
		full := append([]enginedetect.ChatMessage(nil), prior...)
		full = append(full,
			enginedetect.ChatMessage{Role: "user", Text: text},
			enginedetect.ChatMessage{Role: "assistant", Text: reply})
		persistConversation("parent", convID, full)
	}

	sess, err := agentsession.New(ctx, agentsession.Config{
		Provider:   provider,
		WorkingDir: workDir,
		// Same base persona as the web chat — it's one unified conversation, so
		// the prompt shouldn't fork by channel; WhatsApp formatting is the per-turn
		// hint appended to the message above.
		SystemPrompt: parentSystemPrompt(s.Child, s.ParentLabel, s.Pulse),
		// Stable SessionID = the single parent conversation, so the SAME warm
		// tmux session is used across turns AND channels within this process.
		// SessionHandle restores the coding agent's `--resume` state across
		// restarts (loaded from disk) so context survives a restart — the
		// AgentWorks mechanism. Ask sends only the newest message; the CLI
		// reconstructs history from its own session store.
		SessionID:                 convID,
		SessionHandle:             loadSessionHandle("parent", convID),
		BridgeRoutingInstructions: bridgeRoutingInstructions(),
		Tools:                     withLiveStatus("parent:"+convID, []agentsession.Tool{webSearchTool(), readImageTool(s.Engine), notifyTool(), shellTool()}),
	})
	if err != nil {
		persistFull(friendlyTurnError(err))
		return "", err
	}
	defer sess.Close()

	reply, err := sess.Ask(ctx, history)
	if err != nil {
		persistFull(friendlyTurnError(err))
		return "", err
	}
	saveSessionHandle("parent", convID, sess.Handle())
	persistFull(reply)
	return reply, nil
}

// --- HTTP routes ---------------------------------------------------------

type whatsAppStatusResponse struct {
	Paired      bool   `json:"paired"`
	Connected   bool   `json:"connected"`
	QRAvailable bool   `json:"qr_available"`
	QRExpiresAt string `json:"qr_expires_at,omitempty"`
	OwnJID      string `json:"own_jid,omitempty"`
}

// GET /api/whatsapp/status
func handleWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Always ensure the connection is live — EnsureConnecting is idempotent and
	// no-ops if already connected. When paired it (re)establishes the live link
	// so incoming messages arrive; when unpaired it drives the QR flow.
	whatsAppBot.EnsureConnecting(r.Context())
	resp := whatsAppStatusResponse{
		Paired:    whatsAppBot.IsPaired(),
		Connected: whatsAppBot.IsConnected(),
	}
	if own := whatsAppBot.OwnJID(); !own.IsEmpty() {
		resp.OwnJID = own.User
	}
	if code, expires := whatsAppBot.GetQR(); code != "" {
		resp.QRAvailable = true
		resp.QRExpiresAt = expires.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GET /api/whatsapp/pair — a PNG pairing QR code, or 404 if none is
// available yet (already paired, or the code just hasn't arrived — the
// frontend polls this alongside /status).
func handleWhatsAppPair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if whatsAppBot.IsPaired() {
		http.Error(w, "already paired", http.StatusConflict)
		return
	}
	whatsAppBot.EnsureConnecting(r.Context())
	png, err := whatsAppBot.GetQRImagePNG(320)
	if err != nil {
		http.Error(w, "failed to render QR: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if png == nil {
		http.Error(w, "no pairing QR available yet — try again in a moment", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

// POST /api/whatsapp/unpair
func handleWhatsAppUnpair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := whatsAppBot.Unpair(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
