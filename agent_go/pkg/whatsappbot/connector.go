// Package whatsappbot is the one WhatsApp connector shared by every product.
//
// It owns everything that is the same no matter who is on the other end of
// the chat: the session store, linked accounts and their reconnects, QR
// pairing, message de-duplication, the universal drop rules (groups,
// broadcasts, outgoing messages to other people), "@mention" routing with a
// per-chat memory of the active route, acknowledgement reactions and replies
// with retry. A product plugs in through small interfaces (Handler, Router,
// RouteStore, AccessPolicy, ...) and only decides what a route means and what
// happens when a message reaches it.
package whatsappbot

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/whatsapptransport"
)

// Config describes one product's WhatsApp connector.
type Config struct {
	// DBPath is the whatsmeow session database (one per product, or per user).
	DBPath string
	// AppName / AppVersion are what WhatsApp shows under "Linked devices".
	AppName    string
	AppVersion [3]uint32
	// Debug enables verbose whatsmeow logging.
	Debug bool
	// LogPrefix is prepended to every log line, e.g. "[whatsapp]".
	LogPrefix string
	// MultiAccount allows several phones to link (each gets its own device
	// row). With it off the connector manages exactly one device and only
	// offers pairing while that device is unpaired.
	MultiAccount bool
	// AutoPair starts a pairing attempt as soon as Start finds no paired
	// account, instead of waiting for the first EnsureConnecting call.
	AutoPair bool
	// PairingTimeout bounds the wait for WhatsApp to issue the first QR code.
	PairingTimeout time.Duration
	// DedupeWindow is how long a message ID is remembered; WhatsApp redelivers
	// after reconnects. Zero uses ten minutes.
	DedupeWindow time.Duration
	// OutboundText is applied to every text the connector sends on the
	// product's behalf (replies, self-chat notifications, captions).
	OutboundText func(string) string

	// Handler receives the accepted, routed messages. Required.
	Handler Handler
	// Access decides which chats may talk to the bot. Nil means self-chat only.
	Access AccessPolicy
	// Router resolves "@token" mentions to routes. Nil disables routing.
	Router Router
	// Routes remembers the active route per chat. Nil keeps it in memory.
	Routes RouteStore
}

// Connector is a running WhatsApp connector.
type Connector struct {
	cfg Config

	mu       sync.RWMutex
	store    *whatsapptransport.Store
	accounts map[string]*whatsapptransport.Account // paired, keyed by phone (JID.User)
	single   *whatsapptransport.Account            // single-account mode: the (possibly unpaired) device
	bgCtx    context.Context
	started  bool

	pairingMu      sync.Mutex
	pairingActive  bool
	pairingStarted time.Time
	pairingCancel  context.CancelFunc
	lastPairingAt  time.Time
	lastPairingErr string
	lastPairingMsg string

	qrMu      sync.RWMutex
	lastQR    string
	qrExpires time.Time

	seen *whatsapptransport.Dedupe

	routesMu sync.Mutex
	routes   map[string]string // in-memory RouteStore fallback
}

// New builds a connector; call Start to open the session store.
func New(cfg Config) *Connector {
	if cfg.LogPrefix == "" {
		cfg.LogPrefix = "[whatsapp]"
	}
	if cfg.PairingTimeout <= 0 {
		cfg.PairingTimeout = 30 * time.Second
	}
	if cfg.DedupeWindow <= 0 {
		cfg.DedupeWindow = 10 * time.Minute
	}
	if cfg.AppName == "" {
		cfg.AppName = "Chrome"
	}
	if cfg.AppVersion == ([3]uint32{}) {
		cfg.AppVersion = [3]uint32{120, 0, 0}
	}
	return &Connector{cfg: cfg, seen: whatsapptransport.NewDedupe(cfg.DedupeWindow), routes: map[string]string{}}
}

func (c *Connector) logf(format string, args ...interface{}) {
	log.Printf(c.cfg.LogPrefix+" "+format, args...)
}

