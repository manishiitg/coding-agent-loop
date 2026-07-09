## Execution Policy — Run ONE group at a time by default

When calling `run_full_workflow` for a multi-group workflow, **default
to sequential per-group execution** — pass
`group_name="<single-group>"` and wait for that group to finish before
starting the next. Only run groups in parallel when the user explicitly
says so ("run all groups", "run in parallel", "all at once", etc.).

## Why per-group by default

- **Cleaner failure signal.** A failure in group A is isolated; you
  can diagnose it before group B runs and reach a different conclusion
  than if both failed mixed-together.
- **Fixes propagate forward.** If you run and fix issues per group,
  group A's fixes apply BEFORE group B runs — group B benefits from
  the improvements without re-running A.
- **Avoids resource contention.** Browsers, MCP server connections,
  API rate limits, file-system contention all behave better with
  serialized groups than with N parallel workers fighting over them.
- **Earlier abort.** If group A reveals a structural problem (wrong
  selectors, missing variable, broken auth), you can stop the loop
  before wasting compute on B and C.
- **Iteration rotation still works correctly.** The first group's run
  triggers the paired iteration-0 → iteration-N rotation; subsequent
  partial-group runs in the same session reuse iteration-0, so all
  groups end up in the same iteration-0 by the end of the loop.

## The pattern

```
1. Read variables/variables.json → list of enabled group names.
2. for each group in groups:
     run_full_workflow(group_name="{group}")
     wait for completion via the system's [AUTO-NOTIFICATION] completion message; use list_executions / query_step only for one-off ambiguity checks, never as a sleep/poll loop
     if user asked for hardening or optimization, switch to Workshop mode
3. After all groups: summarize.
```

## Exceptions where parallel is appropriate

(Still requires explicit user signal.)

- User says "run all groups in parallel" / "all at once" / "fan out".
- Single-group workflow (only one group exists — there's nothing to
  serialize).
- The workflow's steps have no shared external resources (no browser,
  no API rate limits, no shared files) AND speed matters more than
  per-group debug clarity. Even then, prefer asking the user before
  defaulting to parallel.

## If the user is ambiguous

If the user says only "run the workflow" without naming groups,
default to sequential per-group and tell them: "I'm running groups one
at a time so any failures isolate cleanly. Say 'run in parallel' if
you'd rather fan out."
