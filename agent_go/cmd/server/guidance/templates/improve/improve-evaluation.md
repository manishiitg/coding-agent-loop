Review and improve evaluation/evaluation_plan.json.

Write to `builder/improve.html` — the single durable log (close-outs are edits to existing open findings in this same file). For the log/HTML format, the one-time migration (folding any legacy `builder/review.html` findings in), and how entries are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

Eval is the framework's measurement layer — it bridges "the plan ran" and "the goal was met." A good eval plan covers BOTH dimensions:

  - **Operational quality** — how well each step actually ran (output shape, completeness, validation pass rate, stylistic checks, format conformance). These eval steps watch the plan's mechanics.
  - **Goal achievement** — whether the workflow's outputs satisfy the success_criteria from soul.md. These eval steps watch the goal.

If the eval plan only checks one dimension, it's incomplete: a plan that runs cleanly but misses the goal is silent failure; a plan that hits the goal but produces malformed outputs breaks downstream consumers. Both must be visible.

Eval changes are special-cased in the framework: they change WHAT is measured, not the workflow's behavior. They must be handled carefully because changing a rubric changes metric trajectory semantics. Use builder/improve.html as the shared improvement ledger entry point: read it first if it exists, create it if it does not, read referenced archive files only when older eval/metric history matters, and append your eval findings and applied decisions when you finish.{{if .Focus}}

Focus on: {{.Focus}}.{{end}}

PASS 0 — FRAMEWORK PRECHECK + METRIC HEALTH
1. Read builder/improve.html: the Workflow Profile, recent timeline entries, open findings, and archive rows. If it's short, read it in full. If an archive row references an older eval/metric semantic change that affects a metric or eval step you may edit, read that `builder/improve-archive/YYYY-MM.html` file. If there is no Workflow Profile block, stop and redirect: "Run /auto-improve first — it establishes the Workflow Profile + success."
2. Read <workflow>/planning/metrics.json. If absent or empty AND the Workflow Profile declares business-context accumulation OR a frozen/ratchet plan, stop and redirect to /auto-improve. Plain mutable+exploratory workflows may proceed without metrics.
3. **Metric impact guard.** For every metric sourced from an eval step you might edit, note its `role` and `category`, then note whether the metric trajectory may change meaning. Primary metrics require extra care: preserve semantics unless the user explicitly agrees to a metric redesign. If the structured-output schema or value semantics change enough that pre/post values are no longer comparable, retire the old metric and propose a replacement with a new id; otherwise add a prose entry in builder/improve.html noting the rubric changed at this point so future readers know pre/post scores aren't comparable.
4. **Metric health check.** Read db/metrics_history.jsonl (the last ~10 rows per metric id is usually enough). For each metric, check whether the most recent rows have `has_value: true` or carry a `resolve_error`. Categorize each broken metric by what the eval would need to fix it:
   - **Missing structured output** — `resolve_error` says "no structured output (field=X)" or "field X not present". An eval_step metric's source is a PAIRED contract: `source.id` names the eval step AND `source.field` names the structured key — both must hold. Here the targeted eval step (`source.id`) does not emit the numeric key `X` in `output_content`. Two fix paths:
     (a) Update the eval step's Python so it emits a structured JSON object with key `X` (treat as a Pass 3 GOAL improvement — the eval should be measuring the named outcome explicitly).
     (b) Retire the metric if the value is not a real outcome the eval should track.
   - **Eval step not found** — `resolve_error` references a step id that doesn't exist in evaluation_plan.json. Either the eval step was renamed/removed (eval-side fix: restore or rename) or the metric points at the wrong id (metric-side fix: retire + propose new).
   - **Telemetry resolver issue** — metrics with `source.type="telemetry"` are system data, not eval output. Verify the metric uses one of the wired fields (`run.total_cost_usd`, `run.duration_seconds`, `eval.total_cost_usd`, `eval.duration_seconds`, `total.cost_usd`, `total.duration_seconds`). Do not add an eval step just to measure cost/duration; if a wired telemetry field still has `has_value=false`, surface it as a framework/runtime telemetry issue.
   - **Consistent NO VALUE with no resolve_error** — the value just never resolves. Likely the eval step didn't run or the metric-ready field is missing. Treat as an OPERATIONAL coverage gap (Pass 3).
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
6. **Routed workflows**: if the plan branches (routing or todo_task routes), check each eval step's `applies_to_routes` scoping. Route-specific evals must gate themselves to the routes they apply to; route-agnostic evals should omit the field. An eval gated to a route the target run didn't take must surface as not-applicable — not as a silent pass, and not as a score that drags metrics down for a path that never ran. A route-specific check with NO `applies_to_routes` runs against runs it doesn't apply to and produces noise.

