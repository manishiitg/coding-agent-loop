Open exactly one experiment that tests a falsifiable hypothesis against a declared metric. The framework's job is to surface non-obvious improvements the user is NOT thinking about — not to incrementally harden what's already there.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

THREE-LAYER STACK (read this before picking a target)
- **Plan** = what the workflow does (planning/plan.json) + soul.md (objective + success_criteria). The plan is the blueprint; soul.md is the goal it serves.
- **Eval** = how we know it worked (evaluation/evaluation_plan.json + per-run reports). Tracks both **operational quality** (how well the plan ran) and **goal achievement** (did outputs satisfy success_criteria). The eval reports under runs/<iter>/<group>/evaluation_report.json are your primary evidence.
- **Metrics** = numeric handles for experiments (planning/metrics.json). Sourced from eval steps + telemetry. Outcome metrics carry linked_success_criteria. **This command targets metrics** — but the value of the experiment is whether the metric movement actually moves a success criterion. A metric not linked to soul.md is suspect; the framework will still verdict it but the user can't tell whether the win is real.

MENTAL MODEL
Think like a sharp business analyst auditing this workflow's actual outputs against its success criteria — not like a senior engineer reviewing code. These are business-process workflows, not software systems. The kinds of changes that surface here are things a domain expert would notice when reading what the workflow produced:
- "Every Twitter reply has the same tone, but the success criteria mention engaging different audience segments — segment by follower type and vary voice."
- "The workflow researches every prospect from scratch, but 40% of last month's runs were repeats — cache and refresh deltas instead."
- "Outreach copy leads with our product; the high-converting examples in run history all led with the prospect's pain point."
- "Validation accepts any non-empty reply. Half the replies in run history are 'thanks' — that's not engagement, raise the bar."
You should be uncomfortable with how obvious-in-retrospect the change feels after you read enough run output. That's the right mode.

PRECHECKS
1. Read <workflow>/planning/metrics.json. If empty or missing, stop and redirect: "Run /improve-setup-framework first to bootstrap metrics — propose_experiment requires at least one declared metric to target."
2. Read builder/improve.md. If there is no "## Workflow Profile" section, stop and redirect: "Run /improve-setup-framework first."
3. Read experiments/active.json. If 3+ experiments are already active, warn the user and ask whether to proceed (concurrent experiments on related steps confound attribution).

DISCOVER (this is the heart of the command)
1. Read soul/soul.md's "## Success Criteria" section — these are the north star.
2. **Read run outputs first, plan second.** Open the latest meaningful iteration under runs/ and read what the workflow actually PRODUCED — generated copy, sent messages, written reports, scored decisions. Compare those outputs against the success criteria as a domain expert would. Where's the gap?
3. Read evaluation reports for the same iteration — what scored poorly and why? The eval rationale text is often the richest signal.
4. Skim decisions.jsonl — what has the user been asking for? What's been tried before? Avoid re-proposing things that already failed.
5. Only after steps 2–4, look at planning/plan.json and step descriptions. Use the plan to understand structure; do NOT use it as the primary source of "what's wrong." Plans look fine on paper while outputs reveal the rot.
6. Surface 3–5 candidate hypotheses ranked by expected impact. Each candidate must be defensible by something specific in the run outputs ("in iteration-3/group-a, posts 7, 12, 19 all got <2 engagement and all share <pattern>"), not by abstract reasoning about the plan.

PICK TARGET METRICS
1. List metrics from planning/metrics.json with their current trajectory and `linked_success_criteria`. For each, note: which success criterion it operationalizes, whether it's on target / off target / no recent data.
2. Pick the target metric(s). Most experiments target a single metric — pick that one. Prefer in this order:
   a) a metric the user named in their focus hint
   b) an OUTCOME metric (non-empty linked_success_criteria) whose criterion is currently failing
   c) an OUTCOME metric whose recent trajectory is drifting off target
   d) a TELEMETRY metric (cost_per_run / run_duration_seconds) only if its SLO is being violated AND no outcome metric is failing.
