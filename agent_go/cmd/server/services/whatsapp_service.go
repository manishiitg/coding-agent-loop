package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
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

	// Pure-Go SQLite driver (registered as "sqlite"). SQLite is an exception
	// to the project's "no-database, workspace-file only" convention — see
	// WhatsAppService doc below for the reason. Pure-Go is chosen over
	// mattn/go-sqlite3 so the agent binary stays CGO-free and builds the
	// same way across hosts.
	_ "modernc.org/sqlite"
)

// WhatsAppService implements BotConnector on top of whatsmeow — a Go library
// that speaks the multi-device WhatsApp Web protocol. The bot operates
// through a personal WhatsApp account (paired once via QR code); no Meta
// Business API, verified business, or approved templates are required.
//
// Tradeoff: whatsmeow uses the unofficial WhatsApp Web protocol. Meta may ban
// numbers exhibiting bot-like behaviour at scale, so this is best suited to
// personal / internal use, not customer-facing commercial volume.
//
// NOTE on storage: this is the ONE place in the server that uses a database.
// Everywhere else we deliberately persist to workspace/ files only; WhatsApp
// is the exception because whatsmeow's multi-device protocol state (Signal
// identity keys, session keys, prekey bundles, device records) is structured
// key-value material with transactional update requirements that can't be
// reasonably expressed as flat JSON files. The sqlstore sits at a local-to-
// agent path (see StartListening / dbPath), not shared infrastructure — so
// it's closer to a "protocol state file" than a "database" in the
// architectural sense. If the file is lost or corrupted, the user re-pairs
// by deleting it and scanning a new QR.
type WhatsAppService struct {
	dbPath string
	// selfChatPrefix (if non-empty) is prepended to every bot reply in the
	// "Message Yourself" chat so the user can visually separate bot output
	// from their own typing (both render as "from me"). Empty by default;
	// set WHATSAPP_SELF_CHAT_PREFIX in .env to re-enable, e.g. "🤖 " or
	// "[AgentForge] ". Read once at StartListening so live-toggling requires
	// a restart.
	selfChatPrefix string

	mu        sync.RWMutex
	container *sqlstore.Container
	device    *store.Device
	client    *whatsmeow.Client
	// metaDB is a lightweight second connection to the same SQLite file
	// holding non-whatsmeow metadata (currently the owner binding). Using
	// one store keeps everything in a single file that Unpair can wipe
	// atomically. WAL mode on the DSN lets whatsmeow and metaDB share the
	// file without lock contention.
	metaDB *sql.DB

	qrMu      sync.RWMutex
	lastQR    string
	qrExpires time.Time

	messageHandler     BotMessageHandler
	interactionHandler BotInteractionHandler

	// Routing maps user-chosen slugs (e.g. "rca") to a workflow + mode.
	// A message starting with "@<slug> ..." peels the slug, looks it up, and
	// routes to the specified workflow instead of the default multi-agent
	// chat. Editable from the frontend "Workflow routing" card; persisted in
	// the whatsapp_meta table.
	routingMu sync.RWMutex
	routing   WhatsAppRouting

	// Owner binds the paired WhatsApp account to a specific workspace user.
	// Every incoming message stamps this user's email + ID so the bot manager
	// can route to that user's per-user chat history, memory, and schedules.
	// Populated at pair time (whoever is authenticated when /api/whatsapp/pair
	// is first called wins) and persisted inside the same SQLite file
	// whatsmeow already uses (whatsapp_meta table) — single source of truth,
	// Unpair deletes it along with everything else.
	ownerMu sync.RWMutex
	owner   *WhatsAppOwner
}