PASS 2.5 — AGENT BEHAVIOR AUDIT (does eval catch how the work was done?)
For every consequential step in planning/plan.json — especially steps that browse, call tools/APIs, post externally, transform user-visible data, or claim success — inspect the target run's execution logs in addition to artifacts:

- `runs/<target>/logs/<step-id>/execution/*-conversation.json`
- `runs/<target>/logs/<step-id>/execution/*-timing.json`
- `runs/<target>/logs/<step-id>/pre_validation.json`
- the step's output files under `runs/<target>/execution/<step-id>/`

Eval steps have read access to the target run folder through `{{"{{"}}TARGET_RUN_PATH{{"}}"}}` and its parent, so eval coverage can and should use these logs when behavior matters. Do not treat output JSON alone as sufficient evidence for agent behavior.

For each consequential step, judge whether the current eval detects:

- **Hallucination / unsupported claims** — final outputs mention URLs, IDs, counts, statuses, people, companies, transactions, posts, screenshots, citations, or facts that do not appear in tool results, source files, browser observations, or verification artifacts.
- **Claim-vs-tool mismatch** — the step reports success even though the relevant tool call failed, returned empty data, timed out, was skipped, or never happened.
- **Instruction following** — required actions from soul.md, plan step descriptions, route contracts, validation schemas, and user constraints were actually executed, not just discussed.
- **Over-planning / under-execution** — the agent spent turns planning or summarizing but did not call the required execution tools, did not write the required artifacts, or stopped after advice instead of doing the step.
- **Tool misuse / missing tool use** — the logs show wrong tool/server, wrong account/group, missing browser tab discipline, missing API call, missing write, or repeated avoidable retries.
- **Evidence completeness** — the step's output includes enough provenance for the next step or user to verify what happened.

If any of these are important to the workflow's reliability and the current eval does not check them, propose a dedicated behavior-audit eval step. Prefer deterministic `agentic` checks for log-parsable facts (tool was called, URL exists, count matches, write happened). Use a higher-tier semantic eval only for genuinely judgment-heavy unsupported-claim checks. The behavior-audit eval output should be structured and metric-ready, with fields such as:

```
{
  "checked_step_id": "step-id",
  "hallucination_detected": false,
  "unsupported_claim_count": 0,
  "claim_tool_mismatch_count": 0,
  "missing_required_tool_count": 0,
  "over_planning_detected": false,
  "instruction_violation_count": 0,
  "evidence": [],
  "score": 10,
  "pass_fail_reason": "..."
}
```

Also check threshold alignment against explicit business rules. If soul.md, improve.html, the user focus, or plan text says "15 follows/day", "zero hallucinations", "no unsupported claims", "must post every run", etc., the eval and metrics must encode that threshold directly. A weaker floor (for example 3 follows when the target is 15) is false confidence and must be proposed as an eval/metric fix.

