Review and improve the workflow's report for accuracy, live-data wiring, evidence coverage, and presentation quality in two passes. **Load `get_reference_doc(kind="report-plan")` first** — it is the canonical report model (HTML document(s) in `reports/report_plan.json` reading `db/db.sqlite` live via `window.report`, no widget grammar) plus the content guide, design-quality bar, theming, and responsive rules; judge the report against it. Use builder/improve.html as the shared improvement log: read it first if it exists, create it if it does not, and append your findings and applied decisions when you finish. If you touch any `.html` output, also follow `get_reference_doc(kind="html-output")`.{{if .Focus}}

Focus on: {{.Focus}}.{{end}}

INTENT
The report dashboard should help the user measure and track whether the workflow is achieving its goal, what changed in the current plan/strategy or plan draft/proposal, and which issues need attention. It is not only a data dump. A strong dashboard answers, above the fold:
- Are we on track against `soul.md` success criteria?
- Which success signals prove that: current value/state, target or baseline, trend/delta, and status?
- What changed or is being tried now (current plan, plan draft/replan proposal, active improvement, or important Pulse finding)?
- What is broken, blocked, stale, expensive, missing, or risky?
- What evidence supports that conclusion?

GOAL TRACKING CONTRACT
Before proposing visual/layout work, translate `soul.md` success criteria into the dashboard's tracked signals using existing evidence:
- For each important success criterion, show the best available signal from `db/db.sqlite`, `evaluation/`, `costs/`, `workflow.json`, `builder/improve.html`, or durable report-facing files.
- Prefer a compact goal band: status, current value/state, target/baseline, trend/delta vs prior run/window, last updated, and a short plain-language interpretation.
- If a success criterion cannot be measured from existing persisted evidence, show an honest "not measured yet" or "missing evidence" state and log the missing data requirement. Do not hardcode guesses and do not create a separate metrics system.
- Keep detailed tables/charts below the goal band; the user should know progress and issues before inspecting raw rows.

MODE
- **Interactive/user-initiated mode:** show all proposed changes concretely and ask before editing.
- **Scheduled/background mode** (the focus says scheduled, unattended, background, or no-confirm): do not ask. Apply only bounded report-only fixes that are clearly supported by existing evidence and existing data surfaces; record larger redesigns, missing-data needs, or workflow/eval changes as a Decision/Open Finding in `builder/improve.html`.
- Never invent data in the dashboard. If a useful dashboard section needs data the workflow does not persist yet, record the missing data requirement and choose harden/replan/DB work from the improve workflow instead.

PASS 1 — VALIDATION
Call validate_report_plan.
- For each error: explain what's wrong in plain language, name the document/entry it refers to, and propose the exact fix.
- For warnings: separate ones that would visibly degrade the report (a registered document missing/unreadable, wrong renderFormat) from cosmetic ones.
- **Errors are blockers: resolve them (apply the fix after user confirmation, or get an explicit decision to defer) BEFORE moving to PASS 2.** Improvement suggestions layered on top of a structurally broken plan waste the user's attention — the rendered preview in PASS 2 won't reflect reality until the plan validates clean.

PASS 2 — IMPROVEMENT SUGGESTIONS
Call preview_report_render first so you can inspect what the report actually renders like with current data — treat that as a required input. Then call get_report_plan and read the actual document file(s) under `db/reports/`. For HTML reports, also sample the data they read: run their queries against `db/db.sqlite` (`sqlite3 db/db.sqlite ".schema"` + `SELECT ... LIMIT`), and check `db/assets/`, `knowledgebase/context/context.md`, and `knowledgebase/notes/`. Use both the rendered preview and the raw data/document to propose improvements in these categories:

1. **Live vs stale.** The report is HTML; it should read its numbers live via `window.report.query` so it never goes stale. Flag any report that hardcodes data as static text (it should query the db instead), or that depends on a workflow step regenerating it each run (it shouldn't — author once, read live).
2. **Layout (insight-first / inverted pyramid).** Does it lead with the answer? Canonical skeleton: conditional alert/status banner → headline KPI tiles → the key supporting chart → detailed tables last. A report should read like a briefing (answer first, evidence below), not a data dump. For multi-entity reports, is each entity its own tab?
3. **Live-data correctness (HTML).** Do the `window.report.query` SQL statements hit the right tables/columns? Do the joins/aggregation/sort/limit happen in SQL (one `SELECT ... JOIN ... GROUP BY ... ORDER BY ... LIMIT`) rather than fetching everything and reshaping in JS, or relying on a pre-flattened helper table? Collapse derived/helper tables back to a query against the canonical tables.
4. **Visualization fit.** For each chart, is bar/line/area/pie right for the data? (bar=categorical, line=time series, pie=composition ≤6 slices.) Are numbers shown as tables (right-aligned, tabular-nums), not raw JSON/logs?
5. **Theme & color.** Does it follow the app's light/dark theme — keying off the `.dark`/`data-theme` and the `report:theme` event, or using the injected `hsl(var(--token))` palette? Is contrast **WCAG AA in BOTH themes**? Use semantic colour only for meaning (ok/attention/fail), not decoration. Set a per-report palette with `set_report_theme` only if a brand requires it.
6. **Responsive & self-contained (HTML).** No horizontal overflow at ~480px; wide tables scroll/wrap; multi-column layouts stack on narrow screens; all CSS/JS inline (no CDN); body height NOT pinned (the frame auto-sizes).
7. **Rendered reality check.** Based on the preview, what actually looks broken, cramped, misleading, empty, or visually weak even if the plan is valid?

In interactive mode, show ALL proposed changes concretely (before/after snippets of the HTML, or the plan entry) before editing. Ask whether to apply all, some, or none. Don't edit the report files or report_plan.json until I confirm.

In scheduled/background mode, apply only changes that are safe, local, and report-only, such as:
- adding a clearer goal-tracking/status band from existing `soul.md`, eval, Pulse, cost/time, workflow, or db data
- reordering sections so goal verdicts/issues come before detailed tables
- fixing static/stale values to live `window.report` reads
- fixing bad SQL, missing tabs, broken theme handling, responsive overflow, or low-contrast styling
- adding explicit empty/error states when evidence is missing

Defer and log, instead of editing, when the change would require new workflow data, a new plan step, evaluation redesign, business judgment, or a broad visual rewrite.

When you finish, update builder/improve.html with:
- what report evidence you reviewed
- the main report weaknesses you found
- what you recommended
- what was applied vs deferred

Record this as a prose Decision entry in `builder/improve.html` per `get_reference_doc(kind="review-improve-log")` — note what changed and the report file(s) touched (`reports/report_plan.json` or the report HTML). If it resolves a matching open finding, close that finding out in place (add a `Resolved YYYY-MM-DD — <how>` line): `/review-plan`'s Phase 4 includes a report audit, so report findings can land in builder/improve.html as open findings. This applies to chat-intent report fixes too.
