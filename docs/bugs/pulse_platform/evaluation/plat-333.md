[← Pulse platform index](../../pulse_platform_issue_register.md)

# PLAT-333 — Eval subsystem removal with producer-owned measurement

| Coordination | Value |
|---|---|
| Ticket state | `implemented locally; not committed, not deployed` |
| Last synchronized | `2026-09-19` |
| Priority | `P0 direction` |

## Background

The file/run-based workflow evaluation subsystem is being deleted per
[eval_removal_plan.md](../../../workflow/eval_removal_plan.md). A mandatory
replacement migration (contract 1.0.43: every workflow gains a
`measurement-router` → `measure-outcomes` route plus a `workflow_metrics`
table) was implemented, then rejected in final review before shipping.

## Review findings

- **[P0]** The migration imposed unnecessary topology: many steps already
  calculate and store the outcomes Pulse needs.
- **[P0]** The proposed router cannot be created: one routing step per
  workflow, and routes may not point directly at `end`.
- **[P0]** The migration used the nonexistent `add_regular_step` tool (the
  real `add_scripted_step` does not create the required `main.py`).
- **[P1]** An appended router does not connect every execution path
  (routes, branches, explicit `next_step_id: "end"` edges bypass it).
- **[P1]** `workflow_metrics` was documentation only: no runtime code reads
  or writes it. Pulse and Goal Advisor consume `workflow_goal_metrics` /
  `pulse_goal_observations` through `get_goal_metrics`.
- **[P1]** Table-scoped DB authorization does not exist (steps get
  database-wide read-write), and `RUN_FOLDER` reuses `iteration-0`, so the
  promised permissions and immutable measurement identity were unachievable.
- **[P2]** The migration test only checked prompt strings, so the suite
  stayed green despite the migration being unusable.

## Decision

No mandatory route, step, or table. Any normal step or scheduled collector
may produce a run-scoped, evidence-backed measurement; Pulse does not depend
on which step or route produced it. Per legacy eval: reuse a producer's DB
output, add `record_goal_observations` only for Pulse history, localize to
routes, converge or dedicate a step only when genuinely required, otherwise
change nothing, and flag ambiguity for manual migration. Old evaluation
artifacts stay read-only history.

## Implemented

- Deleted the 1.0.43 migration, its test, and the version const; the upgrade
  chain ends at 1.0.42. Added `TestNoUpgradeMandatesMeasurementTopology`, a
  full-chain guard against the rejected topology returning.
- Rewrote `measurement-plan.md`, the Goal Advisor/optimizer prompts,
  eight guidance templates, and the `MetricSource` schema to the flexible
  contract (producer outputs + goal observations).
- Scrubbed live eval path/endpoint references from active docs and schemas
  (e.g. `workflow_monitoring.md` claimed the removed
  `/api/workflow/evaluation-reports` endpoint was current).
- Rebuilt the frontend and synced `agent_go/static/`; deleted the orphaned
  `PulseEvalSummary` bundles.

## Verification

- `go build ./...`, `go vet ./...`, `go test ./... -count=1`: green.
  (`TestSlackAllocatorAcrossProcesses` flaked once under full-suite load,
  then passed solo and in-package with no code change.)
- `tsc -b`: clean; prompt-size ceiling holds (23935/24000).
- `vitest`: 1422 passed; the 9 failures fail identically on a clean HEAD
  worktree (pre-existing, unrelated).
- Grep gate (see the plan): zero unmarked hits; remaining matches are
  documented retired-table guards, historical classifiers, legacy-marked
  doc lines, and tests pinning retired behavior.
