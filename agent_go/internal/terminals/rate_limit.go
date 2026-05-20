package terminals

import (
	"regexp"
	"strings"
)

// rateLimitPatterns matches messages each coding-agent CLI prints into
// its pane when the underlying provider blocks further work due to a
// rate / quota / billing limit. Patterns are intentionally broad — a
// false positive (e.g., the CLI prints a help line mentioning "rate
// limit") is recoverable (user sees a rate_limited badge and clicks
// to expand), while a false negative leaves the pane silently stuck
// with "running" status forever.
//
// Ordering matters: more specific provider-prefixed patterns come
// first so we attribute correctly. Generic fallback patterns at the
// end catch any provider that prints a recognizable phrase.
var rateLimitPatterns = []*regexp.Regexp{
	// Claude Code: prints messages like "5-hour limit reached", "Your
	// usage limit will reset at <time>", "You have run out of credits".
	regexp.MustCompile(`(?i)(5[- ]hour|5h)\s+limit\s+(reached|hit)`),
	regexp.MustCompile(`(?i)usage\s+limit\s+(reached|hit|will\s+reset)`),
	regexp.MustCompile(`(?i)you\s+have\s+(reached|hit)\s+your\s+(message|usage|rate)\s+limit`),
	regexp.MustCompile(`(?i)you'?ve?\s+run\s+out\s+of\s+credits`),

	// Codex CLI / OpenAI: prints messages with HTTP 429 hints, "rate
	// limit exceeded", "quota exceeded", "tokens per minute".
	regexp.MustCompile(`(?i)rate[- ]limit(ed|\s+(exceeded|hit|reached))`),
	regexp.MustCompile(`(?i)quota\s+(exceeded|exhausted)`),
	regexp.MustCompile(`(?i)429\s+too\s+many\s+requests`),
	regexp.MustCompile(`(?i)tokens?\s+per\s+minute\b.*\b(exceeded|limit)`),

	// Gemini CLI: prints "Resource has been exhausted", "RESOURCE_EXHAUSTED",
	// "Quota exceeded for quota metric".
	regexp.MustCompile(`(?i)RESOURCE_EXHAUSTED`),
	regexp.MustCompile(`(?i)resource\s+has\s+been\s+exhausted`),
	regexp.MustCompile(`(?i)quota\s+exceeded\s+for\s+quota\s+metric`),

	// Cursor CLI: prints "you have reached your usage limit", "monthly
	// limit reached".
	regexp.MustCompile(`(?i)monthly\s+(usage\s+)?limit\s+(reached|hit)`),
	regexp.MustCompile(`(?i)you'?ve?\s+reached\s+your\s+usage\s+limit`),

	// Generic Anthropic / API responses surfaced by structured CLIs.
	regexp.MustCompile(`(?i)anthropic[._]rate[._]limit`),
	regexp.MustCompile(`(?i)overloaded_error`),
}

// detectRateLimit returns true when the rendered pane content contains
// any rate-limit phrase. The caller has already stripped ANSI escapes
// before passing content here.
func detectRateLimit(content string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	for _, pattern := range rateLimitPatterns {
		if pattern.MatchString(content) {
			return true
		}
	}
	return false
}
