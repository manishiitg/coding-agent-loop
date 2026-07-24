package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Gmail is a single-user notification channel backed by the Google Workspace
// CLI (`gws`, https://github.com/googleworkspace/cli). Unlike Slack/WhatsApp
// it shells out to the CLI rather than using a Go SDK — `gws` owns the OAuth
// dance and stores encrypted credentials, so the server only needs an
// authenticated binary on the host.
//
// "Single user" means one shared Gmail account sends every notification. The
// account is whichever identity `gws` is authenticated as (via `gws auth
// login`, a service-account file, or a token env var). Per-request and
// per-user *recipient* routing still works through NotificationDestination /
// NotificationPreference, but all mail is sent FROM the one configured account.
//
// This connector is OUTBOUND-ONLY — it sends mail but does not read replies.
// Responses to feedback requests are collected through another channel.
//
// Config lives at <workspace-docs>/config/gmail-config.json and is loaded into
// memory on startup, mirroring the Slack config pattern.

func gmailConfigFilePath() string {
	return "config/gmail-config.json"
}

// GmailConfig is the on-disk configuration for the Gmail channel.
type GmailConfig struct {
	Enabled bool `json:"enabled"`

	// DefaultTo is the workspace-wide fallback recipient (the equivalent of
	// Slack's default channel). Used when a notification carries no explicit
	// destination hint and the target user has no Gmail preference set.
	DefaultTo string `json:"default_to"`

	// BlockedRecipients is a denylist for outbound Gmail recipients. Explicit
	// destination hints, per-user Gmail preferences, To overrides, and CC
	// recipients matching this list are rejected instead of being sent.
	BlockedRecipients []string `json:"blocked_recipients,omitempty"`

	// GwsPath is the path to the gws binary. Empty means "gws" on $PATH.
	GwsPath string `json:"gws_path,omitempty"`

	// Auth knobs (all optional). When set they are exported into the gws
	// child process environment so the server can pin a specific account
	// without relying on the invoking user's ~/.config/gws:
	//   ConfigHome      -> XDG_CONFIG_HOME (gws reads <ConfigHome>/gws)
	//   CredentialsFile -> GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE (service acct)
	//   Token           -> GOOGLE_WORKSPACE_CLI_TOKEN (pre-obtained token)
	ConfigHome      string `json:"config_home,omitempty"`
	CredentialsFile string `json:"credentials_file,omitempty"`
	Token           string `json:"token,omitempty"`
}

// GmailService implements NotificationConnector (and UserNotificationConnector)
// by invoking `gws gmail +send`.
type GmailService struct {
	mu        sync.RWMutex
	config    *GmailConfig
	enabled   bool
	defaultTo string
	gwsPath   string
}

var (
	globalGmailService *GmailService
	gmailServiceMux    sync.RWMutex
)

// SetGmailService sets the global Gmail service instance.
func SetGmailService(service *GmailService) {
	gmailServiceMux.Lock()
	defer gmailServiceMux.Unlock()
	globalGmailService = service
}

// GetGmailService returns the global Gmail service instance (may be nil).
func GetGmailService() *GmailService {
	gmailServiceMux.RLock()
	defer gmailServiceMux.RUnlock()
	return globalGmailService
}

// InitGmailService initializes the Gmail service from the filesystem-backed
// config file. Called on server startup.
func InitGmailService() (*GmailService, error) {
	service := &GmailService{}
	if err := service.ReloadConfig(context.Background()); err != nil {
		SetGmailService(service)
		return service, err
	}
	SetGmailService(service)
	return service, nil
}

// loadGmailConfigFromDisk reads gmail-config.json from the workspace.
func loadGmailConfigFromDisk() (*GmailConfig, error) {
	ctx := context.Background()
	data, exists, err := readWorkspaceFile(ctx, workspaceAPIURL(), gmailConfigFilePath())
	if err != nil {
		return nil, err
	}
	if !exists {
		return &GmailConfig{Enabled: false}, nil
	}
	var cfg GmailConfig
	if err := json.Unmarshal([]byte(data), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse gmail-config.json: %w", err)
	}
	return &cfg, nil
}

