package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Server-driven Gmail OAuth.
//
// The alternative — authenticating each account with `gws auth login` on the
// host — means a terminal step per mailbox, which is not something a user of a
// web UI can do. So the server runs the OAuth flow itself and never asks gws to
// authenticate at all.
//
// It works because gws accepts a pre-obtained access token via
// GOOGLE_WORKSPACE_CLI_TOKEN, which takes priority over every other credential
// source. We hold the refresh token, mint short-lived access tokens, and hand
// one to each gws invocation. Nothing writes to gws's own credential store, so
// there is no dependency on its on-disk format.
//
// Refresh tokens are secrets and live OUTSIDE the workspace: gmail-config.json
// is workspace content, readable by anything that can read the workspace, and a
// refresh token there would be a standing grant to send as that person.

// gmailOAuthScopes is the minimum that supports the send path plus identity
// discovery. Deliberately narrower than `gws auth login -s gmail`, which
// requests sixteen scopes including cloud-platform.
var gmailOAuthScopes = []string{
	"https://www.googleapis.com/auth/gmail.send",
	"https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/userinfo.email",
}

// gmailOAuthTokenDir is where refresh tokens live — host-local, never the
// workspace.
func gmailOAuthTokenDir() string {
	if v := strings.TrimSpace(os.Getenv("GMAIL_OAUTH_TOKEN_DIR")); v != "" {
		return v
	}
	// Same rule as the MCP connector tokens and the DCR client cache: a host
	// whose ~/.config is not writable by the service user (RTS: root-owned)
	// points XDG_CONFIG_HOME at a writable tree, and this must follow it.
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "agentworks", "gmail-oauth")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "agentworks-gmail-oauth")
	}
	return filepath.Join(home, ".config", "agentworks", "gmail-oauth")
}

func gmailOAuthTokenPath(connectionID string) string {
	return filepath.Join(gmailOAuthTokenDir(), connectionID+".json")
}

// gmailOAuthConfig builds the OAuth client for one connection.
//
// clientName, when set, resolves through the named OAuth client registry
// (gmail_oauth_clients.go) — every connection created after that registry
// existed carries one. An empty clientName falls back to the pre-registry
// shared client_secret.json / env vars, so a connection created before this
// migration keeps working unattended until GmailService.ImportLegacyOAuthClient
// backfills its name.
func gmailOAuthConfig(redirectURL, clientName string) (*oauth2.Config, error) {
	clientName = strings.TrimSpace(clientName)

	var clientID, clientSecret string
	var err error
	if clientName != "" {
		clientID, clientSecret, err = GetOAuthClientSecret(clientName)
		if err != nil {
			return nil, err
		}
	} else {
		clientID = strings.TrimSpace(os.Getenv("GOOGLE_WORKSPACE_CLI_CLIENT_ID"))
		clientSecret = strings.TrimSpace(os.Getenv("GOOGLE_WORKSPACE_CLI_CLIENT_SECRET"))
		if clientID == "" || clientSecret == "" {
			clientID, clientSecret, err = readGmailClientSecretFile()
			if err != nil {
				return nil, err
			}
		}
	}
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("no OAuth client configured: add a named OAuth client, or set GOOGLE_WORKSPACE_CLI_CLIENT_ID/_SECRET or create ~/.config/gws/client_secret.json")
	}
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       gmailOAuthScopes,
		Endpoint:     google.Endpoint,
	}, nil
}

// readGmailClientSecretFile parses a Desktop-app client_secret.json. Its
// "installed" object carries the client id and secret; project_id is
// deliberately ignored, because sending it as a quota project requires every
// consenting account to hold serviceusage permission on that project.
func readGmailClientSecretFile() (string, string, error) {
	path := strings.TrimSpace(os.Getenv("GMAIL_CLIENT_SECRET_FILE"))
	if path == "" {
		// Same rule as the MCP connector tokens, the DCR client cache, and the
		// Gmail refresh-token directory: a host whose ~/.config is not
		// writable by the service user (RTS: root-owned) points
		// XDG_CONFIG_HOME at a writable tree instead.
		if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
			path = filepath.Join(xdg, "gws", "client_secret.json")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", "", fmt.Errorf("locate home directory: %w", err)
			}
			path = filepath.Join(home, ".config", "gws", "client_secret.json")
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", path, err)
	}
	var parsed map[string]struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", fmt.Errorf("parse %s: %w", path, err)
	}
	for _, key := range []string{"installed", "web"} {
		if entry, ok := parsed[key]; ok {
			return entry.ClientID, entry.ClientSecret, nil
		}
	}
	return "", "", fmt.Errorf("%s has neither an \"installed\" nor a \"web\" client", path)
}

// storeGmailOAuthToken persists a refresh token for one connection, 0600.
func storeGmailOAuthToken(connectionID string, token *oauth2.Token) error {
	if strings.TrimSpace(connectionID) == "" || token == nil {
		return fmt.Errorf("gmail oauth: connection id and token are required")
	}
	if err := os.MkdirAll(gmailOAuthTokenDir(), 0o700); err != nil {
		return fmt.Errorf("gmail oauth: create token dir: %w", err)
	}
	blob, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("gmail oauth: marshal token: %w", err)
	}
	return os.WriteFile(gmailOAuthTokenPath(connectionID), blob, 0o600)
}

func loadGmailOAuthToken(connectionID string) (*oauth2.Token, bool) {
	raw, err := os.ReadFile(gmailOAuthTokenPath(connectionID))
	if err != nil {
		return nil, false
	}
	var token oauth2.Token
	if err := json.Unmarshal(raw, &token); err != nil {
		return nil, false
	}
	if strings.TrimSpace(token.RefreshToken) == "" && strings.TrimSpace(token.AccessToken) == "" {
		return nil, false
	}
	return &token, true
}

