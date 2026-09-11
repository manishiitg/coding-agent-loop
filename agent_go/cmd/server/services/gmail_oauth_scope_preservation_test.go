package services

import (
	"net/url"
	"testing"

	"golang.org/x/oauth2"
)

func gmailOAuthOptionQuery(t *testing.T, clientName string) url.Values {
	t.Helper()
	cfg := oauth2.Config{
		ClientID:    "client-id",
		RedirectURL: "http://localhost/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL: "https://accounts.example.test/authorize",
		},
	}
	parsed, err := url.Parse(cfg.AuthCodeURL("state", gmailOAuthAuthCodeOptions(clientName)...))
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	return parsed.Query()
}

func TestGmailOAuthSharedClientPreservesExistingScopes(t *testing.T) {
	query := gmailOAuthOptionQuery(t, "")
	if got := query.Get("include_granted_scopes"); got != "true" {
		t.Fatalf("include_granted_scopes = %q, want true", got)
	}
}

func TestGmailOAuthNamedClientCanRemoveScopes(t *testing.T) {
	query := gmailOAuthOptionQuery(t, "mailbox-client")
	if _, ok := query["include_granted_scopes"]; ok {
		t.Fatalf("named client unexpectedly preserves previously granted scopes")
	}
}
