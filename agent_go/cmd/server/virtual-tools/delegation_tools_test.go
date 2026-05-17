package virtualtools

import (
	"strings"
	"testing"
)

func TestBuildCLIToolEnvironmentPromptUsesProviderSpecificBridgeToolNames(t *testing.T) {
	tests := []struct {
		provider string
		want     string
		forbid   string
	}{
		{
			provider: "claude-code",
			want:     "mcp__api-bridge__execute_shell_command",
			forbid:   "mcp_api-bridge_execute_shell_command",
		},
		{
			provider: "codex-cli",
			want:     "mcp_api-bridge_execute_shell_command",
			forbid:   "mcp__api-bridge__execute_shell_command",
		},
		{
			provider: "gemini-cli",
			want:     "mcp_api-bridge_execute_shell_command",
			forbid:   "mcp__api-bridge__execute_shell_command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := BuildCLIToolEnvironmentPrompt(tt.provider)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("prompt missing provider bridge tool %q:\n%s", tt.want, got)
			}
			if strings.Contains(got, tt.forbid) {
				t.Fatalf("prompt contains wrong provider bridge tool %q:\n%s", tt.forbid, got)
			}
		})
	}
}
