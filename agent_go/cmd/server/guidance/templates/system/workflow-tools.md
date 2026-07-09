## Workshop & Workflow Tools — Full Reference

The workshop chat agent has access to a broad set of workflow-management
tools. The inline system prompt now carries only a one-line-per-category
cheat sheet; this skill is the deep reference with full signatures,
parameters, when-to-use rules, and gotchas.

If you need to confirm an exact parameter shape that isn't documented
here, call `get_api_spec(server_name="workflow", tool_name="...")` — that
returns the live JSON schema for the tool.

## Step Execution & Inspection

- **`execute_step(step_id, group_name, instructions?, human_input?, tier?, message_sequence_restart?)`** — Start a single step in the background; returns `execution_id`. In Workshop mode this is the primary way to test one step after adding or editing it. Execution uses `iteration-0`. For `message_sequence` steps, pass `human_input` to resume with one new user message, or `message_sequence_restart=true` to start from scratch. For `human_input` steps, `human_input` is used as the response. For other executable steps, `human_input` is high-priority custom context.
- **`execute_step(step_id, group_name, fast_path_only=true)`** *(Workshop mode only)* — Run the step's saved Python `learnings/{step-id}/main.py` directly with the same workflow env, args, output folder, and validation behavior as a real workflow run. Never falls back to LLM. Used to quickly test `scripted` main.py patches.
- **`query_step(step_id, tool_call_id?)`** — Live status check for a running single step. Resolves the latest execution for that step and shows execution status plus structured MCP tool calls and tool-call details captured so far. For coding-CLI providers, terminal/TUI progress does not appear as MCP tool calls — but when the step runs in tmux, query_step **inlines the latest lines of the live terminal pane** and also returns the tmux session name + a `tmux capture-pane -pt <session> -S -200` command for deeper history (same session as the UI terminal).
- **`debug_step(step_id, iteration, group_name)`** — Rich insights: learning status, validation result, log paths.
- **`list_executions(status_filter?)`** — List all background executions.
- **`stop_step(execution_id)` / `stop_all_executions()`** — Cancel running steps.
- **`run_in_background(name, instruction)`** — Spawn an independent background agent with the same tools.
- **`run_goal_advisor_review(pulse_run_id?, focus?)`** — Spawn the dedicated Goal Advisor background agent. Use this when Pulse Gate selects the strategic advisor module; the parent Pulse turn should capture the returned `execution_id`, wait with `query_step(step_id="goal-advisor", execution_id="<returned execution_id>")`, then record `mark_pulse_module_result`. Do not do the expensive strategy review inline in the parent Pulse turn.
- **`run_full_workflow(group_name, human_inputs?, route_selections?, disable_eval?)`** — Execute the complete workflow (all steps) for a single variable group in background. Always uses `iteration-0` and starts from the beginning. If the selected path has `human_input` steps, provide `human_inputs` (object mapping step_id to response string). For deterministic routers, pass `route_selections` keyed by routing step ID, with each value as a `route_id` or unique `next_step_id`. Pass `disable_eval=true` only when the user explicitly wants to skip the automatic evaluation pass. Returns `execution_id`.

## Step Config & Analysis (Workshop mode)