// deleteGmailOAuthToken removes a connection's stored credential. Called when
// the connection is deleted, so revoking access does not leave a live token.
func deleteGmailOAuthToken(connectionID string) {
	_ = os.Remove(gmailOAuthTokenPath(connectionID))
}

// HasServerManagedOAuth reports whether this connection authenticates through
// the server's own flow rather than `gws auth login`.
func HasServerManagedOAuth(connectionID string) bool {
	_, ok := loadGmailOAuthToken(strings.TrimSpace(connectionID))
	return ok
}

// gmailAccessTokenCache avoids a token endpoint round trip on every send. The
// oauth2 library refreshes automatically, but only if we reuse the TokenSource.
var (
	gmailTokenSourceMu sync.Mutex
	gmailTokenSources  = map[string]oauth2.TokenSource{}
)

// accessTokenForConnection returns a currently-valid access token, refreshing
// through the stored refresh token when needed.
//
// A refreshed token is written back, because Google may rotate the refresh
// token, and losing that rotation would silently break the connection later.
func accessTokenForConnection(ctx context.Context, connectionID, clientName string) (string, error) {
	connectionID = strings.TrimSpace(connectionID)
	stored, ok := loadGmailOAuthToken(connectionID)
	if !ok {
		return "", fmt.Errorf("gmail connection %q has no stored OAuth credential", connectionID)
	}

	gmailTokenSourceMu.Lock()
	source, cached := gmailTokenSources[connectionID]
	if !cached {
		cfg, err := gmailOAuthConfig("", clientName)
		if err != nil {
			gmailTokenSourceMu.Unlock()
			return "", err
		}
		source = cfg.TokenSource(context.Background(), stored)
		gmailTokenSources[connectionID] = source
	}
	gmailTokenSourceMu.Unlock()

	token, err := source.Token()
	if err != nil {
		// A dead refresh token is a reconnect, not a transient error — say so.
		return "", fmt.Errorf("gmail connection %q needs to be reconnected: %w", connectionID, err)
	}
	if token.RefreshToken != "" && token.RefreshToken != stored.RefreshToken {
		_ = storeGmailOAuthToken(connectionID, token)
	}
	return token.AccessToken, nil
}

// GmailOAuthPending tracks in-flight authorization attempts, keyed by the OAuth
// state parameter. Held in memory only: an interrupted flow should expire
// rather than persist, and the window is seconds to minutes.
type GmailOAuthPending struct {
	ConnectionID string
	// ClientName is captured at the start of the flow and reused for the
	// token exchange — the exchange must run against the exact same OAuth
	// client the consent URL was built for, or Google rejects it.
	ClientName  string
	RedirectURL string
	CreatedAt   time.Time
}

var (
	gmailOAuthPendingMu sync.Mutex
	gmailOAuthPending   = map[string]GmailOAuthPending{}
)

const gmailOAuthPendingTTL = 15 * time.Minute

// BeginGmailOAuth returns the URL the user's browser must visit, and the opaque
// state tying the eventual callback back to this connection.
//
// state is random and server-held, so a callback cannot be forged to attach
// someone else's Google account to a connection.
func BeginGmailOAuth(connectionID, clientName, redirectURL string) (string, error) {
	cfg, err := gmailOAuthConfig(redirectURL, clientName)
	if err != nil {
		return "", err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("gmail oauth: generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(buf)

	gmailOAuthPendingMu.Lock()
	for key, pending := range gmailOAuthPending {
		if time.Since(pending.CreatedAt) > gmailOAuthPendingTTL {
			delete(gmailOAuthPending, key)
		}
	}
	gmailOAuthPending[state] = GmailOAuthPending{
		ConnectionID: connectionID,
		ClientName:   strings.TrimSpace(clientName),
		RedirectURL:  redirectURL,
		CreatedAt:    time.Now(),
	}
	gmailOAuthPendingMu.Unlock()

	// AccessTypeOffline is what yields a refresh token at all; ApprovalForce
	// makes Google re-issue one even if the user has consented before, which
	// otherwise returns an access token only and leaves the connection unable
	// to send once it expires.
	return cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
		oauth2.SetAuthURLParam("prompt", "select_account consent"),
	), nil
}

// CompleteGmailOAuth exchanges the callback code and stores the credential.
// Returns the connection the flow belonged to.
func CompleteGmailOAuth(ctx context.Context, state, code string) (string, error) {
	gmailOAuthPendingMu.Lock()
	pending, ok := gmailOAuthPending[state]
	delete(gmailOAuthPending, state)
	gmailOAuthPendingMu.Unlock()

	if !ok {
		return "", fmt.Errorf("this sign-in link has expired or was already used — start again from the Gmail settings")
	}
	if time.Since(pending.CreatedAt) > gmailOAuthPendingTTL {
		return "", fmt.Errorf("this sign-in took too long and expired — start again from the Gmail settings")
	}

	cfg, err := gmailOAuthConfig(pending.RedirectURL, pending.ClientName)
	if err != nil {
		return "", err
	}
	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		return "", fmt.Errorf("gmail oauth: exchange authorization code: %w", err)
	}
	if strings.TrimSpace(token.RefreshToken) == "" {
		return "", fmt.Errorf("Google did not return a refresh token — revoke this app's access in your Google account and try again")
	}
	if err := storeGmailOAuthToken(pending.ConnectionID, token); err != nil {
		return "", err
	}
	// Drop any cached source so the next send uses the new credential rather
	// than a TokenSource still holding the previous account's refresh token.
	gmailTokenSourceMu.Lock()
	delete(gmailTokenSources, pending.ConnectionID)
	gmailTokenSourceMu.Unlock()

	return pending.ConnectionID, nil
}