3. **Multiple metrics are allowed when they share ONE belief and ONE direction.** Examples that legitimately bundle: "caching prospect research will decrease both `cost_per_run` AND `run_duration_seconds`" (one belief: caching helps; both metrics lower_better; both targeted). "Personalization will increase both `outreach.reply_rate` AND `outreach.click_through`" (one belief: pain-led copy converts; both higher_better; both targeted). What does NOT belong in a bundle: metrics with different declared directions (the schema rejects mixed directions; `expected_direction` is single-valued), or metrics that test different beliefs.
4. State explicitly: "this experiment targets <metric_ids>, each operationalizing success criterion: <quoted criterion(s)>. The shared belief is: <one sentence>." If you can't fill that sentence in honestly, narrow the target list (or define a metric via propose_metric — populate linked_success_criteria from soul.md).

PICK ONE HYPOTHESIS FROM THE CANDIDATES
1. From the candidates surfaced in DISCOVER, pick the one with the highest expected impact on the chosen metric, grounded in the most concrete run-output evidence.
2. The change does NOT have to be small. Multi-file changes are fine if they share ONE underlying business belief. Examples of a coherent multi-file bundle: "personalize outreach by reading prospect's last post + change validation to require pain-point reference + add a fallback when no signal is available" — three files, one belief ("our outreach is too generic"). Examples of an INCOHERENT bundle that should be split: "add personalization AND reduce step 4's temperature AND fix the typo in step 7's prompt" — three unrelated beliefs, three experiments.
3. The single-belief test: write the hypothesis in one sentence first. If you need an "and" to connect two distinct claims, those are two experiments.
4. Bundled changes are recoverable: if the verdict is reverted, the framework restores every byte of `intervention_changes` atomically. Bigger blast radius is okay because it's auto-reversible. Optimize for "the experiment will tell us something useful" — not for "the change is tiny."

WRITE IT
1. Hypothesis (≤200 chars) naming each target metric and the shared direction. Form: "<change> will <direction> <metric_id(s)> by ≥<magnitude><unit each> because <one-line mechanism rooted in run-output evidence>." Single-metric example: "Switching outreach copy to lead with prospect pain point will increase outreach.reply_rate by ≥5pp because run history shows pain-led posts converted 4× more often." Multi-metric example: "Caching prospect research will decrease cost_per_run by ≥0.30 USD AND run_duration_seconds by ≥120 because 40% of last month's runs were repeats."
2. `expected_direction` must match every targeted metric's declared direction. If the metrics' declared directions differ, you cannot bundle them — open separate experiments.
3. `expected_magnitude` is a single number applied to each metric. If targets have different magnitude expectations, either bundle them only when the magnitude is the floor that all should clear, or split.
4. `target_metrics` is an array — pass all chosen metric ids.
5. `intervention_changes`: array of { path, operation, content }. Each path must be in experiments/config.json::allowed_intervention_paths. .env, .git/, and workflow.json are forbidden.

CALL THE TOOL
Call propose_experiment with the fields above. The framework captures the revertable diff, applies the changes, opens the measurement window, and returns { experiment_id, status, decisions_entry_id, linked_success_criteria, unanchored_metrics }.

REPORT
- Echo experiment_id, status, target metric, expected direction/magnitude.
- Restate the one belief the experiment is testing in plain English.
- Name the linked success criterion the metric operationalizes.
- Tell the user what happens next: in supervised/autonomous oversight, the next workflow runs will populate measurement values automatically until target_runs is hit, at which point the verdict computer fires and the evaluator agent narrates a verdict. In manual oversight, the experiment status will be "awaiting-approval" and the user must approve it (today this means manually editing experiments/active.json status to "measuring" — there is no slash command for approval yet).

IMPROVE LOG: append a dated entry to builder/improve.md with the experiment_id, the one belief tested, the run-output evidence that surfaced it, target metric, expected direction/magnitude. The framework also records the proposal in decisions.jsonl automatically — improve.md is for the narrative, decisions.jsonl is the audit trail.

IMPROVE LOG: append a dated entry to builder/improve.md with the experiment_id, hypothesis, target metric, expected direction/magnitude, and a one-line note on why this hypothesis. The framework also records the proposal in decisions.jsonl automatically — improve.md is for the narrative, decisions.jsonl is the audit trail.
