package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	st.Scopes = append([]string(nil), gmailOAuthScopes...)
	st.Email = email
	return st
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
