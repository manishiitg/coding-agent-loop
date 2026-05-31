package server

import (
	"context"
	"testing"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	agycliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/agycli"
)

// TestTerminalStatusLineHTTPCapturesRealAgyTelemetry drives the shared statusline
// HTTP e2e harness against a real agy-cli run. See runStatusLineHTTPE2E.
// Gated on RUN_AGY_CLI_REAL_E2E=1 + the agy binary in PATH.
func TestTerminalStatusLineHTTPCapturesRealAgyTelemetry(t *testing.T) {
	runStatusLineHTTPE2E(t, statusLineE2ECase{
		name:    "agy",
		envGate: "RUN_AGY_CLI_REAL_E2E",
		binary:  "agy",
		cleanup: func() { _ = agycliadapter.CleanupAgyCLIInteractiveSessions(context.Background()) },
		run: func(ctx context.Context, sessionID string) (*llmtypes.StatusLine, error) {
			model := statusLineModelOr("AGY_CLI_REAL_E2E_MODEL", "agy-cli")
			a := agycliadapter.NewAgyCLIAdapter("", model, &e2eMockLogger{})
			if _, err := a.GenerateContent(ctx, statusLineOKPrompt(), agycliadapter.WithInteractiveSessionID(sessionID)); err != nil {
				return nil, err
			}
			return a.GetStatusLine(ctx, sessionID)
		},
	})
}