- **`update_step_config(step_id, ...)`** — Update servers, tools, skills, learning settings, execution mode, LLMs, locks, review notes, and description review state. For eval steps this writes to `evaluation/step_config.json`.
- **`harden_workflow(group_name?, focus?)`** — Reliability repair. Always reads `iteration-0` eval reports and execution outputs. Pass `group_name` to scope to one group, or omit it to analyze all groups under `iteration-0`. Use when the path is otherwise sound but local step behavior, validation, artifact shape, config, learnings, KB/db/report/eval wiring, or deterministic invariants are broken. It patches `main.py` only for `scripted` steps and deletes stale `main.py` files for `agentic` steps. **Precondition: call `get_reference_doc(kind="optimize-playbook")` first.**
- **Objective + success criteria** — Edit `soul/soul.md` directly via shell (fill in the `## Objective` and `## Success Criteria` sections). `soul.md` is the canonical source; `plan.json` no longer stores these fields. No dedicated tool — use `diff_patch_workspace_file` or a shell heredoc.
- **Notification preference** — When the user tells you *when/what they want to be alerted about* for this workflow (e.g. "include the eval score in every run summary", "only WhatsApp me when it breaks, never on recovery", "always include the Pulse log link", "don't notify me at all"), capture it verbatim-in-intent as a `## Notifications` section in `soul/soul.md` (same shell/`diff_patch` edit as above). The post-run monitor reads that section and obeys it, overriding its default every-run summary policy. Keep it short and plain-language — it's an instruction to the monitor, not config. If the user hasn't said anything, leave the section out and the monitor keeps the default: one compact run summary after every run, with transitions clearly marked.
- **Goal Advisor plan-change proposals** — Material strategy/path changes use the existing report interaction flow, not a separate replan tool. In scheduled Pulse, use `run_goal_advisor_review` so the dedicated background advisor does the heavy strategic review. It may create or refresh `create_human_input_request(source="goal_advisor", input_id="plan-proposal-...", options=[approve,reject,defer], context="<proposal + exact intended edits + rationale + expected impact + risk + evidence>")`. A later Pulse run may apply an approved proposal with normal plan modification/config/eval/report tools, call `mark_human_input_consumed`, and remove or replace the matching visible question card in `builder/improve.html` with a short outcome. In active manual workshop chat, apply a bounded evidence-backed plan change directly only when the user is asking for improvement and the evidence is strong.
- **`review_workflow_results(iteration?, group_name?, focus?)`** — Read-only outcome review: checks whether a real run is achieving the objective and success criteria, and whether the evaluation actually measures them properly.
- **`review_workflow_timing(iteration?, group_name?, focus?)`** — Read-only latency review: finds the slowest groups/steps/tools/LLM calls and recommends faster descriptions, fewer handoffs, safer step merges, or plan changes.
- **`review_workflow_costs(iteration?, group_name?, focus?)`** — Read-only cost review: finds the biggest cost drivers and recommends cheaper models, fewer retries/handoffs, better descriptions, or plan changes without sacrificing success criteria.
- **`get_cost_summary(run_folder?)`** — Token usage and cost breakdown.

## Read-Only Info

- **`get_step_prompts(step_id, attempt?, iteration?)`** — System prompt and user message for a step.
- **`get_workflow_config`** — Inspect the workflow's current MCP servers, selected skills, available secrets, LLM config, and schedules. Use this instead of `cat workflow.json` when you need the full workflow config. For the global installed skill catalog, use `list_skills`.
- **`get_llm_config`** — Per-step LLM overrides.
- **`get_workflow_command_guidance(kind="review-artifact-drift", focus?)`** *(Workshop only)* — Canonical artifact drift audit after material plan/config changes. Checks unreviewed `planning/changelog/` entries against learnings, saved `main.py`, KB, db, reports, and eval wiring. In Pulse it runs as its own report-only Artifact Review item, separate from `harden_workflow`. Writes its cursor/report in `builder/improve.html` and uses `mark_changelog_artifact_reviewed` to stamp inspected entries with `artifact_review.done=true`; do not edit/delete changelog files directly or create a new state file.

## Plan Modification (Workshop mode)

- **Create steps**: `create_plan`, `add_regular_step`, `add_message_sequence_step`, `add_human_input_step`, `add_todo_task_step`, `add_routing_step`, `delete_plan_steps`, `cleanup_orphan_step_configs`.
- **Update steps**: `update_regular_step`, `update_message_sequence_step`, `update_human_input_step`, `update_routing_step`, `update_todo_task_step`.
- **Todo task routes**: `add_todo_task_route`, `update_todo_task_route`, `delete_todo_task_route`. For todo_task routes, choose one pattern per route: inline `sub_agent_step` for a route-specific agent, or `orphan_step_ref` to reuse a shared orphan step already allowlisted via `shared_with.orchestrator_ids`. Do not set both.
- **Validation**: `update_validation_schema`.

