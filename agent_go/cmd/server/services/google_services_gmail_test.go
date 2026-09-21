package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoogleCLIAccessIncludesGmailOnlyAfterObservedReadGrant(t *testing.T) {
	connection := GmailConnection{
		ID:              "gmail_001",
		DisplayName:     "Finance",
		Email:           "finance@example.com",
		ClientName:      "finance",
		AuthBackend:     "gog",
		Enabled:         true,
		AllowReadAccess: true,
	}
	service := &GmailService{config: &GmailConfig{
		Connections:         []GmailConnection{connection},
		DefaultConnectionID: connection.ID,
	}}
	service.gogPath = fakeCheckedGog(t, []gogStoredAccount{{
		Email: connection.Email, Client: connection.ClientName, Valid: true,
		Scopes: []string{GmailReadonlyScope},
	}})

	access, err := service.GoogleCLIAccessForConnection(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if writable, ok := access.Grants["gmail"]; !ok || writable {
		t.Fatalf("gmail grant = %v, want present and read-only", access.Grants)
	}

	// The connection still records the read-access opt-in, but Google's
	// observed grant no longer includes it (e.g. downgraded, or the account
	// was never actually reconnected with that scope) — must fail closed on
	// the LIVE check, not on whatever the connection happens to have cached.
	service.InvalidateConnectionAuthCache(connection.ID)
	service.gogPath = fakeCheckedGog(t, []gogStoredAccount{{
		Email: connection.Email, Client: connection.ClientName, Valid: true,
		Scopes: []string{"https://www.googleapis.com/auth/gmail.send"},
	}})
	if _, err := service.GoogleCLIAccessForConnection(context.Background(), ""); err == nil {
		t.Fatal("stored Gmail read request without Google's observed grant must fail closed")
	}
}

func TestGoogleCLIAccessRequiresOptInAndLiveComposeScopeForAgentWrites(t *testing.T) {
	connection := GmailConnection{
		ID:                    "gmail_003",
		DisplayName:           "Support",
		Email:                 "support@example.com",
		ClientName:            "support",
		AuthBackend:           "gog",
		Enabled:               true,
		AllowReadAccess:       true,
		AllowAgentWriteAccess: true,
	}
	service := &GmailService{config: &GmailConfig{
		Connections:         []GmailConnection{connection},
		DefaultConnectionID: connection.ID,
	}}
	service.gogPath = fakeCheckedGog(t, []gogStoredAccount{{
		Email: connection.Email, Client: connection.ClientName, Valid: true,
		Scopes: []string{GmailReadonlyScope, GmailComposeScope},
	}})

	access, err := service.GoogleCLIAccessForConnection(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if writable, ok := access.Grants["gmail"]; !ok || !writable {
		t.Fatalf("gmail grant = %v, want writable", access.Grants)
	}

	// A stored opt-in is not authority by itself. If the live compose scope is
	// absent, the same connection must fall back to read-only access.
	service.InvalidateConnectionAuthCache(connection.ID)
	service.gogPath = fakeCheckedGog(t, []gogStoredAccount{{
		Email: connection.Email, Client: connection.ClientName, Valid: true,
		Scopes: []string{GmailReadonlyScope},
	}})
	access, err = service.GoogleCLIAccessForConnection(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if writable, ok := access.Grants["gmail"]; !ok || writable {
		t.Fatalf("gmail grant = %v, want read-only without live compose scope", access.Grants)
	}

	// Conversely, a token carrying compose does not authorize agents unless
	// the operator explicitly enabled the per-connection permission.
	connection.AllowAgentWriteAccess = false
	service.config.Connections[0] = connection
	service.InvalidateConnectionAuthCache(connection.ID)
	service.gogPath = fakeCheckedGog(t, []gogStoredAccount{{
		Email: connection.Email, Client: connection.ClientName, Valid: true,
		Scopes: []string{GmailReadonlyScope, GmailComposeScope},
	}})
	access, err = service.GoogleCLIAccessForConnection(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if writable := access.Grants["gmail"]; writable {
		t.Fatalf("gmail grant = %v, compose scope bypassed stored opt-in", access.Grants)
	}
}

func TestRunGoogleCLIRemovesGmailSendGuardsOnlyForResolvedWriteGrant(t *testing.T) {
	t.Setenv("GOG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "gog")
	script := `#!/bin/sh
case " $* " in
  *" auth list "*) printf '%s\n' '{"accounts":[{"email":"support@example.com","client":"support","valid":true,"scopes":["https://www.googleapis.com/auth/gmail.readonly","https://www.googleapis.com/auth/gmail.compose"]}]}' ;;
  *) printf '%s\n' "$@" ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	connection := GmailConnection{
		ID: "gmail_004", DisplayName: "Support", Email: "support@example.com",
		ClientName: "support", AuthBackend: "gog", Enabled: true,
		AllowReadAccess: true,
	}
	service := &GmailService{config: &GmailConfig{
		Connections: []GmailConnection{connection}, DefaultConnectionID: connection.ID, GogPath: path,
	}}
	service.gogPath = path
	previous := GetGmailService()
	SetGmailService(service)
	defer SetGmailService(previous)

	output, err := RunGoogleCLI(context.Background(), connection.ID, []string{"gmail", "search", "subject:test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "--readonly") || !strings.Contains(output, "--gmail-no-send") {
		t.Fatalf("read-only Gmail invocation lost its safety guards:\n%s", output)
	}

	service.mu.Lock()
	service.config.Connections[0].AllowAgentWriteAccess = true
	service.mu.Unlock()
	service.InvalidateConnectionAuthCache(connection.ID)
	output, err = RunGoogleCLI(context.Background(), connection.ID, []string{"gmail", "drafts", "create"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "--readonly") || strings.Contains(output, "--gmail-no-send") {
		t.Fatalf("opted-in Gmail write invocation still carried safety guards:\n%s", output)
	}
}

func TestGoogleCLIAccessCrossChecksServiceGrantsAgainstLiveScopes(t *testing.T) {
	connection := GmailConnection{
		ID:          "gmail_002",
		DisplayName: "Ops",
		Email:       "ops@example.com",
		ClientName:  "ops",
		AuthBackend: "gog",
		Enabled:     true,
		Services: []GoogleServiceGrant{
			{Service: "drive", Write: true},     // live grant will only cover read
			{Service: "calendar", Write: false}, // live grant will not cover this at all
		},
	}
	service := &GmailService{config: &GmailConfig{
		Connections:         []GmailConnection{connection},
		DefaultConnectionID: connection.ID,
	}}
	// drive.readonly only: no calendar scope, and calendar isn't part of the
	// Sheets/Docs/Slides "broader Drive scope also counts" carve-out in
	// GoogleScopesGrant, so this is a clean case of an unrelated service.
	service.gogPath = fakeCheckedGog(t, []gogStoredAccount{{
		Email: connection.Email, Client: connection.ClientName, Valid: true,
		Scopes: []string{"https://www.googleapis.com/auth/drive.readonly"},
	}})

	access, err := service.GoogleCLIAccessForConnection(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	writable, ok := access.Grants["drive"]
	if !ok {
		t.Fatal("drive grant missing despite a live read scope being granted")
	}
	if writable {
		t.Fatal("drive grant reported writable, but Google only granted the read scope — requested write must not be trusted over the live check")
	}
	if _, ok := access.Grants["calendar"]; ok {
		t.Fatal("calendar grant present despite no matching live scope — stored request alone must not authorize a service")
	}
}
