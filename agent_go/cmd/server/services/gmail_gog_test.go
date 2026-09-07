package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubGoogleTokenInfo points googleTokenGrantedScopes at a local server for
// the duration of the test, instead of making a real call to Google — a live
// network dependency in this suite would be slow and break offline/CI runs.
func stubGoogleTokenInfo(t *testing.T, scope string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"scope": %q}`, scope)
	}))
	t.Cleanup(server.Close)
	original := googleTokenInfoURL
	googleTokenInfoURL = server.URL
	t.Cleanup(func() { googleTokenInfoURL = original })
}

func TestGogArgsForAuthPrefersAccessToken(t *testing.T) {
	cfg := &GmailConfig{Token: "tok-123", gogAccountEmail: "ignored@example.com", gogClientName: "ignored"}
	args, err := gogArgsForAuth(cfg)
	if err != nil {
		t.Fatalf("gogArgsForAuth: %v", err)
	}
	want := []string{"--access-token", "tok-123"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestGogArgsForAuthFallsBackToAccountAndClient(t *testing.T) {
	cfg := &GmailConfig{gogAccountEmail: "you@example.com", gogClientName: "primary"}
	args, err := gogArgsForAuth(cfg)
	if err != nil {
		t.Fatalf("gogArgsForAuth: %v", err)
	}
	want := []string{"--account", "you@example.com", "--client", "primary"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestGogArgsForAuthErrorsWithNeither(t *testing.T) {
	if _, err := gogArgsForAuth(&GmailConfig{}); err == nil {
		t.Fatal("expected an error when neither a token nor account/client is set")
	}
	if _, err := gogArgsForAuth(nil); err == nil {
		t.Fatal("expected an error for a nil config")
	}
}

// fakeGog writes an executable that answers `api call gmail v1
// gmail.users.getProfile` with profileJSON (or fails if empty), and echoes
// its full argv, one arg per line, to argvFile — every other test inspects
// that file rather than trying to parse shell quoting.
func fakeGog(t *testing.T, profileJSON string, argvFile string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gog")
	profile := "exit 1"
	if profileJSON != "" {
		profile = "cat <<'EOF'\n" + profileJSON + "\nEOF"
	}
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do echo \"$a\" >> " + shellQuote(argvFile) + "; done\n" +
		"case \" $* \" in\n" +
		"*\" getProfile \"*|*\"gmail.users.getProfile\"*) " + profile + "\n;;\n" +
		"*) echo '{\"id\":\"msg-fake-1\"}' ;;\n" +
		"esac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func readArgvLines(t *testing.T, argvFile string) []string {
	t.Helper()
	data, err := os.ReadFile(argvFile)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func TestComputeAuthStatusGogUsesAccessTokenAndReportsEmail(t *testing.T) {
	stubGoogleTokenInfo(t, "https://www.googleapis.com/auth/gmail.send https://www.googleapis.com/auth/gmail.readonly")
	argvFile := filepath.Join(t.TempDir(), "argv.log")
	gog := fakeGog(t, `{"emailAddress": "sender@example.com"}`, argvFile)

	cfg := &GmailConfig{Token: "tok-abc"}
	st := (&GmailService{}).computeAuthStatusGog(context.Background(), gog, cfg)

	if !st.GwsInstalled || !st.Authenticated || !st.HasGmailScope {
		t.Fatalf("expected an authenticated status, got %+v", st)
	}
	if st.Email != "sender@example.com" {
		t.Fatalf("Email = %q, want sender@example.com", st.Email)
	}

	argv := strings.Join(readArgvLines(t, argvFile), " ")
	if !strings.Contains(argv, "--access-token tok-abc") {
		t.Errorf("argv %q did not pass --access-token tok-abc", argv)
	}
	if !strings.Contains(argv, "gmail.users.getProfile") {
		t.Errorf("argv %q did not call gmail.users.getProfile", argv)
	}
}

// The whole point of the tokeninfo check: report what was actually granted,
// not what we last requested — a project's consent screen can silently drop
// a requested scope (the "College Khabar" gotcha), and a stale hardcoded
// Scopes value would hide that from the UI.
func TestComputeAuthStatusGogReportsActuallyGrantedScopesNotTheRequestedConstant(t *testing.T) {
	// Deliberately narrower than gmailOAuthScopes (which also includes
	// userinfo.email) — proves this isn't just echoing the constant back.
	stubGoogleTokenInfo(t, "https://www.googleapis.com/auth/gmail.readonly")
	argvFile := filepath.Join(t.TempDir(), "argv.log")
	gog := fakeGog(t, `{"emailAddress": "sender@example.com"}`, argvFile)

	cfg := &GmailConfig{Token: "tok-abc"}
	st := (&GmailService{}).computeAuthStatusGog(context.Background(), gog, cfg)

	if len(st.Scopes) != 1 || st.Scopes[0] != "https://www.googleapis.com/auth/gmail.readonly" {
		t.Fatalf("Scopes = %v, want exactly the narrower tokeninfo-reported set", st.Scopes)
	}
}

func TestComputeAuthStatusGogFallsBackToRequestedScopesWhenTokenInfoFails(t *testing.T) {
	// No stub installed: googleTokenInfoURL points at the real Google host,
	// which this test never lets it reach — a short client timeout combined
	// with an unroutable address makes the call fail fast instead of relying
	// on network absence, so the fallback path is exercised deterministically.
	original := googleTokenInfoURL
	googleTokenInfoURL = "http://127.0.0.1:1" // nothing listens here
	t.Cleanup(func() { googleTokenInfoURL = original })

	argvFile := filepath.Join(t.TempDir(), "argv.log")
	gog := fakeGog(t, `{"emailAddress": "sender@example.com"}`, argvFile)

	cfg := &GmailConfig{Token: "tok-abc"}
	st := (&GmailService{}).computeAuthStatusGog(context.Background(), gog, cfg)

	if !st.Authenticated {
		t.Fatalf("expected authentication to still succeed despite the scope check failing, got %+v", st)
	}
	if len(st.Scopes) == 0 {
		t.Fatal("expected a fallback scope list, got none")
	}
}

func TestComputeAuthStatusGogReportsNotAuthenticatedOnFailure(t *testing.T) {
	argvFile := filepath.Join(t.TempDir(), "argv.log")
	gog := fakeGog(t, "", argvFile) // empty profileJSON -> the fake exits 1

	cfg := &GmailConfig{Token: "tok-abc"}
	st := (&GmailService{}).computeAuthStatusGog(context.Background(), gog, cfg)

	if st.Authenticated {
		t.Fatalf("expected Authenticated=false on a getProfile failure, got %+v", st)
	}
	if st.Detail == "" {
		t.Error("expected a non-empty Detail explaining the failure")
	}
}

func TestComputeAuthStatusGogReportsMissingBinary(t *testing.T) {
	st := (&GmailService{}).computeAuthStatusGog(context.Background(), filepath.Join(t.TempDir(), "definitely-not-gog"), &GmailConfig{Token: "x"})
	if st.GwsInstalled || st.Authenticated {
		t.Fatalf("expected an unavailable status for a missing binary, got %+v", st)
	}
}

func TestComputeAuthStatusGogErrorsWithoutAuthKnobs(t *testing.T) {
	argvFile := filepath.Join(t.TempDir(), "argv.log")
	gog := fakeGog(t, `{"emailAddress": "x@example.com"}`, argvFile)

	// No Token and no gogAccountEmail/gogClientName: nothing to authenticate with.
	st := (&GmailService{}).computeAuthStatusGog(context.Background(), gog, &GmailConfig{})
	if st.Authenticated {
		t.Fatalf("expected Authenticated=false with no auth knobs, got %+v", st)
	}
}

func TestSendGogPassesExpectedArgs(t *testing.T) {
	argvFile := filepath.Join(t.TempDir(), "argv.log")
	gog := fakeGog(t, "", argvFile)

	cfg := &GmailConfig{Token: "tok-send"}
	g := &GmailService{}
	msgID, err := g.sendGog(context.Background(), gog, cfg, "to@example.com", "Subject line", "body text")
	if err != nil {
		t.Fatalf("sendGog: %v", err)
	}
	if msgID != "msg-fake-1" {
		t.Fatalf("msgID = %q, want msg-fake-1", msgID)
	}

	argv := strings.Join(readArgvLines(t, argvFile), " ")
	for _, want := range []string{"gmail", "send", "--to", "to@example.com", "--body", "body text", "--access-token", "tok-send", "--json"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv %q missing %q", argv, want)
		}
	}
}

