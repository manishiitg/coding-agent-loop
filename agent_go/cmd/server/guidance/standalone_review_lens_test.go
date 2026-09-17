package guidance

import (
	"strings"
	"testing"
)

// These five kinds are review LENSES: their own template text explicitly
// says findings are recorded by a parent turn after loading them alongside
// sibling lenses inside ops-review's Technical Review. ops-review reaches
// them only through materialize.go's read_skill bundle, never through
// get_workflow_command_guidance — so any call reaching that tool for one of
// these kinds is a genuine standalone invocation with no parent turn to
// record on its behalf. Without appendStandaloneReviewLensNotice, findings
// generated exactly as instructed would be silently discarded when the turn
// ends.
func TestAppendStandaloneReviewLensNoticeCoversEveryOrphanableLens(t *testing.T) {
	for _, kind := range []string{"improve-report", "improve-knowledge", "improve-database", "improve-learnings", "improve-evaluation"} {
		if !standaloneReviewLensKinds[kind] {
			t.Errorf("%s is a read-only Engineering Review lens but is missing from standaloneReviewLensKinds", kind)
		}
		got := appendStandaloneReviewLensNotice(kind, "base guidance text")
		if !strings.Contains(got, "STANDALONE MODE") {
			t.Errorf("appendStandaloneReviewLensNotice(%q, ...) missing the standalone recording notice", kind)
		}
		if !strings.Contains(got, "record_pulse_result") || !strings.Contains(got, "focuses=") {
			t.Errorf("appendStandaloneReviewLensNotice(%q, ...) does not tell the agent which typed Pulse tools to call", kind)
		}
		if !strings.HasPrefix(got, "base guidance text") {
			t.Errorf("appendStandaloneReviewLensNotice(%q, ...) must append, not replace, the rendered guidance", kind)
		}
	}
}

// Kinds that either apply changes directly (the diff is the record) or
// already own a dedicated Pulse-recording pipeline (goal-advisor) must not
// get this notice — it would be noise, or in ops-review's own kind
// (reached only via materialize, but defensively checked here too) a
// duplicate-recording risk if this ever became reachable through the tool.
func TestAppendStandaloneReviewLensNoticeLeavesOtherKindsUnchanged(t *testing.T) {
	for _, kind := range []string{"design-plan", "review-plan", "review-code", "review-artifact-drift", "ops-review", "strategy-auditor", "define-success", "pulse", "pulse-setup", "pulse-fixer", "goal-advisor"} {
		got := appendStandaloneReviewLensNotice(kind, "base guidance text")
		if got != "base guidance text" {
			t.Errorf("appendStandaloneReviewLensNotice(%q, ...) unexpectedly modified the text: %q", kind, got)
		}
	}
}
