package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

// waAccount is ONE linked WhatsApp account — one parent's own phone, linked
// via its own QR scan. Immutable after construction (client is set once and
// never reassigned in place — unpairing removes the *waAccount from the
// manager's map entirely rather than mutating it), so its methods need no
// locking of their own.
type waAccount struct {
	client *whatsmeow.Client
}

func (a *waAccount) OwnJID() types.JID {
	if a == nil || a.client == nil || a.client.Store == nil || a.client.Store.ID == nil {
		return types.JID{}
	}
	return *a.client.Store.ID
}

// OwnLID returns this account's own LID identity (the "@lid" JID modern
// WhatsApp uses alongside the phone-number JID). Self-chat messages arrive on
// the LID identity, so isSelfChat must match it too — see the LID handling
// AgentWorks' whatsapp_service.go also does.
func (a *waAccount) OwnLID() types.JID {
	if a == nil || a.client == nil || a.client.Store == nil {
		return types.JID{}
	}
	return a.client.Store.LID
}

// isSelfChat is the safety boundary described on waBot: true only for THIS
// account's own "Message Yourself" chat. Modern WhatsApp routes the self-chat
// through the account's LID identity (chat server "lid"), so match BOTH the
// classic phone-number JID and the LID — otherwise self-chat messages arrive
// as "@lid" and get silently rejected.
func (a *waAccount) isSelfChat(chat types.JID) bool {
	if chat.User == "" {
		return false
	}
	if own := a.OwnJID(); !own.IsEmpty() && chat.Server == types.DefaultUserServer && chat.User == own.User {
		return true
	}
	if lid := a.OwnLID(); !lid.IsEmpty() && chat.Server == types.HiddenUserServer && chat.User == lid.User {
		return true
	}
	return false
}

// react adds (or, with emoji "", clears) a whatsmeow emoji reaction on an
// incoming message — the "got it / working on it" acknowledgement, since an
// agent turn can take a minute or two and there'd otherwise be no sign the
// message was received. Mirrors AgentWorks' WhatsApp reaction ack. Best-effort.
func (a *waAccount) react(chat, sender types.JID, msgID types.MessageID, emoji string) {
	if a == nil || a.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reaction := a.client.BuildReaction(chat, sender, msgID, emoji)
	if _, err := a.client.SendMessage(ctx, chat, reaction); err != nil {
		log.Printf("[whatsapp] reaction failed: %v", err)
	}
}

// SendToSelf pushes a message into this account's own "Message Yourself"
// chat proactively — used by notify_user/Pulse.
func (a *waAccount) SendToSelf(ctx context.Context, text string) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("whatsapp not paired")
	}
	own := a.OwnJID()
	if own.IsEmpty() {
		return fmt.Errorf("whatsapp own JID unknown")
	}
	// OwnJID is this device's own JID (has a ":<device>" part, e.g.
	// "919717071555:24@s.whatsapp.net") — whatsmeow rejects that as a
	// message recipient ("message recipient must be a user JID with no
	// device part"). ToNonAD strips it down to the plain addressable user
	// JID, which is what SendMessage actually wants.
	own = own.ToNonAD()
	msg := &waProto.Message{Conversation: &text}
	_, err := a.client.SendMessage(ctx, own, msg)
	return err
}

// SendDocumentToSelf uploads a file and sends it as a WhatsApp document
// attachment to this account's own "message yourself" chat — the same
// self-chat-only pattern as SendToSelf. Used for handing over a test/study
// material as a real PDF instead of only describing it in text.
func (a *waAccount) SendDocumentToSelf(ctx context.Context, data []byte, filename, mimetype, caption string) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("whatsapp not paired")
	}
	own := a.OwnJID()
	if own.IsEmpty() {
		return fmt.Errorf("whatsapp own JID unknown")
	}
	own = own.ToNonAD() // strip the ":<device>" part — see SendToSelf's comment
	uploaded, err := a.client.Upload(ctx, data, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("upload document: %w", err)
	}
	fileLength := uint64(len(data))
	doc := &waProto.DocumentMessage{
		URL:           &uploaded.URL,
		DirectPath:    &uploaded.DirectPath,
		MediaKey:      uploaded.MediaKey,
		Mimetype:      &mimetype,
		FileName:      &filename,
		FileSHA256:    uploaded.FileSHA256,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileLength:    &fileLength,
	}
	if strings.TrimSpace(caption) != "" {
		doc.Caption = &caption
	}
	msg := &waProto.Message{DocumentMessage: doc}
	_, err = a.client.SendMessage(ctx, own, msg)
	return err
}

