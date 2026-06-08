## REPORT PLAN — reports/report_plan.json

Workshop may maintain the live frontend report defined by `reports/report_plan.json` so the report stays aligned with current outputs, metrics, and evaluation evidence. Use report-plan tools for report edits; use workshop tools only when the underlying workflow behavior or eval coverage actually needs to change.

### The report model — an HTML document

A report is one or more **HTML documents**. There is no widget grammar and no second format: every report is HTML. The viewer renders each document in a sandboxed iframe and hands it the live data via the `window.report` API; the HTML renders its own visuals (charts, tables, KPI tiles, branded CSS — whatever) with full styling control.

HTML is a superset of anything a plain document needs — prose, headings, tables, links — AND it can read the db live and draw charts. Start every report from the shipped HTML skeleton so even a simple narrative report is quick to author and looks consistent: load `get_reference_doc(kind="html-output")` for the layout baseline, dark-mode styles, and inline chart pattern.

### The reporting toolchain

- Before move/remove/toggle operations, call `get_report_plan` so you have stable section, entry, and document IDs.
- A report is registered in `reports/report_plan.json` as one or more **HTML document widgets** pointing at a file:
  ```json
  { "kind": "file", "source": "db/reports/<name>.html", "renderFormat": "html" }
  ```
  Use `upsert_report_widget` (kind `file`, `renderFormat` `html`) to create/update, `move_report_widget` to reposition, `toggle_report_widget` to hide/show, and `remove_report_widget` to delete. Document files live under `db/reports/` and may reference `db/`, `knowledgebase/`, or `docs/` assets.
- **Author the document once; wire it to read data LIVE. Do NOT add a workflow step that (re)generates the report each run.** An HTML report reads `db/db.sqlite` live via `window.report` (below), so the workflow's normal steps write the data and the report just reads it — always current, zero per-run work. (Baking live data into the HTML as static text is the anti-pattern — it goes stale and costs tokens every run.)
- For a multi-entity report (per-PAN, per-account, per-route), put one document per entity into a **tabbed** section: call `set_section_layout(mode="tabs")`, then pass `tab: "<entity name>"` to `upsert_report_widget` for each document so they render under one tabbed area instead of many sections.
- For per-report color palettes: call `set_report_theme` with `brand` / `warm` / `cool` for bundled themes, or pass `colors: { primary, accent, card, muted, border, chart: [...] }` (hex strings) for an inline custom palette. Omit fields you don't want to override; pass null/empty to clear.
- After every edit to `reports/report_plan.json`, call `validate_report_plan`.
- When you need to inspect what the final report will actually show with current data, call `preview_report_render`.

### HTML reports get LIVE data via `window.report`

