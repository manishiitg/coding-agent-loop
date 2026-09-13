package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// stubSupabasePKCETransport pretends to be Supabase's /auth/v1/token PKCE
// endpoint: it captures the exchange body and answers with canned JSON.
type stubSupabasePKCETransport struct {
	t        *testing.T
	status   int
	body     string
	captured *map[string]string
}

func (s *stubSupabasePKCETransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path != "/auth/v1/token" || r.URL.Query().Get("grant_type") != "pkce" {
		s.t.Errorf("unexpected token request: %s %s", r.Method, r.URL)
	}
	if got := r.Header.Get("apikey"); got != "test-anon-key" {
		s.t.Errorf("apikey header = %q, want test-anon-key", got)
	}
	raw, _ := io.ReadAll(r.Body)
	var body map[string]string
	if err := json.Unmarshal(raw, &body); err != nil {
		s.t.Errorf("decode exchange body: %v", err)
	}
	*s.captured = body
	return &http.Response{
		StatusCode: s.status,
		Status:     http.StatusText(s.status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}, nil
}

// withStubSupabasePKCE swaps the token endpoint for the stub for one test.
func withStubSupabasePKCE(t *testing.T, status int, body string, captured *map[string]string) {
	t.Helper()
	prev := supabaseSocialHTTPClient
	supabaseSocialHTTPClient = &http.Client{Transport: &stubSupabasePKCETransport{
		t: t, status: status, body: body, captured: captured,
	}}
	t.Cleanup(func() { supabaseSocialHTTPClient = prev })
}

func googleUserBody(id, email, name, fullName string) string {
	meta, _ := json.Marshal(map[string]string{"name": name, "full_name": fullName})
	return `{"access_token":"sb-access","user":{"id":"` + id + `","email":"` + email +
		`","user_metadata":` + string(meta) + `}}`
}

func stubbedGoogleProvider() *SupabaseSocialProvider {
	return &SupabaseSocialProvider{SocialProvider: "google", URL: "https://proj.supabase.co", AnonKey: "test-anon-key"}
}

func TestSupabaseGoogleGetAuthURL(t *testing.T) {
	p := stubbedGoogleProvider()
	authURL := p.GetAuthURL("state-123", "https://app.example.com/auth/callback")
	if authURL == "" {
		t.Fatal("expected an auth URL for a configured provider")
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	if u.Scheme != "https" || u.Host != "proj.supabase.co" || u.Path != "/auth/v1/authorize" {
		t.Errorf("unexpected authorize endpoint: %s", authURL)
	}
	q := u.Query()
	if q.Get("provider") != "google" {
		t.Errorf("provider = %q, want google", q.Get("provider"))
	}
	if q.Get("redirect_to") != "https://app.example.com/auth/callback" {
		t.Errorf("redirect_to = %q", q.Get("redirect_to"))
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "s256" {
		t.Errorf("missing PKCE challenge params: %s", authURL)
	}
	// The flow stored a verifier for this state; consumption is single-use.
	if _, ok := consumeSupabasePKCEVerifier("state-123"); !ok {
		t.Error("expected a stored PKCE verifier for the flow state")
	}
	if _, ok := consumeSupabasePKCEVerifier("state-123"); ok {
		t.Error("PKCE verifier must be single-use")
	}
}

func TestSupabaseGoogleGetAuthURLUnconfigured(t *testing.T) {
	p := &SupabaseSocialProvider{SocialProvider: "google"}
	if got := p.GetAuthURL("s", "https://app.example.com/auth/callback"); got != "" {
		t.Errorf("expected empty auth URL, got %q", got)
	}
	if p.IsConfigured() {
		t.Error("expected IsConfigured false without URL/key")
	}
}

func TestSupabaseGoogleExchangeCode(t *testing.T) {
	var captured map[string]string
	withStubSupabasePKCE(t, http.StatusOK,
		googleUserBody("sb-user-1", "ana@gmail.com", "Ana", "Ana Example"), &captured)

	p := stubbedGoogleProvider()
	authURL := p.GetAuthURL("flow-state", "https://app.example.com/auth/callback")
	challenge := mustParseQuery(t, authURL).Get("code_challenge")

	ext, err := p.ExchangeCode(context.Background(), "auth-code-xyz", "https://app.example.com/auth/callback", "flow-state")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if ext.ExternalID != "sb-user-1" || ext.Email != "ana@gmail.com" {
		t.Errorf("unexpected identity: %+v", ext)
	}
	if ext.Username != "Ana" {
		t.Errorf("username = %q, want Ana", ext.Username)
	}
	if ext.Provider != "supabase-google" {
		t.Errorf("provider = %q, want supabase-google", ext.Provider)
	}
	// The exchange must send the code plus the verifier matching the challenge.
	if captured["auth_code"] != "auth-code-xyz" {
		t.Errorf("auth_code = %q", captured["auth_code"])
	}
	sum := sha256.Sum256([]byte(captured["code_verifier"]))
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != challenge {
		t.Error("code_verifier sent does not match the authorize challenge")
	}
}

func TestSupabaseGoogleUsernameFallbacks(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantUsername string
	}{
		{"full name", googleUserBody("u1", "a@gmail.com", "", "Ana Example"), "Ana Example"},
		{"email", googleUserBody("u2", "b@gmail.com", "", ""), "b@gmail.com"},
		{"id", googleUserBody("u3", "", "", ""), "u3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured map[string]string
			withStubSupabasePKCE(t, http.StatusOK, tc.body, &captured)
			p := stubbedGoogleProvider()
			state := "fallback-" + tc.name
			p.GetAuthURL(state, "https://app.example.com/auth/callback")
			ext, err := p.ExchangeCode(context.Background(), "code", "https://app.example.com/auth/callback", state)
			if err != nil {
				t.Fatalf("ExchangeCode: %v", err)
			}
			if ext.Username != tc.wantUsername {
				t.Errorf("username = %q, want %q", ext.Username, tc.wantUsername)
			}
		})
	}
}

func TestSupabaseGoogleExchangeCodeUnknownState(t *testing.T) {
	p := stubbedGoogleProvider()
	if _, err := p.ExchangeCode(context.Background(), "code", "https://app.example.com/auth/callback", "no-such-state"); err == nil {
		t.Error("expected an error for a state with no PKCE verifier")
	} else if !strings.Contains(err.Error(), "expired or already used") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSupabaseGoogleExchangeCodeExpiredState(t *testing.T) {
	p := stubbedGoogleProvider()
	p.GetAuthURL("stale-state", "https://app.example.com/auth/callback")

	old := stateExpiration
	stateExpiration = -time.Second
	defer func() { stateExpiration = old }()

	if _, err := p.ExchangeCode(context.Background(), "code", "https://app.example.com/auth/callback", "stale-state"); err == nil {
		t.Error("expected an error for an expired flow")
	}
}

func TestSupabaseGoogleExchangeCodeServerError(t *testing.T) {
	var captured map[string]string
	withStubSupabasePKCE(t, http.StatusBadRequest, `{"error":"bad code"}`, &captured)

	p := stubbedGoogleProvider()
	p.GetAuthURL("err-state", "https://app.example.com/auth/callback")
	if _, err := p.ExchangeCode(context.Background(), "bad-code", "https://app.example.com/auth/callback", "err-state"); err == nil {
		t.Error("expected an error when Supabase rejects the exchange")
	}
}

func TestSupabaseGoogleProviderRegistered(t *testing.T) {
	p, ok := GetProvider("supabase-google")
	if !ok {
		t.Fatal("supabase-google provider is not registered")
	}
	if p.Type() != "oauth" {
		t.Errorf("type = %q, want oauth", p.Type())
	}
	if _, err := p.ValidateCredentials("u", "p"); err == nil {
		t.Error("expected ValidateCredentials to refuse (OAuth-only provider)")
	}
}

func mustParseQuery(t *testing.T, rawURL string) url.Values {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	return u.Query()
}