// ingestWhatsAppMedia downloads an image/document/voice-note attachment from
// an incoming WhatsApp message into the inbox and reports whether one was
// saved. For images/documents this deliberately does NOT tell the model about
// it or the path — only text messages go to the agent; the file just lands in
// the inbox and the process-file skill's own "check inbox/ before every
// reply" habit picks it up naturally on whatever the next real turn is, the
// same as any other inbox arrival. A voice note is different: it IS meant to
// drive a turn, so on top of saving the raw audio, it's transcribed locally
// (see transcribeAudioFile) and the transcript is returned so the caller can
// treat it as if the parent had typed it. Best-effort throughout: a failed
// download or transcription just degrades gracefully (any accompanying text
// still gets handled normally; a voice note that fails to transcribe still
// gets saved and silently acknowledged, same as an image/document).
func (a *waAccount) ingestWhatsAppMedia(evt *events.Message) (saved bool, voiceText string) {
	m := evt.Message
	if m == nil {
		return false, ""
	}
	var dl whatsmeow.DownloadableMessage
	var name string
	isVoice := false
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
	case m.AudioMessage != nil:
		dl = m.AudioMessage
		isVoice = true
		name = "wa-voice-" + evt.Info.ID + extForMime(m.AudioMessage.GetMimetype(), ".ogg")
	default:
		return false, ""
	}
	if a.client == nil {
		return false, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, err := a.client.Download(ctx, dl)
	if err != nil {
		log.Printf("[whatsapp] media download failed: %v", err)
		return false, ""
	}

	name = sanitizeInboxName(name)
	relDir := "inbox"
	absDir := filepath.Join(familyDataDir(), "workspace", relDir)
	if err := os.MkdirAll(absDir, 0o700); err != nil {
		log.Printf("[whatsapp] inbox mkdir failed: %v", err)
		return false, ""
	}
	absPath := filepath.Join(absDir, name)
	if err := os.WriteFile(absPath, data, 0o600); err != nil {
		log.Printf("[whatsapp] media save failed: %v", err)
		return false, ""
	}
	relPath := filepath.ToSlash(filepath.Join(relDir, name))
	log.Printf("[whatsapp] saved attachment to %s (%d bytes)", relPath, len(data))

	if isVoice {
		stateMu.Lock()
		voiceOn := whatsAppVoiceEnabled(loadState())
		stateMu.Unlock()
		if !voiceOn {
			log.Printf("[whatsapp] voice transcription disabled by parent — saved audio only")
		} else {
			transcript, err := transcribeAudioFile(ctx, absPath)
			if err != nil {
				log.Printf("[whatsapp] voice transcription unavailable: %v", err)
			} else if strings.TrimSpace(transcript) != "" {
				voiceText = strings.TrimSpace(transcript)
				log.Printf("[whatsapp] transcribed voice note (%d chars)", len(voiceText))
			}
		}
	}
	return true, voiceText
}

