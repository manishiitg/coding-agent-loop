# Plan-change impact analysis

A workflow is a web of **implicit dependencies**: a step's output feeds downstream steps, evals score it, the report dashboard queries its data, db stores it, and learnings/KB describe its behavior. Change a step and those silently rot — a downstream step reads a field you renamed, a report query returns nothing, an eval scores the wrong thing, a learning teaches the old behavior.

**Whenever you change a plan** — add / remove / reorder a step, or change a step's output contract, behavior, or description — you are not done until you have checked and reconciled the blast radius. This applies whether the change came from an active Workshop edit, an approved Goal Advisor proposal, or a bounded Pulse Fixer repair.

## 1. Name the change surface
First, pin down exactly what other artifacts key off — the **surface** of the change:
- the **step id**,
- the **output file / artifact** it produces (path + the JSON fields / shape),
- the **db tables / columns** it writes,
- its **topic / behavior** (what learnings & KB describe).

If you renamed a field, removed an output, changed a file path, changed what's written to db, or changed what the step does — that's the surface to trace. A change with no surface change (see Scope note) has no blast radius.

## 2. Trace each dimension — search, don't guess
For each dimension, **search the workspace** for references to the surface and reconcile every hit. Don't reason about ripple effects in the abstract — grep for the *actual* references (step id, output file/field, db table/column, topic).

- **Downstream steps** — search `planning/plan.json` and step descriptions for the step id / output file / changed field. A later step that consumes a field you changed must have its expectation **and** its `validation_schema` updated. (`get_reference_doc(kind="step-config")`)
- **Evals** — search `evaluation/` for the step id / output path. An eval that reads the changed output must be updated so it still resolves and scores the right thing. (`get_reference_doc(kind="evaluation-plan")`)
- **Report dashboard** — search `reports/report_plan.json` and the dashboard HTML's `window.report.query` SQL for the db tables/columns and output fields. A query that reads changed data must be fixed, and `report_plan.json` updated. (`get_reference_doc(kind="report-plan")`)
- **db** — read `db/README.md` (it already lists each table's writers + shape). If this step writes db and the shape changed, update the schema, the README contract, and any readers. (`get_reference_doc(kind="stores")`)
- **Learnings** — the step's `learnings/{step-id}/` and `learnings/_global/SKILL.md` were generated against the old behavior. If behavior changed, clear `lock_learnings`/`lock_code` so they regenerate, and prune any now-wrong notes.
- **KB** — search `knowledgebase/notes/` for the step's topic. Notes describing the old behavior must be updated or flagged.

It is tractable **because the contracts already exist** — `db/README.md` lists writers and shape, `report_plan.json` declares its queries, the eval plan declares scope, the plan holds the step handoffs. You are cross-linking what is already written, not inventing a dependency graph.

## 3. Reconcile or flag
For each affected dependent:
- **Fix it now** if the fix is clear and contained — update the consumer's schema, fix the report query, repair the eval source, update the db contract, clear a learning lock. (Back up first, or rely on the surrounding pass's backup.)
- **Flag an open finding** if the reconciliation needs judgment — a downstream step needs real redesign, a metric definition is now ambiguous. Record it in `builder/improve.html` so it isn't lost.

**Never leave a plan change with a silently broken dependent.**

## 4. Record an impact summary
After tracing, write a short **impact summary** into `builder/improve.html`: what changed (the surface), which dependents it touched across the six dimensions, what you reconciled, and what you flagged. This makes the blast radius auditable and tells the next pass what was already handled.

## The changelog is your work-list — keep it lean
Every plan-mod tool call is auto-written to `planning/changelog/changelog-*.json` (tool, `reason`, affected step ids, old/new values). Treat entries without `artifact_review.done=true` as the **ledger of changes whose blast radius may not be reconciled yet** — your work-list. When you reconcile a change (steps 1–3), **record its impact summary in `builder/improve.html`** so it is human-visible.

Do **not** edit or delete changelog files directly. The read-only Artifact Review agent returns exact inspected entries; the parent writer records the review in `builder/improve.html` and then marks those entries through the dedicated `mark_changelog_artifact_reviewed` tool. Pulse uses that metadata to skip future no-op review turns.

This proactive check is one end of a loop; the **`review-artifact-drift` audit** uses the read-only `review_artifact_sync` tool as the agentic backstop that sweeps the changelog and catches anything the proactive pass missed. Its parent writer advances the **Artifact Sync Cursor** in `builder/improve.html` and calls `mark_changelog_artifact_reviewed` only for exact inspected entries. Pulse handles the same concern inside its consolidated reviewer/fixer pass.

## Scope note
The discipline **scales to the change**. A change that is purely internal to a step (same output contract, same db writes, same described behavior) has no blast radius — confirm that quickly and move on. A renamed output field touches downstream + report + eval + db; a reworded description that keeps the same contract may only touch learnings. Trace what the surface actually reaches, not a fixed checklist for its own sake.