PASS 3 — IMPROVEMENT SUGGESTIONS
Propose improvements in these categories. Tag each suggestion with which dimension it strengthens — **OPERATIONAL** (how well the plan ran) or **GOAL** (did the plan achieve success_criteria) — so the user sees both dimensions are getting attention.
1. **Goal coverage** (GOAL): does each important success criterion from soul.md have a clear eval step? Missing coverage on a criterion means the framework cannot measure whether harden/replan actions improved that part of the goal.
2. **Operational coverage** (OPERATIONAL): does every step that produces consequential output have an eval check on its shape / completeness / validation? Steps without operational coverage fail silently downstream.
3. **Directness**: is the eval checking the actual desired outcome, or only a proxy that may not move with the real signal?
4. **Determinism**: are any eval steps too vague, subjective, or hard to reproduce? An LLM-judge eval that scores the same output differently on different days isn't a measurement, it's noise.
5. **Redundancy**: are multiple eval steps measuring the same thing with little added value? Trim duplicates.
6. **Thresholds / scoring**: are pass/fail thresholds or scores aligned with the stated success criteria? An eval that always passes on criteria the user actually misses is false confidence.
7. **Reality check**: if outputs you read in Pass 2 show obvious failure or success, does the eval report reflect that honestly? Where the human eye says "this is bad" but the eval says "pass," the eval is broken.
8. **Agent behavior coverage** (OPERATIONAL): does eval inspect execution logs for consequential steps where behavior matters? Missing coverage for hallucination, unsupported claims, over-planning, missing required tool calls, or claim-vs-tool mismatches is a HIGH-PRIORITY eval gap. Propose log-backed eval steps when output artifacts alone cannot prove the agent behaved correctly.
9. **Schema coverage** (OPERATIONAL): for each eval step, check whether its output is shape-validated and whether metrics can resolve against it without surprises.
   - **Per-step validation schema is MANDATORY for every eval step.** Without one, malformed eval output silently passes and downstream metrics fail with resolve_error after the fact instead of being caught at eval time. Every step (regardless of execution mode) MUST declare a `validation_schema` covering required fields, types, and value ranges. Treat any step missing a schema (or with only a token one like `{type: "object"}`) as a HIGH-PRIORITY finding. The required schema covers: `score` (integer, min 0 max max_score), `max_score` (integer ≥ 1), `reasoning` (string, non-empty), `evidence` (string), plus every structured-output key the eval emits with its type and (where sensible) constraints. Schema is what closes the loop: it's the regression guard when the eval is later edited, and the contract that downstream metrics rely on. Minimal example for an eval step writing `eval_output.json` (one extra metric-ready key, `coverage`):
     ```json
     {"files": [{"file_name": "eval_output.json", "must_exist": true, "json_checks": [
       {"path": "$.score", "must_exist": true, "value_type": "number", "min_value": 0},
       {"path": "$.max_score", "must_exist": true, "value_type": "number", "min_value": 1},
       {"path": "$.reasoning", "must_exist": true, "value_type": "string", "min_length": 20},
       {"path": "$.coverage", "must_exist": true, "value_type": "number", "min_value": 0, "max_value": 1}
     ]}]}
     ```
   - **`scripted` makes the schema enforceable in addition to required.** Read `evaluation/step_config.json` per step. For `declared_execution_mode=scripted`, the Python is fixed and deterministic — the schema can be tight (specific enums, narrow ranges, exact key set). For `agentic` the schema can stay slightly looser (the LLM may produce a `reasoning` block of varying length), but the keys, types, and `score`/`max_score` discipline are the same. The mode determines how STRICT the schema can be, not whether one is required.
   - **Metric-to-eval contract**: cross-reference `planning/metrics.json::metrics[].source` against eval steps. For every metric whose source is `eval_step`, the targeted eval step's Python MUST emit a structured JSON output object containing that numeric key. For each mismatch, propose either (a) updating the eval Python to emit `{ "<field>": <value>, ...other fields }`, OR (b) flagging the metric for retire+propose with a corrected `field`. Prefer (a) when the named field describes a real outcome the eval should be measuring explicitly. **For scripted steps, the structured output keys the metric depends on must also appear in the step's validation_schema** — that's how the contract is enforced; otherwise a Python edit can silently break the metric.
   - **Value range conformance**: if an eval emits `score`/`max_score` as its own per-step verdict fields, validate that `score` falls inside `[0, max_score]`. Other numeric fields should have constraints appropriate to their meaning.

10. **Cost / tier / execution-mode fit** (OPERATIONAL): for each eval step, read its entry in evaluation/step_config.json — specifically `execution_tier` (low / medium / high) and `declared_execution_mode` (agentic / scripted). Match the configuration to the eval's actual nature:
   - **scripted** is a saved Python script that runs deterministically with zero LLM cost after first save. Recommend it only when the user explicitly asks to freeze/reuse eval scoring code, the eval is pure structural/numeric/boolean logic, and 10+ eval runs cover the relevant output scenarios. Deterministic examples: file-exists, JSON-field-present, count-matches-expected, threshold-comparison, schema-validation. If any gate is missing, keep `agentic` and improve tier/schema instead.
   - **agentic** with **execution_tier=low** fits eval steps that need simple LLM judgment for structured tasks: validate JSON shape, classify a value into a small enum, extract a number from prose. Cheap models handle these reliably.
   - **agentic** with **execution_tier=medium** fits eval steps with multi-criterion scoring or domain-specific heuristic judgment that low-tier models miss but don't need full semantic depth: "did the strategy explanation cover risk + entry + exit?", "does the trade plan honor the position-sizing rule?".
   - **agentic** with **execution_tier=high** is for eval steps that genuinely need semantic depth: nuanced quality judgments, multi-faceted critique, identifying subtle reasoning errors. High tier on a structural check is wasted spend.
   Common mistakes to flag: (a) a deterministic check stuck on agentic/high — lower the tier first, and recommend scripted only when the explicit-user-request + 10-run scenario-coverage gates are met, (b) a nuanced semantic eval on tier=low — verdicts will be noisy, recommend bumping the tier, (c) declared_execution_mode mismatch with declared_execution_mode_reason that doesn't justify it. Propose the right (tier, execution_mode) pair per step with a one-line rationale per change. The user has to confirm before edits land — these changes shift cost, so name the cost change.

