## Pulse archive preflight

Use only for the scheduler's conditional `archive-improve-log` stage. This is a
semantic history compaction pass, not a normal Pulse review or a format rewrite.

`builder/improve.html` remains the authoritative current Pulse view. Preserve:

- the complete top dashboard, current metrics, and freshness labels;
- every open finding, user rule, current note, unresolved/unconfirmed decision,
  and unanswered or not-yet-consumed human question;
- every timeline card and recent-run row from the latest 15 calendar days;
- every item without a valid `data-date="YYYY-MM-DD"` (undated history is never
  archived automatically);
- the current Agent handoff and all evidence still needed by a later Pulse.

Move only resolved findings, superseded confirmed decisions, and routine run
rows whose `data-date` is strictly older than 15 calendar days into
`builder/improve-archive/YYYY-MM.html`. Age alone never makes an open or pending
item safe to move. Each archive is a
complete renderable HTML document, never a fragment. Merge an existing month
without duplicates and keep entries newest first. Add or update one compact
Archive Index link (`href="improve-archive/YYYY-MM.html"`) in the active page
with the month, date range, and moved count.

Archive safely: read active and target archive files fully; stage complete
documents as temporary files under `builder/`; verify both are non-empty and
contain html/head/body;
verify every moved item appears exactly once across active plus archive and no
protected/open item moved; only then replace the archive and active files.
Never truncate the original first. If nothing is safely archivable, leave the active
file unchanged and report that plainly. Do not change workflow logic, verdicts,
plans, cadence, reports, or any non-Pulse artifact. Stop after this stage.
