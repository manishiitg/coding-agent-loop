# Pulse review visibility

The Pulse workspace has three selectable areas: Drift check, Technical Review,
and Strategic Review. Technical Review opens by default. Each selection shows
its summary, coverage where applicable, Markdown reports, checks, and findings
below the selector. Human decisions and finalization remain workflow-wide.
Clearing the review-area filter makes the issue queue and checks cover all areas.

Drift findings appear directly below the Drift check content. If there are no
current drift findings but resolved ones exist, selecting Drift check opens the
Resolved queue. If none were recorded, an explicit empty state points to the
completed-check history above. There is no separate “View drift findings” link.

Technical Review shows each maintenance category and its last recorded review.
Learnings and knowledge-base coverage are shown separately when a
`store_integrity` receipt explicitly identifies that scope in `route_scope`.
A general store review, a Gate decision, or a Pulse tick is not proof of either.
Missing evidence is shown as “No specific review recorded.”

`GET /api/workflow/pulse-reviews` keeps its existing `reviews` response and adds:

- `coverage`: the latest durable focus receipt for each module, focus, and scope,
  including its evidence and issue IDs. This is independent of the recent
  activity window so older learnings reviews remain visible.
- `audits`: the latest 100 non-skipped module outcomes, plus the latest outcome
  for each module when older. Expanded entries show recorded verification,
  changed files, and evidence; they do not infer test success from completion.
- `reports`: existing `technical-review.md`, `strategic-review.md`, and
  `plan-drift-review.md` files under the workflow's `runs/pulse/<run>/` folders.
  Checkpoints without completed audits are included. File modification times
  are labelled “Updated,” distinct from completed-review timestamps.

Reports load through the existing authenticated workspace document API and
render inline in Pulse. They can be collapsed and failed reads can be retried.
The report directory is supplied as the Markdown base path for relative links.
