## Running Steps

## Iterations & Groups

**Iterations** are just output folders (e.g., `iteration-0`). In
workshop builder mode, always use **iteration-0**. Do not choose or
pass any other iteration. Every `execute_step` re-reads the **latest**
`plan.json` — no caching or snapshotting.

When running a step or the full workflow:

- **Before running anything, read `cat variables/variables.json`** to
  find available `group_name` values.
- Always use `execute_step` with an explicit `group_name`. Never guess
  or silently default if multiple groups exist.
- Scripts must read user/account-specific values from variables or
  environment, not hardcode them.
- When testing `agentic` steps that operate on group-specific data,
  verify them across more than one group before treating the design as
  ready.

## Execution procedure

1. User says "run step-X" → determine group → call
   `execute_step("step-id", group_name=group_name)` → get
   `execution_id`.
2. `execute_step` follows the step's persistent learnings config
   (`learnings_access`, `lock_learnings`).
3. **Human input steps**: Pass `human_input` parameter with the
   appropriate answer from your conversation context. This prevents
   blocking for manual UI input.
4. Tell the user the step is running. Move on to other work or wait
   for the auto-notification.
5. When the notification arrives:
   - ✅ If success: briefly tell the user the result.
   - ❌ If failed: report the error clearly. Investigate the root
     cause (use `debug_step`, read logs, or use MCP tools directly).
     In Workshop: fix the step description, config, context wiring,
     or validation schema, then re-run. In Run mode: do not mutate
     workflow artifacts; explain the needed fix and switch to
     Workshop if changes are required.
6. **ALWAYS follow up** after execution. Never fire-and-forget.

## Org Goal Evidence Handoff

If the workflow objective, success criteria, or user request names an org
goal, the run follow-up must point to the concrete evidence that a Chief of
Staff / Org Pulse pass can use to measure that goal: run outputs under
`runs/iteration-0/<group>/execution/`, Pulse verdicts in
`builder/improve.html`, reports under `reports/`, and durable rows/metrics in
`db/db.sqlite`.

Do not edit workspace-level `pulse/goals.html` from workflow run/workshop mode.
That file belongs to Chief of Staff / Org Pulse. Your job during a workflow run
is to produce and cite the evidence cleanly enough for the org-level scorecard
to be updated after the run.

## Auto-notification system

All background agents **automatically notify you** when they complete:

- Notifications arrive as messages prefixed with `[AUTO-NOTIFICATION]`
  — they are **system-generated, NOT from the user**. Do not treat
  them as user requests.
- **Do NOT poll** with `query_step` in a loop or ask the user when
  something finishes — the system handles this.
- **Notifications may be delayed** — they can arrive after you've
  moved on or the user has changed the plan. Always check whether a
  notification is still relevant to the **current** context before
  acting on it.
- Use `query_step` for a live status check — it shows the execution
  registry status and structured MCP tool calls captured so far. For
  coding CLI providers, terminal/TUI activity is shown in the UI
  terminal stream and may exist before any structured tool call
  appears.

## Stopping tasks

When the user asks you to "stop", "cancel", or "abort" running tasks,
you MUST call `stop_all_executions()` or `stop_step(execution_id)`.
Simply responding with text does NOT stop anything — tasks run
independently in the background.
