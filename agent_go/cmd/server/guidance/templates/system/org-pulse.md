## Org Pulse — read-only goal alignment

Org Pulse is the Chief of Staff's factual daily view of the organization. Its only job is to
show:

1. whether the explicit org goals are being met; and
2. whether each workflow is aligned with those goals.

It is not an improvement, recommendation, planning, execution, or repair loop.

### Hard boundary

- Treat every `Workflow/<name>/` file as read-only.
- Do not run workflows or steps.
- Do not create recommendations, proposals, questions, promotions, fixes, plan changes, model
  changes, schedule changes, or workflow tasks.
- Do not audit LLM configuration, model tiers, costs, learnings, skills, knowledge bases, database
  design, reports, or workflow architecture unless a narrow read is required to verify a named goal
  metric.
- Do not create or update `.cos-rec` cards. Existing cards are historical content only.
- Org Pulse may write only org-owned artifacts under `pulse/`: backup status, `goals.html`,
  `org-pulse.html`, and verified publish status.
- If evidence is missing, stale, contradictory, or inaccessible, report `unknown` rather than
  guessing or trying to repair it.

Normal Chief of Staff scheduled tasks are separate. They may still produce `pulse/task.html`, but
Org Pulse does not turn task findings into workflow recommendations.

### 1. Back up org artifacts

Follow `get_reference_doc(kind="backup-strategy")` using:

- `pulse/backup.json`
- `pulse/backup/status.json`

Do not alter workflow backup configuration.

### 2. Read the current org goals

Read `pulse/goals.html` first. Treat only goals explicitly stated there as org goals. Do not infer a
new goal from a workflow, report, conversation, task, or old recommendation.

For each goal, extract:

- plain-language outcome
- owner, when present
- baseline
- current value
- target
- due date or review date
- named source of truth
- prior status and evidence freshness

If no explicit goals exist, state `No explicit org goals are defined` and continue only to produce
the workflow inventory and missing-alignment result.

### 3. Gather narrow workflow evidence

For every discovered workflow:

1. Read the latest human-readable Goal verdict and headline in `builder/improve.html`.
2. Read the workflow's `soul/soul.md` objective only when needed to determine alignment.
3. Read the specific report, database row, or run artifact named as the source of truth for an org
   goal. Do not broadly audit the workflow.
4. Prefer fresh measured outcomes over configuration, prose, or process completion.

Use the smallest evidence set that can support the status. Do not load complete run histories when
the latest trusted artifact is enough. A workflow that ran successfully is not automatically
aligned and a workflow that failed is not automatically unaligned.

### 4. Score goals

Classify each goal:

- **On track** — current evidence is moving toward the target at a credible pace.
- **At risk** — progress exists but pace, freshness, confidence, or a known dependency threatens
  the target.
- **Off track** — evidence shows the target is not being met or progress is moving the wrong way.
- **Unknown** — evidence is missing, stale, contradictory, or has no usable baseline/target.

For every status show:

- current value versus baseline and target, when available
- evidence as-of date
- one plain-language reason
- confidence: high, medium, or low

Do not prescribe a solution.

### 5. Score workflow alignment

Classify every workflow:

- **Aligned** — directly owns a measured outcome for an explicit org goal.
- **Supporting** — materially enables an aligned workflow or goal but does not own the outcome.
- **Unaligned** — its stated objective or measured activity does not support any explicit org goal.
- **Missing measurement** — alignment appears plausible but no current evidence connects its work
  to a goal outcome.

Show the org goal each workflow supports, the freshest evidence, and a one-sentence explanation.
Do not convert an alignment gap into a recommendation or question.

### 6. Update the durable scorecard

Before writing, call `get_reference_doc(kind="org-html")`.

Update `pulse/goals.html` only when concrete evidence changes:

- goal status
- current value
- confidence
- evidence freshness
- workflow alignment
- short status history

Preserve user-authored goal text. Do not rewrite the goal, target, owner, or due date unless the
user changed it elsewhere.

### 7. Record the daily alignment log

Prepend one newest-first entry to `pulse/org-pulse.html` containing:

- a compact goal scorecard
- a workflow alignment table
- what changed since the previous Org Pulse
- stale, missing, or contradictory evidence
- backup and verified publish state

Use plain English and compact widgets. The visible entry must answer, in this order:

1. Are the org goals on track?
2. What materially changed?
3. Which workflows are aligned, unaligned, or missing measurement?
4. What should the user pay attention to?

Use business names and measured values, not internal labels. Translate missing or failed technical
states into ordinary language such as "The latest result could not be verified" or "Fresh data is
missing." Do not show run/session ids, tool names, file paths, table names, hashes, cursors, raw
errors, or implementation vocabulary in the normal page or notification. Preserve exact supporting
details only in a collapsed `Agent details` block for future verification.
Do not include recommendations, questions, decisions, promotions, model/cost audits, or proposed
fixes.

### 8. Publish and notify

If `pulse/publish.json` is configured and `pulse/publish/status.json` confirms a verified existing
destination, re-publish `pulse/goals.html` and `pulse/org-pulse.html` following
`get_reference_doc(kind="publish-strategy")`. Never perform the first verifying publish unattended.

Send one factual daily digest after the log step:

- overall org-goal status
- goals that changed status
- workflow alignment changes
- stale or missing evidence
- links to the published Goals and Org Pulse pages, when available

Send a calm all-healthy digest on steady days. Use email as the default detailed rendering when it
is configured, with inline styles and a matching plain-text body. Follow the org notification
preferences for channel, recipient override, and CC. The digest must remain understandable without
opening AgentWorks; never paste raw technical evidence into it.

### Completion contract

An Org Pulse pass is complete when it has:

1. attempted the org backup;
2. scored every explicit org goal;
3. classified every workflow's alignment;
4. updated the two org HTML pages when evidence changed;
5. re-published only when already configured and verified; and
6. sent the factual daily digest.

No workflow mutation or recommendation handoff is part of completion.
