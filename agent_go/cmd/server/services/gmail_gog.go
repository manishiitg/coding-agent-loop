package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gog (github.com/openclaw/gogcli) backend, selected via GmailConfig.UseGogBackend.
//
// Unlike gws, gog has no notion of "the account this process happens to be
// pointed at" — every invocation names its account/client explicitly
// (--access-token, or --account/--client), so there is no directory-per-
// connection isolation to manage: every call shares one GOG_HOME.
//
// gws stays the default (UseGogBackend unset/false) — shipping this file
// changes no deployment's behavior until an operator explicitly opts in,
// verifies a real send locally, and flips the flag.

// gogHomeDir is the single shared root gog stores every account and named
// client under, always passed explicitly via --home rather than relying on
// ambient GOG_HOME — so an operator's own shell environment cannot silently
// repoint the server at a different store than the one gmail_oauth_clients.go
// manages.
func gogHomeDir() string {
	if v := strings.TrimSpace(os.Getenv("GOG_HOME")); v != "" {
		return v
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "agentworks", "gog")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "agentworks-gog")
	}
	return filepath.Join(home, ".config", "agentworks", "gog")
}

// gogArgsForAuth picks how one gog invocation authenticates, from the same
// per-connection knobs gmailConnectionConfig already resolves:
//   - cfg.Token set (server-managed OAuth, gmail_oauth.go) -> --access-token.
//     Takes priority, mirroring GOOGLE_WORKSPACE_CLI_TOKEN's precedence in
//     gmailChildEnv: a connection holding a live access token needs no
//     stored gog account at all.
//   - else cfg.gogAccountEmail/gogClientName set (a connection migrated via
//     GmailService.ImportLegacyOAuthClient or GmailService's
//     MigrateLegacyConnectionToGog, Phase 3) -> --account/--client, letting
//     gog manage its own refresh.
//   - else: nothing to authenticate with. Reported as an error rather than
//     falling back silently — sending under the wrong (or no) identity is
//     worse than failing loudly.
func gogArgsForAuth(cfg *GmailConfig) ([]string, error) {
	if cfg != nil && strings.TrimSpace(cfg.Token) != "" {
		return []string{"--access-token", cfg.Token}, nil
	}
	if cfg != nil && strings.TrimSpace(cfg.gogAccountEmail) != "" && strings.TrimSpace(cfg.gogClientName) != "" {
		return []string{"--account", cfg.gogAccountEmail, "--client", cfg.gogClientName}, nil
	}
	return nil, fmt.Errorf("gog: this connection has no access token and is not migrated to a named account/client — reconnect it, or run the legacy-client import")
}

// gogBaseArgs prefixes every gog invocation with the shared home directory.
func gogBaseArgs(authArgs []string) []string {
	args := make([]string, 0, len(authArgs)+2)
	args = append(args, "--home", gogHomeDir())
	args = append(args, authArgs...)
	return args
}

// computeAuthStatusGog is the gog counterpart of computeAuthStatus. It
// deliberately does not call `gog auth status`: that command reports on a
// stored account, but a server-managed connection (--access-token) has no
// stored account to report on at all. Instead it calls the same
// gmail.users.getProfile the gws path already used for identity discovery —
// gmail.readonly is already part of gmailOAuthScopes, so success at that call
// is both the liveness check and the identity lookup in one round trip.
func (g *GmailService) computeAuthStatusGog(ctx context.Context, gogPath string, cfg *GmailConfig) GmailAuthStatus {
	if gogPath == "" {
		gogPath = "gog"
	}

	st := GmailAuthStatus{}
	if _, err := exec.LookPath(gogPath); err != nil {
		st.Detail = "gog binary not found on PATH — install gogcli (brew install openclaw/tap/gogcli)"
		return st
	}
	st.GwsInstalled = true // field name predates gog; means "the active CLI backend is installed"

	authArgs, err := gogArgsForAuth(cfg)
	if err != nil {
		st.Detail = err.Error()
		return st
	}

	email, err := gogFetchGmailProfileEmail(ctx, gogPath, authArgs)
	if err != nil {
		st.Detail = "not authenticated — reconnect this account"
		return st
	}
	st.Authenticated = true
	st.HasGmailScope = true
	st.Email = email

	// Report what was actually granted, not what we last asked for — an
	// operator (or the agent, via `gog auth add`) may have widened access
	// since this connection was created, and the UI's Scopes display is
	// only honest if it reflects that.
	//
	// Only possible for the access-token path: with an --account/--client
	// connection there is no raw token in hand to introspect here (gog
	// manages refresh internally and exposes no documented "print token"
	// command), so this falls back to the base requested set, which may
	// under-report a scope granted directly through gog outside this app.
	if cfg != nil && strings.TrimSpace(cfg.Token) != "" {
		if scopes, scopeErr := googleTokenGrantedScopes(ctx, cfg.Token); scopeErr == nil && len(scopes) > 0 {
			st.Scopes = scopes
		} else {
			st.Scopes = append([]string(nil), gmailOAuthScopes...)
		}
	} else {
		st.Scopes = append([]string(nil), gmailOAuthScopes...)
	}
	return st
}

// googleTokenInfoURL is a var, not a constant, so tests can point it at a
// local httptest server instead of making a real network call to Google for
// every status check exercised in the suite.
var googleTokenInfoURL = "https://oauth2.googleapis.com/tokeninfo"

