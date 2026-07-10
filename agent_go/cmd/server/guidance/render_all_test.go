package guidance

import (
	"strings"
	"testing"
)

// TestAllGuidanceTemplatesRender renders every template in both registries with
// empty caller context. A template that references a tmplData field that does
// not exist (or has a malformed action) only fails at execute time, which
// previously panicked at materialize time in production (buildMegaSkill). This
// guards that whole class of bug at test time.
func TestAllGuidanceTemplatesRender(t *testing.T) {
	for kind := range allKinds {
		if _, err := renderFromRegistry(kind, tmplData{}, allKinds); err != nil {
			t.Errorf("allKinds/%s failed to render: %v", kind, err)
		}
	}
	for kind := range referenceKinds {
		if _, err := renderFromRegistry(kind, tmplData{}, referenceKinds); err != nil {
			t.Errorf("referenceKinds/%s failed to render: %v", kind, err)
		}
	}
}

func TestPulseGuidanceRequiresAuthoritativeHTMLAndVisibleFreshness(t *testing.T) {
	postRun, err := renderFromRegistry("post-run-monitor", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render post-run-monitor: %v", err)
	}
	for _, want := range []string{
		"builder/improve.html` is the authoritative durable source",
		"only the current machine-readable Gate/worklist/result cache",
		"not measured this run · last measured",
		"Every skipped module must set at least one concrete next-check condition",
		"what new evidence caused the override",
		"progressive evidence triage",
		"one ordered finalizer turn",
		"mark_pulse_final_command_result",
		"not automatically due every Pulse",
	} {
		if !strings.Contains(postRun, want) {
			t.Fatalf("post-run-monitor missing %q", want)
		}
	}

	reviewLog, err := renderFromRegistry("review-improve-log", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render review-improve-log: %v", err)
	}
	if !strings.Contains(reviewLog, "Every `.briefitem`, `.crit`, and important `.tile` needs a visible freshness label") {
		t.Fatal("review-improve-log missing visible freshness contract")
	}
	for _, want := range []string{
		"scheduler conditionally sends a dedicated archive turn",
		"newest **20** timeline cards",
		"Stage complete active and archive HTML documents",
		`href="improve-archive/YYYY-MM.html"`,
	} {
		if !strings.Contains(reviewLog, want) {
			t.Fatalf("review-improve-log missing archive contract %q", want)
		}
	}

	skeleton, err := renderFromRegistry("review-improve-log-skeleton", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render review-improve-log-skeleton: %v", err)
	}
	if !strings.Contains(skeleton, `class="asof"`) || !strings.Contains(skeleton, ".tile .asof") {
		t.Fatal("review-improve-log-skeleton missing visible tile freshness markup")
	}
	for _, want := range []string{`id="pulse-bug-verdict"`, `id="pulse-goal-verdict"`, `class="as"`} {
		if !strings.Contains(skeleton, want) {
			t.Fatalf("review-improve-log-skeleton missing stable verdict markup %q", want)
		}
	}
	if !strings.Contains(skeleton, `href="improve-archive/YYYY-MM.html"`) {
		t.Fatal("review-improve-log-skeleton missing archive link example")
	}
}
