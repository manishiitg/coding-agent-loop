## Reporting Policy

The workflow has a **live frontend report viewer** at the top
toolbar's "Report" tab. It reads `reports/report_plan.json` and
renders the widget blocks defined there against `db/*.json`, durable
`db/assets/` references, `knowledgebase/` context/notes, and dedicated
workflow APIs for built-in `costs` / `evals` / `runs` widgets. It is
always available — there is **NO separate "generate report" phase** and
no standalone dashboard artifact that replaces the viewer. (A workflow
may still write a narrative `.md`/`.html` document into `db/` and surface
it through a `file` widget — see the document escape hatch below; that is
content, not a replacement dashboard.)

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
- **Prefer `db/`-bound widgets for anything live, structured, or
  interactive** (tables, charts, stats, file lists) — they auto-refresh
  and stay in sync with run output.
- **Do NOT write Python or a step that emits a standalone HTML/PDF
  *dashboard* file meant to replace the report viewer.** The React
  frontend renders the report plan; a generated dashboard file
  duplicates it and goes stale. (This is the anti-pattern — distinct
  from the document escape hatch below.)
- If the user wants a NEW kind of visualization the widget grammar
  can't express, say so explicitly and propose one of: (a) a generated
  `.md`/`.html` document shown via a `file` widget (see escape hatch
  below — usually the fastest answer), (b) a new widget type to add to
  the renderer, or (c) reshaping the underlying `db/` data to fit
  existing widget types. Don't silently fall back to "I'll write a
  Python script that makes HTML."

### The md/html document escape hatch — suggest this proactively

When the user wants a rich, narrative, or highly custom layout the
widget grammar can't express cleanly — or simply doesn't like the
widget-composed look — the simplest answer is often: have a workflow
step write a detailed `.md` or `.html` document into `db/`, then surface
the whole thing with a single `file` widget:

```json
{ "kind": "file", "source": "db/reports/<name>.html", "renderFormat": "html" }
{ "kind": "file", "source": "db/reports/<name>.md",   "renderFormat": "markdown" }
```

The viewer renders Markdown via its renderer and HTML in a sandboxed
iframe — self-contained HTML (inline CSS/JS, tables, charts, dark mode)
all work, and file/PDF links inside the document are clickable and open
in the in-report preview. This is a first-class supported pattern, NOT
the dashboard anti-pattern above.

**Proactively offer this option, with the tradeoff, whenever a user asks
for a report or is unhappy with the existing widgets:**

- **Generated `.md`/`.html` document** — maximum layout/narrative
  flexibility, trivial plan (one widget), ideal for a report a human
  reads top-to-bottom. But it is a **static snapshot regenerated each
  run**, not live-bound to `db/`, and costs tokens to author every run.
- **Widgets** — live-bound to `db/`, auto-refresh, interactive and
  structured, consistent theming. But more authoring effort and less
  narrative freedom.

Best of both is a **hybrid**: a generated `.md`/`.html` for the rich
narrative body, plus a few live widgets (status/freshness stat, key
metrics, file lists) for signals that must stay current. md/html is
frequently the right call for document-style reports — don't default to
widgets silently; surface the choice and let the user decide.

### Choosing a report shape — one decision the agent makes for the user

There are three shapes on a spectrum from structured/live to freeform/
static. The user should NOT have to learn all three — you pick and name
the tradeoff:

| Shape | Layout | Live data? | Best for |
|---|---|---|---|
| **Widget plan** | structured (grid, tabs, spans, themes) | live, auto-refresh | dashboards, at-a-glance metrics/tables/charts, multi-route reports |
| **Markdown doc (+ embedded widgets)** | narrative / linear | live (embedded widgets) | a report read top-to-bottom — prose plus a few live numbers/tables |
| **HTML doc (+ embedded widgets)** | pixel-perfect / branded | live (embedded widgets) | highly designed/branded/print-like layouts that also want live data |

Decision rule: default to a **widget plan** for dashboards/live metrics;
use a **markdown doc** for a narrative report (simplest to author); use an
**HTML doc** when you need pixel-perfect/branded layout. Both document
shapes can embed live widgets, so the old "documents are static" tradeoff
is mostly gone — a doc with no embedded widgets is a static snapshot, a doc
full of them ≈ a widget plan with custom layout.

**Embedding live widgets in a document:**
- **Markdown** — a fenced ` ```report-widget ` block whose body is a widget
  JSON spec renders as a live db-bound widget inline.
- **HTML** — a placeholder `<div data-report-widget='{…spec…}'></div>`; the
  viewer injects app styles into the iframe and mounts a live widget into
  it (content inside the div is a static fallback for standalone viewing).

Same widget JSON schema as `report_plan.json` (any kind). Full syntax +
examples, and the **"writing a good report document" formatting guide**
(lead with a summary, structure with headings, data as tables/widgets not
raw JSON, self-contained HTML, dark mode): `get_reference_doc(kind="report-plan")`.

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