// waBot manages every linked WhatsApp account (one per parent) via whatsmeow
// (the unofficial WhatsApp Web multi-device protocol — the same mechanism as
// scanning a QR to link a device in the real WhatsApp app). Each parent links
// their OWN phone; all of them share the single "parent" conversation Quill
// already uses for web chat + Pulse (one unified family memory, not
// per-parent silos — the SAME shared conversation, just reachable from
// multiple linked phones).
//
// Safety boundary: each account only ever acts in ITS OWN paired "Message
// Yourself" chat (see waAccount.isSelfChat) — never a real contact or group.
// Without that restriction, Quill would start replying to whoever else a
// linked phone talks to, which would be a serious safety problem for a
// family app. This mirrors AgentWorks' whatsapp_service.go pattern, stripped
// of its multi-tenant/routing-table machinery this app doesn't need.
type waBot struct {
	mu        sync.RWMutex
	container *sqlstore.Container
	accounts  map[string]*waAccount // keyed by phone number (JID.User) once paired
	// bgCtx is a long-lived context for the connection/pairing goroutines —
	// deliberately NOT derived from any HTTP request's context. A request's
	// context is canceled the instant that response is written, which would
	// silently kill an in-flight whatsmeow Connect()/GetQRChannel() call if it
	// were used here instead (this was a real bug caught in testing: the
	// pairing goroutine died silently on every request with no log at all).
	bgCtx context.Context

	pairingMu sync.Mutex
	pending   *waAccount // the in-progress (not-yet-paired) pairing slot; nil when none
	qrMu      sync.RWMutex
	lastQR    string
	qrExpires time.Time
}

var whatsAppBot = &waBot{}

func whatsAppSessionPath() string {
	return filepath.Join(familyDataDir(), "whatsapp", "session.db")
}

// initWhatsAppBot opens (or creates) the local device store and reconnects
// every already-paired account found in it. It does NOT block on any one
// account's connect — each reconnects in its own goroutine so a slow/offline
// phone never delays server startup or the others. Called once at server
// startup so status reflects real state immediately.
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
	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp: load devices: %w", err)
	}

	whatsAppBot.mu.Lock()
	whatsAppBot.container = container
	whatsAppBot.accounts = map[string]*waAccount{}
	whatsAppBot.bgCtx = context.Background()
	whatsAppBot.mu.Unlock()

	for _, device := range devices {
		acct := whatsAppBot.buildAccount(device)
		if id := acct.OwnJID(); !id.IsEmpty() {
			whatsAppBot.mu.Lock()
			whatsAppBot.accounts[id.User] = acct
			whatsAppBot.mu.Unlock()
		}
		go func(a *waAccount) {
			if err := a.client.Connect(); err != nil {
				log.Printf("[whatsapp] connect failed for %s: %v", a.OwnJID().User, err)
			} else {
				log.Printf("[whatsapp] connected as %s", a.OwnJID().String())
			}
		}(acct)
	}
	return nil
}

// buildAccount wraps a device in a client and registers its event handler,
// bound to THIS account so incoming events are always checked against the
// right JID/LID — never returns an already-registered account.
func (w *waBot) buildAccount(device *store.Device) *waAccount {
	client := whatsmeow.NewClient(device, waLog.Noop)
	acct := &waAccount{client: client}
	client.AddEventHandler(func(rawEvt interface{}) { w.handleEvent(acct, rawEvt) })
	return acct
}

// EnsureConnecting reconnects any disconnected paired accounts and, if no
// pairing attempt is currently in flight, starts one for a possible NEW
// phone — so opening/polling the Connectors WhatsApp panel both keeps
// existing links alive and offers a fresh QR to add one more parent.
// Idempotent and safe to call on every status/pair poll.
func (w *waBot) EnsureConnecting(_ context.Context) {
	w.mu.RLock()
	accounts := make([]*waAccount, 0, len(w.accounts))
	for _, a := range w.accounts {
		accounts = append(accounts, a)
	}
	bgCtx := w.bgCtx
	w.mu.RUnlock()

	for _, a := range accounts {
		if a.client != nil && !a.client.IsConnected() {
			go func(a *waAccount) { _ = a.client.Connect() }(a)
		}
	}

	if !w.pairingMu.TryLock() {
		return // a pairing attempt is already in flight
	}
	go func() {
		defer w.pairingMu.Unlock()
		w.startPairingAttempt(bgCtx)
	}()
}

