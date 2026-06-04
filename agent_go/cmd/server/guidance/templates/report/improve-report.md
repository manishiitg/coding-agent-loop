Review and improve the workflow's report in two passes. A report is a set of **documents** registered in `reports/report_plan.json` — each an **HTML** file (live data via `window.report`, renders its own visuals) or a **Markdown** file (static narrative). There is no widget grammar. **Load `get_reference_doc(kind="report-plan")` first** for the full `window.report` API, the good-document + design-quality bar, theming, and responsive rules. Use builder/improve.html as the shared improvement log: read it first if it exists, create it if it does not, and append your findings and applied decisions when you finish. If you touch any `.html` output, also follow `get_reference_doc(kind="html-output")`.{{if .Focus}}

Focus on: {{.Focus}}.{{end}}

PASS 1 — VALIDATION
Call validate_report_plan.
- For each error: explain what's wrong in plain language, name the document/entry it refers to, and propose the exact fix.
- For warnings: separate ones that would visibly degrade the report (a registered document missing/unreadable, wrong renderFormat) from cosmetic ones.

PASS 2 — IMPROVEMENT SUGGESTIONS
Call preview_report_render first so you can inspect what the report actually renders like with current data — treat that as a required input. Then call get_report_plan and read the actual document file(s) under `db/reports/`. For HTML reports, also sample the data they read: run their queries against `db/db.sqlite` (`sqlite3 db/db.sqlite ".schema"` + `SELECT ... LIMIT`), and check `db/assets/`, `knowledgebase/context/context.md`, and `knowledgebase/notes/`. Use both the rendered preview and the raw data/document to propose improvements in these categories:

1. **Format fit.** Is each document in the right format? Anything visual/dashboard-like or that should stay live on view belongs in **HTML** (reads `db/db.sqlite` via `window.report.query`); static narrative belongs in **markdown**. Flag a markdown report whose numbers should be live (convert to HTML), or an HTML report that's really just static prose (could be markdown).
2. **Layout (insight-first / inverted pyramid).** Does it lead with the answer? Canonical skeleton: conditional alert/status banner → headline KPI tiles → the key supporting chart → detailed tables last. A report should read like a briefing (answer first, evidence below), not a data dump. For multi-entity reports, is each entity its own tab?
3. **Live-data correctness (HTML).** Do the `window.report.query` SQL statements hit the right tables/columns? Do the joins/aggregation/sort/limit happen in SQL (one `SELECT ... JOIN ... GROUP BY ... ORDER BY ... LIMIT`) rather than fetching everything and reshaping in JS, or relying on a pre-flattened helper table? Collapse derived/helper tables back to a query against the canonical tables.
4. **Visualization fit.** For each chart, is bar/line/area/pie right for the data? (bar=categorical, line=time series, pie=composition ≤6 slices.) Are numbers shown as tables (right-aligned, tabular-nums), not raw JSON/logs?
5. **Theme & color.** Does it follow the app's light/dark theme — keying off the `.dark`/`data-theme` and the `report:theme` event, or using the injected `hsl(var(--token))` palette? Is contrast **WCAG AA in BOTH themes**? Use semantic colour only for meaning (ok/attention/fail), not decoration. Set a per-report palette with `set_report_theme` only if a brand requires it.
6. **Responsive & self-contained (HTML).** No horizontal overflow at ~480px; wide tables scroll/wrap; multi-column layouts stack on narrow screens; all CSS/JS inline (no CDN); body height NOT pinned (the frame auto-sizes).
7. **Rendered reality check.** Based on the preview, what actually looks broken, cramped, misleading, empty, or visually weak even if the plan is valid?

Show ALL proposed changes concretely (before/after snippets of the HTML/markdown, or the plan entry) before editing. Ask whether to apply all, some, or none. Don't edit the report files or report_plan.json until I confirm.

When you finish, update builder/improve.html with:
- what report evidence you reviewed
- the main report weaknesses you found
- what you recommended
- what was applied vs deferred

Each new entry that records a *proposed but not-yet-applied* report change gets a stable id of the form `I-YYYY-MM-DD-NNN` — today's date plus a 3-digit sequence that restarts at `001` per day. Scan the file for today's highest existing sequence and continue from there; never reuse an id.

CLOSE-OUT EDITS — read this carefully.

Reporting findings rarely live in builder/review.html (the /review-* commands focus on plan/eval/cost/speed, not report layout). But if you can find a matching finding (e.g. user previously flagged "the funnel chart is unreadable" and that landed in review.html), apply close-out the same way the other /improve-* commands do:

1. **Edit builder/review.html** to append, on its own line immediately after each matched finding:
   ```
   **[RESOLVED YYYY-MM-DD — <one-line how it was fixed>]**
   ```

2. **Append a structured builder/improve.html decision block** for the report change (use `diff_patch_workspace_file`):
   ```improve-decision
{"id":"<short-id>","ts":"<ISO-8601 UTC>","source":"agent","trigger":"improve-report","applied_changes":["reports/report_plan.json"],"rationale":"<one-line>","linked_review_finding":["F-..."]}
   ```
   `linked_review_finding` is omitted when no matching finding exists. This improve.html decision trail is what makes report changes auditable alongside plan/eval changes.

This applies to chat-intent report fixes too.