// ReloadConfig reloads configuration from disk and recomputes enablement.
func (g *GmailService) ReloadConfig(ctx context.Context) error {
	cfg, err := loadGmailConfigFromDisk()
	if err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	cfg = normalizeGmailConfig(cfg)
	g.config = cfg

	gwsPath := strings.TrimSpace(cfg.GwsPath)
	if gwsPath == "" {
		gwsPath = "gws"
	}

	// Enabled requires: the flag on, a default recipient, and a resolvable
	// gws binary. Without a binary every send would fail, so we report
	// disabled rather than register a dead connector.
	binaryOK := true
	if _, lookErr := exec.LookPath(gwsPath); lookErr != nil {
		binaryOK = false
	}

	g.gwsPath = gwsPath
	g.defaultTo = strings.TrimSpace(cfg.DefaultTo)
	g.enabled = cfg.Enabled && g.defaultTo != "" && binaryOK

	if cfg.Enabled && !g.enabled {
		log.Printf("[GMAIL] Service disabled: enabled=%v, hasDefaultTo=%v, gwsFound=%v",
			cfg.Enabled, g.defaultTo != "", binaryOK)
	} else if g.enabled {
		log.Printf("[GMAIL] Service enabled: default_to=%s, blocked_recipients=%d, gws=%s", g.defaultTo, len(cfg.BlockedRecipients), g.gwsPath)
	}
	return nil
}

// GetConfig returns a copy of the current on-disk config (never nil).
func (g *GmailService) GetConfig() *GmailConfig {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.config == nil {
		return &GmailConfig{}
	}
	c := *g.config
	c.BlockedRecipients = append([]string(nil), g.config.BlockedRecipients...)
	return &c
}

// SaveConfig persists the config to the workspace and reloads the service so
// enablement takes effect immediately. This is what the UI's enable/disable
// toggle calls — the user never touches gmail-config.json directly.
func (g *GmailService) SaveConfig(ctx context.Context, cfg *GmailConfig) error {
	if cfg == nil {
		cfg = &GmailConfig{}
	}
	cfg = normalizeGmailConfig(cfg)
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal gmail config: %w", err)
	}
	if err := writeWorkspaceFile(ctx, workspaceAPIURL(), gmailConfigFilePath(), string(out)); err != nil {
		return fmt.Errorf("write gmail config: %w", err)
	}
	return g.ReloadConfig(ctx)
}

// GmailAuthStatus reports what the UI needs to show connection state without
// the user inspecting any files: is the gws binary present, is it
// authenticated, and does the account hold a Gmail send scope.
type GmailAuthStatus struct {
	GwsInstalled  bool     `json:"gws_installed"`
	Authenticated bool     `json:"authenticated"`
	HasGmailScope bool     `json:"has_gmail_scope"`
	Scopes        []string `json:"scopes,omitempty"`
	Detail        string   `json:"detail,omitempty"`
}

// AuthStatus shells out to `gws auth status` and interprets the result. It is
// best-effort: any failure surfaces as "not authenticated" with a hint rather
// than an error, so the UI can render a "Connect Gmail" prompt.
func (g *GmailService) AuthStatus(ctx context.Context) GmailAuthStatus {
	g.mu.RLock()
	gwsPath := g.gwsPath
	cfg := g.config
	g.mu.RUnlock()
	if gwsPath == "" {
		gwsPath = "gws"
	}

	st := GmailAuthStatus{}
	if _, err := exec.LookPath(gwsPath); err != nil {
		st.Detail = "gws binary not found on PATH — install the Google Workspace CLI"
		return st
	}
	st.GwsInstalled = true

	cmd := exec.CommandContext(ctx, gwsPath, "auth", "status")
	cmd.Env = gmailChildEnv(cfg)
	out, err := cmd.Output()
	if err != nil {
		st.Detail = "not authenticated — run `gws auth login` on the host"
		return st
	}

	var raw struct {
		EncryptionValid          bool     `json:"encryption_valid"`
		HasRefreshToken          bool     `json:"has_refresh_token"`
		EncryptedCredentialsHave bool     `json:"encrypted_credentials_exists"`
		Scopes                   []string `json:"scopes"`
	}
	if jsonErr := json.Unmarshal(out, &raw); jsonErr != nil {
		st.Detail = "could not parse `gws auth status` output"
		return st
	}
	st.Authenticated = raw.EncryptedCredentialsHave && raw.EncryptionValid
	st.Scopes = raw.Scopes
	for _, s := range raw.Scopes {
		if strings.Contains(s, "gmail.send") || strings.Contains(s, "gmail.modify") || strings.Contains(s, "mail.google.com") {
			st.HasGmailScope = true
			break
		}
	}
	if st.Authenticated && !st.HasGmailScope {
		st.Detail = "authenticated, but the account is missing a Gmail send scope — re-run `gws auth login -s gmail`"
	}
	return st
}

