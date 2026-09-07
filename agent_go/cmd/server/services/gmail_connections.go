package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"
)

// Connection registry for the Gmail channel.
//
// One connection = one isolated gws config directory = one authenticated Google
// identity. gws itself has no notion of profiles: it reads whichever directory
// GOOGLE_WORKSPACE_CLI_CONFIG_DIR names (see gmailChildEnv), so the registry
// here is what turns a pile of directories into named, selectable accounts.
//
// This file owns the *model and bookkeeping* only. Delivery still resolves the
// single legacy config — wiring send/sendRaw to a resolved connection is a
// separate change, so adding connections is inert until then.
//
// Deliberately absent: any token or secret field. Credential material belongs
// in the connection's private directory (or a secret store), never in
// gmail-config.json, which lives in the workspace and is readable by anything
// that can read the workspace.

// GmailConnectionStatus is the health of one connection, as last observed.
type GmailConnectionStatus string

const (
	// GmailConnectionUnknown is the zero value: never checked yet.
	GmailConnectionUnknown GmailConnectionStatus = ""
	// GmailConnectionConnected means gws reported usable credentials.
	GmailConnectionConnected GmailConnectionStatus = "connected"
	// GmailConnectionNeedsReconnect means credentials are missing or expired.
	// The UI offers Reconnect rather than surfacing a raw error.
	GmailConnectionNeedsReconnect GmailConnectionStatus = "needs_reconnect"
)

