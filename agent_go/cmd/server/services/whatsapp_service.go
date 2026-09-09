package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/whatsappbot"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/whatsapptransport"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"

	// Pure-Go SQLite driver (registered as "sqlite"). SQLite is an exception
	// to the project's "no-database, workspace-file only" convention — see
	// WhatsAppService doc below for the reason. Pure-Go is chosen over
	// mattn/go-sqlite3 so the agent binary stays CGO-free and builds the
	// same way across hosts.
	_ "modernc.org/sqlite"
)

// WhatsAppService implements BotConnector on top of whatsmeow — a Go library
// that speaks the multi-device WhatsApp Web protocol. The bot operates
// through a paired WhatsApp account (paired once via QR code); no Meta
// Business API, verified business, or approved templates are required.
//
// Tradeoff: whatsmeow uses the unofficial WhatsApp Web protocol. Meta may ban
// numbers exhibiting bot-like behavior at scale, so this is best suited to
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

	// selfChatPrefix is prepended to replies in the owner's own chat so the
	// bot's messages are told apart from the owner's (WHATSAPP_SELF_CHAT_PREFIX).
	selfChatPrefix string

	mu sync.RWMutex
	// conn is the shared connector (pkg/whatsappbot): session, pairing,
	// reconnects, dedupe and "@slug" routing all live there. This service
	// keeps only AgentWorks' policy: one workspace owner per phone, link
	// codes for other chats, workflow routes and the bot-manager hand-off.
	conn *whatsappbot.Connector

	// metaDB holds the owner, routes, active routes and access state in the
	// same SQLite file as the session (table whatsapp_meta).
	metaDB *sql.DB

	messageHandler     BotMessageHandler
	interactionHandler BotInteractionHandler
	statusProvider     BotThreadStatusFunc
	// profileRouter resolves the default product's own @tokens for this
	// pairing's owner; consulted after the workflow slugs. nil: none.
	profileRouter ProfileRouterFunc
	// voiceTranscriber turns a downloaded voice note into text on-device.
	// nil: voice notes fall back to the generic media-upload description.
	voiceTranscriber VoiceTranscriberFunc

	routingMu sync.RWMutex
	routing   WhatsAppRouting
	// autoRoutedWorkflows holds the workflow IDs that have already been given
	// their default @<automation-name> route. Provisioning is once-only per
	// workflow: a route the operator renames or deletes afterwards stays gone
	// instead of reappearing on the next scan.
	autoRoutedWorkflows map[string]bool
	// autoRouteCheckedAt throttles the workspace scan behind
	// EnsureDefaultWorkflowRoutes, which is driven by polled HTTP handlers.
	autoRouteCheckedAt time.Time

	activeRoutesMu sync.RWMutex
	activeRoutes   map[string]string

	activeRouteHints map[string]time.Time

	accessMu    sync.RWMutex
	accessState WhatsAppAccessState

	ownerMu sync.RWMutex
	owner   *WhatsAppOwner
}

// WhatsAppOwner records which workspace user owns the paired WhatsApp
// account. Serialized to JSON alongside the SQLite session file.
type WhatsAppOwner struct {
	UserID   string    `json:"user_id"`
	Email    string    `json:"email"`
	Username string    `json:"username,omitempty"`
	PairedAt time.Time `json:"paired_at"`
	// DefaultProfileID names the agent profile whose own conversation takes
	// every message in this pairing that names no @<slug> workflow route. A
	// product sets it when it pairs (the server checks the profile declares
	// the whatsapp capability); it goes with the owner on unpair.
	DefaultProfileID string `json:"default_profile_id,omitempty"`
	// DefaultUploadFolder is where that profile wants attachments saved —
	// workspace-relative, inside its own root, so its agent can read them.
	// Empty keeps the per-user chat uploads folder.
	DefaultUploadFolder string `json:"default_upload_folder,omitempty"`
}

type WhatsAppAccessState struct {
	LinkCode        string                `json:"link_code,omitempty"`
	LinkCodeExpires time.Time             `json:"link_code_expires_at,omitempty"`
	BoundChats      []WhatsAppBoundDMChat `json:"bound_chats,omitempty"`
}

type WhatsAppBoundDMChat struct {
	ChatJID    string    `json:"chat_jid"`
	Identities []string  `json:"identities,omitempty"`
	BoundAt    time.Time `json:"bound_at"`
	LastSeenAt time.Time `json:"last_seen_at,omitempty"`
}

// NewWhatsAppService constructs a service that will persist its multi-device
// pairing state in dbPath (a SQLite file local to the agent process).
func NewWhatsAppService(dbPath string) *WhatsAppService {
	return &WhatsAppService{dbPath: dbPath}
}

// Name returns the connector name used in routing and logs.
func (w *WhatsAppService) Name() string { return "whatsapp" }

// IsEnabled reports whether the underlying client has been initialized via
// StartListening. Note: enabled != paired != connected.
func (w *WhatsAppService) IsEnabled() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.conn != nil && w.conn.Started()
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
	w.selfChatPrefix = os.Getenv("WHATSAPP_SELF_CHAT_PREFIX")

	if err := w.openMetaStore(ctx); err != nil {
		return err
	}
	w.loadOwner(ctx)
	w.loadRouting(ctx)
	w.loadActiveRoutes(ctx)
	w.loadAutoRoutedWorkflows(ctx)
	w.loadAccessState(ctx)

	debug := os.Getenv("WHATSAPP_DEBUG") == "true"
	if debug {
		log.Printf("[WHATSAPP] Verbose whatsmeow logging enabled (WHATSAPP_DEBUG=true)")
	}
	conn := whatsappbot.New(whatsappbot.Config{
		DBPath:     w.dbPath,
		AppName:    "Chrome",
		AppVersion: [3]uint32{120, 0, 0},
		Debug:      debug,
		LogPrefix:  "[WHATSAPP]",
		AutoPair:   true, // one phone per workspace user; offer the QR right away
		Handler:    w,
		Access:     w,
		Router:     w,
		Routes:     activeSlugStore{w},
	})
	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()
	// IMPORTANT: do not tie the connector's background context to an HTTP
	// request context. The manager lazily starts services inside request
	// handlers; if we pass r.Context() through, it gets cancelled as soon as
	// the response is written, which immediately kills pairing/reconnect loops
	// (you'll see "Context is done" from whatsmeow's QR emitter).
	if err := conn.Start(context.Background()); err != nil {
		w.mu.Lock()
		w.conn = nil
		w.mu.Unlock()
		return err
	}
	log.Printf("[WHATSAPP] Service started (db=%s, paired=%v)", w.dbPath, conn.IsPaired())
	return nil
}

// StopListening disconnects the client. The session DB is left intact so the
// next StartListening resumes the paired state without re-scanning.
func (w *WhatsAppService) StopListening() {
	w.mu.Lock()
	conn := w.conn
	w.conn = nil
	w.mu.Unlock()
	if conn != nil {
		conn.Stop()
	}
	w.closeMetaStore()
}

// Unpair disconnects the client, drops the session DB, and re-initializes a
// fresh empty store so the next pairing attempt starts with a clean slate.
// After Unpair returns, the service is back in "unpaired" state — the next
// pairing QR will be generated on the next reconnect.
func (w *WhatsAppService) Unpair(ctx context.Context) error {
	w.StopListening()
	w.clearOwner()

	// Delete the session DB (and WAL/SHM sidecars) so the next start is a
	// clean pairing with no leftover device rows or owner binding.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := w.dbPath + suffix
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("whatsapp: remove %s: %w", path, err)
		}
	}
	return w.StartListening(ctx)
}

// GetQR returns the most recent pairing QR code string and its expiration.
// Empty code means no pairing flow is active (either already paired, or
// StartListening has not been called).
func (w *WhatsAppService) GetQR() (code string, expires time.Time) {
	if conn := w.connector(); conn != nil {
		return conn.GetQR()
	}
	return "", time.Time{}
}

// PairingInfo provides diagnostics for the current or most recent pairing
// attempt (used by the UI to show why scanning didn't stick).
func (w *WhatsAppService) PairingInfo() (active bool, started time.Time, lastErr string, lastMsg string, lastAt time.Time) {
	if conn := w.connector(); conn != nil {
		return conn.PairingInfo()
	}
	return false, time.Time{}, "", "", time.Time{}
}

func (w *WhatsAppService) connector() *whatsappbot.Connector {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.conn
}

// EnsurePairingQR starts a fresh QR flow when the service is unpaired and the
// previous code has expired or timed out. The new code arrives asynchronously;
// callers should poll status until qr_available flips true.
func (w *WhatsAppService) EnsurePairingQR(ctx context.Context) error {
	conn := w.connector()
	if conn == nil {
		return fmt.Errorf("whatsapp: client not initialized")
	}
	conn.EnsureConnecting(ctx)
	return nil
}

// GetQRImagePNG renders the active QR code as a PNG image of the requested
// size in pixels. Returns (nil, nil) if no QR is available; an error only on
// encoding failure.
func (w *WhatsAppService) GetQRImagePNG(size int) ([]byte, error) {
	conn := w.connector()
	if conn == nil {
		return nil, nil
	}
	if size <= 0 {
		size = 384
	}
	return conn.GetQRImagePNG(size)
}

