Manually conclude the active experiment. Use shell + file primitives — no API call.{{if .Focus}}

Focus / reason: {{.Focus}}{{end}}

This is the OVERRIDE path. Prefer letting the evaluator agent narrate the system-computed verdict. Use this only when you genuinely believe the heuristic is wrong (large world drift, broken eval, mistaken metric, or to early-conclude an experiment whose direction is obvious).

DISCOVERY
1. Read experiments/active.json. List active experiments and confirm the id with the user.
2. Decide the verdict with the user: kept | reverted | inconclusive | extend.
3. Write the rationale (≤500 chars) and the override reason.

REVERT (only if verdict = reverted)
If verdict=reverted, restore the intervention's files first:
- For each entry in the experiment record's `revertable_diff`, use `diff_patch_workspace_file` to write the saved pre-intervention content back. Use `operation: "create"` for files that didn't exist before, `"replace"` for files that did.

ARCHIVE THE EXPERIMENT
4. Append the experiment record to experiments/history.jsonl as a single JSON line. Set:
   - `status`: "concluded"
   - `concluded_at`: ISO-8601 UTC now
   - `conclusion.verdict`: <chosen verdict>
   - `conclusion.rationale`: <prose rationale>
   - `conclusion.verdict_overridden`: true
   - `conclusion.override_reason`: <user's override reason>
5. Use `diff_patch_workspace_file` to remove the experiment from experiments/active.json (the JSON object's `experiments` array). Re-marshal the file cleanly.

AUDIT
6. Append one line to builder/decisions.jsonl:
   {"id":"<short-id>","ts":"<ISO-8601 UTC>","source":"user","trigger":"exp-conclude","applied_changes":[<paths restored if any>],"linked_experiment_id":"<id>","rationale":"manual conclude: verdict=<v>; <rationale>","target_metrics":[<exp.target_metrics>]}

REPORT
- Final verdict.
- Whether it was archived to history.jsonl.
- If verdict=reverted, list the files restored.
- Decisions entry id.
