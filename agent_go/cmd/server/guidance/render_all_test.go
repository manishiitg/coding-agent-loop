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

func TestManualPulseCommandsKeepRunSetupReviewAndFixBoundariesSeparate(t *testing.T) {
	tests := map[string][]string{
		"pulse": {
			"MANUAL ONE-OFF PULSE",
			"must not create, edit, enable",
			"change `post_run_monitor`",
			"record_pulse_worklist",
			"call_generic_agent",
			"only Pulse Fixer",
		},
		"pulse-setup": {
			"Set up recurring workflow runs with dynamic Pulse",
			"update_workflow_config(post_run_monitor=true)",
			"Create or update one normal workshop Run-mode schedule",
		},
		"bug-review": {
			"STANDALONE PULSE BUG REVIEW",
			"without applying fixes",
			"READ-ONLY REVIEW",
			"`/pulse-fixer`",
		},
		"llm-ops-review": {
			"STANDALONE LLM AND OPERATIONS REVIEW",
			"must not edit files or config",
			"material goal criterion is below target",
			"Missing evidence means keep the tier",
			"before `/pulse-fixer` can apply them",
		},
		"pulse-fixer": {
			"STANDALONE PULSE FIXER",
			"does not rerun Pulse Gate or launch review agents",
			"post-change evidence boundary",
			"changed_unverified",
			"awaiting_next_valid_run",
			"standalone command must not impersonate",
		},
	}

	for kind, wants := range tests {
		rendered, err := renderFromRegistry(kind, tmplData{}, allKinds)
		if err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		for _, want := range wants {
			if !strings.Contains(rendered, want) {
				t.Fatalf("%s guidance missing %q", kind, want)
			}
		}
	}

	advisor, err := renderFromRegistry("goal-advisor", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render goal-advisor: %v", err)
	}
	if !strings.Contains(advisor, "a user can also invoke it manually") {
		t.Fatal("goal-advisor must describe its one-off manual path")
	}
	if strings.Contains(advisor, "update_workflow_config(post_run_monitor=true)") {
		t.Fatal("goal-advisor must not configure recurring Pulse")
	}
}

func TestEvaluationPlanGuidanceAcceptsSourceGroundedValidEmptyResults(t *testing.T) {
	guidance, err := renderFromRegistry("evaluation-plan", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render evaluation-plan: %v", err)
	}
	for _, want := range []string{
		"Empty is not automatically missing",
		"source-grounded legitimate zero-cardinality state",
		"fabricated or silently missing data still fails closed",
	} {
		if !strings.Contains(guidance, want) {
			t.Fatalf("evaluation guidance missing %q\n\nGuidance:\n%s", want, guidance)
		}
	}
}

func TestPulseCostGuidanceReconcilesRawLedgersWithoutDoubleCounting(t *testing.T) {
	postRun, err := renderFromRegistry("post-run-monitor", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render post-run-monitor: %v", err)
	}
	reviewCost, err := renderFromRegistry("review-cost", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render review-cost: %v", err)
	}

	for kind, rendered := range map[string]string{
		"post-run-monitor": postRun,
		"review-cost":      reviewCost,
	} {
		for _, want := range []string{
			"group_folder",
			"by_model",
			"authoritative LLM total",
			"by_step_and_model",
			"never add",
			"unattributed/orchestrator",
			"workflow_orchestrator",
			"scripted/zero-LLM step",
			"run-folder",
		} {
			if !strings.Contains(rendered, want) {
				t.Fatalf("%s cost reconciliation guidance missing %q", kind, want)
			}
		}
	}
}

