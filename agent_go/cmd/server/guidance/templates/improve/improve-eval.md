Review and improve evaluation/evaluation_plan.json. Eval changes are special-cased in the framework: they change WHAT is measured, not the workflow's behavior. So eval changes do NOT open experiments — but they DO have rules to follow because changing the rubric mid-stream invalidates trajectory baselines and active experiments. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your eval findings and applied decisions when you finish.{{if .Focus}}

Focus on: {{.Focus}}.{{end}}

PASS 0 — FRAMEWORK PRECHECK + ACTIVE-EXPERIMENT GUARD
1. Read builder/improve.md. If there is no "## Workflow Profile" section, stop and redirect: "Run /improve-setup-framework first."
2. Read <workflow>/planning/metrics.json. If absent or empty AND the Workflow Profile declares business-context accumulation OR a frozen/ratchet plan, stop and redirect to /improve-setup-framework. Plain mutable+exploratory workflows may proceed without metrics.
3. **Active-experiment guard.** Read experiments/active.json. For each experiment whose status is 'measuring' or 'evaluating', look at its target_metrics and resolve each metric in metrics.json. If any of those metrics is sourced from an eval step you might be about to edit (source.type=eval_step, source.id matches an eval step id), STOP and tell the user: "experiment <id> is currently measuring metric <m> against eval step <step_id>. Editing that step now would change its rubric mid-stream and invalidate the experiment's baseline. Either wait for the experiment to conclude (or /exp-abort it) before editing this eval step, or focus this command on eval steps not under active measurement." Proceed only with eval steps that are NOT under measurement.

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
Propose improvements in these categories:
1. **Coverage**: does each important success criterion have a clear eval step?
2. **Directness**: is the eval checking the actual desired outcome, or only proxies?
3. **Determinism**: are any eval steps too vague, subjective, or hard to reproduce?
4. **Redundancy**: are multiple eval steps measuring the same thing with little added value?
5. **Thresholds / scoring**: are pass/fail thresholds or scores aligned with the stated success criteria?
6. **Reality check**: if outputs you read in Pass 2 show obvious failure or success, does the eval report reflect that honestly?

Show ALL proposed changes as a diff (before/after snippets per eval step) before editing. Ask whether to apply all, some, or none. Don't edit evaluation/evaluation_plan.json until I confirm.

PASS 4 — RECORD THE CHANGE (every eval edit)
After applying any change to evaluation/evaluation_plan.json:
1. Append an entry to decisions.jsonl using diff_patch_workspace_file. Format (one JSON object per line):
   {"id": "<short-id-or-uuid>", "ts": "<ISO-8601 UTC>", "source": "agent", "trigger": "improve-eval", "applied_changes": ["evaluation/evaluation_plan.json"], "rationale": "<one-line summary of what changed and why>", "target_metrics": [<list of metric ids whose source.id points to edited eval steps, if any>]}
2. The decisions entry serves as a "rubric change" marker. Trajectory chart renderers should break the line at this timestamp because pre-change and post-change scores aren't comparable.

When you finish, update builder/improve.md with:
- what workflow/eval evidence you reviewed (especially output-vs-rubric mismatches from Pass 2)
- the main eval weaknesses you found
- which eval steps you skipped because they're under active measurement (per Pass 0 guard)
- what you recommended and what was applied
- the decisions.jsonl entries you appended (rubric-change markers)