## Variables & Config

- **`update_variable(action, name?, value?, description?)`** — Add, update, or delete a variable.
- **`add_group` / `update_group` / `delete_group`** — Manage variable groups.
- **MCP servers workflow**:
  1. `get_workflow_config` to inspect which servers are currently selected.
  2. `update_workflow_config(add_servers=["server-name"])` selects an **already-registered** server into the workflow. **Do NOT edit `workflow.json` manually.**
     - To **register a new server first** (so it can be selected), use `add_mcp_server(name, protocol="stdio"|"sse"|"http", ...)`: for a stdio server give `command` + `args` (+ optional `env`, `working_dir`) — e.g. an npx-launched server is `command="npx", args=["-y","<package>"]`; for SSE/HTTP give `url`. It registers a user-defined server and triggers discovery; then select it with `add_servers`.
  3. Optional workflow-level allowlist: `update_workflow_config(add_tools=["server:*"])` or `add_tools=["server:tool_name"]`. Tool entries must reference selected workflow servers.
  4. `update_step_config(step_id, servers=["server-name"], tools=["server:tool_name"])` to scope specific servers/tools to a step.
- **Browser workflow**:
  1. Pick the workflow mode with `update_workflow_config(browser_mode="none"|"headless"|"cdp"|"playwright")`.
  2. For `agent_browser` steps, enable `workspace_browser:agent_browser` via `update_step_config(enabled_custom_tools=[...])` and attach the matching runtime skill with `enabled_skills=["agent-browser"]`.
  3. For Playwright steps, select the Playwright MCP server/tools and attach `enabled_skills=["playwright"]` when the step needs the Playwright operating rules.
- **`update_workflow_config(add_servers?, remove_servers?, add_tools?, remove_tools?, add_skills?, remove_skills?, add_secrets?, remove_secrets?, browser_mode?, run_retention_count?)`** — Update workflow MCP servers, workflow-level MCP tool allowlist, skills, secrets, browser mode, or run/eval backup retention.

## Schedule Management (Workshop mode)

For the operational cheat sheet on creating / editing / deleting schedules
(cron syntax and workshop run payload shape), see this section. For the
multi-agent-only schedule cron flow, see
`get_reference_doc(kind="schedule-management")` instead.

- **Tools**: `list_schedules`, `create_schedule`, `create_calendar_schedule`, `update_schedule`, `delete_schedule`, `trigger_schedule`, `get_schedule_runs`.
- To view existing schedules, call `list_schedules`; it includes schedule IDs, type, mode, workshop mode, cron/calendar shape, timezone, enabled state, groups, and recent runtime state. `get_workflow_config` also includes a Schedules section when you are already inspecting broader workflow settings.
- **Entry shape**:
  ```
  { "id": "...", "name": "...", "description": "...",
    "cron_expression": "0 9 * * 1-5", "timezone": "UTC",
    "enabled": true, "trigger_payload": {},
    "group_names": ["confida-prod"],
    "mode": "workshop", "workshop_mode": "run" }
  ```
  Fields: `id` (auto-assigned), `name` (display label), `description` (optional), `cron_expression` (standard 5-field cron), `timezone` (IANA tz e.g. `America/New_York`), `enabled` (bool), `trigger_payload` (arbitrary JSON passed to the run), `group_names` (required array of one or more explicit group names from `variables/variables.json`), `mode` (`workshop` for workflow schedules), `workshop_mode` (`run` for normal recurring workflow runs).
- Schedule management is available in **Workshop mode**. If the user asks in Run mode, tell them to switch.

### Two schedule types: cron vs calendar