func TestPulseGuidanceTracesStateChangesToRuntimeConsumers(t *testing.T) {
	postRun, err := renderFromRegistry("post-run-monitor", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render post-run-monitor: %v", err)
	}
	// Gate keeps the failure-mode flags visible (via the bug_review pointer) so
	// it can classify a suspect signal; the full reachability method lives in
	// pulse-bug-review, loaded only when bug_review is due.
	for _, want := range []string{
		"wrong_store_write",
		"shadow_store_drift",
		"successful write to a plausible table is",
	} {
		if !strings.Contains(postRun, want) {
			t.Fatalf("post-run monitor missing control-path contract %q", want)
		}
	}
	bugReview, err := renderFromRegistry("pulse-bug-review", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render pulse-bug-review: %v", err)
	}
	for _, want := range []string{
		"control-path reachability check",
		"wrong_store_write",
		"shadow_store_drift",
		"prove which persisted value it consumed",
	} {
		if !strings.Contains(bugReview, want) {
			t.Fatalf("pulse-bug-review missing control-path contract %q", want)
		}
	}

	dbReview, err := renderFromRegistry("improve-database", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render improve-database: %v", err)
	}
	for _, want := range []string{
		"control-state ownership map",
		"source-of-truth collisions",
		"writer -> canonical record -> runtime reader -> decision/output",
		"runtime decision consumed the canonical value",
	} {
		if !strings.Contains(dbReview, want) {
			t.Fatalf("database review missing control-path contract %q", want)
		}
	}

	artifactReview, err := renderFromRegistry("review-artifact-drift", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render review-artifact-drift: %v", err)
	}
	for _, want := range []string{
		"trace the exact changed record to the current runtime",
		"clean changelog/file diff is not enough",
		"plausible but non-canonical store",
	} {
		if !strings.Contains(artifactReview, want) {
			t.Fatalf("artifact review missing control-path contract %q", want)
		}
	}
}