// startPairingAttempt runs one QR-pairing attempt for a BRAND NEW phone —
// never touches already-linked accounts (each gets its own fresh
// container.NewDevice()). On success the newly-paired account is added to
// the accounts map under its own phone number; on timeout/failure nothing
// changes and the next EnsureConnecting call starts a fresh attempt.
func (w *waBot) startPairingAttempt(ctx context.Context) {
	w.mu.RLock()
	container := w.container
	w.mu.RUnlock()
	if container == nil {
		return
	}
	device := container.NewDevice()
	acct := w.buildAccount(device)

	w.mu.Lock()
	w.pending = acct
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.pending = nil
		w.mu.Unlock()
	}()

	client := acct.client
	log.Printf("[whatsapp] starting a new pairing attempt")
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
				own := acct.OwnJID()
				log.Printf("[whatsapp] paired successfully as %s", own.String())
				w.qrMu.Lock()
				w.lastQR = ""
				w.qrExpires = time.Time{}
				w.qrMu.Unlock()
				if !own.IsEmpty() {
					w.mu.Lock()
					w.accounts[own.User] = acct
					w.mu.Unlock()
				}
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

// IsPaired reports whether at least one phone is linked.
func (w *waBot) IsPaired() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.accounts) > 0
}

// IsConnected reports whether at least one linked phone currently has a live
// connection.
func (w *waBot) IsConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, a := range w.accounts {
		if a.client != nil && a.client.IsConnected() {
			return true
		}
	}
	return false
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

// Unpair removes ONE linked account by its phone number (JID.User) — logs it
// out, disconnects, and deletes just its own device row, leaving every OTHER
// linked account untouched. (The old single-account design deleted the whole
// session DB file on unpair, which would have wiped out every other parent
// too — that's exactly the regression this per-account deletion avoids.)
func (w *waBot) Unpair(ctx context.Context, phone string) error {
	w.mu.Lock()
	acct, ok := w.accounts[phone]
	container := w.container
	if ok {
		delete(w.accounts, phone)
	}
	w.mu.Unlock()
	if !ok || acct == nil || acct.client == nil {
		return fmt.Errorf("no linked account for %q", phone)
	}

	_ = acct.client.Logout(ctx)
	acct.client.Disconnect()
	if container != nil && acct.client.Store != nil {
		if err := container.DeleteDevice(ctx, acct.client.Store); err != nil {
			return fmt.Errorf("whatsapp: delete device: %w", err)
		}
	}
	return nil
}

// SendToAllSelf broadcasts a text notification to every currently-connected
// linked account (every linked parent) — used by notify_user/Pulse. Returns
// how many succeeded and the last error seen (if any), for an honest
// per-channel delivery status.
func (w *waBot) SendToAllSelf(ctx context.Context, text string) (sent int, lastErr error) {
	for _, a := range w.connectedAccounts() {
		if err := a.SendToSelf(ctx, text); err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	return sent, lastErr
}

// SendDocumentToAllSelf is SendToAllSelf for a document attachment — used by
// send_whatsapp_file. Sends to every currently-connected linked account, not
// just whichever one (if any) originated the turn — matching notify_user's
// existing "goes to every channel you've set up" philosophy rather than
// threading "which account asked" through the whole tool-call chain.
func (w *waBot) SendDocumentToAllSelf(ctx context.Context, data []byte, filename, mimetype, caption string) (sent int, lastErr error) {
	for _, a := range w.connectedAccounts() {
		if err := a.SendDocumentToSelf(ctx, data, filename, mimetype, caption); err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	return sent, lastErr
}

func (w *waBot) connectedAccounts() []*waAccount {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]*waAccount, 0, len(w.accounts))
	for _, a := range w.accounts {
		if a.client != nil && a.client.IsConnected() {
			out = append(out, a)
		}
	}
	return out
}

