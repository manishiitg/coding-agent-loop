## HTML-only reporting policy

The Report tab renders one workflow-owned document: `db/reports/index.html`.
That HTML owns the entire reporting experience and decides whether to use tabs,
a sidebar, anchored sections, expandable panels, or one scrolling briefing.
The platform does not manufacture navigation from filenames. There is no report
plan, widget registry, JSON layout, platform-generated decision control, or report
generation step.

### Page contract

- Write one complete, self-contained `db/reports/index.html` document.
- Include a non-empty `<title>` and accessible internal navigation when the
  report has multiple views or sections.
- Keep CSS and JavaScript inline. Do not pin body height or create a nested
  scroll container.
- Read durable live data with `window.report.query`, `get`, `getText`,
  `getHtml`, `renderMarkdown`, `fileUrl`, `mediaUrl`, and `openFile`. Write a business
  field on an already-existing row with `window.report.updateField`/
  `updateFields` (see below). Do not bake changing run results into the
  document or add a step that regenerates it each run.
- A user action button can offer a contextual workflow-agent request through
  `window.report.sendChatMessage`. The app sends it directly to an existing automation chat,
  creating one only if none exists. For report-owned approvals, save first and then offer the request;
  see "Sending a report request to the workflow agent" below.
- A markdown file under `db/` renders inline with
  `el.innerHTML = await window.report.getHtml('db/notes/brief.md')`; a
  markdown string from a query row with `window.report.renderMarkdown(text)`.
  Both come back themed (`.report-markdown`), and links/images inside them
  that point at workspace files (`db/...`, or paths relative to the .md
  file) load and open the in-report preview. Never show markdown as raw text.
- For audio or video under `db/assets/`, persist the workspace-relative path
  in the database and obtain a fresh playback URL with
  `await window.report.mediaUrl(path)` only when the user opens the media.
  Assign that URL to a native `<audio controls preload="metadata">` or
  `<video controls playsinline preload="metadata">` element. The platform
  owns authentication, expiring URLs, byte ranges, and streaming: never call
  an internal report-preview/report-media HTTP endpoint directly, persist the
  returned URL, embed media as base64/data URLs, or eagerly load every media
  row with `fileUrl`. `fileUrl` remains appropriate for ordinary downloadable
  files and backward-compatible reports, but `mediaUrl` is the explicit
  contract for new audio/video report code. Show playback errors and let a
  retry request a fresh URL. Preserve the existing media element and its source
  during data refresh when the recording has not changed. Compare the workspace
  path plus a run/version identifier, not the expiring signed URL; repeated
  `innerHTML` replacement or `src` assignment resets playback. Initialize the
  player once after data is ready, and ignore stale asynchronous media responses.
- Theme off the app, not the OS: dark styles under `:root.dark` /
  `[data-theme="dark"]` (or the injected `hsl(var(--token))` palette), with
  `report:theme` for live re-styling. `prefers-color-scheme` alone follows
  the viewer's OS and ignores the in-app toggle.
- After editing, call `validate_report_html()`. Beyond the document shape it
  runs every literal `window.report.query` SQL against the live
  `db/db.sqlite`, confirms every referenced `db/` file exists, rejects
  external stylesheet/script URLs, and warns on OS-only dark mode.
- `validate_report_html()` is static and fast; it cannot tell you the page
  actually renders. Call `preview_report()` after it passes, or whenever
  visual review is requested: it opens the report in a real headless browser
  through the same runtime the Report tab uses, waits for it to settle, and
  returns whether it errored, its script/data-fetch errors, its tab labels,
  any `Loading…` placeholder still on screen, and screenshots per theme/width
  under `db/reports/preview/` — open them with `read_image`. Prefer it over
  asking the user to open the Report tab for you.