// WhatsAppOwner records which workspace user owns the paired WhatsApp
// account. Serialised to JSON alongside the SQLite session file.
type WhatsAppOwner struct {
	UserID   string    `json:"user_id"`
	Email    string    `json:"email"`
	Username string    `json:"username,omitempty"`
	PairedAt time.Time `json:"paired_at"`
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

	// Self-chat reply prefix. Empty = no prefix (default). Mirrors OpenClaw's
	// configurable messages.responsePrefix — timing + bubble rhythm usually
	// makes bot replies obvious, but set this env var if you want explicit
	// labelling. A trailing space is up to you; this is concatenated verbatim.
	w.selfChatPrefix = os.Getenv("WHATSAPP_SELF_CHAT_PREFIX")

	// Open the metadata side of the same SQLite file and load the owner
	// binding (if any) so every incoming message can be tagged with the
	// workspace user. No row = pairing hasn't been claimed yet; the first
	// authenticated pair request claims it.
	if err := w.openMetaStore(ctx); err != nil {
		return err
	}
	w.loadOwner(ctx)
	w.loadRouting(ctx)

	// Name shown in the paired phone's "Linked Devices" list. Default would be
	// the literal string "whatsmeow", which looks suspicious; override it so
	// the user can clearly identify this bot. Takes effect only on fresh
	// pairings — if a device is already paired, it keeps whatever name was
	// registered when it first linked, and you need to unpair + re-pair to
	// see the new name.
	//
	// Note: Meta's fraud heuristics are more trusting of common OS names
	// ("Chrome", "Linux", "Windows") than arbitrary strings. If this name
	// turns out to trigger "can't link new devices" on new pairings, we may
	// need to switch back to a more generic one.
	store.SetOSInfo("AgentForge", [3]uint32{1, 0, 0})

	// Build the whatsmeow loggers. Default is silent; set WHATSAPP_DEBUG=true
	// to see the underlying protocol exchange (QR flow, handshake, device-add
	// IQ stanzas). This is what surfaces server-side reasons for errors like
	// "can't link new devices" — otherwise the user-facing message on the
	// phone is the only hint we get.
	logger := waLog.Noop
	clientLogger := waLog.Noop
	if os.Getenv("WHATSAPP_DEBUG") == "true" {
		logger = waLog.Stdout("WhatsApp-DB", "DEBUG", true)
		clientLogger = waLog.Stdout("WhatsApp", "DEBUG", true)
		log.Printf("[WHATSAPP] Verbose whatsmeow logging enabled (WHATSAPP_DEBUG=true)")
	}

	// Local SQLite file holding whatsmeow's Signal-protocol state. This is the
	// only DB in the server — kept because whatsmeow can't function without a
	// transactional key/session store. The file is agent-local (not in the
	// workspace/ HTTP mount), so it's closer to a "protocol state cache" than
	// shared infrastructure. WAL mode avoids blocking on concurrent reads.
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", w.dbPath)
	container, err := sqlstore.New(ctx, "sqlite", dsn, logger)
	if err != nil {
		return fmt.Errorf("whatsapp: open sqlstore: %w", err)
	}

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp: get device: %w", err)
	}

	client := whatsmeow.NewClient(device, clientLogger)
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

// Unpair disconnects the client, drops the session DB, and re-initialises a
// fresh empty store so the next pairing attempt starts with a clean slate.
// After Unpair returns, the service is back in "unpaired" state — the next
// pairing QR will be generated on the next reconnect.
func (w *WhatsAppService) Unpair(ctx context.Context) error {
	// Detach and tear down the current client first.
	w.mu.Lock()
	if w.client != nil {
		w.client.Disconnect()
	}
	w.client = nil
	w.device = nil
	w.container = nil
	w.mu.Unlock()

	// Clear any QR state so the status endpoint doesn't briefly claim a stale QR.
	w.qrMu.Lock()
	w.lastQR = ""
	w.qrExpires = time.Time{}
	w.qrMu.Unlock()

	// Close the metadata connection before removing the file, so no open
	// handle blocks the delete on strict filesystems. clearOwner also drops
	// the cached owner pointer — the row inside the DB goes away with the
	// file.
	w.closeMetaStore()
	w.clearOwner()

	// Delete the SQLite store (main file + WAL/SHM siblings). WAL mode leaves
	// two extra files alongside the main one; wipe all three so the next
	// StartListening gets a virgin store — and a fresh empty whatsapp_meta
	// table with no owner binding.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := w.dbPath + suffix
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("whatsapp: remove %s: %w", path, err)
		}
	}

	// Re-start with a fresh store so the pairing QR flow can begin immediately
	// — users don't need to restart the server to re-pair.
	return w.StartListening(ctx)
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

