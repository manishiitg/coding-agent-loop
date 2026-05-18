package orchestrator

import (
	"testing"

	"github.com/manishiitg/mcpagent/events"
)

// TestEffectiveModelIDFromTokenEventPicksRightKey locks in the
// priority order of the keys we read from GenerationInfo. The CLI
// adapters emit one or more of these per turn; the helper must pick
// the most-specific one available.
func TestEffectiveModelIDFromTokenEventPicksRightKey(t *testing.T) {
	cases := []struct {
		name string
		gi   map[string]interface{}
		want string
	}{
		{
			name: "cost_model_id wins (canonical key)",
			gi: map[string]interface{}{
				"cost_model_id":     "gpt-5.4",
				"claude_code_model": "claude-opus-4-7",
				"cursor_model":      "composer-2.5",
			},
			want: "gpt-5.4",
		},
		{
			name: "claude_code_model when cost_model_id missing",
			gi: map[string]interface{}{
				"claude_code_model": "claude-haiku-4-5",
			},
			want: "claude-haiku-4-5",
		},
		{
			name: "codex_effective_model",
			gi: map[string]interface{}{
				"codex_effective_model": "gpt-5.4",
			},
			want: "gpt-5.4",
		},
		{
			name: "gemini_effective_model (tmux)",
			gi: map[string]interface{}{
				"gemini_effective_model": "gemini-3.1-flash-lite",
			},
			want: "gemini-3.1-flash-lite",
		},
		{
			name: "gemini_model (structured)",
			gi: map[string]interface{}{
				"gemini_model": "gemini-2.5-flash",
			},
			want: "gemini-2.5-flash",
		},
		{
			name: "cursor_model",
			gi: map[string]interface{}{
				"cursor_model": "composer-2.5",
			},
			want: "composer-2.5",
		},
		{
			name: "empty when no known key present",
			gi: map[string]interface{}{
				"unrelated_field": "x",
			},
			want: "",
		},
		{
			name: "empty when GenerationInfo nil",
			gi:   nil,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := &events.TokenUsageEvent{GenerationInfo: tc.gi}
			if got := effectiveModelIDFromTokenEvent(ev); got != tc.want {
				t.Fatalf("effectiveModelIDFromTokenEvent = %q, want %q", got, tc.want)
			}
		})
	}
}