- Always include one section, as its own top-level tab (not a subsection
  scrolled past within another tab, and not merely an anchored region on a
  single scrolling page), that reports what the workflow actually did, in
  plain non-technical language (recent runs and the actions taken in
  each) — named for the workflow's real run cadence (e.g. `Daily Action`
  for a daily workflow, `Recent Activity` for hourly/weekly/on-demand ones).
  A report with no other tabs still needs this one; the rest of its content
  becomes a second tab.
  **Default source, no extra step needed:** every `notify_user(notification_kind="run_summary")`
  call (required at the end of a Pulse cycle, and normal after an ordinary
  run) already writes a row — title, status, message, structured fields,
  timestamp — into `org_dashboard_notifications` in this same `db.sqlite`.
  Query `notification_kind = 'run_summary'` from it for this tab; that is
  the default and needs no new step, table, or column. The `message` is
  agent-written markdown — render it with `window.report.renderMarkdown`,
  never as raw text, or the reader sees `###` and stray backticks. The
  structured `fields_json` / `sections_json` render as plain values. Build something
  custom only if the parent explicitly asks for a different or richer
  activity view than the run summaries already give them — never invent a
  step or table whose sole purpose is feeding this tab. See the
  `design-reporting-ui` skill for the full authoring requirement.

  **Routes are sub-workflows.** When route data is present, the Daily Action /
  Recent Activity tab groups or filters runs by `(routing_step_id, route_id)`
  from `route_summaries_json` in the same notification row. Parse that JSON
  array and render each entry's label, title, status, message, fields, and
  sections; do not display raw JSON or merge same-named routes. Keep a shared
  workflow section for top-level facts. `branch` choices are not separate
  sub-workflows. Use the same grouping for any Pulse-summary history.
  Check the real schema first: absent table/column or older rows use the old
  message and explicit legacy Route field, with scope shown as not recorded
  when absent. Never backfill route identity from prose, show an unreviewed
  route as clean, or copy a workflow total into each route. No extra plan step
  or workflow-owned activity table is needed.

### Responding to `report:focus` (tabbed reports)

A report with top-level tabs should let the agent point the reader at one:

```js
window.addEventListener('report:focus', function () {
  // window.report.focus is the tab name the agent asked for.
  switchToTab(window.report.focus);
});
```

`open_workspace_view(view="report", target="<tab>")` sets `report.focus` and
fires this event. It is optional — a report that ignores it simply stays on
its current tab — but a tabbed report that honors it lets the agent say
"here is the answer" and land the reader on the right tab.

### Data lifecycle: always gate on `window.report.ready(fn)`

`window.report` is injected into the page AFTER the report's own `<script>`
has already parsed and started running — it does not exist on the page's
first line. Wrap every use of `window.report.*` in:

```js
window.report.ready(function () {
  // window.report.query/get/getText/getHtml/fileUrl/mediaUrl are live here.
  // Runs once on load, and again on every later data refresh.
});
```

Do NOT gate data calls on `DOMContentLoaded`, `window.onload`, or a bare
top-level `(async () => { await window.report.query(...) })()`. All three can
run before injection, before `window.report.query` is truly live — the single
most common cause of a report that shows a "data loading error" or is stuck on
"Loading…" the first time it is opened. `validate_report_html()` cannot catch
this: it parses the markup, it does not execute the page. `.ready()` is the
only pattern that is safe regardless of when it runs.

### Writing back: `window.report.updateField`/`updateFields`

A report may write a plain business field on a row it already reads via
`query` — for example an inline Approve/Reject button that flips a
`status` column, or a bulk list where each row edits independently. This
is a narrow, structured write, not a general database API:

```js
// One cell:
await window.report.updateField('emails', row.id, 'status', 'approved')
// Several columns on the same row, applied atomically (all or none) — a
// form submit:
await window.report.updateFields('emails', row.id, { status: 'approved', note: 'looks good' })
```

Both resolve `{ oldValue(s), newValue(s) }` once committed, or reject with
a clear error. The backend validates every call against the table's own
live schema before writing — there is no way to pass raw SQL through
either function, and no declaration step is needed first:

- the target table must exist, have exactly one primary-key column (the
  row is matched on it), and must not be a platform-owned table
  (`report_human_inputs`, `report_human_input_events`,
  `schema_migration_log`);
- every named column must exist on that table, and must not be the
  primary key, end in `_id`, or be named `created_at`/`updated_at` — a
  column that identifies or timestamps the row is never a legitimate
  write target;
- every value must be a plain string, number, boolean, or null — no
  objects/arrays.

