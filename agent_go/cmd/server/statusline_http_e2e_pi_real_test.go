package server

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	picliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/picli"
)

// TestTerminalStatusLineHTTPCapturesRealPiTelemetry drives the shared
// statusline HTTP e2e harness against a real pi-cli run. It proves Pi's
// estimated statusline telemetry reaches the same terminal API consumed by the
// frontend.
// Gated on RUN_PI_CLI_BUILDER_E2E=1 + the pi binary in PATH.
func TestTerminalStatusLineHTTPCapturesRealPiTelemetry(t *testing.T) {
	runStatusLineHTTPE2E(t, statusLineE2ECase{
		name:    "pi",
		envGate: "RUN_PI_CLI_BUILDER_E2E",
		binary:  "pi",
		cleanup: func() { _ = picliadapter.CleanupPiCLIInteractiveSessions(context.Background()) },
		run: func(ctx context.Context, sessionID string) (*llmtypes.StatusLine, error) {
			model := statusLineModelOr("PI_CLI_REAL_E2E_MODEL", "google/gemini-3.5-flash")
			apiKey := strings.TrimSpace(os.Getenv("PI_API_KEY"))
			if apiKey == "" {
				apiKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
			}
			if apiKey == "" {
				apiKey = strings.TrimSpace(os.Getenv("GOOGLE_API_KEY"))
			}
			a := picliadapter.NewPiCLIAdapter(apiKey, model, &e2eMockLogger{})
			if _, err := a.GenerateContent(ctx, statusLineOKPrompt(),
				picliadapter.WithInteractiveSessionID(sessionID),
				picliadapter.WithPersistentInteractiveSession(true),
			); err != nil {
				return nil, err
			}
			return a.GetStatusLine(ctx, sessionID)
		},
	})
}