// IsPaired reports whether a device identity has been stored. Paired implies
// a prior successful QR scan, but not that the client is currently connected
// — use IsConnected for liveness.
func (w *WhatsAppService) IsPaired() bool {
	conn := w.connector()
	return conn != nil && conn.IsPaired()
}

// IsConnected reports whether the whatsmeow client is live on the WhatsApp
// websocket right now.
func (w *WhatsAppService) IsConnected() bool {
	conn := w.connector()
	return conn != nil && conn.IsConnected()
}

// metaKeyOwner is the row key under which the owner binding JSON is stored
// in the whatsapp_meta table.
const metaKeyOwner = "owner"

// metaKeyRouting holds the JSON map of slug → ChannelRoute used to route
// incoming messages to specific workflows via an @<slug> prefix.
const metaKeyRouting = "routing"

// metaKeyActiveRouting holds the JSON map of WhatsApp chat JID → active slug.
const metaKeyActiveRouting = "active_routing"

// metaKeyAutoRoutedWorkflows holds the JSON array of workflow IDs that have
// already been handed a default @<automation-name> route, so provisioning
// never resurrects a route the operator deliberately removed.
const metaKeyAutoRoutedWorkflows = "auto_routed_workflows"

// metaKeyAccessState holds explicit WhatsApp DM bindings. This avoids using
// fragile phone-number/LID self-detection as the security boundary.
const metaKeyAccessState = "access_state"

// WhatsAppRouting is the full slug → ChannelRoute map persisted to the meta
// table. A nil / empty map means "no routing — all messages go to the
// default multi-agent chat flow".
type WhatsAppRouting map[string]ChannelRoute

type whatsappDownloadedMedia struct {
	Kind      string
	FileName  string
	FilePath  string
	MimeType  string
	Caption   string
	SizeBytes int
	// Transcript is set only for a voice note (Kind "audio") the platform
	// successfully transcribed on-device. When set, the message is handled
	// as plain text — the chat, and the agent's own context, never mention
	// audio at all; a voice note reads exactly like one that was typed.
	Transcript string
	// VoiceSetupNeeded is true for a voice note the platform could not
	// transcribe because the speech model isn't installed yet (see
	// ErrVoiceNotInstalled) — a one-time thing, so the parent gets asked to
	// set it up rather than the note just silently arriving as a bare file.
	VoiceSetupNeeded bool
}

// openMetaStore opens the lightweight metadata connection and ensures the
// whatsapp_meta table exists. Called from StartListening after sqlstore has
// been set up, so the underlying SQLite file is already initialized.
func (w *WhatsAppService) openMetaStore(ctx context.Context) error {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", w.dbPath)
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

// loadActiveRoutes pulls the chat → active slug map from whatsapp_meta.
// Missing row means no chat has pinned a workflow yet.
func (w *WhatsAppService) loadActiveRoutes(ctx context.Context) {
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return
	}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM whatsapp_meta WHERE key = ?`, metaKeyActiveRouting).Scan(&raw)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("[WHATSAPP] Failed to read active routing row: %v", err)
		}
		return
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		log.Printf("[WHATSAPP] Failed to parse active routing row: %v (ignoring)", err)
		return
	}
	w.activeRoutesMu.Lock()
	w.activeRoutes = m
	w.activeRoutesMu.Unlock()
	log.Printf("[WHATSAPP] Loaded %d active workflow route(s)", len(m))
}

// loadAutoRoutedWorkflows pulls the set of workflow IDs that already got
// their default @<automation-name> route. Missing row means nothing has been
// provisioned yet — normal for a pairing made before this behaviour existed.
func (w *WhatsAppService) loadAutoRoutedWorkflows(ctx context.Context) {
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return
	}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM whatsapp_meta WHERE key = ?`, metaKeyAutoRoutedWorkflows).Scan(&raw)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("[WHATSAPP] Failed to read auto-routed workflow row: %v", err)
		}
		return
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		log.Printf("[WHATSAPP] Failed to parse auto-routed workflow row: %v (ignoring)", err)
		return
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			seen[id] = true
		}
	}
	w.routingMu.Lock()
	w.autoRoutedWorkflows = seen
	w.routingMu.Unlock()
}

