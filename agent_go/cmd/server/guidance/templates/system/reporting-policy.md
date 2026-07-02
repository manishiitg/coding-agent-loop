## Reporting Policy

The workflow has a **live frontend report viewer** at the top toolbar's
"Report" tab. It reads `reports/report_plan.json` and renders the **HTML
document(s)** registered there — each an HTML file under `db/reports/`. HTML
documents read `db/db.sqlite` live via the `window.report` API and render their
own visuals. The viewer is always available — there is **NO separate "generate
report" phase**: you author the document **once** and it reads live data on view.

There is no widget grammar and no second format — a report is HTML document(s)
that read the db live via `window.report`. The canonical report model, the full
`window.report` API, and the content + formatting + design guide all live in
`get_reference_doc(kind="report-plan")` — load it before authoring or editing a
report. This policy only covers when/where reports apply and the mode boundary.

## Workshop mode: own the report plan

Workshop mode can author and maintain the report when it needs to reflect
optimization/evaluation/run evidence: authoring the HTML document(s), themes,
tabs, and `reports/report_plan.json` edits. Keep report edits presentation-only
unless the user also asked for workflow hardening/eval changes.

### When the user asks "create a report" / "build a reporting UI" / "show me X in a dashboard"

- The answer is: **author an HTML document** (live data via `window.report`,
  renders its own charts/tables/branded layout) and register it in
  `reports/report_plan.json` with a `file` widget (`renderFormat: "html"`).
  Start from the shipped HTML skeleton — `get_reference_doc(kind="html-output")`
  — so even a simple narrative report is quick to author and looks consistent.
- **Author the document once; wire it to read data LIVE.** Do NOT add a
  workflow step that (re)generates the report each run — the workflow's normal
  steps already write the data to `db/db.sqlite`, and the HTML report reads it
  live via `window.report`, so there is nothing to "generate." (Writing the
  `.html` file once is correct and expected — the staleness anti-pattern is
  *re-emitting* the report each run, or baking live data into the file as static
  text.)
- If the workflow has routing routes, predefined task routes, or other
  per-entity outputs (per-PAN, per-account), use a **tabbed** section: one
  HTML document per entity, `set_section_layout(mode="tabs")`, and give each
  document `tab: "<entity/route name>"`. One tab per user-meaningful entity so
  the report doesn't mix unrelated outputs in one long page.
- When creating or improving a report for a routed workflow, first inspect the
  route list and decide the report structure (which tabs, what each shows) from
  that route map.

### Live data + how to author

HTML reports read `db/db.sqlite` live via `window.report` and draw their own
charts/tables/CSS. The full `window.report` API, **what to put in the report
(content/insight), the formatting + design-quality bar, theming, and responsive
rules** are all in `get_reference_doc(kind="report-plan")`; the HTML skeleton is
in `get_reference_doc(kind="html-output")`. Load those before authoring — don't
restate them here.

A report dashboard should answer the workflow's business question, not just list
files. Prefer a first-screen goal-tracking structure that tells the user the
goal status, tracked success signals, current plan/strategy or active
improvement, important issues/blockers, latest movement since the prior run, and
the evidence behind the claim.

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
