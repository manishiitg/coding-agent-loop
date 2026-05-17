package testing

import stdtesting "testing"

func TestDefaultCodingAgentE2EModelIncludesCursorCLI(st *stdtesting.T) {
	tests := map[string]string{
		"gemini-cli":  "gemini-3.1-flash-lite",
		"codex-cli":   "gpt-5.3-codex-spark",
		"cursor-cli":  "cursor-cli",
		"claude-code": "claude-haiku-4.5",
	}

	for provider, want := range tests {
		st.Run(provider, func(st *stdtesting.T) {
			if got := defaultCodingAgentE2EModel(provider); got != want {
				st.Fatalf("defaultCodingAgentE2EModel(%q) = %q, want %q", provider, got, want)
			}
		})
	}
}
