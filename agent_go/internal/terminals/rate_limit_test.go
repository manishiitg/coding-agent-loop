package terminals

import "testing"

func TestDetectRateLimitClaudeSessionLimit(t *testing.T) {
	limited := []string{
		"  X  You've hit your session limit · resets 1:30pm (Asia/Calcutta)\n     /usage-credits to finish what you're working on.",
		"You've hit your usage limit",
		"5-hour limit reached",
		"Your usage limit will reset at 3pm",
		"You have run out of credits",
		"   /usage-credits to finish what you're working on.",
		"rate limit exceeded",
		"RESOURCE_EXHAUSTED",
		"you have reached your usage limit",
	}
	for _, content := range limited {
		if !DetectRateLimit(content) {
			t.Errorf("expected rate-limit detection for:\n%q", content)
		}
	}

	for _, content := range []string{
		"",
		"Working on the task...",
		"> ",
		"Allocator done; the Opportunity Scanner is running.",
		"Reading the plan.json to apply the scoped fixes.",
	} {
		if DetectRateLimit(content) {
			t.Errorf("false positive on benign pane: %q", content)
		}
	}
}
