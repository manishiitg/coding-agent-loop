Set up recurring workflow runs with dynamic Pulse and Goal Advisor.

Write to `builder/improve.html` — the durable Pulse and advisor log. For the log/HTML format, load `get_reference_doc(kind="review-improve-log")`.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

## Mental model

Pulse is the recurring maintenance system. After every scheduled run, Pulse Gate decides which modules are due:

- harden
- artifact review
- report health
- learning health
- knowledgebase health
- DB health
- cost/LLM/time reporting
- Goal Advisor

Goal Advisor is not a separate schedule anymore. It is the strategic Pulse module that runs only when Gate sees enough evidence or user/Chief-of-Staff input. It asks whether the workflow is pursuing the right strategy for `soul.md`, whether the current plan is capped even when it runs correctly, and whether an experienced operator would suggest something outside the current plan.

## Goal

Set up this workflow so it can keep improving from scheduled runs:

1. Enable Pulse: call `update_workflow_config(post_run_monitor=true)`.
2. Create or update one normal workshop Run-mode schedule for recurring execution.
3. Do not create a separate optimizer/Goal Advisor schedule. If an old optimizer schedule exists for this same purpose, disable it after recording why in `builder/improve.html`.

## Discovery

1. Call `get_workflow_config` and inspect schedules.
2. If candidate schedules exist, call `get_schedule_runs` on the most relevant ones.
3. Read `soul/soul.md` for objective and success criteria.
4. Read `variables/variables.json` for valid group names.
5. Read `builder/improve.html` for Workflow Profile, Maintenance Radar, recent Bug/Goal verdicts, open findings, prior Goal Advisor decisions, human input cards, and Chief of Staff recommendations.
6. Read `planning/changelog/` if present. Recent plan/config changes increase regression risk and can make Pulse Gate select deeper modules on the next run.
7. If success is not defined, call `get_workflow_command_guidance(kind="define-success")` and follow it before scheduling.

## Schedule strategy

Prefer updating or reusing a good existing run schedule. Only create a new schedule when no existing schedule already serves the purpose.

The run cadence should create useful evidence without wasting runs:

- hourly or more frequent only when the workflow naturally has fresh inputs that often
- daily for normal active workflows
- weekdays for workday operational workflows
- weekly for slower workflows where evidence changes slowly
- calendar schedules when the user gives fixed dates/times instead of a recurring rhythm

Pulse decides its own internal module cadence from evidence stored in `db/db.sqlite`; do not add new schedule JSON fields for module cadence.

## Run schedule

Create or update a schedule for normal recurring execution with:

- `mode="workshop"`
- `workshop_mode="run"`
- valid `group_names`
- a clear name and description marking it as the primary recurring run schedule
- a single unattended scheduled message that names the exact groups and tells Run mode to call `run_full_workflow(group_name="<group>")` for each configured group

The scheduled message must say:

- do not ask for confirmation
- proceed autonomously
- use the exact configured group names
- run the full workflow for each configured group
- use default evaluation behavior so Pulse has fresh run/eval evidence
- stop after the configured groups have run and report status plainly

## Existing optimizer schedules

If you find an old optimizer-mode schedule that clearly exists only for the previous Auto Improve / Goal Advisor loop:

1. Disable it with `update_schedule(enabled=false)`; do not delete it unless the user asked.
2. Record a short Decision card in `builder/improve.html`: Goal Advisor moved into Pulse Gate; this old schedule is paused to avoid duplicate strategy passes.
3. Leave manually-authored optimizer schedules alone when they are clearly for another custom job.

## Persistent log

Create or update `builder/improve.html`.

- Seed or refresh the Workflow Profile from `soul.md`.
- Record the setup/change as a readable Decision card.
- Make clear that Pulse Gate will decide when deeper modules, including Goal Advisor, are due.
- If nothing changed, record a short confirmation rather than creating noise.

## Final report

Reply with:

- Pulse enabled status
- run schedule created/updated/reused
- group scope
- cadence and why
- old optimizer schedule disabled/reused/left alone
- any missing evidence or user decisions