// sendWhatsAppFileTool lets the parent-mode agent hand over a test or study
// guide as a real PDF attachment on WhatsApp, instead of only describing it
// in text — e.g. "send me the fractions test as a PDF on WhatsApp". Scoped to
// files inside an activity folder only (not an answer key elsewhere, not any
// arbitrary file) — those stay off WhatsApp — and only sends to linked
// accounts' own self-chats (SendDocumentToAllSelf) — never a third party.
// onSent, when non-nil, is called with the workspace-relative path of each
// file successfully sent — so the caller (a web-chat or WhatsApp turn) can
// append a real, clickable reference to the persisted reply afterward. The
// model's own reply text alone doesn't reliably do this: the system prompt
// tells it to keep file paths out of prose, so without this the file was
// sent but genuinely invisible anywhere in the chat transcript/UI.
func sendWhatsAppFileTool(onSent func(path string)) agentsession.Tool {
	return agentsession.Tool{
		Name: "send_whatsapp_file",
		Description: "Send a test or study material file to the parent as a real PDF attachment on their own WhatsApp " +
			"(their linked \"message yourself\" chat) — only call this when the parent explicitly asks for a file/PDF over " +
			"WhatsApp. The file must already exist as a PDF (use agent_browser: open the file, then run its \"pdf\" command " +
			"to export a PDF into the same folder, e.g. <Subject>/<Topic>/<activity>/<name>.pdf, before calling this). " +
			"Requires WhatsApp to be linked (Connectors → WhatsApp) — if it's not, tell the parent to link it there first.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "workspace-relative path to the PDF, e.g. Math/Fractions/2026-07-23-quick-check/quick-check.pdf"},
				"caption": map[string]interface{}{"type": "string", "description": "optional short caption to send with the file"},
			},
			"required": []string{"path"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			if !whatsAppBot.IsConnected() {
				return "", fmt.Errorf("whatsapp is not linked")
			}
			rel := strings.TrimSpace(fmt.Sprint(args["path"]))
			caption, _ := args["caption"].(string)
			if !strings.HasSuffix(strings.ToLower(rel), ".pdf") {
				return "", fmt.Errorf("path must be a .pdf file")
			}
			if findActivityForPath(rel) == "" {
				return "", fmt.Errorf("send_whatsapp_file only sends files inside an activity folder")
			}
			abs, ok := resolveWorkspacePath(rel)
			if !ok {
				return "", fmt.Errorf("invalid path")
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				return "", fmt.Errorf("read %s: %w", rel, err)
			}
			sendCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			sent, sendErr := whatsAppBot.SendDocumentToAllSelf(sendCtx, data, filepath.Base(rel), "application/pdf", caption)
			if sent == 0 {
				if sendErr != nil {
					return "", fmt.Errorf("send whatsapp document: %w", sendErr)
				}
				return "", fmt.Errorf("send whatsapp document: no connected accounts")
			}
			if onSent != nil {
				onSent(rel)
			}
			return `{"status":"sent"}`, nil
		},
	}
}

// --- incoming messages -------------------------------------------------

func (w *waBot) handleEvent(acct *waAccount, rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.Message:
		w.handleIncomingMessage(acct, evt)
	case *events.LoggedOut:
		log.Printf("[whatsapp] %s logged out (reason=%v) — session invalid, re-pair required", acct.OwnJID().User, evt.Reason)
	}
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
	case strings.Contains(mime, "ogg"):
		return ".ogg"
	default:
		return def
	}
}

// sanitizeInboxName strips path separators and dodgy characters from an
// attachment filename so it can't escape the inbox.
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

