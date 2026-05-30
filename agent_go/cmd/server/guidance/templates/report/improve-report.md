Review and improve reports/report_plan.json in two passes. Use builder/improve.html as the shared improvement log: read it first if it exists, create it if it does not, and append your report-plan findings and applied decisions when you finish. If you generate any standalone `.html` output file during this flow, first call `get_reference_doc(kind="html-output")` and follow its quality checklist.{{if .Focus}}

Focus on: {{.Focus}}.{{end}}

PASS 1 — VALIDATION
Call validate_report_plan.
- For each error: explain what's wrong in plain language, show the section + widget it refers to, and propose the exact fix (which line to change, to what).
- For warnings: group by severity. Flag ones that would visibly degrade the report (unknown chart_type, missing axis fields, invalid colors) separately from cosmetic ones (empty arrays that will fill in after a run).

PASS 2 — IMPROVEMENT SUGGESTIONS
Call preview_report_render first so you can inspect what the report actually renders like with current data. Treat that rendered preview as a required input, not an optional extra.

Then call get_report_plan yourself and also sample the underlying db/*.json sources, db/assets references, knowledgebase/context/context.md when present, plus knowledgebase/notes/_index.json and the relevant topic markdown files. Use both the rendered preview and the raw data/plan to propose improvements in these categories:

1. **Layout (insight-first / inverted pyramid)**: order sections so the most decision-relevant content is above the fold. Canonical skeleton: conditional **alert** banners first → headline KPI **stat** tiles → the key supporting **chart**/**pivot** → detailed **tables** last. Are the most important widgets at the top? Are there too many widgets in a row cramming the view? Is the H2 structure grouping related content? A report should read like a briefing (answer first, evidence below), not a data dump.
2. **Chart-type fit**: for each chart, is bar/line/area/pie the right choice for that data? (bar=categorical, line=time series, pie=composition ≤6 slices)
3. **Theme & color**: default the report to the warm editorial **`anthropic`** theme via `set_report_theme(theme: "anthropic")` unless it already carries a deliberate brand palette (e.g. an HDFC-red / Citi-blue inline `colors` set the user asked for). The `anthropic` theme is the recommended baseline — warm "paper" surfaces, a single clay/terracotta accent, a muted earthy chart palette, and serif headings applied across the whole report; it makes a report read as a polished document rather than a generic dashboard. On top of the theme, layer semantic coloring where meaningful (status fields, pass/fail, severity): suggest adding color_by + color_map for any status-like fields you see in the data. Only propose custom palettes (colors + colors_dark) when a brand requires colors the theme doesn't provide.
4. **Formatting**: any number/date/currency fields that should have a format preset? Any tables with too many columns that could benefit from hide_columns?
5. **Density**: any charts with >10 points that need top_n? Any tables without default_sort that would be hard to scan?
6. **Rendered reality check**: based on the preview, what actually looks broken, cramped, misleading, empty, or visually weak even if the JSON is technically valid?
7. **In-widget transforms (JSONata `query`)**: prefer a `query:` JSONata expression over reading from pre-flattened helper files (`*_rows.json`, `*_summary.json`) or from a workflow `step-generate-report` flatten step. The pipeline order is `source → query → path → filter → render`, so when `query` returns the final array/scalar, leave `path` empty (or `$`). Use this to **collapse derived db files** back to their canonical sources.

   Common patterns:
   - filter rows: `query: rows[status='OPEN']`
   - filter + project: `query: rows[status='OPEN'].{symbol: symbol, pnl: pnl}`
   - aggregate: `query: $sum(rows[status='paid'].amount)` (stat widget, format: `currency-usd`)
   - top-N sort: `query: $sort(rows, function($l, $r) { $l.pnl < $r.pnl })[[0..9]]`
   - count: `query: $count(rows[status='OPEN'])`
   - group + sum: `query: rows{strategy: $sum(pnl)}`

   When proposing migrations, look for these smells and recommend collapsing:
   - a db file that obviously mirrors another db file but pre-grouped/pre-sorted (e.g. `per_strategy_rows.json` derived from `all_trades.json`)
   - a `step-generate-report` (or similarly named "flatten data") step whose only job is producing those helper files
   - any widget whose `source` is a `*_rows.json` / `*_summary.json` / `flat_*.json` — almost always replaceable by `source` = canonical db file + `query`
   Once every widget that reads a helper file has been migrated to `query` against the canonical source, flag the helper file and the flatten step as removable (Builder-mode change — surface as a deferred improvement, don't delete from Reporting mode).

Show ALL proposed changes as a diff (before/after snippets per widget) before editing. Ask whether to apply all, some, or none. Don't edit report_plan.json until I confirm.

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
   `linked_review_finding` is omitted when no matching finding exists. This improve.html decision trail is what makes report-layout changes auditable alongside plan/eval changes.

This applies to chat-intent report fixes too.
