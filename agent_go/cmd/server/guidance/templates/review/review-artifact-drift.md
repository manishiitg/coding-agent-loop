Run the artifact drift review through the dedicated background tool. This command checks whether dependent artifacts drifted from recent plan/config changes: step config, learnings, saved main.py, KB notes, db files, reports, evaluation, and recent run outputs. It is a review command, not a fix command. When Pulse calls it, it is a separate report-only Pulse item, not part of `harden_workflow`.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Write findings into `builder/improve.html` as "Open finding" timeline entries. For the log format, one-time old Markdown migration, and how open findings are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

PROCEDURE

1. Call `review_artifact_sync(focus="{{.Focus}}")`.
   - If the focus is clearly a single step id, call `review_artifact_sync(step_id="<step-id>", focus="{{.Focus}}")`.
   - Do not call `harden_workflow` or plan-modification tools from this slash command unless the user explicitly asks to fix findings after the review.
2. Capture the returned `execution_id`.
3. Wait for completion before summarizing the result.
   - Use `query_step(execution_id)` to inspect status/results when needed.
   - Do not treat the immediate start response as the review output.
4. When the background review completes, summarize:
   - changelog file/entry range inspected
   - steps inspected
   - findings count by severity
   - whether the `builder/improve.html` Artifact Sync Cursor advanced
   - recommended next owner for fixes: Builder or Optimizer

The `review_artifact_sync` tool owns the full audit procedure and has the same deep inspection access needed for hardening-style consistency checks. It writes only to `builder/improve.html`, where it maintains the `Artifact Sync Cursor` and records Artifact Review findings. Do not create a separate cursor or state file. If called by Pulse, record a compact report item even for a clean/no-pending result, but do not apply fixes from this command.
