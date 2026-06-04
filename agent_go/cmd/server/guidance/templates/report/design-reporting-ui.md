Design the workflow's reporting UI from the ground up. A report is a set of **documents** registered in `reports/report_plan.json` — each an **HTML** file (primary; live data via `window.report`, renders its own visuals) or a **Markdown** file (secondary; static narrative with baked-in numbers). There is no widget grammar. **Load `get_reference_doc(kind="report-plan")` first and follow it** — it carries the full `window.report` API, the good-document + design-quality bar, theming, and responsive rules. If you write any standalone `.html`, also follow `get_reference_doc(kind="html-output")`.{{if .Focus}}

Focus on: {{.Focus}}.{{end}}

STEP 1 — UNDERSTAND WHAT TO SHOW
- Clarify with the user what the report should answer: who reads it, the key questions/KPIs, and whether it's one report or per-entity (per-PAN, per-account, per-route).
- Inspect the data the report will read: `sqlite3 db/db.sqlite ".schema"` and `SELECT ... LIMIT` the relevant tables; check `db/README.md`, `db/assets/`, and `knowledgebase/context/context.md`. If routed, read the route list and decide which tabs the report needs.
- If the tables are empty, run the producing step (`execute_step`) or `run_full_workflow(group_name=..., disable_eval=true)` so you design against real data, not guesses.

STEP 2 — PICK THE FORMAT (one decision, you make it for the user)
- **Visualizing data, or it must stay live on view → HTML.** Charts, KPI tiles, tables, dashboards, branded/print layouts. HTML reads `db/db.sqlite` live via `window.report.query(sql)` and draws its own visuals.
- **Narrative document with numbers baked in at generation → Markdown.** Prose read top-to-bottom; themes automatically; cheapest to author.
- Default to **HTML** for anything dashboard-like or that should re-read the db when viewed. Use markdown only for static narrative.
- For a multi-entity report, author one document per entity and put them in a **tabbed** section.

STEP 3 — AUTHOR THE DOCUMENT(S)
- Write the file under `db/reports/` (e.g. `db/reports/report.html`, or `db/reports/<entity>/report.html` per entity). **Author it ONCE and wire HTML to read data live via `window.report`** — do NOT add a workflow step that regenerates the report each run, and do NOT bake live data into a static file.
- For HTML, hold the design bar from the reference doc: lead with a summary/KPI band; clear hierarchy and generous whitespace; one accent + neutral palette; tables right-align numbers; **WCAG AA contrast in BOTH light and dark**; self-contained (inline all CSS/JS, no CDN); **do not pin body height** (the frame auto-sizes); fluid/responsive down to ~480px; key off the app theme (`.dark` / `data-theme`, the `report:theme` event) or use the injected `hsl(var(--token))` palette so it matches the app and flips with light/dark.
- For markdown, lead with a TL;DR, use headings/bullets, **bold** key figures, and show numbers as tables — never raw JSON.

STEP 4 — REGISTER, VALIDATE, PREVIEW
- Register each document with `upsert_report_widget` (kind `file`, `renderFormat` `html` or `markdown`, `source` = the file path). For per-entity, `set_section_layout(mode="tabs")` and give each document `tab: "<entity name>"`. Set a palette with `set_report_theme` if wanted.
- Call `validate_report_plan` after every edit; fix and re-validate until clean.
- Call `preview_report_render` to confirm it renders with current data.

STEP 5 — OPTIONAL VISUAL REVIEW
- Only if the user asks to review it visually (or it's clearly part of the task): open `/report?path=<base64url path>` with `agent-browser` at desktop/tablet/mobile widths, render BOTH light and dark, `read_image`, and critique against the design bar — hierarchy, spacing, contrast in both themes, alignment, no clipping, chart legibility. Fix the real HTML and repeat until polished. Otherwise validate and stop. (Full instructions in the reference doc.)

Before authoring anything large, show the user your plan: which format, how many documents/tabs, and what each shows. Author after they confirm, then validate and preview.
