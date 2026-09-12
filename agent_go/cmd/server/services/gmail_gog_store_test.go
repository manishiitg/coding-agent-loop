package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func fakeCheckedGog(t *testing.T, accounts []gogStoredAccount) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gog")
	data, _ := json.Marshal(map[string]any{"accounts": accounts})
	script := "#!/bin/sh\ncat <<'JSON'\n" + string(data) + "\nJSON\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGogMigrationVerifiesIdentityBeforeSwitching(t *testing.T) {
	t.Setenv("GOG_HOME", t.TempDir())
	t.Setenv("GMAIL_OAUTH_TOKEN_DIR", t.TempDir())
	conn := GmailConnection{ID: "gmail_001", Email: "me@example.com", ClientName: "primary", Enabled: true}
	account := gogStoredAccount{Email: conn.Email, Client: conn.ClientName, Valid: true, Scopes: []string{"https://www.googleapis.com/auth/gmail.send"}}
	cfg := &GmailConfig{Connections: []GmailConnection{conn}, GogPath: fakeCheckedGog(t, []gogStoredAccount{account})}
	migrated, err := MigrateGmailConfigToGog(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Connections[0].AuthBackend != "" || cfg.UseGogBackend {
		t.Fatal("migration mutated live config before it could be saved")
	}
	if !migrated.UseGogBackend || migrated.Connections[0].AuthBackend != "gog" {
		t.Fatal("verified account did not switch to gog")
	}
	// A different client's token must never satisfy this account's migration.
	account.Client = "different-client"
	cfg.GogPath = fakeCheckedGog(t, []gogStoredAccount{account})
	if _, err := MigrateGmailConfigToGog(context.Background(), cfg); err == nil {
		t.Fatal("accepted wrong client")
	}
}

