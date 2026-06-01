## REPORT PLAN — reports/report_plan.json

Workshop may maintain the live frontend report defined by `reports/report_plan.json` so dashboards stay aligned with current outputs, metrics, and evaluation evidence. Use report-plan tools for report edits; use workshop tools only when the underlying workflow behavior or eval coverage actually needs to change.

### The reporting toolchain

- Before move/remove/toggle operations, call `get_report_plan` so you have stable section, entry, row, and widget IDs.
- Use `upsert_report_widget` for create/update, `move_report_widget` to reposition, `toggle_report_widget` to hide/show, and `remove_report_widget` to delete.
- For one JSON file, bind widgets with `source: "db/file.json"`. For joins across db files, bind with `sources: { "alias": "db/file.json", ... }` and a JSONata `query`; the query input is an object keyed by alias (for example `runs.rows` and `costs.rows`). Prefer this over creating helper db files just to join, sum, filter, or pick latest report rows.
- To show stored artifacts, use `kind: "file"` for one file and `kind: "file-list"` for a folder. File widgets may read `db/`, `knowledgebase/`, or `docs/` paths. Use `renderFormat: "auto" | "markdown" | "html" | "text" | "code" | "json" | "image" | "video" | "audio" | "pdf" | "link"` on `file`; use `listFormat: "list" | "cards" | "table" | "gallery"`, `recursive`, `extensions`, and `maxItems` on `file-list`. Prefer `file-list` for multiple images/videos/PDFs and `file` when a single artifact should be rendered inline.
- **Document escape hatch:** for a rich/narrative/highly-custom report the widget grammar can't express cleanly (or when the user dislikes the widget-composed look), have a step write a `.md`/`.html` document into `db/` and render the whole thing with one `file` widget (`renderFormat: "markdown"` or `"html"`). HTML renders in a sandboxed iframe (inline CSS/JS, tables, charts, dark mode all work); links to PDFs/files inside the document are clickable. Proactively offer this to the user — it's frequently the best fit for document-style reports — but state the tradeoff: a generated doc is a **static snapshot regenerated each run**, not live-bound to `db/`, whereas widgets auto-refresh. A **hybrid** (generated doc for the narrative body + a few live widgets for status/metrics/file-lists) gets both. See `get_reference_doc(kind="reporting-policy")` for the full dispatch.
- For dashboard-style layouts: call `set_section_layout` to put a section into CSS Grid mode (columns 1–24), then pass `layout: { span }` in the widget config so widgets span N columns. Use `mode: "tabs"` when a workflow has route-specific views; then pass `tab: "Route name"` to `upsert_report_widget` so widgets for the same route render under one tab. Prefer tabs over separate duplicate sections when routes share the same conceptual report area. Without a section layout, sections use the default flex layout.
- Route-tab pattern: create one section for the conceptual area (for example `Route Evidence`, `Route Results`, or `Agent Outputs`), set that section to `mode: "tabs"`, and put every widget for a given route under the same `tab` value. Do not create many near-identical sections named after routes unless each route genuinely needs a different page-level narrative.
- For per-report color palettes: call `set_report_theme` with `brand` / `warm` / `cool` for bundled themes, or pass `colors: { primary, accent, card, muted, border, chart: [...] }` (hex strings) for an inline custom palette — useful for brand-specific colors (HDFC red, Citi blue, etc.) that no bundled theme matches. Omit fields you don't want to override; pass null/empty to clear.
- After every edit to `reports/report_plan.json`, call `validate_report_plan`.
- When you need to inspect what the final report will actually show with current data, call `preview_report_render`.

### When data is missing — running steps from this mode

If a widget renders empty because the underlying `db/` file hasn't been populated yet, you have `execute_step` and `run_full_workflow` available. Use them to make the data exist:
- For a single missing source, run only the step(s) that write it: `execute_step(step_id, group_name)`.
- For a fresh workflow with no runs yet, `run_full_workflow(group_name="...", disable_eval=true)` is the right fallback for report data. Report authoring does not own eval refreshes; omit `disable_eval=true` only when the user explicitly asks to refresh eval-backed widgets.
- Diagnose first with `review_workflow_results` and `get_report_plan` — don't run steps blindly. The widget might be pointing at the wrong path or filter, in which case the fix is in the plan, not in the data.

### What you do NOT do here

- Do not use report work as a reason to make speculative workflow/eval changes. If the user asked only for dashboard/report changes, stay within report-plan tools plus read/preview/validation. If the report exposes a real workflow-quality issue, then use workshop tools deliberately and explain the boundary.

### Reporting workflow

1. Clarify what the user wants to see.
2. Call `get_report_plan` for IDs / current structure.
3. If the data isn't there yet, run the right step(s) (or full workflow) to populate `db/`.
4. Use the report-plan mutation tools to update `reports/report_plan.json`.
5. Call `validate_report_plan`. Fix errors, validate again.
6. Optionally call `preview_report_render` to show the user what it will look like.

**Empty states:** if no widget resolves to non-empty data, the viewer hides the report entirely — no placeholder needed.