// Start opens the session store and connects every paired account. The
// context outlives the call: it is the background context for reconnects
// and pairing attempts.
func (c *Connector) Start(ctx context.Context) error {
	if c.cfg.Handler == nil {
		return fmt.Errorf("whatsapp: connector needs a Handler")
	}
	st, err := whatsapptransport.OpenStore(ctx, c.cfg.DBPath, c.cfg.AppName, c.cfg.AppVersion, c.cfg.Debug)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.store = st
	c.accounts = map[string]*whatsapptransport.Account{}
	c.single = nil
	c.bgCtx = ctx
	c.started = true
	c.mu.Unlock()

	var devices []*store.Device
	if c.cfg.MultiAccount {
		devices, err = st.Devices(ctx)
		if err != nil {
			return fmt.Errorf("whatsapp: load devices: %w", err)
		}
	} else {
		device, err := st.FirstDevice(ctx)
		if err != nil {
			return fmt.Errorf("whatsapp: get device: %w", err)
		}
		if device.ID == nil {
			c.mu.Lock()
			c.single = c.buildAccount(device)
			c.mu.Unlock()
		} else {
			devices = []*store.Device{device}
		}
	}
	for _, device := range devices {
		acct := c.buildAccount(device)
		id := acct.OwnJID()
		if id.IsEmpty() {
			continue
		}
		c.mu.Lock()
		c.accounts[id.User] = acct
		if !c.cfg.MultiAccount {
			c.single = acct
		}
		c.mu.Unlock()
		go c.connect(acct)
	}
	c.logf("started (db=%s, paired=%d)", c.cfg.DBPath, len(devices))
	if c.cfg.AutoPair && len(devices) == 0 {
		c.EnsureConnecting(ctx)
	}
	return nil
}

// Stop disconnects every account and closes the session store.
func (c *Connector) Stop() {
	c.pairingMu.Lock()
	if c.pairingCancel != nil {
		c.pairingCancel()
	}
	c.pairingMu.Unlock()
	c.mu.Lock()
	accounts := c.accountsLocked()
	if c.single != nil {
		accounts = append(accounts, c.single)
	}
	st := c.store
	c.store = nil
	c.accounts = map[string]*whatsapptransport.Account{}
	c.single = nil
	c.started = false
	c.mu.Unlock()
	for _, a := range accounts {
		a.Disconnect()
	}
	if st != nil {
		_ = st.Close()
	}
	c.clearQR()
}

// Started reports whether Start has run (and Stop has not).
func (c *Connector) Started() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

func (c *Connector) buildAccount(device *store.Device) *whatsapptransport.Account {
	var acct *whatsapptransport.Account
	acct = c.store.NewAccount(device, func(rawEvt interface{}) { c.handleEvent(acct, rawEvt) })
	return acct
}

func (c *Connector) connect(a *whatsapptransport.Account) {
	if err := a.Connect(); err != nil {
		c.logf("connect failed for %s: %v", a.OwnJID().User, err)
		return
	}
	c.logf("connected as %s", a.OwnJID().String())
}

func (c *Connector) accountsLocked() []*whatsapptransport.Account {
	out := make([]*whatsapptransport.Account, 0, len(c.accounts))
	for _, a := range c.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OwnJID().User < out[j].OwnJID().User })
	return out
}

// Accounts lists the paired accounts, ordered by phone number.
func (c *Connector) Accounts() []*whatsapptransport.Account {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.accountsLocked()
}

// Primary returns the (first) paired account, or nil.
func (c *Connector) Primary() *whatsapptransport.Account {
	accounts := c.Accounts()
	if len(accounts) == 0 {
		return nil
	}
	return accounts[0]
}

// AccountFor picks the account that should speak in a chat: the one whose
// own chat it is, else the first connected one.
func (c *Connector) AccountFor(chat types.JID) *whatsapptransport.Account {
	var fallback *whatsapptransport.Account
	for _, a := range c.Accounts() {
		if a.IsSelfChat(chat) {
			return a
		}
		if fallback == nil && a.IsConnected() {
			fallback = a
		}
	}
	if fallback == nil {
		return c.Primary()
	}
	return fallback
}

// IsSelfChat reports whether chat is the "message yourself" chat of any
// paired account.
func (c *Connector) IsSelfChat(chat types.JID) bool {
	for _, a := range c.Accounts() {
		if a.IsSelfChat(chat) {
			return true
		}
	}
	return false
}

// IsPaired reports whether at least one phone is linked.
func (c *Connector) IsPaired() bool { return len(c.Accounts()) > 0 }

// IsConnected reports whether at least one linked phone is online.
func (c *Connector) IsConnected() bool {
	for _, a := range c.Accounts() {
		if a.IsConnected() {
			return true
		}
	}
	return false
}

