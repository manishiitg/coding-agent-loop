## Org Goals — Source Of Truth And Workflow Alignment

Org goals live in **`pulse/goals.html`**. This file is the CEO's scorecard for the
organization. It is not memory, not a chat note, and not a workflow-internal file.

Use this doc when the user asks to set, change, review, or measure org goals; when creating
or assigning workflows; and when reporting workflow results against business outcomes.

### 1. Get Goals From The User

Treat org goals like real company operating targets. A goal is not complete just because it
has an inspiring outcome. It needs one or more explicit targets that can be inspected later:
baseline/current value, target value, unit, source of truth, owner, deadline, and review cadence.

If the user gives a vague goal, ask only for the missing pieces needed to make it measurable:

- **Outcome** — what should be true when this goal is achieved?
- **Horizon** — by when should it be achieved or formally reviewed?
- **KPI target(s)** — metric name, baseline/current value, target value, unit, direction
  (`increase`, `decrease`, `maintain`, `milestone`), and target date.
- **Source of truth** — which workflow report, db table, external system, or manual update will
  supply the value?
- **Owner** — person accountable for moving or reviewing the target.
- **Contributing workflows** — which existing workflows should move this goal, or is a new
  workflow needed?
- **Cadence** — how often should progress be reviewed?

Prefer numeric targets: revenue, leads, conversion rate, cycle time, error rate, response SLA,
proposals sent, qualified pipeline, retained customers, cost/run, eval score, publish freshness.
If the goal is inherently qualitative, turn it into a concrete milestone/checklist target with
a dated acceptance condition. Do **not** invent quantities. If the CEO has not provided a
number, ask for it or mark the target as `needs-target`.

If the user gives enough detail, do not over-interview. Draft the goal, state any assumptions
briefly, and write the HTML.

Good company-style goal examples:

- "Grow qualified outbound pipeline to **$250k by 2026-09-30**, from **$40k today**, measured
  from `Workflow/outbound/reports/pipeline.html`, owner: Sales Ops, reviewed weekly."
- "Keep proposal quality at **>= 0.90 eval score for 4 consecutive runs** by 2026-07-31,
  measured from evaluation reports, owner: Workflow owner."
- "Reduce daily bidding cost to **<= $0.35/run** while keeping submitted proposals **>= 5/week**,
  measured from workflow cost telemetry and proposal DB, owner: Chief of Staff."

### 2. Goals Must Align Workflows

Every goal should list the workflows that contribute to it. Every recurring workflow should
either:

- contribute to one or more org goals,
- be marked as a supporting/maintenance workflow with a reason, or
- be flagged as unaligned so the CEO can decide whether to retire, redesign, or attach it to
  a goal.

When creating a workflow because of an org goal, make the alignment explicit:

- include the goal name in the workflow objective/success criteria,
- design the workflow report/evaluation/db so it produces the exact KPI value named by the
  goal target,
- add the new workflow to `pulse/goals.html` under the goal's contributing workflows.

Group goal-aligned workflows together so each goal's contributing workflows are easy to see.
The workflows own execution; the goal remains in `pulse/goals.html`.

### 3. Workflow Runs Must Measure Goal Performance

After a workflow run completes, interpret the run against any goals that name that workflow.
This applies to full workflow runs and step runs launched from Chief of Staff chat. Use
concrete evidence from:

- the workflow's own Pulse verdict in `builder/improve.html`,
- the latest run outputs under `runs/iteration-0/<group>/execution/`,
- live/finished reports under `reports/`,
- durable metrics or rows in `db/db.sqlite`,
- relevant workflow learnings or KB notes when they explain the result.

For each relevant goal, report an **Org goal alignment** summary:

- **goal**: the goal title/id from `pulse/goals.html`,
- **target**: the KPI target id/name, baseline/current value, target value, unit, and due date,
- **workflow**: the workflow path/name and group that ran,
- **status**: `on-track`, `at-risk`, `off-track`, or `unknown`,
- **evidence**: the file/table/report/output that supports the status,
- **gap**: what evidence is missing if status is `unknown`,
- **next action**: run again, improve workflow measurement, create a missing workflow, or ask
  the CEO to refine the goal.

