Review and improve `evaluation/evaluation_plan.json`.

Write to `builder/improve.html` - the single durable log. For the log/HTML format, one-time migration from legacy review files, and close-out rules, follow `get_reference_doc(kind="review-improve-log")` and `get_reference_doc(kind="html-output")`.

Eval is the framework's measurement layer. It bridges "the plan ran" and "the goal was met." A good eval plan covers both dimensions:
- Operational quality: how well each step actually ran, including output shape, completeness, validation pass rate, tool use, and format conformance.
- Goal achievement: whether the workflow's outputs satisfy the success criteria from `soul.md`.

If the eval plan only checks one dimension, it is incomplete. A plan that runs cleanly but misses the goal is silent failure; a plan that hits the goal but produces malformed outputs breaks downstream consumers.

Eval changes are special-cased because they change what is measured, not the workflow's behavior. Handle them carefully and record rubric changes in `builder/improve.html` so future agents can interpret before/after runs honestly.{{if .Focus}}

Focus on: {{.Focus}}.{{end}}

PASS 0 - FRAMEWORK PRECHECK
1. Read `builder/improve.html`: Workflow Profile, recent timeline entries, open findings, and archive rows. If it is short, read it in full. If an archive row references an older eval semantic change that affects a step you may edit, read that archive file.
2. If there is no Workflow Profile block, stop and redirect: "Run /auto-improve first - it establishes the Workflow Profile + success."
3. Read `soul/soul.md` and extract the objective and success criteria. These are the standard eval should measure against.

PASS 1 - VALIDATION
1. Call `validate_evaluation_plan`.
2. For each error, explain what is wrong in plain language, show the eval step/widget/field it refers to, and propose the exact fix.
3. For warnings, separate correctness-risk warnings from lower-priority quality issues.

PASS 2 - OUTPUT-FIRST ALIGNMENT
1. Read the latest meaningful run outputs under `runs/`, then read the matching eval reports.{{if .RunFolder}} Use the selected run folder "{{.RunFolder}}" as the primary evidence set.{{end}}
2. Read `planning/plan.json` so you understand what the workflow is producing.
3. Compare the run outputs, eval reports, and `soul.md` success criteria:
   - which success criteria are directly measured by the current eval
   - which are weakly or indirectly measured
   - which are not measured at all
   - whether any eval checks give false confidence or miss obvious failure modes
4. Routed workflows: check each eval step's `applies_to_routes` scoping. Route-specific evals must gate themselves to the routes they apply to; route-agnostic evals should omit the field.

PASS 2.5 - AGENT BEHAVIOR AUDIT
For every consequential step in `planning/plan.json` - especially steps that browse, call tools/APIs, post externally, transform user-visible data, or claim success - inspect the target run's execution logs in addition to artifacts:
- `runs/<target>/logs/<step-id>/execution/*-conversation.json`
- `runs/<target>/logs/<step-id>/execution/*-timing.json`
- `runs/<target>/logs/<step-id>/pre_validation.json`
- the step's output files under `runs/<target>/execution/<step-id>/`

Eval steps have read access to the target run folder through `{{"{{"}}TARGET_RUN_PATH{{"}}"}}`, so eval coverage can and should use these logs when behavior matters.

For each consequential step, judge whether the current eval detects:
- hallucination or unsupported claims
- claim-vs-tool mismatch
- instruction-following failures
- over-planning or under-execution
- tool misuse or missing tool use
- evidence completeness for downstream steps or the user

If any of these are important to reliability and the current eval does not check them, propose a dedicated behavior-audit eval step. Prefer deterministic `scripted` checks for log-parsable facts and LLM judgment only for genuinely semantic checks.

PASS 3 - IMPROVEMENT SUGGESTIONS
Propose improvements in these categories. Tag each suggestion as OPERATIONAL or GOAL:
1. Goal coverage: does each important success criterion from `soul.md` have a clear eval step?
2. Operational coverage: does every consequential step have an eval check on shape, completeness, validation, and tool behavior?
3. Directness: is the eval checking the actual desired outcome, or only a proxy?
4. Determinism: are any eval steps too vague, subjective, or hard to reproduce?
5. Redundancy: are multiple eval steps measuring the same thing with little added value?
6. Thresholds/scoring: are pass/fail thresholds aligned with the stated success criteria?
7. Reality check: where human-visible outputs are clearly bad or good, does the eval reflect that honestly?
8. Agent behavior coverage: does eval inspect execution logs for consequential steps where behavior matters?
9. Schema coverage: every eval step should have a validation schema covering required fields, types, and value ranges. At minimum include `score`, `max_score`, `reasoning`, and `evidence`, plus every structured-output key the eval emits.
10. Cost/tier/execution-mode fit: for each eval step, read `evaluation/step_config.json` and match `execution_tier` and `declared_execution_mode` to the eval's actual nature. Do not promote evals to `scripted` unless the user explicitly asks and there is enough scenario coverage to trust the saved script.
11. Fail-closed robustness: missing input files, null/empty fields, stale artifacts, and parse errors must all produce a failing score with the failure named in `reasoning`.

For every proposed eval change:
- show the before/after snippets per eval step before editing
- call out whether the change affects score semantics
- ask whether to apply all, some, or none

Do not edit `evaluation/evaluation_plan.json` until the user confirms.

PASS 4 - RECORD THE CHANGE
After applying any change to `evaluation/evaluation_plan.json`, record a readable Decision entry at the top of `builder/improve.html`'s timeline. State what changed and why, which file(s) were touched, whether the score/rubric semantics changed, and what future runs should verify.

When you finish, update `builder/improve.html` with:
- what workflow/eval evidence you reviewed
- the main eval weaknesses you found
- what you recommended and what was applied
- any rubric-change Decision entries you recorded

If you record a proposed but not-yet-applied eval change as an open finding, give it a short anchor id only so a later decision can mark it resolved.

CLOSE-OUT EDITS
Before applying eval changes, scan `builder/improve.html` for open findings that the change addresses. Match by intent, not exact wording.

After each eval change is applied, close out each matched open finding in place by adding:
```
Resolved YYYY-MM-DD - <one-line how it was fixed>.
```

Say "partially resolved" if only part of the finding was addressed, or "invalid" if the finding turned out to be wrong. Never delete or rewrite the original finding text.