// Name returns the connector name.
func (g *GmailService) Name() string { return "gmail" }

// IsEnabled reports whether Gmail notifications can be sent.
func (g *GmailService) IsEnabled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.enabled
}

// pickRecipient resolves where this Gmail notification should land.
// Precedence:
//  1. dest.Gmail.Email (explicit per-request hint)
//  2. per-user preference (looked up via dest.UserID)
//  3. workspace-wide default (g.defaultTo)
//
// Returns "" only when Gmail is disabled or has no default and no hint — the
// caller treats that as "skip silently".
func (g *GmailService) pickRecipient(dest *NotificationDestination) (string, error) {
	candidate := ""
	if dest != nil && dest.Gmail != nil && strings.TrimSpace(dest.Gmail.Email) != "" {
		candidate = strings.TrimSpace(dest.Gmail.Email)
	} else if dest != nil && dest.UserID != "" {
		if pref := getNotificationPreferences(dest.UserID); pref != nil && pref.GmailEmail != "" && !pref.GmailDisabled {
			candidate = strings.TrimSpace(pref.GmailEmail)
		}
	}
	if candidate == "" {
		g.mu.RLock()
		candidate = g.defaultTo
		g.mu.RUnlock()
	}
	if candidate == "" {
		return "", nil
	}
	recipients, err := g.validateRecipients([]string{candidate}, "to", destBlockedRecipients(dest)...)
	if err != nil {
		return "", err
	}
	if len(recipients) == 0 {
		return "", nil
	}
	return strings.Join(recipients, ", "), nil
}

func (g *GmailService) validateCCRecipients(cc []string, extraBlocked ...string) ([]string, error) {
	return g.validateRecipients(cc, "cc", extraBlocked...)
}

// validateRecipients rejects any recipient in the account-wide blocked list OR
// in extraBlocked (a per-notification/per-workflow denylist). The two lists are
// unioned — extraBlocked can only add to the block set, never remove from it.
func (g *GmailService) validateRecipients(recipients []string, label string, extraBlocked ...string) ([]string, error) {
	blocked := g.blockedRecipients()
	if len(extraBlocked) > 0 {
		blocked = append(append([]string(nil), blocked...), extraBlocked...)
	}
	return validateRecipientsAgainstList(recipients, label, blocked)
}

// destBlockedRecipients extracts the per-notification Gmail denylist carried on
// the destination hint (nil-safe), used to augment the account-wide blocked list.
func destBlockedRecipients(dest *NotificationDestination) []string {
	if dest != nil && dest.Gmail != nil {
		return dest.Gmail.BlockedRecipients
	}
	return nil
}

func validateRecipientsAgainstList(recipients []string, label string, blockedRecipients []string) ([]string, error) {
	recipients = normalizeEmailList(recipients)
	blocked := map[string]bool{}
	for _, recipient := range normalizeEmailList(blockedRecipients) {
		blocked[recipient] = true
	}
	for _, recipient := range recipients {
		if blocked[recipient] {
			return nil, fmt.Errorf("gmail %s recipient %q is in the blocked recipients list", label, recipient)
		}
	}
	return recipients, nil
}

// SendNotification implements NotificationConnector. It renders the feedback
// request as a plain email. Gmail is an OUTBOUND-ONLY connector: it notifies
// the recipient that input is needed, but the response itself is collected
// through another channel (Slack bot, web UI). Any options are listed in the
// body for context only — there is no email reply path.
func (g *GmailService) SendNotification(ctx context.Context, uniqueID string, message string, contextMsg string, buttonOptions *ButtonOptions, dest *NotificationDestination) (string, error) {
	if !g.IsEnabled() {
		return "", nil
	}
	to, err := g.pickRecipient(dest)
	if err != nil {
		return "", err
	}
	if to == "" {
		return "", nil
	}

	subject := gmailSubject(message)
	body := strings.TrimSpace(message)
	if c := strings.TrimSpace(contextMsg); c != "" {
		body += "\n\n" + c
	}
	if opts := renderGmailButtonOptions(buttonOptions); opts != "" {
		body += "\n\n" + opts
	}

	// Typed Gmail content (if provided) overrides the derived subject/body and
	// can carry attachments.
	var attachments []string
	var cc []string
	var htmlBody string
	if gc := gmailContentFrom(dest); gc != nil {
		if s := strings.TrimSpace(gc.Subject); s != "" {
			subject = s
		}
		cc = gc.CC
		if b := strings.TrimSpace(gc.Body); b != "" {
			body = b
		}
		htmlBody = gc.HTMLBody
		attachments = gc.Attachments
	}
	cc, err = g.validateCCRecipients(cc, destBlockedRecipients(dest)...)
	if err != nil {
		return "", err
	}

	return g.deliver(ctx, to, cc, subject, body, htmlBody, attachments)
}

