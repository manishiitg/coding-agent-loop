package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthMiddlewareRequiresJWTInSingleUserMode(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	t.Setenv("AUTH_SECRET", "test-auth-secret-with-enough-entropy")

	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/terminals", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddlewareAcceptsJWTInSingleUserMode(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	t.Setenv("AUTH_SECRET", "test-auth-secret-with-enough-entropy")

	token, err := GenerateJWT("default", "user", "")
	if err != nil {
		t.Fatalf("GenerateJWT failed: %v", err)
	}

	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := GetUserIDFromContext(r.Context()); got != "default" {
			t.Fatalf("user id = %q, want default", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/terminals", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}

func TestSingleUserProductWorkspaceMapsGatewayIdentityToDefaultOwner(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	t.Setenv("DEFAULT_USER_ID", "default")
	t.Setenv("AUTH_SECRET", "test-auth-secret-with-enough-entropy")

	// The rootless gateway uses a product identity for its signed loopback
	// token. Single-user project data is intentionally stored under the
	// deployment's default user, so the request must resolve to that owner.
	token, err := GenerateJWT("video-studio", "video-studio", "")
	if err != nil {
		t.Fatalf("GenerateJWT failed: %v", err)
	}

	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := GetUserIDFromContext(r.Context()); got != "video-studio" {
			t.Fatalf("request identity = %q, want gateway identity", got)
		}
		if got := productWorkspaceUserID(r.Context()); got != "default" {
			t.Fatalf("product workspace owner = %q, want default", got)
		}
		if got := GetUserIDFromContext(productWorkspaceContext(r.Context())); got != "default" {
			t.Fatalf("forwarded workspace owner = %q, want default", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/agent-profiles/video-studio/query", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}

func TestSingleUserPublicWorkspaceMapsGatewayIdentityToDefaultOwner(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	t.Setenv("DEFAULT_USER_ID", "default")
	t.Setenv("AUTH_SECRET", "test-auth-secret-with-enough-entropy")

	token, err := GenerateJWT("video-studio", "video-studio", "")
	if err != nil {
		t.Fatalf("GenerateJWT failed: %v", err)
	}

	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := publicWorkspaceUserID(r); got != "default" {
			t.Fatalf("public workspace owner = %q, want default", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/public/file?uid=another-user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}

func TestAuthMiddlewareRequiresAuthForPprof(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	t.Setenv("AUTH_SECRET", "test-auth-secret-with-enough-entropy")

	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, path := range []string{"/debug/pprof/", "/debug/pprof/heap?debug=1", "/debug/pprof/goroutine?debug=2"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s status = %d, want %d", path, rr.Code, http.StatusUnauthorized)
		}
	}
}

func TestAuthMiddlewareAcceptsJWTForPprof(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	t.Setenv("AUTH_SECRET", "test-auth-secret-with-enough-entropy")

	token, err := GenerateJWT("default", "user", "")
	if err != nil {
		t.Fatalf("GenerateJWT failed: %v", err)
	}

	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/heap?debug=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}

func TestAuthSecretMustBeExplicitAndNonDefault(t *testing.T) {
	if err := ValidateAuthSecretValue(""); err == nil {
		t.Fatal("ValidateAuthSecretValue accepted an empty secret")
	}
	if err := ValidateAuthSecretValue(deprecatedDefaultAuthSecret); err == nil {
		t.Fatal("ValidateAuthSecretValue accepted the deprecated public default")
	}
	if err := ValidateAuthSecretValue("test-auth-secret-with-enough-entropy"); err != nil {
		t.Fatalf("ValidateAuthSecretValue rejected explicit secret: %v", err)
	}
}

func TestCORSMiddlewareAllowsLoopbackOrigin(t *testing.T) {
	api := &StreamingAPI{config: ServerConfig{CORSOrigins: []string{"loopback"}}}
	handler := api.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/terminals", nil)
	req.Header.Set("Origin", "http://127.0.0.1:51734")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:51734" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want loopback origin", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want true", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPatch) {
		t.Fatalf("Access-Control-Allow-Methods = %q, want PATCH", got)
	}
}

func TestCORSMiddlewareRejectsNonLoopbackOrigin(t *testing.T) {
	api := &StreamingAPI{config: ServerConfig{CORSOrigins: []string{"loopback"}}}
	handler := api.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/terminals", nil)
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}
