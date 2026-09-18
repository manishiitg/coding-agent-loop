[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-331 — concurrent webhook runs shared one mutable route decision

| Coordination | Value |
|---|---|
| State | Implemented and regression-tested locally; workflow migration, deployment, and live acceptance pending |
| Date | 2026-09-18 |
| Owner | step execution / deterministic routing |
| Related | [PLAT-328](plat-328.md) |

## Incident

RTS PR Reviewer run `iteration-68-hook` received `pull_request/opened` for
`course_designer#87` (`339f74cb-4cd2-51e2-9b94-9f9e47aae9b0`) at
15:41:09 UTC. Its gate correctly wrote a run-scoped `route_selection.json`
with `select_route=review`, and the branch selected `review` at 15:41:10.

Run `iteration-69-hook` then received `pull_request/closed` for PR #82 and
wrote `select_route=skip` at 15:41:19. The review step from iteration 68 was
instructed to reread the workflow-shared `db/assets/route_selection.json`, so
it saw iteration 69's PR identity and skipped #87. The two immutable run
artifacts were correct; the compatibility mirror introduced for PLAT-328 was
the race.

## Platform contract

Step outputs, including gate decisions and resolved routing/branch decisions,
belong under:

`runs/<iteration>/<group>/execution/<step-id>/`

`db/assets` remains available for deliberately shared workflow inputs, but a
dynamic route produced earlier in the same run must be consumed through
`context_dependencies: ["route_selection.json"]`.

The executor now persists every resolved routing or branch decision as its own
run-scoped `route_selection.json`. Plan mutations reject the narrow unsafe
case where a prior step declares run-scoped `route_selection.json` but the
router points at `db/assets/route_selection.json`. Legacy plans remain loadable
so Builder can repair them.

## RTS migration and acceptance

- Remove the gate's write to `db/assets/route_selection.json`.
- Change `pr-review-branch` from shared `route_source_file` to
  `context_dependencies: ["route_selection.json"]`.
- Give `record-skip` and `basic-pr-review` the same dependency and remove all
  instructions to read the shared mirror.
- Keep append-only `pr_gate_decisions` rows in SQLite as durable audit history.
- Deploy only after user approval, then send two overlapping eligible/skip
  webhook deliveries and verify each downstream step retains its own PR and
  run ID.

Focused and full `step_based_workflow` package tests pass locally.
