Run the artifact drift review through the dedicated background tool. This command checks whether dependent artifacts drifted from recent plan/config changes: step config, learnings, saved main.py, KB notes, db files, reports, evaluation, and recent run outputs. It also flags eval-coverage drift with an `Eval fix` owner label. It is a review command, not a fix command. When Pulse calls it, it is separate from Bug Review; the parent Pulse Fixer applies verified repairs.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

The background reviewer is strictly read-only. After it returns, the parent workshop agent writes findings into `builder/improve.html` as "Artifact Review" / "Open finding" timeline entries with the `Artifact drift` action label. For the log format, one-time old Markdown migration, classification chips, and how open findings are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

Load `get_reference_doc(kind="assumption-audit")`. While tracing changed plan/config surfaces, check whether dependent learnings, KB, DB, report, eval, or code preserved an old architecture/tactic assumption after the plan evolved. Report that drift explicitly and keep a consequential unresolved restriction under Pulse's Assumptions challenged.

PROCEDURE

1. Call `review_artifact_sync(focus="{{.Focus}}")`.
   - If the focus is clearly a single step id, call `review_artifact_sync(step_id="<step-id>", focus="{{.Focus}}")`.
   - Do not call mutation or plan-modification tools from this review unless the user explicitly asks to fix findings afterward.
2. Capture the returned `execution_id`.
3. Do not babysit the review with `sleep`, repeated `list_executions`, or repeated `query_step` calls.
   - Use `query_step(step_id="review-artifact-sync", execution_id="<returned execution_id>")` at most once for an immediate status/result check.
   - If it is still running, stop and rely on `[AUTO-NOTIFICATION]` to resume when the review completes.
   - Do not treat the immediate start response as the review output.
4. When the background review completes via `[AUTO-NOTIFICATION]` or a one-off result check, summarize:
   - changelog file/entry range inspected
   - steps inspected
   - findings count by severity
   - the current and proposed `builder/improve.html` Artifact Sync Cursor
   - exact changelog files and zero-based entry indexes proposed for marking
   - recommended next owner for fixes: Workshop, Pulse Fixer, Goal Advisor, or deliberate eval/report improvement
5. As the parent writer, verify the review package is internally consistent, then:
   - append one compact Artifact Review item to `builder/improve.html`
   - advance the Artifact Sync Cursor only through the last fully inspected entry
   - call `mark_changelog_artifact_reviewed` for the exact inspected/cursor-backfilled entry indexes returned by the reviewer
   - do not mark blocked, skipped, or inferred entries

`review_artifact_sync` owns only evidence collection and drift judgment. It cannot write files or mark state. The parent owns the human-facing report, Artifact Sync Cursor, and `mark_changelog_artifact_reviewed` call. Do not edit or delete changelog files directly, and do not create a separate cursor or state file. This command records review results but does not apply the artifact fixes it recommends.
