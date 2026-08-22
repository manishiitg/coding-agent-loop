package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestFriendlyError401MapsToReconnect(t *testing.T) {
	fe := friendlyError("Google Workspace", http.StatusUnauthorized, "401 Unauthorized: token expired")

	if fe.Code != "unauthorized" {
		t.Errorf("code = %q, want %q", fe.Code, "unauthorized")
	}
	if fe.Title != "Google Workspace needs reconnection" {
		t.Errorf("title = %q, want %q", fe.Title, "Google Workspace needs reconnection")
	}
	if fe.Action != "reconnect" {
		t.Errorf("action = %q, want %q", fe.Action, "reconnect")
	}
	// Raw diagnostics must survive for the Advanced section.
	if !strings.Contains(fe.Raw, "token expired") {
		t.Errorf("raw = %q, want it to preserve the original error", fe.Raw)
	}
}

func TestFriendlyErrorDetects401InBodyWithoutStatus(t *testing.T) {
	// The delegated OAuth handler often reports failures as text with no
	// meaningful HTTP status, so the body itself has to be classified.
	fe := friendlyError("Notion", 0, "failed to connect: server returned 401")
	if fe.Code != "unauthorized" {
		t.Errorf("code = %q, want %q", fe.Code, "unauthorized")
	}
}

func TestFriendlyErrorMappings(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		raw      string
		wantCode string
	}{
		{"403 forbidden", http.StatusForbidden, "forbidden", "forbidden"},
		{"404 not found", http.StatusNotFound, "not found", "not_found"},
		{"429 rate limited", http.StatusTooManyRequests, "slow down", "rate_limited"},
		{"timeout", 0, "context deadline exceeded", "timeout"},
		{"dns failure", 0, "dial tcp: no such host", "unreachable"},
		{"500 upstream", http.StatusInternalServerError, "internal error", "service_error"},
		{"unclassified", 0, "something odd happened", "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fe := friendlyError("Slack", tc.status, tc.raw)
			if fe.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", fe.Code, tc.wantCode)
			}
			if fe.Title == "" || fe.Message == "" {
				t.Error("friendly errors must always carry a title and message")
			}
			// A raw transport string must never be the user-facing message.
			if fe.Message == tc.raw {
				t.Errorf("message leaked the raw error: %q", fe.Message)
			}
		})
	}
}

func TestEntrySetupRequired(t *testing.T) {
	dcr := &CatalogEntry{Auth: authDCR}
	if entrySetupRequired(dcr) {
		t.Error("DCR entries never require admin setup")
	}

	tok := &CatalogEntry{Auth: authToken}
	if entrySetupRequired(tok) {
		t.Error("token entries never require admin setup")
	}

	missingEnv := &CatalogEntry{Auth: authOAuthApp, ClientIDEnv: "TEST_UNSET_CLIENT_ID"}
	if !entrySetupRequired(missingEnv) {
		t.Error("oauth_app entry with unset client id env must require setup")
	}

	t.Setenv("TEST_SET_CLIENT_ID", "abc123")
	present := &CatalogEntry{Auth: authOAuthApp, ClientIDEnv: "TEST_SET_CLIENT_ID"}
	if entrySetupRequired(present) {
		t.Error("oauth_app entry with client id env set must not require setup")
	}

	// A client id the provider publishes for general use leaves nothing to set up.
	published := &CatalogEntry{
		Auth:        authOAuthApp,
		URL:         "https://mcp.slack.com/mcp",
		ClientID:    "1601185624273.8899143856786",
		ClientIDEnv: "TEST_UNSET_CLIENT_ID",
	}
	if entrySetupRequired(published) {
		t.Error("a published public client id must not require setup")
	}
}

func TestBuildServerConfigUsesPublishedClientID(t *testing.T) {
	entry := &CatalogEntry{
		ID:          "slack",
		Auth:        authOAuthApp,
		URL:         "https://mcp.slack.com/mcp",
		AuthURL:     "https://slack.com/oauth/v2_user/authorize",
		TokenURL:    "https://slack.com/api/oauth.v2.user.access",
		ClientID:    "published-id",
		ClientIDEnv: "TEST_UNSET_SLACK_ID",
	}

	cfg, err := buildServerConfig(entry, "~/tokens/slack.json", "")
	if err != nil {
		t.Fatalf("a published client id must be enough on its own: %v", err)
	}
	if cfg.OAuth.ClientID != "published-id" {
		t.Errorf("client id = %q, want the published one", cfg.OAuth.ClientID)
	}
	if !cfg.OAuth.UsePKCE {
		t.Error("a public client must use PKCE, having no secret to prove itself with")
	}
	if cfg.OAuth.ClientSecret != "" {
		t.Error("a published public client must not carry a secret")
	}
}

