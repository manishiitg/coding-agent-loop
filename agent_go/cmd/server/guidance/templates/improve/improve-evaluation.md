# READ-ONLY EVALUATION HEALTH REVIEW

Review `evaluation/evaluation_plan.json` and its latest evidence. This specialist
is read-only. Do not edit eval/config/report files, call plan/config mutation
tools, run an eval, update `builder/improve.html`, create questions, or mark
module state. Any later wording such as improve, apply, edit, fix, add, retire,
remove, update, resolve, or run means **recommend the exact change to the Pulse
Fixer**. The Pulse Fixer may automatically apply correctness-preserving repairs;
semantic changes still require an exact approved human-input request.

Return only: `verdict`, ordered findings tagged `CORRECTNESS_REPAIR`,
`OPERATIONAL`, or `GOAL_SEMANTIC`, precise `evidence`, bounded
`recommended_fix` items, score-continuity impact, verification steps, and
`user_judgment_required` with reason. Use the remaining document only as the
evaluation-health audit checklist.

Read `builder/improve.html` as the durable prior-decision log, but do not write
it. The Pulse Fixer owns the one consolidated log update and close-out.

The parent Workshop/Pulse agent must load `assumption-audit` and include the parent-provided eval lens with this checklist. The generic reviewer must not call Workshop-only guidance, validation, eval-run, or mutation tools. An eval must measure `soul.md` success, not reward compliance with the current architecture, channel, step sequence, provider, artifact shape, or proxy unless the user explicitly made that part of success. Recommend bounded measurement corrections when semantics stay unchanged; surface material goal/rubric choices for Pulse's Assumptions challenged and require user approval when business meaning changes.

Eval is the framework's goal-measurement layer: does a run satisfy the success criteria in `soul.md`? Operational quality — errored/skipped steps, empty or malformed artifacts, tool misuse, hallucinated successes — is owned by Pulse Gate/Bug Review and by `pre_validation`, not by eval steps. An eval step that would pass on any operationally clean run duplicates Pulse and inflates the score; treat such steps as retirement candidates.

Do not treat every empty collection as a failed result. Distinguish missing or unverified collection from a trustworthy source proving a legitimate zero-cardinality business state. A valid zero should receive the score warranted by the success criterion; the rubric must name the evidence that proves it is real so fabricated or silently missing data still fails closed.

The eval plan's completeness test is the two-way coverage matrix:
- every important success criterion has an eval step measuring it (unmeasured criterion → add coverage), and
- every eval step maps to a criterion (orphan step → retire it).

Eval is also a per-run tax: auto-eval runs after every execution, so cost matters. Fewer, deeper steps; scripted fact-extraction; model judgment only on verdicts. Eval spend rivaling execution spend (see `costs/evaluation/`) is itself a finding.

Eval changes are special-cased because they change what is measured, not the workflow's behavior. Handle them carefully and tell the parent Pulse Fixer to record rubric changes in `builder/improve.html` so future agents can interpret before/after runs honestly.{{if .Focus}}

Focus on: {{.Focus}}.{{end}}

PASS 0 - FRAMEWORK PRECHECK
1. Read `builder/improve.html`: recent timeline entries, open findings, answered decisions, and archive rows. If it is short, read it in full. If an archive row references an older eval semantic change that affects a step you may edit, read that archive file.
2. Read `soul/soul.md` and extract the objective and success criteria. If those are missing or not checkable, return `blocked` and recommend `define-success`; Goal Advisor is not the setup path.
3. Treat `soul/soul.md` as the Goal / Ikigai source of truth. `builder/improve.html` holds time-based Signal, Reflection, and Improvement history, not a duplicate Goal/Profile block.

PASS 1 - STRUCTURAL VALIDATION REVIEW
1. Inspect `evaluation/evaluation_plan.json` and `evaluation/step_config.json`
   directly. The parent Pulse Fixer calls `validate_evaluation_plan` before and
   after any edit.
2. For each error, explain what is wrong in plain language, show the eval step/widget/field it refers to, and propose the exact fix.
3. For warnings, separate correctness-risk warnings from lower-priority quality issues.

PASS 2 - OUTPUT-FIRST ALIGNMENT
1. Read the latest meaningful run outputs under `runs/`, then read the matching eval reports.{{if .RunFolder}} Use the selected run folder "{{.RunFolder}}" as the primary evidence set.{{end}}
2. Read `planning/plan.json` so you understand what the workflow is producing.
3. Compare the run outputs, eval reports, and `soul.md` success criteria (the coverage matrix, both ways):
   - which success criteria are directly measured by the current eval
   - which are weakly or indirectly measured
   - which are not measured at all
   - which eval steps map to NO criterion (orphans) or duplicate Pulse Gate/Bug Review / `pre_validation` (operational checks) — both are retirement candidates
   - whether any eval checks give false confidence or miss obvious failure modes
4. Routed workflows: check each eval step's `applies_to_routes` scoping. Route-specific evals must gate themselves to the routes they apply to; route-agnostic evals should omit the field.

PASS 2.5 - AGENT BEHAVIOR AUDIT
Behavior auditing — hallucination, claim-vs-tool mismatch, tool misuse, skipped work — is Pulse Bug Review's per-run job, not eval's. This pass spot-checks whether behavior problems exist that neither Pulse nor eval caught. For every consequential step in `planning/plan.json` - especially steps that browse, call tools/APIs, post externally, transform user-visible data, or claim success - inspect the target run's execution logs in addition to artifacts:
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

