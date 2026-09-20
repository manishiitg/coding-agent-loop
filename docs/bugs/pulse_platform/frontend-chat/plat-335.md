[← Pulse platform index](../../pulse_platform_issue_register.md)

# PLAT-335 — Dashboard composition widgets (small files instead of one giant HTML)

| Coordination | Value |
|---|---|
| Assigned agent | Unassigned (proposal) |
| Ticket state | `proposal; not implemented` |
| Last synchronized | `2026-09-20` |

## 2026-09-20 — Proposal: grow the `render*` family so dashboards compose instead of hand-rolling

### Observation (surveyed 2026-09-20)

Every dashboard in the fleet is one large self-contained HTML file because the
`sandboxed srcdoc` runtime cannot load relative `<link>`/`<script src>`
(`report_html_tools.go` warns they are "ignored inside the sandboxed report —
drop the tag or inline the asset"). Surveyed:

- Local live dashboards: 8 files, 188–1952 lines
  (`workspace-docs/Workflow/*/db/reports/index.html`,
  `gstdatacollection/db/reports/gst_dashboard.html`, one Crew dashboard).
- Local baked static reports: ~40 × `Comprehensive_Income_Tax_Report.html`
  (400–641 lines each, zero `window.report` calls — per-client generated
  artifacts, a separate genre this ticket does not serve).
- Confida server (`confida@116.202.210.102`,
  `/srv/confida/data/docs/Workflow/confida-login/db/reports/index.html`):
  3827 lines, the largest dashboard in the fleet.

Every live dashboard hand-rolls the same five things with raw
`window.report.query` + custom JS/CSS:

| Pattern | Live dashboards using it |
|---|---|
| Tabs (`nav-tabs`, `role=tab`, `data-tab`) | 7/9 |
| KPI/stat tiles | 6/9 |
| Cards (4–32 per file) | 9/9 |
| Tables (1–15 per file, manual row-html loops) | 9/9 |
| Activity tab (`tab-activity`/`panel-activity`, required by policy) | 6/9 |

Across all local dashboards the only `window.report` calls are `query`,
`getText`, `fileUrl`, `renderMarkdown`, `ready`, `openFile`, `focus`.
Confida additionally shows the heaviest action-button usage in the fleet
(13 `sendChatMessage` calls, 1 `updateField`) plus bespoke collapsible
detail cards (`collapsible-card`, `smoke-detail-card`, `linear-detail-card`).
**Zero uses**
of `renderGoalProgress`, `renderCosts`, `getGoalMetrics`, or `getCosts` —
the two widgets the platform ships cover goal/costs only, while agents need
tables, KPIs, tabs, and activity. `renderEvaluations`/`getEvaluations` are
additionally gone from the runtime after the PLAT-333 eval-subsystem removal,
but `reporting-policy.md`, `report-plan.md`, `improve-report.md`, and
`design-reporting-ui.md` still tell agents to call them — a stale promise
that rejects as unavailable at runtime.

### Why this is a platform gap, not a workflow authoring choice

No workflow can author small dashboard files today: partials via
`getText`+`innerHTML` carry markup/CSS only (`<script>` injected that way
never executes), local JS/CSS files never load in the sandbox, and the only
shipped widgets cover two sections agents rarely need. This is the same shape
as PLAT-293/PLAT-256 — a missing capability tier on `window.report`, not a
per-workflow decision. A full React/TS migration was considered and rejected:
agent-authored TSX needs a build + sandbox anyway (the iframe stays), while
validation, preview, guidance, and every existing dashboard would need a
rewrite.

### Proposed fix (not implemented)

Add six optional composition widgets on `window.report`, following the
existing `renderGoalProgress`/`renderCosts` contract (empty container,
replace contents on refresh, reject on failure, identical behavior in the
app and `preview_report` via the shared host runtime):

- `renderTabs(target, [{id, title, content}])` — accessible tab shell honoring
  `report:focus`; reuse the existing `reportTabSelection.ts` persistence.
- `renderKpis(target, {query | items})` — `{label, value, tone}` tiles.
- `renderTable(target, {query, searchable, sortable})` — themed, responsive
  table with empty state; kills the most repeated code in the fleet.
- `renderActivity(target)` — the policy-required activity tab from
  `org_dashboard_notifications` (`notification_kind='run_summary'`),
  route-grouped via `route_summaries_json`, markdown-rendered. Zero config
  for the one section every dashboard must have.
- `renderActions(target, {actions})` — save-then-send buttons per the
  PLAT-293 contract (`updateField`/`updateFields` first, then
  `sendChatMessage` with stable `requestId`), with built-in
  queued/sending/failed receipts and double-click dedup. Motivated by
  Confida's 13 hand-wired action calls.
- `renderCollapsible(target, {sections})` — themed expandable detail
  sections with summary rows; replaces bespoke collapsible-card JS/CSS
  (Confida's `smoke-detail-card`/`linear-detail-card` pattern).
- Shared card stylesheet (CSS only — not every widget needs JS).

Target: new dashboards drop from ~500–3800 lines to ~100–200 lines of
composition. Adoption is opt-in; existing dashboards keep working untouched.
Suggested pilot: `renderActivity` + `renderTable` first (highest
repetition, clearest contract), with `renderActions` as fast-follow on the
Confida evidence.

### Implementation sketch

- New `frontend/src/components/workflow/reportWidgets/reportComposition.ts`
  (+ tests beside `reportOperationalMetrics.test.ts`).
- Wire into `window.report` in `reportHostRuntime.ts`: real API object,
  bootstrap stub queue list, and pending-call replay map.
- Add names to `reportKnownReportMethods` in
  `pkg/orchestrator/agents/workflow/step_based_workflow/report_html_tools.go`
  so `validate_report_html` accepts them; drop the stale
  `getEvaluations`/`renderEvaluations` entries so calls to the removed API
  are flagged instead of silently accepted.
- Guidance (one rewrite, subsumes the eval-cleanup): document the new
  helpers as optional in `reporting-policy.md` and `design-reporting-ui.md`,
  and remove the `renderEvaluations`/`getEvaluations` promises from
  `reporting-policy.md`, `report-plan.md`, `improve-report.md`, and
  `design-reporting-ui.md`. Note `docs/report-metric-widgets.md` documents
  the eval widget too and needs the same cleanup (outside `guidance/`).
- No `views.json`, multi-document, or `window.report` data-API changes.

### Verification (not started)

Focused widget unit tests; host-runtime install/replay tests;
validator accept/reject tests for new/retired method names;
`preview_report` render of a composed dashboard; guidance-render suite.
No test/build/deploy evidence yet — this is a design proposal from a chat
discussion, not a landed change.
