package common

import "testing"

func TestIsCLIProvider(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"claude-code", true},
		{"gemini-cli", true},
		{"codex-cli", true},
		{"cursor-cli", true},
		{"kimi", false},
		{"KIMI", false},
		{" kimi ", false},
		{"openai", false},
		{"vertex", false},
		{"anthropic", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsCLIProvider(c.in); got != c.want {
			t.Errorf("IsCLIProvider(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