func (w *WhatsAppService) loadAccessState(ctx context.Context) {
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return
	}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM whatsapp_meta WHERE key = ?`, metaKeyAccessState).Scan(&raw)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("[WHATSAPP] Failed to read access state row: %v", err)
		}
		w.ensureLinkCode()
		return
	}
	var state WhatsAppAccessState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		log.Printf("[WHATSAPP] Failed to parse access state row: %v (resetting)", err)
		w.ensureLinkCode()
		return
	}
	w.accessMu.Lock()
	w.accessState = state
	w.accessMu.Unlock()
	w.ensureLinkCode()
	log.Printf("[WHATSAPP] Loaded %d bound DM chat(s)", len(state.BoundChats))
}

func (w *WhatsAppService) persistAccessStateLocked(ctx context.Context) error {
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("whatsapp: meta store not open")
	}
	raw, err := json.Marshal(w.accessState)
	if err != nil {
		return fmt.Errorf("whatsapp: marshal access state: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO whatsapp_meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		metaKeyAccessState, string(raw),
	); err != nil {
		return fmt.Errorf("whatsapp: persist access state: %w", err)
	}
	return nil
}

func (w *WhatsAppService) persistActiveRoutesLocked(ctx context.Context) error {
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("whatsapp: meta store not open")
	}
	raw, err := json.Marshal(w.activeRoutes)
	if err != nil {
		return fmt.Errorf("whatsapp: marshal active routing: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO whatsapp_meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		metaKeyActiveRouting, string(raw),
	); err != nil {
		return fmt.Errorf("whatsapp: persist active routing: %w", err)
	}
	return nil
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
	// Any workflow that is present in this routing map has, by definition,
	// been provisioned once already (by automation or by the operator). Record
	// that fact so a workflow route that is later deliberately deleted is not
	// auto-provisioned again on the next workspace scan.
	for _, route := range cleaned {
		w.markWorkflowAutoRoutedLocked(route.WorkflowID)
	}
	if err := w.persistAutoRoutedWorkflowsLocked(context.Background()); err != nil {
		log.Printf("[WHATSAPP] Failed to persist auto-routed workflow ids after routing update: %v", err)
	}
	w.routingMu.Unlock()

	w.activeRoutesMu.Lock()
	for chatJID, slug := range w.activeRoutes {
		if _, ok := cleaned[slug]; !ok {
			delete(w.activeRoutes, chatJID)
		}
	}
	if err := w.persistActiveRoutesLocked(context.Background()); err != nil {
		log.Printf("[WHATSAPP] Failed to prune active workflow routes: %v", err)
	}
	w.activeRoutesMu.Unlock()
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

func sanitizeWhatsAppFileName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return ""
	}
	name = regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(name, "-")
	name = strings.Trim(name, ".-")
	if name == "" {
		return ""
	}
	if len(name) > 120 {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		if len(ext) > 16 {
			ext = ""
		}
		if maxBase := 120 - len(ext); len(base) > maxBase {
			base = base[:maxBase]
		}
		name = base + ext
	}
	return name
}

func extensionForWhatsAppMedia(mimeType, fallback string) string {
	if exts, err := mime.ExtensionsByType(mimeType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "video/mp4":
		return ".mp4"
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	default:
		return fallback
	}
}

func (w *WhatsAppService) downloadIncomingMedia(ctx context.Context, msg *whatsappbot.Message, owner *WhatsAppOwner, folderPath string) (*whatsappDownloadedMedia, error) {
	if msg == nil || owner == nil || !msg.HasMedia {
		return nil, nil
	}
	const maxWhatsAppUploadBytes = 10 * 1024 * 1024
	downloadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	media, err := msg.Download(downloadCtx, maxWhatsAppUploadBytes)
	if err != nil {
		if errors.Is(err, whatsapptransport.ErrNoMedia) {
			return nil, nil
		}
		return nil, err
	}
	fileName := sanitizeWhatsAppFileName(media.FileName)
	if fileName == "" {
		ext := extensionForWhatsAppMedia(media.MimeType, media.Ext)
		stableID := sanitizeWhatsAppFileName(msg.ID)
		if stableID == "" {
			stableID = fmt.Sprintf("%d", time.Now().UnixNano())
		}
		fileName = fmt.Sprintf("whatsapp-%s-%s%s", media.Kind, stableID, ext)
	}
	if strings.TrimSpace(folderPath) == "" {
		folderPath = whatsappUserChatUploadFolder(owner.UserID)
	}
	wsClient := workspace.NewClient(workspaceAPIURL(), workspace.WithUserID(owner.UserID))
	filePath, err := wsClient.UploadBinary(ctx, folderPath, fileName, media.Data)
	if err != nil {
		return nil, fmt.Errorf("upload %s to workspace: %w", media.Kind, err)
	}
	mimeType := media.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	result := &whatsappDownloadedMedia{
		Kind:      media.Kind,
		FileName:  fileName,
		FilePath:  filePath,
		MimeType:  mimeType,
		Caption:   media.Caption,
		SizeBytes: len(media.Data),
	}
	if media.Kind == "audio" {
		w.mu.RLock()
		transcriber := w.voiceTranscriber
		w.mu.RUnlock()
		if transcriber != nil {
			// The just-uploaded workspace copy, not the transport's temp
			// download — media.Data was already written to disk once for
			// the upload; decoding needs a real path, and the workspace
			// client doesn't hand one back for a remote store, so decode
			// from a local temp file instead of re-fetching.
			tmp, err := os.CreateTemp("", "wa-voice-*"+extensionForWhatsAppMedia(mimeType, media.Ext))
			if err != nil {
				log.Printf("[WHATSAPP] voice note temp file: %v", err)
			} else {
				tmpPath := tmp.Name()
				writeErr := os.WriteFile(tmpPath, media.Data, 0o600)
				tmp.Close()
				if writeErr != nil {
					log.Printf("[WHATSAPP] voice note temp write: %v", writeErr)
				} else {
					text, transcribeErr := transcriber(ctx, tmpPath)
					result.Transcript, result.VoiceSetupNeeded = voiceTranscriptionOutcome(text, transcribeErr)
					if transcribeErr != nil && !result.VoiceSetupNeeded {
						log.Printf("[WHATSAPP] voice note transcription failed: %v", transcribeErr)
					}
				}
				os.Remove(tmpPath)
			}
		}
	}
	return result, nil
}

// voiceTranscriptionOutcome turns a transcriber call's result into what
// downloadIncomingMedia records on whatsappDownloadedMedia: a transcript
// text can be sent immediately even if empty (a silent clip stays a
// no-op transcript, not an error); ErrVoiceNotInstalled asks the parent to
// set voice up; any other failure just logs and falls back to the generic
// file description, same as before this feature existed.
func voiceTranscriptionOutcome(text string, err error) (transcript string, setupNeeded bool) {
	if err != nil {
		return "", errors.Is(err, ErrVoiceNotInstalled)
	}
	return strings.TrimSpace(text), false
}

// whatsappMessageTextForMedia is the effective message text HandleMessage
// hands to the agent: a transcribed voice note reads exactly like a typed
// message (merged with anything the sender also typed above it) — the chat
// never learns it arrived as audio. Anything else keeps the generic
// file-attachment description.
func whatsappMessageTextForMedia(text string, media *whatsappDownloadedMedia) string {
	if media == nil || media.Transcript == "" {
		return appendWhatsAppMediaContext(text, media)
	}
	if trimmed := strings.TrimSpace(text); trimmed != "" {
		return trimmed + "\n\n" + media.Transcript
	}
	return media.Transcript
}

func appendWhatsAppMediaContext(text string, media *whatsappDownloadedMedia) string {
	if media == nil {
		return text
	}
	var sb strings.Builder
	if strings.TrimSpace(text) != "" {
		sb.WriteString(strings.TrimSpace(text))
		sb.WriteString("\n\n")
	}
	sb.WriteString("WhatsApp upload received:\n")
	sb.WriteString(fmt.Sprintf("- Type: %s\n", media.Kind))
	sb.WriteString(fmt.Sprintf("- File: %s\n", media.FilePath))
	sb.WriteString(fmt.Sprintf("- Original name: %s\n", media.FileName))
	sb.WriteString(fmt.Sprintf("- MIME type: %s\n", media.MimeType))
	sb.WriteString(fmt.Sprintf("- Size: %d bytes", media.SizeBytes))
	if media.Caption != "" && !strings.Contains(text, media.Caption) {
		sb.WriteString("\n- Caption: ")
		sb.WriteString(media.Caption)
	}
	sb.WriteString("\n\nUse the uploaded file path above when reading or analyzing the attachment.")
	return sb.String()
}

func whatsappUserChatUploadFolder(userID string) string {
	safeUserID := sanitizeWhatsAppFileName(userID)
	if safeUserID == "" {
		safeUserID = "default"
	}
	return filepath.ToSlash(filepath.Join("_users", safeUserID, "chat_history", "uploads", "whatsapp", time.Now().Format("2006-01-02")))
}

func whatsappWorkflowUploadFolder(route *ChannelRoute) string {
	if route == nil || strings.TrimSpace(route.WorkspacePath) == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Join(route.WorkspacePath, "incoming", "whatsapp", time.Now().Format("2006-01-02")))
}

func (w *WhatsAppService) setActiveSlug(chatJID, slug string) {
	if chatJID == "" || slug == "" {
		return
	}
	w.activeRoutesMu.Lock()
	if w.activeRoutes == nil {
		w.activeRoutes = make(map[string]string)
	}
	w.activeRoutes[chatJID] = strings.ToLower(slug)
	if err := w.persistActiveRoutesLocked(context.Background()); err != nil {
		log.Printf("[WHATSAPP] Failed to save active workflow @%s for %s: %v", slug, chatJID, err)
	}
	w.activeRoutesMu.Unlock()
}

func (w *WhatsAppService) clearActiveSlug(chatJID string) {
	if chatJID == "" {
		return
	}
	w.activeRoutesMu.Lock()
	if w.activeRoutes != nil {
		delete(w.activeRoutes, chatJID)
	}
	if err := w.persistActiveRoutesLocked(context.Background()); err != nil {
		log.Printf("[WHATSAPP] Failed to clear active workflow for %s: %v", chatJID, err)
	}
	w.activeRoutesMu.Unlock()
}

func (w *WhatsAppService) activeSlug(chatJID string) string {
	w.activeRoutesMu.RLock()
	defer w.activeRoutesMu.RUnlock()
	if w.activeRoutes == nil {
		return ""
	}
	return w.activeRoutes[chatJID]
}

type whatsappWorkflowCandidate struct {
	Number        int
	ID            string
	Label         string
	WorkspacePath string
	Slug          string
	WorkshopMode  string
}

type whatsappWorkflowManifest struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	ExecutionDefs struct {
		WorkshopMode string `json:"workshop_mode"`
	} `json:"execution_defaults"`
}

type whatsappDocumentListResponse struct {
	Success bool               `json:"success"`
	Data    []whatsappDocument `json:"data"`
}

type whatsappDocument struct {
	FilePath string             `json:"filepath"`
	Type     string             `json:"type,omitempty"`
	Children []whatsappDocument `json:"children,omitempty"`
}

func (w *WhatsAppService) handleWorkflowCommand(ctx context.Context, text, chatJID string, owner *WhatsAppOwner, info types.MessageInfo) bool {
	cmd, arg, ok := parseWhatsAppWorkflowCommand(text)
	if !ok {
		return false
	}
	_ = w.sendReaction(ctx, chatJID, info.Sender, info.ID, "👀")

	switch cmd {
	case "help", "commands":
		w.sendWorkflowCommandReply(ctx, chatJID, unknownWhatsAppWorkflowCommandMessage(""))
		return true

	case "list", "workflows":
		candidates, err := w.discoverWorkflowCandidates(ctx, owner)
		if err != nil {
			w.sendWorkflowCommandReply(ctx, chatJID, fmt.Sprintf("Couldn't list workflows: %v", err))
			return true
		}
		w.sendWorkflowCommandReply(ctx, chatJID, formatWhatsAppWorkflowList(candidates))
		return true

	case "switch":
		if strings.TrimSpace(arg) == "" {
			w.sendWorkflowCommandReply(ctx, chatJID, "Use: @switch 3 [run|workshop]")
			return true
		}
		workflowQuery, mode, modeErr := parseWhatsAppSwitchArg(arg)
		if modeErr != nil {
			w.sendWorkflowCommandReply(ctx, chatJID, modeErr.Error())
			return true
		}
		candidates, err := w.discoverWorkflowCandidates(ctx, owner)
		if err != nil {
			w.sendWorkflowCommandReply(ctx, chatJID, fmt.Sprintf("Couldn't switch workflow: %v", err))
			return true
		}
		candidate, matches := matchWhatsAppWorkflowCandidate(candidates, workflowQuery)
		if candidate == nil {
			if len(matches) > 1 {
				w.sendWorkflowCommandReply(ctx, chatJID, formatWhatsAppWorkflowMatches(matches))
			} else {
				w.sendWorkflowCommandReply(ctx, chatJID, "Not found. Use @list, then @switch <number>.")
			}
			return true
		}
		slug := w.ensureWorkflowRoute(ctx, *candidate, mode)
		w.setActiveSlug(chatJID, slug)
		w.sendWorkflowCommandReply(ctx, chatJID, fmt.Sprintf("Using %s (%s). Off: @off", candidate.Label, formatWhatsAppRouteMode(mode)))
		return true

	case "status":
		active := w.activeSlug(chatJID)
		if active == "" {
			w.sendWorkflowCommandReply(ctx, chatJID, fmt.Sprintf("No active workflow. %s Use @list.", w.formatWhatsAppDetailModeStatus(chatJID)))
			w.forwardWhatsAppBotSessionControl(cmd, arg, chatJID, owner, info)
			return true
		}
		route := w.resolveSlugRoute(active)
		if route == nil {
			w.clearActiveSlug(chatJID)
			w.sendWorkflowCommandReply(ctx, chatJID, fmt.Sprintf("No active workflow. %s Use @list.", w.formatWhatsAppDetailModeStatus(chatJID)))
			w.forwardWhatsAppBotSessionControl(cmd, arg, chatJID, owner, info)
			return true
		}
		label := active
		if candidate := w.findWorkflowCandidateByRoute(ctx, owner, *route); candidate != nil {
			label = candidate.Label
		}
		w.sendWorkflowCommandReply(ctx, chatJID, fmt.Sprintf("Using %s (@%s, %s). %s Off: @off", label, active, formatWhatsAppRouteMode(route.WorkshopMode), w.formatWhatsAppDetailModeStatus(chatJID)))
		w.forwardWhatsAppBotSessionControl(cmd, arg, chatJID, owner, info)
		return true

	case "resume", "continue", "sessions", "session", "chats", "runs", "full", "verbose", "details", "concise", "short", "brief", "done", "end", "reset", "new", "newsession", "quit", "exit":
		w.forwardWhatsAppBotSessionControl(cmd, arg, chatJID, owner, info)
		return true

	case "deactivate", "deactive", "off", "stop":
		active := w.activeSlug(chatJID)
		w.clearActiveSlug(chatJID)
		if active != "" {
			w.sendWorkflowCommandReply(ctx, chatJID, "Workflow off. Default chat active.")
		} else {
			w.sendWorkflowCommandReply(ctx, chatJID, "No active workflow.")
		}
		return true
	}

	return false
}

func (w *WhatsAppService) forwardWhatsAppBotSessionControl(cmd, arg, chatJID string, owner *WhatsAppOwner, info types.MessageInfo) {
	if w.messageHandler == nil {
		w.sendWorkflowCommandReply(context.Background(), chatJID, "Bot session controls are not available yet.")
		return
	}
	botText := "@" + cmd
	if cmd == "sessions" || cmd == "session" || cmd == "chats" || cmd == "runs" {
		botText = "@status"
	} else if strings.TrimSpace(arg) != "" {
		botText += " " + strings.TrimSpace(arg)
	}
	w.messageHandler(BotIncomingMessage{
		Platform:        "whatsapp",
		UserID:          info.Sender.User,
		UserName:        info.PushName,
		UserEmail:       owner.Email,
		WorkspaceUserID: owner.UserID,
		ChannelID:       chatJID,
		ThreadTS:        "",
		Text:            botText,
		MessageTS:       info.ID,
		Timestamp:       info.Timestamp,
		IsThreadReply:   false,
		IsMention:       true,
	})
}

func parseWhatsAppWorkflowCommand(text string) (cmd, arg string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "", "", false
	}
	if !strings.HasPrefix(fields[0], "@") {
		return "", "", false
	}
	first := strings.ToLower(strings.TrimPrefix(fields[0], "@"))
	switch first {
	case "", "help", "commands", "?":
		return "help", strings.TrimSpace(strings.Join(fields[1:], " ")), true
	case "list", "workflows", "workflow-list":
		return "list", strings.TrimSpace(strings.Join(fields[1:], " ")), true
	case "switch", "sw", "select", "siwthc":
		return "switch", strings.TrimSpace(strings.Join(fields[1:], " ")), true
	case "status":
		return "status", strings.TrimSpace(strings.Join(fields[1:], " ")), true
	case "resume", "continue", "sessions", "session", "chats", "runs", "full", "verbose", "details", "concise", "short", "brief", "done", "end", "reset", "new", "newsession", "quit", "exit":
		return first, strings.TrimSpace(strings.Join(fields[1:], " ")), true
	case "deactivate", "deactive", "off", "stop":
		return first, strings.TrimSpace(strings.Join(fields[1:], " ")), true
	default:
		return "", "", false
	}
}

func parseWhatsAppSwitchArg(arg string) (workflowQuery, mode string, err error) {
	fields := strings.Fields(strings.TrimSpace(arg))
	if len(fields) == 0 {
		return "", "", fmt.Errorf("Use: @switch <workflow> [run|workshop]")
	}
	mode = "run"
	if len(fields) > 1 {
		if parsedMode, ok := normalizeWhatsAppRouteMode(fields[len(fields)-1]); ok {
			mode = parsedMode
			fields = fields[:len(fields)-1]
		} else if looksLikeWhatsAppRouteMode(fields[len(fields)-1]) {
			return "", "", fmt.Errorf("Unknown mode %q. Use run or workshop.", fields[len(fields)-1])
		}
	}
	workflowQuery = strings.TrimSpace(strings.Join(fields, " "))
	if workflowQuery == "" {
		return "", "", fmt.Errorf("Use: @switch <workflow> [run|workshop]")
	}
	return workflowQuery, mode, nil
}

func normalizeWhatsAppRouteMode(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "run", "r":
		return "run", true
	case "workshop", "w":
		return "workshop", true
	default:
		return "", false
	}
}

func looksLikeWhatsAppRouteMode(raw string) bool {
	s := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix("run", s) ||
		strings.HasPrefix("workshop", s)
}

func formatWhatsAppRouteMode(mode string) string {
	switch mode {
	case "workshop":
		return "workshop"
	default:
		return "run"
	}
}

func (w *WhatsAppService) sendWorkflowCommandReply(ctx context.Context, chatJID, message string) {
	if _, err := w.SendThreadMessage(ctx, ThreadID{Platform: "whatsapp", ChannelID: chatJID}, message); err != nil {
		log.Printf("[WHATSAPP] Failed to send workflow command reply: %v", err)
	}
}

func (w *WhatsAppService) formatWhatsAppDetailModeStatus(chatJID string) string {
	w.mu.RLock()
	provider := w.statusProvider
	w.mu.RUnlock()
	if provider == nil {
		return "Detail mode: concise by default. Use @full / @concise during a run."
	}
	status := provider(ThreadID{Platform: "whatsapp", ChannelID: chatJID, ThreadTS: chatJID})
	if !status.HasSession {
		return "Detail mode: concise by default. Use @full / @concise during a run."
	}
	mode := strings.TrimSpace(status.DetailMode)
	if mode == "" {
		mode = "concise"
	}
	return fmt.Sprintf("Detail mode: %s. Use @full / @concise to switch.", mode)
}

func (w *WhatsAppService) discoverWorkflowCandidates(ctx context.Context, owner *WhatsAppOwner) ([]whatsappWorkflowCandidate, error) {
	if owner == nil {
		return nil, fmt.Errorf("pairing has no workspace-user owner")
	}
	wsClient := workspace.NewClient(workspaceAPIURL(), workspace.WithUserID(owner.UserID))
	maxDepth := 2
	list, err := wsClient.ListWorkspaceFiles(ctx, workspace.ListWorkspaceFilesParams{Folder: "Workflow", MaxDepth: &maxDepth})
	if err != nil {
		return nil, err
	}
	var resp whatsappDocumentListResponse
	if err := json.Unmarshal(list.Raw, &resp); err != nil {
		return nil, fmt.Errorf("parse workflow list: %w", err)
	}
	var manifestPaths []string
	var walkDocs func([]whatsappDocument)
	walkDocs = func(docs []whatsappDocument) {
		for _, doc := range docs {
			if strings.HasSuffix(doc.FilePath, "/workflow.json") {
				manifestPaths = append(manifestPaths, doc.FilePath)
			}
			if len(doc.Children) > 0 {
				walkDocs(doc.Children)
			}
		}
	}
	walkDocs(resp.Data)
	sort.Strings(manifestPaths)

	seenManifestPaths := make(map[string]bool, len(manifestPaths))
	seenWorkflowKeys := make(map[string]bool, len(manifestPaths))
	candidates := make([]whatsappWorkflowCandidate, 0, len(manifestPaths))
	for _, manifestPath := range manifestPaths {
		manifestPath = filepath.ToSlash(strings.TrimSpace(manifestPath))
		if manifestPath == "" || seenManifestPaths[manifestPath] {
			continue
		}
		seenManifestPaths[manifestPath] = true
		content, err := wsClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{Filepath: manifestPath})
		if err != nil {
			log.Printf("[WHATSAPP] Failed to read workflow manifest %s: %v", manifestPath, err)
			continue
		}
		var manifest whatsappWorkflowManifest
		if err := json.Unmarshal([]byte(content.Content), &manifest); err != nil {
			log.Printf("[WHATSAPP] Failed to parse workflow manifest %s: %v", manifestPath, err)
			continue
		}
		workspacePath := strings.TrimSuffix(manifestPath, "/workflow.json")
		label := strings.TrimSpace(manifest.Label)
		if label == "" {
			label = filepath.Base(workspacePath)
		}
		id := strings.TrimSpace(manifest.ID)
		if id == "" {
			id = slugifyWhatsAppWorkflow(label)
		}
		workflowKey := strings.ToLower(strings.TrimSpace(id))
		if workflowKey == "" {
			workflowKey = strings.ToLower(strings.TrimSpace(workspacePath))
		}
		if seenWorkflowKeys[workflowKey] {
			continue
		}
		seenWorkflowKeys[workflowKey] = true
		candidates = append(candidates, whatsappWorkflowCandidate{
			ID:            id,
			Label:         label,
			WorkspacePath: workspacePath,
			Slug:          slugifyWhatsAppWorkflow(label),
			WorkshopMode:  strings.TrimSpace(manifest.ExecutionDefs.WorkshopMode),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return strings.ToLower(candidates[i].Label) < strings.ToLower(candidates[j].Label)
	})
	for i := range candidates {
		candidates[i].Number = i + 1
	}
	return candidates, nil
}

func formatWhatsAppWorkflowList(candidates []whatsappWorkflowCandidate) string {
	if len(candidates) == 0 {
		return "No workflows found."
	}
	const maxList = 25
	var sb strings.Builder
	sb.WriteString("Workflows:\n")
	for i, c := range candidates {
		if i >= maxList {
			sb.WriteString(fmt.Sprintf("\n...and %d more. Use @switch <name> for workflows not shown.", len(candidates)-maxList))
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", c.Number, c.Label))
	}
	sb.WriteString("\n@switch 3 [run|workshop]\n@status | @full | @concise | @done | @off")
	return strings.TrimSpace(sb.String())
}

func formatWhatsAppWorkflowMatches(candidates []whatsappWorkflowCandidate) string {
	var sb strings.Builder
	sb.WriteString("Multiple workflows matched. Pick one:\n")
	for _, c := range candidates {
		sb.WriteString(fmt.Sprintf("%d. %s\n", c.Number, c.Label))
	}
	sb.WriteString("\nUse @switch <number>.")
	return strings.TrimSpace(sb.String())
}

func matchWhatsAppWorkflowCandidate(candidates []whatsappWorkflowCandidate, query string) (*whatsappWorkflowCandidate, []whatsappWorkflowCandidate) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	if n, err := strconv.Atoi(q); err == nil {
		for i := range candidates {
			if candidates[i].Number == n {
				return &candidates[i], nil
			}
		}
		return nil, nil
	}
	qSlug := slugifyWhatsAppWorkflow(q)
	for i := range candidates {
		c := candidates[i]
		if strings.EqualFold(c.Label, query) || strings.EqualFold(c.ID, query) || c.Slug == qSlug || strings.EqualFold(filepath.Base(c.WorkspacePath), query) {
			return &candidates[i], nil
		}
	}
	var matches []whatsappWorkflowCandidate
	for _, c := range candidates {
		haystack := strings.ToLower(strings.Join([]string{c.Label, c.ID, c.Slug, filepath.Base(c.WorkspacePath)}, " "))
		if strings.Contains(haystack, q) || strings.Contains(haystack, qSlug) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 1 {
		return &matches[0], matches
	}
	return nil, matches
}

func (w *WhatsAppService) findWorkflowCandidateByRoute(ctx context.Context, owner *WhatsAppOwner, route ChannelRoute) *whatsappWorkflowCandidate {
	candidates, err := w.discoverWorkflowCandidates(ctx, owner)
	if err != nil {
		return nil
	}
	for i := range candidates {
		if candidates[i].ID == route.WorkflowID || candidates[i].WorkspacePath == route.WorkspacePath {
			return &candidates[i]
		}
	}
	return nil
}

func (w *WhatsAppService) ensureWorkflowRoute(ctx context.Context, candidate whatsappWorkflowCandidate, mode string) string {
	w.routingMu.Lock()
	defer w.routingMu.Unlock()

	finalSlug := w.assignWorkflowRouteLocked(candidate, mode)
	w.markWorkflowAutoRoutedLocked(candidate.ID)
	if err := w.persistRoutingLocked(ctx); err != nil {
		log.Printf("[WHATSAPP] Failed to persist auto workflow route @%s: %v", finalSlug, err)
	}
	if err := w.persistAutoRoutedWorkflowsLocked(ctx); err != nil {
		log.Printf("[WHATSAPP] Failed to persist auto-routed workflow ids: %v", err)
	}
	return finalSlug
}

// assignWorkflowRouteLocked maps the automation onto its own name as the
// slug — "Invoice Processing" answers to @invoice-processing — falling back
// to a name+id slug only when a different workflow already holds that name.
// Caller holds routingMu.
func (w *WhatsAppService) assignWorkflowRouteLocked(candidate whatsappWorkflowCandidate, mode string) string {
	slug := candidate.Slug
	if slug == "" {
		slug = slugifyWhatsAppWorkflow(candidate.Label)
	}
	if slug == "" {
		slug = "workflow"
	}
	if normalized, ok := normalizeWhatsAppRouteMode(mode); ok {
		mode = normalized
	} else {
		mode = "run"
	}
	if w.routing == nil {
		w.routing = make(WhatsAppRouting)
	}
	finalSlug := slug
	if existing, ok := w.routing[finalSlug]; ok && existing.WorkflowID != candidate.ID {
		suffix := strings.TrimPrefix(candidate.ID, "wf_")
		suffix = slugifyWhatsAppWorkflow(suffix)
		if suffix == "" || suffix == finalSlug {
			suffix = "workflow"
		}
		finalSlug = slug + "-" + suffix
	}
	w.routing[finalSlug] = ChannelRoute{WorkflowID: candidate.ID, WorkspacePath: candidate.WorkspacePath, WorkshopMode: mode}
	return finalSlug
}

// routingHasWorkflowLocked reports whether any slug already points at this
// workflow. Caller holds routingMu.
func (w *WhatsAppService) routingHasWorkflowLocked(workflowID string) bool {
	for _, route := range w.routing {
		if route.WorkflowID == workflowID {
			return true
		}
	}
	return false
}

// markWorkflowAutoRoutedLocked records that this workflow has had its default
// route provisioned, so it is never provisioned a second time. Caller holds
// routingMu.
func (w *WhatsAppService) markWorkflowAutoRoutedLocked(workflowID string) {
	if strings.TrimSpace(workflowID) == "" {
		return
	}
	if w.autoRoutedWorkflows == nil {
		w.autoRoutedWorkflows = make(map[string]bool)
	}
	w.autoRoutedWorkflows[workflowID] = true
}

// whatsappAutoRouteScanInterval bounds how often EnsureDefaultWorkflowRoutes
// walks the workspace. The handlers that call it are polled, and the scan
// reads every workflow manifest.
const whatsappAutoRouteScanInterval = 30 * time.Second

// EnsureDefaultWorkflowRoutes gives every automation a WhatsApp route named
// after it, so a paired phone reaches one with @<automation-name> without
// anyone having to invent a slug first — before this, a workflow with no
// slug simply could not be talked to over WhatsApp.
//
// Each workflow is provisioned exactly once: a route later renamed or
// deleted is not recreated, and automations added after pairing pick theirs
// up on a later call. No-op until the phone is paired and a workspace user
// owns the pairing (the scan runs as that user).
func (w *WhatsAppService) EnsureDefaultWorkflowRoutes(ctx context.Context) {
	owner := w.GetOwner()
	if owner == nil || !w.IsPaired() {
		return
	}

	w.routingMu.Lock()
	if time.Since(w.autoRouteCheckedAt) < whatsappAutoRouteScanInterval {
		w.routingMu.Unlock()
		return
	}
	w.autoRouteCheckedAt = time.Now()
	w.routingMu.Unlock()

	candidates, err := w.discoverWorkflowCandidates(ctx, owner)
	if err != nil {
		log.Printf("[WHATSAPP] Couldn't provision default workflow routes: %v", err)
		return
	}

	w.routingMu.Lock()
	added := make([]string, 0, len(candidates))
	changed := false
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.ID) == "" || w.autoRoutedWorkflows[candidate.ID] {
			continue
		}
		// A workflow routed by hand counts as provisioned too, so removing
		// that hand-made route later doesn't bring a default one back.
		if w.routingHasWorkflowLocked(candidate.ID) {
			w.markWorkflowAutoRoutedLocked(candidate.ID)
			changed = true
			continue
		}
		added = append(added, w.assignWorkflowRouteLocked(candidate, candidate.WorkshopMode))
		w.markWorkflowAutoRoutedLocked(candidate.ID)
		changed = true
	}
	if len(added) > 0 {
		if err := w.persistRoutingLocked(ctx); err != nil {
			log.Printf("[WHATSAPP] Failed to persist default workflow routes: %v", err)
		}
	}
	if changed {
		if err := w.persistAutoRoutedWorkflowsLocked(ctx); err != nil {
			log.Printf("[WHATSAPP] Failed to persist auto-routed workflow ids: %v", err)
		}
	}
	w.routingMu.Unlock()

	if len(added) > 0 {
		log.Printf("[WHATSAPP] Provisioned %d default workflow route(s): @%s", len(added), strings.Join(added, ", @"))
	}
}

func (w *WhatsAppService) persistRoutingLocked(ctx context.Context) error {
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("whatsapp: meta store not open")
	}
	raw, err := json.Marshal(w.routing)
	if err != nil {
		return fmt.Errorf("whatsapp: marshal routing: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO whatsapp_meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		metaKeyRouting, string(raw),
	); err != nil {
		return fmt.Errorf("whatsapp: persist routing: %w", err)
	}
	return nil
}

// persistAutoRoutedWorkflowsLocked writes the provisioned-workflow set.
// Caller holds routingMu.
func (w *WhatsAppService) persistAutoRoutedWorkflowsLocked(ctx context.Context) error {
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("whatsapp: meta store not open")
	}
	ids := make([]string, 0, len(w.autoRoutedWorkflows))
	for id := range w.autoRoutedWorkflows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	raw, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("whatsapp: marshal auto-routed workflows: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO whatsapp_meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		metaKeyAutoRoutedWorkflows, string(raw),
	); err != nil {
		return fmt.Errorf("whatsapp: persist auto-routed workflows: %w", err)
	}
	return nil
}

func slugifyWhatsAppWorkflow(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

const whatsappActiveRouteHintInterval = 30 * time.Minute

func whatsappActiveRouteHintKey(chatJID, slug string) string {
	return chatJID + "\x00" + strings.ToLower(strings.TrimSpace(slug))
}

func (w *WhatsAppService) markActiveRouteHintSent(chatJID, slug string) {
	if chatJID == "" || slug == "" {
		return
	}
	w.activeRoutesMu.Lock()
	if w.activeRouteHints == nil {
		w.activeRouteHints = make(map[string]time.Time)
	}
	w.activeRouteHints[whatsappActiveRouteHintKey(chatJID, slug)] = time.Now()
	w.activeRoutesMu.Unlock()
}

func (w *WhatsAppService) shouldSendActiveRouteHint(chatJID, slug string) bool {
	if chatJID == "" || slug == "" {
		return false
	}
	key := whatsappActiveRouteHintKey(chatJID, slug)
	now := time.Now()
	w.activeRoutesMu.Lock()
	defer w.activeRoutesMu.Unlock()
	if w.activeRouteHints == nil {
		w.activeRouteHints = make(map[string]time.Time)
	}
	if last, ok := w.activeRouteHints[key]; ok && now.Sub(last) < whatsappActiveRouteHintInterval {
		return false
	}
	w.activeRouteHints[key] = now
	return true
}

func unknownWhatsAppWorkflowCommandMessage(slug string) string {
	if slug = strings.TrimSpace(slug); slug != "" {
		return fmt.Sprintf("Unknown @%s. Try @list, @switch <number>, @status, @sessions, @resume, @full, @concise, @done, or @off.", slug)
	}
	return "Commands: @list, @switch <number> [mode], @status, @sessions, @resume, @full, @concise, @done, @off"
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
		// Re-claim by same user (the pairing UI polls the QR): preserve the
		// original PairedAt and the product's default destination.
		o.PairedAt = existing.PairedAt
		o.DefaultProfileID = existing.DefaultProfileID
		o.DefaultUploadFolder = existing.DefaultUploadFolder
	}
	w.owner = o
	w.ownerMu.Unlock()

	if err := writeWhatsAppOwnerRow(db, o); err != nil {
		return err
	}
	if existing == nil {
		log.Printf("[WHATSAPP] Claimed ownership: user=%s email=%s", userID, email)
	}
	return nil
}

func writeWhatsAppOwnerRow(db *sql.DB, o *WhatsAppOwner) error {
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
	return nil
}

// SetDefaultProfile records the profile whose conversation takes this
// pairing's unrouted messages, and where its attachments go. Only the owner
// can set it; an empty profileID clears both. The caller has already checked
// the profile declares the whatsapp capability — this store knows nothing
// about profiles.
func (w *WhatsAppService) SetDefaultProfile(userID, profileID, uploadFolder string) error {
	userID = strings.TrimSpace(userID)
	profileID = strings.TrimSpace(profileID)
	uploadFolder = strings.Trim(strings.TrimSpace(uploadFolder), "/")
	if profileID == "" {
		uploadFolder = ""
	}
	w.mu.RLock()
	db := w.metaDB
	w.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("whatsapp: meta store not open — StartListening has not run")
	}
	w.ownerMu.Lock()
	defer w.ownerMu.Unlock()
	if w.owner == nil || w.owner.UserID == "" || w.owner.UserID != userID {
		return fmt.Errorf("whatsapp: pair this account first — only its owner can choose where messages go")
	}
	updated := *w.owner
	updated.DefaultProfileID = profileID
	updated.DefaultUploadFolder = uploadFolder
	if err := writeWhatsAppOwnerRow(db, &updated); err != nil {
		return err
	}
	w.owner = &updated
	if profileID == "" {
		log.Printf("[WHATSAPP] Default profile cleared for user=%s", userID)
	} else {
		log.Printf("[WHATSAPP] Default profile for user=%s: %s (attachments → %q)", userID, profileID, uploadFolder)
	}
	return nil
}

// DefaultProfile returns the pairing's default destination profile and its
// attachment folder; both empty when none is set (or the pairing is
// unclaimed).
func (w *WhatsAppService) DefaultProfile() (profileID, uploadFolder string) {
	w.ownerMu.RLock()
	defer w.ownerMu.RUnlock()
	if w.owner == nil {
		return "", ""
	}
	return w.owner.DefaultProfileID, w.owner.DefaultUploadFolder
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
	if conn := w.connector(); conn != nil {
		return conn.OwnJID()
	}
	return types.JID{}
}

// OwnLID returns the paired account's LID ("hidden user") JID if one has
// been assigned. Recent WhatsApp accounts have both a phone-number JID
// (@s.whatsapp.net) and a LID (@lid); self-chat messages can arrive with
// either as the chat JID depending on how the message was routed. Empty
// when unpaired or when the account hasn't been given a LID.
func (w *WhatsAppService) OwnLID() types.JID {
	if conn := w.connector(); conn != nil {
		return conn.OwnLID()
	}
	return types.JID{}
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
	conn := w.connector()
	return conn != nil && conn.IsSelfChat(chat)
}

func randomWhatsAppLinkCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return fmt.Sprintf("%06d", time.Now().UnixNano()%900000+100000)
	}
	return fmt.Sprintf("%06d", n.Int64()+100000)
}

func cloneWhatsAppAccessState(state WhatsAppAccessState) WhatsAppAccessState {
	out := state
	if len(state.BoundChats) > 0 {
		out.BoundChats = make([]WhatsAppBoundDMChat, len(state.BoundChats))
		copy(out.BoundChats, state.BoundChats)
		for i := range out.BoundChats {
			if len(state.BoundChats[i].Identities) > 0 {
				out.BoundChats[i].Identities = append([]string(nil), state.BoundChats[i].Identities...)
			}
		}
	}
	return out
}

func (w *WhatsAppService) ensureLinkCode() WhatsAppAccessState {
	w.accessMu.Lock()
	now := time.Now().UTC()
	if w.accessState.LinkCode == "" || (!w.accessState.LinkCodeExpires.IsZero() && now.After(w.accessState.LinkCodeExpires)) {
		w.accessState.LinkCode = randomWhatsAppLinkCode()
		w.accessState.LinkCodeExpires = now.Add(24 * time.Hour)
		if err := w.persistAccessStateLocked(context.Background()); err != nil {
			log.Printf("[WHATSAPP] Failed to persist link code: %v", err)
		}
	}
	state := cloneWhatsAppAccessState(w.accessState)
	w.accessMu.Unlock()
	return state
}

func (w *WhatsAppService) rotateLinkCodeLocked(now time.Time) {
	w.accessState.LinkCode = randomWhatsAppLinkCode()
	w.accessState.LinkCodeExpires = now.Add(24 * time.Hour)
}

func (w *WhatsAppService) GetAccessState() WhatsAppAccessState {
	return w.ensureLinkCode()
}

func whatsappJIDString(jid types.JID) string {
	if jid.IsEmpty() || jid.User == "" {
		return ""
	}
	return jid.String()
}

func whatsappDMIdentityCandidates(info types.MessageInfo) []string {
	seen := map[string]bool{}
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	add(whatsappJIDString(info.Chat))
	add(whatsappJIDString(info.Sender))
	add(whatsappJIDString(info.SenderAlt))
	add(whatsappJIDString(info.RecipientAlt))
	return out
}

func (w *WhatsAppService) isAllowedDM(info types.MessageInfo) bool {
	if w.isSelfChat(info.Chat) {
		return true
	}
	candidates := whatsappDMIdentityCandidates(info)
	if len(candidates) == 0 {
		return false
	}
	w.accessMu.RLock()
	defer w.accessMu.RUnlock()
	for _, bound := range w.accessState.BoundChats {
		for _, known := range bound.Identities {
			for _, candidate := range candidates {
				if known == candidate {
					return true
				}
			}
		}
	}
	return false
}

func (w *WhatsAppService) bindDMChat(info types.MessageInfo) {
	candidates := whatsappDMIdentityCandidates(info)
	if len(candidates) == 0 {
		return
	}
	now := time.Now().UTC()
	w.accessMu.Lock()
	defer w.accessMu.Unlock()
	matchIdx := -1
	for i, bound := range w.accessState.BoundChats {
		for _, known := range bound.Identities {
			for _, candidate := range candidates {
				if known == candidate {
					matchIdx = i
					break
				}
			}
			if matchIdx >= 0 {
				break
			}
		}
		if matchIdx >= 0 {
			break
		}
	}
	if matchIdx < 0 {
		w.accessState.BoundChats = append(w.accessState.BoundChats, WhatsAppBoundDMChat{
			ChatJID:    whatsappJIDString(info.Chat),
			Identities: candidates,
			BoundAt:    now,
			LastSeenAt: now,
		})
	} else {
		bound := &w.accessState.BoundChats[matchIdx]
		known := map[string]bool{}
		for _, id := range bound.Identities {
			known[id] = true
		}
		for _, candidate := range candidates {
			if !known[candidate] {
				bound.Identities = append(bound.Identities, candidate)
			}
		}
		if bound.ChatJID == "" {
			bound.ChatJID = whatsappJIDString(info.Chat)
		}
		bound.LastSeenAt = now
	}
	if err := w.persistAccessStateLocked(context.Background()); err != nil {
		log.Printf("[WHATSAPP] Failed to persist bound DM chat: %v", err)
	}
}

func parseWhatsAppLinkCommand(text string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 2 {
		return "", false
	}
	cmd := strings.ToLower(strings.TrimPrefix(fields[0], "@"))
	if cmd != "link" && cmd != "pair" {
		return "", false
	}
	code := strings.TrimSpace(fields[1])
	if len(code) != 6 {
		return "", false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return code, true
}

func (w *WhatsAppService) handleDMAccessLinkCommand(ctx context.Context, text, chatJID string, info types.MessageInfo) bool {
	code, ok := parseWhatsAppLinkCommand(text)
	if !ok {
		return false
	}
	state := w.ensureLinkCode()
	now := time.Now().UTC()
	if state.LinkCode == "" || code != state.LinkCode || (!state.LinkCodeExpires.IsZero() && now.After(state.LinkCodeExpires)) {
		w.sendWorkflowCommandReply(ctx, chatJID, "That WhatsApp link code is invalid or expired. Open WhatsApp settings in Runloop and send the current code.")
		return true
	}
	w.bindDMChat(info)
	w.accessMu.Lock()
	w.rotateLinkCodeLocked(now)
	if err := w.persistAccessStateLocked(ctx); err != nil {
		log.Printf("[WHATSAPP] Failed to rotate link code after successful bind: %v", err)
	}
	w.accessMu.Unlock()
	w.sendWorkflowCommandReply(ctx, chatJID, "Linked this WhatsApp chat. You can now send messages normally.")
	return true
}

// handleIncomingMessage converts a whatsmeow message event into a
// BotIncomingMessage and forwards it to the registered handler. Linked 1:1
// DMs are accepted, including the owner's "Message Yourself" chat and inbound
// DMs to a dedicated paired bot number after link-code binding. Group chats,
// broadcasts, status updates, and outgoing messages to other chats are
// skipped. Also adds an "eyes" reaction mirror of the Slack ack UX so the
// sender sees their message was received.
// Allow implements whatsappbot.AccessPolicy: the owner's own chat always
// talks to the bot; any other DM must have been linked with a link code
// ("@link 123456"), which is itself handled here.
func (w *WhatsAppService) Allow(ctx context.Context, msg *whatsappbot.Message) bool {
	info := msg.Event.Info
	log.Printf("[WHATSAPP] incoming: chat=%s sender=%s fromMe=%v type=%s",
		info.Chat.String(), info.Sender.String(), info.IsFromMe, info.Type)
	if w.GetOwner() == nil {
		log.Printf("[WHATSAPP] Dropping message from %s — pairing has no workspace-user owner; re-pair via the UI to claim", info.Sender.User)
		return false
	}
	chatJID := msg.Chat.String()
	if w.handleDMAccessLinkCommand(ctx, msg.Text, chatJID, info) {
		return false
	}
	if !w.isAllowedDM(info) {
		log.Printf("[WHATSAPP] skip: unlinked DM chat=%s sender=%s identities=%v", chatJID, info.Sender.String(), whatsappDMIdentityCandidates(info))
		return false
	}
	w.bindDMChat(info)
	return true
}

// HandleCommand implements whatsappbot.CommandHandler ("@list", "@switch",
// "@status", bot-session controls, "@off").
func (w *WhatsAppService) HandleCommand(ctx context.Context, msg *whatsappbot.Message) bool {
	owner := w.GetOwner()
	if owner == nil {
		return false
	}
	return w.handleWorkflowCommand(ctx, msg.Text, msg.Chat.String(), owner, msg.Event.Info)
}

// Resolve implements whatsappbot.Router: "@slug" names a configured
// workflow route, else one of the default product's own profiles.
func (w *WhatsAppService) Resolve(ctx context.Context, _ string, token string) *whatsappbot.Route {
	key := strings.ToLower(strings.TrimSpace(token))
	if route := w.resolveSlugRoute(key); route != nil {
		return &whatsappbot.Route{Key: key, Value: route}
	}
	profileRoute, err := w.resolveProfileRoute(ctx, key)
	if err != nil {
		// RouteUnknown shows the product's reason for this token.
		log.Printf("[WHATSAPP] @%s: %v", key, err)
		return nil
	}
	if profileRoute == nil {
		return nil
	}
	return &whatsappbot.Route{Key: key, Value: profileRoute}
}

// activeSlugStore persists the active "@slug" per chat in whatsapp_meta.
type activeSlugStore struct{ w *WhatsAppService }

func (s activeSlugStore) Active(chat string) string { return s.w.activeSlug(chat) }
func (s activeSlugStore) Activate(chat, key string) { s.w.setActiveSlug(chat, key) }
func (s activeSlugStore) Deactivate(chat string)    { s.w.clearActiveSlug(chat) }

// RouteActivated implements whatsappbot.RouteObserver. A bare "@slug" is
// acknowledged; "@slug <text>" just runs under the new route.
func (w *WhatsAppService) RouteActivated(ctx context.Context, msg *whatsappbot.Message, route *whatsappbot.Route, continuing bool) {
	if continuing {
		return
	}
	msg.React("👀")
	ack := fmt.Sprintf("Activated @%s for this chat. Send your next message normally. Type @%s deactivate to turn this off.", route.Key, route.Key)
	if profileRoute, ok := route.Value.(*ProfileRoute); ok && strings.TrimSpace(profileRoute.Label) != "" {
		ack = fmt.Sprintf("Now talking to %s. Send your next message normally. Type @%s deactivate to go back.", profileRoute.Label, route.Key)
	}
	if _, err := w.SendThreadMessage(ctx, ThreadID{Platform: "whatsapp", ChannelID: msg.Chat.String()}, ack); err != nil {
		log.Printf("[WHATSAPP] Failed to send activation acknowledgement for @%s: %v", route.Key, err)
	}
}

// RouteDeactivated implements whatsappbot.RouteObserver ("@slug deactivate").
func (w *WhatsAppService) RouteDeactivated(ctx context.Context, msg *whatsappbot.Message, key string) {
	msg.React("👀")
	if _, err := w.SendThreadMessage(ctx, ThreadID{Platform: "whatsapp", ChannelID: msg.Chat.String()}, fmt.Sprintf("Deactivated @%s. Plain WhatsApp messages will use the default chat again.", key)); err != nil {
		log.Printf("[WHATSAPP] Failed to send deactivate acknowledgement for @%s: %v", key, err)
	}
}

// RouteUnknown implements whatsappbot.RouteObserver: a bare unknown "@x"
// gets the command help.
func (w *WhatsAppService) RouteUnknown(ctx context.Context, msg *whatsappbot.Message, token string) {
	msg.React("👀")
	reply := unknownWhatsAppWorkflowCommandMessage(token)
	// A token the default product knows but cannot serve right now (its
	// router returned an error) gets the product's own explanation.
	if _, err := w.resolveProfileRoute(ctx, strings.ToLower(strings.TrimSpace(token))); err != nil {
		reply = err.Error()
	}
	if _, err := w.SendThreadMessage(ctx, ThreadID{Platform: "whatsapp", ChannelID: msg.Chat.String()}, reply); err != nil {
		log.Printf("[WHATSAPP] Failed to send unknown @ command help for @%s: %v", token, err)
	}
}

// HandleMessage implements whatsappbot.Handler: ingest any attachment into
// the workspace, then hand the message to the bot manager with the routed
// workflow (if any) preset.
func (w *WhatsAppService) HandleMessage(ctx context.Context, msg *whatsappbot.Message) {
	w.mu.RLock()
	handler := w.messageHandler
	w.mu.RUnlock()
	if handler == nil {
		log.Printf("[WHATSAPP] skip: no message handler registered")
		return
	}
	owner := w.GetOwner()
	if owner == nil {
		return
	}
	info := msg.Event.Info
	chatJID := msg.Chat.String()
	text := msg.Text

	var presetRoute *ChannelRoute
	var presetProfile *ProfileRoute
	routedSlug := ""
	if msg.Route != nil {
		presetRoute, _ = msg.Route.Value.(*ChannelRoute)
		presetProfile, _ = msg.Route.Value.(*ProfileRoute)
		routedSlug = msg.Route.Key
	}

	uploadFolder := whatsappWorkflowUploadFolder(presetRoute)
	if presetProfile != nil {
		uploadFolder = presetProfile.UploadFolder
	} else if presetRoute == nil && owner.DefaultProfileID != "" {
		// Unrouted messages run in the default profile's own conversation,
		// whose sandbox can only read its own workspace: save attachments
		// where that product asked for them, not in the per-user chat uploads.
		uploadFolder = owner.DefaultUploadFolder
	}
	media, mediaErr := w.downloadIncomingMedia(ctx, msg, owner, uploadFolder)
	if mediaErr != nil {
		log.Printf("[WHATSAPP] Failed to ingest media from %s: %v", info.Sender.User, mediaErr)
		msg.React("⚠️")
		if _, err := w.SendThreadMessage(ctx, ThreadID{Platform: "whatsapp", ChannelID: chatJID}, fmt.Sprintf("I couldn't process that WhatsApp attachment: %v", mediaErr)); err != nil {
			log.Printf("[WHATSAPP] Failed to send media error acknowledgement: %v", err)
		}
		return
	}
	if media != nil && media.VoiceSetupNeeded {
		if _, err := w.SendThreadMessage(ctx, ThreadID{Platform: "whatsapp", ChannelID: chatJID}, "🎙️ Voice notes need a one-time setup first — open SparkQuill on your computer and check Settings → Voice, then send this again."); err != nil {
			log.Printf("[WHATSAPP] Failed to send voice-setup notice: %v", err)
		}
	}
	if media != nil && media.Transcript != "" {
		// Echoed back immediately (ahead of however long the agent's own
		// reply takes) so the parent can catch a mishearing right away.
		if _, err := w.SendThreadMessage(ctx, ThreadID{Platform: "whatsapp", ChannelID: chatJID}, "🎧 Heard: \""+media.Transcript+"\""); err != nil {
			log.Printf("[WHATSAPP] Failed to send voice-note echo: %v", err)
		}
	}
	text = whatsappMessageTextForMedia(text, media)
	if text == "" {
		log.Printf("[WHATSAPP] skip: no text body or supported media (id=%s)", msg.ID)
		return
	}

	if routedSlug != "" && presetRoute != nil {
		log.Printf("[WHATSAPP] Incoming message from %s (%s) → user=%s, routed via @%s to workflow %s: %s",
			info.Sender.User, chatJID, owner.UserID, routedSlug, presetRoute.WorkflowID, botTruncate(text, 80))
	} else if routedSlug != "" && presetProfile != nil {
		log.Printf("[WHATSAPP] Incoming message from %s (%s) → user=%s, routed via @%s to profile %s (%s): %s",
			info.Sender.User, chatJID, owner.UserID, routedSlug, presetProfile.ProfileID, presetProfile.ConversationKey, botTruncate(text, 80))
	} else {
		log.Printf("[WHATSAPP] Incoming message from %s (%s) → user=%s: %s",
			info.Sender.User, chatJID, owner.UserID, botTruncate(text, 80))
	}

	// 👀 the instant we accept it; the bot manager swaps/clears it as the run progresses.
	msg.React("👀")
	handler(BotIncomingMessage{
		Platform:        "whatsapp",
		UserID:          info.Sender.User,
		UserName:        info.PushName,
		UserEmail:       owner.Email,  // binds the conversation to the workspace user
		WorkspaceUserID: owner.UserID, // pre-resolved so bot manager skips email lookup
		ChannelID:       chatJID,
		ThreadTS:        "",
		Text:            text,
		MessageTS:       info.ID,
		PresetWorkflow:  presetRoute,
		PresetProfile:   presetProfile,
		Timestamp:       info.Timestamp,
		IsThreadReply:   false,
		IsMention:       true, // every DM effectively addresses the bot
	})
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
// the user can set the env var to "🤖 " or similar if they want labeling.
func (w *WhatsAppService) SendThreadMessage(ctx context.Context, threadID ThreadID, message string) (string, error) {
	conn := w.connector()
	if conn == nil {
		return "", fmt.Errorf("whatsapp: client not initialized")
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

	if activeSlug := w.activeSlug(threadID.ChannelID); activeSlug != "" {
		if w.resolveSlugRoute(activeSlug) != nil {
			deactivateHint := fmt.Sprintf("@%s deactivate", activeSlug)
			if strings.Contains(strings.ToLower(message), strings.ToLower(deactivateHint)) {
				w.markActiveRouteHintSent(threadID.ChannelID, activeSlug)
			} else if w.shouldSendActiveRouteHint(threadID.ChannelID, activeSlug) {
				message = strings.TrimSpace(message) + fmt.Sprintf("\n\n[%s active | @off]", activeSlug)
			}
		} else {
			w.clearActiveSlug(threadID.ChannelID)
		}
	}

	if w.selfChatPrefix != "" && w.isSelfChat(jid) {
		message = w.selfChatPrefix + message
	}

	parts := splitLongText(message, 4000)
	logBotOutboundMessage("whatsapp", threadID, "thread", message, len(parts), 0)
	var lastID string
	for _, part := range parts {
		id, err := conn.SendTextID(ctx, jid, part)
		if err != nil {
			return lastID, fmt.Errorf("whatsapp: send: %w", err)
		}
		lastID = id
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
	conn := w.connector()
	if conn == nil {
		return nil
	}
	chatJID, err := types.ParseJID(channelID)
	if err != nil {
		return nil
	}
	return conn.React(ctx, chatJID, senderJID, messageID, emoji)
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
	conn := w.connector()
	if conn == nil || channelID == "" {
		return ""
	}
	jid, err := types.ParseJID(channelID)
	if err != nil {
		return ""
	}
	return conn.ChatName(ctx, jid)
}

// SetMessageHandler registers the callback invoked on every inbound text
// message. Called once during bot manager setup.
// ProfileRouterFunc resolves an @token to one of the default product's own
// profiles for this pairing's owner. nil route with nil error: not the
// product's token; an error: the product knows the token but cannot serve it
// now (the text is shown in the chat).
type ProfileRouterFunc func(ctx context.Context, token string) (*ProfileRoute, error)

// SetProfileRouter installs the default product's @token router.
func (w *WhatsAppService) SetProfileRouter(router ProfileRouterFunc) {
	w.mu.Lock()
	w.profileRouter = router
	w.mu.Unlock()
}

// VoiceTranscriberFunc transcribes a downloaded audio file on-device,
// returning the words heard (empty if the clip was silent). Set by the
// server layer, which owns the speech engine. It must not trigger the
// engine's one-time model download — that can take minutes, far past what a
// WhatsApp message handler should block on — and must return
// ErrVoiceNotInstalled instead when the model isn't already on disk.
type VoiceTranscriberFunc func(ctx context.Context, path string) (string, error)

// ErrVoiceNotInstalled is what a VoiceTranscriberFunc returns when the
// on-device speech model isn't installed (or the build has no speech engine
// at all) rather than attempting the download. A voice note then gets a
// plain setup reply instead of silently trying to transcribe nothing.
var ErrVoiceNotInstalled = errors.New("whatsapp: voice transcription is not installed")

// SetVoiceTranscriber installs the on-device transcriber for incoming voice
// notes.
func (w *WhatsAppService) SetVoiceTranscriber(fn VoiceTranscriberFunc) {
	w.mu.Lock()
	w.voiceTranscriber = fn
	w.mu.Unlock()
}

func (w *WhatsAppService) resolveProfileRoute(ctx context.Context, token string) (*ProfileRoute, error) {
	w.mu.RLock()
	router := w.profileRouter
	w.mu.RUnlock()
	if router == nil {
		return nil, nil
	}
	return router(ctx, token)
}

func (w *WhatsAppService) SetMessageHandler(handler BotMessageHandler) {
	w.mu.Lock()
	w.messageHandler = handler
	w.mu.Unlock()
}

func (w *WhatsAppService) SetBotThreadStatusProvider(provider BotThreadStatusFunc) {
	w.mu.Lock()
	w.statusProvider = provider
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
// WhatsApp message to the explicit chat hint, the user's notification
// preference, or the paired account's own number; any button options are
// flattened into numbered choices the user replies with.
func (w *WhatsAppService) SendNotification(ctx context.Context, uniqueID, message, contextMsg string, opts *ButtonOptions, dest *NotificationDestination) (string, error) {
	body := message
	if contextMsg != "" {
		body = body + "\n\n" + contextMsg
	}
	if opts != nil {
		if opts.YesNoOnly {
			yes := strings.TrimSpace(opts.YesLabel)
			if yes == "" {
				yes = "Approve"
			}
			no := strings.TrimSpace(opts.NoLabel)
			if no == "" {
				no = "Reject"
			}
			body += fmt.Sprintf("\n\nReply with: %s or %s", yes, no)
		}
		for i, label := range opts.Options {
			body += fmt.Sprintf("\n%d) %s", i+1, label)
		}
	}
	body += fmt.Sprintf("\n\nRequest ID: %s", uniqueID)

	threadID, ok, err := w.notificationThreadID(dest)
	if err != nil || !ok {
		return "", err
	}
	return w.SendThreadMessage(ctx, threadID, body)
}

func (w *WhatsAppService) SendUserNotification(ctx context.Context, message, contextMsg string, dest *NotificationDestination) (string, error) {
	body := strings.TrimSpace(message)
	if body == "" {
		return "", nil
	}
	if contextMsg = strings.TrimSpace(contextMsg); contextMsg != "" {
		body += "\n\n" + contextMsg
	}
	threadID, ok, err := w.notificationThreadID(dest)
	if err != nil || !ok {
		return "", nil
	}
	return w.SendThreadMessage(ctx, threadID, body)
}

func (w *WhatsAppService) notificationThreadID(dest *NotificationDestination) (ThreadID, bool, error) {
	if dest != nil && dest.WhatsApp != nil {
		if channelID := strings.TrimSpace(dest.WhatsApp.ChannelID); channelID != "" {
			return ThreadID{Platform: "whatsapp", ChannelID: channelID}, true, nil
		}
		if phoneJID := whatsappJIDFromPhone(dest.WhatsApp.PhoneE164); phoneJID != "" {
			return ThreadID{Platform: "whatsapp", ChannelID: phoneJID}, true, nil
		}
	}
	if dest != nil && strings.TrimSpace(dest.UserID) != "" {
		if pref := getNotificationPreferences(dest.UserID); pref != nil {
			if pref.WhatsAppDisabled {
				return ThreadID{}, false, nil
			}
			if phoneJID := whatsappJIDFromPhone(pref.WhatsAppPhone); phoneJID != "" {
				return ThreadID{Platform: "whatsapp", ChannelID: phoneJID}, true, nil
			}
		}
	}

	ownJID := w.OwnJID()
	if ownJID.IsEmpty() {
		return ThreadID{}, false, fmt.Errorf("whatsapp: not paired — cannot send notification")
	}
	return ThreadID{Platform: "whatsapp", ChannelID: ownJID.String()}, true, nil
}

func whatsappJIDFromPhone(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if digits == "" {
		return ""
	}
	return digits + "@" + types.DefaultUserServer
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
