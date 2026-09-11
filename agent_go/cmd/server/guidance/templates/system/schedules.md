## Schedule Management (Workshop mode)

For the operational cheat sheet on creating / editing / deleting schedules
(cron syntax and workshop run payload shape), see this section.

- **Tools**: `list_schedules`, `create_schedule`, `create_calendar_schedule`, `update_schedule`, `delete_schedule`, `trigger_schedule`, `get_schedule_runs`.
- To view existing schedules, call `list_schedules`; it includes schedule IDs, type, mode, workshop mode, cron/calendar shape, timezone, enabled state, groups, and recent runtime state. `get_workflow_config` also includes a Schedules section when you are already inspecting broader workflow settings.
- **Entry shape**:
  ```
  { "id": "...", "name": "...", "description": "...",
    "cron_expression": "0 9 * * 1-5", "timezone": "UTC",
    "enabled": true, "trigger_payload": {},
    "pulse_mode": "basic", "pulse_mode_reason": "Routine daily processing needs backup and a summary; no review is needed on every occurrence.",
    "group_names": ["confida-prod"],
    "mode": "workshop", "workshop_mode": "run" }
  ```
  Fields: `id` (auto-assigned), `name` (display label), `description` (optional), `cron_expression` (standard 5-field cron), `timezone` (IANA tz e.g. `America/New_York`), `enabled` (bool), `trigger_payload` (arbitrary JSON passed to the run), `group_names` (required array of one or more explicit group names from `variables/variables.json`), `mode` (`workshop` for workflow schedules), `workshop_mode` (`run` for normal recurring workflow runs).
- Schedule management is available in **Workshop mode**. If the user asks in Run mode, tell them to switch.

### Two schedule types: cron vs calendar

Every schedule in `workflow.json` has a `schedule_type` — `"cron"` (default) or `"calendar"`. They are stored side by side under the same `schedules` key; the difference is *when* they fire.

- **`cron`** — a repeating pattern that fires forever on a cadence (`create_schedule`, `cron_expression`). Use for "every weekday at 9 AM", "every 30 minutes", "first of the month". This is the default; *Writing messages for scheduled runs* below applies to cron schedules.
- **`calendar`** — a fixed list of specific dated runs, each firing exactly once (`create_calendar_schedule`, `calendar_items`). Use when the user gives concrete dates/times instead of a recurring rhythm — e.g. a full-month Instagram content calendar, a launch sequence, a one-off batch on three specific days. There is no `cron_expression`; the scheduler registers **one job per future `calendar_item`** and each item fires once at its date+time, then is done.

**Choosing:** if the user describes a *rhythm* ("every…", "daily", "weekly") use cron; if they enumerate *dates* ("on the 3rd, 7th, and 12th", "post these on these days") use calendar. When in doubt, ask whether the runs repeat or are a fixed set of dates.

**`create_calendar_schedule` payload:**

```
{ "name": "March content calendar", "timezone": "Asia/Kolkata",
  "pulse_mode": "full", "pulse_mode_reason": "Each infrequent launch batch creates new outcome evidence that warrants review.",
  "group_names": ["group-1"], "mode": "workshop", "workshop_mode": "run",
  "calendar_items": [
    { "date": "2026-03-03", "time": "09:00", "description": "Optional note" },
    { "date": "2026-03-07", "time": "18:30" }
  ] }
```

- `calendar_items` (required): each needs `date` (`YYYY-MM-DD`) and `time` (`HH:MM`), both interpreted in the schedule's `timezone`. `description` is an optional per-item note; `messages` is an optional per-item message queue.
- `timezone` (required, IANA — e.g. `Asia/Kolkata`, not `IST`) and `group_names` (required) work exactly as for cron schedules.
- **Mode is the same as cron**: workflow schedules use `mode="workshop"`. Supply per-item `messages` or a top-level default `messages` array when the default full-workflow run instruction is not specific enough.
- Past-dated items are skipped — only future items get registered. To change a calendar schedule, update its `calendar_items` (add/remove dates); editing tools (`update_schedule`, `delete_schedule`, `trigger_schedule`, `get_schedule_runs`) work on calendar schedules too.

### How workflow schedules execute

Workflow schedules always use the workshop builder execution path. Do not create direct `mode="workflow"` schedules; legacy manifests with that value are normalized to workshop execution.

- **Run** (`mode=workshop`, `workshop_mode=run`) — LLM-driven execution. Prefer an empty queue plus `group_names`/`route_selections` for durable workflow behavior: canonical steps receive their normal learning, validation/retry, repair, and Pulse attribution lifecycle. Direct messages remain valid for genuinely schedule-specific conversation, but require `direct_messages_reason` and do not automatically gain that step-level lifecycle.

**Default mode rule:** create workflow schedules with `mode="workshop"`. New schedules should never use `mode="workflow"`.

**Pulse after scheduled runs**: Every cron and calendar schedule must persist `pulse_mode` (`off`, `basic`, or `full`) and a non-empty `pulse_mode_reason`. New schedules cannot inherit. Updates must leave both fields complete; when changing the mode, supply a fresh reason with it. Legacy workflows can still load until the explicit-schedule-Pulse contract migration decides every schedule, including disabled entries. The workflow Pulse toggle only affects legacy inherited schedules; it does not override explicit policies. Do not create `pulse_review_only` or a separate Goal Advisor schedule. `/strategy-auditor` is the one-off review command; `/goal-advisor` is its compatibility alias.