// SendUserNotification sends a non-blocking informational email.
func (g *GmailService) SendUserNotification(ctx context.Context, message string, contextMsg string, dest *NotificationDestination) (string, error) {
	if !g.IsEnabled() {
		return "", nil
	}
	to, err := g.pickRecipient(dest)
	if err != nil {
		return "", err
	}
	if to == "" {
		return "", nil
	}
	subject := gmailSubject(message)
	body := strings.TrimSpace(message)
	if c := strings.TrimSpace(contextMsg); c != "" {
		body += "\n\n" + c
	}
	var attachments []string
	var cc []string
	var htmlBody string
	if gc := gmailContentFrom(dest); gc != nil {
		if s := strings.TrimSpace(gc.Subject); s != "" {
			subject = s
		}
		cc = gc.CC
		if b := strings.TrimSpace(gc.Body); b != "" {
			body = b
		}
		htmlBody = gc.HTMLBody
		attachments = gc.Attachments
	}
	if strings.TrimSpace(body) == "" && strings.TrimSpace(htmlBody) == "" && len(attachments) == 0 {
		return "", nil
	}
	cc, err = g.validateCCRecipients(cc, destBlockedRecipients(dest)...)
	if err != nil {
		return "", err
	}
	return g.deliver(ctx, to, cc, subject, body, htmlBody, attachments)
}

// send shells out to `gws gmail +send` and returns the sent message ID.
func (g *GmailService) send(ctx context.Context, to, subject, body string) (string, error) {
	g.mu.RLock()
	gwsPath := g.gwsPath
	cfg := g.config
	g.mu.RUnlock()
	if strings.TrimSpace(gwsPath) == "" {
		gwsPath = "gws"
	}

	// RFC 2047-encode the subject so non-ASCII (em dash, emoji, accents) renders
	// correctly. gws passes --subject verbatim into the header and does NOT encode
	// it, so a raw "Trading workflow — test" shows a broken character in the client.
	// mime.QEncoding.Encode is a no-op for pure-ASCII subjects. (The attachment path,
	// buildGmailMIME, already encodes — this brings the plain path in line.)
	args := []string{"gmail", "+send", "--to", to, "--subject", mime.QEncoding.Encode("UTF-8", subject), "--body", body, "--format", "json"}
	cmd := exec.CommandContext(ctx, gwsPath, args...)
	cmd.Env = gmailChildEnv(cfg)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gws gmail +send failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseGwsMessageID(stdout.Bytes()), nil
}

// maxGmailAttachmentBytes caps the total attachment payload per message to keep
// well under Gmail's raw-send limit and avoid pathological memory use.
const maxGmailAttachmentBytes = 20 * 1024 * 1024 // 20 MB

// deliver routes to the simple `+send` helper for plain text, or to the raw
// MIME path when attachments are present (gws `+send` can't attach files).
func (g *GmailService) deliver(ctx context.Context, to string, cc []string, subject, body, htmlBody string, attachments []string) (string, error) {
	// The plain gws --body path can't carry HTML or attachments, so route through
	// the raw-MIME path whenever either is present.
	if htmlBody == "" && len(attachments) == 0 && len(cc) == 0 && !strings.Contains(to, ",") {
		return g.send(ctx, to, subject, body)
	}
	return g.sendRaw(ctx, to, cc, subject, body, htmlBody, attachments)
}