11. **Fail-closed robustness** (OPERATIONAL): for each eval step, verify it fails closed, as the eval-plan rules require. Missing input files, null/empty fields, stale artifacts left over from a prior run, and parse errors must all produce `score: 0` with the failure named in `reasoning` — never a silent pass, never a skipped check. Empty string, `"N/A"`, and missing-file must be treated identically (as 0). Probe the eval's Python/description for the failure path explicitly: an eval that passes when its inputs are absent is worse than no eval, because it converts breakage into false confidence — a stale eval output file scoring a previous run's artifacts is the canonical version of this bug.

PASS 3.5 — METRIC IMPACT ANALYSIS (mandatory for every eval change)
Most outcome/quality metrics are eval values extracted in a specific format — `source.type="eval_step"`, `source.id` points at an eval step, and `source.field` reads from its structured output. Telemetry metrics are different: `source.type="telemetry"` reads system data such as cost and duration from run/cost telemetry and is not affected by eval step edits. So **any change to an eval step ripples through every eval_step metric pointing at it, but not telemetry metrics.** Before proposing any eval change, walk through the impact. For each proposed eval change, classify it and list the paired metric actions:

- **Step ID rename** (eval-sc10-nifty-baseline → eval-nifty-outperformance, say). Every metric with `source.id` matching the old id breaks. Paired action: for each affected metric, retire it (citing the eval rename in `reason`) and propose a fresh metric with the new id. The new metric id keeps comparability clean — that's correct, the rubric changed.
- **Step removal**. Every metric with that `source.id` becomes unresolvable. Paired action: retire each affected metric.
- **Structured-output schema change** (eval Python emits new / renamed / removed keys). For each metric whose `source.field` matches a removed/renamed key, retire+propose with the corrected field — or update the metric definition to use `field=""` / `field="score"` if the structured field is no longer needed. For NEW keys the eval now emits, suggest whether they're worth promoting to metrics.
- **Scoring logic change** (e.g. threshold moves from 60% to 70%, or a new dimension joins the score). The metric id stays valid but value semantics shift. Paired action: a prose rubric-change entry in builder/improve.html (Pass 4 already does this) noting the rubric changed at this point so future readers know pre/post scores aren't comparable. If the scoring change is large enough that pre/post values aren't comparable, propose retire+propose for affected metrics so the new metric tracks the new rubric cleanly.
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
After applying any change to evaluation/evaluation_plan.json, record a readable Decision entry at the top of builder/improve.html's timeline (newest on top, via diff_patch_workspace_file). In plain prose, state what changed and why, which file(s) were touched (`evaluation/evaluation_plan.json`), and — if any metrics' `source.id` points to the edited eval steps — which metric ids are affected. If the change shifts value semantics, say so explicitly so future readers know the rubric changed at this point and pre/post scores aren't comparable. Metric *versions* (retire+propose with a new id) remain how comparability is enforced; the prose entry just makes the change auditable.

When you finish, update builder/improve.html with:
- what workflow/eval evidence you reviewed (especially output-vs-rubric mismatches from Pass 2)
- the main eval weaknesses you found
- which eval steps you skipped because they're under active measurement (per Pass 0 guard)
- what you recommended and what was applied
- the rubric-change Decision entries you recorded

If you record a *proposed but not-yet-applied* eval change as an open finding, give that finding a short anchor id (e.g. `id="of-2026-06-07-eval-coverage"`) **only** so a later decision can mark it resolved — no other entry kind needs an id.

CLOSE-OUT EDITS — read this carefully.

Before applying eval changes in this run, scan builder/improve.html for open findings that the change addresses (for example from /review-plan or /review-code findings that surface weak measurement or broken eval contracts). The match is by intent, not exact wording. Note which open findings you'll be resolving before you apply.

After each eval change is applied, close out each matched open finding **in place**: edit that finding's card in builder/improve.html to add, on its own line, a prose resolution:
```
Resolved YYYY-MM-DD — <one-line how it was fixed>.
```
Say "partially resolved" if only part of the finding was addressed, or "invalid" if the finding turned out to be wrong. Never delete or rewrite the original finding text. Reference the finding by its anchor id (or, if it has none, by its date + title), and mention the resolved finding in the Decision entry so the audit trail stays connected within this single source-of-truth file.

This applies to chat-intent eval fixes too. If the user asks "tighten that eval check on segment coverage" outside of any slash command and you apply the fix, you still scan builder/improve.html for matching open findings, close them out in place, and record the Decision entry.