Report-owned approval buttons are supported: update an existing business row
(an email's `status`, an audit finding's approval), then optionally offer a
scoped agent request using `sendChatMessage` below. This does not replace the
platform's human-decision lifecycle. Platform decisions created through
`create_human_input_request` retain their options, answers, consumption, and
audit trail in the Pulse panel/chat; do not edit `report_human_inputs` through
the business-field write API or invent a duplicate platform decision store.

### Sending a report request to the workflow agent

When choosing between a business approval, a Pulse decision, a blocking
checkpoint, and an agent request, read
`read_skill(skills=[{"name":"builder-reference","path":"references/human-in-the-loop.md"}])`.
The API details below implement the report-to-chat pattern.

`await window.report.sendChatMessage(message, { requestId })` sends directly
from the report action, without a second popup or chat-choice step. The app
uses the same workflow-scoped chat queue as the human-decision panel's
**Ask in chat**: reuse an interactive chat,
queue behind its running foreground turn, or create a chat if none exists.
Scheduled/view-only/bot tabs are excluded. This sends a conversational request;
it does not directly execute a route or trigger the scheduler.

Call this only from a user action handler. Never call it from `ready`, render,
polling, or refresh handlers. It is unavailable in `preview_report`. Before the
host initializes, it rejects instead of queuing a request to replay on load.
The host supplies the workspace; reports cannot choose another workspace.
Messages must be non-empty and at most 12,000 characters.

For an existing report-owned approval, await the DB commit before offering the
action request. Include the exact row/proposal version and the intended route
or consumer step. Ask the agent to re-read the current approval, skip work
already applied, and act only on that item. Do not use a generic “run workflow”
message when it would repeat collection/audit or another approval gate.

```js
// In a user click handler, with the button disabled until finally.
await window.report.updateField('audit_findings', row.id, 'status', 'approved');
showStatus('Approval saved. Sending action request…');
const result = await window.report.sendChatMessage(
  `Apply only approved audit_findings row ${row.id}, proposal version ${row.proposal_version}. ` +
  `Re-read its current approval and proposed fix from the database; skip it if already applied. ` +
  `Use the existing remediation route for this item, then verify and refresh the report.`,
  { requestId: `finding:${row.id}:${row.proposal_version}:apply` }
);
showStatus(result.queuedBehindRunningTurn
  ? 'Request queued behind the current chat turn.'
  : 'Request queued in chat.');
```

Use real schema fields/versions and actual route or step IDs, never copy
placeholder names into a workflow that lacks them. The successful result is
`{ status: 'queued', tabId, reused,
queuedBehindRunningTurn }`. Queued is not proof that work started or completed;
show applied/verified outcomes only from fresh execution evidence.

The approval write and chat enqueue are separate operations. A failed enqueue
does not undo the saved approval (a later scheduled consumer can still read it).
On failure, keep that approval visible and offer **Send action request** again
without rewriting it. Catch errors locally and re-enable the
button in `finally`. Repeated clicks while sending share one request;
an optional stable `requestId` (max 200 characters) reuses a successful receipt
for the same message in the current report view (up to 100 receipts). Reloads,
different views, and later sessions still require the consumer's durable
already-applied check. This API does not provide atomic approval-and-execution.

### Referenced files must live under `db/`

A report may only reference paths under `db/`. That is the durable store the
Report tab reads; anything else is invisible to it.

A step's own execution folder (`runs/iteration-N/<group>/execution/...`) is
per-run scratch, not a referenceable location. A screenshot, PDF, or export
captured there is not reachable from the report just because it exists on disk
— the step must copy it into `db/` (for example `db/reports/<key>/proof.png`)
for the report to show it.

Referencing a path that was never published produces a broken-image icon and
nothing else at runtime: no error, no log line, no failed run. Verify the file
exists at its `db/` path, not only at the path the step wrote.

`validate_report_html()` checks every literal path the document references
(`window.report.get/getText/getHtml/fileUrl/mediaUrl/openFile('db/...')`, `src="db/..."`,
`href="db/..."`) and reports the missing ones. A path assembled at runtime from
variables is invisible to it — prefer literal paths, or verify those yourself.

### Workshop and Run boundaries

