package main

import (
	"regexp"
	"strings"
)

// reply_sanitize.go is a narrow, clearly-temporary stopgap for a known
// upstream bug in multi-llm-provider-go's codex-cli interactive adapter: long,
// deeply nested paths (this app's own <Subject>/<Topic>/<slug>/ activity
// paths can be long) sometimes make the tmux pane word-wrap a tool-call
// narration line ("Called api-bridge.foo(...)") across multiple physical
// lines with a "└" continuation marker — and the adapter's own single-line
// noise-stripper doesn't recognize that wrapped continuation, so raw
// tool-call narration, echoed prompts, and CLI chrome ("You have N usage
// limit resets...") leak straight into the returned reply text.
//
// This is NOT a general terminal-output parser — it just recognizes the
// specific leak markers observed live and truncates the reply at the first
// one, keeping whatever clean prose came before it. Delete this file once the
// real fix lands upstream (teaching multi-llm-provider-go's stripper to treat
// a "Called"/"Calling" line + its wrapped "└ ..." continuation as one block).

var codexUsageLimitLineRE = regexp.MustCompile(`(?i)^you have \d+ usage limit resets? available`)

// isLeakedNarrationLine reports whether a line looks like leaked internal
// transcript rather than part of the model's real reply.
func isLeakedNarrationLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "›") || strings.HasPrefix(trimmed, "└") {
		return true
	}
	lower := strings.ToLower(trimmed)
	if trimmed == "Called" || trimmed == "Calling" || strings.HasPrefix(lower, "called ") || strings.HasPrefix(lower, "calling ") {
		return true
	}
	if codexUsageLimitLineRE.MatchString(trimmed) {
		return true
	}
	if strings.HasPrefix(trimmed, "{") && (strings.Contains(trimmed, `"status"`) || strings.Contains(trimmed, `"opened"`)) {
		return true
	}
	return false
}

// sanitizeAgentReply truncates a reply at the first recognizable leaked-
// narration line, keeping any clean prose that came before it. Returns the
// input unchanged when nothing looks leaked.
func sanitizeAgentReply(text string) string {
	lines := strings.Split(text, "\n")
	cut := -1
	for i, line := range lines {
		if isLeakedNarrationLine(line) {
			cut = i
			break
		}
	}
	if cut == -1 {
		return text
	}
	clean := strings.TrimSpace(strings.Join(lines[:cut], "\n"))
	if clean == "" {
		return "Done."
	}
	return clean
}
