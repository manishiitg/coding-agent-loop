package server

import (
	"net/http/httptest"
	"testing"
)

func TestDeriveOAuthRedirectURIUsesRequestHost(t *testing.T) {
	t.Setenv("PUBLIC_URL", "")

	req := httptest.NewRequest("POST", "http://localhost:18743/api/oauth/start", nil)

	got := deriveOAuthRedirectURI(req)
	want := "http://localhost:18743/api/oauth/callback"
	if got != want {
		t.Fatalf("deriveOAuthRedirectURI() = %q, want %q", got, want)
	}
}

func TestDeriveOAuthRedirectURIUsesForwardedHeaders(t *testing.T) {
	t.Setenv("PUBLIC_URL", "")

	req := httptest.NewRequest("POST", "http://internal:8000/api/oauth/start", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "app.example.com")

	got := deriveOAuthRedirectURI(req)
	want := "https://app.example.com/api/oauth/callback"
	if got != want {
		t.Fatalf("deriveOAuthRedirectURI() = %q, want %q", got, want)
	}
}

func TestDeriveOAuthRedirectURIUsesPublicURL(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://public.example.com/")

	req := httptest.NewRequest("POST", "http://localhost:18743/api/oauth/start", nil)

	got := deriveOAuthRedirectURI(req)
	want := "https://public.example.com/api/oauth/callback"
	if got != want {
		t.Fatalf("deriveOAuthRedirectURI() = %q, want %q", got, want)
	}
}