// sendRaw builds an RFC 2822 MIME message (body + attachments) and posts it via
// `gws gmail users messages send --json '{"raw": <base64url>}'`.
func (g *GmailService) sendRaw(ctx context.Context, to string, cc []string, subject, body, htmlBody string, attachments []string) (string, error) {
	g.mu.RLock()
	gwsPath := g.gwsPath
	cfg := g.config
	g.mu.RUnlock()
	if strings.TrimSpace(gwsPath) == "" {
		gwsPath = "gws"
	}

	mimeBytes, err := buildGmailMIME(to, cc, subject, body, htmlBody, attachments)
	if err != nil {
		return "", fmt.Errorf("build email: %w", err)
	}
	reqBody, err := json.Marshal(map[string]string{"raw": base64.URLEncoding.EncodeToString(mimeBytes)})
	if err != nil {
		return "", fmt.Errorf("marshal send request: %w", err)
	}

	// The raw users.messages.send API requires the userId path param ("me"); unlike
	// the `+send` helper it is not implied, so without it gws returns 400
	// "Required path parameter userId is missing".
	cmd := exec.CommandContext(ctx, gwsPath, "gmail", "users", "messages", "send", "--params", `{"userId":"me"}`, "--json", string(reqBody), "--format", "json")
	cmd.Env = gmailChildEnv(cfg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// gws prints the API error JSON to stdout, so include both streams.
		detail := strings.TrimSpace(stderr.String() + " " + stdout.String())
		return "", fmt.Errorf("gws gmail users messages send failed: %w: %s", err, detail)
	}
	return parseGwsMessageID(stdout.Bytes()), nil
}

// buildGmailMIME assembles a multipart/mixed RFC 2822 message: a UTF-8 text
// body plus one base64 part per attachment. Attachment paths are read from the
// host filesystem (the same host gws runs on), so any file type is supported.
func buildGmailMIME(to string, cc []string, subject, body, htmlBody string, attachments []string) ([]byte, error) {
	parts := &bytes.Buffer{}
	mw := multipart.NewWriter(parts)

	// Body part: a bare text/plain, or — when HTML is supplied — a
	// multipart/alternative carrying both the plain fallback and the rich HTML
	// (so clients without HTML rendering still show the text).
	if strings.TrimSpace(htmlBody) != "" {
		altBuf := &bytes.Buffer{}
		altW := multipart.NewWriter(altBuf)
		ptw, err := altW.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"text/plain; charset=UTF-8"},
			"Content-Transfer-Encoding": {"8bit"},
		})
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(ptw, body); err != nil {
			return nil, err
		}
		htw, err := altW.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"text/html; charset=UTF-8"},
			"Content-Transfer-Encoding": {"8bit"},
		})
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(htw, htmlBody); err != nil {
			return nil, err
		}
		if err := altW.Close(); err != nil {
			return nil, err
		}
		aw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type": {fmt.Sprintf("multipart/alternative; boundary=%q", altW.Boundary())},
		})
		if err != nil {
			return nil, err
		}
		if _, err := aw.Write(altBuf.Bytes()); err != nil {
			return nil, err
		}
	} else {
		tw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"text/plain; charset=UTF-8"},
			"Content-Transfer-Encoding": {"8bit"},
		})
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(tw, body); err != nil {
			return nil, err
		}
	}

	var total int
	for _, p := range attachments {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("attachment %q: %w", p, err)
		}
		total += len(data)
		if total > maxGmailAttachmentBytes {
			return nil, fmt.Errorf("attachments exceed the %d MB limit", maxGmailAttachmentBytes/(1024*1024))
		}
		name := filepath.Base(p)
		ctype := mime.TypeByExtension(filepath.Ext(name))
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		aw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {ctype},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Disposition":       {fmt.Sprintf("attachment; filename=%q", name)},
		})
		if err != nil {
			return nil, err
		}
		if err := writeBase64Wrapped(aw, data); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	full := &bytes.Buffer{}
	fmt.Fprintf(full, "To: %s\r\n", to)
	if len(cc) > 0 {
		fmt.Fprintf(full, "Cc: %s\r\n", strings.Join(normalizeEmailList(cc), ", "))
	}
	fmt.Fprintf(full, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject))
	full.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(full, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", mw.Boundary())
	full.Write(parts.Bytes())
	return full.Bytes(), nil
}

// writeBase64Wrapped writes standard base64 wrapped at 76 columns (RFC 2045).
func writeBase64Wrapped(w io.Writer, data []byte) error {
	enc := base64.StdEncoding.EncodeToString(data)
	for i := 0; i < len(enc); i += 76 {
		end := i + 76
		if end > len(enc) {
			end = len(enc)
		}
		if _, err := io.WriteString(w, enc[i:end]+"\r\n"); err != nil {
			return err
		}
	}
	return nil
}

// SendTest sends a one-off test email, bypassing the enabled gate so the UI's
// "test connection" button works before the channel is saved/enabled. It still
// requires gws to be installed and authenticated.
func (g *GmailService) SendTest(ctx context.Context, to string) (string, error) {
	return g.sendTest(ctx, to, nil, false)
}

