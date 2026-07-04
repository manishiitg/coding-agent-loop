Run the artifact drift review through the dedicated background tool. This command checks whether dependent artifacts drifted from recent plan/config changes: step config, learnings, saved main.py, KB notes, db files, reports, evaluation, and recent run outputs. It also flags eval-coverage drift — a changed/added success criterion with no eval step measuring it, an orphaned eval step, or an eval step that only duplicates `pre_validation`/Pulse triage — with an `Eval fix` owner label. It is a review command, not a fix command. When Pulse calls it, it is a separate report-only Pulse item, not part of `harden_workflow`.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Write findings into `builder/improve.html` as "Artifact Review" / "Open finding" timeline entries with the `Artifact drift` action label. For the log format, one-time old Markdown migration, classification chips, and how open findings are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

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
   - how many changelog entries were marked `artifact_review.done=true`
   - recommended next owner for fixes: Builder or Optimizer

The `review_artifact_sync` tool owns the full audit procedure and has the same deep inspection access needed for hardening-style consistency checks. It writes the human-facing report to `builder/improve.html`, maintains the `Artifact Sync Cursor`, and uses `mark_changelog_artifact_reviewed` to stamp fully inspected changelog entries with `artifact_review.done=true`. Do not edit or delete changelog files directly, and do not create a separate cursor or state file. If called by Pulse, record a compact report item even for a clean/no-pending result, but do not apply fixes from this command.