**Choosing `pulse_mode`**: Before adding a schedule, inspect the existing schedules and decide how often this workflow needs Pulse. Consider token cost, route purpose, frequency, evidence maturity and retention. Prefer reviewing multiple runs together when their combined evidence makes a better review. Record the intended review cadence/window and verified cross-route coverage in the reason; choose among existing schedule timings rather than silently adding a review cron. Prefer `basic` for frequent routine operations: backup, report publication and run-summary notification continue, while Gate, drift review, reviewers and Fixer are skipped. Choose `full` when review cost is justified by this schedule's risk, evidence and cadence; external actions or durable writes alone do not require Full after every run. Use `off` when skipping all post-run backup/publication/notification is intentional, including an existing owner-disabled policy; explain that consequence. Do not choose Off merely to reduce review cost when Basic is appropriate. If a Basic schedule relies on another Full schedule, name it and verify that its review actually reads this route's evidence—one route's Full run does not automatically cover all routes.

**Persist the reason**: `pulse_mode_reason` must explain this schedule's purpose/frequency, why its selected review level is appropriate, and any verified coverage dependency (schedule ID or route). Store the explanation on the schedule, not only in chat. For example: `pulse_mode="basic", pulse_mode_reason="Runs approved-queue processing four times daily; each run needs backup and a summary, but routine unchanged queue processing does not warrant review/repair every time."` Do not copy this example without checking the actual schedule.

### Back up scheduled workflows

Scheduled runs execute unattended and accumulate state (`workflow.json`, `planning/`, `knowledgebase/`, `learnings/`, `db/`, reports) that otherwise lives only on local disk. **Whenever you set up a recurring schedule, decide its backup coverage** against the existing owner policy and Pulse cadence. For enabled backup, arrange off-device persistence; do not override an approved disabled/off policy. Load `read_skill(skills=[{"name":"builder-reference","path":"references/backup-strategy.md"}])`, follow it once to initialise the workflow's backup destination, and persist the result in `workflow.json.backup`.

- When backup is enabled by the chosen policy, set `workflow.json.backup.enabled=true`, `mode="agent"`, `triggers.after_scheduled_run=true`, and a `destinations` entry for each backup target (git/github for config, R2/S3/B2/HuggingFace for large artifacts as needed).
- After each backup attempt, write `backup/status.json` with the destination results, timestamps, summary, and errors. Do not put changing backup status in `workflow.json`.
- If you rely on the default full-workflow message, the auto-notification after `run_full_workflow` will still ask the builder to honor `workflow.json.backup` and write `backup/status.json`.

Honor an existing user-approved disabled/off backup policy without asking again. If backup coverage has not been decided, resolve it while configuring the schedule; do not add an interactive confirmation to an unattended run.

### Writing messages for scheduled runs

`messages` is an ordered queue of strings sent to the workshop LLM one-by-one as user turns. The LLM completes all tool calls triggered by message N before message N+1 is sent.

- Write each message as a plain instruction, like you would type in chat: `"Run the full workflow"`, `"Generate the final report"`.
- **Route-backed mode (default)**: select planned work through `group_names` plus optional `route_selections`, and keep `messages` empty. Use this for durable workflow behavior so one canonical plan owns its lifecycle.
- **Direct-sequence mode (supported exception)**: use one or more messages when the conversation itself is genuinely schedule-specific and should not become reusable plan behavior. Set `direct_messages_reason` with the concrete tradeoff. These turns use the workshop execution path with Run authority (`workshop_mode="run"`), but are not canonical steps, so step learnings, validation/retry, repair, and Pulse attribution are weaker or unavailable unless the sequence explicitly invokes planned work.
- Never choose solely from message length. Compare behavior, inputs/outputs, external side effects, approval boundaries, failure behavior, and expected reuse.
- Read `variables/variables.json` for available group names and include them explicitly in the message if needed.

**CRITICAL — schedules run unattended; messages must never require human input:**

- Scope unattended work to existing authorization. Supply known human answers explicitly; a schedule is not permission to invent approval or change workflow design.
- Provide all required parameters upfront in the message (group names, run folders, step IDs) so the agent never needs to ask.
- Use only configured, authorized safe defaults. If required input or approval is missing, leave the affected action unexecuted and report it or persist a pending decision through the available decision tools; do not guess.
- Never include open-ended questions or `"let me know"` style instructions.
- **Bad**: `"Run the workflow and ask me which steps to optimize"`.
- **Good**: `"Run the planned daily-processing route for group-1. Process only explicitly approved items; leave pending items untouched and report missing inputs."`

Pulse module cadence is not encoded in schedule JSON. Pulse Gate stores module state in `db/db.sqlite` and decides which modules are due after each normal run.

Do not create new `workshop_mode="optimizer"` schedules. Existing saved legacy
values are handled by migration/backend compatibility; new continuous
improvement uses normal Run mode plus Pulse.

## Goal measurement setup
When setting up Pulse for a new workflow, use get_workflow_command_guidance(kind="setup-goals") before enabling it. Propose outcomes, one primary metric, supporting metrics and boundaries using existing context. Ask only unresolved user decisions. Connect collection to producing runs or appropriate delayed refreshes, independently of Pulse review cadence. Existing workflows without configured metrics keep running; recommend /setup-goals without repeatedly raising technical bugs for missing setup.
