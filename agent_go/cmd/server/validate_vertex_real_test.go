package server

import (
	"os"
	"strings"
	"testing"
)

// requireVertexValidateKeyE2E gates on the same env vars as the
// adapter-level e2e suite. Accepts the canonical AI Studio name
// (GEMINI_API_KEY) plus the legacy aliases.
func requireVertexValidateKeyE2E(t *testing.T) string {
	t.Helper()
	if os.Getenv("RUN_VERTEX_REAL_E2E") == "" {
		t.Skip("set RUN_VERTEX_REAL_E2E=1 to run real Vertex validate-key tests")
	}
	for _, name := range []string{"GEMINI_API_KEY", "VERTEX_API_KEY", "GOOGLE_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	t.Skip("set GEMINI_API_KEY (or VERTEX_API_KEY / GOOGLE_API_KEY) to run real Vertex validate-key tests")
	return ""
}

// TestValidateAPIKeyVertexRealAccepts proves an AI Studio key rides
// the full server → provider dispatch → adapter → live Gemini API
// path and comes back valid.
func TestValidateAPIKeyVertexRealAccepts(t *testing.T) {
	key := requireVertexValidateKeyE2E(t)

	resp, raw := postValidateKey(t, "vertex", key)
	if !resp.Valid {
		t.Fatalf("expected valid=true for real Vertex/Gemini API key; got valid=%v error=%q message=%q\nraw=%s",
			resp.Valid, resp.Error, resp.Message, raw)
	}
}

// TestValidateAPIKeyVertexRealRejectsBadKey asserts auth failures are
// classified and the bogus key does NOT leak into the user-facing
// error.
func TestValidateAPIKeyVertexRealRejectsBadKey(t *testing.T) {
	if os.Getenv("RUN_VERTEX_REAL_E2E") == "" {
		t.Skip("set RUN_VERTEX_REAL_E2E=1 to run real Vertex validate-key tests")
	}

	const badKey = "AIza-DELIBERATELY-INVALID-FOR-AUTH-CLASSIFICATION-TEST"
	resp, raw := postValidateKey(t, "vertex", badKey)
	if resp.Valid {
		t.Fatalf("expected valid=false for bogus key; got valid=true\nraw=%s", raw)
	}
	if strings.Contains(resp.Error, badKey) || strings.Contains(resp.Message, badKey) {
		t.Fatalf("user-facing message echoed the bogus key back; this would leak real keys when a user pastes one with a typo.\nerror=%q\nmessage=%q",
			resp.Error, resp.Message)
	}
	combined := strings.ToLower(resp.Error + " " + resp.Message)
	hint := false
	for _, marker := range []string{"auth", "key", "invalid", "credential", "unauthorized", "401", "403", "permission", "api key"} {
		if strings.Contains(combined, marker) {
			hint = true
			break
		}
	}
	if !hint {
		t.Fatalf("validation failure should hint at auth/credential trouble; got error=%q message=%q",
			resp.Error, resp.Message)
	}
}