// EnsureConnecting reconnects idle accounts and, when a new phone may link
// (always in multi-account mode, only while unpaired otherwise), makes sure
// a pairing attempt is producing a QR code. Safe to call on every status poll.
func (c *Connector) EnsureConnecting(_ context.Context) {
	c.mu.RLock()
	accounts := c.accountsLocked()
	bgCtx := c.bgCtx
	started := c.started
	c.mu.RUnlock()
	if !started {
		return
	}
	for _, a := range accounts {
		if !a.IsConnected() {
			go c.connect(a)
		}
	}
	if !c.cfg.MultiAccount && len(accounts) > 0 {
		return
	}
	c.pairingMu.Lock()
	defer c.pairingMu.Unlock()
	if c.pairingActive {
		// A live QR is fine; a stalled attempt (no code within the timeout)
		// gets canceled so the next poll starts fresh.
		if code, _ := c.GetQR(); code != "" || time.Since(c.pairingStarted) <= c.cfg.PairingTimeout {
			return
		}
		c.logf("pairing attempt stalled for %s; restarting", time.Since(c.pairingStarted).Round(time.Second))
		c.pairingCancel()
		return
	}
	pairCtx, cancel := context.WithCancel(bgCtx)
	c.pairingActive = true
	c.pairingStarted = time.Now()
	c.pairingCancel = cancel
	c.lastPairingErr = ""
	c.lastPairingMsg = ""
	go func() {
		defer func() {
			cancel()
			c.pairingMu.Lock()
			c.pairingActive = false
			c.pairingCancel = nil
			c.pairingMu.Unlock()
		}()
		c.runPairing(pairCtx)
	}()
}

func (c *Connector) runPairing(ctx context.Context) {
	c.mu.Lock()
	st := c.store
	acct := c.single
	if st == nil {
		c.mu.Unlock()
		return
	}
	if acct == nil {
		acct = c.buildAccount(st.NewDevice())
		if !c.cfg.MultiAccount {
			c.single = acct
		}
	}
	c.mu.Unlock()
	c.clearQR()
	c.logf("starting a new pairing attempt")
	sawQR := false
	paired, err := acct.Pair(ctx, c.cfg.PairingTimeout, func(u whatsapptransport.QRUpdate) {
		c.qrMu.Lock()
		c.lastQR, c.qrExpires = u.Code, u.Expires
		c.qrMu.Unlock()
		if u.Code != "" {
			sawQR = true
			c.logf("QR ready (expires %s)", u.Expires.Format(time.Kitchen))
		}
	})
	c.clearQR()
	if err != nil {
		c.pairingMu.Lock()
		c.lastPairingAt = time.Now()
		c.lastPairingErr = err.Error()
		c.lastPairingMsg = "Pairing failed"
		c.pairingMu.Unlock()
		if ctx.Err() == nil {
			c.logf("pairing attempt failed: %v", err)
		}
		return
	}
	if !paired {
		c.pairingMu.Lock()
		c.lastPairingAt = time.Now()
		c.lastPairingErr = ""
		if sawQR {
			c.lastPairingMsg = "QR was shown, but pairing did not complete (scan rejected or timed out)"
		} else {
			c.lastPairingMsg = "QR was not issued before the pairing attempt timed out"
		}
		c.pairingMu.Unlock()
		if !c.cfg.MultiAccount {
			// Keep the unpaired device for the next attempt but make sure it
			// is offline until then.
			acct.Disconnect()
		}
		return
	}
	own := acct.OwnJID()
	c.logf("paired successfully as %s", own.String())
	c.pairingMu.Lock()
	c.lastPairingAt = time.Now()
	c.lastPairingErr = ""
	c.lastPairingMsg = ""
	c.pairingMu.Unlock()
	if own.IsEmpty() {
		return
	}
	c.mu.Lock()
	c.accounts[own.User] = acct
	c.mu.Unlock()
}

func (c *Connector) clearQR() {
	c.qrMu.Lock()
	c.lastQR, c.qrExpires = "", time.Time{}
	c.qrMu.Unlock()
}

// GetQR returns the current pairing QR payload, or "" when none is live.
func (c *Connector) GetQR() (code string, expires time.Time) {
	c.qrMu.RLock()
	defer c.qrMu.RUnlock()
	if c.lastQR != "" && !c.qrExpires.IsZero() && time.Now().After(c.qrExpires) {
		return "", time.Time{}
	}
	return c.lastQR, c.qrExpires
}

