package server

import (
	"net/http"
	"net/http/httptest"
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
