## Reporting Policy

The workflow has a **live frontend report viewer** at the top toolbar's
"Report" tab. It reads `reports/report_plan.json` and renders the
**document(s)** registered there — each an **HTML** or **Markdown** file
under `db/reports/`. HTML documents read `db/db.sqlite` live via the
`window.report` API and render their own visuals; markdown documents are
static narrative. The viewer is always available — there is **NO separate
"generate report" phase**: you author the document **once** and it reads
live data on view.

There is no widget grammar and no declarative dashboard format. A report
is documents. To show data, write an **HTML** document that queries it via
`window.report`. To write narrative prose with baked-in numbers, write
**markdown**.

## Workshop mode: own the report plan

Workshop mode can author and maintain the report when it needs to reflect
optimization/evaluation/run evidence: authoring the HTML/markdown
document(s), themes, tabs, and `reports/report_plan.json` edits. Keep
report edits presentation-only unless the user also asked for workflow
hardening/eval changes.

### When the user asks "create a report" / "build a reporting UI" / "show me X in a dashboard"

- The answer is: **author an HTML document** (live data via `window.report`,
  renders its own charts/tables/branded layout) and register it in
  `reports/report_plan.json` with a `file` widget
  (`renderFormat: "html"`). For a purely narrative report whose numbers are
  baked in at generation, a **markdown** document is the simpler choice.
- **Author the document once; wire it to read data LIVE.** Do NOT add a
  workflow step that (re)generates the report each run — the workflow's
  normal steps already write the data to `db/db.sqlite`, and an HTML report
  reads it live via `window.report`, so there is nothing to "generate."
  (Writing the `.html` file once is correct and expected — this is NOT a
  "generated dashboard file" that goes stale; the staleness anti-pattern is
  *re-emitting* the report each run, or baking live data into a static file.)
- If the workflow has routing routes, predefined task routes, or other
  per-entity outputs (per-PAN, per-account), use a **tabbed** section: one
  document per entity, `set_section_layout(mode="tabs")`, and give each
  document `tab: "<entity/route name>"`. One tab per user-meaningful entity
  so the report doesn't mix unrelated outputs in one long page.
- When creating or improving a report for a routed workflow, first inspect
  the route list and decide the report structure (which tabs, what each
  shows) from that route map.

### Choosing the format — one decision the agent makes for the user

Two formats, picked by the job:

| Format | Best for | Data |
|---|---|---|
| **HTML** (primary) | dashboards, charts, at-a-glance metrics/tables, branded/print layouts, anything that should stay live | live via `window.report` (or baked in) |
| **Markdown** (secondary) | narrative reports read top-to-bottom; per-entity prose with baked-in tables | static — whatever the generating step wrote in |

Decision rule: **visualizing data or want it live on view → HTML. Writing a
narrative document with the numbers already in it → markdown.** Markdown is
cheaper to author and themes automatically, so prefer it for static
narrative; reach for HTML whenever the report needs charts, bespoke layout,
or data that re-reads the db when viewed.

**Live data (HTML only):** the viewer exposes `window.report` inside the
iframe — `await query(sql)` against `db/db.sqlite`, plus
`await get(path)` / `await getText(path)` / `await fileUrl(path)` for files,
and `openFile(path)` for the preview modal. It fires a `report:data` event
on load/refresh and `report:theme` on light/dark toggle. The HTML draws its
own charts/tables/branded CSS from that data — full styling control. Markdown
does not read the db; its numbers are whatever the generating step baked in.

Full syntax + examples, the `window.report` API, and the **"writing a good
report document" formatting guide** (lead with a summary, structure with
headings, data as tables not raw JSON, self-contained + responsive HTML,
dark mode, design-quality bar): `get_reference_doc(kind="report-plan")`.

### Diagnosis

- **When the report shows "No report yet":** `reports/report_plan.json`
  is missing or registers no usable document. Fix by authoring the
  document and registering it.
- **When an HTML report renders but is empty/missing data the user
  expects:** the document loaded but its `window.report.query` returns no
  rows yet (the table is empty), or it points at the wrong table/column.
  Either a step hasn't run, or the query is off. Inspect the report HTML and
  query `db/db.sqlite` with sqlite3 to diagnose.

### Refresh discipline

**Report viewer auto-updates** when the user opens or switches to the
Report tab — no rebuild step needed. After the agent updates the document
or `report_plan.json`, the user just clicks Report (or refreshes if they're
already on it) to see the new content.

## Run mode: do not edit reports

Run mode does not own report authoring. If the user asks to create or edit
the report, themes, tabs, or `reports/report_plan.json`, tell them to switch
to Workshop mode. Do not offer to draft or edit `reports/report_plan.json`
via shell/direct file writes from Run mode.

## See also

For the report-plan toolchain (registering documents, tabs, per-report
themes, the `window.report` API, the good-document + design-quality guide,
missing-data triage, full workflow), call
`get_reference_doc(kind="report-plan")`.
