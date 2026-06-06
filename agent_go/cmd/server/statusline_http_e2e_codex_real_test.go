package server

import (
	"context"
	"testing"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	codexcliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/codexcli"
)

// TestTerminalStatusLineHTTPCapturesRealCodexTelemetry drives the shared
// statusline HTTP e2e harness against a real codex-cli run. Beyond the shared
// telemetry assertions, it proves codex surfaces plan rate-limit usage as
// generic statusline extras (status_meta.status_extras), parsed from the
// rollout's token_count rate_limits and rendered provider-agnostically.
// Gated on RUN_CODEX_CLI_REAL_E2E=1 + the codex binary in PATH.
func TestTerminalStatusLineHTTPCapturesRealCodexTelemetry(t *testing.T) {
	runStatusLineHTTPE2E(t, statusLineE2ECase{
		name:             "codex",
		envGate:          "RUN_CODEX_CLI_REAL_E2E",
		binary:           "codex",
		wantStatusExtras: true,
		cleanup:          func() { _ = codexcliadapter.CleanupCodexCLIInteractiveSessions(context.Background()) },
		run: func(ctx context.Context, sessionID string) (*llmtypes.StatusLine, error) {
			model := statusLineModelOr("CODEX_REAL_E2E_MODEL", "gpt-5.5")
			a := codexcliadapter.NewCodexCLIAdapter("", model, &e2eMockLogger{})
			if _, err := a.GenerateContent(ctx, statusLineOKPrompt(), codexcliadapter.WithInteractiveSessionID(sessionID)); err != nil {
				return nil, err
			}
			return a.GetStatusLine(ctx, sessionID)
		},
	})
}
