Review and improve evaluation/evaluation_plan.json. Eval is the framework's measurement layer — it bridges "the plan ran" and "the goal was met." A good eval plan covers BOTH dimensions:

  - **Operational quality** — how well each step actually ran (output shape, completeness, validation pass rate, stylistic checks, format conformance). These eval steps watch the plan's mechanics.
  - **Goal achievement** — whether the workflow's outputs satisfy the success_criteria from soul.md. These eval steps watch the goal.

If the eval plan only checks one dimension, it's incomplete: a plan that runs cleanly but misses the goal is silent failure; a plan that hits the goal but produces malformed outputs breaks downstream consumers. Both must be visible.

Eval changes are special-cased in the framework: they change WHAT is measured, not the workflow's behavior. So eval changes do NOT open experiments — but they DO have rules to follow because changing the rubric mid-stream invalidates trajectory baselines and active experiments. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your eval findings and applied decisions when you finish.{{if .Focus}}

Focus on: {{.Focus}}.{{end}}

PASS 0 — FRAMEWORK PRECHECK + ACTIVE-EXPERIMENT GUARD
1. Read builder/improve.md. If there is no "## Workflow Profile" section, stop and redirect: "Run /improve-setup-framework first."
2. Read <workflow>/planning/metrics.json. If absent or empty AND the Workflow Profile declares business-context accumulation OR a frozen/ratchet plan, stop and redirect to /improve-setup-framework. Plain mutable+exploratory workflows may proceed without metrics.
3. **Active-experiment guard.** Read experiments/active.json. For each experiment whose status is 'measuring' or 'evaluating', look at its target_metrics and resolve each metric in metrics.json. If any of those metrics is sourced from an eval step you might be about to edit (source.type=eval_step, source.id matches an eval step id), STOP and tell the user: "experiment <id> is currently measuring metric <m> against eval step <step_id>. Editing that step now would change its rubric mid-stream and invalidate the experiment's baseline. Either wait for the experiment to conclude (or /exp-abort it) before editing this eval step, or focus this command on eval steps not under active measurement." Proceed only with eval steps that are NOT under measurement.
4. **Metric health check.** Read db/metrics_history.jsonl (the last ~10 rows per metric id is usually enough). For each metric, check whether the most recent rows have `has_value: true` or carry a `resolve_error`. Categorize each broken metric by what the eval would need to fix it:
   - **Missing structured output** — `resolve_error` says "no structured output (field=X)". The metric specifies `source.field=X` but the targeted eval step's report has only flat fields (score/max_score/reasoning/evidence). Two fix paths:
     (a) Update the eval step's Python so it emits a structured JSON object with key `X` (treat as a Pass 3 GOAL improvement — the eval should be measuring the named outcome explicitly).
     (b) If the metric was meant to track the eval step's pass/fail score, retire the metric and propose a replacement with `field=""` (percent score) or `field="score"` (raw 0-10) instead — this is a metric-design fix, not an eval fix, but worth surfacing so the user knows.
   - **Eval step not found** — `resolve_error` references a step id that doesn't exist in evaluation_plan.json. Either the eval step was renamed/removed (eval-side fix: restore or rename) or the metric points at the wrong id (metric-side fix: retire + propose new).
   - **Consistent NO VALUE with no resolve_error** — the value just never resolves. Likely the eval step didn't run or its score is missing. Treat as an OPERATIONAL coverage gap (Pass 3).
   Surface every broken metric with its diagnosis BEFORE proposing other eval changes — broken metrics make subsequent verdicts unreliable, so they're highest priority.

PASS 1 — VALIDATION
1. Call validate_evaluation_plan.
2. For each error: explain what's wrong in plain language, show the eval step/widget/field it refers to, and propose the exact fix.
3. For warnings: separate correctness-risk warnings from lower-priority quality issues.

PASS 2 — OUTPUT-FIRST ALIGNMENT (does eval catch what success_criteria care about?)
1. Read soul/soul.md and extract the objective and success criteria. These are the standard eval should measure against.
2. **Read run outputs first.** Open the latest meaningful iteration under runs/ and look at what was produced. Then read the matching eval reports. Where does the eval rubric MISS what a domain expert would notice? Examples: outputs are bland and repetitive but eval says they pass; outputs make unsupported claims but eval doesn't check; outputs ignore audience segmentation but eval has no segment-specific check.
3. Read planning/plan.json so you understand what the workflow is producing.
4. {{if .RunFolder}}Use the selected run folder "{{.RunFolder}}" as the primary evidence set.{{else}}If a meaningful prior run exists, use it as evidence; otherwise find the latest meaningful run first.{{end}}
5. From the output review + run/eval comparison, judge:
   - which success criteria are directly measured by the current eval
   - which are only weakly or indirectly measured
   - which are not measured at all (coverage gap)
   - whether any eval checks give false confidence (says pass when outputs are clearly weak) or miss obvious failure modes