func TestPulseGuidanceRequiresReviewedBaselineBeforeCadenceSkip(t *testing.T) {
	postRun, err := renderFromRegistry("post-run-monitor", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render post-run-monitor: %v", err)
	}
	for _, want := range []string{
		"Reviewed-baseline rule",
		"successful workflow run is evidence for a review; it is not a substitute",
		"completed, evidence-backed",
		"baseline review for that module",
		"**review outcomes**, not run",
		"not count as clean reviews",
		"review's checkpoint forward",
		"bounded adaptive backoff",
		"baseline pending",
		"baseline cannot justify skipping",
	} {
		if !strings.Contains(postRun, want) {
			t.Fatalf("post-run monitor missing reviewed-baseline contract %q", want)
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
		"Parallel Review Team And Single Fixer",
		"parallel batches of at most four reviewers",
		"under 3000 characters",
		"in-turn review ledger",
		".pulse-fixer-recovery",
		"blindly reapply",
		"fixed_verified",
		"normal user-facing cards once",
		"conflict map grouped by target key",
		"explicit user-approved decisions and constraints",
		"mark only the affected",
		"finding-id manifest",
		"every finding id must have",
		"global finding-ID reconciliation",
		"Do not claim",
		"approval revalidation",
		"Unrelated drift",
		"stale_not_applied",
		"Never silently rebase or broaden",
		"post-change evidence boundary",
		"changed_unverified",
		"awaiting_next_valid_run",
		"Do not use `run_in_background`",
		"READ-ONLY REVIEW",
		"same parent Pulse turn",
		"does not launch",
		"`run_goal_advisor_review`",
		"without adding backend coordination",
		"confirm every module marked",
		"Never silently treat a",
		"missing result as skipped or successful",
		"Step concerns are first-class run evidence",
		"`CONCERNS: <brief evidence-backed concern>`",
		"runs/<run_folder>/logs/<step>/execution/execution-final-summary.json",
		"runs/<run_folder>/logs/<step>/execution/execution-attempt-*.json",
		"runs/<run_folder>/execution/<step>/session.json",
		"Never silently drop a concern",
		"Off-track goals tighten Bug Review cadence",
		"below target",
		"declining, or stalled",
		"no exploratory QA checkpoint was completed after the latest observed goal",
		"does not justify a long calendar cooldown",
		"finding-free reviews over unchanged runtime paths may widen",
		"`correctness_bug`",
		"`efficiency_or_coaching`",
		"`insufficient_evidence`",
		"successful step may have chosen the wrong",
		"prior Bug Review recorded `efficiency_or_coaching` trace evidence",
		"Backup risk: local only",
		"no verified destination is off-device",
		"Never describe this state as healthy",
		"warning in every Pulse",
		"notification until off-device protection is verified",
	} {
		if !strings.Contains(postRun, want) {
			t.Fatalf("post-run-monitor missing %q", want)
		}
	}

	// The deep Bug Review mechanics were extracted out of the Gate-loaded
	// post-run-monitor doc into pulse-bug-review, loaded only when bug_review
	// is due. Guard against re-inlining them into the frequent Gate turn.
	for _, moved := range []string{
		"Observable execution-trace review",
		"semantic execution defects",
		"execution/execution-attempt-*-iteration-*-conversation.json",
		"Judge observable decisions and evidence, not hidden chain-of-thought",
		"Route `efficiency_or_coaching` findings",
		"Exploratory QA contract",
	} {
		if strings.Contains(postRun, moved) {
			t.Fatalf("post-run-monitor should not re-inline extracted Bug Review contract %q", moved)
		}
	}
	if !strings.Contains(postRun, `get_reference_doc(kind="pulse-bug-review")`) {
		t.Fatal("post-run-monitor missing pointer to pulse-bug-review")
	}

	// The fix-verification contract is single-sourced: post-run-monitor and
	// pulse-fixer reference it instead of restating the detail. Guard against
	// the detail drifting back into the Gate-loaded post-run-monitor doc.
	if !strings.Contains(postRun, `get_reference_doc(kind="fix-verification")`) {
		t.Fatal("post-run-monitor missing pointer to fix-verification")
	}
	for _, moved := range []string{"baseline only, never proof", "mtime alone"} {
		if strings.Contains(postRun, moved) {
			t.Fatalf("post-run-monitor should reference fix-verification, not restate %q", moved)
		}
	}
	fixVerify, err := renderFromRegistry("fix-verification", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render fix-verification: %v", err)
	}
	for _, want := range []string{
		"post-change evidence boundary",
		"baseline only, never proof",
		"real runtime consumer",
		"a successful write alone is not proof",
		"mtime alone",
		"changed_unverified",
		"awaiting_next_valid_run",
	} {
		if !strings.Contains(fixVerify, want) {
			t.Fatalf("fix-verification missing %q", want)
		}
	}

	bugReview, err := renderFromRegistry("pulse-bug-review", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render pulse-bug-review: %v", err)
	}
	for _, want := range []string{
		"Exploratory QA contract",
		"control-path reachability",
		"wrong_store_write",
		"Observable execution-trace review",
		"semantic execution defects",
		"execution/execution-attempt-*-iteration-*-conversation.json",
		"Judge observable decisions and evidence, not hidden chain-of-thought",
		"`correctness_bug`",
		"`efficiency_or_coaching`",
		"`insufficient_evidence`",
		"Route `efficiency_or_coaching` findings",
		// Weak-validation-gate check: flag a gate that passes on a self-asserted
		// marker without proving the real effect; not every step has a db.
		"self-asserted marker",
		"not every step has a db",
	} {
		if !strings.Contains(bugReview, want) {
			t.Fatalf("pulse-bug-review missing %q", want)
		}
	}

	reviewLog, err := renderFromRegistry("review-improve-log", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render review-improve-log: %v", err)
	}
	if !strings.Contains(reviewLog, "Every important `.briefitem` and `.tile` needs a visible freshness label") {
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
		"Goal — Ikigai",
		"Signals — Kizuki",
		"Reflection — Hansei",
		"Improvements — Kaizen",
		"Do not add a second active-question card",
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
	for _, want := range []string{`data-pulse-schema="2"`, `id="pulse-bug-verdict"`, `id="pulse-goal-verdict"`, `class="as"`, `class="assumptions"`, `class="technical"`, `class="agentlog"`, `id="pulse-agent-handoff"`, `data-pulse-section="signals" data-module="bug_review"`, `data-pulse-section="reflection" data-module="run_summary"`, `data-pulse-section="improvements" data-module="goal_advisor"`, `Today's outcome`} {
		if !strings.Contains(skeleton, want) {
			t.Fatalf("review-improve-log-skeleton missing stable verdict markup %q", want)
		}
	}
	if !strings.Contains(skeleton, `href="improve-archive/YYYY-MM.html"`) {
		t.Fatal("review-improve-log-skeleton missing archive link example")
	}
	if strings.Contains(skeleton, `class="goalcard"`) || strings.Contains(skeleton, `data-status="open"`) {
		t.Fatal("review-improve-log-skeleton must not duplicate the Goal or an active SQLite question")
	}
	for _, want := range []string{`data-pulse-section="reflection" data-module="goal_advisor"`, `data-status="answered"`, `Question + answer`} {
		if !strings.Contains(skeleton, want) {
			t.Fatalf("review-improve-log-skeleton missing historical question/answer contract %q", want)
		}
	}
}

func TestPulseGuidanceRejudgesActiveExperimentCadenceFromCurrentEvidence(t *testing.T) {
	postRun, err := renderFromRegistry("post-run-monitor", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render post-run-monitor: %v", err)
	}
	for _, want := range []string{
		"Every Gate must re-judge current goal evidence",
		"planned evidence boundary, not a lock",
		"reachable in the real runtime control",
		"not received a fair test",
		"implementation/control-path evidence",
		"real business or strategy",
	} {
		if !strings.Contains(postRun, want) {
			t.Fatalf("post-run monitor missing active-experiment cadence contract %q", want)
		}
	}

	advisor, err := renderFromRegistry("goal-advisor", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render goal-advisor: %v", err)
	}
	for _, want := range []string{
		"verify its fair-test state from current evidence",
		"actual runtime control",
		"zero valid outcome-bearing runs is not a fair test",
		"repair, unblock, or revise the same experiment in place",
		"runtime-path",
	} {
		if !strings.Contains(advisor, want) {
			t.Fatalf("goal advisor missing active-experiment lifecycle contract %q", want)
		}
	}
}

func TestTierGuidanceProtectsQualityWhileGoalsAreBelowTarget(t *testing.T) {
	cases := map[string][]string{
		"post-run-monitor": {
			"Goal quality outranks tier savings",
			"material success criterion is",
			"not evidence for a downgrade",
		},
		"llm-selection": {
			"material workflow goal is below target",
			"representative eval/run evidence is at target",
			"Missing evidence means do not downgrade",
		},
		"optimize-playbook": {
			"material goals are below target",
			"proven quality-equivalent outputs",
			"explicitly approved reversible downgrade trial",
		},
	}
	for kind, wants := range cases {
		rendered, err := renderFromRegistry(kind, tmplData{}, referenceKinds)
		if err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		for _, want := range wants {
			if !strings.Contains(rendered, want) {
				t.Fatalf("%s missing goal-quality tier guard %q", kind, want)
			}
		}
	}
}

func TestGoalAdvisorPrioritizesStrategyOverHTMLFormatting(t *testing.T) {
	advisor, err := renderFromRegistry("goal-advisor", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render goal-advisor: %v", err)
	}
	for _, want := range []string{
		"Spend the run on goal evidence, strategy, alternatives, and experiment judgment",
		"Do not audit its CSS, visual design, unrelated historical cards, or overall format",
		"one targeted in-place update",
		"do not perform an HTML design or migration pass",
		"skip the HTML write",
	} {
		if !strings.Contains(advisor, want) {
			t.Fatalf("goal advisor missing analysis-first reporting contract %q", want)
		}
	}
}

func TestPulseRunsEveryDueReviewerAndWritesAttributedResults(t *testing.T) {
	monitor, err := renderFromRegistry("post-run-monitor", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render post-run-monitor: %v", err)
	}
	for _, want := range []string{
		`one reviewer task for **every** due module`,
		`Never rank the due worklist and run only a "top 3"`,
		`one compact dated result card for every due module`,
		`data-pulse-section`,
		`data-module`,
	} {
		if !strings.Contains(monitor, want) {
			t.Fatalf("post-run-monitor missing complete reviewer contract %q", want)
		}
	}

	pulse, err := renderFromRegistry("pulse", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render pulse: %v", err)
	}
	for _, want := range []string{`Never select only a`, `one explicitly attributed result card per due module`} {
		if !strings.Contains(pulse, want) {
			t.Fatalf("manual pulse missing complete reviewer contract %q", want)
		}
	}
	for _, forbidden := range []string{"record_pulse_worklist", "mark_pulse_module_result", "mark_pulse_final_command_result"} {
		if !strings.Contains(pulse, "do not call\n   `"+forbidden+"`") && !strings.Contains(pulse, "`"+forbidden+"`") {
			t.Fatalf("manual pulse does not explicitly fence scheduler-only tool %q", forbidden)
		}
	}

	setup, err := renderFromRegistry("pulse-setup", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render pulse-setup: %v", err)
	}
	for _, want := range []string{
		"evaluation health",
		"LLM/operations review",
		"builder/improve.html",
		"only the current machine-readable scheduler/UI mirror",
	} {
		if !strings.Contains(setup, want) {
			t.Fatalf("pulse-setup missing complete module/source-of-truth contract %q", want)
		}
	}

	for _, kind := range []string{"bug-review", "llm-ops-review"} {
		review, renderErr := renderFromRegistry(kind, tmplData{}, allKinds)
		if renderErr != nil {
			t.Fatalf("render %s: %v", kind, renderErr)
		}
		for _, want := range []string{"review-improve-log", "data-pulse-section", "data-module", "Do not truncate"} {
			if !strings.Contains(review, want) {
				t.Fatalf("%s missing standalone report contract %q", kind, want)
			}
		}
	}
}

func TestPulseRelatedGuidanceUsesFourPartSectionOwnership(t *testing.T) {
	cases := map[string][]string{
		"design-plan":           {`data-pulse-section="signals"`, `data-module="goal_advisor"`, "never discard findings"},
		"review-code":           {`data-pulse-section="signals"`, `data-module="bug_review"`, "every finding"},
		"review-cost":           {`data-pulse-section="signals"`, `data-module="cost_llm_time"`, "every finding"},
		"review-speed":          {`data-pulse-section="signals"`, `data-module="cost_llm_time"`, "every finding"},
		"review-artifact-drift": {`data-pulse-section="signals"`, `data-module="artifact_review"`},
		"improve-evaluation":    {`data-pulse-section="signals"`, `data-module="eval_health"`},
		"define-success":        {`data-pulse-section="reflection"`, `data-module="run_summary"`, "do not copy the Goal"},
		"pulse-setup":           {`data-pulse-section="improvements"`, `data-module="pulse_fixer"`, "do not seed or refresh a Goal/Profile card"},
		"pulse-fixer":           {`data-pulse-section="improvements"`, `data-module="pulse_fixer"`},
		"goal-advisor":          {`data-pulse-section="improvements"`, `data-module="goal_advisor"`, "do not duplicate the pending question"},
	}

	for kind, wants := range cases {
		rendered, err := renderFromRegistry(kind, tmplData{}, allKinds)
		if err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		for _, want := range wants {
			if !strings.Contains(rendered, want) {
				t.Fatalf("%s missing four-part Pulse contract %q", kind, want)
			}
		}
	}
}

func TestMaintenanceImproveGuidanceIsReadOnlyForPulseFixerHandoff(t *testing.T) {
	cases := map[string][]string{
		"review-artifact-drift": {
			"read-only audit checklist",
			"call_generic_agent",
			"Never launch another reviewer",
			"Pulse Fixer",
			"mark_changelog_artifact_reviewed",
		},
		"improve-learnings": {
			"READ-ONLY LEARNING HEALTH REVIEW",
			"generic read-only reviewer",
			"no separate learning-maintenance tool",
			"call_generic_agent",
			"Pulse Fixer",
			"recommended_fix",
		},
		"improve-knowledge": {
			"READ-ONLY KNOWLEDGEBASE HEALTH REVIEW",
			"generic read-only reviewer",
			"no separate KB-maintenance tool",
			"call_generic_agent",
			"Pulse Fixer",
			"recommended_fix",
		},
		"improve-database": {
			"READ-ONLY DATABASE HEALTH REVIEW",
			"generic read-only reviewer",
			"no separate DB-maintenance tool",
			"call_generic_agent",
			"Pulse Fixer",
			"verification commands",
		},
		"improve-report": {
			"READ-ONLY REPORT HEALTH REVIEW",
			"do not edit or ask from the reviewer",
			"Pulse Fixer",
			"recommended_fix",
		},
		"improve-evaluation": {
			"READ-ONLY EVALUATION HEALTH REVIEW",
			"The reviewer does not edit or run anything",
			"Pulse Fixer",
			"GOAL_SEMANTIC",
		},
	}
	for kind, wants := range cases {
		rendered, err := renderFromRegistry(kind, tmplData{}, allKinds)
		if err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		for _, want := range wants {
			if !strings.Contains(rendered, want) {
				t.Fatalf("%s missing read-only reviewer contract %q", kind, want)
			}
		}
	}
}

func TestPulseSpecialistsReturnStructuredPacketsAndParentOwnsHTML(t *testing.T) {
	kinds := []string{
		"design-plan",
		"bug-review",
		"llm-ops-review",
		"review-code",
		"review-cost",
		"review-speed",
		"review-artifact-drift",
		"improve-learnings",
		"improve-knowledge",
		"improve-database",
		"improve-evaluation",
		"improve-report",
		"goal-advisor",
	}
	for _, kind := range kinds {
		rendered, err := renderFromRegistry(kind, tmplData{}, allKinds)
		if err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		for _, want := range []string{
			"finding_id",
			"target_key",
			"recommended_fix",
			"verification",
			"user_judgment_required",
			"builder/improve.html",
		} {
			if !strings.Contains(rendered, want) {
				t.Fatalf("%s missing structured specialist handoff %q", kind, want)
			}
		}
	}

	logGuidance, err := renderFromRegistry("review-improve-log", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render review-improve-log: %v", err)
	}
	for _, want := range []string{
		"Reviewer/writer boundary",
		"A specialist is strictly read-only",
		"the parent validates evidence",
		"updates `builder/improve.html` once",
		"specialists never load either presentation reference",
	} {
		if !strings.Contains(logGuidance, want) {
			t.Fatalf("review-improve-log missing parent-only writer contract %q", want)
		}
	}
}

func TestImprovementAndPlanGuidanceIncludesAssumptionAudit(t *testing.T) {
	for _, kind := range []string{
		"design-plan",
		"review-code",
		"review-artifact-drift",
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
		if !strings.Contains(rendered, "assumption-audit") {
			t.Fatalf("%s guidance does not include assumption-audit", kind)
		}
	}

	designPlan, err := renderFromRegistry("design-plan", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render design-plan: %v", err)
	}
	for _, want := range []string{
		"Call `review_plan",
		"dependent artifacts",
		"VISUAL MAP",
		"PRIORITIES",
		"never discard findings",
	} {
		if !strings.Contains(designPlan, want) {
			t.Fatalf("combined design-plan guidance missing %q", want)
		}
	}
	if _, exists := allKinds["review-plan"]; exists {
		t.Fatal("review-plan must remain merged into design-plan")
	}
	for _, kind := range []string{
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
		if !strings.Contains(rendered, "parent") || !strings.Contains(rendered, "provided") {
			t.Fatalf("%s must tell the parent to provide assumption-audit to the generic reviewer", kind)
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
		"Pulse can run Bug Review and Goal Advisor in the same cycle",
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

func TestPlanReviewAndGoalAdvisorPreferCoherentAgenticSteps(t *testing.T) {
	checks := map[string][]string{
		"design-plan": {
			"Modern agents can own a substantial end-to-end outcome in one step",
			"Validation sequence, not micro-steps",
			"first work turn the complete outcome",
			"one-message-per-routine-action sequences",
		},
		"goal-advisor": {
			"Modern agentic models can own a substantial end-to-end outcome",
			"fewest durable steps",
			"give the first work turn the whole outcome",
			"Do not replace regular-step fragmentation",
		},
		"plan-design": {
			"Give the work turn the complete outcome",
			"do not create one item per checklist line",
			"re-open the evidence and verify every success criterion",
			"tiny routine instructions",
			"one coherent agentic outcome",
		},
	}

	for kind, wants := range checks {
		registry := allKinds
		if kind == "plan-design" {
			registry = referenceKinds
		}
		rendered, err := renderFromRegistry(kind, tmplData{}, registry)
		if err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		for _, want := range wants {
			if !strings.Contains(rendered, want) {
				t.Fatalf("%s missing coherent-agentic-step guidance %q", kind, want)
			}
		}
	}
}

func TestDeterministicFetchersFeedLargeAgenticProcessors(t *testing.T) {
	checks := map[string]struct {
		registry map[string]kindMeta
		wants    []string
	}{
		"design-plan": {
			registry: allKinds,
			wants: []string{
				"Scripted acquisition, agentic processing",
				"batch related calls",
				"feed the durable rows/artifacts into a large message sequence",
				"10+-run evidence bar is only for *freezing*",
			},
		},
		"goal-advisor": {
			registry: allKinds,
			wants: []string{
				"Separate deterministic acquisition from agentic processing",
				"fetcher steps with explicit outputs",
				"large downstream `message_sequence`",
				"Do not keep fixed API/CLI retrieval inside LLM turns",
			},
		},
		"plan-design": {
			registry: referenceKinds,
			wants: []string{
				"Deterministic fetcher → agentic processor",
				"do not create one step per endpoint or command",
				"scripted regular step executes it",
				"Consume deterministic evidence; do not fetch it conversationally",
			},
		},
		"regular": {
			registry: referenceKinds,
			wants: []string{
				"Declare these steps `scripted` from initial design",
				"No run-history threshold is required",
				"regular scripted fetcher(s) → message_sequence processor",
			},
		},
		"message-sequence": {
			registry: referenceKinds,
			wants: []string{
				"fetch-and-normalize-authoritative-data",
				"Do not use one step per endpoint",
				"execute-request-spec",
				// Store-writable allow-list (db/assets is the only step-writable file home).
				"the hard allow-list",
				// Validate-on-what-it-produces, db-first, no forced throwaway JSON.
				"Validate on what the step actually produces",
				"will not force a throwaway output file",
				"has no gate at all",
			},
		},
		"step-config": {
			registry: referenceKinds,
			wants: []string{
				"Scripts are the default for DETERMINISTIC execution",
				"Use coherent scripted fetchers, not micro-scripts",
				"10+ representative-run threshold applies only before freezing",
			},
		},
	}

	for kind, check := range checks {
		rendered, err := renderFromRegistry(kind, tmplData{}, check.registry)
		if err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		for _, want := range check.wants {
			if !strings.Contains(rendered, want) {
				t.Fatalf("%s missing deterministic-fetcher guidance %q", kind, want)
			}
		}
	}

	stale := map[string]struct {
		registry map[string]kindMeta
		text     string
	}{
		"planning-steps":    {registry: referenceKinds, text: "one atomic action with no"},
		"optimize-playbook": {registry: referenceKinds, text: "add a separate step after it that reads the output"},
		"workflow-patterns": {registry: referenceKinds, text: "`regular`(action) → `regular`(verify"},
		"todo-task":         {registry: referenceKinds, text: "manages multiple discrete tasks"},
	}
	for kind, check := range stale {
		rendered, err := renderFromRegistry(kind, tmplData{}, check.registry)
		if err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		if strings.Contains(rendered, check.text) {
			t.Fatalf("%s retains stale micro-step guidance %q", kind, check.text)
		}
	}
}

func TestSharedContextSpansOwnProofValidationAndRepair(t *testing.T) {
	checks := map[string]struct {
		registry map[string]kindMeta
		wants    []string
	}{
		"planning-steps": {
			registry: referenceKinds,
			wants: []string{
				"one large `message_sequence` per shared-context span",
				"proof/provenance output",
				"Use multiple large sequences when their contexts should not be shared",
				"The builder must decide this from",
			},
		},
		"plan-design": {
			registry: referenceKinds,
			wants: []string{
				"one large `message_sequence` for each coherent shared-context span",
				"proof/evidence contract",
				"Multiple large sequences are correct when their contexts should not be shared",
				"builder must decide this from the workflow semantics",
			},
		},
		"goal-advisor": {
			registry: allKinds,
			wants: []string{
				"one large `message_sequence` per coherent shared-context span",
				"run-specific proof/provenance fields",
				"desire for more validation is not by itself",
				"Multiple large sequences are appropriate when context should be isolated",
			},
		},
		"optimize-playbook": {
			registry: referenceKinds,
			wants: []string{
				"keep it in the same shared context",
				"repair/double-check turn",
				"Strengthen before splitting",
				"Validate in context",
			},
		},
		"workflow-patterns": {
			registry: referenceKinds,
			wants: []string{
				"one large `message_sequence` owns",
				"re-read the system of record and prove the effect",
				"start with one large `message_sequence` per shared-context span",
			},
		},
	}

	for kind, check := range checks {
		rendered, err := renderFromRegistry(kind, tmplData{}, check.registry)
		if err != nil {
			t.Fatalf("render %s: %v", kind, err)
		}
		for _, want := range check.wants {
			if !strings.Contains(rendered, want) {
				t.Fatalf("%s missing shared-context proof guidance %q", kind, want)
			}
		}
	}
}

func TestWorkflowPatternsUseCurrentRuntimeAndStoreContracts(t *testing.T) {
	rendered, err := renderFromRegistry("workflow-patterns", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render workflow-patterns: %v", err)
	}

	for _, want := range []string{
		"already supplied by the user, launch variables",
		"one large `message_sequence` to investigate and produce the proof-bearing deliverable",
		"scripted `regular` for deterministic API/CLI/DB/auth/connectivity checks",
		"another large `message_sequence` only when adaptive post-approval judgment",
		"knowledgebase/notes/",
		"learnings/_global/SKILL.md",
		"HTML reports read report-facing rows live with `window.report.query`",
		"read-only `source_sql`",
		"message_sequence.items[]",
		"todo_task.messages[]",
		"processed-versus-selected counts",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("workflow-patterns missing current contract %q", want)
		}
	}

	for _, stale := range []string{
		"writes a JSON array to `db/<file>.json`",
		"KB SKILL.md update",
		"`regular`(draft / propose / select)",
		"Skipping the `human_input`",
		"First `regular` step is a cheap probe",
		"consumer's `source` must point",
	} {
		if strings.Contains(rendered, stale) {
			t.Fatalf("workflow-patterns retains stale contract %q", stale)
		}
	}
}

// The store freshness mechanism: Gate reads the code-owned _freshness ledgers and
// marks learning_health / knowledgebase_health due on a confirmation-recency
// signal; the reviewer docs gain a re-verify -> demote pass and protect the
// code-owned ledger from edits.
func TestPulseStoreFreshnessTriggerAndReviewerPass(t *testing.T) {
	postRun, err := renderFromRegistry("post-run-monitor", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render post-run-monitor: %v", err)
	}
	for _, want := range []string{
		"learnings/_global/_freshness.json",
		"knowledgebase/_freshness.json",
		"last_confirmed_run",
		"freshness (confirmation recency)",
	} {
		if !strings.Contains(postRun, want) {
			t.Fatalf("post-run-monitor missing freshness trigger %q", want)
		}
	}

	learn, err := renderFromRegistry("improve-learnings", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render improve-learnings: %v", err)
	}
	for _, want := range []string{
		"FRESHNESS PASS (confirmation recency)",
		"Confirmation recency, not calendar age",
		"code-owned freshness ledger",
	} {
		if !strings.Contains(learn, want) {
			t.Fatalf("improve-learnings missing freshness pass %q", want)
		}
	}

	kb, err := renderFromRegistry("improve-knowledge", tmplData{}, allKinds)
	if err != nil {
		t.Fatalf("render improve-knowledge: %v", err)
	}
	for _, want := range []string{
		"FRESHNESS PASS (confirmation recency)",
		"Confirmation recency, not calendar age",
		"code-owned freshness ledger",
	} {
		if !strings.Contains(kb, want) {
			t.Fatalf("improve-knowledge missing freshness pass %q", want)
		}
	}
}