Every schedule in `workflow.json` has a `schedule_type` — `"cron"` (default) or `"calendar"`. They are stored side by side under the same `schedules` key; the difference is *when* they fire.

- **`cron`** — a repeating pattern that fires forever on a cadence (`create_schedule`, `cron_expression`). Use for "every weekday at 9 AM", "every 30 minutes", "first of the month". This is the default; everything in *Three ways to schedule* and *Writing messages* below applies to cron schedules.
- **`calendar`** — a fixed list of specific dated runs, each firing exactly once (`create_calendar_schedule`, `calendar_items`). Use when the user gives concrete dates/times instead of a recurring rhythm — e.g. a full-month Instagram content calendar, a launch sequence, a one-off batch on three specific days. There is no `cron_expression`; the scheduler registers **one job per future `calendar_item`** and each item fires once at its date+time, then is done.

**Choosing:** if the user describes a *rhythm* ("every…", "daily", "weekly") use cron; if they enumerate *dates* ("on the 3rd, 7th, and 12th", "post these on these days") use calendar. When in doubt, ask whether the runs repeat or are a fixed set of dates.

**`create_calendar_schedule` payload:**

```
{ "name": "March content calendar", "timezone": "Asia/Kolkata",
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

> The cron flow for **multi-agent chat** schedules (`multiagent-schedules.json`, edited via shell) is separate and cron-only — see `get_reference_doc(kind="schedule-management")`. Calendar schedules are a **workflow-schedule** feature and live in `workflow.json`.

### How workflow schedules execute

Workflow schedules always use the workshop builder execution path. Do not create direct `mode="workflow"` schedules; legacy manifests with that value are normalized to workshop execution.

- **Run** (`mode=workshop`, `workshop_mode=run`) — LLM-driven execution with per-step notifications. `messages` is optional; if omitted, the scheduler sends a default full-workflow run instruction. Prefer an explicit message when you need group-specific wording, backup instructions, or strict unattended behavior.
- **Optimize** (`mode=workshop`, `workshop_mode=optimizer`) — legacy/custom optimizer job. Do not create this for Goal Advisor; Pulse Gate now selects the Goal Advisor module after normal runs. Use optimizer schedules only for an explicitly requested bespoke scheduled analysis job with bounded stop conditions.

**Default mode rule:** create workflow schedules with `mode="workshop"`. New schedules should never use `mode="workflow"`.

**`/goal-advisor` rule**: When setting up continuous improvement, create or update the recurring execution schedule only: `mode="workshop", workshop_mode="run"` with a message that calls `run_full_workflow(group_name="...")` for each configured group, and enable Pulse with `update_workflow_config(post_run_monitor=true)`. Do not create a separate optimizer Goal Advisor schedule; Pulse Gate decides when the Goal Advisor module is due.

### Back up scheduled workflows

Scheduled runs execute unattended and accumulate state (`workflow.json`, `planning/`, `knowledgebase/`, `learnings/`, `db/`, reports) that otherwise lives only on local disk. **Whenever you set up a recurring schedule, also arrange a backup** so each run persists its output off-box. Load `get_reference_doc(kind="backup-strategy")`, follow it once to initialise the workflow's backup destination, and persist the result in `workflow.json.backup`.

- Set `workflow.json.backup.enabled=true`, `mode="agent"`, `triggers.after_scheduled_run=true`, and a `destinations` entry for each backup target (git/github for config, R2/S3/B2/HuggingFace for large artifacts as needed).
- After each backup attempt, write `backup/status.json` with the destination results, timestamps, summary, and errors. Do not put changing backup status in `workflow.json`.
- If an explicit schedule message is needed, append a final backup turn to `messages`, e.g. `"After the run completes, follow workflow.json.backup and the backup-strategy skill, perform the configured backup, and update backup/status.json. Do not ask for confirmation."`
- If you rely on the default full-workflow message, the auto-notification after `run_full_workflow` will still ask the builder to honor `workflow.json.backup` and write `backup/status.json`.

Confirm with the user before skipping backup on a recurring schedule.

### Writing messages for scheduled runs

`messages` is an ordered queue of strings sent to the workshop LLM one-by-one as user turns. The LLM completes all tool calls triggered by message N before message N+1 is sent.

- Write each message as a plain instruction, like you would type in chat: `"Run the full workflow"`, `"Generate the final report"`.
- **Run mode** (`workshop_mode="run"`): typically one message with exact groups, e.g. `"Do not ask for confirmation. Run the full workflow for group-1 using run_full_workflow(group_name=\"group-1\")."`
- **Optimize mode**: legacy/custom only. For `/goal-advisor`, do not create optimizer schedules; Pulse Gate runs Goal Advisor as a module when evidence warrants it.
- Use multiple messages to break work into sequential phases, e.g. `["Run the workflow", "Generate the final report"]`.
- Read `variables/variables.json` for available group names and include them explicitly in the message if needed.

**CRITICAL — schedules run unattended; messages must never require human input:**

- Explicitly tell the agent to make all decisions autonomously: `"Do not ask for confirmation, proceed automatically"`.
- Provide all required parameters upfront in the message (group names, run folders, step IDs) so the agent never needs to ask.
- Tell the agent to skip or use defaults for anything unclear rather than pausing to ask.
- Never include open-ended questions or `"let me know"` style instructions.
- **Bad**: `"Run the workflow and ask me which steps to optimize"`.
- **Good**: `"Review runs/iteration-0 for group-1, read eval/log evidence, then choose harden_workflow, an approved plan-change application, or a Goal Advisor proposal using the scheduled decision model. Log no action if nothing is ready."`

### Legacy/custom optimizer schedules

When creating a schedule with `workshop_mode="optimizer"`, craft the message around the exact recurring custom job. Do not use this for Goal Advisor; use `/goal-advisor` setup to enable Pulse and a normal run schedule.

For custom optimizer messages:
- Name the configured `group_names`.
- Use only `runs/iteration-0` evidence for those groups.
- Inspect run outputs plus execution/tool logs for failures, retries, wrong tool arguments, timeouts, validation errors, and stuck steps.
- Read `builder/improve.html` (the single durable log), recent `planning/changelog/` entries, and current run/eval evidence.
- Handle report accuracy/live-data/layout work with report-plan tools only when the recurring job explicitly includes report quality or an unresolved review/improve item queues it.

Pulse module cadence is not encoded in schedule JSON. Pulse Gate stores module state in `db/db.sqlite` and decides which modules are due after each normal run.

**Infinite loop prevention**: Scheduled optimizer runs are unattended — they MUST have built-in stop conditions. The message should instruct the agent to: (1) use bounded evidence review, (2) apply at most one primary harden/replan action per fire, (3) avoid fresh workflow reruns unless verification is explicitly needed, (4) stop after recording what was applied or deferred.

## Shell & Discovery

- **`execute_shell_command`** — Run shell commands. Quick lookups:
  - `jq '[.steps[] | {id, title, type}]' planning/plan.json`
  - `` jq --arg sid "step-id" '.. | objects | select(.id? == $sid) | {id, title, type, description, context_dependencies, context_output}' planning/plan.json ``
  - `cat planning/step_config.json`
  - `ls runs/`
  - `cat variables/variables.json`
- **`human_feedback`** — Ask the user a question during a run.
- **`create_human_input_request`** — Non-blocking Pulse/goal-advisor/Chief of Staff question stored in the workflow's `db/db.sqlite` table `report_human_inputs`; the user answers in the Runloop Pulse/report panel.
- **`mark_human_input_consumed`** — Mark an answered report question consumed after using it and recording the outcome; then clear the matching visible question card from the Pulse HTML so it no longer appears actionable.

## Skills

Skills are reusable instruction sets injected into step agents at runtime. They live at the **workspace root** `{{"{{"}}.AbsDocsRoot{{"}}"}}/skills/{folder}/SKILL.md` — shared across all workflows. Do NOT create or reference skills inside the workflow folder (e.g. `Workflow/trading/skills/` does not exist).

**Workflow for managing skills**:

1. **Find**: `list_skills` to see installed skills, or `search_skills(query)` to search the public registry.
2. **Install**: `install_skill(source)` (e.g. `owner/repo@skill-name`) or `import_skill(github_url)` — downloads into `{{"{{"}}.AbsDocsRoot{{"}}"}}/skills/{folder}/`. If a skill folder exists but has no `SKILL.md`, reinstall it using the same method it was originally installed with — **never write `SKILL.md` content manually**.
3. **Select for workflow/builder context**: `update_workflow_config(add_skills=["folder-name"])` — makes the skill visible as a selected workflow capability for workshop/builder discovery. **Do NOT edit `workflow.json` manually.**
4. **Enable for runtime steps**: `update_step_config(step_id, enabled_skills=["skill-a"])`. Step execution only receives the skills listed in that step's `enabled_skills`; workflow-selected skills do not cascade into runtime agents.
5. **Remove from workflow**: `update_workflow_config(remove_skills=["folder-name"])`.
6. **Uninstall**: `uninstall_skill(folder_name)` — removes files from workspace entirely.

Use `get_workflow_config` to see the workflow's selected skills. Use `list_skills` to see all installed skills.

## Secrets

Secrets are credentials (API keys, tokens, passwords) injected into step agents as `$SECRET_<NAME>` environment variables at execution time. They exist in three buckets:

- **Workflow secrets** — per-user, encrypted server-side, scoped only to this workflow. Use these by default for workflow-specific credentials.
- **User secrets** — per-user, encrypted server-side, reusable across workflows.
- **Global secrets** — operator-managed via `GLOBAL_SECRET_*` env vars on the server. Read-only from chat.

**Adding a secret is a TWO-STEP flow. Doing only step 2 is a common silent-failure trap: the name gets attached but `$SECRET_<NAME>` is empty at runtime.**

1. **Store the value**: prefer `set_workflow_secret(name="BUFFER_API_KEY", value="<plaintext>")` for workflow-only credentials. Use `set_user_secret` only when the same credential should be reusable across workflows.
2. **Attach to this workflow**: `update_workflow_config(add_secrets=["BUFFER_API_KEY"])`. This step validates that a value exists (workflow store, user store, or global); attaching an orphan name is rejected with an error pointing to step 1.

**When the user asks you to add/save/set a secret for this workflow, complete both steps in the same turn.** Do not stop after `set_workflow_secret` or `set_user_secret`; immediately call `update_workflow_config(add_secrets=[...])` so the next step run receives `$SECRET_<NAME>`. If the user only gives a name and no value, call `list_secrets` first and attach an existing available secret if present; otherwise ask for the value. If the user pastes a value in chat, store it and then refer to it by name only.

Do **not** give boilerplate advice like `"rotate this secret"` after a normal user-requested save. Recommend rotation only when there is a concrete exposure reason: the value was printed into logs/output, committed to a file, sent to the wrong channel, or the user explicitly asks for security remediation.

**Other secret ops**:

- **Inspect**: `list_secrets` returns `global`, `workflow`, and `user` buckets — values are never exposed.
- **Edit a value**: call `set_workflow_secret` or `set_user_secret` again with the same name — it upserts.
- **Delete from store**: `delete_workflow_secret(name)` or `delete_user_secret(name)`. Workflow attachments are separate — also run `update_workflow_config(remove_secrets=["NAME"])` to detach.
- **Detach only (keep value)**: `update_workflow_config(remove_secrets=["NAME"])`.

Secret VALUES are never rendered into prompts, logs, or tool outputs. Step agents read them only from `$SECRET_<NAME>` in `execute_shell_command`. Never echo, print, or hardcode a secret value in descriptions, learnings, or `main.py`.
