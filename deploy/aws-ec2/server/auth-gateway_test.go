package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAgentTokenIsShortLivedAndSignedWithGatewaySecret(t *testing.T) {
	gateway := &gateway{secret: []byte("test-secret-that-is-long-enough")}
	raw, err := gateway.agentToken()
	if err != nil {
		t.Fatalf("agentToken: %v", err)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("token parts = %d, want 3", len(parts))
	}
	mac := hmac.New(sha256.New, gateway.secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	wantSignature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[2]), []byte(wantSignature)) {
		t.Fatal("token signature does not verify")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	claims := &gatewayClaims{}
	if err := json.Unmarshal(payload, claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if claims.UserID != "video-studio" || claims.Username != "video-studio" || claims.Provider != "gateway" {
		t.Fatalf("claims = %#v", claims)
	}
	untilExpiry := time.Until(time.Unix(claims.ExpiresAt, 0))
	if untilExpiry <= 0 || untilExpiry > 16*time.Minute {
		t.Fatalf("unexpected expiration: %d", claims.ExpiresAt)
	}
}

func TestServeAgentForwardsQueryTokenAsBearerCredential(t *testing.T) {
	var gotAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	gateway := &gateway{
		secret: []byte("test-secret-that-is-long-enough"),
		agent:  httputil.NewSingleHostReverseProxy(target),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/public/file?token=workspace-user-token", nil)
	response := httptest.NewRecorder()
	gateway.serveAgent(response, req)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if gotAuthorization != "Bearer workspace-user-token" {
		t.Fatalf("authorization = %q", gotAuthorization)
	}
}

func TestUnauthenticatedAPIRequestReturnsExplicitLoginSignal(t *testing.T) {
	gateway := &gateway{secret: []byte("test-secret-that-is-long-enough")}
	req := httptest.NewRequest(http.MethodGet, "/api/health?full=1", nil)
	req.Header.Set("Referer", "https://example.com/projects/123?tab=chat")
	response := httptest.NewRecorder()

	gateway.ServeHTTP(response, req)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if got := response.Header().Get(authRequiredHeader); got != "/login?next=%2Fprojects%2F123%3Ftab%3Dchat" {
		t.Fatalf("login header = %q", got)
	}
	if got := strings.TrimSpace(response.Body.String()); got != `{"error":"authentication_required"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestGmailOAuthCallbackIsPublicAtGateway(t *testing.T) {
	if !agentPublicPath("/api/human-feedback/gmail/auth/callback") {
		t.Fatal("Gmail OAuth callback must bypass the gateway JWT check: Google cannot send an AgentWorks bearer token")
	}
	if agentPublicPath("/api/human-feedback/gmail/connections/gmail_001/auth/start") {
		t.Fatal("only the OAuth callback may be public; Gmail connection management must require authentication")
	}
}

func TestUnauthenticatedAPIRequestIgnoresExternalReferer(t *testing.T) {
	gateway := &gateway{secret: []byte("test-secret-that-is-long-enough")}
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Referer", "https://attacker.example/steal")
	response := httptest.NewRecorder()

	gateway.ServeHTTP(response, req)

	if got := response.Header().Get(authRequiredHeader); got != "/login?next=%2F" {
		t.Fatalf("login header = %q", got)
	}
}

func TestUnauthenticatedAPIRequestDoesNotNestLoginReferer(t *testing.T) {
	gateway := &gateway{secret: []byte("test-secret-that-is-long-enough")}
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Referer", "https://video.example/login?next=%2Flogin%3Fnext%3D%252F")
	response := httptest.NewRecorder()

	gateway.ServeHTTP(response, req)

	if got := response.Header().Get(authRequiredHeader); got != "/login?next=%2F" {
		t.Fatalf("login header = %q, want a clean login target", got)
	}
}

func TestUnauthenticatedFrontendRequestStillRedirectsToLogin(t *testing.T) {
	gateway := &gateway{secret: []byte("test-secret-that-is-long-enough")}
	req := httptest.NewRequest(http.MethodGet, "/projects/123", nil)
	response := httptest.NewRecorder()

	gateway.ServeHTTP(response, req)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if got := response.Header().Get("Location"); got != "/login?next=%2Fprojects%2F123" {
		t.Fatalf("location = %q", got)
	}
}

func TestAuthenticatedRequestRefreshesSessionNearExpiry(t *testing.T) {
	gateway := &gateway{
		secret:        []byte("test-secret-that-is-long-enough"),
		frontendDir:   t.TempDir(),
		sessionCookie: sessionCookieName("video-studio"),
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: gateway.sessionCookie, Value: gateway.signedSession(time.Now().Add(time.Hour))})
	response := httptest.NewRecorder()

	gateway.ServeHTTP(response, req)

	result := response.Result()
	defer result.Body.Close()
	var refreshed *http.Cookie
	for _, cookie := range result.Cookies() {
		if cookie.Name == gateway.sessionCookie {
			refreshed = cookie
			break
		}
	}
	if refreshed == nil {
		t.Fatal("expected a refreshed session cookie")
	}
	if remaining := time.Until(refreshed.Expires); remaining < 11*time.Hour || remaining > 13*time.Hour {
		t.Fatalf("refreshed session lifetime = %s", remaining)
	}
}

func TestSessionCookieNameNamespacesByGatewayIdentity(t *testing.T) {
	if got := sessionCookieName("video-studio"); got != "video_studio_session" {
		t.Errorf("sessionCookieName(%q) = %q, want the original literal so an already-running Video Studio deployment's browser sessions survive a redeploy of this now-parameterized binary", "video-studio", got)
	}
	if got := sessionCookieName("dominion"); got != "dominion_session" {
		t.Errorf("sessionCookieName(%q) = %q, want dominion_session", "dominion", got)
	}
}

func TestNewGatewayDerivesSessionCookieFromGatewayUserID(t *testing.T) {
	t.Setenv("AUTH_SECRET", "test-secret-that-is-long-enough-x")
	t.Setenv("ACCESS_PASSWORD", "pw")
	t.Setenv("GATEWAY_USER_ID", "dominion")

	gw := newGateway()

	if gw.sessionCookie != "dominion_session" {
		t.Errorf("sessionCookie = %q, want dominion_session", gw.sessionCookie)
	}
}

func TestNewGatewayKeepsThePasswordGateByDefault(t *testing.T) {
	t.Setenv("AUTH_SECRET", "test-secret-that-is-long-enough-x")
	t.Setenv("ACCESS_PASSWORD", "pw")

	gw := newGateway()

	if gw.disablePasswordGate {
		t.Fatal("disablePasswordGate should default to false so every existing deployment keeps its current behavior")
	}
}

func TestNewGatewayCanDisableThePasswordGateExplicitly(t *testing.T) {
	t.Setenv("AUTH_SECRET", "test-secret-that-is-long-enough-x")
	t.Setenv("ACCESS_PASSWORD", "pw")
	t.Setenv("GATEWAY_DISABLE_PASSWORD_GATE", "true")

	gw := newGateway()

	if !gw.disablePasswordGate {
		t.Fatal("GATEWAY_DISABLE_PASSWORD_GATE=true should disable the password gate")
	}
}

func TestDisabledPasswordGateRoutesWithoutAnySessionCookie(t *testing.T) {
	frontendDir := t.TempDir()
	if err := os.WriteFile(frontendDir+"/index.html", []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	gw := &gateway{
		secret:              []byte("test-secret-that-is-long-enough"),
		frontendDir:         frontendDir,
		sessionCookie:       sessionCookieName("dominion"),
		disablePasswordGate: true,
	}

	req := httptest.NewRequest(http.MethodGet, "/projects/123", nil)
	response := httptest.NewRecorder()

	gw.ServeHTTP(response, req)

	// No session cookie, and no password gate to reject it: this must reach
	// serveFrontend's SPA fallback (200, index.html), never the /login
	// redirect a password-gated deployment would produce here.
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (SPA fallback, no login redirect)", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Location"); got != "" {
		t.Fatalf("unexpected redirect to %q -- the password gate should be fully bypassed", got)
	}
}

// With the password gate off the gateway must NOT lend its service identity
// to anonymous callers (the pre-2026-09-02 behaviour, which let anyone act
// as the fixed product user). Public app routes pass through untouched;
// everything else needs the caller's own app JWT.
func TestDisabledPasswordGateNeverMintsAFallbackToken(t *testing.T) {
	var gotAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}

	gw := &gateway{
		secret:              []byte("test-secret-that-is-long-enough"),
		agent:               httputil.NewSingleHostReverseProxy(target),
		disablePasswordGate: true,
	}

	response := httptest.NewRecorder()
	gw.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("public route status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if gotAuthorization != "" {
		t.Fatalf("authorization = %q, want none: the gateway must not mint a token for anonymous callers", gotAuthorization)
	}

	response = httptest.NewRecorder()
	gw.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("protected route without a token: status = %d, want 401", response.Code)
	}
}

// With the password gate off, the gateway must never lend its service
// identity: an unauthenticated API request is refused, a valid app JWT is
// forwarded with X-User-ID stamped from the token (never from the client),
// and the login/mode routes the app needs beforehand still pass.
func TestDisabledGateRequiresUserTokenAndStampsUser(t *testing.T) {
	var seenAuth, seenUser string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth, seenUser = r.Header.Get("Authorization"), r.Header.Get("X-User-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	g := &gateway{secret: []byte("0123456789abcdef0123456789abcdef"), userID: "video-studio", disablePasswordGate: true}
	g.agent = proxyFor(upstream.URL)
	g.workspace = proxyFor(upstream.URL)

	for _, path := range []string{"/api/agent-profiles/video-studio/query", "/api/wp/api/documents/x"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-User-ID", "spoofed")
		g.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s without a token: got %d, want 401", path, rec.Code)
		}
	}

	token, err := g.agentToken()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agent-profiles/video-studio/query?token="+token, nil)
	req.Header.Set("X-User-ID", "spoofed")
	g.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || seenAuth != "Bearer "+token || seenUser != "video-studio" {
		t.Fatalf("valid token: code=%d auth=%q user=%q", rec.Code, seenAuth, seenUser)
	}

	rec = httptest.NewRecorder()
	g.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/mode", nil))
	if rec.Code != http.StatusOK || seenAuth != "" {
		t.Fatalf("public auth route: code=%d auth=%q (must reach the app without a minted token)", rec.Code, seenAuth)
	}

	if _, ok := g.verifyAgentToken(token[:len(token)-2] + "xx"); ok {
		t.Fatal("tampered signature accepted")
	}
}

func TestWorkflowWebhookPreservesCredentialsAndBodyWithoutAppLogin(t *testing.T) {
	for _, disableGate := range []bool{false, true} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer trigger-secret" || r.Header.Get("X-Hub-Signature-256") != "sha256=original" || r.Header.Get("X-User-ID") != "" {
				t.Errorf("gateway changed webhook authentication")
			}
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["event"] != "test" {
				t.Errorf("body was not forwarded: %v", err)
			}
			w.WriteHeader(http.StatusAccepted)
		}))
		target, _ := url.Parse(upstream.URL)
		g := &gateway{disablePasswordGate: disableGate, agent: httputil.NewSingleHostReverseProxy(target)}
		req := httptest.NewRequest(http.MethodPost, "/api/hooks/workflow/trigger-id", strings.NewReader(`{"event":"test"}`))
		req.Header.Set("Authorization", "Bearer trigger-secret")
		req.Header.Set("X-Hub-Signature-256", "sha256=original")
		req.Header.Set("X-User-ID", "spoofed-user")
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, req)
		upstream.Close()
		if rec.Code != http.StatusAccepted {
			t.Fatalf("webhook response = %d", rec.Code)
		}
	}
}

func TestWorkflowWebhookExceptionDoesNotExposeManagementOrOtherMethods(t *testing.T) {
	for _, path := range []string{"/api/workflow-webhooks", "/api/workflow-webhooks/id", "/api/hooks/workflow/", "/api/hooks/workflow/id/extra", "/api/hooks/workflow/..", "/api/wp/execute"} {
		if isWorkflowWebhookRequest(httptest.NewRequest(http.MethodPost, path, nil)) {
			t.Errorf("unexpected webhook exception: %s", path)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if isWorkflowWebhookRequest(httptest.NewRequest(method, "/api/hooks/workflow/id", nil)) {
			t.Errorf("unexpected webhook method: %s", method)
		}
	}
}

func TestWebhookReadRoutesPreserveCredentials(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer hook-secret" {
			t.Error("credential changed")
		}
		w.WriteHeader(204)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	g := &gateway{agent: httputil.NewSingleHostReverseProxy(target)}
	for _, p := range []string{"/api/hooks/workflow/id/runs/run", "/api/hooks/workflow/id/runs/run/artifact?path=file&token=signed"} {
		r := httptest.NewRequest("GET", p, nil)
		r.Header.Set("Authorization", "Bearer hook-secret")
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code != 204 {
			t.Fatalf("read route: %d", w.Code)
		}
	}
	if isWorkflowWebhookRequest(httptest.NewRequest("DELETE", "/api/hooks/workflow/id/runs/run", nil)) {
		t.Fatal("mutation bypass")
	}
}
