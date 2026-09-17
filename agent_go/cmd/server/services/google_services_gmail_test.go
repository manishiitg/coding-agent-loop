package services

import (
	"context"
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