Then route what you find:
- If the behavior IS a success criterion (e.g. "posts to the external site correctly", "every claim is sourced"), propose a dedicated eval step for it — scripted extraction of log-parsable facts, LLM judgment only for the genuinely semantic part.
- Otherwise record the gap as an open finding tagged Bug for Pulse Bug Review/Fixer. Do NOT add an eval step that duplicates Pulse operational review.

PASS 3 - CLASSIFY AND IMPROVE
Classify every candidate change before acting. The classification controls whether user approval is needed.

**CORRECTNESS REPAIR — recommend automatic application by the Pulse Fixer; no user question.** A repair is correctness-preserving when the intended success criterion and score semantics stay unchanged and the edit only makes the existing check tell the truth. This includes:
- binding evidence to the current run/group instead of accepting an older receipt, report, DB row, or artifact
- fixing `TARGET_RUN_PATH`, route scoping, file paths, parsing, or validation-schema wiring
- replacing ambiguous "under TARGET_RUN_PATH" artifact references with exact producer-relative paths or narrow single-match patterns
- making missing, null, empty, stale, malformed, or provider-unconfirmed evidence fail closed
- correcting deterministic extraction so the existing rubric receives the right facts
- fixing an eval/report mismatch that is unambiguously contradicted by the current plan/output contract

For a correctness repair, return exact before/after intent, artifacts affected,
validation commands, and whether a targeted eval would materially verify it.
The reviewer does not edit or run anything. Never recommend a human-input
request merely to ask whether stale evidence should fail, whether the current
run should be used, or whether a declared provider failure should remain a
failure. Those are engineering truths, not business choices.

**SEMANTIC CHANGE — require user/business approval unless an already-approved Goal Advisor proposal names the exact change.** A change is semantic when it changes what success means or how it is scored. This includes changing a success criterion, threshold, weight, rubric interpretation, score scale, pass/fail policy, or adding/removing coverage in a way that changes the aggregate meaning.

For a semantic change, show the before/after, explain the score discontinuity,
and return one concrete proposed decision for the Pulse Fixer to expose through
the existing human-input flow. Do not ask or edit from the reviewer.

Review these categories, tagging findings as CORRECTNESS REPAIR, OPERATIONAL, or GOAL/SEMANTIC:
1. Goal coverage: does each important success criterion from `soul.md` have a clear eval step?
2. Pulse redundancy: which eval steps duplicate Pulse Gate/Bug Review or `pre_validation` (existence/format/step-ran/tool-behavior checks)? Propose retiring them — operational coverage is Pulse's job.
3. Directness: is the eval checking the actual desired outcome, or only a proxy?
4. Determinism: are any eval steps too vague, subjective, or hard to reproduce?
5. Redundancy: are multiple eval steps measuring the same thing with little added value?
6. Thresholds/scoring: are pass/fail thresholds aligned with the stated success criteria?
7. Reality check: where human-visible outputs are clearly bad or good, does the eval reflect that honestly?
8. Agent behavior coverage: does eval inspect execution logs for consequential steps where behavior matters?
9. Schema coverage: every eval step should have a validation schema covering required fields, types, and value ranges. At minimum include `score`, `max_score`, `reasoning`, and `evidence`, plus every structured-output key the eval emits.
10. Cost/tier/execution-mode fit: for each eval step, read `evaluation/step_config.json` and match `execution_tier` and `declared_execution_mode` to the eval's actual nature. Scripting an objective, contract-anchored check is allowed anytime (no gate); the explicit-user-request + scenario-coverage bar applies only to locking/freezing a saved script. Flag scripted evals coupled to incidental artifact shapes — plan changes alter those; scripts should anchor to stable contracts (`db/README.md` schemas, the report contract). Compare `costs/evaluation/` to `costs/execution/`: eval spend rivaling execution spend means the plan needs fewer/cheaper steps (retire redundant steps, demote tiers, script the extraction).
11. Fail-closed robustness: missing input files, null/empty fields, stale artifacts, and parse errors must all produce a failing score with the failure named in `reasoning`.

For every eval recommendation, show before/after snippets per eval step and state
whether score semantics would change. The Pulse Fixer applies correctness repairs
and waits for approval before semantic changes.

PASS 4 - FIXER HANDOFF
Return proposed Decision text for the Pulse Fixer. State what should change and
why, which files would be touched, whether score/rubric semantics change, and
what future runs should verify.

When you finish, return:
- what workflow/eval evidence you reviewed
- the main eval weaknesses you found
- what you recommend and what is safe for the Pulse Fixer to apply
- any proposed rubric-change Decision entry

The parent records every evaluation-health result as a **Signals / Kizuki** card
using `data-pulse-section="signals"` and `data-module="eval_health"`. A later
verified repair is a separate Improvements / Kaizen Pulse Fixer card.

If you recommend a proposed but not-yet-applied eval change as an open finding, give it a short anchor id only so the parent can record it and a later decision can mark it resolved.

CLOSE-OUT RECOMMENDATIONS
When preparing eval recommendations, scan `builder/improve.html` for open findings that the change would address. Match by intent, not exact wording.

For each matching open finding, propose this close-out text for the Pulse Fixer:
```
Resolved YYYY-MM-DD - <one-line how it was fixed>.
```

Recommend "partially resolved" if only part would be addressed, or "invalid" if
the finding is wrong. Never edit, delete, or rewrite the finding from the reviewer.