func TestSendGogPropagatesAuthError(t *testing.T) {
	g := &GmailService{}
	if _, err := g.sendGog(context.Background(), "gog", &GmailConfig{}, "to@example.com", "s", "b"); err == nil {
		t.Fatal("expected an error when the connection has no usable auth knobs")
	}
}

func TestSendRawGogPipesMIMEOnStdin(t *testing.T) {
	// A script that dumps stdin to a file, so the test can assert the exact
	// bytes buildGmailMIME produced were what reached the CLI.
	stdinFile := filepath.Join(t.TempDir(), "stdin.log")
	argvFile := filepath.Join(t.TempDir(), "argv.log")
	path := filepath.Join(t.TempDir(), "gog")
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do echo \"$a\" >> " + shellQuote(argvFile) + "; done\n" +
		"cat > " + shellQuote(stdinFile) + "\n" +
		"echo '{\"id\":\"msg-raw-1\"}'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &GmailConfig{Token: "tok-raw"}
	g := &GmailService{}
	msgID, err := g.sendRawGog(context.Background(), path, cfg, "to@example.com", []string{"cc@example.com"}, "Subject", "plain body", "<b>html</b>", nil)
	if err != nil {
		t.Fatalf("sendRawGog: %v", err)
	}
	if msgID != "msg-raw-1" {
		t.Fatalf("msgID = %q, want msg-raw-1", msgID)
	}

	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	if !strings.Contains(string(stdin), "To: to@example.com") || !strings.Contains(string(stdin), "html") {
		t.Errorf("stdin did not look like the built MIME message: %q", string(stdin))
	}

	argv := strings.Join(readArgvLines(t, argvFile), " ")
	if !strings.Contains(argv, "--raw-file -") {
		t.Errorf("argv %q missing --raw-file -", argv)
	}
}