// metaKeyOwner is the row key under which the owner binding JSON is stored
// in the whatsapp_meta table.
const metaKeyOwner = "owner"

// metaKeyRouting holds the JSON map of slug → ChannelRoute used to route
// incoming messages to specific workflows via an @<slug> prefix.
const metaKeyRouting = "routing"

// WhatsAppRouting is the full slug → ChannelRoute map persisted to the meta
// table. A nil / empty map means "no routing — all messages go to the
// default multi-agent chat flow".
type WhatsAppRouting map[string]ChannelRoute

// openMetaStore opens the lightweight metadata connection and ensures the
// whatsapp_meta table exists. Called from StartListening after sqlstore has
// been set up, so the underlying SQLite file is already initialised.
func (w *WhatsAppService) openMetaStore(ctx context.Context) error {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", w.dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("whatsapp: open meta db: %w", err)
	}
	// Tiny pool — metadata access is rare. Keeping this small avoids
	// competing with whatsmeow's own connection pool on the same file.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS whatsapp_meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return fmt.Errorf("whatsapp: create meta table: %w", err)
	}

	w.mu.Lock()
	w.metaDB = db
	w.mu.Unlock()
	return nil
}

// closeMetaStore disconnects the metadata pool. Called during Unpair so the
// SQLite file can be safely removed.
func (w *WhatsAppService) closeMetaStore() {
	w.mu.Lock()
	db := w.metaDB
	w.metaDB = nil
	w.mu.Unlock()
	if db != nil {
		_ = db.Close()
	}
}

// loadOwner pulls the owner binding from the whatsapp_meta table. A missing
// row is not an error — it just means nobody has claimed the pairing yet.
// Called from StartListening after openMetaStore.
func (w *WhatsAppService) loadOwner(ctx context.Context) {
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return
	}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM whatsapp_meta WHERE key = ?`, metaKeyOwner).Scan(&raw)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("[WHATSAPP] Failed to read owner row: %v (treating as unclaimed)", err)
		}
		return
	}
	var o WhatsAppOwner
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		log.Printf("[WHATSAPP] Failed to parse owner row: %v (ignoring)", err)
		return
	}
	w.ownerMu.Lock()
	w.owner = &o
	w.ownerMu.Unlock()
	log.Printf("[WHATSAPP] Loaded owner binding: user=%s email=%s (paired %s)", o.UserID, o.Email, o.PairedAt.Format(time.RFC3339))
}

// loadRouting pulls the slug → workflow map from whatsapp_meta. Missing row
// means no routes are configured — normal for a fresh install.
func (w *WhatsAppService) loadRouting(ctx context.Context) {
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return
	}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM whatsapp_meta WHERE key = ?`, metaKeyRouting).Scan(&raw)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("[WHATSAPP] Failed to read routing row: %v", err)
		}
		return
	}
	var m WhatsAppRouting
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		log.Printf("[WHATSAPP] Failed to parse routing row: %v (ignoring)", err)
		return
	}
	w.routingMu.Lock()
	w.routing = m
	w.routingMu.Unlock()
	log.Printf("[WHATSAPP] Loaded %d workflow route(s)", len(m))
}

// GetRouting returns a copy of the slug → workflow map. Safe for UI display.
func (w *WhatsAppService) GetRouting() WhatsAppRouting {
	w.routingMu.RLock()
	defer w.routingMu.RUnlock()
	out := make(WhatsAppRouting, len(w.routing))
	for k, v := range w.routing {
		out[k] = v
	}
	return out
}

