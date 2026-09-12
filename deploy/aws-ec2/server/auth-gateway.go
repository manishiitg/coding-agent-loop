package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	sessionDuration      = 12 * time.Hour
	sessionRefreshWindow = 6 * time.Hour
	authRequiredHeader   = "X-AgentWorks-Login"
)

type gateway struct {
	secret        []byte
	password      []byte
	frontendDir   string
	appName       string
	userID        string
	username      string
	sessionCookie string
	agent         *httputil.ReverseProxy
	workspace     *httputil.ReverseProxy
	// disablePasswordGate skips the shared-password session entirely,
	// relying solely on the inner app's own per-user auth. Zero value
	// (false) preserves the shared-password gate every existing deployment
	// (Video Studio, and every gateway{...} literal in this package's own
	// tests) already depends on -- this is opt-in per deployment via
	// GATEWAY_DISABLE_PASSWORD_GATE, never a default.
	disablePasswordGate bool
}

// sessionCookieName derives a product-namespaced cookie name from the
// gateway's identity instead of a separate env var, so two gateways sharing
// a browser origin family never collide on cookie name. The default input
// "video-studio" reproduces the original hardcoded "video_studio_session"
// literal byte for byte, so an already-running Video Studio deployment's
// logged-in sessions survive a redeploy of this now-parameterized binary.
func sessionCookieName(userID string) string {
	return strings.ReplaceAll(userID, "-", "_") + "_session"
}

// gatewayClaims deliberately mirrors only the identity fields consumed by the
// agent API. The password session remains the public-facing authentication
// boundary; this signed, short-lived token is created server-side solely for
// the loopback hop from the gateway to the agent API.
type gatewayClaims struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Provider  string `json:"provider"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func proxyFor(rawURL string) *httputil.ReverseProxy {
	target, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return httputil.NewSingleHostReverseProxy(target)
}

func gatewayEnv(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// gatewayBoolEnv defaults to false (unset means "off") so a new opt-in flag
// can never silently change behavior for a deployment that predates it.
func gatewayBoolEnv(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "true" || value == "1"
}

func newGateway() *gateway {
	secret := os.Getenv("AUTH_SECRET")
	password := os.Getenv("ACCESS_PASSWORD")
	if len(secret) < 32 || password == "" {
		log.Fatal("AUTH_SECRET (32+ chars) and ACCESS_PASSWORD are required")
	}
	userID := gatewayEnv("GATEWAY_USER_ID", "video-studio")
	return &gateway{
		secret:              []byte(secret),
		password:            []byte(password),
		frontendDir:         os.Getenv("FRONTEND_DIR"),
		appName:             gatewayEnv("APP_NAME", "Video Studio"),
		userID:              userID,
		username:            gatewayEnv("GATEWAY_USERNAME", "video-studio"),
		sessionCookie:       sessionCookieName(userID),
		agent:               proxyFor(gatewayEnv("AGENT_API_URL", "http://127.0.0.1:8000")),
		workspace:           proxyFor(gatewayEnv("WORKSPACE_API_URL", "http://127.0.0.1:8080")),
		disablePasswordGate: gatewayBoolEnv("GATEWAY_DISABLE_PASSWORD_GATE"),
	}
}

func (g *gateway) serveFrontend(w http.ResponseWriter, r *http.Request) {
	cleanPath := filepath.Clean("/" + r.URL.Path)
	path := filepath.Join(g.frontendDir, cleanPath)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		// Release deployments swap the frontend directory behind a stable URL.
		// The HTML entry point selects hashed JS assets, so it must never be
		// served from a previous browser cache after a release.
		if filepath.Base(path) == "index.html" || filepath.Base(path) == "runtime-config.js" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFile(w, r, path)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Join(g.frontendDir, "index.html"))
}

func (g *gateway) signedSession(expires time.Time) string {
	payload := strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, g.secret)
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (g *gateway) sessionExpiry(r *http.Request) (time.Time, bool) {
	cookie, err := r.Cookie(g.sessionCookie)
	if err != nil {
		return time.Time{}, false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return time.Time{}, false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().After(time.Unix(expires, 0)) {
		return time.Time{}, false
	}
	expiresAt := time.Unix(expires, 0)
	want := g.signedSession(expiresAt)
	if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(want)) != 1 {
		return time.Time{}, false
	}
	return expiresAt, true
}

func (g *gateway) setSessionCookie(w http.ResponseWriter, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     g.sessionCookie,
		Value:    g.signedSession(expires),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
}

func isGatewayAPIRoute(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/") || path == "/ws" || strings.HasPrefix(path, "/ws/")
}

func apiLoginURL(r *http.Request) string {
	next := "/"
	if referer, err := url.Parse(r.Referer()); err == nil && referer.Host == r.Host {
		// A protected API request can be made by the login screen itself while
		// an earlier authenticated request is still settling during logout.
		// Never use /login as the next destination or every 401 would wrap the
		// current login URL in another /login?next=... redirect.
		if referer.Path != "/login" && !strings.HasPrefix(referer.Path, "/login/") {
			next = safeNext(referer.RequestURI())
		}
	}
	return "/login?next=" + url.QueryEscape(next)
}

func (g *gateway) agentToken() (string, error) {
	now := time.Now()
	userID := g.userID
	if userID == "" {
		userID = "video-studio"
	}
	username := g.username
	if username == "" {
		username = "video-studio"
	}
	claims := gatewayClaims{
		UserID:    userID,
		Username:  username,
		Provider:  "gateway",
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(15 * time.Minute).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := header + "." + body
	mac := hmac.New(sha256.New, g.secret)
	_, _ = mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// verifyAgentToken checks an HS256 JWT issued by the agent API (or by
// agentToken above — same secret) and returns its user id. Only what the
// gateway needs: signature, expiry, a non-empty user_id. The agent still
// runs its own full validation afterwards.
func (g *gateway) verifyAgentToken(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	mac := hmac.New(sha256.New, g.secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	var claims struct {
		UserID string `json:"user_id"`
		Exp    int64  `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.UserID == "" || claims.Exp <= time.Now().Unix() {
		return "", false
	}
	return claims.UserID, true
}