func (w *waBot) handleIncomingMessage(acct *waAccount, evt *events.Message) {
	info := evt.Info
	if info.IsFromMe && !acct.isSelfChat(info.Chat) {
		return // outgoing message to a real contact/group — never act on these
	}
	if info.IsGroup || info.Chat.Server == types.BroadcastServer {
		return
	}
	if !acct.isSelfChat(info.Chat) {
		return // a real contact DMed this linked account — never reply as Quill
	}
	text := extractWhatsAppMessageText(evt.Message)

	// An image or document attachment: save it straight into the inbox and
	// stop there — only text messages ever reach the agent. A bare attachment
	// with no caption just gets acknowledged (👀) below and never starts a
	// turn; the file sits in the inbox until the next real text message, at
	// which point the process-file skill's own "check inbox/ before
	// every reply" habit picks it up like any other inbox arrival. A voice
	// note is the exception: its local transcript (see ingestWhatsAppMedia)
	// stands in for typed text below, so it drives a turn just like normal.
	gotMedia, voiceText := acct.ingestWhatsAppMedia(evt)
	if strings.TrimSpace(text) == "" && voiceText != "" {
		text = "🎙️ " + voiceText
		// Confirm what was actually heard right away, decoupled from the real
		// turn below — a turn can take minutes, and speech-to-text isn't
		// perfect, so the parent should see (and can correct/retype) what
		// Quill transcribed without waiting for the full reply.
		if acct.client != nil {
			confirmCtx, confirmCancel := context.WithTimeout(context.Background(), 15*time.Second)
			heard := fmt.Sprintf("🎙️ I heard: “%s”", voiceText)
			if _, err := acct.client.SendMessage(confirmCtx, info.Chat, &waProto.Message{Conversation: &heard}); err != nil {
				log.Printf("[whatsapp] voice transcript confirmation send failed: %v", err)
			}
			confirmCancel()
		}
	}

	if strings.TrimSpace(text) == "" {
		if gotMedia {
			acct.react(info.Chat, info.Sender, info.ID, "👀")
		}
		return
	}

	// If a turn is already running for the shared parent conversation (started
	// from the web app, or another linked phone), try to inject this message
	// into it LIVE instead of just queuing behind agentTurnMu — the same
	// steer mechanism the web app's composer uses (see steer.go). This is
	// genuinely different from queuing: without it, a second WhatsApp message
	// sent while Quill is still mid-reply just waits its turn and gets
	// processed as a completely separate turn afterward, even though the
	// parent very likely meant to redirect or add to what's already running.
	if trySteer(context.Background(), parentConversationID, text) {
		appendUserMessageToConversation("parent", parentConversationID, text)
		acct.react(info.Chat, info.Sender, info.ID, "↩️") // delivered live — no separate reply coming for THIS message
		return
	}

	// Eager acknowledgement: 👀 on the parent's message the instant we accept it,
	// so they see it was received while the (possibly 1-2 min) turn runs. If the
	// turn runs long, layer on ⏳ ("still working"). Cleared when the reply is
	// sent; swapped to ⚠️ if the turn fails. Mirrors AgentWorks' reaction ack.
	acct.react(info.Chat, info.Sender, info.ID, "👀")
	longRunDone := make(chan struct{})
	go func() {
		select {
		case <-longRunDone:
		case <-time.After(12 * time.Second):
			acct.react(info.Chat, info.Sender, info.ID, "⏳")
		}
	}()

	reply, err := w.runTurn(text)
	close(longRunDone)
	if err != nil {
		acct.react(info.Chat, info.Sender, info.ID, "⚠️")
		log.Printf("[whatsapp] turn failed: %v", err)
		return
	}
	acct.react(info.Chat, info.Sender, info.ID, "") // clear the ack — the reply is the completion signal

	if acct.client == nil {
		return
	}
	msg := &waProto.Message{Conversation: &reply}
	sendCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := acct.client.SendMessage(sendCtx, info.Chat, msg); err != nil {
		log.Printf("[whatsapp] send failed: %v", err)
	}
}