func TestGogConnectionDoesNotReadOrRefreshLegacyToken(t *testing.T) {
	t.Setenv("GMAIL_OAUTH_TOKEN_DIR", t.TempDir())
	if err := storeGmailOAuthToken("gmail_001", &oauth2.Token{AccessToken: "legacy", RefreshToken: "legacy-refresh"}); err != nil {
		t.Fatal(err)
	}
	conn := GmailConnection{ID: "gmail_001", Email: "me@example.com", ClientName: "primary", AuthBackend: "gog"}
	cfg := gmailConnectionConfig(conn)
	args, err := gogArgsForAuth(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "" || !cfg.UseGogBackend || strings.Join(args, " ") != "--account me@example.com --client primary" {
		t.Fatal("gog connection fell back to backend-managed credentials")
	}
}

func TestGogSendOnlyStatusUsesActualScopes(t *testing.T) {
	account := gogStoredAccount{Email: "me@example.com", Client: "primary", Valid: true, Scopes: []string{"https://www.googleapis.com/auth/gmail.send"}}
	binary := fakeCheckedGog(t, []gogStoredAccount{account})
	st := (&GmailService{}).computeAuthStatusGog(context.Background(), binary, gmailConnectionConfig(GmailConnection{Email: account.Email, ClientName: account.Client, AuthBackend: "gog"}))
	if !st.Authenticated || !st.HasGmailScope || st.Email != account.Email || len(st.Scopes) != 1 {
		t.Fatalf("send-only status incorrect: %+v", st)
	}
}

func TestNewClientHasOnlyOneCredentialStore(t *testing.T) {
	t.Setenv("GMAIL_OAUTH_CLIENTS_DIR", t.TempDir())
	isolateGogClientStore(t)
	if _, err := CreateOAuthClient(context.Background(), "primary", gmailOAuthClientJSON(t, "client", "secret"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(gmailOAuthClientSecretPath("primary")); !os.IsNotExist(err) {
		t.Fatal("new client created a duplicate secret")
	}
	if !OAuthClientExists("primary") {
		t.Fatal("client unavailable from gog store")
	}
}

func TestFailedGogMigrationRetainsLegacyCredentials(t *testing.T) {
	t.Setenv("GOG_HOME", t.TempDir())
	t.Setenv("GMAIL_OAUTH_CLIENTS_DIR", t.TempDir())
	t.Setenv("GMAIL_OAUTH_TOKEN_DIR", t.TempDir())
	if err := storeGmailOAuthToken("gmail_001", &oauth2.Token{RefreshToken: "fixture-refresh"}); err != nil {
		t.Fatal(err)
	}
	cfg := &GmailConfig{GogPath: fakeCheckedGog(t, nil), Connections: []GmailConnection{{ID: "gmail_001", Email: "me@example.com", ClientName: "missing"}}}
	if _, err := MigrateGmailConfigToGog(context.Background(), cfg); err == nil {
		t.Fatal("migration should fail without client credentials")
	}
	if !HasServerManagedOAuth("gmail_001") || cfg.Connections[0].AuthBackend != "" {
		t.Fatal("failed migration discarded the working legacy state")
	}
}

func TestRetireLegacyGogCredentialsRequiresMatchingClient(t *testing.T) {
	for _, matches := range []bool{true, false} {
		t.Run(map[bool]string{true: "matching", false: "different"}[matches], func(t *testing.T) {
			t.Setenv("GMAIL_OAUTH_CLIENTS_DIR", t.TempDir())
			t.Setenv("GMAIL_OAUTH_TOKEN_DIR", t.TempDir())
			isolateGogClientStore(t)
			if _, err := CreateOAuthClient(context.Background(), "primary", gmailOAuthClientJSON(t, "client", "secret"), false); err != nil {
				t.Fatal(err)
			}
			legacySecret := "secret"
			if !matches {
				legacySecret = "different"
			}
			if err := os.WriteFile(gmailOAuthClientSecretPath("primary"), gmailOAuthClientJSON(t, "client", legacySecret), 0600); err != nil {
				t.Fatal(err)
			}
			if err := storeGmailOAuthToken("gmail_001", &oauth2.Token{RefreshToken: "fixture-refresh"}); err != nil {
				t.Fatal(err)
			}
			cfg := &GmailConfig{Connections: []GmailConnection{{ID: "gmail_001", ClientName: "primary", AuthBackend: "gog"}}}
			err := RetireLegacyGmailCredentials(cfg)
			if matches {
				if err != nil {
					t.Fatal(err)
				}
				if HasServerManagedOAuth("gmail_001") {
					t.Fatal("duplicate backend token retained")
				}
				if _, err := os.Stat(gmailOAuthClientSecretPath("primary")); !os.IsNotExist(err) {
					t.Fatal("duplicate client secret retained")
				}
				if !OAuthClientExists("primary") {
					t.Fatal("authoritative gog client deleted")
				}
			} else if err == nil || !HasServerManagedOAuth("gmail_001") {
				t.Fatal("cleanup must retain unmatched credentials for review")
			}
		})
	}
}

func TestGogMigrationRejectsUnregisteredLegacyConfig(t *testing.T) {
	cfg := &GmailConfig{ConfigHome: "/legacy", Token: "legacy-token"}
	if _, err := MigrateGmailConfigToGog(context.Background(), cfg); err == nil {
		t.Fatal("migration would silently discard unregistered legacy authentication")
	}
	if cfg.Token != "legacy-token" {
		t.Fatal("legacy config changed")
	}
}

func TestOAuthCallbackStoresOnlyInGog(t *testing.T) {
	t.Setenv("GMAIL_OAUTH_CLIENTS_DIR", t.TempDir())
	t.Setenv("GMAIL_OAUTH_TOKEN_DIR", t.TempDir())
	isolateGogClientStore(t)
	if _, err := CreateOAuthClient(context.Background(), "primary", gmailOAuthClientJSON(t, "client", "secret"), false); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			w.Write([]byte(`{"access_token":"fixture-access","refresh_token":"fixture-refresh","token_type":"Bearer","expires_in":3600}`))
			return
		}
		w.Write([]byte(`{"email":"me@example.com","scope":"https://www.googleapis.com/auth/gmail.send"}`))
	}))
	defer server.Close()
	previousEndpoint, previousInfo := google.Endpoint, googleTokenInfoURL
	google.Endpoint.TokenURL = server.URL + "/token"
	googleTokenInfoURL = server.URL + "/info"
	t.Cleanup(func() { google.Endpoint = previousEndpoint; googleTokenInfoURL = previousInfo })
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, succeeds := range []bool{true, false} {
		exit := "0"
		if !succeeds {
			exit = "1"
		}
		if err := os.WriteFile(filepath.Join(bin, "gog"), []byte("#!/bin/sh\nexit "+exit+"\n"), 0755); err != nil {
			t.Fatal(err)
		}
		authURL, err := BeginGmailOAuth("gmail_001", "primary", "http://localhost/callback", false, nil)
		if err != nil {
			t.Fatal(err)
		}
		parsed, _ := url.Parse(authURL)
		id, email, err := CompleteGmailOAuth(context.Background(), parsed.Query().Get("state"), "fixture-code")
		if succeeds && (err != nil || id != "gmail_001" || email != "me@example.com") {
			t.Fatalf("successful import not completed: %s %s %v", id, email, err)
		}
		if !succeeds && err == nil {
			t.Fatal("failed gog import was reported as connected")
		}
		if _, err := os.Stat(gmailOAuthTokenPath("gmail_001")); !os.IsNotExist(err) {
			t.Fatal("OAuth callback wrote duplicate backend token")
		}
	}
}

func TestMixedGoogleBackendsRemainAvailable(t *testing.T) {
	installed := fakeCheckedGog(t, nil)
	missing := filepath.Join(t.TempDir(), "missing-cli")
	cfg := &GmailConfig{Connections: []GmailConnection{{Enabled: true}, {Enabled: true, AuthBackend: "gog"}}}
	if !gmailConfiguredBinaryAvailable(cfg, installed, missing) {
		t.Fatal("missing gog disabled a working gws connection")
	}
	if !gmailConfiguredBinaryAvailable(cfg, missing, installed) {
		t.Fatal("missing gws disabled a working gog connection")
	}
	if gmailConfiguredBinaryAvailable(cfg, missing, missing) {
		t.Fatal("channel enabled without either CLI")
	}
	cfg.UseGogBackend = true
	if gmailConfiguredBinaryAvailable(cfg, installed, missing) {
		t.Fatal("global gog selection fell back to gws")
	}
}
