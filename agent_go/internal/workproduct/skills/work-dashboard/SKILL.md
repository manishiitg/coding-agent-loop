---
name: work-dashboard
description: Create or update a Work project's visual Dashboard for tasks, notes, plans, status, research, project information, or any other content the user wants to manage visually.
---

# Work Dashboard

Use this skill when the user asks for a dashboard, board, tracker, visual home,
or another project view intended to organize or manage information visually.

## Contract

- The Dashboard entry document is `db/reports/index.html` in the current Work
  project. Supporting files belong under `db/reports/` or `db/assets/`.
- This is a general project artifact, not an AgentWorks workflow report. Do not
  create a workflow, plan, phase, step, route, Pulse configuration, or managed
  workflow merely to provide a Dashboard. A project-owned managed database is
  optional and should be added only when the view needs durable structured data.
- Inspect the existing `db/reports/` folder before changing it. Preserve useful
  content and the user's established organization and visual language.
- Build one complete, responsive HTML experience. It may use tabs, boards,
  lists, cards, timelines, calendars, notes, or other layouts appropriate to
  the user's information.
- Support both app themes using `:root.dark` or
  `[data-theme="dark"]`. Avoid external dependencies unless the user requests
  them or the project already uses them.

## Project data and actions

- Use `window.report.query(sql, params)` for live structured data. Inspect the
  schema with `query_workflow_db`; create idempotent migrations under
  `db/migrations/` and apply them with `apply_workflow_db_migration`. Change rows
  only through `mutate_workflow_db`. Never open or edit `db/db.sqlite`, WAL, or
  SHM files directly.
- Dashboard documents can read allowed project files with `window.report.get`,
  `getText`, or `getHtml` when a database is unnecessary.
- A Dashboard action that needs the agent to change project state may call
  `window.report.sendChatMessage(message, { requestId })`. Use a stable,
  item-specific request ID and make the message describe the exact requested
  change. Queued only means the message reached the project chat; it does not
  prove completion.
- `window.report.updateField` and `updateFields` may be used for explicit,
  user-initiated edits. Keep SQL parameterized and scope updates to stable keys.
- Treat files outside `db/reports/` as project evidence. Do not move or rewrite
  unrelated project content solely to fit a Dashboard layout.

## Verification

- Re-read changed files, run `validate_report_html`, and then use
  `preview_report` when browser rendering is available.
- Verify responsive layout, light and dark themes, navigation, empty states,
  and any buttons or filters. Use the managed browser when it materially
  improves confidence.
- When the user asks for a URL to this Dashboard, call `get_report_link` with
  no arguments and present its returned `url` verbatim. It opens the full live
  Dashboard runtime, not the restricted generic HTML file preview. The link is
  private to the same signed-in Work account; it contains no credential and
  does not grant access or publish the project publicly. Inspect `shareable`,
  `scope`, and `warning`; when `shareable` is false (including localhost and
  loopback deployments), describe it only as a same-machine preview and relay
  the warning instead of presenting it as shareable. It uses AgentWorks SSO and
  must not receive a second publish password/login gate; only a separately
  hosted public/static report uses publish visibility controls.
- Tell the user the Dashboard is available from the Work **Dashboard** button.