// SetRouting replaces the entire routing map (called from the UI save path)
// and persists it to the meta table. A nil or empty map clears all routes.
// Invalid slugs are rejected; valid characters are [a-z0-9-] so they're
// safe to match as the first token of a WhatsApp message.
func (w *WhatsAppService) SetRouting(routing WhatsAppRouting) error {
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("whatsapp: meta store not open")
	}
	cleaned := make(WhatsAppRouting, len(routing))
	for slug, route := range routing {
		slug = strings.ToLower(strings.TrimSpace(slug))
		if slug == "" {
			continue
		}
		if !isValidSlug(slug) {
			return fmt.Errorf("whatsapp: invalid slug %q — use lowercase letters, digits, and hyphens only", slug)
		}
		if strings.TrimSpace(route.WorkflowID) == "" {
			return fmt.Errorf("whatsapp: slug %q must map to a workflow_id", slug)
		}
		cleaned[slug] = route
	}
	raw, err := json.Marshal(cleaned)
	if err != nil {
		return fmt.Errorf("whatsapp: marshal routing: %w", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO whatsapp_meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		metaKeyRouting, string(raw),
	); err != nil {
		return fmt.Errorf("whatsapp: persist routing: %w", err)
	}
	w.routingMu.Lock()
	w.routing = cleaned
	w.routingMu.Unlock()
	log.Printf("[WHATSAPP] Saved %d workflow route(s)", len(cleaned))
	return nil
}

// isValidSlug validates a user-provided slug used as the @<slug> prefix.
// Restricted to lowercase alphanumerics + hyphen so slugs can't collide
// with whitespace-separated tokens or inject regex-y control chars.
func isValidSlug(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

// resolveSlugRoute returns the ChannelRoute for the given slug (case-
// insensitive), or nil when unknown.
func (w *WhatsAppService) resolveSlugRoute(slug string) *ChannelRoute {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return nil
	}
	w.routingMu.RLock()
	defer w.routingMu.RUnlock()
	if route, ok := w.routing[slug]; ok {
		r := route
		return &r
	}
	return nil
}

// GetOwner returns the currently-bound owner, or nil when unclaimed.
func (w *WhatsAppService) GetOwner() *WhatsAppOwner {
	w.ownerMu.RLock()
	defer w.ownerMu.RUnlock()
	if w.owner == nil {
		return nil
	}
	// Return a copy so callers can't mutate the cached value.
	o := *w.owner
	return &o
}

// ClaimOwnership records the given user as the owner of the pairing. It is
// idempotent for re-claiming by the same user, and fails with an explicit
// error if a *different* user tries to claim an already-bound pairing — so
// two workspace users can't accidentally collide on one WhatsApp pairing.
// The caller should block the pair flow on a failed claim and surface the
// message (e.g. "already paired to alice@example.com — unpair first").
func (w *WhatsAppService) ClaimOwnership(userID, email, username string) error {
	if userID == "" {
		return fmt.Errorf("whatsapp: cannot claim ownership without a user ID")
	}
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("whatsapp: meta store not open — StartListening has not run")
	}

	w.ownerMu.Lock()
	existing := w.owner
	if existing != nil && existing.UserID != "" && existing.UserID != userID {
		w.ownerMu.Unlock()
		return fmt.Errorf("whatsapp: already paired to %s — unpair first to transfer ownership", existing.Email)
	}
	o := &WhatsAppOwner{UserID: userID, Email: email, Username: username, PairedAt: time.Now().UTC()}
	if existing != nil {
		// Re-claim by same user: preserve the original PairedAt.
		o.PairedAt = existing.PairedAt
	}
	w.owner = o
	w.ownerMu.Unlock()

	raw, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("whatsapp: marshal owner: %w", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO whatsapp_meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		metaKeyOwner, string(raw),
	); err != nil {
		return fmt.Errorf("whatsapp: persist owner: %w", err)
	}
	if existing == nil {
		log.Printf("[WHATSAPP] Claimed ownership: user=%s email=%s", userID, email)
	}
	return nil
}

