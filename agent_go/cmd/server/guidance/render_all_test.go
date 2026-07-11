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
		"progressive evidence scan",
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
		"Needs your decision",
		"Assumptions challenged",
		"Today's outcome",
		`<details class="technical">`,
		"Agent log",
		`#pulse-agent-handoff`,
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
	for _, want := range []string{`id="pulse-bug-verdict"`, `id="pulse-goal-verdict"`, `class="as"`, `class="assumptions"`, `class="technical"`, `class="agentlog"`, `id="pulse-agent-handoff"`, `Today's outcome`} {
		if !strings.Contains(skeleton, want) {
			t.Fatalf("review-improve-log-skeleton missing stable verdict markup %q", want)
		}
	}
	if !strings.Contains(skeleton, `href="improve-archive/YYYY-MM.html"`) {
		t.Fatal("review-improve-log-skeleton missing archive link example")
	}
}

func TestImprovementAndPlanGuidanceLoadsAssumptionAudit(t *testing.T) {
	for _, kind := range []string{
		"design-plan",
		"review-plan",
		"review-code",
		"review-artifact-drift",
		"improve-workflow",
		"goal-advisor",
		"improve-evaluation",
		"improve-report",
		"improve-knowledge",
		"improve-learnings",
		"improve-database",
	} {
		rendered, err := renderFromRegistry(kind, tmplData{}, allKinds)
		if err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		if !strings.Contains(rendered, `get_reference_doc(kind="assumption-audit")`) {
			t.Fatalf("%s guidance does not load assumption-audit", kind)
		}
	}

	audit, err := renderFromRegistry("assumption-audit", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render assumption-audit: %v", err)
	}
	for _, want := range []string{
		"Explicit user constraint",
		"Verified external constraint",
		"Current design choice",
		"Agent-inferred assumption",
		"Assumptions challenged",
		"Do not turn targeted maintenance into a full audit",
	} {
		if !strings.Contains(audit, want) {
			t.Fatalf("assumption-audit missing %q", want)
		}
	}
}

func TestGoalAdvisorTreatsCleanAbstentionAsStrategyEvidence(t *testing.T) {
	rendered, err := renderFromRegistry("goal-advisor", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render goal-advisor: %v", err)
	}
	for _, want := range []string{
		"A green answer to the first question must not mask",
		"broader criteria within explicit user boundaries",
		"Never recommend violating an explicit user exclusion",
		"opportunity supply or conversion",
		"Do not require every producing step to be clean before reviewing strategy",
		"Pulse can run Harden and Goal Advisor in the same cycle",
		"include an alternative growth path",
		"Check optimization headroom even when every success criterion is currently",
		"Treat a numeric target as a floor",
		"preserve the successful baseline and propose a bounded",
		"PHASE 1B - ACTIVE EXPERIMENT LIFECYCLE",
		"Exactly one experiment may be active for a workflow",
		"Apply a 10x counterfactual as a thinking lens, not a promise",
		`class="entry decision major advisor-experiment"`,
		"Current strategy ceiling",
		"PHASE 1A - MEASUREMENT DESIGN",
		"one normal `regular`",
		"timestamped, group/run-scoped rows",
		"Measurement plan",
		"Rollback condition",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("goal-advisor guidance missing %q:\n%s", want, rendered)
		}
	}
}

func TestGoalAdvisorMetricsFlowUsesPlanAndReportHandoff(t *testing.T) {
	advisor, err := renderFromRegistry("goal-advisor", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render goal-advisor: %v", err)
	}
	for _, want := range []string{
		"does not revive a generic metrics subsystem",
		"decision it informs",
		"normal `regular` measurement step",
		"db/db.sqlite",
		"Report Health as due after the first trustworthy rows exist",
	} {
		if !strings.Contains(advisor, want) {
			t.Fatalf("goal-advisor measurement guidance missing %q", want)
		}
	}

	report, err := renderFromRegistry("improve-report", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render improve-report: %v", err)
	}
	for _, want := range []string{
		"GOAL ADVISOR MEASUREMENT HANDOFF",
		"An unapproved metric proposal is not report data",
		"window.report.query",
		"not measured yet",
		"Report Health must not create workflow steps itself",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("improve-report measurement handoff missing %q", want)
		}
	}
}
