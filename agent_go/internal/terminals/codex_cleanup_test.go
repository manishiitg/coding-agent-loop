package terminals

import "strings"

import "testing"

func TestCleanCompletedCodexContent(t *testing.T) {
	// Real content lines (one carries an ANSI color code) interleaved with codex
	// TUI chrome: a pure box border, the footer hint bar, and a repeated frame.
	content := strings.Join([]string{
		"\x1b[32mHere is the answer:\x1b[0m",
		"╭──────────────────────────╮",
		"The result is 42.",
		"The result is 42.", // duplicate repaint frame
		"│ type your message here   │",
		"  esc to interrupt  ctrl+o expand",
		"Done.",
	}, "\n")

	got := CleanCompletedCodexContent(content)

	if !strings.Contains(got, "\x1b[32mHere is the answer:\x1b[0m") {
		t.Errorf("colored answer line should be kept verbatim (ANSI intact); got:\n%s", got)
	}
	if !strings.Contains(got, "The result is 42.") || !strings.Contains(got, "Done.") {
		t.Errorf("answer content dropped; got:\n%s", got)
	}
	if strings.Count(got, "The result is 42.") != 1 {
		t.Errorf("consecutive duplicate frame not de-duped; got:\n%s", got)
	}
	for _, chrome := range []string{"type your message", "esc to interrupt", "╭──"} {
		if strings.Contains(got, chrome) {
			t.Errorf("chrome %q should be removed; got:\n%s", chrome, got)
		}
	}
}

func TestCleanCompletedCodexContentGuardFallback(t *testing.T) {
	// Almost entirely chrome -> cleanup would gut it -> return original untouched.
	allChrome := strings.Join([]string{
		"╭──────────────╮",
		"│ type your message │",
		"esc to interrupt",
		"openai codex",
	}, "\n")
	if got := CleanCompletedCodexContent(allChrome); got != allChrome {
		t.Errorf("guard should return original when cleanup g, got:\n%s", got)
	}
}

func TestSnapshotIsCodex(t *testing.T) {
	if !SnapshotIsCodex(Snapshot{Status: Status{ProviderLabel: "Codex CLI"}}) {
		t.Error("should detect codex via provider label")
	}
	if !SnapshotIsCodex(Snapshot{Content: "  OpenAI Codex  "}) {
		t.Error("should detect codex via content banner")
	}
	if SnapshotIsCodex(Snapshot{Status: Status{ProviderLabel: "Claude Code"}, Content: "hello"}) {
		t.Error("should not flag a non-codex pane")
	}
}
