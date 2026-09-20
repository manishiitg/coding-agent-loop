## WORKFLOW REPORT — db/reports/*.html

The workflow owns one or more live report documents under `db/reports/`;
`index.html` is the default. The shared toolbar discovers additional HTML files,
with optional `views.json` metadata for titles, order, and default. It is not a
widget/layout plan.

- Each HTML document decides whether to use tabs, a sidebar, anchored sections,
  expandable panels, or one scrolling briefing.
- Design tablet-first for the default ~768px Report pane: one or two primary
  columns, 44px touch targets, wrapping/scroll-safe tabs, and tables that reflow
  or scroll within their own labeled region. Verify 480px mobile and 1280px
  laptop as required secondary widths; wider layouts progressively enhance the
  tablet composition rather than defining it.
- Read current durable data through `window.report.query`, `get`, `getText`,
  `getHtml`, `renderMarkdown`, `fileUrl`, and `openFile`. Write a plain field
  on an existing row (e.g. an inline Approve button) with
  `window.report.updateField`/`updateFields` — see `reporting-policy.md`.
- Goal and cost sections can use `getGoalMetrics` and `getCosts({ days: 30 })`.
  Their optional `renderGoalProgress` and `renderCosts` widgets include
  responsive styling and details; no custom chart design or duplicate
  measurement store is required. Tables and the activity section can use the
  optional `renderTable` and `renderActivity` composition widgets instead of
  hand-rolled markup. Use only the sections relevant to the report. See
  `reporting-policy.md` for examples, history limits, missing-data handling
  and cost window semantics.
- A user click can call `window.report.sendChatMessage(message, { requestId })`
  to send directly to an existing workflow chat, creating one only if none exists.
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
- After editing, validate and preview every changed document, then inspect its
  tablet, mobile, and desktop screenshots.

Load `references/reporting-policy.md` for the complete authoring contract.