// clearOwner removes the owner binding. Used when the pairing is reset from
// memory but the DB file is being deleted immediately after (e.g. Unpair);
// a no-op on the row level is fine because the whole file is about to go.
func (w *WhatsAppService) clearOwner() {
	w.ownerMu.Lock()
	w.owner = nil
	w.ownerMu.Unlock()
}

// OwnJID returns the paired account's own phone-number JID (the
// s.whatsapp.net one), or an empty JID if unpaired.
func (w *WhatsAppService) OwnJID() types.JID {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.client == nil || w.client.Store == nil || w.client.Store.ID == nil {
		return types.JID{}
	}
	return *w.client.Store.ID
}

// OwnLID returns the paired account's LID ("hidden user") JID if one has
// been assigned. Recent WhatsApp accounts have both a phone-number JID
// (@s.whatsapp.net) and a LID (@lid); self-chat messages can arrive with
// either as the chat JID depending on how the message was routed. Empty
// when unpaired or when the account hasn't been given a LID.
func (w *WhatsAppService) OwnLID() types.JID {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.client == nil || w.client.Store == nil {
		return types.JID{}
	}
	return w.client.Store.LID
}

// isSelfChat reports whether the given chat JID is the "Message Yourself"
// chat — the one whose counterpart is the paired account itself. WhatsApp
// routes both the user's and the bot's messages into the same thread there,
// and self-chat is our enabler for letting the owner talk to their own bot
// without needing a second phone number.
//
// Handles both of the paired account's identities: the phone-number JID
// (chat.Server = "s.whatsapp.net") and the LID (chat.Server = "lid").
// Recent WhatsApp accounts can arrive on either depending on how the
// message was routed through the multi-device / privacy protocol.
func (w *WhatsAppService) isSelfChat(chat types.JID) bool {
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
	case *events.ConnectFailure:
		// Explicit rejection from the WhatsApp server — captured as a
		// first-class event separate from Disconnected. The Reason code
		// tells us exactly why (e.g. "bad-user-agent", "multi-device-mismatch",
		// "client-outdated"). Surface it; this is the signal we usually care
		// about when pairing silently fails.
		log.Printf("[WHATSAPP] Connect failure: reason=%v message=%s", evt.Reason, evt.Message)
	case *events.ClientOutdated:
		log.Printf("[WHATSAPP] Client outdated — whatsmeow needs upgrading against the current WhatsApp server")
	case *events.TemporaryBan:
		log.Printf("[WHATSAPP] Temporary ban: code=%v expires=%s — account is restricted by WhatsApp", evt.Code, evt.Expire)
	case *events.StreamError:
		// Protocol-level stream error (often shown to the user as "can't link
		// new devices"). Code is the XMPP-ish error code from WhatsApp's XML.
		log.Printf("[WHATSAPP] Stream error: code=%s raw=%v", evt.Code, evt.Raw)
	case *events.StreamReplaced:
		log.Printf("[WHATSAPP] Stream replaced — another client took over this session")
	case *events.PairSuccess:
		log.Printf("[WHATSAPP] Pair success: id=%s platform=%s businessName=%s", evt.ID, evt.Platform, evt.BusinessName)
	case *events.PairError:
		// This is the one most likely to fire on the "can't link new devices"
		// error. Logs the full reason returned by the WhatsApp server.
		log.Printf("[WHATSAPP] Pair error: id=%s platform=%s error=%v", evt.ID, evt.Platform, evt.Error)
	}
}

