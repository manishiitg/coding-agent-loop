package server

import "testing"

// TestGracefulCloseCodingCLITmuxByNameRecognizesEveryRegisteredProvider is
// the session_lifecycle.go counterpart to
// TestIsCodingAgentTmuxSessionNameRecognizesEveryRegisteredProvider: muse-cli
// was added as a 5th coding CLI provider without this function's switch
// being updated, so its tmux sessions fell through to the `default: return
// false` branch and never got a graceful close call at all. Each of the
// underlying Close*InteractiveSessionByTmux calls is safe to run against a
// tmux name with no real session (a no-op fallback to a harmless
// kill-session attempt), so this can run as a fast, non-live unit test.
func TestGracefulCloseCodingCLITmuxByNameRecognizesEveryRegisteredProvider(t *testing.T) {
	cases := []struct {
		provider string
		tmuxName string
	}{
		{"claude-code", "mlp-claude-code-abc123"},
		{"codex-cli", "mlp-codex-cli-abc123"},
		{"cursor-cli", "mlp-cursor-cli-abc123"},
		{"pi-cli", "mlp-pi-cli-abc123"},
		{"muse-cli", "mlp-muse-abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			if ok := gracefulCloseCodingCLITmuxByName(tc.tmuxName, "test"); !ok {
				t.Errorf("gracefulCloseCodingCLITmuxByName(%q, ...) = false, want true (matched) for registered provider %s", tc.tmuxName, tc.provider)
			}
		})
	}
	if ok := gracefulCloseCodingCLITmuxByName("some-unrelated-tmux-session", "test"); ok {
		t.Error("expected an unrelated tmux session name to not match any provider")
	}
	if ok := gracefulCloseCodingCLITmuxByName("", "test"); ok {
		t.Error("expected an empty tmux session name to not match any provider")
	}
}