Workshop owns report authoring and may create or edit `db/reports/index.html`.
Keep report-only changes presentational unless the user also asked to change
workflow behavior or evaluation. Run mode never authors report pages; it only
produces the durable data those pages read.

Platform human-decision lifecycles stay in the Pulse panel and chat. A report
can expose its own business approval buttons using `updateField`/`updateFields`
and hand a specific request to the workflow agent using `sendChatMessage`.
See "Writing back" and "Sending a report request" above.

For typed route rows, `summary_text` contains only the shared lead; `message`
remains the complete rendered digest for older reports. Render the lead plus
route entries once, or the complete message as a fallback, never both.

### Goal progress: shared data and an optional ready-made widget

Reports can show the same primary metric, supporting metrics, and measured history
as Pulse. No additional collector, table, chart library, or custom CSS is needed:

```html
<section id="goal-progress"></section>
<script>
window.report.ready(async function () {
  await window.report.renderGoalProgress('#goal-progress');
});
</script>
```

The optional widget renders primary/supporting cards, targets, change since the
previous measurement, trends, freshness, and expandable definitions/evidence/history.
It inherits the report's text color, fits the available width, and replaces its
contents on refresh. Use an empty `div` or `section` as the container. Keep the
returned promise inside `ready` so preview_report can observe loading/errors.

For a custom layout, `await window.report.getGoalMetrics()` returns
`{ metrics, observations, progress }`. Each progress item contains `metric`,
`current`, `delta`, `state`, `stale`, `targetMet`, `latest`, `history`, and `numeric`.
`current` can be undefined: never turn missing data into zero. The latest failed
measurement remains unavailable rather than falling back to an older good value.
History includes up to 120 comparable observations per active metric, with scope,
unit, and evidence preserved. Use `window.report.query` against
`workflow_goal_metrics` and `pulse_goal_observations` for a longer/custom history;
check `sqlite_master` first for workflows that have not configured measurements.
These are platform-owned, read-only tables for reports; use the managed goal tools
for changes, not `updateField`/`updateFields` or direct SQL writes. Run `/setup-goals`
when definitions or collection are missing. Do not invent targets or samples.

Adding this section is optional and does not replace the report's own navigation
or layout. Offer it when a user wants goal tracking in their dashboard. Validate
and preview the report using the normal report tools after adding it.

### Evaluations and costs: matching report helpers

Use the existing stored evaluations and canonical cost ledger without creating
report-owned copies. Both helpers work in the app and `preview_report`:

```html
<section id="evaluations"></section>
<section id="costs"></section>
<script>
window.report.ready(async function () {
  await Promise.all([
    window.report.renderEvaluations('#evaluations'),
    window.report.renderCosts('#costs', { days: 30 })
  ]);
});
</script>
```

The widgets include their own responsive styling and expandable details. No chart
library or design work is required. Add them only when useful to the user's report.

For custom layouts, call `getEvaluations()` and `getCosts({ days: 30 })`:
- Evaluations returns `{ results, criteria, run_count, result_limit, possibly_truncated }`.
  Each criterion has `id`, `title`, `historical`, `latest`, and `history`. Results
  include captured/skipped flags, raw scores, reasoning, evidence, and run/date.
  The current reader supplies up to 200 rows; this is recent history, not an
  all-time run count. Uncaptured and skipped scores are not zeros or failures.
  Keep criteria separate; do not invent a blended score or a pass threshold.
- Costs returns `{ summary, history, window_total_usd, state }`. `summary.total` and `summary.by_scope` are **all-time** recorded amounts.
  `summary.by_model`, `summary.by_date`, and `window_total_usd` cover only the requested UTC date window.
  `summary` is null and `state` is `unavailable` if the ledger is unavailable.
  Options support `days` (1–90, default 30) and `before` (exclusive YYYY-MM-DD).
  Read older daily pages using `history.next_before` when `history.has_more` is true.
  Do not add the repeated all-time total across pages. USD costs are recorded ledger
  amounts and do not prove every call was priced. No model prices are guessed.

Continue using `window.report.ready` for loading and refresh. Returned promises
reject on read failures, which the host exposes in the report's error surface.