// GmailConnection is one sending identity.
type GmailConnection struct {
	// ID is stable for the life of the connection and is what workflow config
	// references. An identifier, never a secret.
	ID string `json:"id"`

	// DisplayName is the human label shown in the UI ("Ops", "Support").
	DisplayName string `json:"display_name"`

	// Email is the authenticated address, discovered via
	// `gws gmail users getProfile` rather than configured by hand. Empty until
	// the connection has been checked at least once.
	Email string `json:"email,omitempty"`

	// ConfigHome is the isolated gws config directory backing this connection,
	// exported as GOOGLE_WORKSPACE_CLI_CONFIG_DIR. Connections created through
	// the registry get a private directory under gmailConnectionsBaseDir();
	// a migrated legacy connection may point anywhere, including the gws
	// default when the field was never set.
	ConfigHome string `json:"config_home,omitempty"`

	// CredentialsFile optionally pins a service-account or user key file.
	CredentialsFile string `json:"credentials_file,omitempty"`

	// ClientName names the OAuth app registration (gmail_oauth_clients.go)
	// this connection authorizes under. Required for every connection
	// created through CreateConnection; empty only on a connection that
	// predates the named-client registry, until
	// GmailService.ImportLegacyOAuthClient backfills it.
	ClientName string `json:"client_name,omitempty"`

	Status GmailConnectionStatus `json:"status,omitempty"`

	// Enabled gates use without discarding the connection. A disabled
	// connection is never selectable and never inherited as the default.
	Enabled bool `json:"enabled"`

	// Scopes is the granted scope list from the last auth check.
	Scopes []string `json:"scopes,omitempty"`

	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// Clone returns a deep copy, so callers cannot mutate registry state through a
// returned value.
func (c GmailConnection) Clone() GmailConnection {
	out := c
	out.Scopes = append([]string(nil), c.Scopes...)
	return out
}

// gmailConnectionsBaseDir is where registry-created config directories live.
// Mirrors the WhatsApp session-directory convention: an env override, else a
// path under the workspace config directory.
func gmailConnectionsBaseDir() string {
	if v := strings.TrimSpace(os.Getenv("GMAIL_CONNECTIONS_DIR")); v != "" {
		return v
	}
	return filepath.Join(fsutil.WorkspaceDocsRoot(), "config", "gmail-connections")
}

// nextGmailConnectionID returns the lowest unused gmail_NNN identifier.
// Sequential rather than random so directories on disk stay readable.
func nextGmailConnectionID(existing []GmailConnection) string {
	taken := make(map[string]bool, len(existing))
	for _, c := range existing {
		taken[c.ID] = true
	}
	for n := 1; ; n++ {
		id := fmt.Sprintf("gmail_%03d", n)
		if !taken[id] {
			return id
		}
	}
}

// migrateGmailConnections seeds the registry from a pre-registry config.
//
// Runs inside normalizeGmailConfig, which every read and write passes through,
// so an existing install migrates on first load with no separate job and no
// user action. The legacy top-level auth fields are left in place: an
// un-migrated file must still parse, and delivery still reads them.
func migrateGmailConnections(cfg *GmailConfig) {
	if cfg == nil || len(cfg.Connections) > 0 {
		return
	}
	// Nothing worth carrying: a blank or never-configured install stays empty
	// rather than gaining a placeholder connection that names no account.
	if !cfg.Enabled &&
		strings.TrimSpace(cfg.ConfigHome) == "" &&
		strings.TrimSpace(cfg.CredentialsFile) == "" &&
		strings.TrimSpace(cfg.Token) == "" {
		return
	}
	now := time.Now().UTC()
	cfg.Connections = []GmailConnection{{
		ID:              "gmail_001",
		DisplayName:     "Default",
		ConfigHome:      strings.TrimSpace(cfg.ConfigHome),
		CredentialsFile: strings.TrimSpace(cfg.CredentialsFile),
		Enabled:         true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}}
	cfg.DefaultConnectionID = "gmail_001"
}

// hostAccountConnection builds the registry entry for the host's own gws
// login. migrateGmailConnections only seeds from a legacy gmail-config.json;
// an operator who ran `gws auth login` on the host (RTS, 2026-09-03) had a
// working sender that the Sending accounts panel could not see ("No sending
// accounts yet") and could not select per workflow. ConfigHome stays empty on
// purpose: that is the host default gws directory, which the server's own
// environment already points at, and the registry never deletes a directory
// it did not provision. The address, not a placeholder, is the display name
// so the panel shows which mailbox mail leaves from.
func hostAccountConnection(st GmailAuthStatus, now time.Time) (GmailConnection, bool) {
	email := strings.TrimSpace(st.Email)
	if !st.GwsInstalled || !st.Authenticated || !st.HasGmailScope || email == "" {
		return GmailConnection{}, false
	}
	return GmailConnection{
		ID:          nextGmailConnectionID(nil),
		DisplayName: email,
		Email:       email,
		Status:      GmailConnectionConnected,
		Enabled:     true,
		Scopes:      append([]string(nil), st.Scopes...),
		CreatedAt:   now,
		UpdatedAt:   now,
	}, true
}

// AdoptHostAccount seeds an empty registry from the host's own authenticated
// gws login, so the account the host already sends from appears as the
// default connection without the operator re-adding it by hand. A no-op once
// any connection exists, and once the operator has deleted an adopted host
// account (HostAccountDismissed): deleting it is a decision, not something
// the next page load should undo.
func (g *GmailService) AdoptHostAccount(ctx context.Context) (GmailConnection, bool, error) {
	cfg := g.GetConfig()
	if len(cfg.Connections) > 0 || cfg.HostAccountDismissed {
		return GmailConnection{}, false, nil
	}
	conn, ok := hostAccountConnection(g.AuthStatus(ctx), time.Now().UTC())
	if !ok {
		return GmailConnection{}, false, nil
	}
	cfg.Connections = []GmailConnection{conn}
	cfg.DefaultConnectionID = conn.ID
	if err := g.SaveConfig(ctx, cfg); err != nil {
		return GmailConnection{}, false, err
	}
	return conn, true, nil
}

// normalizeGmailConnections repairs registry invariants: no blank IDs, no
// duplicates, and a DefaultConnectionID that names an enabled member.
func normalizeGmailConnections(cfg *GmailConfig) {
	if cfg == nil {
		return
	}
	seen := make(map[string]bool, len(cfg.Connections))
	out := cfg.Connections[:0]
	for _, c := range cfg.Connections {
		c.ID = strings.TrimSpace(c.ID)
		if c.ID == "" || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		c.DisplayName = strings.TrimSpace(c.DisplayName)
		if c.DisplayName == "" {
			c.DisplayName = c.ID
		}
		c.Email = strings.TrimSpace(c.Email)
		c.ConfigHome = strings.TrimSpace(c.ConfigHome)
		c.CredentialsFile = strings.TrimSpace(c.CredentialsFile)
		c.ClientName = strings.TrimSpace(c.ClientName)
		out = append(out, c)
	}
	cfg.Connections = out

	// A default that names a missing or disabled connection would silently
	// reroute mail to another account. Drop it instead and let the caller
	// treat "no default" as a configuration error.
	if cfg.DefaultConnectionID != "" {
		valid := false
		for _, c := range cfg.Connections {
			if c.ID == cfg.DefaultConnectionID && c.Enabled {
				valid = true
				break
			}
		}
		if !valid {
			cfg.DefaultConnectionID = ""
		}
	}
	// Exactly one enabled connection needs no explicit choice.
	if cfg.DefaultConnectionID == "" {
		var enabled []string
		for _, c := range cfg.Connections {
			if c.Enabled {
				enabled = append(enabled, c.ID)
			}
		}
		if len(enabled) == 1 {
			cfg.DefaultConnectionID = enabled[0]
		}
	}
}

// ListConnections returns every connection, ordered by ID for a stable UI.
func (g *GmailService) ListConnections() []GmailConnection {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.config == nil {
		return nil
	}
	out := make([]GmailConnection, 0, len(g.config.Connections))
	for _, c := range g.config.Connections {
		out = append(out, c.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetConnection looks one up by ID.
func (g *GmailService) GetConnection(id string) (GmailConnection, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return findGmailConnection(g.config, id)
}

func findGmailConnection(cfg *GmailConfig, id string) (GmailConnection, bool) {
	id = strings.TrimSpace(id)
	if cfg == nil || id == "" {
		return GmailConnection{}, false
	}
	for _, c := range cfg.Connections {
		if c.ID == id {
			return c.Clone(), true
		}
	}
	return GmailConnection{}, false
}

// DefaultConnection returns the connection used when a send names none.
// Reports false rather than guessing when no default is configured — picking an
// arbitrary account would send mail from the wrong identity.
func (g *GmailService) DefaultConnection() (GmailConnection, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.config == nil || g.config.DefaultConnectionID == "" {
		return GmailConnection{}, false
	}
	c, ok := findGmailConnection(g.config, g.config.DefaultConnectionID)
	if !ok || !c.Enabled {
		return GmailConnection{}, false
	}
	return c, true
}

// GmailConnectionInput is the mutable subset of a connection. Fields the server
// discovers (Email, Status, Scopes) are not settable by a caller.
type GmailConnectionInput struct {
	DisplayName     string
	ConfigHome      string
	CredentialsFile string
	// ClientName selects which named OAuth client (gmail_oauth_clients.go)
	// this connection authorizes under. Required on create — see CreateConnection.
	ClientName string
	Enabled    *bool
}

// CreateConnection registers a new sending identity and provisions its private
// config directory. The directory starts empty: authenticating it is a separate
// step, so a fresh connection is correctly reported as needing to connect.
//
// ClientName is required: every connection created through this registry
// must name which OAuth app it authorizes under, so a second Google Cloud
// project's credentials can never silently replace the one an existing
// connection depends on (the shared client_secret.json failure mode this
// registry replaces).
func (g *GmailService) CreateConnection(ctx context.Context, in GmailConnectionInput) (GmailConnection, error) {
	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		return GmailConnection{}, fmt.Errorf("gmail connection: display name is required")
	}
	clientName := strings.TrimSpace(in.ClientName)
	if clientName == "" {
		return GmailConnection{}, fmt.Errorf("gmail connection: client_name is required — add an OAuth client first")
	}
	if !OAuthClientExists(clientName) {
		return GmailConnection{}, fmt.Errorf("gmail connection: OAuth client %q not found", clientName)
	}

	cfg := g.GetConfig()
	id := nextGmailConnectionID(cfg.Connections)

	configHome := strings.TrimSpace(in.ConfigHome)
	if configHome == "" {
		configHome = filepath.Join(gmailConnectionsBaseDir(), id)
		if err := os.MkdirAll(configHome, 0o700); err != nil {
			return GmailConnection{}, fmt.Errorf("gmail connection: create config dir: %w", err)
		}
	}

	now := time.Now().UTC()
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	conn := GmailConnection{
		ID:              id,
		DisplayName:     name,
		ConfigHome:      configHome,
		CredentialsFile: strings.TrimSpace(in.CredentialsFile),
		ClientName:      clientName,
		Status:          GmailConnectionNeedsReconnect,
		Enabled:         enabled,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	cfg.Connections = append(cfg.Connections, conn)
	if err := g.SaveConfig(ctx, cfg); err != nil {
		return GmailConnection{}, err
	}
	return conn, nil
}

// UpdateConnection edits the mutable fields of one connection.
func (g *GmailService) UpdateConnection(ctx context.Context, id string, in GmailConnectionInput) (GmailConnection, error) {
	cfg := g.GetConfig()
	idx := -1
	for i, c := range cfg.Connections {
		if c.ID == strings.TrimSpace(id) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return GmailConnection{}, fmt.Errorf("gmail connection %q not found", id)
	}

	conn := &cfg.Connections[idx]
	if v := strings.TrimSpace(in.DisplayName); v != "" {
		conn.DisplayName = v
	}
	if v := strings.TrimSpace(in.ConfigHome); v != "" {
		conn.ConfigHome = v
	}
	if v := strings.TrimSpace(in.CredentialsFile); v != "" {
		conn.CredentialsFile = v
	}
	if v := strings.TrimSpace(in.ClientName); v != "" {
		if !OAuthClientExists(v) {
			return GmailConnection{}, fmt.Errorf("gmail connection: OAuth client %q not found", v)
		}
		conn.ClientName = v
	}
	if in.Enabled != nil {
		conn.Enabled = *in.Enabled
	}
	conn.UpdatedAt = time.Now().UTC()

	updated := conn.Clone()
	if err := g.SaveConfig(ctx, cfg); err != nil {
		return GmailConnection{}, err
	}
	return updated, nil
}

// SetDefaultConnection chooses the connection used when a send names none.
func (g *GmailService) SetDefaultConnection(ctx context.Context, id string) error {
	cfg := g.GetConfig()
	conn, ok := findGmailConnection(cfg, id)
	if !ok {
		return fmt.Errorf("gmail connection %q not found", id)
	}
	if !conn.Enabled {
		return fmt.Errorf("gmail connection %q is disabled and cannot be the default", id)
	}
	cfg.DefaultConnectionID = conn.ID
	return g.SaveConfig(ctx, cfg)
}

// DeleteConnection removes a connection and, when the registry provisioned it,
// its config directory and stored credentials.
func (g *GmailService) DeleteConnection(ctx context.Context, id string) error {
	cfg := g.GetConfig()
	conn, ok := findGmailConnection(cfg, id)
	if !ok {
		return fmt.Errorf("gmail connection %q not found", id)
	}

	kept := make([]GmailConnection, 0, len(cfg.Connections))
	for _, c := range cfg.Connections {
		if c.ID != conn.ID {
			kept = append(kept, c)
		}
	}
	cfg.Connections = kept
	if cfg.DefaultConnectionID == conn.ID {
		cfg.DefaultConnectionID = ""
	}
	// An adopted host account (empty ConfigHome, see AdoptHostAccount) that
	// the operator removes must stay removed; otherwise the next listing
	// would adopt it straight back.
	if strings.TrimSpace(conn.ConfigHome) == "" && len(kept) == 0 {
		cfg.HostAccountDismissed = true
	}

	if err := g.SaveConfig(ctx, cfg); err != nil {
		return err
	}

	// Only remove directories this registry created. A migrated connection may
	// point at ~/.config/gws or an operator-managed path, and deleting a
	// bookkeeping row must never destroy credentials we do not own.
	if ownsGmailConnectionDir(conn.ConfigHome) {
		if err := os.RemoveAll(conn.ConfigHome); err != nil {
			return fmt.Errorf("gmail connection: remove config dir: %w", err)
		}
	}
	// Always drop the stored OAuth credential, whoever owned the directory:
	// removing the connection must not leave a token that can still send.
	deleteGmailOAuthToken(conn.ID)
	return nil
}

// ownsGmailConnectionDir reports whether dir is a registry-provisioned config
// directory, i.e. strictly inside gmailConnectionsBaseDir(). Guards deletion.
func ownsGmailConnectionDir(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	base, err := filepath.Abs(gmailConnectionsBaseDir())
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return false
	}
	// Inside base and not base itself: no "..", no absolute escape, non-empty.
	if rel == "." || rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return false
	}
	return true
}

// gmailConnectionConfig projects a connection onto the auth knobs gmailChildEnv
// understands, so a connection can drive a gws invocation the same way the
// legacy singleton config does.
//
// A connection authenticated through the server's own OAuth flow carries a
// short-lived access token instead of pointing at a gws credential store.
// GOOGLE_WORKSPACE_CLI_TOKEN outranks every other source in gws, so such a
// connection never needs `gws auth login` to have run.
func gmailConnectionConfig(conn GmailConnection) *GmailConfig {
	cfg := &GmailConfig{
		ConfigHome:      conn.ConfigHome,
		CredentialsFile: conn.CredentialsFile,
	}
	if token, err := accessTokenForConnection(context.Background(), conn.ID, conn.ClientName); err == nil && token != "" {
		cfg.Token = token
	}
	return cfg
}

// gmailAuthKnobsEqual reports whether two connections would run gws under the
// same credentials. Only the auth-relevant fields count: renaming a connection
// or toggling its default must not throw away a valid cached auth status.
func gmailAuthKnobsEqual(a, b GmailConnection) bool {
	return a.ConfigHome == b.ConfigHome && a.CredentialsFile == b.CredentialsFile && a.ClientName == b.ClientName
}

// retainedGmailAuthCaches decides which cached auth statuses survive a config
// reload.
//
// A wholesale flush would send every connection back to a "checking" spinner
// whenever any one of them was edited, and each recheck costs a ~5.5s
// subprocess. So a connection keeps its cached status when its auth knobs are
// byte-identical across the reload, and loses it otherwise.
//
// The legacy singleton slot ("") is always dropped: its auth knobs live in
// top-level config fields that this comparison does not cover, so retaining it
// risks reporting the previous account's status.
func retainedGmailAuthCaches(
	caches map[string]*gmailAuthCacheEntry,
	previous, next *GmailConfig,
) map[string]*gmailAuthCacheEntry {
	if len(caches) == 0 || previous == nil || next == nil {
		return nil
	}
	before := make(map[string]GmailConnection, len(previous.Connections))
	for _, c := range previous.Connections {
		before[c.ID] = c
	}

	retained := make(map[string]*gmailAuthCacheEntry)
	for _, c := range next.Connections {
		entry := caches[c.ID]
		if entry == nil {
			continue
		}
		if old, ok := before[c.ID]; ok && gmailAuthKnobsEqual(old, c) {
			retained[c.ID] = entry
		}
	}
	if len(retained) == 0 {
		return nil
	}
	return retained
}

// destGmailConnectionIDs reads the requested sender(s) from a destination hint.
// Sender selection is deliberately separate from pickRecipient: one decides who
// mail comes FROM, the other who it goes TO, and entangling them is how a
// recipient rule ends up silently changing the sending identity.
//
// Returns nil when none is named, which means "use the default connection" —
// distinct from an empty non-nil list, which cannot occur here.
func destGmailConnectionIDs(dest *NotificationDestination) []string {
	if dest == nil || dest.Gmail == nil {
		return nil
	}
	out := make([]string, 0, len(dest.Gmail.ConnectionIDs))
	seen := map[string]bool{}
	for _, id := range dest.Gmail.ConnectionIDs {
		id = strings.TrimSpace(id)
		// Deduplicate: naming the same account twice must not send twice.
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolveSendConfig picks the auth knobs one send runs under.
//
// Precedence: an explicitly named connection, else the configured default.
// There is no third tier. When the named connection is missing or disabled the
// send fails — it never falls through to another account, because mail leaving
// from an unintended identity is worse than mail not leaving at all.
//
// With an empty registry this returns the legacy singleton config unchanged, so
// installs that have not adopted connections behave exactly as before.
//
// Returns the RESOLVED connection ID alongside the config. Provenance must
// record which account actually sent, not which one was asked for — those
// differ precisely when a send inherits the default, which is the common case.
func (g *GmailService) resolveSendConfig(connectionID string) (*GmailConfig, string, error) {
	g.mu.RLock()
	cfg := g.config
	g.mu.RUnlock()

	connectionID = strings.TrimSpace(connectionID)
	if cfg == nil {
		cfg = &GmailConfig{}
	}

	if len(cfg.Connections) == 0 {
		if connectionID != "" {
			return nil, "", fmt.Errorf("gmail connection %q was requested but no connections are configured", connectionID)
		}
		// No registry: the legacy singleton sends, and has no ID to report.
		return cfg, "", nil
	}

	if connectionID != "" {
		conn, ok := findGmailConnection(cfg, connectionID)
		if !ok {
			return nil, "", fmt.Errorf("gmail connection %q not found", connectionID)
		}
		if !conn.Enabled {
			return nil, "", fmt.Errorf("gmail connection %q (%s) is disabled — reconnect it before sending",
				conn.ID, conn.DisplayName)
		}
		return gmailConnectionConfig(conn), conn.ID, nil
	}

	if cfg.DefaultConnectionID == "" {
		return nil, "", fmt.Errorf("no default gmail connection is set — choose which account should send")
	}
	conn, ok := findGmailConnection(cfg, cfg.DefaultConnectionID)
	if !ok || !conn.Enabled {
		return nil, "", fmt.Errorf("the default gmail connection %q is unavailable — reconnect it before sending",
			cfg.DefaultConnectionID)
	}
	return gmailConnectionConfig(conn), conn.ID, nil
}

// destWorkflowName reads the workflow identity from a destination hint, for
// provenance only. Never influences routing.
func destWorkflowName(dest *NotificationDestination) string {
	if dest == nil {
		return ""
	}
	return strings.TrimSpace(dest.WorkflowName)
}

// EffectiveAuthStatusCached reports the auth state that represents the account
// as a whole, without blocking.
//
// Once connections exist the legacy singleton config is no longer what sends:
// its top-level ConfigHome is typically empty, so checking it inspects the
// default ~/.config/gws — which an install using connections never
// authenticates. Reading that as the account's health reports "not ready" while
// mail is in fact sending fine. The account's health is the DEFAULT
// connection's health, falling back to the singleton only when no connections
// are configured.
func (g *GmailService) EffectiveAuthStatusCached() GmailAuthStatus {
	if conn, ok := g.DefaultConnection(); ok {
		if st, found := g.AuthStatusForConnection(conn.ID); found {
			return st
		}
	}
	return g.AuthStatusCached()
}

// EffectiveAuthStatus is the blocking counterpart, for callers that need a
// definitive answer rather than a cached badge.
func (g *GmailService) EffectiveAuthStatus(ctx context.Context) GmailAuthStatus {
	if conn, ok := g.DefaultConnection(); ok {
		if st, found := g.AuthStatusForConnectionBlocking(ctx, conn.ID); found {
			return st
		}
	}
	return g.AuthStatus(ctx)
}

// InvalidateConnectionAuthCache drops one connection's cached auth status.
//
// Required after authorizing: the cached entry was computed while the
// connection had no credential, and the 60s TTL would otherwise serve that
// stale "not authenticated, no address" answer to the very code that just
// connected the account.
func (g *GmailService) InvalidateConnectionAuthCache(id string) {
	id = strings.TrimSpace(id)
	g.mu.Lock()
	delete(g.authCaches, id)
	// The account-wide slot is derived from the default connection, so it can
	// be describing this connection too.
	delete(g.authCaches, "")
	g.mu.Unlock()
}

// MarkConnectionConnected records a successful authorization: the discovered
// address and a connected status, persisted so the account is named everywhere
// immediately rather than only after the next auth refresh.
//
// Goes through SaveConfig, which reloads the service in-process — that is what
// makes a newly connected account appear without restarting the server.
func (g *GmailService) MarkConnectionConnected(ctx context.Context, id, email string) (GmailConnection, error) {
	cfg := g.GetConfig()
	idx := -1
	for i, c := range cfg.Connections {
		if c.ID == strings.TrimSpace(id) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return GmailConnection{}, fmt.Errorf("gmail connection %q not found", id)
	}

	conn := &cfg.Connections[idx]
	if e := strings.TrimSpace(email); e != "" {
		conn.Email = e
	}
	conn.Status = GmailConnectionConnected
	// Reconnecting a disabled connection is a request to use it again.
	conn.Enabled = true
	conn.UpdatedAt = time.Now().UTC()

	updated := conn.Clone()
	if err := g.SaveConfig(ctx, cfg); err != nil {
		return GmailConnection{}, err
	}
	return updated, nil
}

// shouldAutoEnableGmail decides whether an authenticated gws is reason enough
// to switch the channel on by itself. The operator asked for exactly that
// (2026-09-03): gws present, authenticated, Gmail scope granted -> enabled,
// no toggle to find. A deliberate disable in the settings form is respected.
func shouldAutoEnableGmail(cfg *GmailConfig, st GmailAuthStatus) bool {
	if cfg == nil || cfg.Enabled || cfg.ManuallyDisabled {
		return false
	}
	return st.GwsInstalled && st.Authenticated && st.HasGmailScope
}

// EnableIfAuthenticated switches the Gmail channel on when the effective
// sending account is authenticated with the Gmail scope, persists that, and
// registers the connector with the live NotificationManager so notify_user
// offers Gmail on the next turn without a restart. Reports whether it did.
func (g *GmailService) EnableIfAuthenticated(ctx context.Context) (bool, error) {
	cfg := g.GetConfig()
	if cfg.Enabled || cfg.ManuallyDisabled {
		return false, nil
	}
	if !shouldAutoEnableGmail(cfg, g.EffectiveAuthStatus(ctx)) {
		return false, nil
	}
	cfg.Enabled = true
	if err := g.SaveConfig(ctx, cfg); err != nil {
		return false, err
	}
	if nm := GetNotificationManager(); nm != nil && g.IsEnabled() {
		nm.RegisterConnector(g)
	}
	return true, nil
}

// autoDefaultGmailRecipient reports the address a fresh config should default
// its recipient to: the account's own authenticated sending address, and only
// when no recipient has ever been set. A running gws login already answers
// "which Google account", so a human should not have to retype that same
// address into a second field before Gmail can actually deliver anything.
// Never overrides an existing value -- an operator who wants a different
// recipient, or an empty one, is respected once DefaultTo has been set once.
func autoDefaultGmailRecipient(cfg *GmailConfig, st GmailAuthStatus) (string, bool) {
	if cfg == nil || strings.TrimSpace(cfg.DefaultTo) != "" {
		return "", false
	}
	if !st.Authenticated || !st.HasGmailScope {
		return "", false
	}
	email := strings.TrimSpace(st.Email)
	if email == "" {
		return "", false
	}
	return email, true
}

// AutoDefaultRecipientIfMissing sets DefaultTo to the authenticated sending
// address the first time gws is connected and no recipient has been
// configured yet, so a fresh connection is usable without an extra manual
// step. Reports whether it did.
func (g *GmailService) AutoDefaultRecipientIfMissing(ctx context.Context) (bool, error) {
	cfg := g.GetConfig()
	email, ok := autoDefaultGmailRecipient(cfg, g.EffectiveAuthStatus(ctx))
	if !ok {
		return false, nil
	}
	cfg.DefaultTo = email
	if err := g.SaveConfig(ctx, cfg); err != nil {
		return false, err
	}
	return true, nil
}