Do not invent proxy metrics. If the workflow does not produce the evidence needed for a
goal target, say `unknown` and suggest adding that exact measurement to the workflow.
When the next action is workflow-specific improvement or missing measurement, pass it to the
workflow builder by adding a Chief of Staff recommendation card to that workflow's
`builder/improve.html`. Include the goal/KPI target, alignment verdict, evidence path, gap,
priority, suggested builder action, expected KPI/success-criteria impact, stable `data-cos-rec-id`,
and lifecycle `data-status`. Workflow Pulse and Goal Advisor reply through
`mark_cos_recommendation_status`; Chief of Staff should read those statuses before creating another
recommendation for the same goal/gap. Do not edit the workflow plan/config directly from Chief of
Staff chat.

Update `pulse/goals.html` when a run or Org Pulse pass provides concrete new evidence that
changes the scorecard (status, latest evidence, confidence, freshness/last-reviewed, or history).
Before editing, load `get_reference_doc(kind="org-html")`, preserve existing goal history, and add
a compact history row for the run or pulse pass. If the evidence is incomplete, leave the
scorecard unchanged and report the gap instead.

If no goal names the workflow, classify the run as:

- **supporting/maintenance** — it keeps a goal-critical system healthy, with a short reason;
- **unaligned** — it is recurring work with no goal link and no clear supporting rationale.

Do not silently treat unaligned recurring runs as useful. Surface them so the CEO can attach,
retire, or redesign the workflow.

### 4. HTML Contract For `pulse/goals.html`

Before writing or materially changing `pulse/goals.html`, load and follow
`get_reference_doc(kind="org-html")`. Also follow `get_reference_doc(kind="html-output")`
for the generic self-contained HTML rules.

Before writing, also follow the same safety contract as Org Pulse:

- read `pulse/backup.json` and `pulse/backup/status.json` when present,
- call `get_reference_doc(kind="backup-strategy")`,
- back up org-level artifacts using the workflow-style org backup contract before changing
  `pulse/goals.html`,
- if backup is not configured, set up the zero-config local-git default and create or update
  `pulse/backup.json` plus `pulse/backup/status.json`,
- stop before editing if backup fails.

`pulse/goals.html` must use the Goals skeleton from `org-html`. The page should read as a
durable operating scorecard, not a chat transcript or plain table. Top to bottom:

- header with title, last updated date, review cadence, and evidence freshness,
- one status banner with the overall goal read,
- KPI strip for total goals, on-track, at-risk/off-track, and unknown/missing evidence,
- one structured goal card per goal,
- workflow alignment matrix listing aligned, supporting, and unaligned workflows,
- measurement gaps,
- change history at the bottom, newest first.

Each goal should include:

- title,
- horizon,
- target outcome,
- KPI targets: at least one target with `data-target-id`, baseline/current value, target value,
  unit, direction, due date, source of truth, and owner,
- measurement method and threshold/status rule,
- contributing workflows,
- current status (`on-track` / `at-risk` / `off-track` / `unknown`),
- latest evidence,
- confidence,
- notes/history.

Keep previous goal history unless the CEO explicitly asks to remove it. If a goal changes,
record the change in the history section rather than silently overwriting context.

Use stable ids/classes/data attributes from `org-html` (`data-goal-id`, `data-status`,
`data-workflow`, and the `<!-- GOALS: current scorecard -->` / `<!-- GOAL HISTORY: newest first -->`
anchors) so future Chief of Staff turns and Org Pulse can update the page reliably.

### 5. What Org Pulse Does With Goals

Org Pulse reads `pulse/goals.html`, measures the named workflows against those goals, updates
`pulse/goals.html` as the durable current scorecard when evidence changes, and writes the daily
measured narrative into `pulse/org-pulse.html`.

`pulse/goals.html` is the planned target and current scorecard. `pulse/org-pulse.html` is the
daily measured narrative: what changed, what is drifting, which task findings matter, and what
decision the CEO should make next.
