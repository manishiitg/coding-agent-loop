package server

import "testing"

func TestClaudePromptDraftFromTerminalContentIgnoresContextualSuggestion(t *testing.T) {
	content := `
⏺ Workflow finished.

─────────────────────────────────────────────────────────────────── mcp-agent ──
❯ show me what it found
────────────────────────────────────────────────────────────────────────────────
  ⏵⏵ don't ask on (shift+tab to cycle)
`
	if draft, ok := claudePromptDraftFromTerminalContent(content); ok {
		t.Fatalf("claudePromptDraftFromTerminalContent = (%q, true), want no active draft for Claude suggestion", draft)
	}
}

func TestClaudePromptDraftFromTerminalContentDetectsUserDraft(t *testing.T) {
	content := `
⏺ Done

─────────────────────────────────────────────────────────────────── mcp-agent ──
❯ continue from the saved workflow result
────────────────────────────────────────────────────────────────────────────────
  ⏵⏵ don't ask on (shift+tab to cycle)
`
	draft, ok := claudePromptDraftFromTerminalContent(content)
	if !ok {
		t.Fatal("claudePromptDraftFromTerminalContent ok = false, want true for user draft")
	}
	if draft != "continue from the saved workflow result" {
		t.Fatalf("draft = %q, want user draft", draft)
	}
}
