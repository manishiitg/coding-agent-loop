## WORKFLOW REPORT — db/reports/index.html

The workflow owns one complete live reporting experience at
`db/reports/index.html`. There is no JSON report plan, widget registry, or
platform-generated page navigation.

- The HTML decides whether to use tabs, a sidebar, anchored sections,
  expandable panels, or one scrolling briefing.
- Read current durable data through `window.report.query`, `get`, `getText`,
  `getHtml`, `renderMarkdown`, `fileUrl`, and `openFile`. Write a plain field
  on an existing row (e.g. an inline Approve button) with
  `window.report.updateField`/`updateFields` — see `reporting-policy.md`.
- Goal, evaluation and cost sections can use `getGoalMetrics`, `getEvaluations`,
  and `getCosts({ days: 30 })`. Their optional `renderGoalProgress`,
  `renderEvaluations`, and `renderCosts` widgets include responsive styling and
  details; no custom chart design or duplicate measurement store is required.
  Use only the sections relevant to the report. See `reporting-policy.md` for
  examples, history limits, missing-data handling and cost window semantics.
- A user click can call `window.report.sendChatMessage(message, { requestId })`
  to review and send a contextual request in an existing or new workflow chat.
  Save any report-owned approval first, scope the message to its exact item and
  intended consumer, and distinguish saved approval from queued work. Never
  send from a render/ready/poll callback; see `reporting-policy.md` for receipts.
- Gate ALL data calls behind `window.report.ready(fn)` — never `DOMContentLoaded`,
  `window.onload`, or a bare top-level call/await. `window.report` is injected
  after the page's own script runs, so those fire too early.
- Keep CSS and JavaScript inline. Use `db/assets/` for durable media.
- Do not add a workflow step that regenerates the report. Steps update durable
  data; the report reads it live.
- Platform human-decision lifecycles stay in Pulse/chat. Report-owned business
  approval buttons are supported through `updateField`/`updateFields` followed
  by an optional `sendChatMessage` request; do not duplicate the platform's
  decision store or write its records through the business-field API.
- After editing, call `validate_report_html()` and repair every error.

Load `references/reporting-policy.md` for the complete authoring contract.
