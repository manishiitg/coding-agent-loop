package types

import (
	"os"
	"testing"

	"github.com/joho/godotenv"
)

// TestMain loads agent_go/.env (if present) so the real-LLM e2e tests can pick
// up credentials like GEMINI_API_KEY without the caller exporting them by hand.
//
// godotenv.Load does NOT override variables already set in the process
// environment, so an explicit `GEMINI_API_KEY=... go test` still wins. The e2e
// RUN_* gates (RUN_WORKFLOW_REAL_E2E, RUN_VERTEX_REAL_E2E, ...) are deliberately
// NOT stored in .env, so loading it here does not un-gate the e2e tests — they
// still skip unless those gates are passed explicitly.
func TestMain(m *testing.M) {
	// CWD during `go test` is the package dir; walk up to agent_go/.env.
	for _, p := range []string{".env", "../../../.env", "../../../../.env"} {
		if err := godotenv.Load(p); err == nil {
			break
		}
	}
	os.Exit(m.Run())
}
