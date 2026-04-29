Abort the active experiment and revert its intervention. Use shell + file primitives — there is no dedicated tool for this and you should NOT call any HTTP API.{{if .Focus}}

Reason: {{.Focus}}{{end}}

DISCOVERY
1. Read experiments/active.json. List active experiments. If multiple, ask the user which one to abort.
2. Confirm the user wants to abort (this rolls back the intervention via the captured revertable_diff).

REVERT THE INTERVENTION
The experiment record carries `revertable_diff` — a JSON envelope listing each intervention path with its pre-intervention content. To revert:
1. For each entry in `revertable_diff.changes` (or whatever the field is named on the record): use `diff_patch_workspace_file` to restore that path's pre-intervention content. Use `operation: "create"` if the file didn't exist before the experiment, otherwise `"replace"` with the saved content.
2. Verify each restored file matches the saved content before moving on.

ARCHIVE THE EXPERIMENT
3. Append the experiment record to experiments/history.jsonl as a single JSON line. Set:
   - `status`: "aborted"
   - `concluded_at`: ISO-8601 UTC now
   - `conclusion.verdict`: omit or null (aborts have no verdict)
   - `conclusion.rationale`: the user's reason
4. Use `diff_patch_workspace_file` to remove the experiment from experiments/active.json (the JSON object's `experiments` array). Re-marshal the file with the entry removed; do NOT just delete the line.

AUDIT
5. Append one line to builder/decisions.jsonl:
   {"id":"<short-id>","ts":"<ISO-8601 UTC>","source":"user","trigger":"exp-abort","applied_changes":[<paths restored>],"linked_experiment_id":"<id>","rationale":"<user's reason>"}

REPORT
- Confirm the experiment is gone from active.json and present in history.jsonl with status=aborted.
- List the files restored.
- Echo the decisions.jsonl entry id.
