## Reporting Policy

The workflow has a **live frontend report viewer** at the top
toolbar's "Report" tab. It reads `reports/report_plan.json` and
renders the widget blocks defined there against `db/*.json`, durable
`db/assets/` references, `knowledgebase/` context/notes, and dedicated
workflow APIs for built-in `costs` / `evals` / `runs` widgets. It is
always available — there is **NO "generate report" phase**, no HTML/PDF
artifact to produce, no step that writes a finished report.

## Workshop mode: own the report plan

Workshop mode can author and maintain report widgets when reporting
needs to reflect optimization/evaluation/run evidence: creating
dashboard widgets, themes, layouts, custom colors, and
`reports/report_plan.json` edits. Keep report edits presentation-only
unless the user also asked for workflow hardening/eval changes.

### When the user asks "create a report" / "build a reporting UI" / "show me X in a dashboard"

- The answer is almost always: **update `reports/report_plan.json`
  via the report-plan tools** — add, move, toggle, or remove widgets.
- If the workflow has routing routes, todo_task predefined routes, or
  other route-specific outputs, prefer a tabbed report section:
  `set_section_layout(mode="tabs")`, then give each route's widgets
  `tab: "<route name>"`. Use one tab per user-meaningful route so the
  dashboard does not mix unrelated path outputs in one long section.
- When creating or improving a report for a routed workflow, first
  inspect the route list and decide the report structure from that
  route map. Use tabs for route-specific evidence/results by default;
  use a combined table only when the user explicitly wants cross-route
  comparison or the route outputs share the same schema.
- **Do NOT add a step that generates HTML, markdown, or any other
  "rendered report" artifact.**
- **Do NOT write Python that produces a dashboard file.** The React
  frontend already does this from the report plan.
- If the user wants a NEW kind of visualization the widget grammar
  can't express, say so explicitly and propose either (a) a new widget
  type to add to the renderer, or (b) reshaping the underlying `db/`
  data to fit existing widget types. Don't silently fall back to "I'll
  write a Python script that makes HTML."

### Diagnosis

- **When the report shows "No report yet":** `reports/report_plan.json`
  is missing or contains zero usable widgets. Fix by creating/updating
  the report plan.
- **When the report renders but is empty/missing widgets the user
  expects:** the plan resolved correctly but the widget `source` JSON
  is missing or has no rows yet. Either a step hasn't run, or the
  widget points at the wrong path. Inspect `reports/report_plan.json`
  and the actual `db/` files to diagnose.

### Refresh discipline

**Report viewer auto-updates** when the user opens or switches to the
Report tab — no rebuild step needed. After the agent updates
`report_plan.json`, the user just clicks Report (or refreshes if
they're already on it) to see the new widgets.

## Run mode: do not edit dashboards

Run mode does not own report widget authoring. If the user asks to
create dashboard widgets, themes, layouts, custom colors, or
`reports/report_plan.json` edits, tell them to switch to Workshop
mode. Do not offer to draft or edit `reports/report_plan.json` via
shell/direct file writes from Run mode.

## See also

For the report-plan toolchain (layouts/tabs, per-report themes,
route-tab patterns, missing-data triage, do-not rules, full
workflow), call `get_reference_doc(kind="report-plan")`.
