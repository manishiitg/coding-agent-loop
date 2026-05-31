package server

import (
	"context"
	"testing"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	claudecodeadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/claudecode"
)

// TestTerminalStatusLineHTTPCapturesRealClaudeCodeTelemetry drives the shared
// statusline HTTP e2e harness against a real claude-code run. It additionally
// proves claude-code surfaces the real model (model.display_name) and cost.
// Gated on RUN_CLAUDE_CODE_EXPERIMENTAL_LIVE_E2E=1 + the claude binary in PATH.
func TestTerminalStatusLineHTTPCapturesRealClaudeCodeTelemetry(t *testing.T) {
	runStatusLineHTTPE2E(t, statusLineE2ECase{
		name:    "claude",
		envGate: "RUN_CLAUDE_CODE_EXPERIMENTAL_LIVE_E2E",
		binary:  "claude",
		cleanup: func() { _ = claudecodeadapter.CleanupClaudeCodeTmuxSessions(context.Background()) },
		run: func(ctx context.Context, sessionID string) (*llmtypes.StatusLine, error) {
			model := statusLineModelOr("CLAUDE_CODE_EXPERIMENTAL_MODEL", "claude-haiku-4-5-20251001")
			a := claudecodeadapter.NewClaudeCodeInteractiveAdapter(model, &e2eMockLogger{})
			if _, err := a.GenerateContent(ctx, statusLineOKPrompt(), claudecodeadapter.WithInteractiveSessionID(sessionID)); err != nil {
				return nil, err
			}
			return a.GetStatusLine(ctx, sessionID)
		},
	})
}
