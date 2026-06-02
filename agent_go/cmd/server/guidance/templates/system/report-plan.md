## REPORT PLAN — reports/report_plan.json

Workshop may maintain the live frontend report defined by `reports/report_plan.json` so dashboards stay aligned with current outputs, metrics, and evaluation evidence. Use report-plan tools for report edits; use workshop tools only when the underlying workflow behavior or eval coverage actually needs to change.

### The reporting toolchain

- Before move/remove/toggle operations, call `get_report_plan` so you have stable section, entry, row, and widget IDs.
- Use `upsert_report_widget` for create/update, `move_report_widget` to reposition, `toggle_report_widget` to hide/show, and `remove_report_widget` to delete.
- Bind data widgets (table/chart/cards/stat/alert/pivot/text) with `db: "db/db.sqlite"` and a read-only `sql` query, e.g. `"sql": "SELECT name, score FROM companies ORDER BY score DESC LIMIT 20"`. Do the joining, summing, filtering, grouping, and latest-row selection in SQL (`JOIN`, `WHERE`, `GROUP BY`, `ORDER BY`, `LIMIT`) — do not create helper tables just to reshape data for a widget. `path`/`filter`/`formats` still apply to the returned rows.
- To show stored artifacts, use `kind: "file"` for one file and `kind: "file-list"` for a folder. File widgets may read `db/`, `knowledgebase/`, or `docs/` paths. Use `renderFormat: "auto" | "markdown" | "html" | "text" | "code" | "json" | "image" | "video" | "audio" | "pdf" | "link"` on `file`; use `listFormat: "list" | "cards" | "table" | "gallery"`, `recursive`, `extensions`, and `maxItems` on `file-list`. Prefer `file-list` for multiple images/videos/PDFs and `file` when a single artifact should be rendered inline.
- **Document escape hatch:** for a rich/narrative/highly-custom report the widget grammar can't express cleanly (or when the user dislikes the widget-composed look), have a step write a `.md`/`.html` document into `db/` and render the whole thing with one `file` widget (`renderFormat: "markdown"` or `"html"`). HTML renders in a sandboxed iframe (inline CSS/JS, tables, charts, dark mode all work); links to PDFs/files inside the document are clickable. Proactively offer this to the user — it's frequently the best fit for document-style reports — but state the tradeoff: a generated doc is a **static snapshot regenerated each run**, not live-bound to `db/`, whereas widgets auto-refresh. A **hybrid** (generated doc for the narrative body + a few live widgets for status/metrics/file-lists) gets both. See `get_reference_doc(kind="reporting-policy")` for the full dispatch.
- **Embed LIVE widgets inside a markdown document** — removes the "static snapshot" drawback for the `.md` path. In a `.md` file rendered via a `file`/`markdown` widget, a fenced ` ```report-widget ` block whose body is a widget JSON spec is replaced by a real, live, db-bound widget inline. The widget's `sql` runs on demand against `db/db.sqlite` (it need not be in the plan), so the narrative is freeform prose AND the numbers/tables/charts stay live. Example inside the `.md`:

  ~~~
  ## TDS Reconciliation — X SPACES

  Pulled live from the canonical DB:

  ```report-widget
  { "kind": "table", "db": "db/db.sqlite", "sql": "SELECT * FROM tds_rows WHERE pan = 'AAAFX2962N'" }
  ```

  ```report-widget
  { "kind": "stat", "db": "db/db.sqlite", "sql": "SELECT total_tds FROM tax_summary WHERE pan = 'AAAFX2962N'", "label": "Total TDS" }
  ```
  ~~~

  The JSON is the same widget schema you'd put in `report_plan.json` (any kind: table/stat/chart/cards/text/markdown/file/file-list/alert/pivot).

- **HTML reports get LIVE data, not embedded widgets** — the `.html` path is for full styling control, so instead of injecting our widgets the viewer hands the HTML the live data and lets it render its own visuals (Chart.js, custom tables, branded CSS — whatever). Inside the iframe the viewer exposes `window.report`:
  - `await window.report.query(sql)` — run a read-only SQL query against `db/db.sqlite` → array of row objects (the primary data source)
  - `await window.report.get(path)` — fetch any `db/`/`knowledgebase/`/`docs/` file live → parsed JSON (or text); use for markdown/text/assets, not structured data
  - `await window.report.getText(path)` — raw file text
  - `await window.report.fileUrl(path)` — an authenticated blob URL for a workspace file (image/PDF/etc.); use it in `<img src>`, `<a href>`, or `<iframe src>` to show artifacts
  - `window.report.openFile(path)` — open a file in the in-report preview modal (e.g. a "view PDF" button)
  - `window.report.theme` — `'dark'`/`'light'` (the app's current mode); the `report:data` event fires on load + refresh, `report:theme` on toggle — render/restyle in their handlers
  (all paths scoped to `db/`/`knowledgebase/`/`docs/`, same as file widgets)

  ~~~html
  <h1>Portfolio Report</h1>
  <div id="total">—</div>
  <canvas id="chart"></canvas>
  <script>
    window.addEventListener('report:data', async () => {
      const rows = await window.report.query('SELECT total_portfolio_value FROM sync_runs');
      const total = rows.reduce((s, r) => s + r.total_portfolio_value, 0);
      document.getElementById('total').textContent = '₹' + total.toLocaleString('en-IN');
      // ...draw your own Chart.js chart / styled table from the rows
    });
  </script>
  ~~~

  This is the right model for HTML: live data + 100% control over look. (Markdown embeds our widgets; HTML renders its own.) **Prefer markdown** overall — it renders richly (headings, tables, file links, embedded widgets) with zero styling burden; reach for HTML when you want bespoke/branded visuals and are willing to write the rendering yourself.
- **Writing a GOOD report document (md/html).** Mechanics above place content; these make it readable — instruct the step that generates the doc accordingly:
  - **Lead with the answer.** Title, then a short summary/TL;DR block up top — the key numbers, status, and "what needs action" — before any detail. A reader should get the verdict in the first screen.
  - **Structure + scannability.** Use clear section headings; short paragraphs and bullets over walls of prose; **bold** the key figures; one logical section per topic/entity. For multi-entity reports, one section (or tab) per entity.
  - **Show data as data.** Use tables (or embedded `table`/`stat` widgets) for numbers — never dump raw JSON or logs into the prose. Use status labels/semantic colour (✅ ok / ⚠️ attention / ❌ fail) for pass-fail fields.
  - **Embed live widgets for anything that changes** (totals, per-entity tables, file lists, metrics) via the syntaxes above; hardcode only genuinely static narrative. This keeps the doc from going stale.
  - **Link, don't inline, big artifacts.** Reference PDFs/files with relative links (clickable, open in the in-report viewer) instead of pasting their contents.
  - **HTML specifics:** make it **self-contained** — inline all CSS/JS, no external CDN. **Height auto-sizes to your content** — do NOT pin the body to a fixed or viewport height (`height: 100vh`/`100%`) and do NOT build your own scroll container; let content flow top-to-bottom and the report frame grows to fit it (no clipping, no inner scrollbar). Get live data from `window.report` (see the HTML data bullet above).
  - **Design quality — aim for a polished, "designed" report, not a default-styled page.** You own the look; hold a high bar:
    - **Restraint & hierarchy.** One accent colour, a small neutral palette, generous whitespace. Establish clear typographic hierarchy (a few sizes/weights, not many). Align everything to a consistent spacing scale (e.g. 4/8px steps). Let whitespace do the work — crowded ≠ informative.
    - **Typography.** Use a clean system stack (`system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`); comfortable line-height (~1.5) and line length (~60–80ch for prose); tabular/aligned numbers in tables (`font-variant-numeric: tabular-nums`).
    - **Colour & status.** Neutral surfaces; semantic colour only for meaning (green ok / amber attention / red fail), not decoration. Ensure **WCAG AA contrast** (≥4.5:1 body text) in BOTH themes. The app ships an `anthropic` aesthetic — warm off-white/clay neutrals with a single restrained accent — match that feel unless the user asks for brand colours.
    - **Structure.** Lead with a summary/KPI band, then sections with quiet hairline dividers or cards (subtle 1px borders, gentle radius ~8–12px, shadows sparingly). Consistent card padding. Tables: light row separators, subheads, right-align numbers.
    - **Charts.** Minimal chart-junk — no heavy gridlines/3D; label directly; reuse the palette; clear titles/units.
    - **Polish pass.** Before finishing, check: consistent spacing, aligned columns, no clipped/overflowing content at ~480px, AA contrast in light AND dark, no lonely default-blue links or unstyled tables.
  - **Dark + light theming (design BOTH, follow the app).** The app tells you the current mode — the frame mirrors it onto the iframe `<html>` as a `.dark` class **and** `data-theme="dark|light"`, exposes `window.report.theme` (`'dark'|'light'`), and fires a `report:theme` event when the user toggles. Define your palette as CSS variables and override them per theme; key off the class/attribute (with `prefers-color-scheme` only as a standalone fallback):
    ```css
    :root { --bg:#faf9f5; --fg:#1a1a1a; --muted:#6b6b6b; --accent:#cc785c; --card:#fff; --border:#e8e4dc; }
    :root.dark, [data-theme="dark"] { --bg:#1a1714; --fg:#ece9e3; --muted:#a8a29a; --accent:#e0a08a; --card:#241f1b; --border:#332d27; }
    body { background:var(--bg); color:var(--fg); }
    ```
    Never hardcode a single-mode colour (e.g. plain `#000`/`#fff`) — it breaks in the other theme. Re-style on the `report:theme` event if your JS-rendered charts bake colours in.
  - **Responsive design (raw HTML is YOUR job).** The report renders at three widths: **mobile ≈ 480px, tablet ≈ 880px, desktop full (content ≈ 1024px)**. Author HTML that flows at all three: use **fluid widths** (`%` / `max-width: 100%`, never fixed `px` page widths), make wide tables wrap or scroll (`overflow-x:auto`), **stack multi-column layouts on narrow screens** with `@media (max-width: 640px)` and `(max-width: 960px)`, use relative font sizes, and never assume a desktop width. A quick check: it must read with no horizontal overflow at ~480px. (Markdown documents and report-plan widgets reflow automatically — this only applies to hand-written HTML.)
  - **SELF-REVIEW the design visually — don't ship HTML you haven't looked at.** You cannot judge a visual design by reading its source; render it and look. Loop:
    1. Build a **self-contained** version of the report HTML with **data baked in** — run the queries yourself and inline the actual numbers/rows, and inline images as base64 (or omit). It must NOT depend on `window.report` (that only exists inside the app; opened standalone it renders blank), and must reference nothing external.
    2. Render it with the **`agent-browser` CLI** (no server URL serves a workspace file as a renderable page — the document API returns JSON or a download). Write the self-contained HTML to a temp path and open it by `file://` (verified to work — agent-browser runs on the same host):
       `agent-browser open "file:///abs/path/to/_review.html"` → `agent-browser viewport 480 900` → `agent-browser screenshot /tmp/mobile.png` → `agent-browser viewport 1280 900` → `agent-browser screenshot /tmp/desktop.png`.
       (A `data:text/html;base64,<…>` URL also works if you don't want a temp file.) Then `read_image` each PNG.
    3. `read_image` each screenshot and **critique against the design bar above** — hierarchy, spacing, contrast in light AND dark (screenshot both — toggle a `.dark` class / `data-theme="dark"` on `<html>` in the data: HTML), alignment, no clipping/overflow, chart legibility.
    4. Fix the real report HTML and repeat until it looks genuinely polished.

    Do this whenever you author or substantially change an HTML report — a screenshot-and-look catches the cramped spacing / broken dark mode / overflowing table that source review misses.
- For dashboard-style layouts: call `set_section_layout` to put a section into CSS Grid mode (columns 1–24), then pass `layout: { span }` in the widget config so widgets span N columns. Use `mode: "tabs"` when a workflow has route-specific views; then pass `tab: "Route name"` to `upsert_report_widget` so widgets for the same route render under one tab. Prefer tabs over separate duplicate sections when routes share the same conceptual report area. Without a section layout, sections use the default flex layout.
- Route-tab pattern: create one section for the conceptual area (for example `Route Evidence`, `Route Results`, or `Agent Outputs`), set that section to `mode: "tabs"`, and put every widget for a given route under the same `tab` value. Do not create many near-identical sections named after routes unless each route genuinely needs a different page-level narrative.
- For per-report color palettes: call `set_report_theme` with `brand` / `warm` / `cool` for bundled themes, or pass `colors: { primary, accent, card, muted, border, chart: [...] }` (hex strings) for an inline custom palette — useful for brand-specific colors (HDFC red, Citi blue, etc.) that no bundled theme matches. Omit fields you don't want to override; pass null/empty to clear.
- After every edit to `reports/report_plan.json`, call `validate_report_plan`.
- When you need to inspect what the final report will actually show with current data, call `preview_report_render`.

### When data is missing — running steps from this mode

If a widget renders empty because the underlying `db/db.sqlite` table hasn't been populated yet, you have `execute_step` and `run_full_workflow` available. Use them to make the data exist:
- For a single missing table, run only the step(s) that write it: `execute_step(step_id, group_name)`.
- For a fresh workflow with no runs yet, `run_full_workflow(group_name="...", disable_eval=true)` is the right fallback for report data. Report authoring does not own eval refreshes; omit `disable_eval=true` only when the user explicitly asks to refresh eval-backed widgets.
- Diagnose first with `review_workflow_results` and `get_report_plan` — don't run steps blindly. The widget might be pointing at the wrong path or filter, in which case the fix is in the plan, not in the data.

### What you do NOT do here

- Do not use report work as a reason to make speculative workflow/eval changes. If the user asked only for dashboard/report changes, stay within report-plan tools plus read/preview/validation. If the report exposes a real workflow-quality issue, then use workshop tools deliberately and explain the boundary.

### Reporting workflow

1. Clarify what the user wants to see.
2. Call `get_report_plan` for IDs / current structure.
3. If the data isn't there yet, run the right step(s) (or full workflow) to populate `db/db.sqlite`.
4. Use the report-plan mutation tools to update `reports/report_plan.json`.
5. Call `validate_report_plan`. Fix errors, validate again.
6. Optionally call `preview_report_render` to show the user what it will look like.

**Empty states:** if no widget resolves to non-empty data, the viewer hides the report entirely — no placeholder needed.