PASS 3 — IMPROVEMENT SUGGESTIONS
Propose improvements in these categories. Tag each suggestion with which dimension it strengthens — **OPERATIONAL** (how well the plan ran) or **GOAL** (did the plan achieve success_criteria) — so the user sees both dimensions are getting attention.
1. **Goal coverage** (GOAL): does each important success criterion from soul.md have a clear eval step? Missing coverage on a criterion means the framework can't verdict experiments against that part of the goal.
2. **Operational coverage** (OPERATIONAL): does every step that produces consequential output have an eval check on its shape / completeness / validation? Steps without operational coverage fail silently downstream.
3. **Directness**: is the eval checking the actual desired outcome, or only a proxy that may not move with the real signal?
4. **Determinism**: are any eval steps too vague, subjective, or hard to reproduce? An LLM-judge eval that scores the same output differently on different days isn't a measurement, it's noise.
5. **Redundancy**: are multiple eval steps measuring the same thing with little added value? Trim duplicates.
6. **Thresholds / scoring**: are pass/fail thresholds or scores aligned with the stated success criteria? An eval that always passes on criteria the user actually misses is false confidence.
7. **Reality check**: if outputs you read in Pass 2 show obvious failure or success, does the eval report reflect that honestly? Where the human eye says "this is bad" but the eval says "pass," the eval is broken.
8. **Schema coverage** (OPERATIONAL): for each eval step, check whether its output is shape-validated and whether metrics can resolve against it without surprises.
   - **Per-step validation schema**: does the step declare a `pre_validation` / `validation_schema`? Without one, a malformed eval output silently passes and downstream metrics fail with resolve_error after the fact instead of being caught at eval time. Recommend adding a minimal validation schema covering the fields the step is supposed to emit (`score`, `max_score`, `reasoning`, `evidence`, plus any structured-output keys the eval Python writes).
   - **Metric-to-eval contract**: cross-reference `planning/metrics.json::metrics[].source` against eval steps. For every metric whose source is `eval_step` with a non-empty `field` other than `score` / `max_score`, the targeted eval step's Python MUST emit a structured JSON output object containing that key. If the eval emits only flat output (score/max_score/reasoning/evidence), the metric will fail every snapshot. For each such mismatch, propose either (a) updating the eval Python to emit `{ "<field>": <value>, ...other fields }` as part of its scoring output, OR (b) flagging the metric for retire+propose with a corrected `field`. Prefer (a) when the named field describes a real outcome the eval should be measuring explicitly.
   - **Score range conformance**: eval reports use `score` and `max_score`. If a step's typical scores fall outside `[0, max_score]`, that's a bug in the scoring logic — surface and propose a fix.

9. **Cost / tier / execution-mode fit** (OPERATIONAL): for each eval step, read its entry in evaluation/step_config.json — specifically `execution_tier` (low / medium / high) and `declared_execution_mode` (code_exec / learn_code). Match the configuration to the eval's actual nature:
   - **learn_code** is a saved Python script that runs deterministically with zero LLM cost after first save. Recommend it for evals that are pure structural / numeric / boolean checks: file-exists, JSON-field-present, count-matches-expected, threshold-comparison, schema-validation. If you can describe the check as a deterministic algorithm in 20 lines of Python, it should be learn_code. Misclassifying these as code_exec means paying LLM cost every run for work that could be free.
   - **code_exec** with **execution_tier=low** fits eval steps that need simple LLM judgment for structured tasks: validate JSON shape, classify a value into a small enum, extract a number from prose. Cheap models handle these reliably.
   - **code_exec** with **execution_tier=medium** fits eval steps with multi-criterion scoring or domain-specific heuristic judgment that low-tier models miss but don't need full semantic depth: "did the strategy explanation cover risk + entry + exit?", "does the trade plan honor the position-sizing rule?".
   - **code_exec** with **execution_tier=high** is for eval steps that genuinely need semantic depth: nuanced quality judgments, multi-faceted critique, identifying subtle reasoning errors. High tier on a structural check is wasted spend.
   Common mistakes to flag: (a) a deterministic check stuck on code_exec/high — should be learn_code, (b) a nuanced semantic eval on tier=low — verdicts will be noisy, recommend bumping the tier, (c) declared_execution_mode mismatch with declared_execution_mode_reason that doesn't justify it. Propose the right (tier, execution_mode) pair per step with a one-line rationale per change. The user has to confirm before edits land — these changes shift cost, so name the cost change.

PASS 3.5 — METRIC IMPACT ANALYSIS (mandatory for every eval change)
A metric is just an eval value extracted in a specific format — `source.id` points at an eval step, `source.field` reads from its output. So **any change to an eval step ripples through every metric pointing at it.** Before proposing any eval change, walk through the impact. For each proposed eval change, classify it and list the paired metric actions:

- **Step ID rename** (eval-sc10-nifty-baseline → eval-nifty-outperformance, say). Every metric with `source.id` matching the old id breaks. Paired action: for each affected metric, retire it (citing the eval rename in `reason`) and propose a fresh metric with the new id. The trajectory chart starts a new line — that's correct, the rubric changed.
- **Step removal**. Every metric with that `source.id` becomes unresolvable. Paired action: retire each affected metric.
- **Structured-output schema change** (eval Python emits new / renamed / removed keys). For each metric whose `source.field` matches a removed/renamed key, retire+propose with the corrected field — or update the metric definition to use `field=""` / `field="score"` if the structured field is no longer needed. For NEW keys the eval now emits, suggest whether they're worth promoting to metrics.
- **Scoring logic change** (e.g. threshold moves from 60% to 70%, or a new dimension joins the score). The metric id stays valid but value semantics shift. Paired action: a `decisions.jsonl` rubric-change entry (Pass 4 already does this), and the trajectory chart should break the line at that timestamp. If the scoring change is large enough that pre/post values aren't comparable, propose retire+propose for affected metrics so the new metric tracks the new rubric cleanly.
- **No metric impact** (e.g. polishing the description, fixing a typo in reasoning). Note this explicitly: "no metrics affected — pure eval-side cleanup."

For each proposed eval change, output a block like:
```
Proposed change: <one-line summary of the eval edit>
Metric impact: <one-line classification>
Paired metric actions:
  - retire metric_id_1 (reason: <eval change>)
    propose new metric: <new_id> (...)
  - <or "none — pure eval-side change">
```

If any proposed eval change is "step rename" or "structured-output schema change" but the user hasn't yet been shown the metric_id ripple, STOP and surface them before showing the diff. Eval changes that silently break metrics are the failure mode — making the linkage explicit is the whole point of this pass.

Show ALL proposed changes as a diff (before/after snippets per eval step) before editing. Ask whether to apply all, some, or none. **Apply eval edits and the paired metric retire/propose calls together** — never apply an eval change first and leave metrics dangling. Don't edit evaluation/evaluation_plan.json until I confirm.

PASS 4 — RECORD THE CHANGE (every eval edit)
After applying any change to evaluation/evaluation_plan.json:
1. Append an entry to builder/decisions.jsonl using diff_patch_workspace_file. Format (one JSON object per line):
   {"id": "<short-id-or-uuid>", "ts": "<ISO-8601 UTC>", "source": "agent", "trigger": "improve-eval", "applied_changes": ["evaluation/evaluation_plan.json"], "rationale": "<one-line summary of what changed and why>", "target_metrics": [<list of metric ids whose source.id points to edited eval steps, if any>]}
2. The decisions entry serves as a "rubric change" marker. Trajectory chart renderers should break the line at this timestamp because pre-change and post-change scores aren't comparable.

When you finish, update builder/improve.md with:
- what workflow/eval evidence you reviewed (especially output-vs-rubric mismatches from Pass 2)
- the main eval weaknesses you found
- which eval steps you skipped because they're under active measurement (per Pass 0 guard)
- what you recommended and what was applied
- the decisions.jsonl entries you appended (rubric-change markers)

Each new entry that records a *proposed but not-yet-applied* eval change gets a stable id of the form `I-YYYY-MM-DD-NNN` — today's date plus a 3-digit sequence that restarts at `001` per day. Scan the file for today's highest existing sequence and continue from there; never reuse an id.

CLOSE-OUT EDITS — read this carefully.

Before applying eval changes in this run, scan builder/review.md for findings that the change addresses (most likely from /review-goal-alignment, but /review-plan can also surface "no validation_schema / weak measurement" findings that map to eval). The match is by intent, not exact wording. Collect the matching `F-YYYY-MM-DD-NNN` ids before you apply.

After each eval change is applied:

1. **Edit builder/review.md** to append, on its own line immediately after each matched finding:
   ```
   **[RESOLVED YYYY-MM-DD — <one-line how it was fixed>]**
   ```
   Use `[PARTIALLY RESOLVED ...]` if only part of the finding was addressed; use `[INVALID YYYY-MM-DD — ...]` if the finding turned out to be wrong. Never delete or rewrite the original finding.

2. **In the builder/decisions.jsonl entry from Pass 4** (the rubric-change marker), include `linked_review_finding` populated with the array of matched `F-...` ids. This is what makes the audit trail searchable: every rubric change that closed a review item points back at it, and every resolved review item names the decision that closed it.

This applies to chat-intent eval fixes too. If the user asks "tighten that eval check on segment coverage" outside of any slash command and you apply the fix, you still scan review.md for matching findings, append the RESOLVED marker, and link the decision.