The viewer hands the HTML the live data and the HTML renders its own visuals (charts, custom tables, branded CSS). Inside the iframe the viewer exposes `window.report`:
- `await window.report.query(sql)` — run a read-only SQL query against `db/db.sqlite` → array of row objects (the primary data source). Do the joining, summing, filtering, grouping, and latest-row selection in SQL (`JOIN`, `WHERE`, `GROUP BY`, `ORDER BY`, `LIMIT`).
- `await window.report.get(path)` — fetch a workflow file live → parsed JSON (or text). Reads the stores AND the operational/config data (costs, metrics, evals, groups, workflow.json, …) — see **Read scope** below. Use for files/assets/operational data; for bulk `db/` rows prefer `query`.
- `await window.report.getText(path)` — raw file text
- `await window.report.getHtml(path)` — render a **markdown file** to an HTML string (the app's markdown engine + GFM tables), wrapped in `<div class="report-markdown">` with a default theme-aware prose style. Use it to **embed a rendered `.md` inline inside your HTML report**: `el.innerHTML = await window.report.getHtml('db/reports/notes.md')`. Lets you keep your custom HTML design and drop a markdown-rendered section in between. Override `.report-markdown` in your CSS to restyle.
- `window.report.renderMarkdown(md)` — same renderer, but for a markdown **string you already hold** (not a file): a `db`/`sql` value, a knowledgebase field, or inline text. **Synchronous** (no `await`), returns the same `<div class="report-markdown">…</div>`. Use it to render markdown that lives in your data, e.g. a notes column in a table: `cell.innerHTML = window.report.renderMarkdown(row.notes_md)`. This is the answer to "my content is markdown but my report is HTML" — call it from your HTML report; you do NOT need a React markdown component.
- `await window.report.fileUrl(path)` — an authenticated blob URL for a workspace file (image/PDF/etc.); use it in `<img src>`, `<a href>`, or `<iframe src>` to show artifacts
- `window.report.openFile(path)` — open a file in the in-report preview modal (e.g. a "view PDF" button)
- `window.report.theme` — `'dark'`/`'light'` (the app's current mode); the `report:data` event fires on load + refresh, `report:theme` on toggle — render/restyle in their handlers

**Read scope — what a report can pull.** Beyond `db/`, `knowledgebase/`, `docs/`, `get`/`getText`/`getHtml` can also read the workflow's operational + config data (all stable top-level paths — read them and surface whatever the report actually needs; you do NOT have to use all of it):
- **Costs / tokens** — `get('costs/phase/token_usage.json')`, `get('costs/execution/<group>/<date>.json')`
- **Metrics** — `get('planning/metrics.json')`
- **Eval results** — `get('evaluation/evaluation_plan.json')` and the `evaluation/` scores
- **Variable groups** — `get('variables/variables.json')` → `groups: [{ name, enabled, values }]`
- **Workflow config** — `get('workflow.json')` (id, label, schedule, …)
- **Soul / persona** — `getHtml('soul.md')`
- **Prior generated docs** — `getText('improve.html')`, `getText('review.html')`

Use this to make reports context-aware — e.g. show this run's cost and eval score, badge the active variable group, or include the workflow's purpose from `soul.md`. (`runs/` per-run transcripts are NOT exposed: per-run paths aren't knowable at authoring time and can be sensitive.)

```html
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
```

### What to put in the report (content, not just chrome)

A report answers questions and drives action — it is not a mirror of the database. Decide the content before the layout:
- **Lead with the verdict + what needs action.** The top states the bottom line — pass/fail, the headline number, and the 1–3 things that need attention — before any detail. A reader who leaves after the first screen should still have the answer.
- **Show numbers in context, never bare.** A lone value means little — compare it: vs. the previous run/period (delta, ▲/▼), vs. a target/baseline/threshold, vs. other entities. Trends and deltas usually beat a single snapshot.
- **Surface the exceptions.** Call out outliers, failures, regressions, and stale/missing data explicitly — sort/filter so what matters is on top; don't make the reader hunt the one red row in a long table.
- **Assign status from explicit thresholds.** Drive ✅ ok / ⚠️ attention / ❌ fail from stated rules (e.g. "≥90% = ok") so the colour is consistent and trustworthy.
- **One entity per tab.** Per-PAN/route/account reports give each entity its own document/tab with its own verdict — never blend unrelated entities into one page.
- **Content quality starts upstream.** The report only shows what the workflow's steps write to `db/db.sqlite`. If the data needed to answer the key question isn't there (a computed metric, a baseline, a status flag), the fix is in the producing step — instruct it to emit that — not in the HTML.

### Writing a GOOD report document (formatting)

The content section above decides WHAT to show; these make it READABLE when you author the HTML:
- **Structure + scannability.** Use clear section headings; short paragraphs and bullets over walls of prose; **bold** the key figures; one logical section per topic/entity. For multi-entity reports, one document (tab) per entity.
- **Show data as data.** Use tables for numbers — never dump raw JSON or logs into the prose. Use status labels/semantic colour (✅ ok / ⚠️ attention / ❌ fail) for pass-fail fields.
- **Keep live data live.** For anything that changes (totals, per-entity tables, file lists, metrics), read it from `window.report` so the report never goes stale. Only hardcode genuinely static narrative.
- **Link, don't inline, big artifacts.** Reference PDFs/files with relative links (clickable, open in the in-report viewer) instead of pasting their contents.
- **Self-contained.** Inline all CSS/JS, no external CDN. **Height auto-sizes to your content** — do NOT pin the body to a fixed or viewport height (`height: 100vh`/`100%`) and do NOT build your own scroll container; let content flow top-to-bottom and the report frame grows to fit it (no clipping, no inner scrollbar). Get live data from `window.report` (above).
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
- **Match the app palette via its CSS variables (preferred over inventing colours).** The app injects its **resolved theme tokens** into the report iframe as CSS variables (HSL triplets) that **auto-switch with the app's light/dark mode** — so you don't have to define your own palette or guess hex values. Use them with `hsl(var(--token))`:
  ```css
  body { background: hsl(var(--background)); color: hsl(var(--foreground)); }
  .card { background: hsl(var(--card)); border: 1px solid hsl(var(--border)); }
  .accent { color: hsl(var(--primary)); }       /* primary accent */
  .muted { color: hsl(var(--muted-foreground)); }
  ```
  Available: `--background --foreground --card --card-foreground --primary --primary-foreground --secondary --muted --muted-foreground --accent --accent-foreground --border --input --ring --destructive` and chart colours `--chart-1 … --chart-5`. These already reflect the active light/dark theme (and any report theme), so a report built on them matches the rest of the app and flips correctly when the user toggles — no extra work. (You may still define your own palette if you want a bespoke brand look; the tokens are there when you want consistency.)
- **Responsive design (raw HTML is YOUR job).** The report renders at three widths: **mobile ≈ 480px, tablet ≈ 880px, desktop full (content ≈ 1024px)**. Author HTML that flows at all three: use **fluid widths** (`%` / `max-width: 100%`, never fixed `px` page widths), make wide tables wrap or scroll (`overflow-x:auto`), **stack multi-column layouts on narrow screens** with `@media (max-width: 640px)` and `(max-width: 960px)`, use relative font sizes, and never assume a desktop width. A quick check: it must read with no horizontal overflow at ~480px.
- **Optional visual review.** If the user asks you to review the report visually, critique the design, or verify screenshots, render it and look. Otherwise do not spend time/tokens on browser screenshots just because you authored or changed an HTML report. When reviewing, use the dedicated frontend report URL so the app renders the same live report the user sees:
  1. Build the URL: `/report?path=<base64url UTF-8 workspace path>`, where the decoded path is the workflow root, e.g. `Workflow/My Workflow`.
  2. Open that short URL with **`agent-browser`** at desktop/tablet/mobile widths and screenshot only as needed. **Always screenshot with `--full`** (e.g. `screenshot --full report.png`) so the capture covers the report's ENTIRE scroll height — a report is usually taller than the viewport, and a plain screenshot clips everything below the fold. Optionally `set viewport <w> <h> 2` first for a sharper 2× capture. **Do NOT paste base64 HTML or a long `data:` URL into a tool call** — it bloats the transcript and can dominate token usage. Also do not use `file://`; agent-browser may run away from the host filesystem and open `about:blank`.
     ```bash
     encoded_path="$(python3 - <<'PY'
     import base64
     print(base64.urlsafe_b64encode("Workflow/My Workflow".encode()).decode().rstrip("="))
     PY
     )"
     agent-browser open "http://localhost:<app-port>/report?path=${encoded_path}"
     agent-browser viewport 1280 900 && agent-browser screenshot /tmp/desktop.png
     agent-browser viewport 480 900  && agent-browser screenshot /tmp/mobile.png
     ```
     If `localhost` is unreachable because the browser daemon is in a different network namespace, retry the same app URL with `host.docker.internal`. Use a `data:` URL only as a last-resort fallback for a tiny standalone HTML file; never inline a large base64 payload in a browser/tool argument.
  3. **Match what the app shows — set the theme.** The app renders the report in **dark mode** when the user is in dark mode, by putting `class="dark" data-theme="dark"` on the iframe `<html>`. Your standalone render won't have that, so it looks light even though the app shows dark. To review faithfully, render BOTH: once with `<html>` plain (light) and once with `<html class="dark" data-theme="dark">` (dark) — screenshot each. `read_image` and **critique against the design bar above** — hierarchy, spacing, **contrast in BOTH light and dark**, alignment, no clipping/overflow, chart legibility.
  4. Fix the real report HTML and repeat until it looks genuinely polished.

  Use this only when visual review was requested or is clearly part of the task; otherwise validate the report plan and stop.

### When data is missing — running steps from this mode

If an HTML report renders empty because the underlying `db/db.sqlite` table hasn't been populated yet, you have `execute_step` and `run_full_workflow` available. Use them to make the data exist:
- For a single missing table, run only the step(s) that write it: `execute_step(step_id, group_name)`.
- For a fresh workflow with no runs yet, `run_full_workflow(group_name="...", disable_eval=true)` is the right fallback for report data. Report authoring does not own eval refreshes; omit `disable_eval=true` only when the user explicitly asks to refresh eval-backed data.
- Diagnose first with `review_workflow_results` and `get_report_plan` — don't run steps blindly. The report's `window.report.query` might be hitting the wrong table/column, in which case the fix is in the HTML, not in the data.

### What you do NOT do here

- Do not use report work as a reason to make speculative workflow/eval changes. If the user asked only for report changes, stay within report-plan tools plus read/preview/validation. If the report exposes a real workflow-quality issue, then use workshop tools deliberately and explain the boundary.

### Reporting workflow

1. Clarify what the user wants to see.
2. Call `get_report_plan` for IDs / current structure.
3. If the data isn't there yet, run the right step(s) (or full workflow) to populate `db/db.sqlite`.
4. Author/update the HTML document (start from the `html-output` skeleton), then register/update it with `upsert_report_widget` (kind `file`, `renderFormat` `html`).
5. Call `validate_report_plan`. Fix errors, validate again.
6. Optionally call `preview_report_render` to show the user what it will look like.

**Empty states:** if no document resolves to renderable content, the viewer hides the report entirely — no placeholder needed.
