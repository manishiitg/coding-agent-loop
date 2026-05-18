package server

import (
	"os"
	"strings"
	"testing"
)

// requireOpenAIValidateKeyE2E gates on the same env vars as the
// adapter-level e2e suite so one secret-set unlocks both.
func requireOpenAIValidateKeyE2E(t *testing.T) string {
	t.Helper()
	if os.Getenv("RUN_OPENAI_REAL_E2E") == "" {
		t.Skip("set RUN_OPENAI_REAL_E2E=1 to run real OpenAI validate-key tests")
	}
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		t.Skip("set OPENAI_API_KEY to run real OpenAI validate-key tests")
	}
	return key
}

// TestValidateAPIKeyOpenAIRealAccepts proves a real OPENAI_API_KEY
// rides the full server → provider dispatch → adapter → live API
// path and comes back valid.
func TestValidateAPIKeyOpenAIRealAccepts(t *testing.T) {
	key := requireOpenAIValidateKeyE2E(t)

	resp, raw := postValidateKey(t, "openai", key)
	if !resp.Valid {
		t.Fatalf("expected valid=true for real OPENAI_API_KEY; got valid=%v error=%q message=%q\nraw=%s",
			resp.Valid, resp.Error, resp.Message, raw)
	}
}

// TestValidateAPIKeyOpenAIRealRejectsBadKey asserts auth failures are
// classified (not silent successes, not 500s) and the bogus key does
// NOT leak into the user-facing error.
func TestValidateAPIKeyOpenAIRealRejectsBadKey(t *testing.T) {
	if os.Getenv("RUN_OPENAI_REAL_E2E") == "" {
		t.Skip("set RUN_OPENAI_REAL_E2E=1 to run real OpenAI validate-key tests")
	}

	const badKey = "sk-DELIBERATELY-INVALID-FOR-AUTH-CLASSIFICATION-TEST"
	resp, raw := postValidateKey(t, "openai", badKey)
	if resp.Valid {
		t.Fatalf("expected valid=false for bogus key; got valid=true\nraw=%s", raw)
	}
	if strings.Contains(resp.Error, badKey) || strings.Contains(resp.Message, badKey) {
		t.Fatalf("user-facing message echoed the bogus key back; this would leak real keys when a user pastes one with a typo.\nerror=%q\nmessage=%q",
			resp.Error, resp.Message)
	}
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