// SendTestWithBlockedRecipients sends a one-off test email while validating
// against the caller's draft denylist. The settings UI uses this before saving
// so the test reflects what is currently typed in the form.
func (g *GmailService) SendTestWithBlockedRecipients(ctx context.Context, to string, blockedRecipients []string) (string, error) {
	return g.sendTest(ctx, to, blockedRecipients, true)
}

func (g *GmailService) sendTest(ctx context.Context, to string, blockedRecipients []string, hasBlockedOverride bool) (string, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		to = g.GetConfig().DefaultTo
	}
	if to == "" {
		return "", fmt.Errorf("no recipient: set a default address first")
	}
	var recipients []string
	var err error
	if hasBlockedOverride {
		recipients, err = validateRecipientsAgainstList([]string{to}, "test", blockedRecipients)
	} else {
		recipients, err = g.validateRecipients([]string{to}, "test")
	}
	if err != nil {
		return "", err
	}
	if len(recipients) == 0 {
		return "", fmt.Errorf("no recipient: set a default address first")
	}
	to = strings.Join(recipients, ", ")
	return g.send(ctx, to,
		"[Agent] Gmail test message",
		"This is a test from your agent's Gmail channel. If you received it, outbound Gmail is configured correctly.")
}

func normalizeGmailConfig(cfg *GmailConfig) *GmailConfig {
	if cfg == nil {
		return &GmailConfig{}
	}
	out := *cfg
	out.DefaultTo = strings.TrimSpace(out.DefaultTo)
	out.BlockedRecipients = normalizeEmailList(out.BlockedRecipients)
	return &out
}

func normalizeEmailList(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			email := strings.ToLower(strings.TrimSpace(part))
			if email == "" || seen[email] {
				continue
			}
			seen[email] = true
			out = append(out, email)
		}
	}
	return out
}

func (g *GmailService) blockedRecipients() []string {
	g.mu.RLock()
	cfg := g.config
	g.mu.RUnlock()
	if cfg == nil {
		return nil
	}
	return normalizeEmailList(append([]string(nil), cfg.BlockedRecipients...))
}

// gmailChildEnv builds the environment for the gws child process, layering the
// optional auth knobs on top of the server's own environment.
func gmailChildEnv(cfg *GmailConfig) []string {
	env := os.Environ()
	if cfg == nil {
		return env
	}
	if v := strings.TrimSpace(cfg.ConfigHome); v != "" {
		env = append(env, "XDG_CONFIG_HOME="+v)
	}
	if v := strings.TrimSpace(cfg.CredentialsFile); v != "" {
		env = append(env, "GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE="+v)
	}
	if v := strings.TrimSpace(cfg.Token); v != "" {
		env = append(env, "GOOGLE_WORKSPACE_CLI_TOKEN="+v)
	}
	return env
}

// renderGmailButtonOptions turns ButtonOptions into a plain-text instruction
// line, since email has no interactive buttons.
func renderGmailButtonOptions(opts *ButtonOptions) string {
	if opts == nil {
		return ""
	}
	if len(opts.Options) > 0 {
		return "Options: " + strings.Join(opts.Options, " / ")
	}
	if opts.YesNoOnly {
		yes := opts.YesLabel
		if yes == "" {
			yes = "Approve"
		}
		no := opts.NoLabel
		if no == "" {
			no = "Reject"
		}
		return fmt.Sprintf("Options: %s / %s", yes, no)
	}
	return ""
}

// gmailSubject derives a concise subject line from the message body.
func gmailSubject(message string) string {
	line := strings.TrimSpace(message)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	// Only the auto-derived fallback is bounded (the agent's explicit email_subject
	// is used verbatim, no truncation). Truncate by RUNES, never bytes, so a
	// multi-byte char (em dash, emoji) is never cut in half into a broken character.
	const maxRunes = 150
	if r := []rune(line); len(r) > maxRunes {
		line = strings.TrimSpace(string(r[:maxRunes])) + "…"
	}
	if line == "" {
		return "[Agent] Action needed"
	}
	return "[Agent] " + line
}

// parseGwsMessageID best-effort extracts a message/thread ID from gws JSON
// output. gws emits structured JSON for agents; field names vary by command,
// so we probe the common ones and fall back to a non-empty sentinel so the
// notification manager logs the send as successful.
func parseGwsMessageID(out []byte) string {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return "sent"
	}
	var m map[string]any
	if err := json.Unmarshal(trimmed, &m); err == nil {
		for _, key := range []string{"id", "messageId", "message_id", "threadId", "thread_id"} {
			if v, ok := m[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return "sent"
}