// bearerToken is the credential a request carries: an Authorization header,
// or ?token= for the places a browser cannot set a header (SSE, WebSocket
// upgrades, file links).
func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// agentPublicPath mirrors the agent API's own unauthenticated routes
// (auth_middleware.go shouldSkipAuth): what a browser needs before it has a
// token. Everything else needs one when the password gate is off.
func agentPublicPath(path string) bool {
	for _, p := range []string{
		"/api/auth/login", "/api/auth/register", "/api/auth/mode", "/api/auth/start", "/api/auth/callback",
		"/api/auth/desktop/exchange", "/api/auth/providers", "/api/health", "/api/capabilities",
		"/api/oauth/callback",
		// Google navigates the browser here after Gmail consent. It cannot carry
		// the AgentWorks JWT, and the inner API validates the random, single-use
		// OAuth state before it writes any credential.
		"/api/human-feedback/gmail/auth/callback",
	} {
		if path == p {
			return true
		}
	}
	return strings.HasPrefix(path, "/api/shared/") || strings.HasPrefix(path, "/api/downloads/")
}

// requireUserToken is the per-user gate used when the shared password gate
// is off: the request must carry a valid app JWT, whose user id is stamped
// onto X-User-ID so the workspace API never trusts a client-chosen header.
// Returns false after writing the 401.
func (g *gateway) requireUserToken(w http.ResponseWriter, r *http.Request) bool {
	token := bearerToken(r)
	userID, ok := g.verifyAgentToken(token)
	if !ok {
		w.Header().Set(authRequiredHeader, apiLoginURL(r))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"authentication_required"}`)
		return false
	}
	if r.Header.Get("Authorization") == "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("X-User-ID", userID)
	return true
}

func (g *gateway) serveAgent(w http.ResponseWriter, r *http.Request) {
	// With the shared password gate off, every user signs in to the app
	// itself and arrives with their own JWT. The gateway then never mints
	// its service identity: an unauthenticated request is refused here
	// (apart from the login/mode/health routes the app needs beforehand),
	// so nobody can act as the fixed "video-studio" user any more.
	if g.disablePasswordGate {
		if !agentPublicPath(r.URL.Path) && !g.requireUserToken(w, r) {
			return
		}
		g.agent.ServeHTTP(w, r)
		return
	}
	// An explicit bearer token wins. Public file links and browser SSE cannot
	// attach a custom header, so the agent API also supports ?token=. Preserve
	// that token as a bearer credential instead of replacing it with the
	// gateway's service identity; the token determines which user's workspace
	// the public file handler reads from.
	//
	// Browser users without either token authenticate through the HttpOnly
	// password-session cookie, so the gateway supplies the signed internal
	// token required by the agent API on their behalf.
	if r.Header.Get("Authorization") == "" {
		if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		} else {
			token, err := g.agentToken()
			if err != nil {
				http.Error(w, "Unable to authenticate gateway request", http.StatusInternalServerError)
				return
			}
			r.Header.Set("Authorization", "Bearer "+token)
		}
	}
	g.agent.ServeHTTP(w, r)
}

func safeNext(next string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		return next
	}
	return "/"
}

func (g *gateway) login(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.URL.Query().Get("next"))
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err == nil && subtle.ConstantTimeCompare([]byte(r.Form.Get("password")), g.password) == 1 {
			expires := time.Now().Add(sessionDuration)
			g.setSessionCookie(w, expires)
			http.Redirect(w, r, safeNext(r.Form.Get("next")), http.StatusSeeOther)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html><html><head><meta name=viewport content='width=device-width,initial-scale=1'><title>%s</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#101827;color:#eef2ff;font:16px system-ui}main{width:min(360px,calc(100%% - 48px));padding:32px;border:1px solid #334155;border-radius:16px;background:#172033}input,button{width:100%%;box-sizing:border-box;padding:12px;border-radius:8px;font:inherit}input{border:1px solid #475569;background:#0f172a;color:white;margin:16px 0}button{border:0;background:#38bdf8;color:#082f49;font-weight:700;cursor:pointer}.error{color:#fda4af}</style></head><body><main><h1>%s</h1><p>Enter the shared access password.</p>", g.appName, g.appName)
	if r.Method == http.MethodPost {
		fmt.Fprint(w, "<p class=error>Incorrect password. Try again.</p>")
	}
	fmt.Fprintf(w, "<form method=post><input type=hidden name=next value=%q><input type=password name=password autofocus required><button type=submit>Continue</button></form></main></body></html>", next)
}

// Webhook receivers authenticate the original request with their own secret.
// Keep management endpoints behind the normal user gate.
func isWorkflowWebhookRequest(r *http.Request) bool {
	const prefix = "/api/hooks/workflow/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	id := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(id, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	if r.Method == http.MethodPost {
		return len(parts) == 1
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	return (len(parts) == 3 && parts[1] == "runs" && r.Method == http.MethodGet) || (len(parts) == 4 && parts[1] == "runs" && parts[3] == "artifact")
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isWorkflowWebhookRequest(r) {
		r.Header.Del("X-User-ID")
		g.agent.ServeHTTP(w, r)
		return
	}
	if g.disablePasswordGate {
		// No shared-password session to check or renew -- the inner app's
		// own per-user auth is the sole gate. /login and /logout have
		// nothing to do in this mode, so they fall through to routing like
		// any other path (serveFrontend's SPA fallback handles them).
		g.route(w, r)
		return
	}
	if r.URL.Path == "/login" {
		g.login(w, r)
		return
	}
	if r.URL.Path == "/logout" {
		http.SetCookie(w, &http.Cookie{Name: g.sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	expiresAt, authenticated := g.sessionExpiry(r)
	if !authenticated {
		if isGatewayAPIRoute(r.URL.Path) {
			loginURL := apiLoginURL(r)
			w.Header().Set(authRequiredHeader, loginURL)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"error":"authentication_required"}`)
			return
		}
		loginURL := "/login?next=" + url.QueryEscape(r.URL.RequestURI())
		http.Redirect(w, r, loginURL, http.StatusSeeOther)
		return
	}
	// Renew an active browser session before it expires. This prevents regular
	// users from being asked for the shared password repeatedly while still
	// allowing an idle browser to expire normally.
	if time.Until(expiresAt) < sessionRefreshWindow {
		g.setSessionCookie(w, time.Now().Add(sessionDuration))
	}
	g.route(w, r)
}

func (g *gateway) route(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/wp"):
		// The workspace API has no auth of its own beyond X-User-ID. Behind
		// the password gate the cookie covered it; without that gate the
		// user's JWT must, and its user id is what the header carries.
		if g.disablePasswordGate && !g.requireUserToken(w, r) {
			return
		}
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api/wp")
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		g.workspace.ServeHTTP(w, r)
	case strings.HasPrefix(r.URL.Path, "/api"), strings.HasPrefix(r.URL.Path, "/ws"):
		g.serveAgent(w, r)
	default:
		g.serveFrontend(w, r)
	}
}

func main() {
	server := &http.Server{Addr: gatewayEnv("GATEWAY_ADDR", ":8090"), Handler: newGateway(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.ListenAndServe())
}