// runTurn runs one real agent turn for a message received over any linked
// WhatsApp account — the same agentic runtime as the in-app WhatsApp
// simulator (handleWhatsAppMessage), just triggered by a whatsmeow event
// instead of an HTTP request from the frontend. Every account funnels into
// the SAME shared "parent" conversation, so it never needs to know which
// account triggered it.
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
	history = append(history, agentsession.Message{Role: "user", Text: text + "\n\n(Replying over WhatsApp on the phone — keep it short and plain text: no markdown, headings, or file paths. IMPORTANT: there is no screen/panel here — calling open_file does NOT show the parent anything, it's a silent no-op on this channel. NEVER say \"I've opened it\" or \"it's ready and open\" here — that's only true on the web app. Instead, either describe what's in the file directly in your reply, or if the parent wants the actual file, use send_whatsapp_file to send it as a real PDF attachment (export to PDF via agent_browser first if it isn't one already). If the message above starts with 🎙️, that prefix means it's a LOCAL, ON-DEVICE TRANSCRIPT of a voice note the parent just sent — the text after it is genuinely what they said, already fully readable by you. Respond directly to its content exactly as you would a typed message; do NOT say you can't listen to or process voice/audio messages — you just did.)"})

	// Persist the parent's message the INSTANT the turn starts (not just at
	// completion) — same fix as the web chat path (see persistNewMessages'
	// own doc comment): otherwise a steer landing mid-turn, or simply this
	// snapshot going stale relative to disk, could get silently overwritten
	// by fallback []enginedetect.ChatMessage built once here at turn start.
	fallback := append([]enginedetect.ChatMessage(nil), prior...)
	fallback = append(fallback, enginedetect.ChatMessage{Role: "user", Text: text})
	persistNewMessages("parent", convID, fallback)
	persistFull := func(reply string) {
		persistConversationReply("parent", convID, fallback, reply)
	}

	var sentFilesMu sync.Mutex
	var sentFiles []string

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
		Tools: withLiveStatus("parent:"+convID, []agentsession.Tool{webSearchTool(), readImageTool(s.Engine), notifyTool(), shellTool(), agentBrowserTool(), sendWhatsAppFileTool(func(path string) {
			sentFilesMu.Lock()
			sentFiles = append(sentFiles, path)
			sentFilesMu.Unlock()
		})}),
	})
	if err != nil {
		persistFull(friendlyTurnError(err))
		return "", err
	}
	defer sess.Close()

	// Register this turn as steerable too — it's the same "parent" conversation
	// id as the web chat, so a follow-up the parent sends there while this
	// WhatsApp-originated turn is still running can be injected live (see
	// steer.go) instead of only ever queuing.
	registerActiveTurn(convID, sess.Agent())
	defer clearActiveTurn()

	reply, err := sess.Ask(ctx, history)
	if err != nil {
		persistFull(friendlyTurnError(err))
		return "", err
	}
	saveSessionHandle("parent", convID, sess.Handle())
	reply = sanitizeAgentReply(reply)
	reply = appendSentFileLinks(reply, sentFiles)
	persistFull(reply)
	return reply, nil
}

// --- HTTP routes ---------------------------------------------------------

type whatsAppAccountStatus struct {
	JID       string `json:"jid"`
	Connected bool   `json:"connected"`
}

type whatsAppPairingStatus struct {
	QRAvailable bool   `json:"qr_available"`
	QRExpiresAt string `json:"qr_expires_at,omitempty"`
}

type whatsAppStatusResponse struct {
	Accounts           []whatsAppAccountStatus  `json:"accounts"`
	Pairing            whatsAppPairingStatus    `json:"pairing"`
	VoiceTranscription voiceTranscriptionStatus `json:"voice_transcription"`
}

// GET /api/whatsapp/status
func handleWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Always ensure connections are live and a pairing attempt for a possible
	// new phone is in flight — EnsureConnecting is idempotent.
	whatsAppBot.EnsureConnecting(r.Context())

	whatsAppBot.mu.RLock()
	resp := whatsAppStatusResponse{Accounts: make([]whatsAppAccountStatus, 0, len(whatsAppBot.accounts))}
	for jid, acct := range whatsAppBot.accounts {
		resp.Accounts = append(resp.Accounts, whatsAppAccountStatus{
			JID:       jid,
			Connected: acct.client != nil && acct.client.IsConnected(),
		})
	}
	whatsAppBot.mu.RUnlock()
	sort.Slice(resp.Accounts, func(i, j int) bool { return resp.Accounts[i].JID < resp.Accounts[j].JID })

	if code, expires := whatsAppBot.GetQR(); code != "" {
		resp.Pairing.QRAvailable = true
		resp.Pairing.QRExpiresAt = expires.UTC().Format(time.RFC3339)
	}

	stateMu.Lock()
	s := loadState()
	stateMu.Unlock()
	resp.VoiceTranscription = currentVoiceTranscriptionStatus(s)

	writeJSON(w, http.StatusOK, resp)
}

// GET /api/whatsapp/pair — a PNG pairing QR code for the CURRENT pairing
// attempt (adding one more phone), or 404 if none is available yet — the
// frontend polls this alongside /status. Always available regardless of how
// many phones are already linked (unlike the old single-account version,
// pairing another phone never requires unlinking an existing one first).
func handleWhatsAppPair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

// POST /api/whatsapp/unpair — body {"jid": "<phone number>"}, unpairs just
// that one linked account.
func handleWhatsAppUnpair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		JID string `json:"jid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.JID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "jid is required"})
		return
	}
	if err := whatsAppBot.Unpair(r.Context(), strings.TrimSpace(req.JID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