// PairingInfo reports high-level pairing status for diagnostics and UI
// messaging: whether a pairing attempt is running, when it started, and the
// last failure reason (if any).
func (c *Connector) PairingInfo() (active bool, started time.Time, lastErr string, lastMsg string, lastAt time.Time) {
	if c == nil {
		return false, time.Time{}, "", "", time.Time{}
	}
	c.pairingMu.Lock()
	defer c.pairingMu.Unlock()
	return c.pairingActive, c.pairingStarted, c.lastPairingErr, c.lastPairingMsg, c.lastPairingAt
}

// GetQRImagePNG renders the current QR as PNG; nil when none is live.
func (c *Connector) GetQRImagePNG(size int) ([]byte, error) {
	code, _ := c.GetQR()
	if code == "" {
		return nil, nil
	}
	if size <= 0 {
		size = 320
	}
	return qrcode.Encode(code, qrcode.Medium, size)
}

// Unpair logs out and forgets one linked phone. The remote logout is
// best-effort: a device whose remote session is already dead (removed from
// the phone, or otherwise invalidated) must still be unpairable locally, or
// the service stays registered forever with no way to regenerate a QR.
func (c *Connector) Unpair(ctx context.Context, phone string) error {
	c.mu.RLock()
	acct, ok := c.accounts[phone]
	st := c.store
	c.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no linked account for %q", phone)
	}
	if !acct.IsConnected() {
		// Account.Connect() (whatsmeow's own client.Connect()) takes no
		// context and isn't cancellable, so a caller-supplied timeout can't
		// actually bound it directly -- racing it against ctx here at least
		// bounds how long THIS call waits. The goroutine can outlive that
		// wait if whatsmeow itself hasn't given up yet; that's an accepted
		// tradeoff since local cleanup below proceeds either way once we stop
		// waiting, and IsConnected() being false skips the Logout attempt.
		connectDone := make(chan error, 1)
		go func() { connectDone <- acct.Connect() }()
		select {
		case err := <-connectDone:
			if err != nil {
				log.Printf("[WHATSAPP] Unpair %s: remote connect failed, forcing local cleanup: %v", phone, err)
			}
		case <-ctx.Done():
			log.Printf("[WHATSAPP] Unpair %s: remote connect exceeded the caller's timeout (%v); forcing local cleanup without waiting further", phone, ctx.Err())
		}
	}
	if acct.IsConnected() {
		if err := acct.Logout(ctx); err != nil {
			log.Printf("[WHATSAPP] Unpair %s: remote logout failed, forcing local cleanup: %v", phone, err)
		}
	}
	acct.Disconnect()
	c.mu.Lock()
	if c.accounts[phone] == acct {
		delete(c.accounts, phone)
	}
	if c.single == acct {
		c.single = nil
	}
	c.mu.Unlock()
	if st != nil && acct.Device() != nil {
		if err := st.DeleteDevice(ctx, acct.Device()); err != nil {
			return fmt.Errorf("whatsapp: delete device: %w", err)
		}
	}
	return nil
}

