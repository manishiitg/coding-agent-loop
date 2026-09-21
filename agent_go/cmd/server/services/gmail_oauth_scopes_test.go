package services

import "testing"

func TestGmailOAuthScopesForAgentWriteIsExplicitAndLeastPrivilege(t *testing.T) {
	base := gmailOAuthScopesFor(false, false, nil)
	if containsString(base, GmailComposeScope) {
		t.Fatalf("default scopes unexpectedly include agent write access: %v", base)
	}

	writable := gmailOAuthScopesFor(false, true, nil)
	if !containsString(writable, GmailComposeScope) {
		t.Fatalf("agent write opt-in did not request %s: %v", GmailComposeScope, writable)
	}
	for _, broader := range []string{"https://www.googleapis.com/auth/gmail.modify", "https://mail.google.com/"} {
		if containsString(writable, broader) {
			t.Fatalf("agent write opt-in requested broader scope %s: %v", broader, writable)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
