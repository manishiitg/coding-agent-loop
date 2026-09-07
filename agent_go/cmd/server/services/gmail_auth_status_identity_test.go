package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeGws writes an executable that answers `gws auth status` with statusJSON
// and either fails `gws gmail users getProfile` (profileJSON == "") or answers
// it with profileJSON -- the two lookups computeAuthStatus can make.
func fakeGws(t *testing.T, statusJSON, profileJSON string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gws")
	profile := "exit 1"
	if profileJSON != "" {
		profile = "cat <<'EOF'\n" + profileJSON + "\nEOF"
	}
	script := "#!/bin/sh\ncase \"$1 $2\" in\n\"auth status\") cat <<'EOF'\n" + statusJSON + "\nEOF\n;;\n*) " + profile + "\n;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

const sendOnlyAuthStatus = `{
  "encryption_valid": true,
  "has_refresh_token": true,
  "encrypted_credentials_exists": true,
  "scopes": ["email", "openid", "https://www.googleapis.com/auth/gmail.send", "https://www.googleapis.com/auth/userinfo.email"],
  "user": "sender@example.com"
}`

// A gmail.send-only login cannot call getProfile (that needs a read scope),
// so the address must come from the identity `gws auth status` reports --
// otherwise the UI shows "Address not known yet" for a working sender.
func TestAuthStatusTakesTheAddressFromAuthStatusWhenGetProfileIsRefused(t *testing.T) {
	gws := fakeGws(t, sendOnlyAuthStatus, "")

	st := (&GmailService{}).computeAuthStatus(context.Background(), gws, false, "", nil)

	if !st.GwsInstalled || !st.Authenticated || !st.HasGmailScope {
		t.Fatalf("expected an authenticated send-capable status, got %+v", st)
	}
	if st.Email != "sender@example.com" {
		t.Fatalf("Email = %q, want the auth-status user", st.Email)
	}
}

// Older gws reports no `user`; getProfile stays the fallback there.
func TestAuthStatusFallsBackToGetProfileWhenAuthStatusHasNoIdentity(t *testing.T) {
	status := `{"encryption_valid": true, "has_refresh_token": true, "encrypted_credentials_exists": true, "scopes": ["https://www.googleapis.com/auth/gmail.modify"]}`
	gws := fakeGws(t, status, `{"emailAddress": "profile@example.com", "messagesTotal": 1}`)

	st := (&GmailService{}).computeAuthStatus(context.Background(), gws, false, "", nil)

	if !st.Authenticated || !st.HasGmailScope {
		t.Fatalf("expected an authenticated status, got %+v", st)
	}
	if st.Email != "profile@example.com" {
		t.Fatalf("Email = %q, want the getProfile address", st.Email)
	}
}

// Both unavailable: still authenticated and able to send, just anonymous.
func TestAuthStatusStaysAuthenticatedWithoutAnyIdentity(t *testing.T) {
	status := `{"encryption_valid": true, "has_refresh_token": true, "encrypted_credentials_exists": true, "scopes": ["https://www.googleapis.com/auth/gmail.send"]}`
	gws := fakeGws(t, status, "")

	st := (&GmailService{}).computeAuthStatus(context.Background(), gws, false, "", nil)

	if !st.Authenticated || !st.HasGmailScope || st.Email != "" {
		t.Fatalf("expected authenticated with an empty address, got %+v", st)
	}
}
