Review and improve reports/report_plan.json in two passes. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your report-plan findings and applied decisions when you finish.{{if .Focus}}

Focus on: {{.Focus}}.{{end}}

PASS 1 — VALIDATION
Call validate_report_plan.
- For each error: explain what's wrong in plain language, show the section + widget it refers to, and propose the exact fix (which line to change, to what).
- For warnings: group by severity. Flag ones that would visibly degrade the report (unknown chart_type, missing axis fields, invalid colors) separately from cosmetic ones (empty arrays that will fill in after a run).

PASS 2 — IMPROVEMENT SUGGESTIONS
Call preview_report_render first so you can inspect what the report actually renders like with current data. Treat that rendered preview as a required input, not an optional extra.

Then call get_report_plan yourself and also sample the underlying db/*.json and knowledgebase/*.json sources. Use both the rendered preview and the raw data/plan to propose improvements in these categories:

1. **Layout**: are the most important widgets at the top? Are there too many widgets in a row cramming the view? Is the H2 structure grouping related content?
2. **Chart-type fit**: for each chart, is bar/line/area/pie the right choice for that data? (bar=categorical, line=time series, pie=composition ≤6 slices)
3. **Color**: does the report use semantic coloring where meaningful (status fields, pass/fail, severity)? Suggest adding color_by + color_map for any status-like fields you see in the data. Propose palettes (colors + colors_dark) for brand consistency if multiple charts share a theme.
4. **Formatting**: any number/date/currency fields that should have a format preset? Any tables with too many columns that could benefit from hide_columns?
5. **Density**: any charts with >10 points that need top_n? Any tables without default_sort that would be hard to scan?
6. **Rendered reality check**: based on the preview, what actually looks broken, cramped, misleading, empty, or visually weak even if the JSON is technically valid?

Show ALL proposed changes as a diff (before/after snippets per widget) before editing. Ask whether to apply all, some, or none. Don't edit report_plan.json until I confirm.

When you finish, update builder/improve.md with:
- what report evidence you reviewed
- the main report weaknesses you found
- what you recommended
- what was applied vs deferred