// googleTokenGrantedScopes asks Google's tokeninfo endpoint what scopes an
// access token actually carries — the only authoritative source, since a
// project's OAuth consent screen can silently drop a requested-but-
// unregistered scope (see the setup guide's Data Access gotcha).
// googleTokenInfoClient bounds the tokeninfo call so a network partition
// cannot hang a status check indefinitely — computeAuthStatusGog runs on
// both a request-serving path and a detached background-refresh goroutine,
// and neither should be able to stall on this.
var googleTokenInfoClient = &http.Client{Timeout: 5 * time.Second}

func googleTokenGrantedScopes(ctx context.Context, accessToken string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		googleTokenInfoURL+"?access_token="+url.QueryEscape(accessToken), nil)
	if err != nil {
		return nil, err
	}
	resp, err := googleTokenInfoClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tokeninfo: unexpected status %d", resp.StatusCode)
	}
	var body struct {
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	scopes := strings.Fields(body.Scope)
	if len(scopes) == 0 {
		return nil, fmt.Errorf("tokeninfo: empty scope")
	}
	return scopes, nil
}

// gogFetchGmailProfileEmail calls gmail.users.getProfile through gog's
// generic Discovery API passthrough (`gog api call`), the direct equivalent
// of the `gws gmail users getProfile` call fetchGmailAccountEmail makes.
func gogFetchGmailProfileEmail(ctx context.Context, gogPath string, authArgs []string) (string, error) {
	args := gogBaseArgs(authArgs)
	args = append(args, "api", "call", "gmail", "v1", "gmail.users.getProfile",
		"--params", `{"userId":"me"}`, "--json")
	cmd := exec.CommandContext(ctx, gogPath, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var profile struct {
		EmailAddress string `json:"emailAddress"`
	}
	if err := json.Unmarshal(out, &profile); err != nil {
		return "", err
	}
	email := strings.TrimSpace(profile.EmailAddress)
	if email == "" {
		return "", fmt.Errorf("gog: empty emailAddress in getProfile response")
	}
	return email, nil
}

// sendGog is the gog counterpart of GmailService.send (`gog gmail send`,
// matching gws's `gmail +send`).
func (g *GmailService) sendGog(ctx context.Context, gogPath string, cfg *GmailConfig, to, subject, body string) (string, error) {
	if gogPath == "" {
		gogPath = "gog"
	}
	authArgs, err := gogArgsForAuth(cfg)
	if err != nil {
		return "", err
	}
	args := gogBaseArgs(authArgs)
	args = append(args, "gmail", "send",
		"--to", to,
		"--subject", mime.QEncoding.Encode("UTF-8", subject),
		"--body", body,
		"--json")
	cmd := exec.CommandContext(ctx, gogPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gog gmail send failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseGwsMessageID(stdout.Bytes()), nil
}

// sendRawGog is the gog counterpart of GmailService.sendRaw. gog accepts an
// exact RFC822 message via --raw-file (stdin with "-"), so the existing
// buildGmailMIME output feeds it unchanged — no re-encoding for the switch
// from gws's raw users.messages.send call.
func (g *GmailService) sendRawGog(ctx context.Context, gogPath string, cfg *GmailConfig, to string, cc []string, subject, body, htmlBody string, attachments []string) (string, error) {
	if gogPath == "" {
		gogPath = "gog"
	}
	mimeBytes, err := buildGmailMIME(to, cc, subject, body, htmlBody, attachments)
	if err != nil {
		return "", fmt.Errorf("build email: %w", err)
	}
	authArgs, err := gogArgsForAuth(cfg)
	if err != nil {
		return "", err
	}
	args := gogBaseArgs(authArgs)
	args = append(args, "gmail", "send", "--raw-file", "-", "--json")
	cmd := exec.CommandContext(ctx, gogPath, args...)
	cmd.Stdin = bytes.NewReader(mimeBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String() + " " + stdout.String())
		return "", fmt.Errorf("gog gmail send --raw-file failed: %w: %s", err, detail)
	}
	return parseGwsMessageID(stdout.Bytes()), nil
}

// ImportRefreshTokenIntoGog registers a refresh token gog can use directly
// (via --account/--client) under the given named client, so an agent's own
// `gog <service> ...` shell commands work independently of this server's
// per-call --access-token path — the whole point of teaching the agent to
// drive gog itself for services beyond Gmail send.
//
// Called from the OAuth callback right after this server stores its own
// copy of the credential (gmail_oauth.go). Best-effort and non-fatal: gog
// not being installed, or any other failure here, must never break a sign-in
// that already succeeded and is already usable through the normal send path.
func ImportRefreshTokenIntoGog(ctx context.Context, email, clientName, refreshToken string) error {
	email = strings.TrimSpace(email)
	clientName = strings.TrimSpace(clientName)
	if email == "" || clientName == "" || strings.TrimSpace(refreshToken) == "" {
		return fmt.Errorf("gog import: email, client name, and refresh token are all required")
	}
	const gogPath = "gog"
	if _, err := exec.LookPath(gogPath); err != nil {
		return fmt.Errorf("gog binary not found on PATH: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "gmail-gog-import-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	refreshPath := filepath.Join(tmpDir, "refresh_token")
	if err := os.WriteFile(refreshPath, []byte(refreshToken), 0o600); err != nil {
		return fmt.Errorf("write refresh token: %w", err)
	}

	args := gogBaseArgs(nil)
	args = append(args, "auth", "import",
		"--email", email,
		"--client", clientName,
		"--refresh-token-file", refreshPath,
		"--no-input")
	cmd := exec.CommandContext(ctx, gogPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gog auth import: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
