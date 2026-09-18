[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-331 — concurrent webhook runs shared one mutable route decision

| Coordination | Value |
|---|---|
| State | Deployed; RTS workflow migrated to contract v1.0.42; overlapping-delivery live acceptance pending |
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

Contract v1.0.42 runs the trusted, idempotent
`migrate_run_scoped_routes` migration for every workflow. It rewires each
proven shared-mirror pattern to `context_dependencies`, gives every route
destination the same run-scoped dependency, and removes the obsolete shared
path from affected step instructions. Other `route_source_file` values remain
unchanged, including deliberately shared operator-controlled routing inputs.
The managed migration turn then removes obsolete compatibility writes and
fallback reads from affected scripted steps while preserving the producer's
run output and append-only database audit.

A read-only production census on 2026-09-18 found one unsafe plan,
`rtsprreviweer/pr-review-branch`. Three explicit route sources in
`automationtesting` use the ordinary `route_selection.json` contract and are
left unchanged. All other workflows take the idempotent no-op path.

## RTS migration and acceptance

- Release `e63b6c7-20260918161036` (`e63b6c783`) was deployed with user
  approval on 2026-09-18. The planner health endpoint and the agent, browser,
  gateway, and workspace services were healthy after deployment.
- `rtsprreviweer` was migrated from v1.0.41 to v1.0.42. The branch and both
  destinations now declare `context_dependencies: ["route_selection.json"]`;
  the branch no longer has a shared `route_source_file`.
- The gate's obsolete `db/assets/route_selection.json` compatibility write and
  the skip step's shared-path fallback were removed. The gate still writes
  `STEP_OUTPUT_DIR/route_selection.json` and preserves append-only
  `pr_gate_decisions` audit rows in SQLite.
- Static production validation passed: the workflow plan and affected scripts
  contain no shared route path, both Python scripts parse, and all three
  consumers declare the run-scoped dependency. A timestamped server backup
  was retained before migration.
- Remaining acceptance: send two overlapping eligible/skip webhook deliveries
  and verify each downstream step retains its own PR and run ID.

Focused and full `step_based_workflow` package tests pass locally.
