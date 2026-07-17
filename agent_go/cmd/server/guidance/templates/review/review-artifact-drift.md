Use this as the read-only audit checklist for artifact drift after plan or configuration changes. It checks whether step config, learnings, saved code, knowledge-base notes, database contracts, reports, evaluation, and recent run evidence still match the current workflow. It also flags missing or stale eval coverage with an `Eval fix` owner label.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

## Execution model

- In Pulse, the parent passes this rendered checklist to one `call_generic_agent` reviewer in the consolidated parallel review batch.
- Outside Pulse, the parent may call `call_generic_agent` once with this checklist as its instructions.
- If you are already that generic reviewer, perform the audit directly. Never launch another reviewer, background tool, or nested maintenance agent.
- `call_generic_agent` is synchronous. Its direct result is authoritative; do not poll, sleep, call `query_step`, or wait for an auto-notification.
- The reviewer is strictly read-only. It must not edit files, mutate the plan/config, write `builder/improve.html`, mark changelog entries, or mark Pulse module state.

Load `get_reference_doc(kind="assumption-audit")`. While tracing changed surfaces, identify dependent artifacts that preserved an old architecture, tactic, schema, metric, or execution assumption after the plan evolved. Keep consequential unresolved restrictions under Pulse's Assumptions challenged.

## Audit checklist

1. Read `builder/improve.html` and its Artifact Sync Cursor when present.
2. List `planning/changelog/changelog-*.json` in filename order and select entries where `artifact_review.done` is not true.
   - If reviewed markers are absent but the cursor proves older entries were covered, identify those exact entries as `cursor-backfill`; do not re-audit them.
   - If no cursor exists and more than 100 entries are unreviewed, inspect only the latest 100 and report that the older entries remain unreviewed.
   - Never advance the proposed cursor past an entry that was not fully inspected or safely cursor-backfilled.
3. For each affected step, inspect only relevant current artifacts:
   - `planning/plan.json` and `planning/step_config.json`
   - `learnings/<step-id>/main.py`, script metadata, per-step learning metadata, and relevant `learnings/_global/` guidance
   - relevant `knowledgebase/notes/` content and KB access/contribution settings; treat `knowledgebase/context/` as read-only user-owned context
   - `db/README.md`, named DB tables/assets/contracts, and their writers/consumers
   - report HTML/SQL/data contracts and `reports/report_plan.json` when present
   - `evaluation/evaluation_plan.json`, `evaluation/step_config.json`, and matching goal/success-criteria coverage
   - one representative recent run for changed runtime behavior when evidence exists
   - for any changed status, strategy, feature flag, guard, routing rule, or
     other control value, trace the exact changed record to the current runtime
     reader and one resulting decision/output. If similarly named tables/files
     carry the same logical IDs, compare them and identify the canonical owner
     plus the required mirror rule. A clean changelog/file diff is not enough
     when the runtime reads a different store.
4. Record a finding only when evidence shows drift, including:
   - code, paths, fields, selectors, tool/API usage, or validation still implement an old contract
   - stale code/learning locks after a material change without review evidence
   - learnings or KB preserve obsolete behavior or agent-inferred policy
   - DB writers, report consumers, or eval consumers disagree on schema or semantics
   - a change updated a plausible but non-canonical store, or duplicate control
     stores disagree so the allocator/router/executor cannot observe the repair
   - report/eval checks use stale artifacts, fields, thresholds, or run identity
   - a changed success criterion lacks eval coverage, or an eval is orphaned/duplicative
   - deleted steps still have live references, or new steps lack required dependent wiring
5. Include clean checks briefly. Do not manufacture drift merely because an artifact exists.

## Reviewer result

Return one compact review package containing:

- cursor before and proposed cursor after
- changelog files and zero-based entry indexes fully inspected
- affected steps inspected
- findings ordered by severity, with exact evidence and recommended owner
- clean checks
- exact proposed marks grouped as `clean`, `findings`, or `cursor-backfill`
- any blocked entry that prevented further cursor advancement

The parent Pulse Fixer/workshop agent validates this package, applies only bounded approved fixes, writes one compact **Signals / Kizuki** Artifact Review item to `builder/improve.html` using `data-pulse-section="signals"` and `data-module="artifact_review"`, advances the visible cursor, and calls `mark_changelog_artifact_reviewed` for only the exact verified entries. Do not edit or delete changelog JSON directly and do not create a second cursor or state file.