// UnpairAll logs out and forgets every linked phone.
func (c *Connector) UnpairAll(ctx context.Context) error {
	var lastErr error
	for _, a := range c.Accounts() {
		if err := c.Unpair(ctx, a.OwnJID().User); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// ---- outbound helpers -------------------------------------------------

func (c *Connector) outbound(text string) string {
	if c.cfg.OutboundText != nil {
		return c.cfg.OutboundText(text)
	}
	return text
}

// SendText sends one message into a chat through the account that owns it.
func (c *Connector) SendText(ctx context.Context, chat types.JID, text string) error {
	a := c.AccountFor(chat)
	if a == nil {
		return fmt.Errorf("whatsapp not paired")
	}
	return a.SendText(ctx, chat, c.outbound(text))
}

// SendTextWithRetry retries a send a few times before giving up.
func (c *Connector) SendTextWithRetry(chat types.JID, text string, attempts int, attemptTimeout time.Duration) error {
	a := c.AccountFor(chat)
	if a == nil {
		return fmt.Errorf("whatsapp not paired")
	}
	return a.SendTextWithRetry(chat, c.outbound(text), attempts, attemptTimeout)
}

// SendDocument sends a file into a chat.
func (c *Connector) SendDocument(ctx context.Context, chat types.JID, data []byte, filename, mimetype, caption string) error {
	a := c.AccountFor(chat)
	if a == nil {
		return fmt.Errorf("whatsapp not paired")
	}
	return a.SendDocument(ctx, chat, data, filename, mimetype, c.outbound(caption))
}

// React sets (or with "" clears) the bot's reaction on a message.
func (c *Connector) React(ctx context.Context, chat, sender types.JID, msgID types.MessageID, emoji string) error {
	a := c.AccountFor(chat)
	if a == nil || !a.IsConnected() {
		return nil
	}
	return a.React(ctx, chat, sender, msgID, emoji)
}

func (c *Connector) connectedAccounts() []*whatsapptransport.Account {
	var out []*whatsapptransport.Account
	for _, a := range c.Accounts() {
		if a.IsConnected() {
			out = append(out, a)
		}
	}
	return out
}

// SendToAllSelf posts a text into the "message yourself" chat of every
// connected account.
func (c *Connector) SendToAllSelf(ctx context.Context, text string) (sent int, lastErr error) {
	for _, a := range c.connectedAccounts() {
		self, err := a.SelfChat()
		if err == nil {
			err = a.SendText(ctx, self, c.outbound(text))
		}
		if err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	return sent, lastErr
}

// SendDocumentToAllSelf posts a file into the self chat of every connected account.
func (c *Connector) SendDocumentToAllSelf(ctx context.Context, data []byte, filename, mimetype, caption string) (sent int, lastErr error) {
	for _, a := range c.connectedAccounts() {
		self, err := a.SelfChat()
		if err == nil {
			err = a.SendDocument(ctx, self, data, filename, mimetype, c.outbound(caption))
		}
		if err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	return sent, lastErr
}

// ---- inbound -----------------------------------------------------------

func (c *Connector) handleEvent(acct *whatsapptransport.Account, rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.Message:
		c.handleIncoming(acct, evt)
	default:
		if desc := whatsapptransport.DescribeEvent(rawEvt); desc != "" {
			c.logf("%s: %s", acct.OwnJID().User, desc)
		}
	}
}

func (c *Connector) handleIncoming(acct *whatsapptransport.Account, evt *events.Message) {
	info := evt.Info
	selfChat := acct.IsSelfChat(info.Chat)
	if info.IsFromMe && !selfChat {
		return // our own replies to other people
	}
	if info.IsGroup || info.Chat.Server == types.BroadcastServer || info.Chat.User == "status" {
		return
	}
	if c.seen.Seen(info.ID) {
		return
	}
	msg := &Message{
		conn:      c,
		Account:   acct,
		Event:     evt,
		Chat:      info.Chat,
		Sender:    info.Sender,
		ID:        info.ID,
		PushName:  info.PushName,
		Timestamp: info.Timestamp,
		FromMe:    info.IsFromMe,
		SelfChat:  selfChat,
		HasMedia:  whatsapptransport.HasMedia(evt.Message),
	}
	msg.RawText = whatsapptransport.ExtractText(evt.Message)
	msg.Text = msg.RawText
	c.dispatch(c.bgContext(), msg)
}

func (c *Connector) bgContext() context.Context {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.bgCtx == nil {
		return context.Background()
	}
	return c.bgCtx
}

// SendTextID sends one message into a chat and returns its WhatsApp ID.
func (c *Connector) SendTextID(ctx context.Context, chat types.JID, text string) (types.MessageID, error) {
	a := c.AccountFor(chat)
	if a == nil {
		return "", fmt.Errorf("whatsapp not paired")
	}
	return a.SendTextID(ctx, chat, c.outbound(text))
}

// ChatName returns a display name for a chat, "" when unknown.
func (c *Connector) ChatName(ctx context.Context, chat types.JID) string {
	a := c.AccountFor(chat)
	if a == nil {
		return ""
	}
	return a.ChatName(ctx, chat)
}

// OwnJID returns the primary account's JID (empty when unpaired).
func (c *Connector) OwnJID() types.JID {
	if a := c.Primary(); a != nil {
		return a.OwnJID()
	}
	return types.JID{}
}

// OwnLID returns the primary account's LID (empty when unpaired).
func (c *Connector) OwnLID() types.JID {
	if a := c.Primary(); a != nil {
		return a.OwnLID()
	}
	return types.JID{}
}