func TestBuildServerConfigPrefersOperatorClientIDOverPublished(t *testing.T) {
	t.Setenv("TEST_SLACK_OWN_ID", "operators-own-app")

	entry := &CatalogEntry{
		ID:          "slack",
		Auth:        authOAuthApp,
		URL:         "https://mcp.slack.com/mcp",
		ClientID:    "published-id",
		ClientIDEnv: "TEST_SLACK_OWN_ID",
	}

	cfg, err := buildServerConfig(entry, "~/tokens/slack.json", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// An operator's own app must win, so the consent screen can carry their
	// name rather than the published client's.
	if cfg.OAuth.ClientID != "operators-own-app" {
		t.Errorf("client id = %q, want the operator's own", cfg.OAuth.ClientID)
	}
}

func TestBuildServerConfigDCRUsesAutoDiscovery(t *testing.T) {
	entry := &CatalogEntry{
		ID:   "notion",
		Auth: authDCR,
		URL:  "https://mcp.notion.com/mcp",
	}

	cfg, err := buildServerConfig(entry, "~/.config/mcpagent/tokens/u1/notion.json", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OAuth == nil {
		t.Fatal("DCR entry must produce an OAuth config")
	}
	if !cfg.OAuth.AutoDiscover {
		t.Error("DCR entry must enable auto-discovery")
	}
	if !cfg.OAuth.UsePKCE {
		t.Error("PKCE must be on")
	}
	if cfg.OAuth.ClientID != "" {
		t.Error("DCR entry must not hardcode a client id")
	}
	if cfg.OAuth.TokenFile != "~/.config/mcpagent/tokens/u1/notion.json" {
		t.Errorf("token file = %q, want the per-user path", cfg.OAuth.TokenFile)
	}
}

func TestBuildServerConfigOAuthAppReadsEnvNotCatalog(t *testing.T) {
	t.Setenv("TEST_G_CLIENT_ID", "client-from-env")
	t.Setenv("TEST_G_CLIENT_SECRET", "secret-from-env")

	entry := &CatalogEntry{
		ID:              "google-workspace",
		Auth:            authOAuthApp,
		URL:             "https://example.googleapis.com/mcp",
		AuthURL:         "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:        "https://oauth2.googleapis.com/token",
		ClientIDEnv:     "TEST_G_CLIENT_ID",
		ClientSecretEnv: "TEST_G_CLIENT_SECRET",
		Scopes:          []string{"https://www.googleapis.com/auth/gmail.readonly"},
	}

	cfg, err := buildServerConfig(entry, "~/tokens/g.json", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OAuth.ClientID != "client-from-env" {
		t.Errorf("client id = %q, want the env value", cfg.OAuth.ClientID)
	}
	if cfg.OAuth.ClientSecret != "secret-from-env" {
		t.Errorf("client secret = %q, want the env value", cfg.OAuth.ClientSecret)
	}
	if cfg.OAuth.AutoDiscover {
		t.Error("explicit auth/token URLs must disable auto-discovery")
	}
}

func TestBuildServerConfigOAuthAppMissingEnvIsSetupRequired(t *testing.T) {
	entry := &CatalogEntry{
		ID:          "google-workspace",
		Auth:        authOAuthApp,
		ClientIDEnv: "TEST_DEFINITELY_UNSET_ID",
	}

	_, err := buildServerConfig(entry, "~/tokens/g.json", "")
	if err == nil {
		t.Fatal("expected an error when the client id env is unset")
	}
	if !strings.HasPrefix(err.Error(), "setup_required:") {
		t.Errorf("error = %q, want a setup_required-prefixed error so the handler can map it", err.Error())
	}
}

func TestBuildServerConfigTokenStoresCredentialInEnv(t *testing.T) {
	entry := &CatalogEntry{
		ID:          "slack",
		Auth:        authToken,
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-slack"},
		TokenEnvVar: "SLACK_BOT_TOKEN",
		ExtraEnv:    map[string]string{"SLACK_TEAM_ID": ""},
	}

	cfg, err := buildServerConfig(entry, "~/tokens/slack.json", "xoxb-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env["SLACK_BOT_TOKEN"] != "xoxb-secret" {
		t.Errorf("token env = %q, want the supplied token", cfg.Env["SLACK_BOT_TOKEN"])
	}
	if _, ok := cfg.Env["SLACK_TEAM_ID"]; !ok {
		t.Error("extra env keys must be carried through")
	}
	if cfg.OAuth != nil {
		t.Error("token entries must not build an OAuth config")
	}
}

func TestBuildServerConfigTokenRequiresCredential(t *testing.T) {
	entry := &CatalogEntry{
		ID:          "slack",
		Auth:        authToken,
		TokenEnvVar: "SLACK_BOT_TOKEN",
		TokenLabel:  "Slack bot token",
	}

	if _, err := buildServerConfig(entry, "~/tokens/slack.json", ""); err == nil {
		t.Fatal("expected an error when no token is supplied")
	}
}

func TestCaptureWriterRecordsStatusAndBody(t *testing.T) {
	rec := newCaptureWriter()
	rec.Header().Set("X-Test", "1")
	rec.WriteHeader(http.StatusBadRequest)
	rec.Write([]byte("boom"))

	if rec.status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.status, http.StatusBadRequest)
	}
	if rec.body.String() != "boom" {
		t.Errorf("body = %q, want %q", rec.body.String(), "boom")
	}
	if rec.Header().Get("X-Test") != "1" {
		t.Error("headers must be recorded so they can be replayed")
	}
}

func TestCaptureWriterDefaultsTo200(t *testing.T) {
	// A handler that writes a body without calling WriteHeader implies 200;
	// if this defaulted to 0 the connect handler would treat success as failure.
	rec := newCaptureWriter()
	rec.Write([]byte(`{"auth_url":"https://example.com"}`))
	if rec.status != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.status, http.StatusOK)
	}
}