// handleIncomingMessage converts a whatsmeow message event into a
// BotIncomingMessage and forwards it to the registered handler. v1: 1:1 DMs
// only — group chats, broadcasts, and status updates are skipped. Also adds
// an "eyes" reaction mirror of the Slack ack UX so the sender sees their
// message was received.
func (w *WhatsAppService) handleIncomingMessage(evt *events.Message) {
	info := evt.Info
	// Trace entry so we can tell this handler fired at all; the debug fields
	// tell us why a message was (or wasn't) forwarded when it silently
	// disappears. Kept at log.Printf so it shows without WHATSAPP_DEBUG.
	log.Printf("[WHATSAPP] handleIncomingMessage: chat=%s sender=%s fromMe=%v isGroup=%v category=%s type=%s",
		info.Chat.String(), info.Sender.String(), info.IsFromMe, info.IsGroup, info.Category, info.Type)

	if info.IsFromMe {
		// Self-chat mode: allow messages the user sends in the "Message
		// Yourself" chat — this is how the user talks to the bot when it's
		// paired to their personal WhatsApp account. Any OTHER outgoing
		// message (to contacts, groups, etc.) is ignored so we don't react to
		// the user's regular WhatsApp usage.
		if !w.isSelfChat(info.Chat) {
			log.Printf("[WHATSAPP] skip: outgoing message to non-self chat %s", info.Chat.String())
			return
		}
	}
	if info.IsGroup || info.Chat.Server == types.BroadcastServer {
		log.Printf("[WHATSAPP] skip: group/broadcast chat %s (server=%s)", info.Chat.String(), info.Chat.Server)
		return
	}
	if info.Chat.User == "status" {
		log.Printf("[WHATSAPP] skip: status broadcast")
		return
	}

	text := extractWhatsAppText(evt.Message)
	if text == "" {
		// Help diagnose silent drops — common for non-text messages (media,
		// reactions, receipts) that still fire Message events.
		msgType := "<nil>"
		if evt.Message != nil {
			switch {
			case evt.Message.Conversation != nil:
				msgType = "conversation-empty"
			case evt.Message.ExtendedTextMessage != nil:
				msgType = "extended-text-empty"
			case evt.Message.ImageMessage != nil:
				msgType = "image"
			case evt.Message.AudioMessage != nil:
				msgType = "audio"
			case evt.Message.VideoMessage != nil:
				msgType = "video"
			case evt.Message.DocumentMessage != nil:
				msgType = "document"
			case evt.Message.ReactionMessage != nil:
				msgType = "reaction"
			case evt.Message.ProtocolMessage != nil:
				msgType = "protocol"
			default:
				msgType = "other"
			}
		}
		log.Printf("[WHATSAPP] skip: no text body (payload=%s)", msgType)
		return
	}

	w.mu.RLock()
	handler := w.messageHandler
	w.mu.RUnlock()
	if handler == nil {
		log.Printf("[WHATSAPP] skip: no message handler registered")
		return
	}

	// Reject messages until the pairing has been claimed by a workspace user.
	// Without an owner binding we have no workspace user to route this
	// conversation to (chat history, memory, schedules are per-user). In
	// practice this path only fires when someone paired via the API without
	// hitting the authenticated /api/whatsapp/pair route — rare but worth
	// surfacing rather than silently dropping.
	owner := w.GetOwner()
	if owner == nil {
		log.Printf("[WHATSAPP] Dropping message from %s — pairing has no workspace-user owner; re-pair via the UI to claim", info.Sender.User)
		return
	}

	chatJID := info.Chat.String()
	senderUser := info.Sender.User
	senderName := info.PushName

	// @<slug> workflow routing. If the first token looks like "@foo", look
	// up "foo" in the routing map; on a hit we strip the @foo (plus one
	// trailing space) and set PresetWorkflow so the bot manager routes the
	// rest of the message to the mapped workflow. No match → leave the text
	// intact and fall through to default multi-agent chat, so the agent can
	// see the typed mention and say "I don't know @foo".
	var presetRoute *ChannelRoute
	routedSlug := ""
	if trimmed := strings.TrimSpace(text); strings.HasPrefix(trimmed, "@") {
		firstSpace := strings.IndexAny(trimmed, " \t\n")
		var slugToken string
		if firstSpace < 0 {
			slugToken = trimmed[1:]
		} else {
			slugToken = trimmed[1:firstSpace]
		}
		if route := w.resolveSlugRoute(slugToken); route != nil {
			presetRoute = route
			routedSlug = slugToken
			if firstSpace < 0 {
				text = ""
			} else {
				text = strings.TrimSpace(trimmed[firstSpace+1:])
			}
		}
	}

	if routedSlug != "" {
		log.Printf("[WHATSAPP] Incoming message from %s (%s) → user=%s, routed via @%s to workflow %s: %s",
			senderUser, chatJID, owner.UserID, routedSlug, presetRoute.WorkflowID, botTruncate(text, 80))
	} else {
		log.Printf("[WHATSAPP] Incoming message from %s (%s) → user=%s: %s",
			senderUser, chatJID, owner.UserID, botTruncate(text, 80))
	}

	// Eager ack with 👀 reaction — parity with the Slack UX. Best effort.
	_ = w.sendReaction(context.Background(), chatJID, info.Sender, info.ID, "👀")

	handler(BotIncomingMessage{
		Platform:        "whatsapp",
		UserID:          senderUser,
		UserName:        senderName,
		UserEmail:       owner.Email,  // binds the conversation to the workspace user
		WorkspaceUserID: owner.UserID, // pre-resolved so bot manager skips email lookup
		ChannelID:       chatJID,
		ThreadTS:        "",
		Text:            text,
		MessageTS:       info.ID,
		PresetWorkflow:  presetRoute,
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
//
// In self-chat (same WhatsApp account for user and bot), an optional prefix
// configured via WHATSAPP_SELF_CHAT_PREFIX is prepended so bot output is
// visually distinguishable from the user's own typing (both render as "from
// me"). Default is empty — timing + bubble rhythm is usually enough — and
// the user can set the env var to "🤖 " or similar if they want labelling.
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
	// Convert standard markdown to WhatsApp's formatting subset. Even with
	// Layer 1's channel-aware system prompt telling the LLM to emit WhatsApp
	// markup directly, tool outputs and cached text can still arrive as
	// standard markdown — the formatter is the safety net that normalises.
	formatter := WhatsAppFormatter{}
	message = formatter.FormatMessage(message)

	if w.selfChatPrefix != "" && w.isSelfChat(jid) {
		message = w.selfChatPrefix + message
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
//
// dest is currently ignored — destination resolution from per-user prefs is
// a follow-up. The receiver is always the paired account's own JID for now.
func (w *WhatsAppService) SendNotification(ctx context.Context, uniqueID, message, contextMsg string, opts *ButtonOptions, dest *NotificationDestination) (string, error) {
	_ = dest
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

// WhatsAppFormatter converts standard markdown into WhatsApp's formatting
// subset. Layer 1 (channel-formatting system prompt) teaches the agent to
// emit WhatsApp-native markup directly, so this converter is a safety net
// for standard markdown that slips through (tool outputs, cached text,
// upstream agents that forgot the directive).
//
// Handled:
//   - **bold** / __bold__  →  *bold*  (WhatsApp uses single asterisks)
//   - Markdown headers (#, ##, ###) — strip "# " prefix, bold the line
//   - [text](url) links    →  text (url)  (just url if text equals url)
//   - "- item" / "* item"  →  "• item" (WhatsApp does not style markdown bullets)
//   - Tables → paragraphs of key/value lines (see tableToText)
//
// Untouched (native WhatsApp already renders these):
//   - Single-asterisk *bold*, _italic_, ~strike~, `inline`, ```block```.
type WhatsAppFormatter struct{}

// waCodeFence matches markdown code fences so the formatter can skip their
// contents verbatim — converting bullets/headers inside code would corrupt
// the code.
var waCodeFence = regexp.MustCompile("(?s)```.*?```")

// waHeader matches a line starting with 1-6 "#" followed by a space. Captures
// the header text (group 1).
var waHeader = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`)

// waBoldDouble matches standard markdown **bold** or __bold__ (non-greedy).
var waBoldDouble = regexp.MustCompile(`(?s)(\*\*|__)(.+?)(\*\*|__)`)

// waMarkdownLink matches [text](url). Non-greedy on text to avoid swallowing
// consecutive links.
var waMarkdownLink = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

// waBulletLine matches a line-start "- " or "* " bullet. The literal space
// after the dash/asterisk is intentional — it's how we distinguish a bullet
// from bold syntax: "**bold**" has no space after the first asterisk and so
// never matches. Go's regex (RE2) doesn't support lookahead, so the space
// anchor is load-bearing rather than cosmetic.
var waBulletLine = regexp.MustCompile(`(?m)^(\s*)[-*] `)

// waTableBlock matches a standard pipe-separated markdown table (header row +
// separator row + >=1 data row). Multi-line, non-greedy, anchored on line
// boundaries to avoid eating surrounding prose.
var waTableBlock = regexp.MustCompile(`(?m)^\|.*\|\s*\n\|[\s|:-]+\|\s*\n(?:\|.*\|\s*\n?)+`)

// FormatMessage applies the regex substitutions in an order that avoids
// cross-talk: code fences are carved out and preserved first, then tables,
// then line-level rewrites (headers, bullets), then inline rewrites (bold,
// links). Code-fenced content is restored last.
func (f *WhatsAppFormatter) FormatMessage(markdown string) string {
	if markdown == "" {
		return ""
	}
	// Pass 1 — pull code fences out so we don't transform their contents.
	fences := []string{}
	protected := waCodeFence.ReplaceAllStringFunc(markdown, func(s string) string {
		fences = append(fences, s)
		return fmt.Sprintf("\x00WA_CODE_%d\x00", len(fences)-1)
	})

	// Pass 2 — tables first (they're block-level and need rewriting before
	// their lines get processed by header/bullet rules).
	protected = waTableBlock.ReplaceAllStringFunc(protected, tableToText)

	// Pass 3 — line-level: headers become bold lines, bullets become •.
	protected = waHeader.ReplaceAllString(protected, "*$1*")
	protected = waBulletLine.ReplaceAllString(protected, "${1}• ")

	// Pass 4 — inline: bold then links (bold first so link text can still
	// contain bold markers).
	protected = waBoldDouble.ReplaceAllString(protected, "*$2*")
	protected = waMarkdownLink.ReplaceAllStringFunc(protected, func(s string) string {
		parts := waMarkdownLink.FindStringSubmatch(s)
		if len(parts) != 3 {
			return s
		}
		text, url := strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		if text == "" || text == url {
			return url
		}
		return text + " (" + url + ")"
	})

	// Pass 5 — restore fenced content.
	for i, raw := range fences {
		placeholder := fmt.Sprintf("\x00WA_CODE_%d\x00", i)
		protected = strings.Replace(protected, placeholder, raw, 1)
	}
	return protected
}

// tableToText renders a markdown table as plain text that survives WhatsApp's
// lack of table rendering. Strategy: for each data row, emit "*<col>*: <val>"
// lines, one per cell, then a blank line between rows. Keeps the data
// scannable even without alignment.
func tableToText(table string) string {
	lines := strings.Split(strings.TrimSpace(table), "\n")
	if len(lines) < 3 {
		return table // not enough rows to be a real table; leave alone
	}
	splitRow := func(row string) []string {
		row = strings.Trim(strings.TrimSpace(row), "|")
		cells := strings.Split(row, "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		return cells
	}
	headers := splitRow(lines[0])
	var out strings.Builder
	for _, rowLine := range lines[2:] {
		if strings.TrimSpace(rowLine) == "" {
			continue
		}
		cells := splitRow(rowLine)
		for i, cell := range cells {
			col := ""
			if i < len(headers) {
				col = headers[i]
			}
			if col == "" && cell == "" {
				continue
			}
			if col != "" {
				out.WriteString("*" + col + "*: ")
			}
			out.WriteString(cell)
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
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
