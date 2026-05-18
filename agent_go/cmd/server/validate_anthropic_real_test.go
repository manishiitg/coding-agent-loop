package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// requireAnthropicValidateKeyE2E gates the validate-key real-API tests
// on the same env vars as the adapter-level e2e suite, so a single
// secret-set unlocks both. Tests skip cleanly when either is missing.
func requireAnthropicValidateKeyE2E(t *testing.T) string {
	t.Helper()
	if os.Getenv("RUN_ANTHROPIC_REAL_E2E") == "" {
		t.Skip("set RUN_ANTHROPIC_REAL_E2E=1 to run real Anthropic validate-key tests")
	}
	key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if key == "" {
		t.Skip("set ANTHROPIC_API_KEY to run real Anthropic validate-key tests")
	}
	return key
}

type validateKeyResponse struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// postValidateKey drives the /api/llm-config/validate-key handler the
// same way the frontend would. Returns the decoded response and the
// raw body for diagnostic logging when an assertion fails.
func postValidateKey(t *testing.T, provider, apiKey string) (validateKeyResponse, string) {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"provider": provider,
		"api_key":  apiKey,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm-config/validate-key", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	api := &StreamingAPI{}
	api.handleValidateAPIKey(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("validate-key returned status %d: %s", rec.Code, rec.Body.String())
	}
	var decoded validateKeyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v\nbody=%s", err, rec.Body.String())
	}
	return decoded, rec.Body.String()
}

// TestValidateAPIKeyAnthropicRealAccepts: a real ANTHROPIC_API_KEY hits
// the validate-key endpoint and comes back valid. This proves:
//   - the server-side key resolution path forwards the body's api_key
//     into the validator (rather than silently substituting the env
//     var, which would mask "the user just pasted a bad key" errors)
//   - validateProviderConfig routes "anthropic" through llm.ValidateAPIKey
//   - the underlying Anthropic adapter accepts the request shape
func TestValidateAPIKeyAnthropicRealAccepts(t *testing.T) {
	key := requireAnthropicValidateKeyE2E(t)

	resp, raw := postValidateKey(t, "anthropic", key)
	if !resp.Valid {
		t.Fatalf("expected valid=true for real ANTHROPIC_API_KEY; got valid=%v error=%q message=%q\nraw=%s",
			resp.Valid, resp.Error, resp.Message, raw)
	}
}

// TestValidateAPIKeyAnthropicRealRejectsBadKey proves the auth-failure
// path classifies a clearly-invalid key as invalid (not a 500, not a
// success), and that the bad key's literal value does NOT leak into
// the user-facing error message. The latter matters because the
// frontend echoes the error verbatim in the "Test Connection" toast —
// a key in that toast would make screen-share debugging unsafe.
func TestValidateAPIKeyAnthropicRealRejectsBadKey(t *testing.T) {
	// Gating on RUN_ANTHROPIC_REAL_E2E even though no real key is needed
	// for THIS subtest: keep the test set toggleable as a single unit
	// so a developer running with the env flag on gets the full picture.
	if os.Getenv("RUN_ANTHROPIC_REAL_E2E") == "" {
		t.Skip("set RUN_ANTHROPIC_REAL_E2E=1 to run real Anthropic validate-key tests")
	}

	const badKey = "sk-ant-DELIBERATELY-INVALID-FOR-AUTH-CLASSIFICATION-TEST"
	resp, raw := postValidateKey(t, "anthropic", badKey)
	if resp.Valid {
		t.Fatalf("expected valid=false for bogus key; got valid=true\nraw=%s", raw)
	}
	if strings.Contains(resp.Error, badKey) || strings.Contains(resp.Message, badKey) {
		t.Fatalf("user-facing message echoed the bogus key back; this would leak real keys when a user pastes one with a typo.\nerror=%q\nmessage=%q",
			resp.Error, resp.Message)
	}
	// The error / message must mention something auth-related so the
	// user understands what's wrong. Any of these is acceptable.
	combined := strings.ToLower(resp.Error + " " + resp.Message)
	hint := false
	for _, marker := range []string{"auth", "key", "invalid", "credential", "unauthorized", "401"} {
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
